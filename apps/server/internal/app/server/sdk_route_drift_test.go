package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"servify/apps/server/internal/platform/eventbus"

	"github.com/sirupsen/logrus"
)

// P0-6 SDK ↔ 后端协议漂移守卫。
//
// 漂移形态：SDK 声明了一个后端从未注册的 REST 路径，调用方恒得 404，
// 但 SDK 侧测试全绿（mock fetch 不校验路由）、后端侧测试也全绿
// （没人从 SDK 反查路由面），于是漂移长期潜伏。历史实例：
// getCustomerSessions 长期打 `GET /api/customers/:id/sessions`，
// 而 RegisterCustomerRoutes 只有 CRUD + activity/notes/tags/revoke-tokens。
//
// 基准取运行时路由面（gin r.Routes()）而非静态解析 router_*.go：
// 后者漏掉组前缀拼接与装配门控（ai.asr 未配置 → /api/v1/ws/voice 不注册，
// 该路由面由 voice_route_wiring_test.go 单独守护）。
const (
	sdkCoreAPIPath     = "sdk/packages/core/src/api.ts"
	sdkCoreSDKPath     = "sdk/packages/core/src/sdk.ts"
	sdkWebSocketPath   = "/api/v1/ws"
	sdkMinRequestSites = 20
)

// sdkCallSite 是 ApiClient 里一个 this.request(...) 调用点。
type sdkCallSite struct {
	Method string
	Path   string // 归一化后：已剥 query，模板插值折叠为 :p
	Line   int
}

// TestSDKRESTEndpointsResolveAgainstServerRoutes 核心守卫：SDK 每个 REST
// 调用点都必须落在服务端当前路由面上。
func TestSDKRESTEndpointsResolveAgainstServerRoutes(t *testing.T) {
	sites := parseSDKRequestCallSites(t, readRepoFile(t, filepath.Join(repoRootForTest(t), sdkCoreAPIPath)))

	// 解析器自检：调用点数量下限。解析器一旦失效（例如 TS 语法演进导致
	// 扫描不到 this.request），守卫会静默空转 —— 用下限把它变成硬失败。
	if len(sites) < sdkMinRequestSites {
		t.Fatalf("%s 仅解析出 %d 个 this.request 调用点（下限 %d）：解析器已失效，守卫会静默放行", sdkCoreAPIPath, len(sites), sdkMinRequestSites)
	}

	routes := assembledRouteTable(t)
	drifted := make([]string, 0, 8)
	for _, site := range sites {
		if _, ok := routes[site.Method+" "+site.Path]; !ok {
			drifted = append(drifted, fmt.Sprintf("%s:%d  %s %s", sdkCoreAPIPath, site.Line, site.Method, site.Path))
		}
	}
	if len(drifted) > 0 {
		sort.Strings(drifted)
		t.Errorf("SDK REST 端点与服务端路由面漂移 %d 处：\n  %s\n\n"+
			"修法二选一：以服务端当前路由为准改 SDK（无对应面就改 unsupported 桩，\n"+
			"同 createSession/getCallStatus 先例），或服务端补注册该路由。勿改本断言。",
			len(drifted), strings.Join(drifted, "\n  "))
	}
}

// TestSDKBackendPathLiteralsStayAccountedFor 守住守卫本身的覆盖面：
// SDK 里持有后端路径字面量的文件必须与守卫实际解析的文件一致。
// 将来某处新增 transport/或 api-client 硬编码路径却不被解析，漂移会重新潜伏。
func TestSDKBackendPathLiteralsStayAccountedFor(t *testing.T) {
	root := repoRootForTest(t)
	packages, err := filepath.Glob(filepath.Join(root, "sdk/packages/*/src"))
	if err != nil {
		t.Fatalf("glob sdk packages: %v", err)
	}

	got := make([]string, 0, 4)
	for _, pkgSrc := range packages {
		_ = filepath.Walk(pkgSrc, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() || !strings.HasSuffix(path, ".ts") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}
			if strings.HasSuffix(rel, ".test.ts") || strings.Contains(rel, string(filepath.Separator)+"contracts"+string(filepath.Separator)) {
				return nil
			}
			if fileDeclaresBackendPath(t, path) {
				got = append(got, filepath.ToSlash(rel))
			}
			return nil
		})
	}
	sort.Strings(got)

	// api.ts 走 this.request 解析；sdk.ts 走 TestSDKWebSocketEndpointRegistered
	// 的字面量出处断言（WS 建连不经过 this.request）。
	want := []string{sdkCoreAPIPath, sdkCoreSDKPath}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("SDK 持有后端路径字面量的文件集合变了：\n  实际: %v\n  期望: %v\n"+
			"新增文件未纳入漂移守卫会重新潜伏，请把它接进本守卫或移到契约层。", got, want)
	}
}

// TestSDKWebSocketEndpointRegistered 覆盖 ApiClient 之外硬编码的建连路径：
// sdk.ts 在 config.wsUrl 缺省时拼 '/api/v1/ws'，不经 this.request，
// 上面的 REST 守卫扫不到。
func TestSDKWebSocketEndpointRegistered(t *testing.T) {
	src := readRepoFile(t, filepath.Join(repoRootForTest(t), sdkCoreSDKPath))
	if !strings.Contains(src, "'"+sdkWebSocketPath+"'") {
		t.Fatalf("%s 不再硬编码 %q：WS 建连路径已改由配置注入，请删除或改指本守卫的新出处", sdkCoreSDKPath, sdkWebSocketPath)
	}
	if _, ok := assembledRouteTable(t)["GET "+sdkWebSocketPath]; !ok {
		t.Errorf("SDK WS 建连路径 %s 未在服务端注册", sdkWebSocketPath)
	}
}

// fileDeclaresBackendPath 报告文件是否在代码里持有后端路径字面量。
// 先剥注释：voice.ts 一类文件只在文档注释里举例端点（真实端点由宿主注入），
// 那不是硬依赖；voice 通道本身由 voice_route_wiring_test.go 单独钉住
// （装配门控两形态 + public surface 目录均有断言）。
// 已知边界：正则字面量未特判（本仓库无含 /api 的正则字面量）。
func fileDeclaresBackendPath(t *testing.T, path string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	src := stripTSComments(string(data))
	for _, prefix := range []string{"'/api", `"/api`, "`/api", "'/public", `"/public`} {
		if strings.Contains(src, prefix) {
			return true
		}
	}
	return false
}

// stripTSComments 去掉 // 与 /* */ 注释，保留字符串字面量内容。
func stripTSComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case strings.HasPrefix(src[i:], "/*"):
			i += 2
			for i < len(src) && !strings.HasPrefix(src[i:], "*/") {
				if src[i] == '\n' {
					b.WriteByte('\n')
				}
				i++
			}
			i += 2
		case src[i] == '\'' || src[i] == '"' || src[i] == '`':
			quote := src[i]
			b.WriteByte(src[i])
			i++
			for i < len(src) {
				if src[i] == '\\' && i+1 < len(src) {
					b.WriteString(src[i : i+2])
					i += 2
					continue
				}
				b.WriteByte(src[i])
				if src[i] == quote {
					i++
					break
				}
				i++
			}
		default:
			b.WriteByte(src[i])
			i++
		}
	}
	return b.String()
}

// assembledRouteTable 返回全装配后的运行时路由面（method + 归一化 path）。
func assembledRouteTable(t *testing.T) map[string]struct{} {
	t.Helper()
	cfg := newRuntimeTestConfig(t)
	rt, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer func() { _ = rt.Stop(context.Background()) }()

	table := make(map[string]struct{}, 256)
	for _, route := range BuildRouter(rt.RouterDependencies()).Routes() {
		table[route.Method+" "+canonicalRoutePath(route.Path)] = struct{}{}
	}
	return table
}

// parseSDKRequestCallSites 抽出 api.ts 的 this.request(...) 调用点。
// 自写最小扫描器而非正则：路径是模板字面量且允许嵌套反引号
// （`/public/suggestions/initial${query ? `?${query}` : ”}`），
// 正则会在嵌套反引号处截断成半个路径。
func parseSDKRequestCallSites(t *testing.T, src string) []sdkCallSite {
	t.Helper()
	const marker = "this.request"

	sites := make([]sdkCallSite, 0, sdkMinRequestSites)
	for offset := 0; offset < len(src); {
		idx := strings.Index(src[offset:], marker)
		if idx < 0 {
			break
		}
		pos := offset + idx
		method, path, _, ok := parseRequestArgs(src, pos+len(marker))
		if ok {
			sites = append(sites, sdkCallSite{
				Method: strings.ToUpper(method),
				Path:   normalizeSDKPath(path),
				Line:   1 + strings.Count(src[:pos], "\n"),
			})
		}
		offset = pos + len(marker)
	}
	return sites
}

// parseRequestArgs 解析 this.request 之后的实参：可选泛型、'METHOD'、路径字面量。
func parseRequestArgs(src string, i int) (method, path string, next int, ok bool) {
	i = skipSpacesAndTypeArgs(src, i)
	if i >= len(src) || src[i] != '(' {
		return "", "", 0, false
	}
	if method, i, ok = readJSStringLiteral(src, skipSpaces(src, i+1)); !ok {
		return "", "", 0, false
	}
	i = skipSpaces(src, i)
	if i >= len(src) || src[i] != ',' {
		return "", "", 0, false
	}
	if path, i, ok = readJSStringLiteral(src, skipSpaces(src, i+1)); !ok {
		return "", "", 0, false
	}
	return method, path, i, true
}

// skipSpacesAndTypeArgs 跳过空白与 this.request<T> 的泛型实参。
func skipSpacesAndTypeArgs(src string, i int) int {
	i = skipSpaces(src, i)
	if i >= len(src) || src[i] != '<' {
		return i
	}
	depth := 0
	for ; i < len(src); i++ {
		switch src[i] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return skipSpaces(src, i+1)
			}
		}
	}
	return i
}

func skipSpaces(src string, i int) int {
	for i < len(src) {
		switch src[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

func readJSStringLiteral(src string, i int) (string, int, bool) {
	if i >= len(src) {
		return "", 0, false
	}
	switch src[i] {
	case '\'', '"':
		return readQuotedString(src, i)
	case '`':
		return readTemplateLiteral(src, i)
	default:
		return "", 0, false
	}
}

func readQuotedString(src string, i int) (string, int, bool) {
	quote := src[i]
	var b strings.Builder
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			if j+1 < len(src) {
				b.WriteByte(src[j+1])
				j++
			}
		case quote:
			return b.String(), j + 1, true
		default:
			b.WriteByte(src[j])
		}
	}
	return "", 0, false
}

// readTemplateLiteral 读取模板字面量，把 ${...} 折叠成标记：查询型插值
// （SDK 拼的是 ?a=b 而非路径段）落 query 标记，其余落路径参数标记。
func readTemplateLiteral(src string, i int) (string, int, bool) {
	var b strings.Builder
	for j := i + 1; j < len(src); j++ {
		switch {
		case src[j] == '\\' && j+1 < len(src):
			b.WriteByte(src[j])
			b.WriteByte(src[j+1])
			j++
		case src[j] == '`':
			return b.String(), j + 1, true
		case src[j] == '$' && j+1 < len(src) && src[j+1] == '{':
			end := skipBracedInterpolation(src, j+1)
			if end < 0 {
				return "", 0, false
			}
			if interpolationLooksLikeQuery(src[j+1 : end]) {
				b.WriteString(sdkQueryMarker)
			} else {
				b.WriteString(sdkParamMarker)
			}
			j = end - 1
		default:
			b.WriteByte(src[j])
		}
	}
	return "", 0, false
}

// skipBracedInterpolation 跳过 ${...}（含嵌套字符串与嵌套模板），返回 '}' 之后的位置。
func skipBracedInterpolation(src string, i int) int {
	depth := 0
	for j := i; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return j + 1
			}
		case '\'', '"':
			if _, next, ok := readQuotedString(src, j); ok {
				j = next - 1
			}
		case '`':
			if _, next, ok := readTemplateLiteral(src, j); ok {
				j = next - 1
			}
		}
	}
	return -1
}

const (
	sdkParamMarker = "\x00param\x00"
	sdkQueryMarker = "\x00query\x00"
)

// interpolationLooksLikeQuery 判定插值是否拼的是 query 串。
// 判据：表达式里出现 '?'，或变量名含 query/param/search。
func interpolationLooksLikeQuery(expr string) bool {
	if strings.Contains(expr, "?") {
		return true
	}
	lower := strings.ToLower(expr)
	for _, hint := range []string{"query", "param", "search"} {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// normalizeSDKPath 归一化 SDK 路径：先按 query 边界截断，再把模板插值折叠为 :p。
// 插值在解析期已被替换成标记，所以归一化后残留的 '?' 必然来自字面量。
func normalizeSDKPath(raw string) string {
	cut := len(raw)
	if i := strings.IndexByte(raw, '?'); i >= 0 && i < cut {
		cut = i
	}
	if i := strings.Index(raw, sdkQueryMarker); i >= 0 && i < cut {
		cut = i
	}
	path := strings.ReplaceAll(raw[:cut], sdkParamMarker, ":p")
	return canonicalRoutePath(path)
}

// canonicalRoutePath 折叠路径参数名：SDK 与服务端各写各的（${customerId} vs :id），
// 统一成 :p 后按段比较。
func canonicalRoutePath(path string) string {
	var b strings.Builder
	b.Grow(len(path))
	for i := 0; i < len(path); i++ {
		if path[i] != ':' {
			b.WriteByte(path[i])
			continue
		}
		b.WriteString(":p")
		for i+1 < len(path) && (isPathParamByte(path[i+1])) {
			i++
		}
	}
	out := b.String()
	for strings.Contains(out, ":p:p") {
		out = strings.ReplaceAll(out, ":p:p", ":p")
	}
	if len(out) > 1 {
		out = strings.TrimSuffix(out, "/")
	}
	return out
}

func isPathParamByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// repoRootForTest 由本文件位置向上找工作区根，避免依赖测试工作目录。
// 判据不能用"最近的 go.mod"：apps/server 自身是独立 Go 模块，停在模块根
// 就找不到仓库根的 sdk/ 目录。改为要求同时具备 apps/server 与 sdk/packages。
func repoRootForTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) 失败")
	}
	for dir := filepath.Dir(file); ; {
		_, appErr := os.Stat(filepath.Join(dir, "apps", "server"))
		_, sdkErr := os.Stat(filepath.Join(dir, "sdk", "packages"))
		if appErr == nil && sdkErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("未能在 %s 上层找到工作区根（需同时含 apps/server 与 sdk/packages）", filepath.Dir(file))
		}
		dir = parent
	}
}
