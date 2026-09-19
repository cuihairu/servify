package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"strings"
	"testing"

	"servify/apps/server/internal/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// ---- handlers.go ----

func TestCxcWebSocketHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gw := &unitWsGateway{count: 4}
	h := NewWebSocketHandler(gw)
	r := gin.New()
	r.GET("/ws", h.HandleWebSocket)
	r.GET("/stats", h.GetStats)

	if w := cxcPerform(r, http.MethodGet, "/ws", nil, ""); w.Code != http.StatusOK {
		t.Fatalf("websocket expected 200, got %d", w.Code)
	}
	if gw.handled != 1 {
		t.Fatalf("expected gateway to handle websocket once, got %d", gw.handled)
	}

	w := cxcPerform(r, http.MethodGet, "/stats", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("stats expected 200, got %d", w.Code)
	}
	var got struct {
		Success bool `json:"success"`
		Data    struct {
			ConnectedClients int    `json:"connected_clients"`
			Status           string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Success || got.Data.ConnectedClients != 4 || got.Data.Status != "running" {
		t.Fatalf("unexpected stats body: %s", w.Body.String())
	}
}

func TestCxcWebRTCHandlerGetConnections(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewWebRTCHandler(&stubRTCGateway{count: 7})
	r := gin.New()
	r.GET("/connections", h.GetConnections)

	w := cxcPerform(r, http.MethodGet, "/connections", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"connection_count":7`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestCxcHealthHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHealthHandler()
	r := gin.New()
	r.GET("/health", h.Health)
	r.GET("/ready", h.Ready)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-Time", "2026-01-02T03:04:05Z")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("health expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"healthy"`) || !strings.Contains(w.Body.String(), "2026-01-02T03:04:05Z") {
		t.Fatalf("unexpected health body: %s", w.Body.String())
	}

	wReady := cxcPerform(r, http.MethodGet, "/ready", nil, "")
	if wReady.Code != http.StatusOK {
		t.Fatalf("ready expected 200, got %d", wReady.Code)
	}
	if !strings.Contains(wReady.Body.String(), `"ready"`) {
		t.Fatalf("unexpected ready body: %s", wReady.Body.String())
	}
}

// ---- health_enhanced.go ----

func cxcHealthHandler(cfg *config.Config, ai *unitAIService, db DatabasePing, redisClient *redis.Client) *EnhancedHealthHandler {
	return NewEnhancedHealthHandler(cfg, ai, db, redisClient)
}

func cxcHealthConfig() *config.Config {
	cfg := config.GetDefaultConfig()
	cfg.Monitoring.HealthChecks.Database = false
	cfg.Monitoring.HealthChecks.Redis = false
	cfg.Monitoring.HealthChecks.KnowledgeProvider = false
	cfg.Monitoring.HealthChecks.WeKnora = false
	cfg.WeKnora.Enabled = false
	cfg.Dify.Enabled = false
	return cfg
}

func TestCxcEnhancedReadyVariants(t *testing.T) {
	gin.SetMode(gin.TestMode)

	readyAI := &unitAIService{status: map[string]interface{}{"status": "ok"}}

	// ai not ready + db disabled -> 503
	h := cxcHealthHandler(cxcHealthConfig(), &unitAIService{}, nil, nil)
	r := gin.New()
	r.GET("/ready", h.Ready)
	w := cxcPerform(r, http.MethodGet, "/ready", nil, "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
	var got map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["ready"] != false {
		t.Fatalf("expected ready=false, body=%s", w.Body.String())
	}

	// db.DB() error
	h2 := cxcHealthHandler(cxcHealthConfig(), readyAI, &unitDBPing{err: errors.New("no conn")}, nil)
	r2 := gin.New()
	r2.GET("/ready", h2.Ready)
	if w := cxcPerform(r2, http.MethodGet, "/ready", nil, ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for db error, got %d body=%s", w.Code, w.Body.String())
	}

	// db ping error (closed handle)
	closed, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := closed.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
	h3 := cxcHealthHandler(cxcHealthConfig(), readyAI, &unitDBPing{db: closed}, nil)
	r3 := gin.New()
	r3.GET("/ready", h3.Ready)
	if w := cxcPerform(r3, http.MethodGet, "/ready", nil, ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for ping failure, got %d body=%s", w.Code, w.Body.String())
	}

	// db ping ok -> ready
	open, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer open.Close()
	h4 := cxcHealthHandler(cxcHealthConfig(), readyAI, &unitDBPing{db: open}, nil)
	r4 := gin.New()
	r4.GET("/ready", h4.Ready)
	w4 := cxcPerform(r4, http.MethodGet, "/ready", nil, "")
	if w4.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w4.Code, w4.Body.String())
	}
	var readyResp struct {
		Ready    bool              `json:"ready"`
		Services map[string]string `json:"services"`
	}
	if err := json.Unmarshal(w4.Body.Bytes(), &readyResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !readyResp.Ready || readyResp.Services["ai"] != "ready" || readyResp.Services["database"] != "ready" {
		t.Fatalf("expected fully ready services, body=%s", w4.Body.String())
	}
}

func TestCxcCheckAIServiceBranches(t *testing.T) {
	h := cxcHealthHandler(cxcHealthConfig(), &unitAIService{}, nil, nil)

	// status nil -> unhealthy
	resp := &HealthResponse{Services: map[string]ServiceInfo{}}
	all := true
	h.checkAIService(context.Background(), resp, &all)
	if all {
		t.Fatal("expected unhealthy when status is nil")
	}
	if resp.Services["ai"].Status != "unhealthy" {
		t.Fatalf("unexpected ai info: %+v", resp.Services["ai"])
	}

	// enhanced service with metrics -> details overridden
	h2 := cxcHealthHandler(cxcHealthConfig(), &unitAIService{
		status:    map[string]interface{}{"status": "ok"},
		metrics:   &aidelivery.AIMetrics{QueryCount: 3},
		metricsOK: true,
	}, nil, nil)
	resp2 := &HealthResponse{Services: map[string]ServiceInfo{}}
	all2 := true
	h2.checkAIService(context.Background(), resp2, &all2)
	if !all2 {
		t.Fatal("expected healthy")
	}
	details, ok := resp2.Services["ai"].Details.(map[string]interface{})
	if !ok || details["type"] != "enhanced" {
		t.Fatalf("expected enhanced details, got %#v", resp2.Services["ai"].Details)
	}
}

func TestCxcCheckDatabaseBranches(t *testing.T) {
	h := cxcHealthHandler(cxcHealthConfig(), nil, nil, nil)

	// db nil -> unhealthy
	resp := &HealthResponse{Services: map[string]ServiceInfo{}}
	all := true
	h.checkDatabase(context.Background(), resp, &all)
	if all || resp.Services["database"].Status != "unhealthy" {
		t.Fatalf("expected unhealthy for nil db: %+v all=%v", resp.Services["database"], all)
	}

	// DB() error
	h2 := cxcHealthHandler(cxcHealthConfig(), nil, &unitDBPing{err: errors.New("no conn")}, nil)
	resp2 := &HealthResponse{Services: map[string]ServiceInfo{}}
	all2 := true
	h2.checkDatabase(context.Background(), resp2, &all2)
	if all2 || resp2.Services["database"].Status != "unhealthy" {
		t.Fatalf("expected unhealthy for DB() error: %+v", resp2.Services["database"])
	}

	// ping error
	closed, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	_ = closed.Close()
	h3 := cxcHealthHandler(cxcHealthConfig(), nil, &unitDBPing{db: closed}, nil)
	resp3 := &HealthResponse{Services: map[string]ServiceInfo{}}
	all3 := true
	h3.checkDatabase(context.Background(), resp3, &all3)
	if all3 || resp3.Services["database"].Status != "unhealthy" {
		t.Fatalf("expected unhealthy for ping failure: %+v", resp3.Services["database"])
	}

	// ping ok
	open, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer open.Close()
	h4 := cxcHealthHandler(cxcHealthConfig(), nil, &unitDBPing{db: open}, nil)
	resp4 := &HealthResponse{Services: map[string]ServiceInfo{}}
	all4 := false
	h4.checkDatabase(context.Background(), resp4, &all4)
	if resp4.Services["database"].Status != "healthy" || resp4.Services["database"].Error != "" {
		t.Fatalf("expected healthy db: %+v", resp4.Services["database"])
	}
}

func TestCxcCheckRedisUnhealthy(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	mr.Close()

	h := cxcHealthHandler(cxcHealthConfig(), nil, nil, client)
	resp := &HealthResponse{Services: map[string]ServiceInfo{}}
	all := true
	h.checkRedis(context.Background(), resp, &all)
	if all {
		t.Fatal("expected unhealthy redis after server close")
	}
	if resp.Services["redis"].Status != "unhealthy" {
		t.Fatalf("unexpected redis info: %+v", resp.Services["redis"])
	}
}

func TestCxcCheckKnowledgeProviderBranches(t *testing.T) {
	ctx := context.Background()

	// non-enhanced service -> disabled
	h := cxcHealthHandler(cxcHealthConfig(), &unitAIService{}, nil, nil)
	resp := &HealthResponse{Services: map[string]ServiceInfo{}}
	all := true
	h.checkKnowledgeProvider(ctx, resp, &all)
	if resp.Services["knowledge_provider"].Status != "disabled" {
		t.Fatalf("expected disabled provider info: %+v", resp.Services)
	}

	// provider id from status, dify healthy via "<id>_healthy"
	cfg := cxcHealthConfig()
	h2 := cxcHealthHandler(cfg, &unitAIService{
		status: map[string]interface{}{
			"knowledge_provider": "dify",
			"dify_healthy":       true,
		},
		metricsOK: true,
	}, nil, nil)
	resp2 := &HealthResponse{Services: map[string]ServiceInfo{}}
	all2 := true
	h2.checkKnowledgeProvider(ctx, resp2, &all2)
	if !all2 || resp2.Services["dify"].Status != "healthy" {
		t.Fatalf("expected healthy dify: %+v all=%v", resp2.Services, all2)
	}

	// provider id empty + dify enabled -> fallback key "knowledge_provider_healthy"
	cfg3 := cxcHealthConfig()
	cfg3.Dify.Enabled = true
	h3 := cxcHealthHandler(cfg3, &unitAIService{
		status: map[string]interface{}{
			"knowledge_provider_healthy": true,
		},
		metricsOK: true,
	}, nil, nil)
	resp3 := &HealthResponse{Services: map[string]ServiceInfo{}}
	all3 := true
	h3.checkKnowledgeProvider(ctx, resp3, &all3)
	if !all3 || resp3.Services["dify"].Status != "healthy" {
		t.Fatalf("expected healthy dify via fallback key: %+v all=%v", resp3.Services, all3)
	}

	// provider id empty + dify disabled -> weknora details (default branch)
	cfg4 := cxcHealthConfig()
	h4 := cxcHealthHandler(cfg4, &unitAIService{
		status: map[string]interface{}{
			"knowledge_provider_healthy": true,
		},
		metricsOK: true,
	}, nil, nil)
	resp4 := &HealthResponse{Services: map[string]ServiceInfo{}}
	all4 := true
	h4.checkKnowledgeProvider(ctx, resp4, &all4)
	if !all4 || resp4.Services["weknora"].Status != "healthy" {
		t.Fatalf("expected healthy weknora: %+v all=%v", resp4.Services, all4)
	}
	details, ok := resp4.Services["weknora"].Details.(map[string]interface{})
	if !ok || details["base_url"] == nil {
		t.Fatalf("expected weknora details, got %#v", resp4.Services["weknora"].Details)
	}

	// explicitly unhealthy provider key (note: checkKnowledgeProvider does not
	// flip allHealthy for the external provider, it only reports the state)
	h5 := cxcHealthHandler(cxcHealthConfig(), &unitAIService{
		status: map[string]interface{}{
			"knowledge_provider": "weknora",
			"weknora_healthy":    false,
		},
		metricsOK: true,
	}, nil, nil)
	resp5 := &HealthResponse{Services: map[string]ServiceInfo{}}
	all5 := true
	h5.checkKnowledgeProvider(ctx, resp5, &all5)
	if resp5.Services["weknora"].Status != "unhealthy" || resp5.Services["weknora"].Error != "weknora service unavailable" {
		t.Fatalf("expected unhealthy weknora: %+v", resp5.Services["weknora"])
	}
}

func TestCxcEnhancedHealthFullyConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	open, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer open.Close()

	cfg := cxcHealthConfig()
	cfg.Monitoring.HealthChecks.Database = true
	cfg.Monitoring.HealthChecks.Redis = true
	cfg.Monitoring.HealthChecks.KnowledgeProvider = true
	cfg.Dify.Enabled = true

	ai := &unitAIService{
		status: map[string]interface{}{
			"knowledge_provider": "dify",
			"dify_healthy":       true,
		},
		metrics:   &aidelivery.AIMetrics{QueryCount: 1},
		metricsOK: true,
	}
	h := cxcHealthHandler(cfg, ai, &unitDBPing{db: open}, client)
	r := gin.New()
	r.GET("/health", h.Health)
	w := cxcPerform(r, http.MethodGet, "/health", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"healthy"`) {
		t.Fatalf("expected healthy status, body=%s", w.Body.String())
	}
}

// ---- metrics_ingest.go ----

func TestCxcMetricsIngestBadJSONAndZeroValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	agg := NewMetricsAggregator()
	h := NewMetricsIngestHandler(agg)
	r := gin.New()
	r.POST("/ingest", h.Ingest)

	if w := cxcPerform(r, http.MethodPost, "/ingest", strings.NewReader("{bad"), "application/json"); w.Code != http.StatusBadRequest {
		t.Fatalf("bad json expected 400, got %d body=%s", w.Code, w.Body.String())
	}

	// value 0 is normalized to 1; no tenant/session keeps base labels
	payload := MetricsIngestRequest{
		Source: "agent",
		Metrics: []IngestedMetric{
			{Name: "agent_online_gauge", Value: 0, Labels: map[string]string{"region": "cn"}},
		},
	}
	if w := cxcPerform(r, http.MethodPost, "/ingest", cxcJSONBody(payload), "application/json"); w.Code != http.StatusOK {
		t.Fatalf("ingest expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	snap := agg.Snapshot()
	series, ok := snap["agent_online_gauge"]
	if !ok {
		t.Fatalf("expected agent_online_gauge in snapshot: %#v", snap)
	}
	var gotVal float64
	var gotKey string
	for k, v := range series {
		gotKey, gotVal = k, v
	}
	if gotVal != 1 {
		t.Fatalf("expected normalized value 1, got %v", gotVal)
	}
	if !strings.Contains(gotKey, `source="agent"`) || !strings.Contains(gotKey, `region="cn"`) {
		t.Fatalf("expected merged labels in key %q", gotKey)
	}
	if strings.Contains(gotKey, "tenant") || strings.Contains(gotKey, "session") {
		t.Fatalf("unexpected tenant/session labels in key %q", gotKey)
	}
}

func TestCxcLabelsKeyEmpty(t *testing.T) {
	if got := labelsKey(map[string]string{}); got != "" {
		t.Fatalf("expected empty key, got %q", got)
	}
	if got := labelsKey(nil); got != "" {
		t.Fatalf("expected empty key for nil, got %q", got)
	}
}

// ---- upload_handler.go (FileUploadHandler) ----

func TestCxcFileUploadHandlerBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// no file provided, maxSize == 0 (skips MaxBytesReader)
	h := NewFileUploadHandler(&unitStorageProvider{}, 0)
	r := gin.New()
	r.POST("/upload", h.Upload)
	if w := cxcPerform(r, http.MethodPost, "/upload", nil, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("no file expected 400, got %d body=%s", w.Code, w.Body.String())
	}

	// oversized body with maxSize > 0 -> MaxBytesReader triggers FormFile error
	h2 := NewFileUploadHandler(&unitStorageProvider{}, 8)
	r2 := gin.New()
	r2.POST("/upload", h2.Upload)
	req, err := buildMultipart("/upload", "file", "big.txt", []byte("this is way more than eight bytes"))
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r2.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized expected 400, got %d body=%s", w.Code, w.Body.String())
	}

	// disallowed extension
	h3 := NewFileUploadHandler(&unitStorageProvider{}, 0)
	r3 := gin.New()
	r3.POST("/upload", h3.Upload)
	req3, err := buildMultipart("/upload", "file", "malware.exe", []byte("MZ"))
	if err != nil {
		t.Fatal(err)
	}
	w3 := httptest.NewRecorder()
	r3.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("disallowed ext expected 400, got %d body=%s", w3.Code, w3.Body.String())
	}

	// provider save error
	prov := &unitStorageProvider{err: errors.New("disk full")}
	h4 := NewFileUploadHandler(prov, 0)
	r4 := gin.New()
	r4.POST("/upload", h4.Upload)
	req4, err := buildMultipart("/upload", "file", "ok.txt", []byte("data"))
	if err != nil {
		t.Fatal(err)
	}
	w4 := httptest.NewRecorder()
	r4.ServeHTTP(w4, req4)
	if w4.Code != http.StatusInternalServerError {
		t.Fatalf("save error expected 500, got %d body=%s", w4.Code, w4.Body.String())
	}

	// success
	provOK := &unitStorageProvider{}
	h5 := NewFileUploadHandler(provOK, 1024)
	r5 := gin.New()
	r5.POST("/upload", h5.Upload)
	req5, err := buildMultipart("/upload", "file", "photo.png", []byte("pngdata"))
	if err != nil {
		t.Fatal(err)
	}
	w5 := httptest.NewRecorder()
	r5.ServeHTTP(w5, req5)
	if w5.Code != http.StatusCreated {
		t.Fatalf("success expected 201, got %d body=%s", w5.Code, w5.Body.String())
	}
	var resp struct {
		Message  string `json:"message"`
		Filename string `json:"filename"`
		URL      string `json:"url"`
		Size     int64  `json:"size"`
	}
	if err := json.Unmarshal(w5.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Filename != "photo.png" || resp.Size != 7 || resp.URL == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if provOK.lastKey == "" || !strings.Contains(provOK.lastKey, "photo.png") {
		t.Fatalf("expected date-partitioned key, got %q", provOK.lastKey)
	}
}
