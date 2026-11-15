package realtime

// Codex 深审 Major（2026-11-13）回归：注册确认 + 应答有界重试。
//
// Major-2：register 发送成功只代表 Run 接收了 client，不保证已写入 clients
// map；读写泵若先于入表启动，首帧 webrtc-offer 的应答会被 trySend 的成员
// 检查当作未注册连接丢弃。修复后 HandleWebSocket 等 registered 关闭（Run
// 入表后关闭）才启动读写泵。
//
// Major-3：队列满时静默丢唯一 SDP 应答且无恢复面。修复后 trySend 有界重试
// 并返回投递结果，handleWebRTCOffer 对持续满队显式记错。

import (
	"testing"
	"time"
)

// registered 关闭即已入表：确认后成员检查必命中，trySend 直接投递。
func TestRegisterAckImpliesMapInsert(t *testing.T) {
	h := NewWebSocketHub()
	go h.Run()

	client := &WebSocketClient{
		ID:         "ack-1",
		SessionID:  "s-ack",
		Send:       make(chan WebSocketMessage, 1),
		Hub:        h,
		registered: make(chan struct{}),
	}
	h.register <- client
	select {
	case <-client.registered:
	case <-time.After(2 * time.Second):
		t.Fatal("registered not closed in time")
	}

	h.mutex.RLock()
	_, inMap := h.clients[client.ID]
	h.mutex.RUnlock()
	if !inMap {
		t.Fatal("ack fired but client missing from clients map")
	}

	if !client.trySend(WebSocketMessage{Type: "webrtc-answer", SessionID: "s-ack"}) {
		t.Fatal("trySend must deliver right after ack")
	}
}

// 队列满 → 锁外等待重试 → 排空后投递成功（瞬时背压被吸收）。
func TestTrySendRetriesUntilQueueDrained(t *testing.T) {
	h := NewWebSocketHub()
	client := newRegisteredTestClient(h, "retry-1", "s-retry")

	first := WebSocketMessage{Type: "fill", SessionID: "s-retry"}
	if !client.trySend(first) {
		t.Fatal("first send into empty buffer must succeed")
	}

	// 10ms 后排空一条：重试预算 0/50/100ms，任一后续尝试都能放进
	go func() {
		time.Sleep(10 * time.Millisecond)
		<-client.Send
	}()

	start := time.Now()
	ok := client.trySend(WebSocketMessage{Type: "webrtc-answer", SessionID: "s-retry"})
	if !ok {
		t.Fatal("expected delivery within retry budget after drain")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("retry took %v, budget is %v", elapsed, trySendRetryInterval*time.Duration(trySendAttempts))
	}
}

// 持续满队：有界失败（false + 非阻塞预算），不再无限等待或 panic。
func TestTrySendQueueFullBoundedFailure(t *testing.T) {
	h := NewWebSocketHub()
	client := newRegisteredTestClient(h, "retry-2", "s-retry2")

	_ = client.trySend(WebSocketMessage{Type: "fill", SessionID: "s-retry2"}) // 占满 1 缓冲
	start := time.Now()
	if client.trySend(WebSocketMessage{Type: "webrtc-answer", SessionID: "s-retry2"}) {
		t.Fatal("expected bounded failure on persistently full queue")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("bounded failure took %v", elapsed)
	}
	// 队列里应只剩先占位的 fill 消息：重试未塞入新消息，也未消耗旧消息
	got := <-client.Send
	if got.Type != "fill" {
		t.Fatalf("queue head=%q want fill", got.Type)
	}
	if len(client.Send) != 0 {
		t.Fatalf("unexpected extra messages: %d", len(client.Send))
	}
}

// 已剔除连接：立即 false，不做无谓重试。
func TestTrySendUnregisteredImmediateFalse(t *testing.T) {
	h := NewWebSocketHub()
	client := newRegisteredTestClient(h, "retry-3", "s-retry3")

	h.mutex.Lock()
	delete(h.clients, client.ID)
	h.mutex.Unlock()

	if client.trySend(WebSocketMessage{Type: "webrtc-answer", SessionID: "s-retry3"}) {
		t.Fatal("unregistered client must not deliver")
	}
	select {
	case <-client.Send:
		t.Fatal("nothing may be written after eviction")
	default:
	}
}
