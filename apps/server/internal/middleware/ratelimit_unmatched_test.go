package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"servify/apps/server/internal/config"

	"github.com/gin-gonic/gin"
)

// TestRateLimitMiddlewareFromConfig_UnmatchedPathFallsBackToURLPath 覆盖
// c.FullPath() 为空（未注册路由，gin 仍执行全局中间件）时回退到 URL.Path 的分支。
func TestRateLimitMiddlewareFromConfig_UnmatchedPathFallsBackToURLPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Security: config.SecurityConfig{
			RateLimiting: config.RateLimitingConfig{
				Enabled:           true,
				RequestsPerMinute: 6000,
				Burst:             100,
				Paths: []config.PathRateLimitConfig{
					{
						Enabled:           true,
						Prefix:            "/api/",
						RequestsPerMinute: 3000,
						Burst:             50,
					},
				},
			},
		},
	}

	router := gin.New()
	router.Use(RateLimitMiddlewareFromConfig(cfg))
	router.GET("/api/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	// 未注册路径：FullPath() == ""，必须回退到 URL.Path 再做前缀匹配
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/nowhere", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unregistered path, got %d", w.Code)
	}

	// 已注册路径不受影响（正常 200）
	ok := httptest.NewRecorder()
	router.ServeHTTP(ok, httptest.NewRequest(http.MethodGet, "/api/ping", nil))
	if ok.Code != http.StatusOK {
		t.Fatalf("expected 200 for registered path, got %d", ok.Code)
	}
}
