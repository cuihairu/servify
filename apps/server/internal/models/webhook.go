package models

import webhookdomain "servify/apps/server/internal/modules/webhook/domain"

// WebhookEndpoint 出站 Webhook 订阅端点。定义已迁至 modules/webhook/domain，
// 此处保留类型别名供 legacy 引用方使用。
type WebhookEndpoint = webhookdomain.WebhookEndpoint

// WebhookDelivery 一次出站投递的审计记录。定义已迁至 modules/webhook/domain，
// 此处保留类型别名供 legacy 引用方使用。
type WebhookDelivery = webhookdomain.WebhookDelivery

// 投递状态常量（定义已迁至 modules/webhook/domain，此处保留同名常量供 legacy 引用方使用）。
const (
	WebhookDeliveryStatusPending = webhookdomain.WebhookDeliveryStatusPending
	WebhookDeliveryStatusSuccess = webhookdomain.WebhookDeliveryStatusSuccess
	WebhookDeliveryStatusFailed  = webhookdomain.WebhookDeliveryStatusFailed
	WebhookDeliveryStatusDead    = webhookdomain.WebhookDeliveryStatusDead
)
