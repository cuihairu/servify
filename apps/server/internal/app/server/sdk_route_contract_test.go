package server

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"servify/apps/server/internal/platform/eventbus"
)

// P0-6 SDK↔后端协议漂移守卫（2026-09-30 全库复审收口）。
//
// 背景：SDK（core/web/app-core/mobile/widget/示例/文档）里字面引用的 REST 端点
// 必须存在于 BuildRuntime→BuildRouter 装配出的真实路由面。历史上
// getCustomerSessions() 调用从未注册的 GET /api/customers/:id/sessions 恒 404，
// 靠人工 review 无法拦截复发。本测试把「SDK 引用的端点」与「服务端真实注册的
// 路由」做机器对账：任何新增的字面端点若不在路由面（或 allowlist）里即失败。
//
// 扫描范围（与 todo P0-6 决策一致）：sdk/** 与 apps/demo-sdk/** 下的
// 源码（ts/tsx/js/mjs/kt/swift/vue/html）与 Markdown；排除 node_modules、
// 构建产物、测试目录与测试文件（fixtures 天然允许虚构端点）、生成物
// servify-sdk.*.js（由 scripts/check-generated-drift.sh 覆盖）。
// sdkGuardConditionalRoutes：服务端存在但按配置条件注册的路由，默认装配面
// （测试配置）里缺席。SDK/文档对它们的引用合法，故豁免。
var sdkGuardConditionalRoutes = map[string]string{
	"/api/v1/ws/voice": "router_realtime.go：VoiceHub+VoiceTranslationRuntime 非 nil 才注册（ai.asr 门控），测试配置下不在路由面",
}

// sdkGuardDocOnlyPaths：仅作为「反例/迁移说明」出现的旧端点，服务端刻意无此路由
// （WebSocket-first 已取代）。出现处必须仍是文档语境；若未来真出现对应路由，
// 本豁免自然不再命中（不阻塞），可随后清理。
var sdkGuardDocOnlyPaths = map[string]string{
	"/api/sessions": "demo-sdk README 迁移说明引用的 WebSocket-first 之前的旧端点，服务端现无此路由",
	"/api/messages": "demo-sdk README 迁移说明引用的 WebSocket-first 之前的旧端点，服务端现无此路由",
}

var (
	sdkGuardPathRe   = regexp.MustCompile(`/(?:api|public|uploads)/[A-Za-z0-9_./-]*`)
	sdkGuardMethodRe = regexp.MustCompile(`'(GET|POST|PUT|DELETE|PATCH)'|"(GET|POST|PUT|DELETE|PATCH)"`)
)

// sdkGuardSkipDirs：扫描时整目录跳过。
var sdkGuardSkipDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	"build":        true,
	"__tests__":    true,
	"test":         true, // Android src/test fixtures
	"tests":        true, // iOS Tests fixtures
	"Test":         true,
	"Tests":        true,
	".git":         true,
}

var sdkGuardExts = map[string]bool{
	".ts": true, ".tsx": true, ".jsx": true, ".js": true, ".mjs": true,
	".cjs": true, ".kt": true, ".swift": true, ".md": true, ".vue": true,
	".html": true,
}

func sdkGuardSkipFile(base string) bool {
	if strings.Contains(base, ".test.") {
		return true
	}
	if strings.HasSuffix(base, ".d.ts") {
		return true
	}
	// 生成物（sync-sdk-to-demo.sh 产出，由 check-generated-drift.sh 单独把关）
	if strings.HasPrefix(base, "servify-sdk.") && strings.HasSuffix(base, ".js") {
		return true
	}
	return false
}

// sdkGuardSegMatch：按段匹配 gin 路由模板（:param 任意段，*wild 吃掉余下所有段）。
func sdkGuardSegMatch(pattern, actual string) bool {
	pattern = strings.TrimSuffix(pattern, "/")
	actual = strings.TrimSuffix(actual, "/")
	ps := strings.Split(strings.Trim(pattern, "/"), "/")
	as := strings.Split(strings.Trim(actual, "/"), "/")
	if pattern == "" {
		return actual == ""
	}
	for i, p := range ps {
		if strings.HasPrefix(p, "*") {
			return len(as) >= i
		}
		if i >= len(as) {
			return false
		}
		if strings.HasPrefix(p, ":") {
			continue
		}
		if p != as[i] {
			return false
		}
	}
	return len(as) == len(ps)
}

// sdkGuardRouteMatches：字面端点是否命中某条路由。尾斜杠字面量是拼接前缀
// （如 `/api/v1/sessions/` + id），按前缀判定；其余按段精确匹配。
func sdkGuardRouteMatches(routePath, literal string) bool {
	if strings.HasSuffix(literal, "/") {
		return strings.HasPrefix(routePath, literal) ||
			strings.TrimSuffix(routePath, "/") == strings.TrimSuffix(literal, "/")
	}
	return sdkGuardSegMatch(routePath, literal)
}

func sdkGuardPathExists(routes map[string]map[string]bool, literal string) bool {
	for rp := range routes {
		if sdkGuardRouteMatches(rp, literal) {
			return true
		}
	}
	return false
}

func sdkGuardMethodExists(routes map[string]map[string]bool, literal, method string) bool {
	for rp, ms := range routes {
		if sdkGuardRouteMatches(rp, literal) && ms[method] {
			return true
		}
	}
	return false
}

// sdkGuardPrefixOfRoute：文档宽松模式——字面量是某路由的段边界前缀（如 `/api/v1`）。
func sdkGuardPrefixOfRoute(routes map[string]map[string]bool, literal string) bool {
	p := strings.TrimSuffix(literal, "/")
	for rp := range routes {
		if rp == p || strings.HasPrefix(rp, p+"/") {
			return true
		}
	}
	return false
}

// sdkGuardStripComment：去掉行内 // 注释（避免把 https:// 或注释里的提及
// 当端点）与同行 /* */ 片段；仅对非 Markdown 源码生效。
func sdkGuardStripComment(line string) string {
	for i := 0; i+1 < len(line); {
		idx := strings.Index(line[i:], "//")
		if idx == -1 {
			break
		}
		abs := i + idx
		if abs == 0 || line[abs-1] != ':' {
			line = line[:abs]
			break
		}
		i = abs + 2
	}
	s := strings.Index(line, "/*")
	e := strings.Index(line, "*/")
	if s != -1 && e != -1 && e > s {
		line = line[:s] + line[e+2:]
	}
	return line
}

// TestSDKRouteContractDrift 扫描 SDK 与 demo widget/文档里的字面 REST 端点，
// 断言全部落在真实装配路由面（或附理由的 allowlist）内。
func TestSDKRouteContractDrift(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	rt, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer func() { _ = rt.Stop(context.Background()) }()

	routes := make(map[string]map[string]bool)
	for _, route := range BuildRouter(rt.RouterDependencies()).Routes() {
		ms, ok := routes[route.Path]
		if !ok {
			ms = make(map[string]bool)
			routes[route.Path] = ms
		}
		ms[route.Method] = true
	}
	// 下限自检：装配面缩水意味着守卫在放空枪（漏检）。阈值取实际值 192 条
	// 不同路径（2026-09-30 实测；method+path 组合 234）下方留 ~6% 余量。
	if len(routes) < 180 {
		t.Fatalf("装配路由面异常偏小（%d 条，期望 ≥180），守卫会漏检，先修 BuildRuntime 测试装配", len(routes))
	}

	repoRoot := repoRootForTest(t)
	roots := []string{
		filepath.Join(repoRoot, "sdk"),
		filepath.Join(repoRoot, "apps", "demo-sdk"),
	}

	var misses []string
	scannedFiles := 0
	checkedLiterals := 0

	for _, scanRoot := range roots {
		if _, err := os.Stat(scanRoot); err != nil {
			t.Fatalf("扫描根缺失 %s: %v", scanRoot, err)
		}
		err := filepath.WalkDir(scanRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if sdkGuardSkipDirs[d.Name()] {
					return fs.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if !sdkGuardExts[ext] || sdkGuardSkipFile(d.Name()) {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			scannedFiles++
			lines := strings.Split(string(raw), "\n")
			isDoc := ext == ".md"
			scanLines := lines
			if !isDoc {
				scanLines = make([]string, len(lines))
				for i, l := range lines {
					scanLines[i] = sdkGuardStripComment(l)
				}
			}
			rel, _ := filepath.Rel(repoRoot, path)
			for i, line := range scanLines {
				// RFC2606 示例域名（https://xxx.example.com/...）不作端点证据
				if strings.Contains(line, "example.com") {
					continue
				}
				for _, m := range sdkGuardPathRe.FindAllString(line, -1) {
					checkedLiterals++
					if _, ok := sdkGuardConditionalRoutes[m]; ok {
						continue
					}
					if _, ok := sdkGuardDocOnlyPaths[m]; ok {
						continue
					}
					// this.request 调用（前 2 行窗口内）做方法级对账
					lo := i - 2
					if lo < 0 {
						lo = 0
					}
					window := strings.Join(scanLines[lo:i+1], "\n")
					miss := ""
					if strings.Contains(window, "this.request") {
						if mm := sdkGuardMethodRe.FindStringSubmatch(window); mm != nil {
							method := mm[1]
							if method == "" {
								method = mm[2]
							}
							if !sdkGuardMethodExists(routes, m, method) {
								miss = method
							}
						} else if !sdkGuardPathExists(routes, m) {
							miss = "(no method)"
						}
					} else if strings.HasSuffix(m, "/") {
						// 拼接前缀字面量（尾斜杠）：前缀命中即可
						if !sdkGuardPathExists(routes, m) {
							miss = "(prefix)"
						}
					} else if isDoc {
						if !sdkGuardPathExists(routes, m) && !sdkGuardPrefixOfRoute(routes, m) {
							miss = "(doc)"
						}
					} else if !sdkGuardPathExists(routes, m) {
						miss = "(path)"
					}
					if miss != "" {
						misses = append(misses, fmt.Sprintf("%s:%d: %s %s", rel, i+1, m, miss))
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", scanRoot, err)
		}
	}

	if len(misses) > 0 {
		sort.Strings(misses)
		t.Errorf("SDK↔后端协议漂移：发现 %d 处字面端点不在装配路由面：\n  %s\n"+
			"修法：改 SDK/文档对齐真实路由（后端能力确实存在），或在 sdkGuardConditionalRoutes /\n"+
			"sdkGuardDocOnlyPaths 登记豁免（必须写明理由）。",
			len(misses), strings.Join(misses, "\n  "))
	}
	t.Logf("scanned %d files, %d endpoint literals, %d distinct routes: no drift",
		scannedFiles, checkedLiterals, len(routes))
}
