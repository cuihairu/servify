package realtime

// 本文件通过 seams.go 的包级 seam 驱动 pion WebRTC、websocket writePump
// 以及 router 轮询适配器 ticker 的错误分支覆盖。
// auth 模块的 seam 覆盖已随模块迁移至 internal/modules/auth/application/seams_test.go；
// customer 模块 repo 错误传播段已随刀 18 拆回 modules/customer/application。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

// hscovSetSeam 替换一个包级变量并在测试结束后还原。
func hscovSetSeam[T any](t *testing.T, slot *T, value T) {
	t.Helper()
	old := *slot
	*slot = value
	t.Cleanup(func() { *slot = old })
}

// --- WebRTC：CreateAnswer / SetLocalDescription / Close 错误 ---

func TestHSSeamsWebRTCHandleOfferErrors(t *testing.T) {
	s, _ := newUnitWebRTCService(t)

	offerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("offerer: %v", err)
	}
	defer offerer.Close() //nolint:errcheck
	if _, err := offerer.CreateDataChannel("d", nil); err != nil {
		t.Fatalf("data channel: %v", err)
	}
	offer, err := offerer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if err := offerer.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription: %v", err)
	}

	t.Run("CreateAnswer error", func(t *testing.T) {
		hscovSetSeam(t, &hookPeerConnectionCreateAnswer, func(*webrtc.PeerConnection, *webrtc.AnswerOptions) (webrtc.SessionDescription, error) {
			return webrtc.SessionDescription{}, errors.New("boom: answer")
		})
		if _, err := s.HandleOffer("sess-answer-err", offer); err == nil || !strings.Contains(err.Error(), "failed to create answer") {
			t.Fatalf("expected create answer error, got %v", err)
		}
	})

	t.Run("SetLocalDescription error", func(t *testing.T) {
		hscovSetSeam(t, &hookPeerConnectionSetLocalDescription, func(*webrtc.PeerConnection, webrtc.SessionDescription) error {
			return errors.New("boom: set local")
		})
		if _, err := s.HandleOffer("sess-local-err", offer); err == nil || !strings.Contains(err.Error(), "failed to set local description") {
			t.Fatalf("expected set local description error, got %v", err)
		}
	})
}

func TestHSSeamsWebRTCCloseError(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	if _, err := s.CreatePeerConnection("sess-close-err"); err != nil {
		t.Fatalf("CreatePeerConnection: %v", err)
	}
	hscovSetSeam(t, &hookPeerConnectionClose, func(*webrtc.PeerConnection) error {
		return errors.New("boom: close")
	})
	if err := s.CloseConnection("sess-close-err"); err != nil {
		t.Fatalf("CloseConnection must swallow close error: %v", err)
	}
	if s.GetConnectionCount() != 0 {
		t.Fatalf("connection must still be removed after close error, got %d", s.GetConnectionCount())
	}
}

// --- websocket writePump ping ticker ---

func TestHSSeamsWebSocketPingTickerPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = upgrader.Upgrade(w, r, nil)
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):]+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	client := &WebSocketClient{
		ID: "c-hscov-ping", SessionID: "s-hscov-ping", Conn: conn,
		Send:         make(chan WebSocketMessage),
		pingInterval: 10 * time.Millisecond,
	}
	done := make(chan struct{})
	go func() {
		client.writePump()
		close(done)
	}()
	// 已关闭连接上的 ping 必然失败：writePump 只能经由 ticker 的 ping 分支退出。
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("writePump did not exit via ping branch")
	}
}

// --- router 轮询适配器 ticker ---
