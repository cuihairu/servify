package delivery

import (
	"context"
	webhookdomain "servify/apps/server/internal/modules/webhook/domain"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/webhook/application"
	"servify/apps/server/internal/platform/eventbus"
)

type stubWebhookRepo struct {
	endpoints  []webhookdomain.WebhookEndpoint
	deliveries []webhookdomain.WebhookDelivery
}

func (s *stubWebhookRepo) ListEndpoints(ctx context.Context) ([]webhookdomain.WebhookEndpoint, error) {
	return s.endpoints, nil
}
func (s *stubWebhookRepo) GetEndpoint(ctx context.Context, id uint) (*webhookdomain.WebhookEndpoint, error) {
	return nil, application.ErrNotFound
}
func (s *stubWebhookRepo) CreateEndpoint(ctx context.Context, ep *webhookdomain.WebhookEndpoint) error {
	return nil
}
func (s *stubWebhookRepo) UpdateEndpoint(ctx context.Context, ep *webhookdomain.WebhookEndpoint) error {
	return nil
}
func (s *stubWebhookRepo) DeleteEndpoint(ctx context.Context, id uint) error { return nil }
func (s *stubWebhookRepo) CreateDelivery(ctx context.Context, d *webhookdomain.WebhookDelivery) error {
	s.deliveries = append(s.deliveries, *d)
	return nil
}
func (s *stubWebhookRepo) ListDeliveries(ctx context.Context, query application.DeliveryListQuery) ([]webhookdomain.WebhookDelivery, int64, error) {
	return s.deliveries, int64(len(s.deliveries)), nil
}
func (s *stubWebhookRepo) GetDelivery(ctx context.Context, id uint) (*webhookdomain.WebhookDelivery, error) {
	return nil, application.ErrNotFound
}
func (s *stubWebhookRepo) ResetDeliveryForRedeliver(ctx context.Context, id uint) error {
	return nil
}
func (s *stubWebhookRepo) ClaimDueDeliveries(ctx context.Context, now time.Time, limit int) ([]webhookdomain.WebhookDelivery, error) {
	return nil, nil
}
func (s *stubWebhookRepo) MarkDeliverySuccess(ctx context.Context, id uint, httpStatus int, durationMs int64, deliveredAt time.Time) error {
	return nil
}
func (s *stubWebhookRepo) MarkDeliveryFailure(ctx context.Context, id uint, attempt int, httpStatus int, durationMs int64, lastError string, dead bool, nextRetryAt *time.Time) error {
	return nil
}
func (s *stubWebhookRepo) GetTicketSnapshot(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	return &models.Ticket{ID: ticketID, Title: "t"}, nil
}
func (s *stubWebhookRepo) GetSessionSnapshot(ctx context.Context, sessionID string) (*models.Session, error) {
	return &models.Session{ID: sessionID}, nil
}
func (s *stubWebhookRepo) GetCallSnapshot(ctx context.Context, callID string) (*models.VoiceCall, error) {
	return &models.VoiceCall{ID: callID}, nil
}

type recordingBus struct {
	handlers map[string][]eventbus.Handler
}

func newRecordingBus() *recordingBus {
	return &recordingBus{handlers: map[string][]eventbus.Handler{}}
}

func (b *recordingBus) Subscribe(eventName string, handler eventbus.Handler) {
	b.handlers[eventName] = append(b.handlers[eventName], handler)
}
func (b *recordingBus) Publish(ctx context.Context, evt eventbus.Event) error { return nil }

type stubEvent struct {
	aggregateID string
}

func (e stubEvent) ID() string            { return "evt-1" }
func (e stubEvent) Name() string          { return "stub" }
func (e stubEvent) OccurredAt() time.Time { return time.Unix(0, 0) }
func (e stubEvent) TenantID() string      { return "" }
func (e stubEvent) AggregateID() string   { return e.aggregateID }

func TestSubscriberRegistersWhitelistOnly(t *testing.T) {
	bus := newRecordingBus()
	NewEventBusSubscriber(application.NewService(&stubWebhookRepo{}, nil)).Register(bus)

	for _, name := range application.SupportedEvents() {
		if len(bus.handlers[name]) != 1 {
			t.Fatalf("event %s should have exactly one handler", name)
		}
	}
	if _, ok := bus.handlers["ticket.updated"]; ok {
		t.Fatal("ticket.updated has no publisher and must not be subscribed")
	}
	// voice 事件已入白名单（call.* 五个都应被订阅）
	for _, name := range []string{"call.started", "call.held", "call.resumed", "call.transferred", "call.ended"} {
		if len(bus.handlers[name]) != 1 {
			t.Fatalf("voice event %s should have exactly one handler", name)
		}
	}
	if _, ok := bus.handlers["call.incoming"]; ok {
		t.Fatal("call.incoming has no publisher and must not be subscribed")
	}
}

func TestSubscriberHandlerEnqueuesDelivery(t *testing.T) {
	bus := newRecordingBus()
	repo := &stubWebhookRepo{endpoints: []webhookdomain.WebhookEndpoint{{ID: 1, Active: true}}}
	NewEventBusSubscriber(application.NewService(repo, nil)).Register(bus)

	handler := bus.handlers["ticket.created"][0]
	if err := handler.Handle(context.Background(), stubEvent{aggregateID: "ticket:1"}); err != nil {
		t.Fatalf("handler should not fail: %v", err)
	}
	if len(repo.deliveries) != 1 || repo.deliveries[0].EventName != "ticket.created" {
		t.Fatalf("expected one enqueued delivery, got %+v", repo.deliveries)
	}
}

func TestSubscriberNilSafety(t *testing.T) {
	var nilSub *EventBusSubscriber
	nilSub.Register(newRecordingBus())
	NewEventBusSubscriber(nil).Register(newRecordingBus())
	NewEventBusSubscriber(application.NewService(&stubWebhookRepo{}, nil)).Register(nil)
}
