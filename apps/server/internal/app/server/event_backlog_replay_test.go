package server

// replayEventBusBacklog（todo 197）装配侧分支测试：nil bus 与非 Redis 实现
// 跳过；miniredis 正常路径重放积压；Redis 不可达时告警上抛给调用方忽略。

import (
	"context"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"

	"servify/apps/server/internal/platform/eventbus"
)

type backlogLogHook struct{ entries []logrus.Entry }

func (h *backlogLogHook) Fire(e *logrus.Entry) error {
	h.entries = append(h.entries, *e)
	return nil
}
func (h *backlogLogHook) Levels() []logrus.Level {
	return []logrus.Level{logrus.WarnLevel, logrus.InfoLevel}
}

func (h *backlogLogHook) has(level logrus.Level, substr string) bool {
	for _, entry := range h.entries {
		if entry.Level == level && strings.Contains(entry.Message, substr) {
			return true
		}
	}
	return false
}

func TestReplayEventBusBacklogSkipsNonRedisBuses(t *testing.T) {
	logger := logrus.New()
	replayEventBusBacklog(nil, logger)
	replayEventBusBacklog(eventbus.NewInMemoryBus(), logger)
}

func TestReplayEventBusBacklogReplaysAndLogs(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bus := eventbus.NewRedisBus(client, logrus.New())
	t.Cleanup(func() { _ = bus.Close() })

	handler := &eventbusBacklogProbe{}
	bus.Subscribe("conversation.created", handler)
	if err := client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: "servify:events:conversation.created",
		Values: map[string]interface{}{
			"id": "evt-1", "name": "conversation.created", "data": "{}",
			"occurred_at": "1700000000", "tenant_id": "t1", "aggregate": "conv-1",
		},
	}).Err(); err != nil {
		t.Fatalf("seed stream: %v", err)
	}

	hook := &backlogLogHook{}
	logger := logrus.New()
	logger.AddHook(hook)
	replayEventBusBacklog(bus, logger)

	if len(handler.events) != 1 || handler.events[0].ID() != "evt-1" {
		t.Fatalf("backlog not replayed through wiring: %+v", handler.events)
	}
	if !hook.has(logrus.InfoLevel, "event backlog replayed 1 stream entries") {
		t.Fatalf("expected replay count log, got %+v", hook.entries)
	}
}

func TestReplayEventBusBacklogWarnsOnUnreachableRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bus := eventbus.NewRedisBus(client, logrus.New())
	t.Cleanup(func() { _ = bus.Close() })
	bus.Subscribe("conversation.created", &eventbusBacklogProbe{})
	mr.Close()

	hook := &backlogLogHook{}
	logger := logrus.New()
	logger.AddHook(hook)
	replayEventBusBacklog(bus, logger)

	if !hook.has(logrus.WarnLevel, "event backlog replay failed") {
		t.Fatalf("expected warn log on replay failure, got %+v", hook.entries)
	}
}

type eventbusBacklogProbe struct{ events []eventbus.Event }

func (p *eventbusBacklogProbe) Handle(_ context.Context, event eventbus.Event) error {
	p.events = append(p.events, event)
	return nil
}
