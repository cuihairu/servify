package scripts

import (
	"fmt"
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
	// modelsPkgPath 是共享核心所在的包（modules-dependency-map §2）。
	modelsPkgPath = "servify/apps/server/internal/models"
	// ownerPkgPrefix：模块自有类型的 import 都落在 internal/modules/<owner> 下。
	ownerPkgPrefix = "servify/apps/server/internal/modules/"
)

// TestModelsDoesNotImportModulePackages 是 shared kernel 单向分层的门禁
// （modules-dependency-map §2/§3）：internal/models 是底层共享核心，模块自有
// 类型由各 owner 包定义、modules 单向依赖 models，故 models 不得反向 import
// internal/modules。
//
// 这正是过渡别名的唯一载体形态：`type X = knowledgedomain.X`、`const A = pkg.B`、
// 转发函数都必须先 import owner 包。前身守卫 TestModulesDoNotUseMigratedTypeAliases
// 查的是「modules 不得借道 models 别名」，别名在 2026-10-14 全部删除后该集合恒空、
// 守卫自身触发防假绿分支而失效；改钉这条更强的下层前提——只要 models 不 import
// 模块包，别名（连同常量别名与转发函数）就不可能重新长出来，无需维护类型清单。
//
// 纯静态解析（go/parser，含测试文件），不执行代码、不依赖网络，CI 可跑；
// 测试名已登记 ci.yml script-checks 白名单（元守卫强制）。
func TestModelsDoesNotImportModulePackages(t *testing.T) {
	root := filepath.Join("..", "apps", "server", "internal", "models")
	parsed := 0
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
		parsed++
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, ownerPkgPrefix) {
				offenders = append(offenders, fmt.Sprintf("%s: %s", path, p))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk models dir: %v", err)
	}
	if parsed == 0 {
		t.Fatalf("%s 下未解析出任何 .go 文件——路径漂移或守卫失配，先修解析再放行", modelsPkgPath)
	}
	if len(offenders) > 0 {
		t.Fatalf("internal/models（共享核心）不得 import internal/modules（%d 处）——"+
			"模块自有类型由 owner 包定义、modules → models 单向，别名/常量别名/转发函数一律不得回流 models：\n%s",
			len(offenders), strings.Join(offenders, "\n"))
	}
}
