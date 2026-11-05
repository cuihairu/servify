package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"servify/apps/server/internal/models"
	ticketapp "servify/apps/server/internal/modules/ticket/application"
	"servify/apps/server/internal/platform/eventbus"
)

// BusDispatcher publishes an outboxed TicketEvent to the event bus.
// It deserializes the persisted payload back into the typed event so
// consumers that type-assert (e.g. conversation timeline projection,
// conversationIDFromTicketEvent) see the real TicketEvent, not a wrapper.
type BusDispatcher struct {
	bus eventbus.Bus
}

func NewBusDispatcher(bus eventbus.Bus) *BusDispatcher {
	return &BusDispatcher{bus: bus}
}

func (d *BusDispatcher) Dispatch(ctx context.Context, row models.TicketOutbox) error {
	if d == nil || d.bus == nil {
		return fmt.Errorf("ticket outbox: bus not configured")
	}
	event, err := UnmarshalEvent(row)
	if err != nil {
		return err
	}
	return d.bus.Publish(ctx, event)
}

// UnmarshalEvent reconstructs the typed TicketEvent from an outbox row.
// The payload is the JSON of ticketapp.TicketEvent (produced at enqueue time).
func UnmarshalEvent(row models.TicketOutbox) (eventbus.Event, error) {
	var event ticketapp.TicketEvent
	if row.Payload == "" {
		return nil, fmt.Errorf("ticket outbox: empty payload for event %q", row.EventID)
	}
	if err := json.Unmarshal([]byte(row.Payload), &event); err != nil {
		return nil, fmt.Errorf("ticket outbox: unmarshal event %q: %w", row.EventID, err)
	}
	if event.EventID == "" {
		event.EventID = row.EventID
	}
	if event.EventName == "" {
		event.EventName = row.EventName
	}
	if event.EventAggregateID == "" {
		event.EventAggregateID = row.AggregateID
	}
	if event.EventOccurredAt.IsZero() {
		event.EventOccurredAt = row.CreatedAt
	}
	return event, nil
}

// Now is a seam for deterministic tests.
var Now = time.Now
