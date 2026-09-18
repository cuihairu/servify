package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// newScopedAIFreshDB opens a uniquely named in-memory database so tests that
// seed rows stay safe across repeated in-process runs (go test -count=2).
func newScopedAIFreshDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+fmt.Sprint(time.Now().UnixNano())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.WorkspaceConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestRegisterStaticRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := BuildRouter(Dependencies{Config: testRouterConfig()})

	t.Run("api paths render json 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/unknown", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Not found") {
			t.Fatalf("expected json 404 body, got %s", w.Body.String())
		}
	})

	t.Run("public paths render json 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/public/unknown", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 got %d", w.Code)
		}
	})

	t.Run("spa fallback serves index for missing asset", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/some/spa/route", nil))
		// The fallback targets index.html; without an admin dist in the test
		// working directory the file handler reports not found.
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing SPA index, got %d", w.Code)
		}
	})

	t.Run("root serves index fallback", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing root index, got %d", w.Code)
		}
	})

	t.Run("missing demo-sdk asset falls through to admin root", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/demo-sdk/missing.js", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing demo-sdk asset, got %d", w.Code)
		}
	})
}

func TestRegisterStaticServesDemoSDKAssetWhenPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "apps", "demo-sdk"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	asset := filepath.Join(dir, "apps", "demo-sdk", "widget.js")
	if err := os.WriteFile(asset, []byte("// demo sdk"), 0o644); err != nil {
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
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/demo-sdk/widget.js", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "demo sdk") {
		t.Fatalf("unexpected asset body: %s", w.Body.String())
	}
}

func TestDetectStaticRootFallsBackToDefault(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no", "such", "dir")
	if got := detectStaticRoot([]string{missing}); got != "./apps/admin/dist" {
		t.Fatalf("detectStaticRoot() = %q", got)
	}
}

func TestRegisterBaseMiddlewareCORSAndDebugMode(t *testing.T) {
	cfg := testRouterConfig()
	cfg.Log.Level = "debug"
	cfg.Security.CORS.Enabled = true
	cfg.Security.CORS.AllowedOrigins = []string{"https://admin.example.com"}
	cfg.Security.CORS.AllowedMethods = []string{"GET, POST"}
	cfg.Security.CORS.AllowedHeaders = []string{"X-Trace-Id"}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerBaseMiddleware(r, cfg, nil)
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/ping", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS = %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Fatalf("allow-methods = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "X-Trace-Id" {
		t.Fatalf("allow-headers = %q", got)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Code != http.StatusOK || w.Body.String() != "pong" {
		t.Fatalf("GET /ping = %d %q", w.Code, w.Body.String())
	}
	if gin.Mode() != gin.DebugMode {
		t.Fatalf("debug log level should switch gin to debug mode, got %q", gin.Mode())
	}
}

func TestRegisterBaseMiddlewareDefaultCORSHeaders(t *testing.T) {
	cfg := testRouterConfig()
	cfg.Security.CORS.Enabled = true

	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerBaseMiddleware(r, cfg, nil)
	r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("default allow-origin = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("expected default allow-methods header")
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Fatal("expected default allow-headers header")
	}
}

func TestRegisterHealthRoutesWithoutMonitoring(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := testRouterConfig()
	cfg.Monitoring.Enabled = false

	r := gin.New()
	registerHealthRoutes(r, Dependencies{Config: cfg, DB: newRouterAuthTestDB(t), AIHandlerService: stubAIHandlerService{}})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /health = %d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /ready = %d", w.Code)
	}
}

func TestBuildRouterLogsSecurityWarningsWhenLoggerPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(&strings.Builder{})

	cfg := testRouterConfig()
	cfg.Monitoring.Enabled = false
	router := BuildRouter(Dependencies{Config: cfg, Logger: logger})
	if router == nil {
		t.Fatal("expected router")
	}
}

func TestRouteSecurityWarningsEmptyRoutes(t *testing.T) {
	if got := RouteSecurityWarnings(nil, testRouterConfig()); got != nil {
		t.Fatalf("expected nil warnings for empty routes, got %v", got)
	}
}

func TestDBStatsProvider(t *testing.T) {
	if got := newGormDBStatsProvider(nil); got != nil {
		t.Fatal("expected nil provider for nil db")
	}

	provider := &gormDBStatsProvider{}
	if _, ok := provider.Stats(); ok {
		t.Fatal("expected nil receiver Stats() to report unavailable")
	}

	db := newRouterAuthTestDB(t)
	provider = newGormDBStatsProvider(db)
	stats, ok := provider.Stats()
	if !ok {
		t.Fatal("expected stats from live db")
	}
	if stats.OpenConnections < 1 && stats.MaxOpenConnections != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestBuildAIAssemblyFailurePaths(t *testing.T) {
	t.Run("dify unhealthy with required knowledge provider", func(t *testing.T) {
		cfg := config.GetDefaultConfig()
		cfg.Dify.Enabled = true
		cfg.Dify.BaseURL = "http://127.0.0.1:1"
		cfg.Dify.DatasetID = "ds-missing"
		cfg.WeKnora.Enabled = false

		_, err := BuildAIAssembly(cfg, logrus.New(), AIAssemblyOptions{
			RequireKnowledgeProviderHealthy: true,
			HealthCheckTimeout:              500 * time.Millisecond,
		})
		if err == nil {
			t.Fatal("expected dify health gate failure")
		}
	})

	t.Run("weknora unhealthy with required knowledge provider", func(t *testing.T) {
		cfg := config.GetDefaultConfig()
		cfg.WeKnora.Enabled = true
		cfg.WeKnora.BaseURL = "http://127.0.0.1:1"

		_, err := BuildAIAssembly(cfg, logrus.New(), AIAssemblyOptions{
			RequireWeKnoraHealthy: true,
			HealthCheckTimeout:    500 * time.Millisecond,
		})
		if err == nil {
			t.Fatal("expected weknora health gate failure")
		}
	})

	t.Run("weknora unhealthy with fallback disabled", func(t *testing.T) {
		cfg := config.GetDefaultConfig()
		cfg.WeKnora.Enabled = true
		cfg.WeKnora.BaseURL = "http://127.0.0.1:1"
		cfg.Fallback.Enabled = false

		_, err := BuildAIAssembly(cfg, logrus.New(), AIAssemblyOptions{
			HealthCheckTimeout: 500 * time.Millisecond,
		})
		if err == nil {
			t.Fatal("expected failure when weknora is down and fallback disabled")
		}
	})

	t.Run("weknora unhealthy with fallback enabled degrades gracefully", func(t *testing.T) {
		cfg := config.GetDefaultConfig()
		cfg.WeKnora.Enabled = true
		cfg.WeKnora.BaseURL = "http://127.0.0.1:1"
		cfg.Fallback.Enabled = true

		assembly, err := BuildAIAssembly(cfg, logrus.New(), AIAssemblyOptions{
			HealthCheckTimeout: 500 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("BuildAIAssembly() error = %v", err)
		}
		if assembly.KnowledgeProviderHealthy || assembly.WeKnoraHealthy {
			t.Fatalf("expected unhealthy assembly, got %+v", assembly)
		}
		if assembly.KnowledgeDriver != nil {
			t.Fatal("expected no knowledge driver after failed health check")
		}
	})

	t.Run("nil logger falls back to standard logger", func(t *testing.T) {
		cfg := config.GetDefaultConfig()
		cfg.Dify.Enabled = false
		cfg.WeKnora.Enabled = false

		assembly, err := BuildAIAssembly(cfg, nil, AIAssemblyOptions{})
		if err != nil {
			t.Fatalf("BuildAIAssembly(nil logger) error = %v", err)
		}
		if assembly == nil {
			t.Fatal("expected assembly")
		}
	})
}

func TestBuildAIAssemblySyncKnowledgeBaseWarnsOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := config.GetDefaultConfig()
	cfg.WeKnora.Enabled = true
	cfg.WeKnora.BaseURL = srv.URL
	cfg.WeKnora.APIKey = "wk-key"
	cfg.WeKnora.KnowledgeBaseID = "kb-sync"

	assembly, err := BuildAIAssembly(cfg, logrus.New(), AIAssemblyOptions{
		SyncKnowledgeBase:  true,
		HealthCheckTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("BuildAIAssembly() error = %v", err)
	}
	if !assembly.WeKnoraHealthy || assembly.KnowledgeProviderID != "weknora" {
		t.Fatalf("expected healthy weknora assembly, got %+v", assembly)
	}
	if assembly.WeKnoraClient == nil {
		t.Fatal("expected weknora client to be attached")
	}
}

func TestTimeoutForHealthCheckAndRequireHealthy(t *testing.T) {
	if got := timeoutForHealthCheck(AIAssemblyOptions{HealthCheckTimeout: 3 * time.Second}); got != 3*time.Second {
		t.Fatalf("explicit timeout = %v", got)
	}
	if got := timeoutForHealthCheck(AIAssemblyOptions{}); got != 10*time.Second {
		t.Fatalf("default timeout = %v", got)
	}
	if !(AIAssemblyOptions{RequireKnowledgeProviderHealthy: true}).requireKnowledgeProviderHealthy() {
		t.Fatal("RequireKnowledgeProviderHealthy should require healthy provider")
	}
	if !(AIAssemblyOptions{RequireWeKnoraHealthy: true}).requireKnowledgeProviderHealthy() {
		t.Fatal("RequireWeKnoraHealthy should require healthy provider")
	}
	if (AIAssemblyOptions{}).requireKnowledgeProviderHealthy() {
		t.Fatal("default options should not require healthy provider")
	}
}

func TestAIAssemblyNilReceiverKnowledgeProvider(t *testing.T) {
	var assembly *AIAssembly
	if assembly.KnowledgeProvider(testRouterConfig()) != nil {
		t.Fatal("nil assembly should expose nil provider")
	}
}

func TestScopedAIHandlerServiceNilFallbackBranches(t *testing.T) {
	handler := NewScopedAIHandlerService(config.GetDefaultConfig(), logrus.New(), nil, nil, nil, nil)

	if _, ok := handler.GetMetrics(); ok {
		t.Fatal("expected GetMetrics unavailable without fallback")
	}
	if handler.ResetCircuitBreaker() {
		t.Fatal("expected ResetCircuitBreaker false without fallback")
	}
	if !handler.SetKnowledgeProviderEnabled(true) {
		t.Fatal("expected SetKnowledgeProviderEnabled true without fallback")
	}

	if err := handler.SyncKnowledgeBase(context.Background()); err == nil {
		t.Fatal("expected sync error because default service has no knowledge provider")
	} else if !strings.Contains(err.Error(), "knowledge provider is not enabled") {
		t.Fatalf("unexpected sync error: %v", err)
	}
}

func TestScopedAIHandlerServiceNilResolverUsesDefaults(t *testing.T) {
	svc := &scopedAIHandlerService{cfg: config.GetDefaultConfig(), logger: logrus.New()}
	status := svc.GetStatus(context.Background())
	if _, ok := status["knowledge_provider"]; !ok {
		t.Fatalf("expected status from default service, got %+v", status)
	}
}

func TestScopedAIHandlerApplyRuntimeOverrides(t *testing.T) {
	svc := &scopedAIHandlerService{cfg: config.GetDefaultConfig(), logger: logrus.New()}

	if got := svc.applyRuntimeOverrides(nil); got != nil {
		t.Fatalf("applyRuntimeOverrides(nil) = %v", got)
	}
	if got := svc.applyRuntimeOverrides(stubRuntimeFallback{}); got == nil {
		t.Fatal("expected non-enhanced service to pass through")
	}
}

func TestScopedAIRuntimeServiceNilBranches(t *testing.T) {
	runtimeSvc := &scopedAIRuntimeService{}

	if resp, err := runtimeSvc.ProcessQuery(context.Background(), "q", "s"); resp != nil || err != nil {
		t.Fatalf("nil service ProcessQuery = (%v, %v)", resp, err)
	}
	if status := runtimeSvc.GetStatus(context.Background()); status != nil {
		t.Fatalf("nil service GetStatus = %v", status)
	}
	if runtimeSvc.ShouldTransferToHuman("help", nil) {
		t.Fatal("nil fallback transfer decision should be false")
	}
	if summary, err := runtimeSvc.GetSessionSummary(nil); summary != "" || err != nil {
		t.Fatalf("nil fallback summary = (%q, %v)", summary, err)
	}

	withFallback := &scopedAIRuntimeService{fallback: stubRuntimeFallback{}}
	status := withFallback.GetStatus(context.Background())
	if typ, _ := status["type"].(string); typ != "fallback-runtime" {
		t.Fatalf("expected fallback status without resolver, got %+v", status)
	}
}

func TestScopedAIRuntimeServiceGetStatusUsesWorkspaceDifyOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	db := newScopedAIFreshDB(t)
	if err := db.Create(&models.WorkspaceConfig{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		DifyJSON:    "enabled: true\nbase_url: " + srv.URL + "\napi_key: scoped-key\ndataset_id: ds-1\n",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	runtimeSvc := NewScopedAIRuntimeService(config.GetDefaultConfig(), logrus.New(), db, stubRuntimeFallback{}, nil)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	status := runtimeSvc.GetStatus(ctx)
	if provider, _ := status["knowledge_provider"].(string); provider != "dify" {
		t.Fatalf("expected scoped dify provider, got %+v", status)
	}
}

func TestBuildVoiceRecordingProviderTwilioBranches(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Voice.RecordingProvider = "twilio"
	cfg.Server.Environment = "development"

	if _, err := buildVoiceRecordingProvider(cfg, logrus.New()); err == nil {
		t.Fatal("expected twilio provider without credentials to fail")
	}

	cfg.Voice.Twilio.AccountSID = "sid"
	cfg.Voice.Twilio.AuthToken = "token"
	provider, err := buildVoiceRecordingProvider(cfg, logrus.New())
	if err != nil {
		t.Fatalf("twilio provider error = %v", err)
	}
	if provider == nil {
		t.Fatal("expected twilio recording provider")
	}
}

func TestBuildVoiceTranscriptProviderDeepgramBranches(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Voice.TranscriptProvider = "deepgram"

	if _, err := buildVoiceTranscriptProvider(cfg, logrus.New()); err == nil {
		t.Fatal("expected deepgram provider without api key to fail")
	}

	cfg.Voice.Deepgram.APIKey = "dg-key"
	provider, err := buildVoiceTranscriptProvider(cfg, logrus.New())
	if err != nil {
		t.Fatalf("deepgram provider error = %v", err)
	}
	if provider == nil {
		t.Fatal("expected deepgram transcript provider")
	}
}

func TestBuildVoiceProvidersRejectUnsupportedAndNormalize(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Voice.RecordingProvider = "  MoCk  "
	cfg.Server.Environment = "development"

	provider, err := buildVoiceRecordingProvider(cfg, logrus.New())
	if err != nil || provider == nil {
		t.Fatalf("normalized mock provider = (%v, %v)", provider, err)
	}

	cfg.Voice.RecordingProvider = "unknown-provider"
	if _, err := buildVoiceRecordingProvider(cfg, logrus.New()); err == nil {
		t.Fatal("expected unsupported recording provider error")
	}

	cfg.Voice.TranscriptProvider = "unknown-provider"
	if _, err := buildVoiceTranscriptProvider(cfg, logrus.New()); err == nil {
		t.Fatal("expected unsupported transcript provider error")
	}

	if _, err := buildVoiceRecordingProvider(nil, nil); err != nil {
		t.Fatalf("nil cfg recording provider error = %v", err)
	}
	if _, err := buildVoiceTranscriptProvider(nil, nil); err != nil {
		t.Fatalf("nil cfg transcript provider error = %v", err)
	}
}

func TestNormalizeVoiceProvider(t *testing.T) {
	if got := normalizeVoiceProvider("  "); got != voiceProviderDisabled {
		t.Fatalf("blank provider = %q", got)
	}
	if got := normalizeVoiceProvider(" Twilio "); got != voiceProviderTwilio {
		t.Fatalf("twilio provider = %q", got)
	}
}
