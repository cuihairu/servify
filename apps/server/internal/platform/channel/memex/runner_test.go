package memex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"servify/apps/server/internal/platform/channel"
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

func newTestRunner(t *testing.T, sink *channel.InboxStore, caps *channel.CapabilityStore, url string, sl *runnerSleeper) *Runner {
	t.Helper()
	connector := NewConnector(NewClient(url, "tok", http.DefaultClient), caps, sink, adapterConfig())
	r := NewRunner(connector)
	r.LongPoll = 0
	r.Sleep = func(_ context.Context, d time.Duration) error { return sl.sleep(d) }
	return r
}

// scriptedPolls serves one response per pull from a script list, then drains
// with empty updates forever. Markers: __500__ answers a transient failure,
// __429_<seconds>__ answers rate-limited with a Retry-After header (0 omits
// the header), anything else is served verbatim.
type scriptedPolls struct {
	t      *testing.T
	mu     sync.Mutex
	script []string
	polls  atomic.Int64
}

func newScriptedPolls(t *testing.T, script ...string) *scriptedPolls {
	return &scriptedPolls{t: t, script: script}
}

func (s *scriptedPolls) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/bot/ack" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		s.mu.Lock()
		var body string
		if len(s.script) > 0 {
			body = s.script[0]
			s.script = s.script[1:]
		} else {
			body = `{"ok":true,"updates":[]}`
		}
		s.mu.Unlock()
		s.polls.Add(1)

		if body == "__500__" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"ok":false,"error":"boom"}`))
			return
		}
		if strings.HasPrefix(body, "__429_") {
			secs := strings.TrimSuffix(strings.TrimPrefix(body, "__429_"), "__")
			if n, err := strconv.Atoi(secs); err == nil && n > 0 {
				w.Header().Set("Retry-After", secs)
			}
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error":"slow down"}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}
}

func updateBody(id string) string {
	return `{"ok":true,"updates":[{"msg_id":"` + id + `","from":"alice","to":"bot:echo","text":"hi","ts_ms":1697000000000}]}`
}

func TestRunnerHonorsRetryAfter(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	sl := newRunnerSleeper()
	defer sl.cancel()

	polls := newScriptedPolls(t, "__429_7__", updateBody("m1"))
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
	go func() { _ = runner.Run(sl.ctx) }()
	sl.expectExact(t, 7*time.Second) // server-provided wait: no jitter
	sl.cancel()
}

func TestRunnerRetryAfterClampedToMaxBackoff(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	sl := newRunnerSleeper()
	defer sl.cancel()

	polls := newScriptedPolls(t, "__429_3600__")
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
	runner.MaxBackoff = 30 * time.Second
	go func() { _ = runner.Run(sl.ctx) }()
	sl.expectExact(t, 30*time.Second)
	sl.cancel()
}

func TestRunnerRateLimitWithoutHeaderUsesBackoff(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	sl := newRunnerSleeper()
	defer sl.cancel()

	polls := newScriptedPolls(t, "__429_0__")
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
	runner.MinBackoff = time.Second
	go func() { _ = runner.Run(sl.ctx) }()
	sl.expect(t, 500*time.Millisecond)
	sl.cancel()
}

func TestRunnerRetriesTransientWithBackoff(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	sl := newRunnerSleeper()
	defer sl.cancel()

	polls := newScriptedPolls(t, "__500__", "__500__")
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
	runner.MinBackoff = time.Second
	go func() { _ = runner.Run(sl.ctx) }()
	sl.expect(t, 500*time.Millisecond) // window [0.5s, 1s]
	sl.expect(t, time.Second)          // doubled: [1s, 2s]
	sl.cancel()
}

func TestRunnerBackoffResetsAfterSuccess(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	sl := newRunnerSleeper()
	defer sl.cancel()

	polls := newScriptedPolls(t, "__500__", updateBody("m1"), "__500__")
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
	runner.MinBackoff = time.Second
	runner.IdleWait = 3 * time.Second
	go func() { _ = runner.Run(sl.ctx) }()
	sl.expect(t, 500*time.Millisecond) // 500 → backoff
	sl.expect(t, 500*time.Millisecond) // 500 again → window reset, not escalated
	sl.expectExact(t, 3*time.Second)   // empty drain → idle pause
	sl.cancel()
}

func TestRunnerBackoffCapsAtMax(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	sl := newRunnerSleeper()
	defer sl.cancel()

	polls := newScriptedPolls(t, "__500__", "__500__", "__500__")
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
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
			sink, caps, _ := newAdapterStores(t)
			enableIngestion(t, caps)
			sl := newRunnerSleeper()
			defer sl.cancel()

			var polls atomic.Int64
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				polls.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"ok":false,"error":"no"}`))
			}))
			defer srv.Close()

			runner := newTestRunner(t, sink, caps, srv.URL, sl)
			err := runner.Run(sl.ctx) // must return on its own, no retries
			var apiErr *AdapterError
			if !errors.As(err, &apiErr) || apiErr.Kind != tc.kind {
				t.Fatalf("run = %v, want %s AdapterError", err, tc.kind)
			}
			if polls.Load() != 1 {
				t.Errorf("polls = %d, want 1 (hard errors stop the loop)", polls.Load())
			}
			sl.expectNone(t)
		})
	}
}

func TestRunnerStopsWhenIngestionDisabled(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	sl := newRunnerSleeper()
	defer sl.cancel()

	var pulled atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pulled.Store(true)
		_, _ = w.Write([]byte(`{"ok":true,"updates":[]}`))
	}))
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
	if err := runner.Run(sl.ctx); !errors.Is(err, ErrIngestionDisabled) {
		t.Errorf("run = %v, want ErrIngestionDisabled", err)
	}
	if pulled.Load() {
		t.Error("disabled ingestion must not reach the platform")
	}
	sl.expectNone(t)
}

func TestRunnerSurfacesSinkErrorWithoutRetry(t *testing.T) {
	sink, caps, db := newAdapterStores(t)
	enableIngestion(t, caps)
	if err := db.Callback().Create().Before("gorm:create").Register("runner_sink_error",
		func(tx *gorm.DB) { tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	sl := newRunnerSleeper()
	defer sl.cancel()

	polls := newScriptedPolls(t, updateBody("m1"))
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
	if err := runner.Run(sl.ctx); err == nil || errors.Is(err, context.Canceled) {
		t.Errorf("run = %v, want sink error surfaced (store failures are not retried)", err)
	}
	sl.expectNone(t)
}

func TestRunnerCancelInsideSleepStopsLoop(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	sl := newRunnerSleeper()
	defer sl.cancel()

	polls := newScriptedPolls(t, "__500__")
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
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
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	sl := newRunnerSleeper()
	defer sl.cancel()

	polls := newScriptedPolls(t) // empty script: every poll drains empty
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
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
	sink, caps, db := newAdapterStores(t)
	enableIngestion(t, caps)
	if err := db.Callback().Query().Before("gorm:query").Register("runner_cancel_probe",
		func(tx *gorm.DB) { tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	sl := newRunnerSleeper()
	defer sl.cancel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel() // store errors racing a shutdown must read as cancellation, not db failure

	polls := newScriptedPolls(t)
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
	if err := runner.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("run = %v, want context.Canceled", err)
	}
	sl.expectNone(t)
}

func TestRunnerDeadLettersAfterMaxAttempts(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	sl := newRunnerSleeper()
	defer sl.cancel()

	polls := newScriptedPolls(t, "__500__", "__500__", "__500__")
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
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
	if got := polls.polls.Load(); got != 3 {
		t.Errorf("polls = %d, want 3 (the budgetth attempt is the last)", got)
	}
	sl.expect(t, 500*time.Microsecond) // jittered(1ms) ∈ [500µs, 1ms]
	sl.expect(t, time.Millisecond)     // jittered(2ms) ∈ [1ms, 2ms]
	sl.expectNone(t)
}

func TestRunnerDeadLettersAfterTimeBudget(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	sl := newRunnerSleeper()
	defer sl.cancel()

	polls := newScriptedPolls(t, "__500__", "__500__")
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	base := time.Unix(1697000000, 0)
	clock := 0
	runner := newTestRunner(t, sink, caps, srv.URL, sl)
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
	if got := polls.polls.Load(); got != 2 {
		t.Errorf("polls = %d, want 2", got)
	}
	sl.expect(t, 500*time.Microsecond)
	sl.expectNone(t)
}

func TestRunnerAttemptsResetAfterSuccess(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	sl := newRunnerSleeper()
	defer sl.cancel()

	script := []string{"__500__", updateBody("m1"), "__500__", "__500__"}
	polls := newScriptedPolls(t, script...)
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
	runner.MinBackoff = time.Millisecond
	runner.MaxAttempts = 2
	err := runner.Run(sl.ctx)
	var dl *DeadLetterError
	if !errors.As(err, &dl) || dl.Reason != "attempts" || dl.Attempts != 2 {
		t.Fatalf("run = %v, want attempts DeadLetterError after a fresh streak", err)
	}
	// the second streak ran to its own budget (poll 2 succeeded in between):
	// without the reset the loop would have stopped at poll 3.
	if got := polls.polls.Load(); got != 4 {
		t.Errorf("polls = %d, want 4", got)
	}
}

func TestRunnerBudgetsCanBeDisabled(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	sl := newRunnerSleeper()
	defer sl.cancel()

	polls := newScriptedPolls(t, "__500__", "__500__", "__500__")
	srv := httptest.NewServer(polls.handler())
	defer srv.Close()

	runner := newTestRunner(t, sink, caps, srv.URL, sl)
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
