package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/webhook/application"
)

// adapterRepo 是 application.Repository 的内存最小实现，专供 adapter 委托测试。
type adapterRepo struct {
	endpoints  []models.WebhookEndpoint
	deliveries []models.WebhookDelivery
}

func (r *adapterRepo) ListEndpoints(ctx context.Context) ([]models.WebhookEndpoint, error) {
	return r.endpoints, nil
}

func (r *adapterRepo) GetEndpoint(ctx context.Context, id uint) (*models.WebhookEndpoint, error) {
	for i := range r.endpoints {
		if r.endpoints[i].ID == id {
			return &r.endpoints[i], nil
		}
	}
	return nil, application.ErrNotFound
}

func (r *adapterRepo) CreateEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error {
	ep.ID = uint(len(r.endpoints) + 1)
	r.endpoints = append(r.endpoints, *ep)
	return nil
}

func (r *adapterRepo) UpdateEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error {
	for i := range r.endpoints {
		if r.endpoints[i].ID == ep.ID {
			r.endpoints[i] = *ep
			return nil
		}
	}
	return application.ErrNotFound
}

func (r *adapterRepo) DeleteEndpoint(ctx context.Context, id uint) error {
	for i := range r.endpoints {
		if r.endpoints[i].ID == id {
			r.endpoints = append(r.endpoints[:i], r.endpoints[i+1:]...)
			return nil
		}
	}
	return application.ErrNotFound
}

func (r *adapterRepo) CreateDelivery(ctx context.Context, d *models.WebhookDelivery) error {
	d.ID = uint(len(r.deliveries) + 1)
	r.deliveries = append(r.deliveries, *d)
	return nil
}

func (r *adapterRepo) ListDeliveries(ctx context.Context, query DeliveryListQuery) ([]models.WebhookDelivery, int64, error) {
	return r.deliveries, int64(len(r.deliveries)), nil
}

func (r *adapterRepo) GetDelivery(ctx context.Context, id uint) (*models.WebhookDelivery, error) {
	for i := range r.deliveries {
		if r.deliveries[i].ID == id {
			return &r.deliveries[i], nil
		}
	}
	return nil, application.ErrNotFound
}

func (r *adapterRepo) ResetDeliveryForRedeliver(ctx context.Context, id uint) error {
	for i := range r.deliveries {
		if r.deliveries[i].ID == id {
			r.deliveries[i].Status = models.WebhookDeliveryStatusPending
			return nil
		}
	}
	return application.ErrNotFound
}

func (r *adapterRepo) ClaimDueDeliveries(ctx context.Context, now time.Time, limit int) ([]models.WebhookDelivery, error) {
	return nil, nil
}

func (r *adapterRepo) MarkDeliverySuccess(ctx context.Context, id uint, httpStatus int, durationMs int64, deliveredAt time.Time) error {
	return nil
}

func (r *adapterRepo) MarkDeliveryFailure(ctx context.Context, id uint, attempt int, httpStatus int, durationMs int64, lastError string, dead bool, nextRetryAt *time.Time) error {
	return nil
}

func (r *adapterRepo) GetTicketSnapshot(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	return &models.Ticket{ID: ticketID}, nil
}

func (r *adapterRepo) GetSessionSnapshot(ctx context.Context, sessionID string) (*models.Session, error) {
	return &models.Session{ID: sessionID}, nil
}

func (r *adapterRepo) GetCallSnapshot(ctx context.Context, callID string) (*models.VoiceCall, error) {
	return &models.VoiceCall{ID: callID}, nil
}

// pingDeliverer 恒定返回成功投递。
type pingDeliverer struct{}

func (pingDeliverer) Deliver(ctx context.Context, req application.DeliveryRequest) application.DeliveryResult {
	return application.DeliveryResult{Success: true, StatusCode: 200}
}

var _ application.Deliverer = pingDeliverer{}

func newAdapterUnderTest(t *testing.T) (*HandlerServiceAdapter, *adapterRepo) {
	t.Helper()
	repo := &adapterRepo{endpoints: []models.WebhookEndpoint{
		{ID: 1, Name: "ops", URL: "https://ops.example.com", Secret: "s3cret", Active: true},
	}}
	svc := application.NewService(repo, nil)
	svc.SetDeliverer(pingDeliverer{})
	return NewHandlerServiceAdapter(svc), repo
}

func TestHandlerAdapterExposesUnderlyingService(t *testing.T) {
	adapter, _ := newAdapterUnderTest(t)
	if adapter.Service() == nil {
		t.Fatal("Service() must expose the wrapped application service")
	}
}

func TestHandlerAdapterListEndpointsDelegates(t *testing.T) {
	adapter, _ := newAdapterUnderTest(t)
	eps, err := adapter.ListEndpoints(context.Background())
	if err != nil || len(eps) != 1 || eps[0].Name != "ops" {
		t.Fatalf("ListEndpoints = (%+v, %v)", eps, err)
	}
}

func TestHandlerAdapterCreateEndpointRejectsNilRequest(t *testing.T) {
	adapter, _ := newAdapterUnderTest(t)
	if _, _, err := adapter.CreateEndpoint(context.Background(), nil); !errors.Is(err, application.ErrNilRequest) {
		t.Fatalf("nil create request = %v", err)
	}
	created, secret, err := adapter.CreateEndpoint(context.Background(), &EndpointCreateRequest{
		Name: "new", URL: "https://new.example.com",
	})
	if err != nil || created == nil || len(secret) != 64 {
		t.Fatalf("create via adapter = (%+v, %q, %v)", created, secret, err)
	}
}

func TestHandlerAdapterUpdateEndpointRejectsNilRequest(t *testing.T) {
	adapter, _ := newAdapterUnderTest(t)
	if _, err := adapter.UpdateEndpoint(context.Background(), 1, nil); !errors.Is(err, application.ErrNilRequest) {
		t.Fatalf("nil update request = %v", err)
	}
	updated, err := adapter.UpdateEndpoint(context.Background(), 1, &EndpointUpdateRequest{Name: "renamed"})
	if err != nil || updated.Name != "renamed" {
		t.Fatalf("update via adapter = (%+v, %v)", updated, err)
	}
}

func TestHandlerAdapterDelegatesLifecycleCalls(t *testing.T) {
	adapter, repo := newAdapterUnderTest(t)
	ctx := context.Background()

	if err := adapter.DeleteEndpoint(ctx, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// 重新放入一个端点供 rotate / test 使用
	repo.endpoints = append(repo.endpoints, models.WebhookEndpoint{ID: 2, Name: "again", URL: "https://a.example.com", Active: true})

	rotated, secret, err := adapter.RotateEndpointSecret(ctx, 2)
	if err != nil || len(secret) != 64 || rotated.Secret != secret {
		t.Fatalf("rotate = (%+v, %q, %v)", rotated, secret, err)
	}

	delivery, err := adapter.TestEndpoint(ctx, 2)
	if err != nil || delivery.EventName != "ping" || delivery.Status != models.WebhookDeliveryStatusSuccess {
		t.Fatalf("test endpoint = (%+v, %v)", delivery, err)
	}

	rows, total, err := adapter.ListDeliveries(ctx, DeliveryListQuery{})
	if err != nil || total != int64(len(rows)) || len(rows) == 0 {
		t.Fatalf("list deliveries = (%d rows, %d, %v)", len(rows), total, err)
	}

	redelivered, err := adapter.RedeliverDelivery(ctx, rows[0].ID)
	if err != nil || redelivered.Status != models.WebhookDeliveryStatusPending {
		t.Fatalf("redeliver = (%+v, %v)", redelivered, err)
	}
}

func TestHandlerAdapterSupportedEvents(t *testing.T) {
	adapter, _ := newAdapterUnderTest(t)
	events := adapter.SupportedEvents()
	if len(events) == 0 {
		t.Fatal("supported events must not be empty")
	}
	found := false
	for _, name := range events {
		if strings.TrimSpace(name) == application.EventTicketCreated {
			found = true
		}
	}
	if !found {
		t.Fatalf("supported events must include %q: %v", application.EventTicketCreated, events)
	}
}
