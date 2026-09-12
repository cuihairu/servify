package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/webhook/application"
	webhookdelivery "servify/apps/server/internal/modules/webhook/delivery"

	"github.com/gin-gonic/gin"
)

type fakeWebhookHandlerService struct {
	endpoints       []models.WebhookEndpoint
	deliveries      []models.WebhookDelivery
	createdSecret   string
	createErr       error
	notFoundActions bool
}

func (f *fakeWebhookHandlerService) ListEndpoints(ctx context.Context) ([]models.WebhookEndpoint, error) {
	return f.endpoints, nil
}
func (f *fakeWebhookHandlerService) CreateEndpoint(ctx context.Context, req *webhookdelivery.EndpointCreateRequest) (*models.WebhookEndpoint, string, error) {
	if f.createErr != nil {
		return nil, "", f.createErr
	}
	ep := &models.WebhookEndpoint{ID: 1, Name: req.Name, URL: req.URL, Secret: f.createdSecret, Active: true}
	f.endpoints = append(f.endpoints, *ep)
	return ep, f.createdSecret, nil
}
func (f *fakeWebhookHandlerService) UpdateEndpoint(ctx context.Context, id uint, req *webhookdelivery.EndpointUpdateRequest) (*models.WebhookEndpoint, error) {
	if f.notFoundActions {
		return nil, application.ErrNotFound
	}
	return &f.endpoints[0], nil
}
func (f *fakeWebhookHandlerService) DeleteEndpoint(ctx context.Context, id uint) error {
	if f.notFoundActions {
		return application.ErrNotFound
	}
	return nil
}
func (f *fakeWebhookHandlerService) RotateEndpointSecret(ctx context.Context, id uint) (*models.WebhookEndpoint, string, error) {
	return &f.endpoints[0], "rotated-secret", nil
}
func (f *fakeWebhookHandlerService) TestEndpoint(ctx context.Context, id uint) (*models.WebhookDelivery, error) {
	return &models.WebhookDelivery{ID: 9, EndpointID: id, EventName: "ping", Status: models.WebhookDeliveryStatusSuccess}, nil
}
func (f *fakeWebhookHandlerService) ListDeliveries(ctx context.Context, query application.DeliveryListQuery) ([]models.WebhookDelivery, int64, error) {
	return f.deliveries, int64(len(f.deliveries)), nil
}
func (f *fakeWebhookHandlerService) RedeliverDelivery(ctx context.Context, id uint) (*models.WebhookDelivery, error) {
	if f.notFoundActions {
		return nil, application.ErrNotFound
	}
	return &models.WebhookDelivery{ID: id, Status: models.WebhookDeliveryStatusPending}, nil
}
func (f *fakeWebhookHandlerService) SupportedEvents() []string {
	return application.SupportedEvents()
}

func newWebhookTestRouter(t *testing.T, svc webhookdelivery.HandlerService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	RegisterWebhookRoutes(api, NewWebhookHandler(svc))
	return r
}

func doWebhookRequest(t *testing.T, r *gin.Engine, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestWebhookRoutesCRUDAndSecretHygiene(t *testing.T) {
	svc := &fakeWebhookHandlerService{createdSecret: "plain-secret-value"}
	r := newWebhookTestRouter(t, svc)

	// create：secret 明文一次性返回
	rec := doWebhookRequest(t, r, http.MethodPost, "/api/v1/webhooks",
		[]byte(`{"name":"ops","url":"https://ops.example.com/hook","events":"ticket.created"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Endpoint models.WebhookEndpoint `json:"endpoint"`
		Secret   string                 `json:"secret"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Secret != "plain-secret-value" {
		t.Fatalf("create response should carry the plaintext secret once")
	}

	// list：secret 永不回显
	rec = doWebhookRequest(t, r, http.MethodGet, "/api/v1/webhooks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("plain-secret-value")) {
		t.Fatal("list response must not leak the secret")
	}

	// update / rotate / test / events / deliveries / redeliver
	rec = doWebhookRequest(t, r, http.MethodPut, "/api/v1/webhooks/1", []byte(`{"name":"renamed"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d", rec.Code)
	}
	rec = doWebhookRequest(t, r, http.MethodPost, "/api/v1/webhooks/1/secret", nil)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("rotated-secret")) {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
	rec = doWebhookRequest(t, r, http.MethodPost, "/api/v1/webhooks/1/test", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("test: %d", rec.Code)
	}
	rec = doWebhookRequest(t, r, http.MethodGet, "/api/v1/webhooks/events", nil)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"events":[`)) {
		t.Fatalf("events: %d %s", rec.Code, rec.Body.String())
	}
	rec = doWebhookRequest(t, r, http.MethodGet, "/api/v1/webhooks/deliveries?endpoint_id=1&status=success&page=1&page_size=10", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("deliveries: %d", rec.Code)
	}
	rec = doWebhookRequest(t, r, http.MethodPost, "/api/v1/webhooks/deliveries/7/redeliver", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("redeliver: %d", rec.Code)
	}
	rec = doWebhookRequest(t, r, http.MethodDelete, "/api/v1/webhooks/1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
}

func TestWebhookRoutesNotFoundMapping(t *testing.T) {
	svc := &fakeWebhookHandlerService{notFoundActions: true}
	r := newWebhookTestRouter(t, svc)

	if rec := doWebhookRequest(t, r, http.MethodDelete, "/api/v1/webhooks/99", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing should be 404, got %d", rec.Code)
	}
	if rec := doWebhookRequest(t, r, http.MethodPost, "/api/v1/webhooks/deliveries/99/redeliver", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("redeliver missing should be 404, got %d", rec.Code)
	}
}

func TestWebhookRoutesValidationErrors(t *testing.T) {
	svc := &fakeWebhookHandlerService{}
	r := newWebhookTestRouter(t, svc)

	// 非法 id
	if rec := doWebhookRequest(t, r, http.MethodPut, "/api/v1/webhooks/not-a-number", []byte(`{}`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id should be 400, got %d", rec.Code)
	}
	// 非法 JSON
	if rec := doWebhookRequest(t, r, http.MethodPost, "/api/v1/webhooks", []byte(`{bad`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json should be 400, got %d", rec.Code)
	}
}
