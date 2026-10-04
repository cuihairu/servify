package delivery

import (
	"context"
	"encoding/json"
	"time"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	"servify/apps/server/internal/modules/conversation/domain"
	routingapp "servify/apps/server/internal/modules/routing/application"
	ticketapp "servify/apps/server/internal/modules/ticket/application"
	"servify/apps/server/internal/platform/eventbus"
)

// EventBusSubscriber 把会话服务过程事件投影为 ConversationEvent 流水
// （Service Timeline，V1.0 收敛 B1，docs/v1-convergence-plan.md §3.1）。
// 只做只读投影，不进入业务写路径；ticket.* 事件借工单的 SessionID
// （来源会话）归入同一会话的 Timeline（B1-2）。
type EventBusSubscriber struct {
	repo conversationapp.ConversationEventRepository
}

func NewEventBusSubscriber(repo conversationapp.ConversationEventRepository) *EventBusSubscriber {
	return &EventBusSubscriber{repo: repo}
}

type timelineProjection struct {
	conversationID func(event eventbus.Event) string
	actorType      string
	summary        string
}

func (s *EventBusSubscriber) Register(bus eventbus.Bus) {
	if bus == nil || s == nil || s.repo == nil {
		return
	}
	registrations := map[string]timelineProjection{
		conversationapp.ConversationCreatedEventName: {
			conversationID: conversationIDFromEvent,
			actorType:      "system",
			summary:        "会话创建",
		},
		routingapp.RoutingAgentAssignedEventName: {
			conversationID: conversationIDFromRoutingEvent,
			actorType:      "routing",
			summary:        "分配坐席",
		},
		routingapp.RoutingTransferCompletedEventName: {
			conversationID: conversationIDFromRoutingEvent,
			actorType:      "routing",
			summary:        "转接完成",
		},
		ticketapp.TicketCreatedEventName: {
			conversationID: conversationIDFromTicketEvent,
			actorType:      "system",
			summary:        "创建工单",
		},
		ticketapp.TicketAssignedEventName: {
			conversationID: conversationIDFromTicketEvent,
			actorType:      "system",
			summary:        "工单分配",
		},
		ticketapp.TicketClosedEventName: {
			conversationID: conversationIDFromTicketEvent,
			actorType:      "system",
			summary:        "关闭工单",
		},
	}
	for name, projection := range registrations {
		eventName := name
		bus.Subscribe(eventName, eventbus.HandlerFunc(func(ctx context.Context, event eventbus.Event) error {
			conversationID := projection.conversationID(event)
			if conversationID == "" {
				return nil
			}
			occurredAt := event.OccurredAt()
			if occurredAt.IsZero() {
				occurredAt = time.Now()
			}
			record := domain.ConversationEvent{
				ConversationID: conversationID,
				EventType:      eventName,
				ActorType:      projection.actorType,
				Summary:        projection.summary,
				Payload:        encodeEventPayload(event),
				OccurredAt:     occurredAt,
			}
			return s.repo.Append(ctx, record)
		}))
	}
}

// conversationIDFromEvent 从 conversation 模块事件的聚合 ID
// （"conversation:<id>"）提取会话 ID。
func conversationIDFromEvent(event eventbus.Event) string {
	return trimAggregatePrefix(event.AggregateID(), "conversation:")
}

// conversationIDFromRoutingEvent 从 routing 模块事件的聚合 ID
// （"routing:<sessionID>"）提取会话（session）ID。
func conversationIDFromRoutingEvent(event eventbus.Event) string {
	return trimAggregatePrefix(event.AggregateID(), "routing:")
}

// conversationIDFromTicketEvent 从工单事件提取来源会话 ID：工单事件聚合
// 是 ticket:<id>，会话关联走事件携带的 SessionID；无来源会话（邮件建单
// 等）返回空，投影跳过（Timeline 只覆盖会话视角）。
func conversationIDFromTicketEvent(event eventbus.Event) string {
	ticketEvent, ok := event.(ticketapp.TicketEvent)
	if !ok || ticketEvent.SessionID == nil {
		return ""
	}
	return *ticketEvent.SessionID
}

func trimAggregatePrefix(aggregateID string, prefix string) string {
	if len(aggregateID) > len(prefix) && aggregateID[:len(prefix)] == prefix {
		return aggregateID[len(prefix):]
	}
	return ""
}

func encodeEventPayload(event eventbus.Event) string {
	encoded, err := json.Marshal(map[string]any{
		"event_id":    event.ID(),
		"occurred_at": event.OccurredAt(),
	})
	if err != nil {
		return ""
	}
	return string(encoded)
}
