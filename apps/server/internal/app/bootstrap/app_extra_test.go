package bootstrap

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"servify/apps/server/internal/config"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newBootstrapSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "bootstrap.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func TestBuildAppInitLoggingFailure(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	cfg := config.GetDefaultConfig()
	cfg.Log.Output = "file"
	cfg.Log.FilePath = filepath.Join(blocker, "logs", "app.log")

	if _, err := BuildApp(cfg); err == nil {
		t.Fatal("expected BuildApp to fail when the log directory cannot be created")
	}
}

func TestBuildAppOpenRedisFailure(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.EventBus.Provider = "redis"
	cfg.Redis.Host = "127.0.0.1"
	cfg.Redis.Port = 1

	if _, err := BuildApp(cfg); err == nil {
		t.Fatal("expected BuildApp to fail when redis is unreachable")
	}
}

func TestBuildAppEventBusFailure(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.EventBus.Provider = "kafka"

	if _, err := BuildApp(cfg); err == nil {
		t.Fatal("expected BuildApp to fail for unsupported event bus provider")
	}
}

func TestBuildAppEmbeddingProviderFailure(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Embedding.Provider = "tei"
	cfg.Embedding.TEI.BaseURL = ""

	_, err := BuildApp(cfg)
	if err == nil || !strings.Contains(err.Error(), "create embedding provider") {
		t.Fatalf("BuildApp() error = %v, want embedding provider failure", err)
	}
}

func TestBuildEmbeddingProviderBranches(t *testing.T) {
	if provider, err := BuildEmbeddingProvider(nil); provider != nil || err != nil {
		t.Fatalf("BuildEmbeddingProvider(nil) = (%v, %v), want (nil, nil)", provider, err)
	}

	cfg := config.GetDefaultConfig()
	cfg.Embedding.Provider = ""
	if provider, err := BuildEmbeddingProvider(cfg); provider != nil || err != nil {
		t.Fatalf("empty provider = (%v, %v), want (nil, nil)", provider, err)
	}

	cfg.Embedding.Provider = "openai"
	cfg.Embedding.OpenAI.APIKey = ""
	if provider, err := BuildEmbeddingProvider(cfg); provider != nil || err != nil {
		t.Fatalf("openai without key = (%v, %v), want (nil, nil)", provider, err)
	}
}

func TestOpenRedisBranches(t *testing.T) {
	if client, err := OpenRedis(nil); client != nil || err != nil {
		t.Fatalf("OpenRedis(nil) = (%v, %v), want (nil, nil)", client, err)
	}

	cfg := config.GetDefaultConfig()
	cfg.EventBus.Provider = "inmemory"
	if client, err := OpenRedis(cfg); client != nil || err != nil {
		t.Fatalf("OpenRedis(inmemory) = (%v, %v), want (nil, nil)", client, err)
	}

	cfg.EventBus.Provider = "redis"
	cfg.Redis.Host = "127.0.0.1"
	cfg.Redis.Port = 1
	if _, err := OpenRedis(cfg); err == nil {
		t.Fatal("expected OpenRedis to fail for unreachable redis")
	}
}

func TestInitLoggingBranches(t *testing.T) {
	logger, err := InitLogging(nil)
	if err != nil {
		t.Fatalf("InitLogging(nil) error = %v", err)
	}
	if logger != logrus.StandardLogger() {
		t.Fatal("expected standard logger")
	}

	blocker := filepath.Join(t.TempDir(), "blocker-file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	cfg := config.GetDefaultConfig()
	cfg.Log.Output = "file"
	cfg.Log.FilePath = filepath.Join(blocker, "sub", "log.txt")
	if _, err := InitLogging(cfg); err == nil {
		t.Fatal("expected InitLogging to fail for un-writable log path")
	}

	cfg.Log.Output = "stdout"
	cfg.Log.Level = "info"
	if _, err := InitLogging(cfg); err != nil {
		t.Fatalf("InitLogging(stdout) error = %v", err)
	}
}

func TestBuildEventBusBranches(t *testing.T) {
	bus, err := BuildEventBus(nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildEventBus(nil, ...) error = %v", err)
	}
	if bus == nil {
		t.Fatal("expected default in-memory bus for nil config")
	}

	cfg := config.GetDefaultConfig()
	cfg.EventBus.Provider = "  "
	bus, err = BuildEventBus(cfg, nil, nil)
	if err != nil || bus == nil {
		t.Fatalf("blank provider = (%v, %v), want default in-memory bus", bus, err)
	}
}

func TestAppNilReceiverAndNilArgumentGuards(t *testing.T) {
	var nilApp *App
	if err := nilApp.Shutdown(context.Background()); err != nil {
		t.Fatalf("nil app Shutdown() = %v", err)
	}
	if err := nilApp.StartRuntime(); err != nil {
		t.Fatalf("nil app StartRuntime() = %v", err)
	}
	if err := nilApp.StopRuntime(context.Background()); err != nil {
		t.Fatalf("nil app StopRuntime() = %v", err)
	}
	nilApp.AttachHTTPRuntime(nil)
	nilApp.RegisterWorker(nil)

	app, err := BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	app.RegisterWorker(nil)
	app.AddShutdownHook(nil)
	app.AttachHTTPRuntime(nil)
	if app.Runtime != nil || app.Router != nil {
		t.Fatal("nil runtime should not overwrite app runtime fields")
	}
	if err := app.StartRuntime(); err != nil {
		t.Fatalf("StartRuntime() without runtime = %v", err)
	}
	if err := app.StopRuntime(context.Background()); err != nil {
		t.Fatalf("StopRuntime() without runtime = %v", err)
	}
}

func TestBuildServerRuntimeAttachesRuntime(t *testing.T) {
	app, err := BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	// Avoid duplicate Prometheus collector registration across tests.
	app.Config.Monitoring.Enabled = false
	app.DB = newBootstrapSQLite(t)
	if err := AutoMigrate(app.DB); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	rt, err := app.BuildServerRuntime()
	if err != nil {
		t.Fatalf("BuildServerRuntime() error = %v", err)
	}
	if app.Runtime != rt {
		t.Fatal("expected runtime to be attached to the app")
	}
	if app.Router == nil {
		t.Fatal("expected router to be recorded on the app")
	}
	if err := app.StartRuntime(); err != nil {
		t.Fatalf("StartRuntime() error = %v", err)
	}
	if err := app.StopRuntime(context.Background()); err != nil {
		t.Fatalf("StopRuntime() error = %v", err)
	}
}

func TestHTTPServerHelpers(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Server.Host = "10.0.0.5"
	cfg.Server.Port = 8123

	if got := ListenAddress(cfg, HTTPServerOptions{}); got != "10.0.0.5:8123" {
		t.Fatalf("ListenAddress(config defaults) = %q", got)
	}
	if got := ListenAddress(nil, HTTPServerOptions{Host: "h", Port: 9}); got != "h:9" {
		t.Fatalf("ListenAddress(overrides) = %q", got)
	}
	if got := ListenAddress(nil, HTTPServerOptions{}); got != ":0" {
		t.Fatalf("ListenAddress(nothing) = %q", got)
	}

	handler := http.NewServeMux()
	server := NewHTTPServerForApp(nil, handler, HTTPServerOptions{})
	if server == nil || server.Handler != handler {
		t.Fatalf("NewHTTPServerForApp(nil, ...) = %+v", server)
	}

	if got := BuildHTTPServer(nil, HTTPServerOptions{Port: 7}); got == nil || got.Handler != nil {
		t.Fatalf("BuildHTTPServer(nil, ...) = %+v", got)
	}

	app, err := BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	built := BuildHTTPServer(app, HTTPServerOptions{Port: 8123})
	if app.Server != built {
		t.Fatal("expected server to be recorded on the app")
	}
	if built.Addr != "10.0.0.5:8123" && !strings.HasSuffix(built.Addr, ":8123") {
		t.Fatalf("unexpected server addr %q", built.Addr)
	}
}

func TestStartHTTPServerServesTraffic(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	handler := http.NewServeMux()
	handler.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pong"))
	})
	server := &http.Server{Addr: "127.0.0.1:" + strconv.Itoa(port), Handler: handler}

	logger := logrus.New()
	StartHTTPServer(server, logger, "")

	deadline := time.Now().Add(5 * time.Second)
	ok := false
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/ping")
		if err == nil {
			_ = resp.Body.Close()
			ok = resp.StatusCode == http.StatusOK
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ok {
		t.Fatal("server did not start serving")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestStartHTTPServerFatalOnListenError(t *testing.T) {
	logger := logrus.New()
	exited := make(chan struct{})
	// ExitFunc normally terminates the process; replacing it lets the test
	// observe the Fatalf without dying.
	logger.ExitFunc = func(int) {
		select {
		case <-exited:
		default:
			close(exited)
		}
	}

	server := &http.Server{Addr: "listen-failure:0"}
	StartHTTPServer(server, logger, "")

	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("expected Fatalf exit for listen failure")
	}
}

func TestWaitForShutdownSignalReceivesInterrupt(t *testing.T) {
	// Pre-register a handler so that stray SIGINTs are always consumed and
	// can never fall back to the default disposition (killing the process).
	guard := make(chan os.Signal, 8)
	signal.Notify(guard, syscall.SIGINT)
	defer signal.Stop(guard)

	stop := make(chan struct{})
	received := make(chan struct{})
	go func() {
		// Keep signalling until WaitForShutdownSignal has returned; the guard
		// channel absorbs everything delivered before its notify is armed.
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)
			time.Sleep(5 * time.Millisecond)
		}
	}()
	go func() {
		WaitForShutdownSignal()
		close(received)
	}()

	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("WaitForShutdownSignal did not return")
	}
	close(stop)

	// Drain any queued signals so later tests are unaffected.
	for {
		select {
		case <-guard:
			continue
		default:
		}
		break
	}
}

func TestShutdownContextDefaults(t *testing.T) {
	ctx, cancel := ShutdownContext(0)
	defer cancel()
	if dl, ok := ctx.Deadline(); !ok || time.Until(dl) > 31*time.Second {
		t.Fatalf("default shutdown deadline missing or too long: %v", dl)
	}

	ctx, cancel = ShutdownContext(time.Second)
	defer cancel()
	if dl, ok := ctx.Deadline(); !ok || time.Until(dl) > 2*time.Second {
		t.Fatalf("explicit shutdown deadline not applied: %v", dl)
	}
}

func TestAppWorkerStopErrorWrapping(t *testing.T) {
	app, err := BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	app.RegisterWorker(&stubWorker{name: "bad-stop", stopErr: errors.New("stop boom")})

	err = app.StopWorkers(context.Background())
	if err == nil || !strings.Contains(err.Error(), "bad-stop") || !strings.Contains(err.Error(), "stop boom") {
		t.Fatalf("StopWorkers() error = %v, want wrapped worker failure", err)
	}
}

func stubWorkerName(t *testing.T) string {
	t.Helper()
	return "worker-" + strings.ReplaceAll(t.Name(), "/", "-")
}
