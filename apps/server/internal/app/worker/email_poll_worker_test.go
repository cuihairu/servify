package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/config"
)

type countingPollProcessor struct {
	calls atomic.Int64
	err   error
}

func (p *countingPollProcessor) PollOnce(ctx context.Context) (int, error) {
	p.calls.Add(1)
	return 0, p.err
}

func TestEmailPollWorkerLifecycle(t *testing.T) {
	processor := &countingPollProcessor{}
	w := NewEmailPollWorker(processor, 20*time.Millisecond, nil)
	if w.Name() != "email-poll" {
		t.Fatalf("name = %q", w.Name())
	}
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	deadline := time.After(2 * time.Second)
	for processor.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("PollOnce never called")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestEmailPollWorkerSurfacesPollErrors(t *testing.T) {
	processor := &countingPollProcessor{err: errors.New("imap down")}
	w := NewEmailPollWorker(processor, 20*time.Millisecond, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	deadline := time.After(2 * time.Second)
	for processor.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("PollOnce never called")
		case <-time.After(5 * time.Millisecond):
		}
	}
	// 错误只记日志不终止 worker：再等下一轮调用
	first := processor.calls.Load()
	for processor.calls.Load() <= first {
		select {
		case <-deadline:
			t.Fatal("worker stopped polling after error")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestEmailPollWorkerNotRegisteredWhenDisabled(t *testing.T) {
	app, err := bootstrap.BuildApp(config.GetDefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.GetDefaultConfig()
	cfg.Email.Enabled = false
	RegisterDefaultWorkers(app, cfg, nil, &fakeRuntimeWorkerDependencies{})
	for _, w := range app.Workers {
		if w.Name() == "email-poll" {
			t.Fatal("email-poll worker must not register when channel disabled")
		}
	}
}
