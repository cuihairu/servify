package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/automation/application"
	webhookapp "servify/apps/server/internal/modules/webhook/application"
)

type stubDispatcherRepo struct {
	webhookapp.Repository
	endpoints  []models.WebhookEndpoint
	deliveries []models.WebhookDelivery
}

func (s *stubDispatcherRepo) ListEndpoints(ctx context.Context) ([]models.WebhookEndpoint, error) {
	return s.endpoints, nil
}

func (s *stubDispatcherRepo) CreateDelivery(ctx context.Context, d *models.WebhookDelivery) error {
	d.ID = uint(len(s.deliveries) + 1)
	s.deliveries = append(s.deliveries, *d)
	return nil
}

func (s *stubDispatcherRepo) ClaimDueDeliveries(ctx context.Context, now time.Time, limit int) ([]models.WebhookDelivery, error) {
	return nil, nil
}

type okDeliverer struct{ got webhookapp.DeliveryRequest }

func (d *okDeliverer) Deliver(ctx context.Context, req webhookapp.DeliveryRequest) webhookapp.DeliveryResult {
	d.got = req
	return webhookapp.DeliveryResult{Success: true, StatusCode: 200}
}

func TestAutomationWebhookDispatcherSignsAndPersists(t *testing.T) {
	repo := &stubDispatcherRepo{}
	svc := webhookapp.NewService(repo, nil)
	svc.SetDeliverer(&okDeliverer{})
	dispatcher := NewAutomationWebhookDispatcher(svc)

	err := dispatcher.Dispatch(context.Background(), "https://ops.example.com/hook", "secret-1", map[string]interface{}{"id": float64(3)})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if repo.deliveries[0].EventName != "automation.call_webhook" || repo.deliveries[0].EndpointID != 0 {
		t.Fatalf("unexpected audit row: %+v", repo.deliveries[0])
	}
	if !strings.Contains(repo.deliveries[0].Payload, `"automation.call_webhook"`) {
		t.Fatalf("payload should carry the automation event name: %s", repo.deliveries[0].Payload)
	}
}

func TestAutomationWebhookDispatcherImplementsInterface(t *testing.T) {
	var _ application.WebhookDispatcher = NewAutomationWebhookDispatcher(webhookapp.NewService(&stubDispatcherRepo{}, nil))
}
