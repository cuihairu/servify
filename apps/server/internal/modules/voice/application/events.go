package application

import (
	"context"
	"time"

	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/eventbus"
)

const (
	CallStartedEventName = "call.started"
	CallHeldEventName    = "call.held"
	CallResumedEventName = "call.resumed"
	CallTransferredName  = "call.transferred"
	CallEndedEventName   = "call.ended"
)

type Publisher interface {
	Publish(ctx context.Context, event eventbus.Event) error
}

type VoiceEvent struct {
	eventbus.BaseEvent
	Payload interface{}
}

// NewVoiceEvent 从 auth ctx 盖事件租户章，与仓储写入同源；系统 worker
// 无租户 ctx 时留空。
func NewVoiceEvent(ctx context.Context, name, aggregateID string, payload interface{}) VoiceEvent {
	return VoiceEvent{
		BaseEvent: eventbus.BaseEvent{
			EventID:          aggregateID + ":" + name,
			EventName:        name,
			EventOccurredAt:  time.Now(),
			EventTenantID:    platformauth.TenantIDFromContext(ctx),
			EventAggregateID: aggregateID,
		},
		Payload: payload,
	}
}
