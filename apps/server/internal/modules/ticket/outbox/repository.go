package outbox

import (
	"context"
	"time"

	"servify/apps/server/internal/models"
)

// Repository is the outbox persistence contract used by the dispatcher.
type Repository interface {
	// Append records an outbox row inside the caller's DB transaction.
	// The caller owns the transaction; outbox never opens its own.
	Append(ctx context.Context, row *models.TicketOutbox) error
	// ClaimDue marks a batch of pending rows as in-flight (failed rows whose
	// next_attempt_at has passed also qualify). Returns claimed rows; rows are
	// claimed atomically per DB (single-writer semantics, same pattern as the
	// webhook delivery claim).
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]models.TicketOutbox, error)
	// MarkDelivered marks a claimed row delivered (event published exactly once).
	MarkDelivered(ctx context.Context, id uint, deliveredAt time.Time) error
	// MarkFailed records a transient failure and schedules the next attempt;
	// dead is set after max attempts (worker's policy).
	MarkFailed(ctx context.Context, id uint, attempt int, lastError string, nextAttemptAt *time.Time, dead bool) error
}

// Dispatcher publishes a single outboxed event to the event bus.
// Returning an error keeps the row in the retry window.
type Dispatcher interface {
	Dispatch(ctx context.Context, row models.TicketOutbox) error
}
