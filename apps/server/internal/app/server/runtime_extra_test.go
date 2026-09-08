package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	"servify/apps/server/internal/platform/eventbus"
	realtimeplatform "servify/apps/server/internal/platform/realtime"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newRuntimeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:runtime_assembly_"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.TenantConfig{}, &models.WorkspaceConfig{}, &models.User{}, &models.UserAuthSession{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func newRuntimeTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.GetDefaultConfig()
	cfg.JWT.Secret = "runtime-test-secret"
	cfg.Log.Level = "debug"
	// Keep monitoring off by default: enabling it registers Prometheus
	// collectors on the process-wide DefaultRegistry and must happen at most
	// once per test binary.
	cfg.Monitoring.Enabled = false
	return cfg
}

func TestBuildRuntimeAssemblesFullGraph(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	db := newRuntimeTestDB(t)
	logger := logrus.New()

	rt, err := BuildRuntime(cfg, logger, db, nil, eventbus.NewInMemoryBus())
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer func() { _ = rt.Stop(context.Background()) }()

	if rt.AIService == nil || rt.AIHandlerService == nil {
		t.Fatal("expected AI services to be wired")
	}
	if rt.RealtimeGateway == nil || rt.RTCGateway == nil || rt.MessageRouter == nil {
		t.Fatal("expected realtime services to be wired")
	}
	if rt.ConversationHandler == nil || rt.VoiceCoordinator == nil || rt.VoiceProtocolRegistry == nil {
		t.Fatal("expected conversation/voice services to be wired")
	}
	if rt.AgentHandlerService == nil || rt.CustomerHandlerService == nil || rt.TicketHandlerService == nil {
		t.Fatal("expected operational services to be wired")
	}
	if rt.TransferHandlerService == nil || rt.StatisticsHandlerService == nil || rt.SLAService == nil {
		t.Fatal("expected transfer/statistics services to be wired")
	}

	if err := rt.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if rt.StatisticsServiceForWorker() == nil {
		t.Fatal("expected statistics service for worker")
	}
	if rt.SLAServiceForWorker() == nil {
		t.Fatal("expected SLA service for worker")
	}

	deps := rt.RouterDependencies()
	if deps.Config != cfg || deps.DB != db {
		t.Fatal("router dependencies should carry config and db")
	}
	router := rt.Router()
	if router == nil {
		t.Fatal("expected router handler")
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /health = %d body=%s", w.Code, w.Body.String())
	}

	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestBuildRuntimeInitializesObservabilityWhenMonitoringEnabled(t *testing.T) {
	// initializeObservability registers Prometheus collectors on the
	// process-wide registry; swap in a fresh one so repeated in-process runs
	// (go test -count=2) cannot trip duplicate registration.
	previousRegistry := svcmetrics.DefaultRegistry
	svcmetrics.DefaultRegistry = svcmetrics.NewRegistry()
	t.Cleanup(func() { svcmetrics.DefaultRegistry = previousRegistry })

	cfg := newRuntimeTestConfig(t)
	cfg.Monitoring.Enabled = true

	rt, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer func() { _ = rt.Stop(context.Background()) }()

	if rt.HTTPMetrics == nil {
		t.Fatal("expected HTTP metrics to be initialized")
	}

	router := rt.Router()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, cfg.Monitoring.MetricsPath, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", cfg.Monitoring.MetricsPath, w.Code)
	}
}

func TestBuildRuntimeMetricsRouteUsesAggregateHandlerWithoutHTTPMetrics(t *testing.T) {
	cfg := newRuntimeTestConfig(t)

	// Build with monitoring disabled to avoid duplicate collector registration
	// on the shared DefaultRegistry, then flip the flag before building the
	// router so the aggregate metrics handler path is exercised.
	rt, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer func() { _ = rt.Stop(context.Background()) }()

	cfg.Monitoring.Enabled = true
	router := rt.Router()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, cfg.Monitoring.MetricsPath, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", cfg.Monitoring.MetricsPath, w.Code)
	}
}

func TestRuntimeStopWithoutMessageRouter(t *testing.T) {
	rt := &Runtime{}
	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() with nil router = %v", err)
	}
}

func TestBuildRuntimeVoiceProviderFailure(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	cfg.Server.Environment = "production"
	cfg.Voice.RecordingProvider = "mock"

	if _, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus()); err == nil {
		t.Fatal("expected BuildRuntime to fail with unsupported production voice provider")
	}
}

func TestBuildRealtimeRuntimeWithNilDBAndLifecycle(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	logger := logrus.New()

	rt := BuildRealtimeRuntime(cfg, logger, nil, nil, nil)
	if rt.RealtimeGateway == nil || rt.RTCGateway == nil || rt.MessageRouter == nil {
		t.Fatal("expected realtime primitives to be wired without db")
	}

	if err := rt.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestBuildRealtimeRuntimeWithDB(t *testing.T) {
	cfg := newRuntimeTestConfig(t)
	rt := BuildRealtimeRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, nil)
	if rt.DB == nil {
		t.Fatal("expected db to be attached")
	}
	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRealtimeRuntimeStopWithoutRouter(t *testing.T) {
	rt := &RealtimeRuntime{}
	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() with nil router = %v", err)
	}
}

type stubRealtimeGateway struct {
	session  string
	messages []realtimeplatform.Message
}

func (g *stubRealtimeGateway) HandleWebSocket(*gin.Context) {}
func (g *stubRealtimeGateway) SendToSession(sessionID string, message realtimeplatform.Message) {
	g.session = sessionID
	g.messages = append(g.messages, message)
}
func (g *stubRealtimeGateway) ClientCount() int { return len(g.messages) }

func TestNewRoutingTransferNotifier(t *testing.T) {
	if newRoutingTransferNotifier(nil) != nil {
		t.Fatal("expected nil notifier for nil gateway")
	}

	gateway := &stubRealtimeGateway{}
	notifier := newRoutingTransferNotifier(gateway)
	if notifier == nil {
		t.Fatal("expected notifier")
	}

	sent := time.Now()
	notifier.SendToSession("session-1", routingdelivery.Notification{
		Type:      "transfer.initiated",
		Data:      map[string]string{"queue": "vip"},
		SessionID: "session-1",
		Timestamp: sent,
	})
	if gateway.session != "session-1" {
		t.Fatalf("notified session = %q", gateway.session)
	}
	if len(gateway.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(gateway.messages))
	}
	if gateway.messages[0].Type != "transfer.initiated" || gateway.messages[0].SessionID != "session-1" {
		t.Fatalf("unexpected message: %+v", gateway.messages[0])
	}
}

func TestStatisticsAndSLAAccessorsNil(t *testing.T) {
	rt := &Runtime{}
	if rt.StatisticsServiceForWorker() != nil {
		t.Fatal("expected nil statistics service on empty runtime")
	}
	if rt.SLAServiceForWorker() != nil {
		t.Fatal("expected nil SLA service on empty runtime")
	}
}

func TestRouterDependenciesCarriesConfig(t *testing.T) {
	cfg := config.GetDefaultConfig()
	rt := &Runtime{Config: cfg}

	deps := rt.RouterDependencies()
	if deps.Config != cfg {
		t.Fatal("router dependencies should carry config")
	}
}
