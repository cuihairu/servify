package models

import (
	"time"

	"gorm.io/gorm"
)

// WebhookEndpoint 出站 Webhook 订阅端点（开放平台）。
// Secret 只在创建/轮换响应中出现一次，查询响应永不回显。
type WebhookEndpoint struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	TenantID    string         `json:"tenant_id"`
	WorkspaceID string         `json:"workspace_id"`
	Name        string         `gorm:"not null" json:"name"`
	URL         string         `gorm:"column:url;not null" json:"url"`
	Secret      string         `gorm:"not null" json:"-"`
	Events      string         `json:"events"` // 逗号分隔事件名，空 = 订阅全部已支持事件
	Description string         `json:"description"`
	Active      bool           `gorm:"default:true" json:"active"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// WebhookDelivery 一次出站投递的审计记录。
// 状态机：pending --worker--> success / failed(带 next_retry_at 回到 pending 语义) / dead。
type WebhookDelivery struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	EndpointID  uint       `gorm:"index" json:"endpoint_id"`
	EventName   string     `gorm:"not null" json:"event_name"`
	EventID     string     `json:"event_id"`
	AggregateID string     `json:"aggregate_id"`
	Status      string     `gorm:"default:'pending';index:idx_webhook_deliveries_status_next_retry,priority:1" json:"status"` // pending|success|failed|dead
	Attempt     int        `gorm:"default:0" json:"attempt"`
	HTTPStatus  int        `gorm:"column:http_status" json:"http_status"`
	DurationMs  int64      `json:"duration_ms"`
	LastError   string     `gorm:"type:text" json:"last_error"`
	Payload     string     `gorm:"type:text" json:"payload,omitempty"` // 实际发送的 JSON，超长截断
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeliveredAt *time.Time `json:"delivered_at"`
	NextRetryAt *time.Time `gorm:"index:idx_webhook_deliveries_status_next_retry,priority:2" json:"next_retry_at"`
}

// 投递状态常量
const (
	WebhookDeliveryStatusPending = "pending"
	WebhookDeliveryStatusSuccess = "success"
	WebhookDeliveryStatusFailed  = "failed"
	WebhookDeliveryStatusDead    = "dead"
)
