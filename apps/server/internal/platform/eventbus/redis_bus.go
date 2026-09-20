package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

const (
	// Stream key pattern: servify:events:{event_name}
	eventStreamPattern = "servify:events:%s"
	// Consumer group for reliable delivery
	consumerGroup = "servify-workers"
	// Pub/Sub channel for real-time notification
	eventPubSubChannel = "servify:events:pubsub"
	// Default stream max length
	streamMaxLen = 10000
)

// RedisBus implements a persistent event bus using Redis Streams.
// Events are written to Redis Streams for persistence and published
// to Pub/Sub for real-time delivery to subscribers.
type RedisBus struct {
	client *redis.Client
	logger *logrus.Logger

	// Local handler registry (for pub/sub dispatch)
	mu       sync.RWMutex
	handlers map[string][]Handler

	// Subscription management
	ctx       context.Context
	cancel    context.CancelFunc
	readyOnce sync.Once
	readyCh   chan struct{}
}

// NewRedisBus creates a new Redis-backed event bus.
// It returns a Bus that persists events to Redis Streams and
// delivers them to local subscribers via Pub/Sub.
func NewRedisBus(client *redis.Client, logger *logrus.Logger) *RedisBus {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	ctx, cancel := context.WithCancel(context.Background())
	bus := &RedisBus{
		client:   client,
		logger:   logger,
		handlers: make(map[string][]Handler),
		ctx:      ctx,
		cancel:   cancel,
		readyCh:  make(chan struct{}),
	}

	// Start background subscriber
	go bus.subscribeLoop()

	return bus
}

// Subscribe registers a handler for an event name.
// The handler will be called when events of this type are published.
func (b *RedisBus) Subscribe(eventName string, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventName] = append(b.handlers[eventName], handler)
}

// Publish publishes an event to Redis.
// The event is written to a Redis Stream (persistent) and also
// announced via Pub/Sub for real-time delivery. It first waits for this
// instance's pub/sub subscription to be acknowledged: a notification
// published earlier would be dropped (the stream entry persists, but no
// subscriber dispatches it, so local handlers never run), so early callers
// block briefly instead of silently losing the event.
func (b *RedisBus) Publish(ctx context.Context, event Event) error {
	if event == nil {
		return nil
	}

	if !b.waitUntilReady(ctx) {
		return fmt.Errorf("event bus not ready: %w", ctx.Err())
	}

	// Serialize event data
	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("serialize event: %w", err)
	}

	// Build stream values
	streamKey := fmt.Sprintf(eventStreamPattern, event.Name())
	values := map[string]interface{}{
		"id":          event.ID(),
		"name":        event.Name(),
		"data":        string(eventData),
		"occurred_at": event.OccurredAt().Unix(),
		"tenant_id":   event.TenantID(),
		"aggregate":   event.AggregateID(),
	}

	// Add to Redis Stream (persistent)
	messageID, err := b.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		ID:     "*", // Auto-generate ID
		Values: values,
		MaxLen: streamMaxLen,
		Approx: true,
	}).Result()
	if err != nil {
		return fmt.Errorf("publish to stream: %w", err)
	}

	// Also publish to Pub/Sub for real-time notification
	// Don't fail if pub/sub fails - stream write succeeded
	if err := b.client.Publish(ctx, eventPubSubChannel, formatDispatchPayload(streamKey, messageID)).Err(); err != nil {
		b.logger.Warnf("pub/sub publish failed: %v", err)
	}

	b.logger.Debugf("event published: %s (%s)", event.Name(), event.ID())
	return nil
}

// subscribeLoop listens for event notifications and dispatches to handlers.
func (b *RedisBus) subscribeLoop() {
	pubsub := b.client.Subscribe(b.ctx, eventPubSubChannel)
	defer pubsub.Close()
	b.awaitSubscriptionConfirmed(pubsub)

	ch := pubsub.Channel()
	for {
		select {
		case <-b.ctx.Done():
			b.logger.Debug("redis event bus subscribe loop stopped")
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			// msg.Payload contains the stream key and optionally the message ID.
			b.dispatchFromStream(b.ctx, msg.Payload)
		}
	}
}

// pubSubReceiveFn 是包级 seam（默认读取订阅确认消息）。SUBSCRIBE 确认何时
// 到达取决于服务端处理时机，是天然竞态；seam 让测试确定性地覆盖"未确认→
// 重试→确认"路径。通过 RWMutex 读写：其他测试遗留的 subscribeLoop 协程
// 仍在并发调用，裸 var 会构成数据竞争。
var (
	pubSubReceiveMu sync.RWMutex
	pubSubReceiveFn = func(ctx context.Context, pubsub *redis.PubSub) (interface{}, error) {
		return pubsub.Receive(ctx)
	}
)

func pubSubReceive(ctx context.Context, pubsub *redis.PubSub) (interface{}, error) {
	pubSubReceiveMu.RLock()
	defer pubSubReceiveMu.RUnlock()
	return pubSubReceiveFn(ctx, pubsub)
}

// awaitSubscriptionConfirmed marks the bus ready only after the server has
// acknowledged this connection's SUBSCRIBE command.
//
// go-redis v9 PubSub.Subscribe only writes the SUBSCRIBE command and returns
// without waiting for the server acknowledgement, so a Publish issued right
// after Subscribe can race ahead of the subscription and get dropped. Waiting
// for the *redis.Subscription acknowledgement via Receive closes that window
// for this connection. A channel-wide PUBSUB NUMSUB poll would not: the
// subscriber count does not distinguish connections, so in a multi-instance
// deployment one bus's ready signal can be satisfied by another bus's
// subscription while its own SUBSCRIBE is still in flight.
//
// Receive failures (e.g. Redis briefly unreachable during startup) are
// retried on a ticker; go-redis PubSub reconnects transparently. When the
// bus is closing, the wait is abandoned and Close releases Publish waiters
// by closing readyCh.
func (b *RedisBus) awaitSubscriptionConfirmed(pubsub *redis.PubSub) {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		if _, err := pubSubReceive(b.ctx, pubsub); err == nil {
			break
		}
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			continue
		}
	}

	b.readyOnce.Do(func() {
		close(b.readyCh)
	})
}

func (b *RedisBus) waitUntilReady(ctx context.Context) bool {
	select {
	case <-b.readyCh:
		return true
	case <-ctx.Done():
		return false
	}
}

// dispatchFromStream reads events from a stream and calls local handlers.
func (b *RedisBus) dispatchFromStream(ctx context.Context, payload string) {
	streamKey, messageID := parseDispatchPayload(payload)

	// Extract event name from stream key
	eventName := strings.TrimPrefix(streamKey, fmt.Sprintf(eventStreamPattern, ""))

	// Get local handlers for this event
	b.mu.RLock()
	handlers := make([]Handler, 0, len(b.handlers[eventName]))
	handlers = append(handlers, b.handlers[eventName]...)
	b.mu.RUnlock()

	if len(handlers) == 0 {
		return
	}

	streams, err := b.readMessages(ctx, streamKey, messageID)

	if err != nil && err != redis.Nil {
		b.logger.Warnf("read from stream %s: %v", streamKey, err)
		return
	}

	for _, stream := range streams {
		for _, message := range stream.Messages {
			b.dispatchMessage(ctx, eventName, message, handlers)
		}
	}
}

func (b *RedisBus) readMessages(ctx context.Context, streamKey, messageID string) ([]redis.XStream, error) {
	if messageID != "" {
		messages, err := b.client.XRange(ctx, streamKey, messageID, messageID).Result()
		if err != nil {
			return nil, err
		}
		if len(messages) == 0 {
			return nil, redis.Nil
		}
		return []redis.XStream{{Stream: streamKey, Messages: messages}}, nil
	}

	// Backward-compatible fallback for payloads that only include the stream key.
	return b.client.XRead(ctx, &redis.XReadArgs{
		Streams: []string{streamKey, "$"},
		Count:   10,
		Block:   100 * time.Millisecond,
	}).Result()
}

// dispatchMessage deserializes and dispatches a single message to handlers.
func (b *RedisBus) dispatchMessage(ctx context.Context, eventName string, message redis.XMessage, handlers []Handler) {
	// Extract event data
	data, ok := message.Values["data"].(string)
	if !ok {
		b.logger.Warnf("invalid event data in stream message")
		return
	}

	// Create a minimal event wrapper for dispatch
	wrapper := &streamEvent{
		id:          castToString(message.Values["id"]),
		name:        eventName,
		data:        data,
		occurredAt:  time.Unix(castToInt64(message.Values["occurred_at"]), 0),
		tenantID:    castToString(message.Values["tenant_id"]),
		aggregateID: castToString(message.Values["aggregate"]),
	}

	// Call all handlers
	for _, handler := range handlers {
		if err := handler.Handle(ctx, wrapper); err != nil {
			b.logger.Errorf("handler error for event %s: %v", eventName, err)
		}
	}
}

// Close gracefully shuts down the bus. Publish callers still waiting for the
// subscription confirmation are released first: their events proceed to the
// stream write, which fails naturally once the client is shut down.
func (b *RedisBus) Close() error {
	b.readyOnce.Do(func() {
		close(b.readyCh)
	})
	b.cancel()
	return nil
}

// Health returns the health status of the Redis bus.
func (b *RedisBus) Health(ctx context.Context) error {
	return b.client.Ping(ctx).Err()
}

// streamEvent is a wrapper for events read from Redis Streams.
type streamEvent struct {
	id          string
	name        string
	data        string
	occurredAt  time.Time
	tenantID    string
	aggregateID string
}

func (e *streamEvent) ID() string            { return e.id }
func (e *streamEvent) Name() string          { return e.name }
func (e *streamEvent) OccurredAt() time.Time { return e.occurredAt }
func (e *streamEvent) TenantID() string      { return e.tenantID }
func (e *streamEvent) AggregateID() string   { return e.aggregateID }

// Helper functions for type conversion
func castToString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func castToInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case float64:
		return int64(val)
	case string:
		// Try parsing as string
		var i int64
		fmt.Sscanf(val, "%d", &i)
		return i
	default:
		return 0
	}
}

func formatDispatchPayload(streamKey, messageID string) string {
	if messageID == "" {
		return streamKey
	}
	return streamKey + "|" + messageID
}

func parseDispatchPayload(payload string) (streamKey string, messageID string) {
	parts := strings.SplitN(payload, "|", 2)
	streamKey = parts[0]
	if len(parts) == 2 {
		messageID = parts[1]
	}
	return streamKey, messageID
}

// Ensure RedisBus implements Bus interface
var _ Bus = (*RedisBus)(nil)
