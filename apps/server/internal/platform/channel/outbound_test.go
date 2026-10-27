package channel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDefaultSendRetryPolicy(t *testing.T) {
	p := DefaultSendRetryPolicy()
	if p.MinBackoff != time.Second || p.MaxBackoff != 5*time.Minute ||
		p.MaxAttempts != 10 || p.MaxDuration != 24*time.Hour || p.Sleep == nil {
		t.Errorf("default policy = %+v, want contract budget 1s/5m/10/24h with a sleep", p)
	}
}

func TestRetrySendSucceedsOnFirstAttempt(t *testing.T) {
	calls := 0
	id, err := RetrySend(context.Background(), SendRetryPolicy{}, "key-1", func(context.Context) (string, error) {
		calls++
		return "m1", nil
	})
	if err != nil || id != "m1" || calls != 1 {
		t.Errorf("first-attempt send = %q, %v (%d calls); want m1 in one call", id, err, calls)
	}
}

func TestRetrySendNonAdapterErrorStops(t *testing.T) {
	calls := 0
	_, err := RetrySend(context.Background(), SendRetryPolicy{
		MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxAttempts: 10,
		Sleep: func(context.Context, time.Duration) error { return nil },
	}, "key-2", func(context.Context) (string, error) {
		calls++
		return "", errors.New("store boom")
	})
	if err == nil || !strings.Contains(err.Error(), "key-2") {
		t.Fatalf("err = %v, want wrapped with anchor key-2", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (store failure is never retried)", calls)
	}
}

func TestRetrySendNonRetryableAdapterErrorStops(t *testing.T) {
	calls := 0
	_, err := RetrySend(context.Background(), SendRetryPolicy{
		MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxAttempts: 10,
		Sleep: func(context.Context, time.Duration) error { return nil },
	}, "key-3", func(context.Context) (string, error) {
		calls++
		return "", &AdapterError{Kind: ErrPermission, Status: 403, Op: "send", Detail: "bot kicked"}
	})
	if err == nil || !strings.Contains(err.Error(), "key-3") {
		t.Fatalf("err = %v, want wrapped with anchor key-3", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (permission refusal is never retried)", calls)
	}
}

func TestRetrySendTransportUncertaintyStops(t *testing.T) {
	calls := 0
	_, err := RetrySend(context.Background(), SendRetryPolicy{
		MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxAttempts: 10,
		Sleep: func(context.Context, time.Duration) error { return nil },
	}, "key-4", func(context.Context) (string, error) {
		calls++
		return "", &AdapterError{Kind: ErrTransient, Status: 0, Op: "send", Detail: "eof"}
	})
	if err == nil || !strings.Contains(err.Error(), "key-4") {
		t.Fatalf("err = %v, want wrapped with anchor key-4", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (transport uncertainty is never blindly retried)", calls)
	}
}

func TestRetrySendHonorsRetryAfterCapped(t *testing.T) {
	var waits []time.Duration
	calls := 0
	id, err := RetrySend(context.Background(), SendRetryPolicy{
		MaxAttempts: 2, MaxBackoff: 2 * time.Second,
		Sleep: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil },
	}, "key-5", func(context.Context) (string, error) {
		calls++
		if calls == 1 {
			return "", &AdapterError{Kind: ErrRateLimited, Status: 429, Op: "send", RetryAfter: time.Hour}
		}
		return "m2", nil
	})
	if err != nil || id != "m2" || calls != 2 {
		t.Fatalf("retried send = %q, %v (%d calls); want m2 in two calls", id, err, calls)
	}
	if len(waits) != 1 || waits[0] != 2*time.Second {
		t.Errorf("waits = %v, want exactly [2s] (server Retry-After capped at MaxBackoff)", waits)
	}
}

func TestRetrySendBackoffJitterWhenNoRetryAfter(t *testing.T) {
	var waits []time.Duration
	calls := 0
	id, err := RetrySend(context.Background(), SendRetryPolicy{
		MinBackoff: 2 * time.Second, MaxBackoff: 4 * time.Second, MaxAttempts: 2,
		Sleep: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil },
	}, "key-6", func(context.Context) (string, error) {
		calls++
		if calls == 1 {
			return "", &AdapterError{Kind: ErrRateLimited, Status: 429, Op: "send"}
		}
		return "m3", nil
	})
	if err != nil || id != "m3" || calls != 2 {
		t.Fatalf("backoff retry = %q, %v (%d calls); want m3 in two calls", id, err, calls)
	}
	if len(waits) != 1 || waits[0] < time.Second || waits[0] > 2*time.Second {
		t.Errorf("waits = %v, want full-jitter inside [1s,2s]", waits)
	}
}

func TestRetrySendBudgetStopsAtAttempts(t *testing.T) {
	var waits []time.Duration
	calls := 0
	_, err := RetrySend(context.Background(), SendRetryPolicy{
		MaxAttempts: 3, MinBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond,
		Sleep: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil },
	}, "key-7", func(context.Context) (string, error) {
		calls++
		return "", &AdapterError{Kind: ErrRateLimited, Status: 429, Op: "send"}
	})
	if err == nil || !strings.Contains(err.Error(), "after 3 attempts") || !strings.Contains(err.Error(), "key-7") {
		t.Fatalf("err = %v, want attempt count and anchor key-7", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (budget is attempts, no extra call)", calls)
	}
	if len(waits) != 2 {
		t.Errorf("waits = %v, want 2 pauses before the final refusal", waits)
	}
}

func TestRetrySendSleepCancellationPropagates(t *testing.T) {
	calls := 0
	_, err := RetrySend(context.Background(), SendRetryPolicy{
		MaxAttempts: 2, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
		Sleep: func(context.Context, time.Duration) error { return context.Canceled },
	}, "key-8", func(context.Context) (string, error) {
		calls++
		return "", &AdapterError{Kind: ErrRateLimited, Status: 429, Op: "send"}
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (cancelled backoff aborts the streak)", calls)
	}
}

func TestOutboundSendResultMapsContract(t *testing.T) {
	if got := OutboundSendResult(&AdapterError{Kind: ErrTransient, Status: 0, Op: "send"}); got.Status != SendResultUnknown {
		t.Errorf("transport uncertainty = %+v, want unknown", got)
	}
	if got := OutboundSendResult(&AdapterError{Kind: ErrPermission, Status: 403, Op: "send"}); got.Status != SendResultRejected {
		t.Errorf("platform refusal = %+v, want rejected", got)
	}
	if got := OutboundSendResult(errors.New("store boom")); got.Status != SendResultUnknown {
		t.Errorf("non-adapter failure = %+v, want unknown", got)
	}
}

func TestJitteredWindows(t *testing.T) {
	if got := Jittered(0); got != 0 {
		t.Errorf("jittered(0) = %v, want 0", got)
	}
	if got := Jittered(time.Nanosecond); got != time.Nanosecond {
		t.Errorf("jittered(1ns) = %v, want 1ns", got)
	}
	if got := Jittered(time.Second); got < 500*time.Millisecond || got > time.Second {
		t.Errorf("jittered(1s) = %v, want inside [500ms,1s]", got)
	}
}

func TestSleepContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled sleep = %v, want context.Canceled", err)
	}
	if err := SleepContext(context.Background(), 0); err != nil {
		t.Errorf("non-positive sleep = %v, want nil", err)
	}
	if err := SleepContext(context.Background(), time.Nanosecond); err != nil {
		t.Errorf("timer sleep = %v, want nil", err)
	}
}
