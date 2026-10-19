package memex

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"
)

// DeadLetterError is what Run returns when one retryable-failure streak
// exhausts its budget (contract: stop after 10 attempts or 24h, then
// dead-letter and alert). It deliberately carries no provider detail or
// message content — only the failure class and counters — so it is safe for
// logs, alerts and support surfaces.
type DeadLetterError struct {
	Reason   string // "attempts" or "budget"
	Kind     ErrorKind
	Status   int
	Attempts int
}

func (e *DeadLetterError) Error() string {
	return fmt.Sprintf("memex ingestion dead-lettered (%s) after %d attempts: %s status=%d",
		e.Reason, e.Attempts, e.Kind, e.Status)
}

// Runner is the poll-loop driver for one Memex installation: it repeats
// PollOnce until the context is canceled or a failure says to stop. Retry
// policy follows the contract's error classes — the platform's own status
// decides pacing, never a hardcoded guess.
type Runner struct {
	Connector *Connector

	// LongPoll is the per-pull server-side long-poll budget (Memex caps at
	// 30000ms); staying under it leaves the server time to answer cleanly.
	LongPoll time.Duration
	// IdleWait is the pause after a poll that drained an empty inbox. With a
	// long poll this is only a small breathing gap, not the main delay.
	IdleWait time.Duration
	// MinBackoff is the base failure pause; every retry doubles it (capped).
	MinBackoff time.Duration
	// MaxBackoff caps both the backoff window and any server Retry-After
	// (contract cap: 5m).
	MaxBackoff time.Duration
	// MaxAttempts bounds one streak of retryable failures; hitting it
	// dead-letters the runner so the caller can alert (contract: 10). Zero
	// disables the attempt budget.
	MaxAttempts int
	// MaxDuration bounds the same streak in wall time (contract: 24h); zero
	// disables the time budget.
	MaxDuration time.Duration
	// Sleep pauses without burning wall clock and must honor context
	// cancellation; NewRunner installs a timer-based implementation and tests
	// replace it to observe the policy.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now supplies the clock for streak deadlines; tests drive it.
	Now func() time.Time
}

// NewRunner wires a runner at the contract's retry budget (25s long poll under
// the 30s cap, 1s base backoff, 5m cap, 10 attempts or 24h per streak).
func NewRunner(c *Connector) *Runner {
	return &Runner{
		Connector:   c,
		LongPoll:    25 * time.Second,
		IdleWait:    time.Second,
		MinBackoff:  time.Second,
		MaxBackoff:  5 * time.Minute,
		MaxAttempts: 10,
		MaxDuration: 24 * time.Hour,
		Sleep:       sleepCtx,
		Now:         time.Now,
	}
}

// Run drives the loop. It only returns when something outside a healthy poll
// cycle says to stop: context cancellation, a fail-closed ingestion gate, a
// store failure, a platform error class that retrying cannot fix, or a
// retryable streak that exhausted its budget (a *DeadLetterError for the
// caller to alert on).
func (r *Runner) Run(ctx context.Context) error {
	backoff := r.MinBackoff
	attempts := 0
	var streakStart time.Time
	for {
		n, err := r.Connector.PollOnce(ctx, r.LongPoll)
		if err == nil {
			backoff = r.MinBackoff
			attempts = 0
			streakStart = time.Time{}
			if n == 0 {
				if serr := r.Sleep(ctx, r.IdleWait); serr != nil {
					return serr
				}
			}
			continue
		}
		// No grant recorded: a configuration state, not a flaky call — stop
		// and let the operator record the capability snapshot.
		if errors.Is(err, ErrIngestionDisabled) {
			return err
		}
		var apiErr *AdapterError
		if !errors.As(err, &apiErr) {
			// Store (inbox/capability) failures are surfaced, not retried:
			// a broken database will not heal itself by polling faster. But a
			// cancellation that raced a query comes back shaped as a driver
			// error (sqlite interrupts in-flight statements); report it as
			// what it is so shutdown does not read like a store failure.
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
			return err
		}
		switch apiErr.Kind {
		case ErrRateLimited, ErrTransient:
			attempts++
			if streakStart.IsZero() {
				streakStart = r.now()
			}
			// The Nth consecutive failed attempt gives up on the spot — no
			// pause, no extra poll past the budget.
			if r.MaxAttempts > 0 && attempts >= r.MaxAttempts {
				return &DeadLetterError{Reason: "attempts", Kind: apiErr.Kind,
					Status: apiErr.Status, Attempts: attempts}
			}
			if r.MaxDuration > 0 && !r.now().Before(streakStart.Add(r.MaxDuration)) {
				return &DeadLetterError{Reason: "budget", Kind: apiErr.Kind,
					Status: apiErr.Status, Attempts: attempts}
			}
			var wait time.Duration
			if apiErr.Kind == ErrRateLimited && apiErr.RetryAfter > 0 {
				// Server said when to come back: obey it exactly, capped so a
				// hostile or buggy header cannot pin the runner for hours.
				wait = min(apiErr.RetryAfter, r.MaxBackoff)
			} else {
				wait = r.jittered(backoff)
				backoff = min(backoff*2, r.MaxBackoff)
			}
			if serr := r.Sleep(ctx, wait); serr != nil {
				return serr
			}
		default:
			// auth, permission, permanent and invalid cannot be fixed by
			// retrying: a bad token or a malformed reply repeats forever.
			return err
		}
	}
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// jittered returns a full-jitter wait inside [d/2, d]. Halving the window
// keeps retry storms from synchronizing across runners sharing a rate limit,
// while still bounding the worst-case pause to the window itself. Windows
// under 2ns cannot halve, so they pass through unchanged.
func (r *Runner) jittered(d time.Duration) time.Duration {
	half := int64(d / 2)
	if half <= 0 {
		return d
	}
	return time.Duration(half + rand.Int63n(half+1))
}

// sleepCtx pauses for d and reports context cancellation; a non-positive d
// short-circuits (a zero wait must never busy-spin a failing loop).
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
