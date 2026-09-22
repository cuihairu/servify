package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func TestScopedAIServicesNilLoggerFallback(t *testing.T) {
	handler := NewScopedAIHandlerService(config.GetDefaultConfig(), nil, openScopedAITestDB(t), stubFallbackAIHandler{}, nil, nil)
	if _, ok := handler.GetMetrics(); !ok {
		t.Fatal("expected fallback metrics with nil logger")
	}

	runtimeSvc := NewScopedAIRuntimeService(config.GetDefaultConfig(), nil, openScopedAITestDB(t), stubRuntimeFallback{}, nil)
	if got, err := runtimeSvc.GetSessionSummary(nil); err != nil || got != "summary" {
		t.Fatalf("GetSessionSummary() = (%q, %v)", got, err)
	}
}

func TestScopedAIHandlerServiceNilReceiverBranches(t *testing.T) {
	var handler *scopedAIHandlerService
	if handler.SetKnowledgeProviderEnabled(true) {
		t.Fatal("nil receiver SetKnowledgeProviderEnabled should return false")
	}
	if handler.ResetCircuitBreaker() {
		t.Fatal("nil receiver ResetCircuitBreaker should return false")
	}
	if svc := handler.buildService(context.Background()); svc != nil {
		t.Fatalf("nil receiver buildService = %v", svc)
	}
}

func TestRuntimeServiceFromResolvedConfigNilLogger(t *testing.T) {
	svc := runtimeServiceFromResolvedConfig(config.OpenAIConfig{}, config.DifyConfig{}, config.RagFlowConfig{}, config.WeKnoraConfig{}, config.AIConfig{}, nil, nil, nil)
	if svc == nil {
		t.Fatal("expected default runtime service with nil logger")
	}
}

func TestGormDBStatsProviderInvalidDB(t *testing.T) {
	provider := newGormDBStatsProvider(&gorm.DB{Config: &gorm.Config{}})
	if _, ok := provider.Stats(); ok {
		t.Fatal("expected stats unavailable for db without connection pool")
	}
}

func TestRegisterBaseMiddlewareWithTracing(t *testing.T) {
	cfg := testRouterConfig()
	cfg.Monitoring.Tracing.Enabled = true
	cfg.Monitoring.Tracing.ServiceName = "runtime-extra-test"

	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerBaseMiddleware(r, cfg, nil)
	r.GET("/traced", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/traced", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /traced = %d", w.Code)
	}
}

func TestBuildRuntimeFailsWhenAIRuntimeUnhealthy(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	cfg.WeKnora.Enabled = true
	cfg.WeKnora.BaseURL = "http://127.0.0.1:1"
	cfg.Fallback.Enabled = false

	if _, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus()); err == nil {
		t.Fatal("expected BuildRuntime to fail when the AI assembly is unhealthy and fallback disabled")
	}
}

func TestBuildRuntimeFailsWhenTranscriptProviderInvalid(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	cfg.Server.Environment = "production"
	cfg.Voice.RecordingProvider = "disabled"
	cfg.Voice.TranscriptProvider = "mock"

	if _, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus()); err == nil {
		t.Fatal("expected BuildRuntime to fail with production mock transcript provider")
	}
}

func TestRouteSecurityWarningsDeduplicatesAndIgnoresEmptyPaths(t *testing.T) {
	routes := gin.RoutesInfo{
		{Method: http.MethodGet, Path: ""},
		{Method: http.MethodGet, Path: "/public/uncatalogued"},
		{Method: http.MethodGet, Path: "/public/uncatalogued"},
	}
	cfg := testRouterConfig()

	warnings := RouteSecurityWarnings(routes, cfg)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want single deduplicated entry", warnings)
	}
	if !strings.Contains(warnings[0], "/public/uncatalogued") {
		t.Fatalf("unexpected warning text: %q", warnings[0])
	}
	if routeRequiresSecurityCatalog("   ", cfg) {
		t.Fatal("blank path should not require catalog")
	}
}

func TestRegisterStaticServesExistingAssetFromDetectedRoot(t *testing.T) {
	dir := t.TempDir()
	dist := filepath.Join(dir, "apps", "admin", "dist")
	if err := os.MkdirAll(filepath.Join(dist, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>admin</html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dist, "assets", "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	gin.SetMode(gin.TestMode)
	router := BuildRouter(Dependencies{Config: testRouterConfig()})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /assets/app.js = %d", w.Code)
	}
	if body := w.Body.String(); body != "console.log(1)" {
		t.Fatalf("unexpected asset body: %q", body)
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET / = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "admin") {
		t.Fatalf("expected index html body, got %q", w.Body.String())
	}
}

func TestBuildRuntimeFailsWhenTURNConfigIncomplete(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	// 装配层兜底 gate：URL 非空但 realm/secret 缺失，wireRealtimeGateways 必须拒绝。
	cfg.WebRTC.TURN.URL = "turn:turn.example.com:3478"

	_, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err == nil {
		t.Fatal("expected BuildRuntime to fail when TURN URL is set without realm/secret")
	}
	if !strings.Contains(err.Error(), "webrtc turn") {
		t.Fatalf("expected webrtc turn gate error, got %v", err)
	}
}
