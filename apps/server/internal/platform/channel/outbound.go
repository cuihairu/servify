// This file carries the contract's shared outbound retry policy
// (docs/chat-platform-integration-contract.md). It lives beside the poll-loop
// runner so one retry budget cannot drift between platforms — the same
// single-source principle the ingestion runner follows. Platform adapters
// keep only what is genuinely platform-specific (payload shape, endpoints,
// status mapping) and delegate retry pacing here.

package channel

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"
)

// SendRetryPolicy is the contract's outbound attempt budget: 1s base backoff,
// 5m cap, 10 attempts or 24h per streak. The caller's idempotency key is the
// retry anchor and never leaves the adapter; the policy only decides when a
// server-class refusal is safe to resend.
type SendRetryPolicy struct {
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
	MaxAttempts int
	MaxDuration time.Duration
	Sleep       func(ctx context.Context, d time.Duration) error
}

// DefaultSendRetryPolicy wires the contract's outbound budget (1s base backoff,
// 5m cap, 10 attempts or 24h per streak) with a timer-based sleep.
func DefaultSendRetryPolicy() SendRetryPolicy {
	return SendRetryPolicy{
		MinBackoff:  time.Second,
		MaxBackoff:  5 * time.Minute,
		MaxAttempts: 10,
		MaxDuration: 24 * time.Hour,
		Sleep:       SleepContext,
	}
}

// RetrySend runs one outbound attempt under the policy. attempt returns the
// platform message id on success. Only a server-class refusal — a rate-limit
// or 5xx response (ErrRateLimited or ErrTransient with a non-zero status) — is
// retried, because the platform answered and refused the call. A transport
// failure (ErrTransient status 0) carries no such proof and is returned as
// unknown without a resend. anchor is the caller's idempotency key, carried in
// error text but never sent to the platform.
func RetrySend(ctx context.Context, policy SendRetryPolicy, anchor string, attempt func(context.Context) (string, error)) (string, error) {
	if policy.MinBackoff <= 0 {
		policy.MinBackoff = time.Second
	}
	if policy.MaxBackoff <= 0 {
		policy.MaxBackoff = 5 * time.Minute
	}
	if policy.Sleep == nil {
		policy.Sleep = SleepContext
	}
	backoff := policy.MinBackoff
	start := time.Now()
	for n := 0; ; n++ {
		id, err := attempt(ctx)
		if err == nil {
			return id, nil
		}
		var apiErr *AdapterError
		if !errors.As(err, &apiErr) || apiErr.Kind != ErrRateLimited && apiErr.Kind != ErrTransient || apiErr.Status == 0 {
			return "", fmt.Errorf("send %s: %w", anchor, err)
		}
		// The Nth consecutive failed attempt gives up on the spot — no pause,
		// no extra call past the budget.
		if policy.MaxAttempts > 0 && n+1 >= policy.MaxAttempts ||
			policy.MaxDuration > 0 && time.Since(start) >= policy.MaxDuration {
			return "", fmt.Errorf("send %s after %d attempts: %w", anchor, n+1, err)
		}
		var wait time.Duration
		if apiErr.Kind == ErrRateLimited && apiErr.RetryAfter > 0 {
			// Server said when to come back: obey it exactly, capped so a
			// hostile or buggy header cannot pin the send for hours.
			wait = min(apiErr.RetryAfter, policy.MaxBackoff)
		} else {
			wait = Jittered(backoff)
			backoff = min(backoff*2, policy.MaxBackoff)
		}
		if serr := policy.Sleep(ctx, wait); serr != nil {
			return "", fmt.Errorf("send %s: %w", anchor, serr)
		}
	}
}

// OutboundSendResult maps one outbound attempt outcome onto the contract's
// delivery status. A transport uncertainty is unknown; every platform refusal
// is rejected. Storage and validation failures never reach this helper.
func OutboundSendResult(err error) SendResult {
	var apiErr *AdapterError
	if errors.As(err, &apiErr) {
		if apiErr.Kind == ErrTransient && apiErr.Status == 0 {
			return SendResult{Status: SendResultUnknown}
		}
		return SendResult{Status: SendResultRejected}
	}
	// A non-adapter failure has no server verdict; keep it unknown rather than
	// fabricating a rejection the platform never made.
	return SendResult{Status: SendResultUnknown}
}

// Jittered returns a full-jitter wait inside [d/2, d], matching the shared
// poll-loop runner so outbound retries cannot synchronize across runners
// sharing one platform rate limit. Windows under 2ns cannot halve, so they
// pass through unchanged.
func Jittered(d time.Duration) time.Duration { return jitter(d) }

// jitter is the single full-jitter implementation shared by the poll-loop
// runner and the outbound policy, so one source of randomness cannot drift
// between ingestion and send retries.
func jitter(d time.Duration) time.Duration {
	half := int64(d / 2)
	if half <= 0 {
		return d
	}
	return time.Duration(half + rand.Int63n(half+1))
}

// SleepContext pauses for d and reports context cancellation; a non-positive d
// short-circuits (a zero wait must never busy-spin a failing loop).
func SleepContext(ctx context.Context, d time.Duration) error { return sleepCtx(ctx, d) }
