package realtime

import (
	"sync"
	"testing"

	"github.com/pion/webrtc/v4"
)

// P0-3 回归：广播分支对慢消费者 close(client.Send)+剔除 map 后，该连接的
// readPump 仍可能在运行，其 webrtc-offer 帧的裸 `c.Send <- answer` 会撞上
// 已关闭通道（send on closed channel）→ 读泵 goroutine panic、gin.Recovery
// 覆盖不到 → 进程崩溃。修复后读泵侧发送统一走 trySend（与 close/delete 同
// 持 h.mutex）：已剔除/注销连接不再发送，未剔除发送被序列化在 close 前。

func newRegisteredTestClient(h *WebSocketHub, id, sessionID string) *WebSocketClient {
	client := &WebSocketClient{ID: id, SessionID: sessionID, Send: make(chan WebSocketMessage, 1), Hub: h}
	h.clients[id] = client
	return client
}

func TestTrySendRegisteredClientDelivers(t *testing.T) {
	h := NewWebSocketHub()
	client := newRegisteredTestClient(h, "c1", "s1")

	client.trySend(WebSocketMessage{Type: "webrtc-answer", SessionID: "s1"})
	select {
	case got := <-client.Send:
		if got.Type != "webrtc-answer" {
			t.Fatalf("got type %q", got.Type)
		}
	default:
		t.Fatal("expected message delivered")
	}
}

func TestTrySendQueueFullDropsNoPanic(t *testing.T) {
	h := NewWebSocketHub()
	client := newRegisteredTestClient(h, "c2", "s2")

	client.trySend(WebSocketMessage{Type: "webrtc-answer"}) // fills the 1-buf channel
	client.trySend(WebSocketMessage{Type: "webrtc-answer"}) // must drop, not block/panic
}

// 修复前的崩溃场景：读泵侧发送恰在广播剔除（close+delete）之后 → panic。
func TestTrySendAfterBroadcastEvictionNoPanic(t *testing.T) {
	h := NewWebSocketHub()
	client := newRegisteredTestClient(h, "c3", "s3")

	// 模拟广播慢消费者分支：close + delete（Run 循环在 h.mutex 内执行）
	h.mutex.Lock()
	close(client.Send)
	delete(h.clients, client.ID)
	h.mutex.Unlock()

	// 修复前这里 panic（send on closed channel）；修复后 map 查无即返回
	client.trySend(WebSocketMessage{Type: "webrtc-answer", SessionID: "s3"})
}

// 并发交错：读泵侧 trySend 与广播侧 close+delete+重建通道反复竞争，不得 panic。
func TestTrySendConcurrentCloseRaceNoPanic(t *testing.T) {
	h := NewWebSocketHub()
	client := newRegisteredTestClient(h, "c4", "s4")

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				client.trySend(WebSocketMessage{Type: "webrtc-answer", SessionID: "s4"})
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			h.mutex.Lock()
			close(client.Send)
			delete(h.clients, client.ID)
			client.Send = make(chan WebSocketMessage, 1)
			h.clients[client.ID] = client
			h.mutex.Unlock()
		}
		close(stop)
	}()
	wg.Wait()
}

// handleWebRTCOffer 全链路：rtc 服务返回 answer 后读泵侧发送撞上已关闭通道
// 不得 panic（修复前 :511 裸 send 崩溃）。
func TestHandleWebRTCOfferAfterEvictionNoPanic(t *testing.T) {
	h := NewWebSocketHub()
	h.mutex.Lock()
	h.rtcService = &unitRTCService{}
	h.mutex.Unlock()
	client := newRegisteredTestClient(h, "c5", "s5")

	h.mutex.Lock()
	close(client.Send)
	delete(h.clients, client.ID)
	h.mutex.Unlock()

	client.handleWebRTCOffer(WebSocketMessage{
		Type:      "webrtc-offer",
		Data:      webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "offer-sdp"},
		SessionID: "s5",
	})
}
