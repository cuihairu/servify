package scripts

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 本测试守护 CI「Script Checks / Verify provider acceptance scripts」步骤的
// -run 白名单与 scripts 包实际测试面同步。
//
// 背景（2026-09-30 审计发现）：该步骤用一条手工维护的 alternation 正则挑选
// 要在 CI 跑的脚本守卫测试。白名单是逐笔追加的——每加一个守卫测试就要记得把
// 测试名登记进去，漏登记不会让任何门禁变红，只会让该守卫在 CI 里静默不跑。
// 实际已发生：2026-09-29 新增的两个 compose 运行时守卫
// （TestComposeServifyHealthcheckProbeExistsInRuntimeImage /
// TestComposeServifyMountsLiveUnderImageWorkdir）进了仓库但漏登记，
// 本地 go test ./scripts 全绿、CI 也全绿，唯独这两个测试在 CI 上从未执行过
// （81 个测试函数里恰好漏这 2 个）。这类漂移靠人记 inevitably 复发，
// 故用元测试把它变成契约：新增守卫测试而未登记白名单 → 本测试红。
//
// 纯静态解析（go/parser 读 AST + 正则读 ci.yml），不执行子测试、不依赖
// docker/网络，CI 可跑。

const (
	ciWorkflowPath  = "../.github/workflows/ci.yml"
	scriptTestFiles = "_test.go"
)

// ciScriptChecksRunRegex 抓出 CI 里那条 `go test ./scripts -run "Test(...)"`。
// 用宽松前缀 + 抓最后一个引号内容，避免 alternation 内部的引号/括号影响切分。
var ciScriptChecksRunRegex = regexp.MustCompile(`go test \./scripts\s+-run\s+"([^"]+)"`)

// scriptsTestFuncNames 用 go/parser 列出 scripts 包里全部顶层 Test 函数名
// （Go 会对 Test 开头且 *testing.T 参数的函数按签名归类，这里按名字前缀取，
// 与 go test 的选取口径一致——本包无 TestMain/Benchmark/Example 干扰）。
func scriptsTestFuncNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read scripts dir: %v", err)
	}
	fset := token.NewFileSet()
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), scriptTestFiles) {
			continue
		}
		file, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			if strings.HasPrefix(fn.Name.Name, "Test") {
				names = append(names, fn.Name.Name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("scripts 包未解析出任何 Test 函数（解析口径失效？）")
	}
	sort.Strings(names)
	return names
}

// ciScriptChecksPattern 返回 CI script-checks 步骤的 -run 正则（已 strip 外层
// "Test(...)" 的 Test 前缀与锚点，留下一段可逐测试名匹配的 alternation 主体）。
func ciScriptChecksPattern(t *testing.T) *regexp.Regexp {
	t.Helper()
	raw, err := os.ReadFile(ciWorkflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", ciWorkflowPath, err)
	}
	m := ciScriptChecksRunRegex.FindAllStringSubmatch(string(raw), -1)
	if len(m) == 0 {
		t.Fatalf("%s 未找到 `go test ./scripts -run \"...\"` 调用（Script Checks 步骤被改名或删除？）", ciWorkflowPath)
	}
	if len(m) > 1 {
		t.Fatalf("%s 中出现 %d 处 `go test ./scripts -run`（本测试按唯一调用点解析；多处请扩展本测试）", ciWorkflowPath, len(m))
	}
	rx, err := regexp.Compile(m[0][1])
	if err != nil {
		t.Fatalf("CI -run 正则非法 %q: %v", m[0][1], err)
	}
	return rx
}

// TestCIScriptChecksCoversEveryScriptsTest 自身也在这条枚举里——它要求
// scripts 包每个 Test 函数都被白名单命中，故本元测试无法在不登记自己的情况下
// 通过（自指要求，无需另立一条）。
func TestCIScriptChecksCoversEveryScriptsTest(t *testing.T) {
	rx := ciScriptChecksPattern(t)
	names := scriptsTestFuncNames(t)

	var missing []string
	for _, name := range names {
		if !rx.MatchString(name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		// 漏登记的守卫在 CI 上根本不会执行——本地全绿、CI 全绿，缺陷照样能合进
		// 干。逐条点名，便于照单补进 ci.yml 的 alternation。
		for _, name := range missing {
			t.Errorf("scripts 测试 %s 未登记进 %s 的 script-checks -run 白名单（该守卫在 CI 上不会被执行）", name, ciWorkflowPath)
		}
	}
}

// 守恒性：测试被删/改名后，白名单里留下的陈旧登记会让 alternation 静默失准
// （匹配不到任何东西，不报错、不变红，看着一切正常实则守卫已失效）。
//
// 白名单是嵌套分组的手写 alternation，条目并非独立测试名，而是真实测试名的
// 片段：Compose(Servify(HealthcheckProbe…)) 里的 Servify/HealthcheckProbe…、
// WeKnoraIntegrationScript(RealMode…) 里的 RealMode… 都是如此。故判据取
// 「该 token 是否出现在某个真实测试名里」——出现即对应某条真实测试（分组前缀
// 或嵌套子分支皆满足），一个都不出现才是陈旧项或拼写错误。
func TestCIScriptChecksWhitelistHasNoStaleEntries(t *testing.T) {
	rx := ciScriptChecksPattern(t)
	known := scriptsTestFuncNames(t)

	// 白名单主体里可穷举的标识符片段（非正则元字符）。
	body := strings.TrimSuffix(strings.TrimPrefix(rx.String(), "Test("), "$")
	var stale []string
	for _, tok := range regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{11,}`).FindAllString(body, -1) {
		found := false
		for _, name := range known {
			if strings.Contains(name, tok) {
				found = true
				break
			}
		}
		if !found {
			stale = append(stale, tok)
		}
	}
	for _, tok := range stale {
		t.Errorf("白名单条目片段 %q 不对应 scripts 包任何测试函数（测试被删/改名后的陈旧登记会让 alternation 静默失准）", tok)
	}
}
