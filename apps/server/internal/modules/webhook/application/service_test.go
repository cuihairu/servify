package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

type fakeRepo struct {
	endpoints  []models.WebhookEndpoint
	deliveries []models.WebhookDelivery
	ticket     *models.Ticket
	session    *models.Session

	listErr      error
	ticketErr    error
	sessionErr   error
	createdCount int
}

func (f *fakeRepo) ListEndpoints(ctx context.Context) ([]models.WebhookEndpoint, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.endpoints, nil
}
func (f *fakeRepo) GetEndpoint(ctx context.Context, id uint) (*models.WebhookEndpoint, error) {
	for i := range f.endpoints {
		if f.endpoints[i].ID == id {
			return &f.endpoints[i], nil
		}
	}
	return nil, ErrNotFound
}
func (f *fakeRepo) CreateEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error {
	ep.ID = uint(len(f.endpoints) + 1)
	f.endpoints = append(f.endpoints, *ep)
	return nil
}
func (f *fakeRepo) UpdateEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error {
	for i := range f.endpoints {
		if f.endpoints[i].ID == ep.ID {
			f.endpoints[i] = *ep
			return nil
		}
	}
	return ErrNotFound
}
func (f *fakeRepo) DeleteEndpoint(ctx context.Context, id uint) error {
	for i := range f.endpoints {
		if f.endpoints[i].ID == id {
			f.endpoints = append(f.endpoints[:i], f.endpoints[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
func (f *fakeRepo) CreateDelivery(ctx context.Context, d *models.WebhookDelivery) error {
	f.createdCount++
	d.ID = uint(len(f.deliveries) + 1)
	f.deliveries = append(f.deliveries, *d)
	return nil
}
func (f *fakeRepo) ListDeliveries(ctx context.Context, query DeliveryListQuery) ([]models.WebhookDelivery, int64, error) {
	return f.deliveries, int64(len(f.deliveries)), nil
}
func (f *fakeRepo) GetDelivery(ctx context.Context, id uint) (*models.WebhookDelivery, error) {
	for i := range f.deliveries {
		if f.deliveries[i].ID == id {
			return &f.deliveries[i], nil
		}
	}
	return nil, ErrNotFound
}
func (f *fakeRepo) ResetDeliveryForRedeliver(ctx context.Context, id uint) error {
	for i := range f.deliveries {
		if f.deliveries[i].ID == id {
			f.deliveries[i].Status = models.WebhookDeliveryStatusPending
			f.deliveries[i].Attempt = 0
			f.deliveries[i].NextRetryAt = nil
			return nil
		}
	}
	return ErrNotFound
}
func (f *fakeRepo) ClaimDueDeliveries(ctx context.Context, now time.Time, limit int) ([]models.WebhookDelivery, error) {
	var out []models.WebhookDelivery
	for _, d := range f.deliveries {
		if d.Status != models.WebhookDeliveryStatusPending {
			continue
		}
		if d.NextRetryAt != nil && d.NextRetryAt.After(now) {
			continue
		}
		out = append(out, d)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
func (f *fakeRepo) MarkDeliverySuccess(ctx context.Context, id uint, httpStatus int, durationMs int64, deliveredAt time.Time) error {
	for i := range f.deliveries {
		if f.deliveries[i].ID == id {
			f.deliveries[i].Status = models.WebhookDeliveryStatusSuccess
			f.deliveries[i].Attempt++
			f.deliveries[i].HTTPStatus = httpStatus
			return nil
		}
	}
	return ErrNotFound
}
func (f *fakeRepo) MarkDeliveryFailure(ctx context.Context, id uint, attempt int, httpStatus int, durationMs int64, lastError string, dead bool, nextRetryAt *time.Time) error {
	for i := range f.deliveries {
		if f.deliveries[i].ID == id {
			f.deliveries[i].Attempt = attempt
			f.deliveries[i].HTTPStatus = httpStatus
			f.deliveries[i].LastError = lastError
			if dead {
				f.deliveries[i].Status = models.WebhookDeliveryStatusDead
			} else {
				f.deliveries[i].Status = models.WebhookDeliveryStatusPending
				f.deliveries[i].NextRetryAt = nextRetryAt
			}
			return nil
		}
	}
	return ErrNotFound
}
func (f *fakeRepo) GetTicketSnapshot(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	if f.ticketErr != nil {
		return nil, f.ticketErr
	}
	if f.ticket == nil {
		return nil, ErrNotFound
	}
	return f.ticket, nil
}
func (f *fakeRepo) GetSessionSnapshot(ctx context.Context, sessionID string) (*models.Session, error) {
	if f.sessionErr != nil {
		return nil, f.sessionErr
	}
	if f.session == nil {
		return nil, ErrNotFound
	}
	return f.session, nil
}

type fakeDeliverer struct {
	results []DeliveryResult
	request []DeliveryRequest
}

func (d *fakeDeliverer) Deliver(ctx context.Context, req DeliveryRequest) DeliveryResult {
	d.request = append(d.request, req)
	if len(d.results) == 0 {
		return DeliveryResult{Success: true, StatusCode: 200}
	}
	res := d.results[0]
	d.results = d.results[1:]
	return res
}

func newTestService(repo *fakeRepo, deliverer Deliverer) *Service {
	svc := NewService(repo, nil)
	if deliverer != nil {
		svc.SetDeliverer(deliverer)
	}
	return svc
}

func TestEnqueueEventCreatesPendingDeliveriesForMatchingEndpoints(t *testing.T) {
	repo := &fakeRepo{ticket: &models.Ticket{ID: 7, Title: "t"}}
	repo.endpoints = []models.WebhookEndpoint{
		{ID: 1, Name: "all", URL: "https://a.example.com", Secret: "s1", Active: true},
		{ID: 2, Name: "scoped", URL: "https://b.example.com", Secret: "s2", Active: true, Events: EventTicketAssigned},
		{ID: 3, Name: "inactive", URL: "https://c.example.com", Secret: "s3", Active: false},
	}
	svc := newTestService(repo, &fakeDeliverer{})
	svc.EnqueueEvent(context.Background(), EventTicketCreated, "ticket:7", "evt-1")

	if repo.createdCount != 1 {
		t.Fatalf("expected 1 delivery row, got %d", repo.createdCount)
	}
	if repo.deliveries[0].EndpointID != 1 || repo.deliveries[0].Status != models.WebhookDeliveryStatusPending {
		t.Fatalf("unexpected delivery: %+v", repo.deliveries[0])
	}
	if !strings.Contains(repo.deliveries[0].Payload, `"event":"ticket.created"`) {
		t.Fatalf("payload missing event name: %s", repo.deliveries[0].Payload)
	}
	if !strings.Contains(repo.deliveries[0].Payload, `"title":"t"`) {
		t.Fatalf("payload missing ticket snapshot: %s", repo.deliveries[0].Payload)
	}
}

func TestEnqueueEventIgnoresUnsupportedAndMissingSnapshots(t *testing.T) {
	repo := &fakeRepo{}
	svc := newTestService(repo, &fakeDeliverer{})
	svc.EnqueueEvent(context.Background(), "ticket.updated", "ticket:1", "evt")
	if repo.createdCount != 0 {
		t.Fatalf("unsupported event should be ignored")
	}
	repo.endpoints = []models.WebhookEndpoint{{ID: 1, Active: true}}
	repo.ticketErr = errors.New("boom")
	svc.EnqueueEvent(context.Background(), EventTicketCreated, "ticket:1", "evt")
	if repo.createdCount != 0 {
		t.Fatalf("snapshot failure should skip enqueue")
	}
}

func TestEnqueueEventConversationAndRoutingPrefixes(t *testing.T) {
	repo := &fakeRepo{session: &models.Session{ID: "sess-1"}}
	repo.endpoints = []models.WebhookEndpoint{{ID: 1, Active: true}}
	svc := newTestService(repo, &fakeDeliverer{})
	svc.EnqueueEvent(context.Background(), EventConversationMessageReceived, "conversation:sess-1", "evt-2")
	if repo.createdCount != 1 {
		t.Fatalf("conversation prefix should enqueue, got %d", repo.createdCount)
	}
	svc.EnqueueEvent(context.Background(), EventRoutingTransferCompleted, "routing:sess-1", "evt-3")
	if repo.createdCount != 2 {
		t.Fatalf("routing prefix should enqueue, got %d", repo.createdCount)
	}
}

func TestProcessDueDeliveriesSuccessAndBackoff(t *testing.T) {
	repo := &fakeRepo{
		endpoints: []models.WebhookEndpoint{{ID: 1, Name: "ep", URL: "https://a.example.com", Secret: "s", Active: true}},
		deliveries: []models.WebhookDelivery{
			{ID: 1, EndpointID: 1, EventName: EventTicketCreated, Status: models.WebhookDeliveryStatusPending, Payload: "{}"},
		},
		ticket: &models.Ticket{ID: 1},
	}
	deliverer := &fakeDeliverer{results: []DeliveryResult{{Success: true, StatusCode: 200}}}
	svc := newTestService(repo, deliverer)

	processed := svc.ProcessDueDeliveries(context.Background(), time.Now())
	if processed != 1 {
		t.Fatalf("expected 1 processed, got %d", processed)
	}
	if repo.deliveries[0].Status != models.WebhookDeliveryStatusSuccess || repo.deliveries[0].Attempt != 1 {
		t.Fatalf("delivery should be success with attempt 1: %+v", repo.deliveries[0])
	}
	if deliverer.request[0].DeliveryID != 1 || deliverer.request[0].Secret != "s" {
		t.Fatalf("delivery request should carry delivery id and secret: %+v", deliverer.request[0])
	}
}

func TestProcessDueDeliveriesBackoffAndDead(t *testing.T) {
	repo := &fakeRepo{
		endpoints: []models.WebhookEndpoint{{ID: 1, Active: true}},
		deliveries: []models.WebhookDelivery{
			{ID: 1, EndpointID: 1, EventName: EventTicketCreated, Status: models.WebhookDeliveryStatusPending, Payload: "{}", Attempt: 1},
		},
	}
	now := time.Now()
	svc := newTestService(repo, &fakeDeliverer{results: []DeliveryResult{{Success: false, StatusCode: 500, Error: "boom"}}})
	svc.ProcessDueDeliveries(context.Background(), now)

	d := repo.deliveries[0]
	if d.Status != models.WebhookDeliveryStatusPending || d.Attempt != 2 {
		t.Fatalf("retryable failure should bump attempt: %+v", d)
	}
	if d.NextRetryAt == nil || !d.NextRetryAt.After(now) {
		t.Fatalf("expected future next_retry_at, got %+v", d.NextRetryAt)
	}
	wantBackoff := now.UTC().Add(retryBackoff[1])
	if d.NextRetryAt.Sub(wantBackoff.Truncate(time.Second)) > time.Second {
		t.Fatalf("unexpected backoff: %v", d.NextRetryAt)
	}

	// 410 Gone：不重试直接 dead。
	repo.deliveries[0] = models.WebhookDelivery{ID: 1, EndpointID: 1, Status: models.WebhookDeliveryStatusPending, Payload: "{}"}
	svc2 := newTestService(repo, &fakeDeliverer{results: []DeliveryResult{{Success: false, StatusCode: 410}}})
	svc2.ProcessDueDeliveries(context.Background(), now)
	if repo.deliveries[0].Status != models.WebhookDeliveryStatusDead {
		t.Fatalf("410 should go straight to dead: %+v", repo.deliveries[0])
	}

	// 达到最大次数后 dead。
	repo.deliveries[0] = models.WebhookDelivery{ID: 1, EndpointID: 1, Status: models.WebhookDeliveryStatusPending, Payload: "{}", Attempt: maxAttempts - 1}
	svc3 := newTestService(repo, &fakeDeliverer{results: []DeliveryResult{{Success: false, StatusCode: 500}}})
	svc3.ProcessDueDeliveries(context.Background(), now)
	if repo.deliveries[0].Status != models.WebhookDeliveryStatusDead {
		t.Fatalf("exhausted attempts should be dead: %+v", repo.deliveries[0])
	}
}

func TestProcessDueDeliveriesDeadEndpoint(t *testing.T) {
	repo := &fakeRepo{
		endpoints: []models.WebhookEndpoint{{ID: 1, Active: false}},
		deliveries: []models.WebhookDelivery{
			{ID: 1, EndpointID: 1, Status: models.WebhookDeliveryStatusPending, Payload: "{}"},
		},
	}
	svc := newTestService(repo, &fakeDeliverer{})
	svc.ProcessDueDeliveries(context.Background(), time.Now())
	if repo.deliveries[0].Status != models.WebhookDeliveryStatusDead {
		t.Fatalf("inactive endpoint should dead-letter delivery: %+v", repo.deliveries[0])
	}
}

func TestEndpointCRUDValidation(t *testing.T) {
	repo := &fakeRepo{}
	svc := newTestService(repo, nil)

	if _, _, err := svc.CreateEndpoint(context.Background(), EndpointRequest{Name: "x", URL: "ftp://bad"}); err == nil {
		t.Fatal("expected url scheme validation error")
	}
	if _, _, err := svc.CreateEndpoint(context.Background(), EndpointRequest{Name: "x", URL: "https://ok.example.com", Events: "not.an.event"}); err == nil {
		t.Fatal("expected event whitelist validation error")
	}
	ep, secret, err := svc.CreateEndpoint(context.Background(), EndpointRequest{
		Name: "main", URL: "https://ok.example.com/hook", Events: "ticket.created, ticket.closed",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(secret) != 64 || ep.Secret != secret {
		t.Fatalf("expected generated 32-byte hex secret")
	}
	if ep.Events != "ticket.created,ticket.closed" {
		t.Fatalf("events should be normalized: %q", ep.Events)
	}

	rotated, secret2, err := svc.RotateEndpointSecret(context.Background(), ep.ID)
	if err != nil || secret2 == secret {
		t.Fatalf("rotate should yield a new secret: %v %q", err, secret2)
	}
	if rotated.Secret != secret2 {
		t.Fatal("rotated endpoint should persist new secret")
	}
}

func TestTestEndpointDeliversPingSynchronously(t *testing.T) {
	repo := &fakeRepo{endpoints: []models.WebhookEndpoint{{ID: 1, Name: "ep", URL: "https://a.example.com", Secret: "s", Active: true}}}
	deliverer := &fakeDeliverer{}
	svc := newTestService(repo, deliverer)

	delivery, err := svc.TestEndpoint(context.Background(), 1)
	if err != nil {
		t.Fatalf("test endpoint: %v", err)
	}
	if delivery.EventName != "ping" || delivery.Status != models.WebhookDeliveryStatusSuccess {
		t.Fatalf("unexpected test delivery: %+v", delivery)
	}
	if !strings.Contains(delivery.Payload, `"event":"ping"`) {
		t.Fatalf("ping payload unexpected: %s", delivery.Payload)
	}
	if repo.createdCount != 1 {
		t.Fatal("test delivery should be persisted for audit")
	}
}

func TestDeliverOneShotPersistsAuditRow(t *testing.T) {
	repo := &fakeRepo{endpoints: []models.WebhookEndpoint{{ID: 1, Name: "ep", URL: "https://a.example.com", Secret: "s", Active: true}}}
	deliverer := &fakeDeliverer{}
	svc := newTestService(repo, deliverer)

	err := svc.DeliverOneShot(context.Background(), "https://ops.example.com/hook", "sec", "automation.call_webhook", "", map[string]interface{}{"id": float64(9)})
	if err != nil {
		t.Fatalf("deliver one shot: %v", err)
	}
	if deliverer.request[0].URL != "https://ops.example.com/hook" || deliverer.request[0].Secret != "sec" {
		t.Fatalf("unexpected request target: %+v", deliverer.request[0])
	}
	if repo.createdCount != 1 {
		t.Fatal("automation delivery should be persisted for audit")
	}
	if got := repo.deliveries[0]; got.EndpointID != 0 || got.EventName != "automation.call_webhook" || got.Status != models.WebhookDeliveryStatusSuccess {
		t.Fatalf("unexpected audit row: %+v", got)
	}
}

func TestDeliverOneShotFailureReturnsError(t *testing.T) {
	repo := &fakeRepo{}
	deliverer := &fakeDeliverer{results: []DeliveryResult{{Success: false, Error: "connection refused"}}}
	svc := newTestService(repo, deliverer)

	if err := svc.DeliverOneShot(context.Background(), "https://x.example.com", "", "automation.call_webhook", "", nil); err == nil {
		t.Fatal("expected dispatch failure to return error")
	}
	if repo.createdCount != 1 {
		t.Fatal("failed delivery should still be persisted for audit")
	}
	if got := repo.deliveries[0]; got.Status != models.WebhookDeliveryStatusFailed {
		t.Fatalf("failed dispatch should be terminal failed, got %+v", got)
	}
}

func TestDeliverOneShotRequiresURL(t *testing.T) {
	svc := newTestService(&fakeRepo{}, nil)
	if err := svc.DeliverOneShot(context.Background(), "", "", "automation.call_webhook", "", nil); !errors.Is(err, ErrNilRequest) {
		t.Fatalf("expected ErrNilRequest, got %v", err)
	}
}

func TestRedeliverResetsDelivery(t *testing.T) {
	repo := &fakeRepo{deliveries: []models.WebhookDelivery{{ID: 1, Status: models.WebhookDeliveryStatusDead, Attempt: 6}}}
	svc := newTestService(repo, nil)
	redelivered, err := svc.RedeliverDelivery(context.Background(), 1)
	if err != nil {
		t.Fatalf("redeliver: %v", err)
	}
	if redelivered.Status != models.WebhookDeliveryStatusPending || redelivered.Attempt != 0 {
		t.Fatalf("redeliver should reset: %+v", redelivered)
	}
}

func TestGenerateSecretUniqueness(t *testing.T) {
	a, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GenerateSecret()
	if a == b {
		t.Fatal("secrets must be unique")
	}
}
