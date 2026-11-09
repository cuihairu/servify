package eventbus

// 启动期 catch-up 重放（todo 197）：XAdd 落流成功后 pub/sub 通知丢失
// （实例崩溃、订阅重连窗口）会让流内条目无人派发——通知与流写入非事务。
// ReplayStreamBacklog 在订阅注册完成后按检查点增量重放积压条目；下游
// 消费者按 event_id 幂等去重（timeline Append 吞 ErrDuplicatedKey），
// at-least-once 重放安全。

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

type replayRecordingHandler struct {
	events []Event
}

func (h *replayRecordingHandler) Handle(_ context.Context, event Event) error {
	h.events = append(h.events, event)
	return nil
}

// newReplayTestBus 直接结构体构造（不启动 subscribeLoop 协程）：重放派发
// 是同步调用，不依赖 pub/sub 订阅就绪。
func newReplayTestBus(t *testing.T, mr *miniredis.Miniredis) (*RedisBus, *replayRecordingHandler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	bus := &RedisBus{
		client:   redis.NewClient(&redis.Options{Addr: mr.Addr()}),
		logger:   logrus.New(),
		handlers: make(map[string][]Handler),
		ctx:      ctx,
		cancel:   cancel,
		readyCh:  make(chan struct{}),
	}
	handler := &replayRecordingHandler{}
	bus.Subscribe("conversation.created", handler)
	return bus, handler
}

func seedBacklogEntry(t *testing.T, client *redis.Client, streamKey, eventID string) {
	t.Helper()
	err := client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: streamKey,
		ID:     "*",
		Values: map[string]interface{}{
			"id":          eventID,
			"name":        "conversation.created",
			"data":        `{"conversation_id":"conv-1"}`,
			"occurred_at": "1700000000",
			"tenant_id":   "tenant-a",
			"aggregate":   "conv-1",
		},
	}).Err()
	if err != nil {
		t.Fatalf("seed stream %s: %v", streamKey, err)
	}
}

// TestRedisBusReplayStreamBacklogDispatchesBacklog 重放只触达已注册事件的
// 流：两条积压全量派发（顺序保持、事件字段透传），未注册事件的流不动。
func TestRedisBusReplayStreamBacklogDispatchesBacklog(t *testing.T) {
	mr := miniredis.RunT(t)
	bus, handler := newReplayTestBus(t, mr)

	createdStream := "servify:events:conversation.created"
	seedBacklogEntry(t, bus.client, createdStream, "evt-b1")
	seedBacklogEntry(t, bus.client, createdStream, "evt-b2")
	seedBacklogEntry(t, bus.client, "servify:events:conversation.closed", "evt-x")

	replayed, err := bus.ReplayStreamBacklog(context.Background())
	if err != nil {
		t.Fatalf("ReplayStreamBacklog: %v", err)
	}
	if replayed != 2 {
		t.Fatalf("replayed = %d, want 2 (only registered stream entries)", replayed)
	}
	if len(handler.events) != 2 {
		t.Fatalf("handler got %d events, want 2", len(handler.events))
	}
	first := handler.events[0]
	if first.ID() != "evt-b1" || first.Name() != "conversation.created" ||
		first.TenantID() != "tenant-a" || first.AggregateID() != "conv-1" {
		t.Fatalf("unexpected first event: id=%s name=%s tenant=%s aggregate=%s",
			first.ID(), first.Name(), first.TenantID(), first.AggregateID())
	}
	if handler.events[1].ID() != "evt-b2" {
		t.Fatalf("stream order not preserved: second = %s", handler.events[1].ID())
	}
}

// TestRedisBusReplayStreamBacklogCheckpointIncremental 检查点语义：首跑全量
// 重放后落检查点，二跑零重复；此后新增条目增量补放。
func TestRedisBusReplayStreamBacklogCheckpointIncremental(t *testing.T) {
	mr := miniredis.RunT(t)
	bus, handler := newReplayTestBus(t, mr)

	createdStream := "servify:events:conversation.created"
	seedBacklogEntry(t, bus.client, createdStream, "evt-c1")

	replayed, err := bus.ReplayStreamBacklog(context.Background())
	if err != nil || replayed != 1 {
		t.Fatalf("first replay = (%d, %v), want (1, nil)", replayed, err)
	}

	replayed, err = bus.ReplayStreamBacklog(context.Background())
	if err != nil || replayed != 0 {
		t.Fatalf("second replay = (%d, %v), want (0, nil) — checkpoint must skip replayed entries", replayed, err)
	}
	if len(handler.events) != 1 {
		t.Fatalf("handler got %d events after two replays, want 1", len(handler.events))
	}

	seedBacklogEntry(t, bus.client, createdStream, "evt-c2")
	replayed, err = bus.ReplayStreamBacklog(context.Background())
	if err != nil || replayed != 1 {
		t.Fatalf("incremental replay = (%d, %v), want (1, nil)", replayed, err)
	}
	if len(handler.events) != 2 || handler.events[1].ID() != "evt-c2" {
		t.Fatalf("incremental replay missed new entry: %+v", handler.events)
	}
}

// TestRedisBusReplayStreamBacklogNilReceiverAndUnreachable nil 接收者安全；
// Redis 不可达时上抛错误（调用方告警不阻断启动）。
func TestRedisBusReplayStreamBacklogNilReceiverAndUnreachable(t *testing.T) {
	var nilBus *RedisBus
	replayed, err := nilBus.ReplayStreamBacklog(context.Background())
	if replayed != 0 || err != nil {
		t.Fatalf("nil receiver = (%d, %v), want (0, nil)", replayed, err)
	}

	mr := miniredis.RunT(t)
	bus, _ := newReplayTestBus(t, mr)
	mr.Close()
	if _, err := bus.ReplayStreamBacklog(context.Background()); err == nil {
		t.Fatal("expected error when redis is unreachable")
	}
}
