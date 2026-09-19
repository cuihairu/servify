package worker

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/config"

	"github.com/sirupsen/logrus"
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
		"statistics-daily-stats":  NewStatisticsWorker(nil, time.Hour, nil),
		"sla-monitor":             NewSLAMonitorWorker(nil, time.Minute, nil),
		"audit-retention-cleanup": NewAuditCleanupWorker(nil, time.Hour, nil),
		"revoked-token-cleanup":   NewRevokedTokenCleanupWorker(nil, time.Hour, nil),
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
	started := make(chan struct{})
	w := NewStatisticsWorker(&fakeStatisticsService{started: started}, 100*time.Millisecond, nil)

	if err := w.Start(); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	select {
	case <-started:
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

// countingStatsService 记录 RunDailyStatsUpdate 的调用序列，供验证
// StatisticsWorker 的首轮只当日、后续轮当日+昨日的单轮语义。
type countingStatsService struct {
	calls atomic.Int64
	days  chan time.Time
}

func (c *countingStatsService) RunDailyStatsUpdate(ctx context.Context, day time.Time) error {
	c.calls.Add(1)
	if c.days != nil {
		select {
		case c.days <- day:
		default:
		}
	}
	return nil
}

func TestStatisticsWorkerFirstRunTodayThenYesterday(t *testing.T) {
	svc := &countingStatsService{days: make(chan time.Time, 8)}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	w := NewStatisticsWorker(svc, 20*time.Millisecond, logger)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() = %v", err)
	}

	// 收集三轮调用：首轮（当日）+ 后续轮（当日、昨日）。
	var seq []time.Time
	deadline := time.Now().Add(2 * time.Second)
	for len(seq) < 3 && time.Now().Before(deadline) {
		select {
		case d := <-svc.days:
			seq = append(seq, d)
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if len(seq) < 3 {
		t.Fatalf("expected 3 RunDailyStatsUpdate calls, got %d", len(seq))
	}
	if seq[0].Sub(time.Now()).Abs() > time.Hour || seq[1].Sub(time.Now()).Abs() > time.Hour {
		t.Fatalf("first run and next-run today step should both be today: %v / %v", seq[0], seq[1])
	}
	if !seq[2].Before(seq[1]) || seq[1].Sub(seq[2]) < 23*time.Hour {
		t.Fatalf("third call should be yesterday: %v vs %v", seq[2], seq[1])
	}
}

func TestRegisterDefaultWorkersWithRetentionWorkers(t *testing.T) {
	app, err := bootstrap.BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp() error = %v", err)
	}
	cfg := config.GetDefaultConfig()
	cfg.Security.Audit.Enabled = true
	cfg.Security.TokenRevocation.Enabled = true

	db := openSQLiteMemDB(t)

	RegisterDefaultWorkers(app, cfg, db, &fakeRuntimeWorkerDependencies{})
	if len(app.Workers) != 4 {
		t.Fatalf("expected 4 workers, got %d", len(app.Workers))
	}
}

type countingCleanupService struct {
	calls  atomic.Int64
	result int64
	err    error
	logger bool
}

func (c *countingCleanupService) Cleanup(ctx context.Context, now time.Time) (int64, error) {
	c.calls.Add(1)
	return c.result, c.err
}

func TestStatisticsWorkerDefaultInterval(t *testing.T) {
	w := NewStatisticsWorker(nil, 0, nil)
	if got := w.(*StatisticsWorker).interval; got != time.Hour {
		t.Fatalf("default interval = %v, want 1h", got)
	}
}

func TestStatisticsWorkerStopDuringJitter(t *testing.T) {
	w := NewStatisticsWorker(&fakeStatisticsService{started: make(chan struct{})}, time.Hour, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() during jitter error = %v", err)
	}
}

func TestStatisticsWorkerStopReturnsContextError(t *testing.T) {
	started := make(chan struct{})
	w := NewStatisticsWorker(&fakeStatisticsService{started: started}, 100*time.Millisecond, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("statistics loop did not start")
	}
	stopCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Stop(stopCtx); err == nil {
		t.Fatal("Stop() with cancelled ctx expected context error")
	}
}

func TestSLAMonitorWorkerStopVariants(t *testing.T) {
	w := NewSLAMonitorWorker(nil, time.Minute, nil)
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() before start = %v", err)
	}

	started := make(chan struct{})
	w2 := NewSLAMonitorWorker(&fakeSLAService{started: started}, 100*time.Millisecond, nil)
	if err := w2.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sla loop did not start")
	}
	stopCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w2.Stop(stopCtx); err == nil {
		t.Fatal("Stop() with cancelled ctx expected context error")
	}
}

func TestAuditCleanupWorkerRunsWithLoggerAndTicker(t *testing.T) {
	service := &countingCleanupService{result: 3}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	w := NewAuditCleanupWorker(service, 40*time.Millisecond, logger)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for service.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if service.calls.Load() < 2 {
		t.Fatalf("expected ticker loop to run twice, got %d", service.calls.Load())
	}
}

func TestAuditCleanupWorkerStopBeforeStart(t *testing.T) {
	w := NewAuditCleanupWorker(nil, time.Minute, nil)
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() before start = %v", err)
	}
}

func TestRevokedTokenCleanupWorkerRunsWithLoggerAndTicker(t *testing.T) {
	service := &countingCleanupService{result: 2}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	w := NewRevokedTokenCleanupWorker(service, 40*time.Millisecond, logger)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for service.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if service.calls.Load() < 2 {
		t.Fatalf("expected ticker loop to run twice, got %d", service.calls.Load())
	}
}

func TestRevokedTokenCleanupWorkerStopBeforeStart(t *testing.T) {
	w := NewRevokedTokenCleanupWorker(nil, time.Minute, nil)
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() before start = %v", err)
	}
}

func TestCleanupWorkerLogsErrorsWithLogger(t *testing.T) {
	service := &countingCleanupService{err: errors.New("cleanup boom")}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	w := NewAuditCleanupWorker(service, 30*time.Millisecond, logger)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for service.calls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}
