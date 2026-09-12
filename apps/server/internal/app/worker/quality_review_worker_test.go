package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeQualityScan struct {
	mu        sync.Mutex
	scans     chan struct{}
	err       error
	processed int
}

func (f *fakeQualityScan) RunScan(context.Context) (int, error) {
	f.mu.Lock()
	n := f.processed
	err := f.err
	f.mu.Unlock()
	select {
	case f.scans <- struct{}{}:
	default:
	}
	return n, err
}

func TestQualityReviewWorkerLifecycle(t *testing.T) {
	scan := &fakeQualityScan{scans: make(chan struct{}, 8)}
	w := NewQualityReviewWorker(scan, 30*time.Millisecond, nil)
	if w.Name() != "quality-review-scan" {
		t.Fatalf("name = %s", w.Name())
	}
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-scan.scans:
	case <-time.After(2 * time.Second):
		t.Fatal("initial scan did not run")
	}
	// 周期性再扫
	select {
	case <-scan.scans:
	case <-time.After(2 * time.Second):
		t.Fatal("periodic scan did not run")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestQualityReviewWorkerNilServiceNoop(t *testing.T) {
	w := NewQualityReviewWorker(nil, time.Millisecond, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() with nil service must noop: %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestQualityReviewWorkerScanErrorLoggedNotFatal(t *testing.T) {
	scan := &fakeQualityScan{scans: make(chan struct{}, 8), err: errors.New("db down")}
	w := NewQualityReviewWorker(scan, 20*time.Millisecond, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	<-scan.scans
	<-scan.scans // 错误后循环必须继续
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}
