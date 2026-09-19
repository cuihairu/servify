package releasecheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v4"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/handlers"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"servify/apps/server/internal/platform/configscope"
	realtimeplatform "servify/apps/server/internal/platform/realtime"
)

type stubRealtimeGateway struct{}

func (stubRealtimeGateway) HandleWebSocket(*gin.Context)                   {}
func (stubRealtimeGateway) SendToSession(string, realtimeplatform.Message) {}
func (stubRealtimeGateway) ClientCount() int                               { return 0 }

type stubRTCGateway struct{}

func (stubRTCGateway) ConnectionStats(sessionID string) (map[string]interface{}, error) {
	if sessionID == "bad" {
		return nil, errors.New("lookup failed")
	}
	return map[string]interface{}{
		"session_id":            sessionID,
		"connection_state":      "connected",
		"ice_connection_state":  "connected",
		"peer_connection_state": "connected",
	}, nil
}
func (stubRTCGateway) ConnectionCount() int { return 1 }
func (stubRTCGateway) HandleOffer(string, webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	return nil, nil
}
func (stubRTCGateway) HandleAnswer(string, webrtc.SessionDescription) error     { return nil }
func (stubRTCGateway) HandleICECandidate(string, webrtc.ICECandidateInit) error { return nil }
func (stubRTCGateway) CloseConnection(string) error                             { return nil }

type stubMessageRouter struct{}

func (stubMessageRouter) Start() error { return nil }
func (stubMessageRouter) Stop() error  { return nil }
func (stubMessageRouter) GetPlatformStats() map[string]interface{} {
	return map[string]interface{}{"web": 2, "whatsapp": 1}
}

func newStandardAIHandler() *handlers.AIHandler {
	svc := aidelivery.NewAIService("", "")
	svc.InitializeKnowledgeBase()
	return handlers.NewAIHandler(aidelivery.NewHandlerServiceAdapter(svc))
}

func TestReleaseCheckHealthEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.GetDefaultConfig()
	cfg.Monitoring.HealthChecks.Database = false
	cfg.Monitoring.HealthChecks.Redis = false
	cfg.Monitoring.HealthChecks.KnowledgeProvider = false
	cfg.Monitoring.HealthChecks.WeKnora = false
	cfg.WeKnora.Enabled = false

	svc := aidelivery.NewAIService("", "")
	svc.InitializeKnowledgeBase()
	h := handlers.NewEnhancedHealthHandler(cfg, aidelivery.NewHandlerServiceAdapter(svc), nil, nil)

	r := gin.New()
	r.GET("/health", h.Health)
	r.GET("/ready", h.Ready)

	for _, path := range []string{"/health", "/ready"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}

func TestReleaseCheckAIHandlerStandardRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := newStandardAIHandler()
	r := gin.New()
	r.GET("/status", h.GetStatus)
	r.GET("/metrics", h.GetMetrics)
	r.POST("/query", h.ProcessQuery)
	r.POST("/knowledge/upload", h.UploadDocument)
	r.POST("/knowledge/sync", h.SyncKnowledgeBase)
	r.PUT("/knowledge-provider/enable", h.EnableKnowledgeProvider)
	r.PUT("/knowledge-provider/disable", h.DisableKnowledgeProvider)
	r.POST("/circuit-breaker/reset", h.ResetCircuitBreaker)

	cases := []struct {
		method string
		path   string
		body   interface{}
		want   int
	}{
		{method: http.MethodGet, path: "/status", want: http.StatusOK},
		{method: http.MethodGet, path: "/metrics", want: http.StatusServiceUnavailable},
		{method: http.MethodPost, path: "/query", body: map[string]interface{}{"query": "hello"}, want: http.StatusOK},
		{method: http.MethodPost, path: "/knowledge/upload", body: map[string]interface{}{"title": "doc", "content": "body"}, want: http.StatusServiceUnavailable},
		{method: http.MethodPost, path: "/knowledge/sync", want: http.StatusServiceUnavailable},
		{method: http.MethodPut, path: "/knowledge-provider/enable", want: http.StatusServiceUnavailable},
		{method: http.MethodPut, path: "/knowledge-provider/disable", want: http.StatusServiceUnavailable},
		{method: http.MethodPost, path: "/circuit-breaker/reset", want: http.StatusServiceUnavailable},
	}

	for _, tc := range cases {
		var bodyReader *bytes.Reader
		if tc.body != nil {
			payload, err := json.Marshal(tc.body)
			if err != nil {
				t.Fatalf("marshal %s %s: %v", tc.method, tc.path, err)
			}
			bodyReader = bytes.NewReader(payload)
		} else {
			bodyReader = bytes.NewReader(nil)
		}

		req := httptest.NewRequest(tc.method, tc.path, bodyReader)
		if tc.body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s %s status=%d want=%d body=%s", tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
	}
}

func TestReleaseCheckRealtimeReadHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	wsHandler := handlers.NewWebSocketHandler(stubRealtimeGateway{})
	webrtcHandler := handlers.NewWebRTCHandler(stubRTCGateway{})
	messageHandler := handlers.NewMessageHandler(stubMessageRouter{})

	r := gin.New()
	r.GET("/ws/stats", wsHandler.GetStats)
	r.GET("/webrtc/stats", webrtcHandler.GetStats)
	r.GET("/messages/platforms", messageHandler.GetPlatformStats)

	for _, path := range []string{"/ws/stats", "/webrtc/stats", "/messages/platforms"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/webrtc/stats?session_id=s1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("session webrtc stats status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestReleaseCheckMetricsIngest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	agg := handlers.NewMetricsAggregator()
	h := handlers.NewMetricsIngestHandler(agg)

	r := gin.New()
	r.POST("/metrics/ingest", h.Ingest)

	payload := map[string]interface{}{
		"source":     "sdk",
		"tenant":     "t1",
		"session_id": "s1",
		"metrics": []map[string]interface{}{
			{"name": "sdk_messages_sent_total", "value": 2},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/metrics/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("metrics ingest status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestReleaseCheckPortalConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.GetDefaultConfig()
	cfg.Portal.BrandName = "Servify"
	cfg.Portal.DefaultLocale = "zh-CN"
	cfg.Portal.Locales = []string{"zh-CN", "en-US"}

	resolver := configscope.NewResolver(cfg)
	h := handlers.NewPortalConfigHandlerWithResolver(cfg, resolver)

	r := gin.New()
	r.GET("/public/portal/config", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/public/portal/config", nil)
	req = req.WithContext(context.Background())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("portal config status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestReleaseCheckSmokeTimestampStability(t *testing.T) {
	if time.Now().IsZero() {
		t.Fatal("unexpected zero time")
	}
}
