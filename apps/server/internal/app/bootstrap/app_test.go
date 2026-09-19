package bootstrap

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/alicebob/miniredis/v2"
)

func TestBuildApp(t *testing.T) {
	cfg := config.GetDefaultConfig()

	app, err := BuildApp(cfg)
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	if app == nil {
		t.Fatal("expected app")
	}
	if app.Config != cfg {
		t.Fatal("expected config to be assigned")
	}
	if app.Logger == nil {
		t.Fatal("expected logger to be initialized")
	}
	if app.EventBus == nil {
		t.Fatal("expected event bus to be initialized")
	}
	if app.Workers == nil {
		t.Fatal("expected workers slice")
	}
	if app.ShutdownHooks == nil {
		t.Fatal("expected shutdown hooks slice")
	}
	if app.EmbeddingProvider != nil {
		t.Fatal("expected default app to skip unconfigured embedding provider")
	}
}

func TestBuildEmbeddingProviderSkipsDefaultOpenAIWithoutAPIKey(t *testing.T) {
	cfg := config.GetDefaultConfig()

	provider, err := BuildEmbeddingProvider(cfg)
	if err != nil {
		t.Fatalf("BuildEmbeddingProvider() error = %v", err)
	}
	if provider != nil {
		t.Fatal("expected nil provider when default OpenAI API key is empty")
	}
}

func TestBuildEmbeddingProviderReturnsConfigurationErrors(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Embedding.Provider = "xinference"
	cfg.Embedding.Xinference.BaseURL = ""
	cfg.Embedding.Xinference.ModelUID = ""

	provider, err := BuildEmbeddingProvider(cfg)
	if err == nil {
		t.Fatal("expected configuration error")
	}
	if provider != nil {
		t.Fatal("expected nil provider on configuration error")
	}
	if !strings.Contains(err.Error(), "xinference provider requires base_url") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type stubWorker struct {
	name     string
	started  bool
	stopped  bool
	startErr error
	stopErr  error
}

type stubRuntime struct {
	started  bool
	stopped  bool
	startErr error
	stopErr  error
	router   http.Handler
}

func (w *stubWorker) Name() string { return w.name }
func (w *stubWorker) Start() error {
	w.started = true
	return w.startErr
}
func (w *stubWorker) Stop(context.Context) error {
	w.stopped = true
	return w.stopErr
}

func (r *stubRuntime) Start() error {
	r.started = true
	return r.startErr
}

func (r *stubRuntime) Stop(context.Context) error {
	r.stopped = true
	return r.stopErr
}

func (r *stubRuntime) Router() http.Handler {
	return r.router
}

func TestAppWorkerLifecycle(t *testing.T) {
	app, err := BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	w1 := &stubWorker{name: "w1"}
	w2 := &stubWorker{name: "w2"}
	app.RegisterWorker(w1)
	app.RegisterWorker(w2)

	if err := app.StartWorkers(); err != nil {
		t.Fatalf("StartWorkers() error = %v", err)
	}
	if !w1.started || !w2.started {
		t.Fatalf("expected all workers started: %+v %+v", w1, w2)
	}

	if err := app.StopWorkers(context.Background()); err != nil {
		t.Fatalf("StopWorkers() error = %v", err)
	}
	if !w1.stopped || !w2.stopped {
		t.Fatalf("expected all workers stopped: %+v %+v", w1, w2)
	}
}

func TestAppRuntimeLifecycle(t *testing.T) {
	app, err := BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	router := http.NewServeMux()
	rt := &stubRuntime{router: router}

	app.AttachHTTPRuntime(rt)
	if app.Runtime != rt {
		t.Fatal("expected runtime to be assigned")
	}
	if app.Router != router {
		t.Fatal("expected router to be assigned from runtime")
	}

	if err := app.StartRuntime(); err != nil {
		t.Fatalf("StartRuntime() error = %v", err)
	}
	if !rt.started {
		t.Fatal("expected runtime to start")
	}
	if err := app.StopRuntime(context.Background()); err != nil {
		t.Fatalf("StopRuntime() error = %v", err)
	}
	if !rt.stopped {
		t.Fatal("expected runtime to stop")
	}
}

func TestAppShutdownAggregatesErrors(t *testing.T) {
	app, err := BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	rt := &stubRuntime{router: http.NewServeMux(), stopErr: errors.New("runtime stop failed")}
	app.AttachHTTPRuntime(rt)
	app.RegisterWorker(&stubWorker{name: "broken-worker", stopErr: errors.New("worker stop failed")})
	app.AddShutdownHook(func() error {
		return errors.New("hook failed")
	})

	err = app.Shutdown(context.Background())
	if err == nil {
		t.Fatal("expected shutdown error")
	}
	for _, want := range []string{"worker stop failed", "runtime stop failed", "hook failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("shutdown error missing %q: %v", want, err)
		}
	}
	if !rt.stopped {
		t.Fatal("expected runtime stop to be called")
	}
}

func TestBuildServerRuntimeRejectsNilApp(t *testing.T) {
	var app *App
	if _, err := app.BuildServerRuntime(); err == nil {
		t.Fatal("expected nil app error")
	}
}

func TestAppStartWorkersError(t *testing.T) {
	app, err := BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	app.RegisterWorker(&stubWorker{name: "broken", startErr: errors.New("boom")})

	if err := app.StartWorkers(); err == nil {
		t.Fatal("expected start error")
	}
}

func TestAppShutdownHooks(t *testing.T) {
	app, err := BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	order := make([]string, 0, 2)
	app.AddShutdownHook(func() error {
		order = append(order, "first")
		return nil
	})
	app.AddShutdownHook(func() error {
		order = append(order, "second")
		return nil
	})

	if err := app.RunShutdownHooks(); err != nil {
		t.Fatalf("RunShutdownHooks() error = %v", err)
	}
	if len(order) != 2 || order[0] != "second" || order[1] != "first" {
		t.Fatalf("unexpected hook order: %v", order)
	}
}

// P3-3：production 不允许 in-memory 事件总线——装配层硬失败（与
// config.InsecureDefaults 的启动校验同口径），而非仅告警。
func TestBuildEventBusRejectsInMemoryInProduction(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Server.Environment = "production"

	bus, err := BuildEventBus(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error for inmemory provider in production")
	}
	if bus != nil {
		t.Fatal("expected no event bus")
	}
	if !strings.Contains(err.Error(), "not allowed in production") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEventBusRejectsUnsupportedProvider(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.EventBus.Provider = "kafka"

	bus, err := BuildEventBus(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if bus != nil {
		t.Fatal("expected nil bus for unsupported provider")
	}
}

func TestBuildEventBusRequiresRedisClientForRedisProvider(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.EventBus.Provider = "redis"

	bus, err := BuildEventBus(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing redis client")
	}
	if bus != nil {
		t.Fatal("expected nil bus for missing redis client")
	}
}

func TestRedisRequired(t *testing.T) {
	cfg := config.GetDefaultConfig()
	if redisRequired(cfg) {
		t.Fatal("did not expect redis by default")
	}

	cfg.EventBus.Provider = "redis"
	if !redisRequired(cfg) {
		t.Fatal("expected redis to be required for redis event bus")
	}

	cfg = config.GetDefaultConfig()
	cfg.Monitoring.HealthChecks.Redis = true
	if redisRequired(cfg) {
		t.Fatal("did not expect redis to be required for health checks alone")
	}
}

func TestBuildAppUsesRedisBusWhenConfigured(t *testing.T) {
	mr := miniredis.RunT(t)
	host, portStr, err := net.SplitHostPort(mr.Addr())
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("Atoi() error = %v", err)
	}

	cfg := config.GetDefaultConfig()
	cfg.EventBus.Provider = "redis"
	cfg.Redis.Host = host
	cfg.Redis.Port = port

	app, err := BuildApp(cfg)
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	if app.Redis == nil {
		t.Fatal("expected redis client to be initialized")
	}
	if _, ok := app.EventBus.(*eventbus.RedisBus); !ok {
		t.Fatalf("expected redis event bus, got %T", app.EventBus)
	}
	if err := app.RunShutdownHooks(); err != nil {
		t.Fatalf("RunShutdownHooks() error = %v", err)
	}
}
