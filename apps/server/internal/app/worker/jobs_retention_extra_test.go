package worker

// RetentionCleanupWorker（B2-2b）分支补齐：默认间隔、重复 Start 幂等、
// 清理结果日志与错误上抛、未 Start 的 Stop、Stop 超时、注册分支接线。

import (
	"context"
	"testing"
	"time"

	customerapp "servify/apps/server/internal/modules/customer/application"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/config"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// resultRetentionService 返回带命中数的固定结果（驱动日志分支）。
type resultRetentionService struct{ result *customerapp.RetentionResult }

func (f *resultRetentionService) ScrubExpiredContent(context.Context) (*customerapp.RetentionResult, error) {
	return f.result, nil
}

// hangingRetentionService 无视 ctx 永不返回（驱动 Stop 超时分支；测试进程
// 生命周期内该 goroutine 阻塞在 job 内，loop 与 done 均无法收尾）。
type hangingRetentionService struct{ entered chan struct{} }

func (f *hangingRetentionService) ScrubExpiredContent(context.Context) (*customerapp.RetentionResult, error) {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	select {}
}

// errorRetentionService 每轮返回错误（驱动 job 错误上抛分支）。
type errorRetentionService struct{ err error }

func (f *errorRetentionService) ScrubExpiredContent(context.Context) (*customerapp.RetentionResult, error) {
	return nil, f.err
}

func TestRetentionCleanupWorkerDefaultsAndIdempotentStart(t *testing.T) {
	// interval<=0 回落 24h；logger nil 回落标准 logger。
	w := NewRetentionCleanupWorker(&resultRetentionService{}, 0, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("second Start must be no-op: %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestRetentionCleanupWorkerStopWithoutStart(t *testing.T) {
	w := NewRetentionCleanupWorker(&resultRetentionService{}, time.Hour, logrus.StandardLogger())
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop without Start must be nil: %v", err)
	}
}

func TestRetentionCleanupWorkerLogsHitCounts(t *testing.T) {
	svc := &resultRetentionService{result: &customerapp.RetentionResult{
		RetentionDays:    30,
		MessagesScrubbed: 2,
		TicketsScrubbed:  1,
		CommentsScrubbed: 3,
		FilesDeleted:     1,
	}}
	w := NewRetentionCleanupWorker(svc, 10*time.Millisecond, logrus.StandardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // 至少跑完一轮带命中的日志路径
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestRetentionCleanupWorkerJobErrorPropagates(t *testing.T) {
	svc := &errorRetentionService{err: context.Canceled}
	w := NewRetentionCleanupWorker(svc, 10*time.Millisecond, logrus.StandardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestRetentionCleanupWorkerStopTimeoutReturnsCtxErr(t *testing.T) {
	w := NewRetentionCleanupWorker(&hangingRetentionService{}, time.Hour, logrus.StandardLogger())
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// 已超时的 Stop ctx：job 挂死 → done 永不闭合 → 返回 ctx.Err()。
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)
	if err := w.Stop(stopCtx); err == nil {
		t.Fatal("Stop must surface ctx error when job hangs past deadline")
	}
}

func TestRegisterDefaultWorkersRegistersRetentionCleanup(t *testing.T) {
	app, err := bootstrap.BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatalf("BuildApp: %v", err)
	}
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	cfg := config.GetDefaultConfig()
	cfg.Security.Audit.Enabled = false
	cfg.Security.TokenRevocation.Enabled = false
	cfg.Privacy.RetentionDays = 7
	cfg.Privacy.CleanupInterval = time.Hour

	RegisterDefaultWorkers(app, cfg, db, &fakeRuntimeWorkerDependencies{})

	found := false
	for _, w := range app.Workers {
		if w.Name() == "customer-retention-cleanup" {
			found = true
		}
	}
	if !found {
		t.Fatal("retention cleanup worker must register when privacy.retention_days > 0")
	}
}
