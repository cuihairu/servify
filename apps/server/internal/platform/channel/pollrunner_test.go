package channel

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// runnerSleeper turns every pause the runner asks for into a channel value, so
// tests observe backoff/jitter/Retry-After policy in order without real
// waiting. Sleeping answers with ctx.Err once the test cancels, which is what
// stops the loop.
type runnerSleeper struct {
	pauses chan time.Duration
	cancel context.CancelFunc
	ctx    context.Context
}

func newRunnerSleeper() *runnerSleeper {
	ctx, cancel := context.WithCancel(context.Background())
	return &runnerSleeper{pauses: make(chan time.Duration, 32), cancel: cancel, ctx: ctx}
}

func (s *runnerSleeper) sleep(d time.Duration) error {
	s.pauses <- d
	return s.ctx.Err()
}

// expect reads the next pause and requires it inside [low, 2*low] — the full
// jitter window half(half..whole). Exact waits are asserted with expectExact.
func (s *runnerSleeper) expect(t *testing.T, low time.Duration) time.Duration {
	t.Helper()
	select {
	case got := <-s.pauses:
		if got < low || got > 2*low {
			t.Errorf("pause = %v, want in [%v, %v]", got, low, 2*low)
		}
		return got
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for a pause")
		return 0
	}
}

func (s *runnerSleeper) expectExact(t *testing.T, want time.Duration) {
	t.Helper()
	select {
	case got := <-s.pauses:
		if got != want {
			t.Errorf("pause = %v, want exactly %v", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for a pause")
	}
}

func (s *runnerSleeper) expectNone(t *testing.T) {
	t.Helper()
	select {
	case got := <-s.pauses:
		t.Errorf("unexpected pause %v", got)
	default:
	}
}

// scriptedPoller replays one scripted result per PollOnce call and then keeps
// draining empty forever. Outcomes are contract results (success with a
// processed count, AdapterError, gate error, plain store error), so the policy
// under test is exercised directly without an HTTP platform.
type scriptedPoller struct {
	mu     sync.Mutex
	script []pollOutcome
	idx    int
	polls  int
}

type pollOutcome struct {
	n   int
	err error
}

func okPoll(n int) pollOutcome     { return pollOutcome{n: n} }
func failPoll(e error) pollOutcome { return pollOutcome{err: e} }

func (s *scriptedPoller) PollOnce(context.Context, time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polls++
	if s.idx < len(s.script) {
		i := s.idx
		s.idx++
		return s.script[i].n, s.script[i].err
	}
	return 0, nil // drained
}

func (s *scriptedPoller) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.polls
}

func newTestPolicyRunner(sl *runnerSleeper, outcomes ...pollOutcome) (*Runner, *scriptedPoller) {
	p := &scriptedPoller{script: outcomes}
	r := NewRunner(p)
	r.LongPoll = 0
	r.Sleep = func(_ context.Context, d time.Duration) error { return sl.sleep(d) }
	return r, p
}

func TestRunnerHonorsRetryAfter(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()
	runner, _ := newTestPolicyRunner(sl,
		failPoll(&AdapterError{Kind: ErrRateLimited, Status: http.StatusTooManyRequests, RetryAfter: 7 * time.Second}),
		okPoll(1))
	go func() { _ = runner.Run(sl.ctx) }()
	sl.expectExact(t, 7*time.Second) // server-provided wait: no jitter
	sl.cancel()
}

func TestRunnerRetryAfterClampedToMaxBackoff(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()
	runner, _ := newTestPolicyRunner(sl,
		failPoll(&AdapterError{Kind: ErrRateLimited, Status: http.StatusTooManyRequests, RetryAfter: 3600 * time.Second}))
	runner.MaxBackoff = 30 * time.Second
	go func() { _ = runner.Run(sl.ctx) }()
	sl.expectExact(t, 30*time.Second)
	sl.cancel()
}

func TestRunnerRateLimitWithoutHeaderUsesBackoff(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()
	runner, _ := newTestPolicyRunner(sl,
		failPoll(&AdapterError{Kind: ErrRateLimited, Status: http.StatusTooManyRequests}))
	runner.MinBackoff = time.Second
	go func() { _ = runner.Run(sl.ctx) }()
	sl.expect(t, 500*time.Millisecond)
	sl.cancel()
}

func TestRunnerRetriesTransientWithBackoff(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()
	runner, _ := newTestPolicyRunner(sl,
		failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError}),
		failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError}))
	runner.MinBackoff = time.Second
	go func() { _ = runner.Run(sl.ctx) }()
	sl.expect(t, 500*time.Millisecond) // window [0.5s, 1s]
	sl.expect(t, time.Second)          // doubled: [1s, 2s]
	sl.cancel()
}

func TestRunnerBackoffResetsAfterSuccess(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()
	runner, _ := newTestPolicyRunner(sl,
		failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError}),
		okPoll(1),
		failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError}))
	runner.MinBackoff = time.Second
	runner.IdleWait = 3 * time.Second
	go func() { _ = runner.Run(sl.ctx) }()
	sl.expect(t, 500*time.Millisecond) // 500 → backoff
	sl.expect(t, 500*time.Millisecond) // 500 again → window reset, not escalated
	sl.expectExact(t, 3*time.Second)   // empty drain → idle pause
	sl.cancel()
}

func TestRunnerBackoffCapsAtMax(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()
	runner, _ := newTestPolicyRunner(sl,
		failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError}),
		failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError}),
		failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError}))
	runner.MinBackoff = time.Second
	runner.MaxBackoff = 2 * time.Second
	go func() { _ = runner.Run(sl.ctx) }()
	sl.expect(t, 500*time.Millisecond) // [0.5s, 1s]
	sl.expect(t, time.Second)          // [1s, 2s]
	sl.expect(t, time.Second)          // capped: never exceeds [1s, 2s]
	sl.cancel()
}

func TestRunnerStopsOnHardErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		kind   ErrorKind
	}{
		{http.StatusUnauthorized, ErrAuth},
		{http.StatusForbidden, ErrPermission},
		{http.StatusNotFound, ErrPermanent},
		{http.StatusBadRequest, ErrInvalid},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			sl := newRunnerSleeper()
			defer sl.cancel()
			runner, p := newTestPolicyRunner(sl,
				failPoll(&AdapterError{Kind: tc.kind, Status: tc.status}))
			err := runner.Run(sl.ctx) // must return on its own, no retries
			var apiErr *AdapterError
			if !errors.As(err, &apiErr) || apiErr.Kind != tc.kind {
				t.Fatalf("run = %v, want %s AdapterError", err, tc.kind)
			}
			if got := p.count(); got != 1 {
				t.Errorf("polls = %d, want 1 (hard errors stop the loop)", got)
			}
			sl.expectNone(t)
		})
	}
}

func TestRunnerStopsWhenIngestionDisabled(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()
	runner, p := newTestPolicyRunner(sl, failPoll(ErrIngestionDisabled))
	if err := runner.Run(sl.ctx); !errors.Is(err, ErrIngestionDisabled) {
		t.Errorf("run = %v, want ErrIngestionDisabled", err)
	}
	if got := p.count(); got != 1 {
		t.Errorf("polls = %d, want 1 (gate stops before the next pull)", got)
	}
	sl.expectNone(t)
}

func TestRunnerSurfacesStoreErrorWithoutRetry(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()
	runner, _ := newTestPolicyRunner(sl, failPoll(errors.New("boom")))
	if err := runner.Run(sl.ctx); err == nil || errors.Is(err, context.Canceled) {
		t.Errorf("run = %v, want store error surfaced (store failures are not retried)", err)
	}
	sl.expectNone(t)
}

func TestRunnerCancelInsideSleepStopsLoop(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()
	runner, _ := newTestPolicyRunner(sl,
		failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError}))
	runner.Sleep = func(_ context.Context, d time.Duration) error {
		_ = sl.sleep(d)
		sl.cancel() // the first pause itself stops the loop
		return sl.ctx.Err()
	}
	if err := runner.Run(sl.ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("run = %v, want context.Canceled", err)
	}
	sl.expect(t, 500*time.Millisecond)
}

func TestRunnerJitteredWindows(t *testing.T) {
	r := &Runner{}
	for i := 0; i < 100; i++ {
		got := r.jittered(time.Second)
		if got < 500*time.Millisecond || got > time.Second {
			t.Fatalf("jittered(1s) = %v, want in [500ms, 1s]", got)
		}
	}
	// windows too small to halve pass through unchanged
	if got := r.jittered(0); got != 0 {
		t.Errorf("jittered(0) = %v, want 0", got)
	}
	if got := r.jittered(time.Nanosecond); got != time.Nanosecond {
		t.Errorf("jittered(1ns) = %v, want 1ns", got)
	}
}

func TestSleepCtxHonorsDeadlineAndCancel(t *testing.T) {
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Errorf("short sleep = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled sleep = %v, want context.Canceled", err)
	}
	if err := sleepCtx(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("zero sleep on canceled ctx = %v, want context.Canceled", err)
	}
}

func TestRunnerIdlePauseHonorsCancel(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()
	runner, _ := newTestPolicyRunner(sl) // empty script: every poll drains empty
	runner.IdleWait = 2 * time.Second
	pauses := 0
	runner.Sleep = func(_ context.Context, d time.Duration) error {
		_ = sl.sleep(d)
		pauses++
		if pauses == 2 {
			sl.cancel() // stop from inside an idle pause, not a backoff window
		}
		return sl.ctx.Err()
	}
	done := make(chan error, 1)
	go func() { done <- runner.Run(sl.ctx) }()
	sl.expectExact(t, 2*time.Second) // first idle pause observed
	sl.expectExact(t, 2*time.Second) // loop keeps draining after an idle pause
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("run = %v, want context.Canceled", err)
	}
}

func TestRunnerReportsCancellationFromStoreError(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel() // store errors racing a shutdown must read as cancellation, not db failure

	runner, _ := newTestPolicyRunner(sl, failPoll(errors.New("boom")))
	if err := runner.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("run = %v, want context.Canceled", err)
	}
	sl.expectNone(t)
}

func TestRunnerDeadLettersAfterMaxAttempts(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()
	runner, p := newTestPolicyRunner(sl,
		failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError}),
		failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError}),
		failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError}))
	runner.MinBackoff = time.Millisecond
	runner.MaxAttempts = 3
	err := runner.Run(sl.ctx)
	var dl *DeadLetterError
	if !errors.As(err, &dl) {
		t.Fatalf("run = %v, want DeadLetterError", err)
	}
	if dl.Reason != "attempts" || dl.Attempts != 3 || dl.Kind != ErrTransient || dl.Status != http.StatusInternalServerError {
		t.Errorf("dead letter = %+v", dl)
	}
	if got := p.count(); got != 3 {
		t.Errorf("polls = %d, want 3 (the budgetth attempt is the last)", got)
	}
	sl.expect(t, 500*time.Microsecond) // jittered(1ms) ∈ [500µs, 1ms]
	sl.expect(t, time.Millisecond)     // jittered(2ms) ∈ [1ms, 2ms]
	sl.expectNone(t)
}

func TestRunnerDeadLettersAfterTimeBudget(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()

	base := time.Unix(1697000000, 0)
	clock := 0
	runner, p := newTestPolicyRunner(sl,
		failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError}),
		failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError}))
	runner.MinBackoff = time.Millisecond
	runner.MaxAttempts = 100 // attempt budget off the table; the clock decides
	runner.MaxDuration = time.Hour
	runner.Now = func() time.Time {
		clock++
		if clock <= 2 { // streak start, then a deadline check still in budget
			return base
		}
		return base.Add(2 * time.Hour) // the next check lands past the budget
	}
	err := runner.Run(sl.ctx)
	var dl *DeadLetterError
	if !errors.As(err, &dl) {
		t.Fatalf("run = %v, want DeadLetterError", err)
	}
	if dl.Reason != "budget" || dl.Attempts != 2 {
		t.Errorf("dead letter = %+v", dl)
	}
	if got := p.count(); got != 2 {
		t.Errorf("polls = %d, want 2", got)
	}
	sl.expect(t, 500*time.Microsecond)
	sl.expectNone(t)
}

func TestRunnerAttemptsResetAfterSuccess(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()
	transient := failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError})
	runner, p := newTestPolicyRunner(sl,
		transient, okPoll(1), transient, transient)
	runner.MinBackoff = time.Millisecond
	runner.MaxAttempts = 2
	err := runner.Run(sl.ctx)
	var dl *DeadLetterError
	if !errors.As(err, &dl) || dl.Reason != "attempts" || dl.Attempts != 2 {
		t.Fatalf("run = %v, want attempts DeadLetterError after a fresh streak", err)
	}
	// the second streak ran to its own budget (poll 2 succeeded in between):
	// without the reset the loop would have stopped at poll 3.
	if got := p.count(); got != 4 {
		t.Errorf("polls = %d, want 4", got)
	}
}

func TestRunnerBudgetsCanBeDisabled(t *testing.T) {
	sl := newRunnerSleeper()
	defer sl.cancel()
	transient := failPoll(&AdapterError{Kind: ErrTransient, Status: http.StatusInternalServerError})
	runner, _ := newTestPolicyRunner(sl, transient, transient, transient)
	runner.MinBackoff = time.Millisecond
	runner.MaxAttempts = 0 // no attempt budget
	runner.MaxDuration = 0 // no time budget
	done := make(chan error, 1)
	go func() { done <- runner.Run(sl.ctx) }()
	sl.expect(t, 500*time.Microsecond) // jittered(1ms)
	sl.expect(t, time.Millisecond)     // jittered(2ms)
	sl.expect(t, 2*time.Millisecond)   // jittered(4ms)
	sl.cancel()                        // still looping after 3 failures: only the stop signal ends it
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("run = %v, want context.Canceled", err)
	}
}

func TestDeadLetterErrorTextCarriesNoContent(t *testing.T) {
	dl := &DeadLetterError{Reason: "attempts", Kind: ErrTransient, Status: 500, Attempts: 10}
	msg := dl.Error()
	for _, want := range []string{"attempts", "10", "transient", "500"} {
		if !strings.Contains(msg, want) {
			t.Errorf("dead letter text %q missing %q", msg, want)
		}
	}
	if (&Runner{}).now().IsZero() {
		t.Error("zero Runner must fall back to the wall clock")
	}
}

func TestAdapterErrorTextCarriesClassNotSecrets(t *testing.T) {
	e := &AdapterError{Kind: ErrRateLimited, Status: 429, Op: "get_updates", Detail: "too many requests"}
	msg := e.Error()
	for _, want := range []string{"get_updates", "429", "rate_limited"} {
		if !strings.Contains(msg, want) {
			t.Errorf("adapter error text %q missing %q", msg, want)
		}
	}
}

func TestNewRunnerPinsContractBudget(t *testing.T) {
	r := NewRunner(nil)
	if r.LongPoll != 25*time.Second {
		t.Errorf("long poll = %v, want 25s under the 30s server cap", r.LongPoll)
	}
	if r.MinBackoff != time.Second {
		t.Errorf("base backoff = %v, want 1s (contract: starting at 1s)", r.MinBackoff)
	}
	if r.MaxBackoff != 5*time.Minute {
		t.Errorf("backoff cap = %v, want 5m (contract: capped at 5m)", r.MaxBackoff)
	}
	if r.MaxAttempts != 10 {
		t.Errorf("max attempts = %d, want 10 (contract: stop after 10 attempts)", r.MaxAttempts)
	}
	if r.MaxDuration != 24*time.Hour {
		t.Errorf("max duration = %v, want 24h (contract: or 24h)", r.MaxDuration)
	}
	if r.Sleep == nil || r.Now == nil {
		t.Error("runner must ship a sleep and a clock")
	}
}

// StatusKind is shared vocabulary: every adapter classifies through this one
// table, so the table itself is pinned here (per-package attribution — the
// memex/telegram client tests exercise it through their own HTTP faces, but
// this is where the statements live).
func TestStatusKindCoversContractTable(t *testing.T) {
	cases := []struct {
		status int
		want   ErrorKind
	}{
		{http.StatusUnauthorized, ErrAuth},
		{http.StatusForbidden, ErrPermission},
		{http.StatusTooManyRequests, ErrRateLimited},
		{http.StatusNotFound, ErrPermanent},
		{http.StatusInternalServerError, ErrTransient},
		{http.StatusBadGateway, ErrTransient},
		{http.StatusBadRequest, ErrInvalid},
		{http.StatusTeapot, ErrInvalid},
		{0, ErrInvalid},
	}
	for _, tc := range cases {
		if got := StatusKind(tc.status); got != tc.want {
			t.Errorf("StatusKind(%d) = %s, want %s", tc.status, got, tc.want)
		}
	}
}
