package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	svcmetrics "servify/apps/server/internal/observability/metrics"

	"github.com/gin-gonic/gin"
)

func postIngest(router http.Handler, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/metrics/ingest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestBuildRouter_ClientMetricsBridgeIntoPrometheus(t *testing.T) {
	// 接线把 collector 注册到进程级 DefaultRegistry；换入新 registry，
	// 让 go test -count=2 等重复构建不会触发重复注册 panic。
	previousRegistry := svcmetrics.DefaultRegistry
	svcmetrics.DefaultRegistry = svcmetrics.NewRegistry()
	t.Cleanup(func() { svcmetrics.DefaultRegistry = previousRegistry })

	cfg := testRouterConfig()
	cfg.Monitoring.Enabled = true
	router := BuildRouter(Dependencies{
		Config:      cfg,
		HTTPMetrics: svcmetrics.NewHTTPMetrics(svcmetrics.DefaultRegistry),
	})

	token := createTestHS256JWT(t, map[string]interface{}{
		"token_type": "service",
	}, "test-secret")
	body := `{"source":"sdk","tenant":"t1","metrics":[
		{"name":"sdk_messages_sent_total","value":2,"labels":{"type":"text"}},
		{"name":"agent_online_gauge","value":-1}
	]}`
	if w := postIngest(router, token, body); w.Code != http.StatusOK {
		t.Fatalf("ingest status = %d body=%s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, cfg.Monitoring.MetricsPath, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d body=%s", cfg.Monitoring.MetricsPath, w.Code, w.Body.String())
	}
	text := w.Body.String()
	for _, want := range []string{
		`sdk_messages_sent_total{source="sdk",tenant="t1",type="text"} 2`,
		`agent_online_gauge{source="sdk",tenant="t1"} -1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("metrics output missing %q; got:\n%s", want, text)
		}
	}
}

func TestRegisterRealtimeRoutes_MetricsBridgeSkippedWithoutHTTPMetrics(t *testing.T) {
	// HTTPMetrics 为 nil（降级 JSON handler 场景）时不注册桥接 collector：
	// 直接装配 realtime 路由验证该分支不触发注册、ingest 路由照常挂载。
	previousRegistry := svcmetrics.DefaultRegistry
	svcmetrics.DefaultRegistry = svcmetrics.NewRegistry()
	t.Cleanup(func() { svcmetrics.DefaultRegistry = previousRegistry })

	cfg := testRouterConfig()
	cfg.Monitoring.Enabled = true
	r := gin.New()
	registerRealtimeRoutes(r, Dependencies{Config: cfg})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/metrics/ingest", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 from auth middleware, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildRouter_MetricsBridgeSkippedWhenMonitoringDisabled(t *testing.T) {
	// Monitoring 关闭时 health 路由不挂 /metrics（404），桥接同样跳过。
	// GetDefaultConfig 的 Monitoring.Enabled 默认 true，这里显式关闭。
	cfg := testRouterConfig()
	cfg.Monitoring.Enabled = false
	router := BuildRouter(Dependencies{Config: cfg})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 with monitoring disabled, got %d body=%s", w.Code, w.Body.String())
	}
}
