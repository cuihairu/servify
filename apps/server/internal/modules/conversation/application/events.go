package application

import (
	"context"
	"fmt"
	"time"

	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/eventbus"
)

const (
	ConversationCreatedEventName         = "conversation.created"
	ConversationMessageReceivedEventName = "conversation.message_received"
	ConversationClosedEventName          = "conversation.closed"
)

type ConversationEvent struct {
	eventbus.BaseEvent
	ConversationID string
	Payload        interface{}
}

// NewConversationEvent 从 auth ctx 盖事件租户章，与仓储写入同源
// （infra 各 repo 同以 TenantIDFromContext 落 tenant_id）；系统 worker
// 无租户 ctx 时留空，与该场景下的落库口径一致。
func NewConversationEvent(ctx context.Context, name string, conversationID string, payload interface{}) ConversationEvent {
	return ConversationEvent{
		BaseEvent: eventbus.BaseEvent{
			EventID:          fmt.Sprintf("%s-%s-%d", name, conversationID, time.Now().UnixNano()),
			EventName:        name,
			EventOccurredAt:  time.Now(),
			EventTenantID:    platformauth.TenantIDFromContext(ctx),
			EventAggregateID: fmt.Sprintf("conversation:%s", conversationID),
		},
		ConversationID: conversationID,
		Payload:        payload,
	}
}

type EventPublisher interface {
	Publish(ctx context.Context, event eventbus.Event) error
}
