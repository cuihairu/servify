package scripts

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// lint 门与测试门 build-tag 口径守卫（2026-10-11）。
//
// 踩过的坑：lint 曾跑不带标签的 `golangci-lint run ./...`，而测试门
// （scripts/run-tests.sh）跑 `go test -tags integration ./apps/server/...`。
// 于是 integration 标签下的测试文件对 unused 不可见，包内 helper 被误判
// 死代码删掉，反过来击穿带标签的编译（customer/routing 的 uniqueMemDSN
// 两处踩实，`undefined: uniqueMemDSN`）。两个门必须分析同一源码集，
// 否则任何一门的「绿」都不作数。
//
// 顺带钉住门的三件套：仓库根 .golangci.yml 的 run.build-tags 与
// linters.default、Makefile 与 CI 的 0 上限 flags（默认单 linter 截断
// 50 条会藏真问题——实际曾有 214 条被截成 50）。只做静态文本解析，
// 不执行 lint/go，CI 可跑。

const (
	lintGolangciConfigPath = "../.golangci.yml"
	lintMakefilePath       = "../Makefile"
	lintCIPath             = "../.github/workflows/ci.yml"
	lintRunTestsShPath     = "../scripts/run-tests.sh"
)

func requireContains(t *testing.T, path, want, why string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(raw), want) {
		t.Fatalf("%s 缺少 %q（%s）", path, want, why)
	}
}

// TestLintGateConfigPinsTestGateParity 钉住 lint 门与测试门 build-tag 同口径。
func TestLintGateConfigPinsTestGateParity(t *testing.T) {
	requireContains(t, lintGolangciConfigPath, "build-tags: integration",
		"lint 必须与测试门分析同一源码集，否则 unused 对 integration 标签下的 helper 误判")
	requireContains(t, lintGolangciConfigPath, "default: standard",
		"显式钉住默认 linter 集，防上游默认漂移悄悄扩面")

	// 测试门锚点：run-tests.sh 带 -tags integration，是 build-tag 口径的源头。
	requireContains(t, lintRunTestsShPath, "-tags integration",
		"测试门口径，lint 门必须与其对齐")
}

func TestLintGateFlagsUncappedEverywhere(t *testing.T) {
	// 默认 max-issues-per-linter=50 会截断：实际 214 条被显示成 50 条，
	// 藏真问题。Makefile 与 CI 两处都必须显式置 0。
	for _, flag := range []string{"--max-issues-per-linter 0", "--max-same-issues 0"} {
		requireContains(t, lintMakefilePath, flag, "Makefile lint 目标必须置 0 上限")
		requireContains(t, lintCIPath, flag, "CI Lint step 必须置 0 上限")
	}
}

func TestCILintStepPinsVersion(t *testing.T) {
	raw, err := os.ReadFile(lintCIPath)
	if err != nil {
		t.Fatalf("read %s: %v", lintCIPath, err)
	}
	re := regexp.MustCompile(`(?m)^\s*version:\s*v2\.14\.0\s*$`)
	// 只认 Lint step 里的 version 钉版；SDK/admin 的 npm 侧无此行。
	if !re.Match(raw) {
		t.Fatalf("CI Lint step 必须钉 golangci-lint version v2.14.0，防上游漂移")
	}
}
