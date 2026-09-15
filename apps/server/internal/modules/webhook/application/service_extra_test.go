package application

import (
	"context"
	"errors"
	webhookdomain "servify/apps/server/internal/modules/webhook/domain"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

// errFakeRepo 在 fakeRepo 上叠加各写路径的故障开关与查询捕获。
type errFakeRepo struct {
	*fakeRepo

	createDeliveryErr error
	createEndpointErr error
	updateEndpointErr error
	deleteEndpointErr error
	resetErr          error
	markErr           error
	claimErr          error

	lastDeliveryQuery *DeliveryListQuery
}

func (e *errFakeRepo) CreateDelivery(ctx context.Context, d *webhookdomain.WebhookDelivery) error {
	if e.createDeliveryErr != nil {
		return e.createDeliveryErr
	}
	return e.fakeRepo.CreateDelivery(ctx, d)
}

func (e *errFakeRepo) CreateEndpoint(ctx context.Context, ep *webhookdomain.WebhookEndpoint) error {
	if e.createEndpointErr != nil {
		return e.createEndpointErr
	}
	return e.fakeRepo.CreateEndpoint(ctx, ep)
}

func (e *errFakeRepo) UpdateEndpoint(ctx context.Context, ep *webhookdomain.WebhookEndpoint) error {
	if e.updateEndpointErr != nil {
		return e.updateEndpointErr
	}
	return e.fakeRepo.UpdateEndpoint(ctx, ep)
}

func (e *errFakeRepo) DeleteEndpoint(ctx context.Context, id uint) error {
	if e.deleteEndpointErr != nil {
		return e.deleteEndpointErr
	}
	return e.fakeRepo.DeleteEndpoint(ctx, id)
}

func (e *errFakeRepo) ResetDeliveryForRedeliver(ctx context.Context, id uint) error {
	if e.resetErr != nil {
		return e.resetErr
	}
	return e.fakeRepo.ResetDeliveryForRedeliver(ctx, id)
}

func (e *errFakeRepo) MarkDeliveryFailure(ctx context.Context, id uint, attempt int, httpStatus int, durationMs int64, lastError string, dead bool, nextRetryAt *time.Time) error {
	if e.markErr != nil {
		return e.markErr
	}
	return e.fakeRepo.MarkDeliveryFailure(ctx, id, attempt, httpStatus, durationMs, lastError, dead, nextRetryAt)
}

func (e *errFakeRepo) ClaimDueDeliveries(ctx context.Context, now time.Time, limit int) ([]webhookdomain.WebhookDelivery, error) {
	if e.claimErr != nil {
		return nil, e.claimErr
	}
	return e.fakeRepo.ClaimDueDeliveries(ctx, now, limit)
}

func (e *errFakeRepo) ListDeliveries(ctx context.Context, query DeliveryListQuery) ([]webhookdomain.WebhookDelivery, int64, error) {
	e.lastDeliveryQuery = &query
	return e.fakeRepo.ListDeliveries(ctx, query)
}

func newErrService(repo *errFakeRepo, deliverer Deliverer) *Service {
	svc := NewService(repo, nil)
	if deliverer != nil {
		svc.SetDeliverer(deliverer)
	}
	return svc
}

func TestSetClockDrivesEnvelopeTimestamp(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{ticket: &models.Ticket{ID: 3}}}
	repo.endpoints = []webhookdomain.WebhookEndpoint{{ID: 1, Active: true}}
	svc := newErrService(repo, &fakeDeliverer{})
	fixed := time.Date(2026, 9, 13, 8, 30, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return fixed })

	svc.EnqueueEvent(context.Background(), EventTicketCreated, "ticket:3", "evt-clock")
	if repo.createdCount != 1 {
		t.Fatalf("expected 1 delivery, got %d", repo.createdCount)
	}
	if !strings.Contains(repo.deliveries[0].Payload, `"created_at":"2026-09-13T08:30:00Z"`) {
		t.Fatalf("clock override must drive created_at: %s", repo.deliveries[0].Payload)
	}
}

func TestEnqueueEventSkipsWhenListEndpointsFails(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{listErr: errors.New("read replica down"), ticket: &models.Ticket{ID: 1}}}
	svc := newErrService(repo, &fakeDeliverer{})
	svc.EnqueueEvent(context.Background(), EventTicketCreated, "ticket:1", "evt-1")
	if repo.createdCount != 0 {
		t.Fatal("endpoint listing failure must skip enqueue")
	}
}

func TestEnqueueEventSkipsEndpointWhenCreateDeliveryFails(t *testing.T) {
	repo := &errFakeRepo{
		fakeRepo:          &fakeRepo{ticket: &models.Ticket{ID: 1}},
		createDeliveryErr: errors.New("disk full"),
	}
	repo.endpoints = []webhookdomain.WebhookEndpoint{{ID: 1, Active: true}}
	svc := newErrService(repo, &fakeDeliverer{})
	svc.EnqueueEvent(context.Background(), EventTicketCreated, "ticket:1", "evt-1")
	if repo.createdCount != 0 {
		t.Fatal("failed delivery row must not be counted")
	}
}

func TestEnqueueEventMatchesSubscribedEventWithWhitespace(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{ticket: &models.Ticket{ID: 1}}}
	repo.endpoints = []webhookdomain.WebhookEndpoint{
		{ID: 1, Active: true, Events: " call.started , ticket.created "},
		{ID: 2, Active: true, Events: "call.started"},
	}
	svc := newErrService(repo, &fakeDeliverer{})
	svc.EnqueueEvent(context.Background(), EventTicketCreated, "ticket:1", "evt-1")
	if repo.createdCount != 1 || repo.deliveries[0].EndpointID != 1 {
		t.Fatalf("only the subscribing endpoint must match: %d rows", repo.createdCount)
	}
}

func TestBuildPayloadRejectsMalformedAggregates(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{ticket: &models.Ticket{ID: 1}, sessionErr: errors.New("sess down")}}
	repo.endpoints = []webhookdomain.WebhookEndpoint{{ID: 1, Active: true}}
	svc := newErrService(repo, &fakeDeliverer{})

	cases := []struct{ name, aggregate string }{
		{"non-numeric ticket id", "ticket:abc"},
		{"unsupported aggregate", "order:5"},
		{"session snapshot failure", "conversation:sess-1"},
	}
	for _, tc := range cases {
		svc.EnqueueEvent(context.Background(), EventTicketCreated, tc.aggregate, "evt-x")
		if repo.createdCount != 0 {
			t.Fatalf("%s: payload failure must skip enqueue", tc.name)
		}
	}
}

func TestTruncatePayloadCapsAt64KiB(t *testing.T) {
	small := []byte(`{"ok":true}`)
	if got := truncatePayload(small); got != string(small) {
		t.Fatalf("small payloads pass through, got %q", got)
	}
	big := make([]byte, payloadTruncateLimit+10)
	for i := range big {
		big[i] = 'x'
	}
	if got := truncatePayload(big); len(got) != payloadTruncateLimit {
		t.Fatalf("oversize payload must truncate to %d, got %d", payloadTruncateLimit, len(got))
	}
}

func TestDeliverOneShotTruncatesPersistedPayload(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{}}
	svc := newErrService(repo, &fakeDeliverer{})
	huge := strings.Repeat("y", payloadTruncateLimit)
	err := svc.DeliverOneShot(context.Background(), "https://ops.example.com", "", "automation.call_webhook", "", map[string]interface{}{"blob": huge})
	if err != nil {
		t.Fatalf("one shot: %v", err)
	}
	if len(repo.deliveries[0].Payload) != payloadTruncateLimit {
		t.Fatalf("persisted payload must cap at %d, got %d", payloadTruncateLimit, len(repo.deliveries[0].Payload))
	}
}

func TestValidateEndpointURLRequiresHost(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{}}
	svc := newErrService(repo, nil)
	if _, _, err := svc.CreateEndpoint(context.Background(), EndpointRequest{Name: "x", URL: "not-a-url"}); err == nil || !strings.Contains(err.Error(), "invalid url") {
		t.Fatalf("url without host must fail, got %v", err)
	}
}

func TestCreateEndpointValidationMatrix(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{}}
	svc := newErrService(repo, nil)

	if _, _, err := svc.CreateEndpoint(context.Background(), EndpointRequest{Name: "   ", URL: "https://ok.example.com"}); err == nil {
		t.Fatal("blank name must fail")
	}

	// 空事件串与纯逗号串都合法；归并后空段被剔除
	inactive := false
	ep, secret, err := svc.CreateEndpoint(context.Background(), EndpointRequest{
		Name: "gated", URL: "https://ok.example.com", Events: "ticket.created,,", Active: &inactive,
	})
	if err != nil {
		t.Fatalf("create with sparse events: %v", err)
	}
	if ep.Active {
		t.Fatal("active=false pointer must be respected")
	}
	if ep.Events != "ticket.created" {
		t.Fatalf("events normalized = %q", ep.Events)
	}
	if len(secret) != 64 {
		t.Fatalf("secret = %q", secret)
	}

	broken := &errFakeRepo{fakeRepo: &fakeRepo{}, createEndpointErr: errors.New("insert refused")}
	if _, _, err := newErrService(broken, nil).CreateEndpoint(context.Background(), EndpointRequest{Name: "x", URL: "https://ok.example.com"}); err == nil {
		t.Fatal("repo create failure must surface")
	}
}

func TestUpdateEndpointFullMatrix(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{}}
	repo.endpoints = []webhookdomain.WebhookEndpoint{{ID: 1, Name: "old", URL: "https://old.example.com", Active: true}}
	svc := newErrService(repo, nil)
	ctx := context.Background()

	// 目标不存在
	if _, err := svc.UpdateEndpoint(ctx, 99, EndpointRequest{Name: "n"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing endpoint must return ErrNotFound, got %v", err)
	}
	// 坏 URL
	if _, err := svc.UpdateEndpoint(ctx, 1, EndpointRequest{URL: "ftp://nope"}); err == nil {
		t.Fatal("invalid update url must fail")
	}
	// 坏事件
	if _, err := svc.UpdateEndpoint(ctx, 1, EndpointRequest{Events: "nope.event"}); err == nil {
		t.Fatal("invalid update events must fail")
	}
	// repo 更新失败
	repo.updateEndpointErr = errors.New("update refused")
	if _, err := svc.UpdateEndpoint(ctx, 1, EndpointRequest{Name: "n"}); err == nil {
		t.Fatal("repo update failure must surface")
	}
	repo.updateEndpointErr = nil

	off := false
	updated, err := svc.UpdateEndpoint(ctx, 1, EndpointRequest{
		Name:        "new",
		URL:         "https://new.example.com",
		Events:      " ticket.created ",
		Description: "desc",
		Active:      &off,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "new" || updated.URL != "https://new.example.com" || updated.Events != "ticket.created" || updated.Description != "desc" || updated.Active {
		t.Fatalf("update fields mismatch: %+v", updated)
	}
}

func TestListGetDeleteEndpointPassThrough(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{}}
	repo.endpoints = []webhookdomain.WebhookEndpoint{{ID: 2, Name: "two"}}
	svc := newErrService(repo, nil)
	ctx := context.Background()

	if eps, err := svc.ListEndpoints(ctx); err != nil || len(eps) != 1 {
		t.Fatalf("ListEndpoints = (%v, %v)", eps, err)
	}
	if ep, err := svc.GetEndpoint(ctx, 2); err != nil || ep.ID != 2 {
		t.Fatalf("GetEndpoint = (%+v, %v)", ep, err)
	}
	if _, err := svc.GetEndpoint(ctx, 404); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetEndpoint missing = %v", err)
	}
	if err := svc.DeleteEndpoint(ctx, 2); err != nil {
		t.Fatalf("DeleteEndpoint: %v", err)
	}
	repo.deleteEndpointErr = errors.New("fk constraint")
	if err := svc.DeleteEndpoint(ctx, 2); err == nil {
		t.Fatal("delete failure must surface")
	}
}

func TestRotateEndpointSecretErrors(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{}}
	repo.endpoints = []webhookdomain.WebhookEndpoint{{ID: 1, Secret: "old"}}
	svc := newErrService(repo, nil)

	if _, _, err := svc.RotateEndpointSecret(context.Background(), 404); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rotate missing = %v", err)
	}
	repo.updateEndpointErr = errors.New("rotate write refused")
	if _, _, err := svc.RotateEndpointSecret(context.Background(), 1); err == nil {
		t.Fatal("rotate write failure must surface")
	}
}

func TestTestEndpointSurfacesLookupAndPersistErrors(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{}}
	svc := newErrService(repo, &fakeDeliverer{})
	if _, err := svc.TestEndpoint(context.Background(), 404); !errors.Is(err, ErrNotFound) {
		t.Fatalf("test missing endpoint = %v", err)
	}

	repo.createDeliveryErr = errors.New("audit write refused")
	repo.endpoints = []webhookdomain.WebhookEndpoint{{ID: 1, Active: true}}
	if _, err := svc.TestEndpoint(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "audit write refused") {
		t.Fatalf("persist failure must surface, got %v", err)
	}
}

func TestDeliverOneShotWithoutDelivererFails(t *testing.T) {
	svc := newErrService(&errFakeRepo{fakeRepo: &fakeRepo{}}, nil)
	err := svc.DeliverOneShot(context.Background(), "https://x.example.com", "", "automation.call_webhook", "", nil)
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("missing deliverer must fail fast, got %v", err)
	}
}

func TestDeliverOneShotRejectsUnserializablePayload(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{}}
	svc := newErrService(repo, &fakeDeliverer{})
	err := svc.DeliverOneShot(context.Background(), "https://x.example.com", "", "automation.call_webhook", "", map[string]interface{}{
		"bad": make(chan int),
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("unserializable payload must fail marshal, got %v", err)
	}
	if repo.createdCount != 0 {
		t.Fatal("marshal failure must not persist an audit row")
	}
}

func TestDeliverOneShotHTTPStatusFailure(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{}}
	deliverer := &fakeDeliverer{results: []DeliveryResult{{Success: false, StatusCode: 502}}}
	svc := newErrService(repo, deliverer)
	err := svc.DeliverOneShot(context.Background(), "https://x.example.com", "", "automation.call_webhook", "", nil)
	if err == nil || !strings.Contains(err.Error(), "http status 502") {
		t.Fatalf("status-only failure must name the status, got %v", err)
	}
}

func TestListDeliveriesNormalizesPagination(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{}}
	svc := newErrService(repo, nil)

	cases := []struct {
		in    DeliveryListQuery
		wantP int
		wantS int
	}{
		{in: DeliveryListQuery{}, wantP: 1, wantS: 20},
		{in: DeliveryListQuery{Page: -3, PageSize: -1}, wantP: 1, wantS: 20},
		{in: DeliveryListQuery{Page: 4, PageSize: 500}, wantP: 4, wantS: 100},
	}
	for _, tc := range cases {
		if _, _, err := svc.ListDeliveries(context.Background(), tc.in); err != nil {
			t.Fatalf("ListDeliveries: %v", err)
		}
		if repo.lastDeliveryQuery.Page != tc.wantP || repo.lastDeliveryQuery.PageSize != tc.wantS {
			t.Fatalf("query = %+v, want page=%d size=%d", *repo.lastDeliveryQuery, tc.wantP, tc.wantS)
		}
	}
}

func TestRedeliverSurfacesLookupAndResetErrors(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{deliveries: []webhookdomain.WebhookDelivery{{ID: 1}}}}
	svc := newErrService(repo, nil)
	if _, err := svc.RedeliverDelivery(context.Background(), 404); !errors.Is(err, ErrNotFound) {
		t.Fatalf("redeliver missing = %v", err)
	}
	repo.resetErr = errors.New("reset refused")
	if _, err := svc.RedeliverDelivery(context.Background(), 1); err == nil {
		t.Fatal("reset failure must surface")
	}
}

func TestProcessDueDeliveriesGuards(t *testing.T) {
	repo := &errFakeRepo{fakeRepo: &fakeRepo{}}

	// 未配置 deliverer：直接 0
	if got := newErrService(repo, nil).ProcessDueDeliveries(context.Background(), time.Now()); got != 0 {
		t.Fatalf("nil deliverer must process 0, got %d", got)
	}
	// claim 失败：0
	repo.claimErr = errors.New("claim lost the race")
	if got := newErrService(repo, &fakeDeliverer{}).ProcessDueDeliveries(context.Background(), time.Now()); got != 0 {
		t.Fatalf("claim failure must process 0, got %d", got)
	}
	repo.claimErr = nil
	// 无到期行：0
	if got := newErrService(repo, &fakeDeliverer{}).ProcessDueDeliveries(context.Background(), time.Now()); got != 0 {
		t.Fatalf("empty queue must process 0, got %d", got)
	}
}

func TestProcessDueDeliveriesRecordsMarkFailureError(t *testing.T) {
	repo := &errFakeRepo{
		fakeRepo: &fakeRepo{
			endpoints:  []webhookdomain.WebhookEndpoint{{ID: 1, Active: true}},
			deliveries: []webhookdomain.WebhookDelivery{{ID: 1, EndpointID: 1, Status: webhookdomain.WebhookDeliveryStatusPending, Payload: "{}"}},
		},
		markErr: errors.New("failure write refused"),
	}
	deliverer := &fakeDeliverer{results: []DeliveryResult{{Success: false, StatusCode: 500, Error: "boom"}}}
	svc := newErrService(repo, deliverer)

	// 记录失败不改变处理计数，但也不 panic
	if got := svc.ProcessDueDeliveries(context.Background(), time.Now()); got != 1 {
		t.Fatalf("processed = %d, want 1", got)
	}
}
