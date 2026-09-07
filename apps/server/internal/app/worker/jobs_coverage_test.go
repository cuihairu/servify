package worker

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/config"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestJitterBoundaries(t *testing.T) {
	if got := jitter(0, 0.1); got != 0 {
		t.Fatalf("jitter(0, 0.1) = %v, want 0", got)
	}
	if got := jitter(-time.Second, 0.1); got != 0 {
		t.Fatalf("jitter(negative, 0.1) = %v, want 0", got)
	}
	if got := jitter(time.Minute, 0); got != 0 {
		t.Fatalf("jitter(minute, 0) = %v, want 0", got)
	}
	if got := jitter(time.Minute, -1); got != 0 {
		t.Fatalf("jitter(minute, -1) = %v, want 0", got)
	}

	limit := time.Duration(float64(time.Minute) * 0.1)
	for i := 0; i < 50; i++ {
		got := jitter(time.Minute, 0.1)
		if got < 0 || got >= limit {
			t.Fatalf("jitter(minute, 0.1) = %v, want within [0,%v)", got, limit)
		}
	}
}

func TestWorkerNames(t *testing.T) {
	names := map[string]bootstrap.Worker{
		"statistics-daily-stats":    NewStatisticsWorker(nil, time.Hour, nil),
		"sla-monitor":               NewSLAMonitorWorker(nil, time.Minute, nil),
		"audit-retention-cleanup":   NewAuditCleanupWorker(nil, time.Hour, nil),
		"revoked-token-cleanup":     NewRevokedTokenCleanupWorker(nil, time.Hour, nil),
	}
	for want, w := range names {
		if got := w.Name(); got != want {
			t.Fatalf("Name() = %q, want %q", got, want)
		}
	}
}

func TestStatisticsWorkerStartWithoutService(t *testing.T) {
	w := NewStatisticsWorker(nil, time.Hour, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() without service error = %v", err)
	}
}

func TestStatisticsWorkerStopBeforeStart(t *testing.T) {
	w := NewStatisticsWorker(nil, time.Hour, nil)
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() before start error = %v", err)
	}
}

func TestStatisticsWorkerStartTwiceRunsSingleLoop(t *testing.T) {
	loop := &fakeLoop{started: make(chan struct{}), stopped: make(chan struct{})}
	w := NewStatisticsWorker(&fakeStatisticsService{loop: loop}, 100*time.Millisecond, nil)

	if err := w.Start(); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	select {
	case <-loop.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	if err := w.Start(); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	// Stop again is a no-op.
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
}

func TestSLAMonitorWorkerDefaults(t *testing.T) {
	w := NewSLAMonitorWorker(nil, 0, nil)
	if got := w.(*SLAMonitorWorker).interval; got != 5*time.Minute {
		t.Fatalf("default interval = %v, want 5m", got)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("Start() without service error = %v", err)
	}
}

func TestAuditCleanupWorkerDefaults(t *testing.T) {
	w := NewAuditCleanupWorker(nil, 0, nil)
	if got := w.(*AuditCleanupWorker).interval; got != 24*time.Hour {
		t.Fatalf("default interval = %v, want 24h", got)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("Start() without service error = %v", err)
	}
}

func TestRevokedTokenCleanupWorkerDefaults(t *testing.T) {
	w := NewRevokedTokenCleanupWorker(nil, 0, nil)
	if got := w.(*RevokedTokenCleanupWorker).interval; got != 24*time.Hour {
		t.Fatalf("default interval = %v, want 24h", got)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("Start() without service error = %v", err)
	}
}

func TestAuditCleanupWorkerStopReturnsContextError(t *testing.T) {
	started := make(chan struct{})
	w := NewAuditCleanupWorker(&slowExitAuditService{started}, 100*time.Millisecond, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not start")
	}

	stopCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Stop(stopCtx); err == nil {
		t.Fatal("Stop() with cancelled ctx expected context error")
	}
}

// slowExitAuditService blocks until ctx is done, then lingers briefly so that
// Stop's done channel is not closed before the caller's ctx fires.
type slowExitAuditService struct {
	started chan struct{}
}

func (f *slowExitAuditService) Cleanup(ctx context.Context, now time.Time) (int64, error) {
	close(f.started)
	<-ctx.Done()
	time.Sleep(300 * time.Millisecond)
	return 0, ctx.Err()
}

func TestRevokedTokenCleanupWorkerStopReturnsContextError(t *testing.T) {
	started := make(chan struct{})
	w := NewRevokedTokenCleanupWorker(&slowExitTokenService{started}, 100*time.Millisecond, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not start")
	}

	stopCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Stop(stopCtx); err == nil {
		t.Fatal("Stop() with cancelled ctx expected context error")
	}
}

// slowExitTokenService blocks until ctx is done, then lingers briefly so that
// Stop's done channel is not closed before the caller's ctx fires.
type slowExitTokenService struct {
	started chan struct{}
}

func (f *slowExitTokenService) Cleanup(ctx context.Context, now time.Time) (int64, error) {
	close(f.started)
	<-ctx.Done()
	time.Sleep(300 * time.Millisecond)
	return 0, ctx.Err()
}

func TestRegisterDefaultWorkersWithRetentionWorkers(t *testing.T) {
	app, err := bootstrap.BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	cfg := config.GetDefaultConfig()
	cfg.Security.Audit.Enabled = true
	cfg.Security.TokenRevocation.Enabled = true

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	RegisterDefaultWorkers(app, cfg, db, &fakeRuntimeWorkerDependencies{})
	if len(app.Workers) != 4 {
		t.Fatalf("expected 4 workers, got %d", len(app.Workers))
	}
}
