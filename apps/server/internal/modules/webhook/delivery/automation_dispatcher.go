package delivery

import (
	"context"

	"servify/apps/server/internal/modules/automation/application"
	webhookapp "servify/apps/server/internal/modules/webhook/application"
)

// AutomationWebhookDispatcher 实现 automation.WebhookDispatcher：
// 复用 webhook 投递器对触发器目标 URL 做单次签名投递。审计行
// endpoint_id=0、event_name=automation.call_webhook，可从投递日志手动重放。
type AutomationWebhookDispatcher struct {
	service *webhookapp.Service
}

var _ application.WebhookDispatcher = (*AutomationWebhookDispatcher)(nil)

func NewAutomationWebhookDispatcher(service *webhookapp.Service) *AutomationWebhookDispatcher {
	return &AutomationWebhookDispatcher{service: service}
}

func (d *AutomationWebhookDispatcher) Dispatch(ctx context.Context, url, secret string, payload map[string]interface{}) error {
	return d.service.DeliverOneShot(ctx, url, secret, "automation.call_webhook", "", payload)
}
