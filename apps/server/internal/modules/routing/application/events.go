package application

import (
	"context"
	"fmt"
	"sync"
	"time"

	"servify/apps/server/internal/platform/eventbus"
)

const (
	RoutingAgentAssignedEventName     = "routing.agent_assigned"
	RoutingTransferCompletedEventName = "routing.transfer_completed"
)

type RoutingEvent struct {
	eventbus.BaseEvent
	SessionID string
	Payload   interface{}
}

func NewRoutingEvent(name string, sessionID string, payload interface{}) RoutingEvent {
	return RoutingEvent{
		BaseEvent: eventbus.BaseEvent{
			EventID:          fmt.Sprintf("%s-%s-%d", name, sessionID, time.Now().UnixNano()),
			EventName:        name,
			EventOccurredAt:  time.Now(),
			EventAggregateID: fmt.Sprintf("routing:%s", sessionID),
		},
		SessionID: sessionID,
		Payload:   payload,
	}
}

type EventPublisher interface {
	Publish(ctx context.Context, event eventbus.Event) error
}

// BufferPublisher 事务作用域事件缓冲：事务内的 Publish 只入队，由事务
// 拥有者在提交成功后取走并发到真实总线。sqlite 单写者下，事务内同步
// 发布会让事件订阅者的写库与外层事务互相等锁——订阅者等到 busy 超时
// 报 SQLITE_BUSY，HTTP 请求也被拖满 5 秒（CI 验收脚本的直转/派发全挂
// 即此）。事务内一律先攒后发，回滚则丢弃。
type BufferPublisher struct {
	target  EventPublisher
	mu      sync.Mutex
	pending []eventbus.Event
}

// NewBufferPublisher 构造；target 为 nil 时 Events 仍可取，只是无人可发。
func NewBufferPublisher(target EventPublisher) *BufferPublisher {
	return &BufferPublisher{target: target}
}

// Publish 只入队，不触库、不阻塞。
func (b *BufferPublisher) Publish(_ context.Context, event eventbus.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending = append(b.pending, event)
	return nil
}

// Events 取走攒下的事件（一次性，取后清空）。
func (b *BufferPublisher) Events() []eventbus.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	events := b.pending
	b.pending = nil
	return events
}
