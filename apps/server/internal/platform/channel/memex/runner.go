package memex

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

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
	// MaxBackoff caps both the backoff window and any server Retry-After.
	MaxBackoff time.Duration
	// Sleep pauses without burning wall clock and must honor context
	// cancellation; NewRunner installs a timer-based implementation and tests
	// replace it to observe the policy.
	Sleep func(ctx context.Context, d time.Duration) error
}

// NewRunner wires a runner with conservative defaults (25s long poll under the
// 30s cap, 1s base backoff, 1min cap).
func NewRunner(c *Connector) *Runner {
	return &Runner{
		Connector:  c,
		LongPoll:   25 * time.Second,
		IdleWait:   time.Second,
		MinBackoff: time.Second,
		MaxBackoff: time.Minute,
		Sleep:      sleepCtx,
	}
}

// Run drives the loop. It only returns when something outside a healthy poll
// cycle says to stop: context cancellation, a fail-closed ingestion gate, a
// store failure, or a platform error class that retrying cannot fix.
func (r *Runner) Run(ctx context.Context) error {
	backoff := r.MinBackoff
	for {
		n, err := r.Connector.PollOnce(ctx, r.LongPoll)
		if err == nil {
			backoff = r.MinBackoff
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
