package scripts

import (
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
