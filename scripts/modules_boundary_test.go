package scripts

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModulesDoNotImportLegacyServices 是 modules 边界门禁：
// internal/modules 下的任何 .go 文件（含测试）都不得再 import internal/services。
// 契约类型已下沉到各模块（modules/ai/delivery、modules/customer/application、
// modules/knowledge/application），legacy services 通过类型别名反向引用同一份定义。
func TestModulesDoNotImportLegacyServices(t *testing.T) {
	root := filepath.Join("..", "apps", "server", "internal", "modules")
	forbidden := "servify/apps/server/internal/services"

	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == forbidden {
				offenders = append(offenders, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk modules dir: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("modules must not import internal/services (contract types live in each module):\n%s",
			strings.Join(offenders, "\n"))
	}
}

const (
	// modelsPkgPath 是共享核心与过渡别名所在的包（modules-dependency-map §2）。
	modelsPkgPath = "servify/apps/server/internal/models"
	// ownerPkgPrefix：models 包中指向已迁出模块自有类型的 import 都落在
	// internal/modules/<owner> 下。
	ownerPkgPrefix = "servify/apps/server/internal/modules/"
)

// TestModulesDoNotUseMigratedTypeAliases 是 shared kernel 收口方向在引用面的
// 门禁（modules-dependency-map §3.1 / todo.md P1-7 剩余方向，2026-09-30）：
// 已迁出到各模块的自有类型（类型别名、常量别名、转发函数）在 internal/models
// 保留的只是过渡入口，供 bootstrap/migrate、handlers 的 swag 注解、platform
// provider 等 legacy 引用方零改动；modules 内部必须经 owner 包直连引用同一份
// 定义，不得再借道这些别名——否则模块边界在源码面上永远收不干净，别名也永远
// 删不掉（最终态清理的前置条件）。
//
// 已迁出集合从 models 包源码动态解析：定义处引用了 internal/modules/ 下 owner
// 包的导出标识符（type A = pkg.B、const/var A = pkg.B、函数体转发 pkg.X）都算
// 过渡入口，随新迁移自动扩展，无需维护硬编码清单。解析结果为空视为守卫自身
// 失配（models 包被重写或语法变化）直接失败，防假绿。
//
// 纯静态 AST 解析（go/parser，含测试文件），不执行代码、不依赖网络，CI 可跑；
// 测试名已登记 ci.yml script-checks 白名单（元守卫强制）。
func TestModulesDoNotUseMigratedTypeAliases(t *testing.T) {
	modelsDir := filepath.Join("..", "apps", "server", "internal", "models")
	migrated, aliasCount := collectMigratedIdentifiers(t, modelsDir)
	if aliasCount == 0 || len(migrated) == 0 {
		t.Fatalf("models 包未解析出任何已迁出过渡入口（别名 %d 个、合计标识符 %d 个）——"+
			"models 包被重写或解析失配，守卫失去意义，先修解析再放行", aliasCount, len(migrated))
	}

	// 扫 modules：按「import internal/models 的限定符（含显式别名）」收集
	// SelectorExpr 的 X，命中已迁出集合即违规。按限定符而非写死 "models"，
	// 连 import 别名写法一起覆盖；局部变量 shadow 造成的误报概率可忽略，
	// 误报优于漏报。
	root := filepath.Join("..", "apps", "server", "internal", "modules")
	fset := token.NewFileSet()
	var violations []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		quals := map[string]bool{}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) != modelsPkgPath {
				continue
			}
			if imp.Name != nil {
				quals[imp.Name.Name] = true
			} else {
				quals["models"] = true // models 包声明名
			}
		}
		if len(quals) == 0 {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, isIdent := sel.X.(*ast.Ident)
			if !isIdent || !quals[id.Name] {
				return true
			}
			if owner, hit := migrated[sel.Sel.Name]; hit {
				violations = append(violations, fmt.Sprintf("%s:%d: models.%s → 已迁出至 %s，改经 owner 包直连",
					path, fset.Position(sel.Pos()).Line, sel.Sel.Name, owner))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk modules dir: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("modules 引用了 internal/models 的已迁出过渡入口（%d 处）——\n"+
			"modules 对已迁出类型必须经 owner 包直连（modules-dependency-map §3 定稿）：\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}

// collectMigratedIdentifiers 解析 models 包（跳过测试文件）中「定义处引用了
// internal/modules owner 包」的导出标识符，返回 标识符→owner 包路径 与
// 类型别名计数（后者是防解析失配的关键指标）。
func collectMigratedIdentifiers(t *testing.T, modelsDir string) (map[string]string, int) {
	t.Helper()
	entries, err := os.ReadDir(modelsDir)
	if err != nil {
		t.Fatalf("read %s: %v", modelsDir, err)
	}
	fset := token.NewFileSet()
	migrated := map[string]string{}
	typeAliasCount := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(modelsDir, e.Name())
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		// 该文件里指向 owner 包的 import 限定符 → owner 包路径。
		qual2owner := map[string]string{}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(p, ownerPkgPrefix) {
				continue
			}
			qual := ownerPackageName(t, p)
			if imp.Name != nil {
				qual = imp.Name.Name
			}
			qual2owner[qual] = p
		}
		if len(qual2owner) == 0 {
			continue
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						// type A = pkg.B：仅类型别名（Assign 位置有效）且
						// RHS 是 owner 包选择器时才算迁出别名。
						if !s.Assign.IsValid() {
							continue
						}
						if owner, ok := ownerSelectorPath(s.Type, qual2owner); ok && s.Name.IsExported() {
							migrated[s.Name.Name] = owner
							typeAliasCount++
						}
					case *ast.ValueSpec:
						// const/var A = pkg.B：同名常量/变量别名。
						for _, v := range s.Values {
							if owner, ok := ownerSelectorPath(v, qual2owner); ok {
								for _, name := range s.Names {
									if name.IsExported() {
										migrated[name.Name] = owner
									}
								}
							}
						}
					}
				}
			case *ast.FuncDecl:
				// func A(...) ... { ... pkg.X(...) }：转发函数（如 NewEmbedding）。
				if !d.Name.IsExported() || d.Body == nil {
					continue
				}
				owner := ""
				ast.Inspect(d.Body, func(n ast.Node) bool {
					if owner != "" {
						return false
					}
					if o, ok := ownerSelectorPath(n, qual2owner); ok {
						owner = o
						return false
					}
					return true
				})
				if owner != "" {
					migrated[d.Name.Name] = owner
				}
			}
		}
	}
	return migrated, typeAliasCount
}

// ownerSelectorPath 判断节点是否为「owner 包限定符.Selector」。
func ownerSelectorPath(expr ast.Node, qual2owner map[string]string) (string, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	owner, hit := qual2owner[id.Name]
	return owner, hit
}

// ownerPackageName 读 owner 包目录的 package 声明名（import 无显式别名时的
// 限定符来源；owner 包多为通用名 domain/infra，必须读不能猜）。
func ownerPackageName(t *testing.T, pkgPath string) string {
	t.Helper()
	rel := strings.TrimPrefix(pkgPath, "servify/apps/server/")
	dir := filepath.Join("..", "apps", "server", rel)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read owner pkg dir %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.PackageClauseOnly)
		if parseErr != nil {
			t.Fatalf("parse package clause %s: %v", e.Name(), parseErr)
		}
		return f.Name.Name
	}
	t.Fatalf("owner 包 %s 下无 .go 文件", pkgPath)
	return ""
}
