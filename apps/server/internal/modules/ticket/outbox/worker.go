package outbox

import (
	"context"
	"time"
)

// WorkerContract is the dispatcher-facing processor used by the periodic
// worker. ProcessDueDeliveries claims a batch of due outbox rows and publishes
// each to the event bus; failed rows stay in the retry window with backoff.
type WorkerContract interface {
	ProcessDueEvents(ctx context.Context, now time.Time) int
}

// ProcessDueEvents claims and dispatches due outbox rows. It is intentionally
// simple and deterministic: single-threaded, one batch per call, backoff on
// transient failures, terminal dead state after MaxAttempts.
func ProcessDueEvents(ctx context.Context, repo Repository, dispatcher Dispatcher, now time.Time, limit int) int {
	if repo == nil || dispatcher == nil || limit <= 0 {
		return 0
	}
	rows, err := repo.ClaimDue(ctx, now, limit)
	if err != nil {
		return 0
	}
	processed := 0
	for _, row := range rows {
		if err := dispatcher.Dispatch(ctx, row); err != nil {
			_ = repo.MarkFailed(ctx, row.ID, row.Attempts+1, err.Error(), retryAfter(row.Attempts), row.Attempts+1 >= MaxAttempts)
			continue
		}
		if err := repo.MarkDelivered(ctx, row.ID, Now()); err != nil {
			continue
		}
		processed++
	}
	return processed
}

// MaxAttempts is the terminal retry budget; after this the row is dead and
// requires manual replay (same policy as webhook deliveries).
const MaxAttempts = 5

// retryAfter returns the next attempt time for a transient failure.
// Backoff: 1s, 2s, 4s, 8s, then dead (no more retries).
func retryAfter(failedAttempts int) *time.Time {
	remaining := MaxAttempts - failedAttempts - 1
	if remaining <= 0 {
		return nil
	}
	backoff := time.Duration(1<<uint(failedAttempts)) * time.Second
	next := Now().Add(backoff)
	return &next
}

// Processor adapts Repository + Dispatcher to WorkerContract (avoids adding
// another wiring-layer struct at every call site).
type Processor struct {
	Repo       Repository
	Dispatcher Dispatcher
}

func (p *Processor) ProcessDueEvents(ctx context.Context, now time.Time) int {
	return ProcessDueEvents(ctx, p.Repo, p.Dispatcher, now, DefaultBatchSize)
}

// DefaultBatchSize is the outbox worker batch size (same as webhook deliveries).
const DefaultBatchSize = 50
