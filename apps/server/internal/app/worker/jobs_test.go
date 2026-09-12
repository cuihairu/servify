package worker

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/config"
	webhookapp "servify/apps/server/internal/modules/webhook/application"
	"servify/apps/server/internal/services"
)

type fakeLoop struct {
	started chan struct{}
	stopped chan struct{}
}

func (f *fakeLoop) run(ctx context.Context) {
	close(f.started)
	<-ctx.Done()
	close(f.stopped)
}

type fakeStatisticsService struct {
	loop *fakeLoop
}

func (f *fakeStatisticsService) StartDailyStatsWorkerContext(ctx context.Context, interval time.Duration) {
	f.loop.run(ctx)
}

type fakeSLAService struct {
	loop *fakeLoop
}

func (f *fakeSLAService) StartSLAMonitor(ctx context.Context, interval time.Duration) {
	f.loop.run(ctx)
}

type fakeAuditRetentionService struct {
	calls chan struct{}
}

func (f *fakeAuditRetentionService) Cleanup(ctx context.Context, now time.Time) (int64, error) {
	select {
	case f.calls <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return 0, ctx.Err()
}

type fakeRevokedTokenRetentionService struct {
	calls chan struct{}
}

func (f *fakeRevokedTokenRetentionService) Cleanup(ctx context.Context, now time.Time) (int64, error) {
	select {
	case f.calls <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return 0, ctx.Err()
}

type fakeRuntimeWorkerDependencies struct {
	statistics *services.StatisticsService
	sla        *services.SLAService
}

func (f *fakeRuntimeWorkerDependencies) StatisticsServiceForWorker() *services.StatisticsService {
	return f.statistics
}

func (f *fakeRuntimeWorkerDependencies) SLAServiceForWorker() *services.SLAService {
	return f.sla
}

func (f *fakeRuntimeWorkerDependencies) WebhookDeliveryForWorker() webhookapp.Processor {
	return nil
}

func TestStatisticsWorkerLifecycle(t *testing.T) {
	loop := &fakeLoop{started: make(chan struct{}), stopped: make(chan struct{})}
	w := NewStatisticsWorker(&fakeStatisticsService{loop: loop}, 100*time.Millisecond, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-loop.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case <-loop.stopped:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestSLAMonitorWorkerLifecycle(t *testing.T) {
	loop := &fakeLoop{started: make(chan struct{}), stopped: make(chan struct{})}
	w := NewSLAMonitorWorker(&fakeSLAService{loop: loop}, 100*time.Millisecond, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-loop.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case <-loop.stopped:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestAuditCleanupWorkerLifecycle(t *testing.T) {
	calls := make(chan struct{}, 1)
	w := NewAuditCleanupWorker(&fakeAuditRetentionService{calls: calls}, 100*time.Millisecond, nil)
	w.(*AuditCleanupWorker).now = time.Now
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("worker did not execute cleanup")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRevokedTokenCleanupWorkerLifecycle(t *testing.T) {
	calls := make(chan struct{}, 1)
	w := NewRevokedTokenCleanupWorker(&fakeRevokedTokenRetentionService{calls: calls}, 100*time.Millisecond, nil)
	w.(*RevokedTokenCleanupWorker).now = time.Now
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("worker did not execute cleanup")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRegisterDefaultWorkers(t *testing.T) {
	app, err := bootstrap.BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	cfg := config.GetDefaultConfig()
	cfg.Security.Audit.Enabled = false
	cfg.Security.TokenRevocation.Enabled = false

	RegisterDefaultWorkers(app, cfg, nil, &fakeRuntimeWorkerDependencies{})

	if len(app.Workers) != 2 {
		t.Fatalf("expected 2 workers, got %d", len(app.Workers))
	}
}

func TestRegisterDefaultWorkersWithNilArgs(t *testing.T) {
	RegisterDefaultWorkers(nil, nil, nil, nil)
}
