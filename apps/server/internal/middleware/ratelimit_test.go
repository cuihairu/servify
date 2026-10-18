package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"servify/apps/server/internal/config"
)

func TestTokenBucket_Allow(t *testing.T) {
	b := newBucket(60, 10) // 60 req/min, burst 10

	// 应该允许 burst 个请求
	for i := 0; i < 10; i++ {
		if !b.allow() {
			t.Errorf("request %d should be allowed", i)
		}
	}

	// 下一个请求应该被拒绝
	if b.allow() {
		t.Error("request beyond burst should be denied")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	b := newBucket(600, 10) // 600 req/min = 10 req/sec

	// 消耗所有 tokens
	for i := 0; i < 10; i++ {
		b.allow()
	}

	// 应该被拒绝
	if b.allow() {
		t.Error("should be denied after exhausting tokens")
	}

	// 等待令牌补充
	time.Sleep(150 * time.Millisecond)

	// 现在应该允许一个请求
	if !b.allow() {
		t.Error("should allow after refill")
	}
}

func TestTokenBucket_ZeroParams(t *testing.T) {
	b := newBucket(0, 0) // 应该使用默认值

	// 应该有默认的 burst
	allowed := 0
	for i := 0; i < 100; i++ {
		if b.allow() {
			allowed++
		}
	}

	if allowed == 0 {
		t.Error("expected at least some requests to be allowed")
	}
}

func TestRateLimitMiddlewareFromConfig_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Security: config.SecurityConfig{
			RateLimiting: config.RateLimitingConfig{
				Enabled: false,
			},
		},
	}

	middleware := RateLimitMiddlewareFromConfig(cfg)
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// 应该允许所有请求
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected status 200, got %d", i, w.Code)
		}
	}
}

func TestRateLimitMiddlewareFromConfig_WhitelistIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Security: config.SecurityConfig{
			RateLimiting: config.RateLimitingConfig{
				Enabled:           true,
				RequestsPerMinute: 10,
				Burst:             2,
				WhitelistIPs:      []string{"127.0.0.1"},
			},
		},
	}

	middleware := RateLimitMiddlewareFromConfig(cfg)
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// 白名单 IP 应该允许所有请求
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected status 200 (whitelisted), got %d", i, w.Code)
		}
	}
}

func TestRateLimitMiddlewareFromConfig_PathBased(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Security: config.SecurityConfig{
			RateLimiting: config.RateLimitingConfig{
				Enabled:           true,
				RequestsPerMinute: 100,
				Burst:             50,
				Paths: []config.PathRateLimitConfig{
					{
						Enabled:           true,
						Prefix:            "/api/",
						RequestsPerMinute: 5,
						Burst:             2,
					},
				},
			},
		},
	}

	middleware := RateLimitMiddlewareFromConfig(cfg)
	router := gin.New()
	router.Use(middleware)
	router.GET("/api/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/other", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// /api/ 路径应该受到严格限制
	allowed := 0
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/test", nil)
		router.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			allowed++
		}
	}
	if allowed > 3 {
		t.Errorf("/api/ allowed %d requests, expected at most 3", allowed)
	}

	// /other 路径应该使用全局限制（更宽松）
	allowed = 0
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/other", nil)
		router.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			allowed++
		}
	}
	if allowed < 5 {
		t.Errorf("/other allowed %d requests, expected at least 5", allowed)
	}
}

func TestRateLimitMiddlewareFromConfig_WhitelistUsesSocketPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Security: config.SecurityConfig{
			RateLimiting: config.RateLimitingConfig{
				Enabled:           true,
				RequestsPerMinute: 1,
				Burst:             1,
				WhitelistIPs:      []string{"192.0.2.10"},
			},
		},
	}
	mw := RateLimitMiddlewareFromConfig(cfg)
	router := gin.New()
	router.Use(mw)
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	// A header claiming to be a trusted IP cannot bypass rate limiting.
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "198.51.100.11:1234"
		req.Header.Set("X-Forwarded-For", "192.0.2.10")
		router.ServeHTTP(w, req)
		want := http.StatusOK
		if i > 0 {
			want = http.StatusTooManyRequests
		}
		if w.Code != want {
			t.Fatalf("spoofed whitelist header request %d = %d, want %d", i, w.Code, want)
		}
	}

	// Whitelisting is based on the actual socket peer.
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req1.RemoteAddr = "192.0.2.10:1234"
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("whitelisted peer request = %d", w1.Code)
	}
}

func TestRateLimitMiddlewareFromConfig_RequestHeadersCannotRotateBucket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Security: config.SecurityConfig{
			RateLimiting: config.RateLimitingConfig{
				Enabled:           true,
				RequestsPerMinute: 1,
				Burst:             1,
			},
		},
	}
	router := gin.New()
	router.Use(RateLimitMiddlewareFromConfig(cfg))
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req1.RemoteAddr = "203.0.113.9:1234"
	req1.Header.Set("X-API-Key", "key-a")
	req1.Header.Set("X-Forwarded-For", "203.0.113.9")
	router.ServeHTTP(w1, req1)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "203.0.113.9:1234"
	req2.Header.Set("X-API-Key", "key-b")
	req2.Header.Set("X-Forwarded-For", "198.51.100.8")
	router.ServeHTTP(w2, req2)
	if w1.Code != http.StatusOK || w2.Code != http.StatusTooManyRequests {
		t.Fatalf("key-a requests = %d then %d", w1.Code, w2.Code)
	}
}

func TestRateLimiterBoundsAndReclaimsBuckets(t *testing.T) {
	limiter := newRateLimiter(config.PathRateLimitConfig{RequestsPerMinute: 60, Burst: 1})
	for i := 0; i < rateLimitMaxBuckets; i++ {
		limiter.get(fmt.Sprintf("peer-%d", i))
	}
	if got := len(limiter.buckets); got != rateLimitMaxBuckets {
		t.Fatalf("bucket count = %d, want cap %d", got, rateLimitMaxBuckets)
	}
	overflowA := limiter.get("overflow-a")
	overflowB := limiter.get("overflow-b")
	if overflowA != overflowB || limiter.overflow == nil {
		t.Fatal("new peers at capacity must share the overflow bucket")
	}
	if got := len(limiter.buckets); got != rateLimitMaxBuckets {
		t.Fatalf("bucket count after overflow = %d, want cap %d", got, rateLimitMaxBuckets)
	}

	// Force an expiry sweep and ensure old per-peer buckets are reclaimed.
	for _, bucket := range limiter.buckets {
		bucket.mu.Lock()
		bucket.lastSeen = time.Now().Add(-rateLimitIdleTTL - time.Second)
		bucket.mu.Unlock()
	}
	limiter.nextCleanup = time.Now().Add(-time.Second)
	limiter.get("fresh-peer")
	if got := len(limiter.buckets); got != 1 {
		t.Fatalf("bucket count after idle sweep = %d, want 1", got)
	}
}

func TestRateLimitMiddlewareFromConfig_SkipsDisabledPathsAndFallsBackToGlobal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Security: config.SecurityConfig{
			RateLimiting: config.RateLimitingConfig{
				Enabled:           true,
				RequestsPerMinute: 2,
				Burst:             1,
				Paths: []config.PathRateLimitConfig{
					{Enabled: false, Prefix: "/disabled", RequestsPerMinute: 5},
					{Enabled: true, Prefix: "/other", RequestsPerMinute: 0},
				},
			},
		},
	}
	router := gin.New()
	router.Use(RateLimitMiddlewareFromConfig(cfg))
	router.GET("/disabled", func(c *gin.Context) { c.Status(http.StatusOK) })

	// disabled path entries skipped, global limit applies (rpm 2, burst 1 → second request rejected)
	codes := []int{}
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/disabled", nil)
		req.RemoteAddr = "203.0.113.99:7777"
		router.ServeHTTP(w, req)
		codes = append(codes, w.Code)
	}
	if codes[0] != http.StatusOK {
		t.Fatalf("first request = %v", codes)
	}
	if codes[1] != http.StatusTooManyRequests || codes[2] != http.StatusTooManyRequests {
		t.Fatalf("expected subsequent requests limited, got %v", codes)
	}
}

func TestRateLimitMiddlewareFromConfig_NoLimitersConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Security: config.SecurityConfig{
			RateLimiting: config.RateLimitingConfig{
				Enabled: true,
				Paths: []config.PathRateLimitConfig{
					{Enabled: true, Prefix: "/api", RequestsPerMinute: 3},
				},
			},
		},
	}
	router := gin.New()
	router.Use(RateLimitMiddlewareFromConfig(cfg))
	router.GET("/api/thing", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/elsewhere", func(c *gin.Context) { c.Status(http.StatusOK) })

	// unmatched path has no global limiter → always allowed
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/elsewhere", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("unmatched path request %d = %d", i, w.Code)
		}
	}

	// matched path is limited (rpm 3, burst defaults to 3 → fourth request rejected)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/api/thing", nil))
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/thing", nil))
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/api/thing", nil))
	w4 := httptest.NewRecorder()
	router.ServeHTTP(w4, httptest.NewRequest(http.MethodGet, "/api/thing", nil))
	if w1.Code != http.StatusOK || w2.Code != http.StatusOK || w3.Code != http.StatusOK {
		t.Fatalf("path requests = %d %d %d", w1.Code, w2.Code, w3.Code)
	}
	if w4.Code != http.StatusTooManyRequests {
		t.Fatalf("expected limited request, got %d", w4.Code)
	}
}
