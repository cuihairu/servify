package application

import (
	"context"
	"fmt"
	"time"

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

func NewConversationEvent(name string, conversationID string, payload interface{}) ConversationEvent {
	return ConversationEvent{
		BaseEvent: eventbus.BaseEvent{
			EventID:          fmt.Sprintf("%s-%s-%d", name, conversationID, time.Now().UnixNano()),
			EventName:        name,
			EventOccurredAt:  time.Now(),
			EventAggregateID: fmt.Sprintf("conversation:%s", conversationID),
		},
		ConversationID: conversationID,
		Payload:        payload,
	}
}

type EventPublisher interface {
	Publish(ctx context.Context, event eventbus.Event) error
}
