package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	svrmetrics "servify/apps/server/internal/metrics"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"

	"github.com/gin-gonic/gin"
	_ "github.com/glebarez/sqlite"
)

func cxcPerform(r *gin.Engine, method, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, body)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	r.ServeHTTP(w, req)
	return w
}

func cxcJSONBody(v interface{}) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

func TestCxcAIHandlerProcessQueryServiceError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewAIHandler(&unitAIService{uploadErr: errors.New("query failed")})
	r := gin.New()
	r.POST("/ai/query", h.ProcessQuery)

	w := cxcPerform(r, http.MethodPost, "/ai/query", cxcJSONBody(map[string]string{"query": "q"}), "application/json")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "AI processing failed") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestCxcAIHandlerKnowledgeProviderToggle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enabled := &unitAIService{enableOK: true}
	h := NewAIHandler(enabled)
	r := gin.New()
	r.POST("/ai/knowledge-provider/enable", h.EnableKnowledgeProvider)
	r.POST("/ai/knowledge-provider/disable", h.DisableKnowledgeProvider)

	if w := cxcPerform(r, http.MethodPost, "/ai/knowledge-provider/enable", nil, ""); w.Code != http.StatusOK {
		t.Fatalf("enable expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if w := cxcPerform(r, http.MethodPost, "/ai/knowledge-provider/disable", nil, ""); w.Code != http.StatusOK {
		t.Fatalf("disable expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	disabled := &unitAIService{enableOK: false}
	h2 := NewAIHandler(disabled)
	r2 := gin.New()
	r2.POST("/ai/knowledge-provider/enable", h2.EnableKnowledgeProvider)
	r2.POST("/ai/knowledge-provider/disable", h2.DisableKnowledgeProvider)
	if w := cxcPerform(r2, http.MethodPost, "/ai/knowledge-provider/enable", nil, ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("enable expected 503, got %d", w.Code)
	}
	if w := cxcPerform(r2, http.MethodPost, "/ai/knowledge-provider/disable", nil, ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("disable expected 503, got %d", w.Code)
	}
}

type cxcDBStats struct {
	stats sql.DBStats
	ok    bool
}

func (s *cxcDBStats) Stats() (sql.DBStats, bool) { return s.stats, s.ok }

func TestCxcMetricsHandlerGetMetricsVariants(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 全局限流计数器跨测试累积，先重置保证 "global 0" 兜底分支的断言确定性
	svrmetrics.ResetRateLimit()

	ai := &unitAIService{
		metrics: &aidelivery.AIMetrics{
			QueryCount:                  7,
			DifyUsageCount:              3,
			WeKnoraUsageCount:           2,
			KnowledgeProviderUsageCount: 5,
			FallbackUsageCount:          1,
			AverageLatency:              250 * 1e6, // 250ms in ns
		},
		metricsOK: true,
	}
	h := NewMetricsHandler(&unitWsGateway{count: 9}, &unitNoopRTCGateway{}, ai, nil)
	r := gin.New()
	r.GET("/metrics", h.GetMetrics)

	w := cxcPerform(r, http.MethodGet, "/metrics", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"servify_info{version=",
		"servify_uptime_seconds",
		"servify_websocket_active_connections 9",
		"servify_webrtc_connections 2",
		"servify_ai_requests_total 7",
		"servify_ai_dify_usage_total 3",
		"servify_ai_weknora_usage_total 2",
		"servify_ai_knowledge_provider_usage_total 5",
		"servify_ai_fallback_usage_total 1",
		"servify_go_goroutines",
		"servify_go_mem_alloc_bytes",
		`servify_ratelimit_dropped_total{prefix="global"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in metrics output:\n%s", want, body)
		}
	}
	if strings.Contains(body, "servify_db_max_open_connections") {
		t.Fatalf("db stats should be absent when dbStats is nil")
	}

	// Non-empty rate limit prefix map.
	svrmetrics.IncRateLimitDrop("cxc-test")
	w2 := cxcPerform(r, http.MethodGet, "/metrics", nil, "")
	if !strings.Contains(w2.Body.String(), `servify_ratelimit_dropped_total{prefix="cxc-test"}`) {
		t.Fatalf("expected prefixed rate limit counter in output:\n%s", w2.Body.String())
	}

	// With DB stats available.
	h2 := NewMetricsHandler(nil, nil, ai, &cxcDBStats{stats: sql.DBStats{MaxOpenConnections: 4, OpenConnections: 2, InUse: 1, Idle: 1, WaitCount: 3, WaitDuration: 5e6, MaxIdleClosed: 6, MaxLifetimeClosed: 7}, ok: true})
	r2 := gin.New()
	r2.GET("/metrics", h2.GetMetrics)
	w3 := cxcPerform(r2, http.MethodGet, "/metrics", nil, "")
	for _, want := range []string{
		"servify_db_max_open_connections 4",
		"servify_db_open_connections 2",
		"servify_db_inuse_connections 1",
		"servify_db_idle_connections 1",
		"servify_db_wait_count 3",
		"servify_db_wait_duration_seconds 0.005000",
		"servify_db_max_idle_closed_total 6",
		"servify_db_max_lifetime_closed_total 7",
	} {
		if !strings.Contains(w3.Body.String(), want) {
			t.Fatalf("missing %q in metrics output:\n%s", want, w3.Body.String())
		}
	}

	// DB stats provider present but reporting not-available, AI metrics unavailable.
	h3 := NewMetricsHandler(nil, nil, &unitAIService{}, &cxcDBStats{ok: false})
	r3 := gin.New()
	r3.GET("/metrics", h3.GetMetrics)
	w4 := cxcPerform(r3, http.MethodGet, "/metrics", nil, "")
	if strings.Contains(w4.Body.String(), "servify_db_max_open_connections") {
		t.Fatalf("db stats should be absent when Stats() returns false")
	}
	if !strings.Contains(w4.Body.String(), "servify_ai_requests_total 0") {
		t.Fatalf("expected zero ai counters:\n%s", w4.Body.String())
	}
}

func TestCxcAICapabilityStatusCode(t *testing.T) {
	standard := aidelivery.NewHandlerServiceAdapter(aidelivery.NewAIService("", ""))
	unsupportedErr := standard.SyncKnowledgeBase(context.Background())

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, http.StatusOK},
		{"unsupported enhanced feature", unsupportedErr, http.StatusServiceUnavailable},
		{"not enabled", errors.New("knowledge provider is not enabled"), http.StatusServiceUnavailable},
		{"not initialized", errors.New("provider not initialized"), http.StatusServiceUnavailable},
		{"unavailable", errors.New("service unavailable"), http.StatusServiceUnavailable},
		{"deadline", context.DeadlineExceeded, http.StatusGatewayTimeout},
		{"canceled", context.Canceled, http.StatusGatewayTimeout},
		{"wrapped deadline", fmt.Errorf("wrap: %w", context.DeadlineExceeded), http.StatusGatewayTimeout},
		{"generic", errors.New("boom"), http.StatusBadGateway},
	}
	for _, tc := range cases {
		if got := aiCapabilityStatusCode(tc.err); got != tc.want {
			t.Fatalf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}

func TestCxcSQLiteDriverRegistered(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping sqlite: %v", err)
	}
}
