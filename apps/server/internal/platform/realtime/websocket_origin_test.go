package realtime

import (
	"net/http"
	"testing"
)

func originRequest(origin string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "https://server.example.com/api/v1/ws", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

// TestWebsocketCheckOriginEmptyAllowlistAdmitsAll：未配置白名单（默认部署）
// 保持放行所有来源——原生 WebView 等匿名访客来源不可枚举。
func TestWebsocketCheckOriginEmptyAllowlistAdmitsAll(t *testing.T) {
	t.Cleanup(func() { SetWebsocketAllowedOrigins(nil) })
	SetWebsocketAllowedOrigins(nil)
	if !upgrader.CheckOrigin(originRequest("https://any.example.net")) {
		t.Fatal("empty allowlist must admit any origin")
	}
}

// TestWebsocketCheckOriginBlankOriginAdmitted：带白名单时，无 Origin 头的
// 非浏览器客户端放行（无从校验，由 token/租户校验兜底）。
func TestWebsocketCheckOriginBlankOriginAdmitted(t *testing.T) {
	t.Cleanup(func() { SetWebsocketAllowedOrigins(nil) })
	SetWebsocketAllowedOrigins([]string{"https://admin.example.com"})
	if !upgrader.CheckOrigin(originRequest("")) {
		t.Fatal("blank origin (non-browser client) must be admitted")
	}
}

// TestWebsocketCheckOriginAllowlistEnforced：带白名单时，命中放行、
// 白名单外的浏览器 Origin 一律拒绝。
func TestWebsocketCheckOriginAllowlistEnforced(t *testing.T) {
	t.Cleanup(func() { SetWebsocketAllowedOrigins(nil) })
	SetWebsocketAllowedOrigins([]string{"https://admin.example.com", "https://console.example.com"})
	if !upgrader.CheckOrigin(originRequest("https://console.example.com")) {
		t.Fatal("allowlisted origin must be admitted")
	}
	if upgrader.CheckOrigin(originRequest("https://evil.example.net")) {
		t.Fatal("non-allowlisted origin must be rejected")
	}
	// 大小写与 scheme 必须精确匹配，防止借大小写差异绕过。
	if upgrader.CheckOrigin(originRequest("HTTPS://ADMIN.EXAMPLE.COM")) {
		t.Fatal("origin match must be exact")
	}
}
