package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"servify/apps/server/internal/config"

	"github.com/gin-gonic/gin"
)

// newP25BaseRouter 构造仅挂基础中间件的最小路由，供 CORS/安全头用例复用。
func newP25BaseRouter(t *testing.T, cfg *config.Config) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerBaseMiddleware(r, cfg, nil)
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	return r
}

func p25Config() *config.Config {
	cfg := testRouterConfig()
	cfg.Security.CORS.Enabled = true
	cfg.Security.CORS.AllowedOrigins = []string{"https://admin.example.com", "https://console.example.com"}
	return cfg
}

// TestRegisterBaseMiddlewareCORSPerRequestOriginEcho：多 origin 白名单按
// 请求回显命中的 Origin（此前 strings.Join 多值 ACAO 属非法头，浏览器拒绝）。
func TestRegisterBaseMiddlewareCORSPerRequestOriginEcho(t *testing.T) {
	r := newP25BaseRouter(t, p25Config())
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://console.example.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://console.example.com" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("vary = %q", got)
	}
}

// TestRegisterBaseMiddlewareCORSPerRequestPrefillgetsVaryWithoutEcho：预检
// 请求（OPTIONS 无 Origin 头）不命中白名单，不带 ACAO，但仍须 204 且带
// Vary: Origin。
func TestRegisterBaseMiddlewareCORSPerRequestPreflightWithoutOrigin(t *testing.T) {
	r := newP25BaseRouter(t, p25Config())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/ping", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("preflight without origin must not carry ACAO, got %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("vary = %q", got)
	}
}

// TestRegisterBaseMiddlewareCORSOriginNotAllowlisted：白名单外的 Origin
// 不带 ACAO（由浏览器侧拦截），响应本身仍正常返回。
func TestRegisterBaseMiddlewareCORSOriginNotAllowlisted(t *testing.T) {
	r := newP25BaseRouter(t, p25Config())
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://evil.example.net")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "pong" {
		t.Fatalf("non-allowlisted origin must still serve, got %d %q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("non-allowlisted origin must not get ACAO, got %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("vary = %q", got)
	}
}

// TestBodyLimitBytesNormalizesNil：nil 配置归一化为 0（中间件 no-op）。
func TestBodyLimitBytesNormalizesNil(t *testing.T) {
	if got := bodyLimitBytes(nil); got != 0 {
		t.Fatalf("bodyLimitBytes(nil) = %d", got)
	}
	cfg := testRouterConfig()
	cfg.Security.MaxBodyBytes = 4096
	if got := bodyLimitBytes(cfg); got != 4096 {
		t.Fatalf("bodyLimitBytes(cfg) = %d", got)
	}
}

// TestUploadsLocalFileHandlerServesOnlyRegularFiles：本地 /uploads 只回
// 具体文件，目录、缺失与根路径一律 404（http.Dir 拒绝 `..` 逃逸）。
func TestUploadsLocalFileHandlerServesOnlyRegularFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "2026", "09"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "2026", "09", "report.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	handler := uploadsLocalFileHandler(root)
	r.GET("/uploads/*filepath", handler)
	r.HEAD("/uploads/*filepath", handler)

	// 具体文件：200 + 内容。
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/uploads/2026/09/report.txt", nil))
	if w.Code != http.StatusOK || w.Body.String() != "hello" {
		t.Fatalf("file: status=%d body=%q", w.Code, w.Body.String())
	}
	// HEAD 同样可用。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/uploads/2026/09/report.txt", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d", w.Code)
	}
	// 目录：404（不列目录）。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/uploads/2026/09", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("directory status = %d, want 404", w.Code)
	}
	// 缺失文件：404。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/uploads/2026/09/missing.txt", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing file status = %d, want 404", w.Code)
	}
	// 存储根路径：404。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/uploads/", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("root status = %d, want 404", w.Code)
	}
	// `..` 逃逸：http.Dir 拒绝，404。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/uploads/../../etc/passwd", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("traversal status = %d, want 404", w.Code)
	}
}
