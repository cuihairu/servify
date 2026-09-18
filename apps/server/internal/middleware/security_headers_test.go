package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"servify/apps/server/internal/config"

	"github.com/gin-gonic/gin"
)

func headersConfig(enabled bool) config.SecurityHeadersConfig {
	return config.SecurityHeadersConfig{Enabled: enabled}
}

func newHeaderRouter(t *testing.T, cfg *config.Config, maxBytes int64) (*gin.Engine, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeadersMiddleware(cfg))
	r.Use(MaxBodyBytesMiddleware(maxBytes))
	r.POST("/echo", func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil {
			c.String(http.StatusInternalServerError, "read body: %v", err)
			return
		}
		c.String(http.StatusOK, string(body))
	})
	w := httptest.NewRecorder()
	return r, w
}

// TestSecurityHeadersDisabledKeepsLegacyBehavior：默认关闭时是 no-op，
// 保持既有部署的响应头面不变。
func TestSecurityHeadersDisabledKeepsLegacyBehavior(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.Headers = headersConfig(false)
	r, w := newHeaderRouter(t, cfg, 0)
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString("x")))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	for _, h := range []string{
		"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy",
		"Content-Security-Policy", "Strict-Transport-Security",
	} {
		if got := w.Header().Get(h); got != "" {
			t.Fatalf("%s unexpectedly set to %q when disabled", h, got)
		}
	}
}

// TestSecurityHeadersNilConfigIsNoop：cfg 为 nil 时中间件不得panic，
// 语义等同未启用（registerBaseMiddleware 允许 cfg=nil）。
func TestSecurityHeadersNilConfigIsNoop(t *testing.T) {
	r, w := newHeaderRouter(t, nil, 0)
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString("x")))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("X-Frame-Options = %q for nil config", got)
	}
}

// TestSecurityHeadersEnabledDefaults：开启后未细配的字段取安全默认值，
// CSP/HSTS 不注入（HSTS 默认关：TLS 通常由反向代理终结）。
func TestSecurityHeadersEnabledDefaults(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.Headers = headersConfig(true)
	r, w := newHeaderRouter(t, cfg, 0)
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString("x")))
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff = %q", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("default frame-options = %q", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("default referrer-policy = %q", got)
	}
	if got := w.Header().Values("Content-Security-Policy"); len(got) != 0 {
		t.Fatalf("CSP unexpectedly set: %q", got)
	}
	if got := w.Header().Values("Strict-Transport-Security"); len(got) != 0 {
		t.Fatalf("HSTS unexpectedly set: %q", got)
	}
}

// TestSecurityHeadersEnabledCustom：显式配置逐项生效，含 CSP 与 HSTS max-age。
func TestSecurityHeadersEnabledCustom(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.Headers = config.SecurityHeadersConfig{
		Enabled:               true,
		HSTSEnabled:           true,
		HSTSMaxAge:            600,
		FrameOptions:          "SAMEORIGIN",
		ReferrerPolicy:        "no-referrer",
		ContentSecurityPolicy: "default-src 'self'",
	}
	r, w := newHeaderRouter(t, cfg, 0)
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString("x")))
	if got := w.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("frame-options = %q", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("referrer-policy = %q", got)
	}
	if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Fatalf("csp = %q", got)
	}
	if got := w.Header().Get("Strict-Transport-Security"); got != "max-age=600" {
		t.Fatalf("hsts = %q", got)
	}
}

// TestSecurityHeadersHSTSMaxAgeFallback：开启 HSTS 但 max-age 非法（<=0）
// 时回退一年，避免发出 max-age=0 的无效策略。
func TestSecurityHeadersHSTSMaxAgeFallback(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.Headers = config.SecurityHeadersConfig{Enabled: true, HSTSEnabled: true, HSTSMaxAge: -5}
	r, w := newHeaderRouter(t, cfg, 0)
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString("x")))
	if got := w.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("hsts fallback = %q", got)
	}
}

// TestMaxBodyBytesDisabled：<=0 视为未配置，不包裹 body、不拒绝请求。
func TestMaxBodyBytesDisabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.Headers = headersConfig(false)
	r, w := newHeaderRouter(t, cfg, 0)
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString("payload")))
	if w.Code != http.StatusOK || w.Body.String() != "payload" {
		t.Fatalf("no-op limit: status=%d body=%q", w.Code, w.Body.String())
	}
}

// TestMaxBodyBytesContentLengthExceeded：Content-Length 超限直接 413，
// handler 不执行。
func TestMaxBodyBytesContentLengthExceeded(t *testing.T) {
	r, w := newHeaderRouter(t, nil, 16)
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString("0123456789abcdef0123456789abcdef"))
	req.ContentLength = int64(req.ContentLength)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", w.Code)
	}
	if w.Body.String() == "" {
		t.Fatal("expected JSON error body on 413")
	}
}

// TestMaxBodyBytesWithinLimitWrapsBody：未超限时请求放行，body 经
// http.MaxBytesReader 包裹兜底分块传输的超限读取。
func TestMaxBodyBytesWithinLimitWrapsBody(t *testing.T) {
	r, w := newHeaderRouter(t, nil, 64)
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString("small"))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "small" {
		t.Fatalf("within limit: status=%d body=%q", w.Code, w.Body.String())
	}
}

// TestMaxBodyBytesChunkedOverLimitFailsOnRead：无 Content-Length 的分块
// 请求超限时，MaxBytesReader 在 handler 读取 body 时失败（兜底路径）。
func TestMaxBodyBytesChunkedOverLimitFailsOnRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MaxBodyBytesMiddleware(8))
	r.POST("/echo", func(c *gin.Context) {
		if _, err := c.GetRawData(); err == nil {
			c.String(http.StatusOK, "read-should-have-failed")
			return
		}
		c.String(http.StatusRequestEntityTooLarge, "limited")
	})
	// 构造 Transfer-Encoding: chunked 且声明长度小于实际写入量。
	body := "A0123456789"
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString(body))
	req.ContentLength = -1
	req.Header.Set("Transfer-Encoding", "chunked")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge || w.Body.String() != "limited" {
		t.Fatalf("chunked over-limit: status=%d body=%q", w.Code, w.Body.String())
	}
}
