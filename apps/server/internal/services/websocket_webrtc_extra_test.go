package services

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

func TestWebSocket_ReadPumpCloseAndPong(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := NewWebSocketHub()
	go hub.Run()

	router := gin.New()
	router.GET("/ws", hub.HandleWebSocket)
	server := httptest.NewServer(router)
	defer server.Close()
	wsURL := "ws" + server.URL[len("http"):] + "/ws?session_id=s-close"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// unsolicited pong triggers the server's pong handler (read deadline refresh)
	if err := conn.WriteControl(websocket.PongMessage, nil, time.Now().Add(time.Second)); err != nil {
		t.Fatalf("pong: %v", err)
	}

	// graceful close with normal code triggers the unexpected-close error branch
	msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done")
	if err := conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second)); err != nil {
		t.Fatalf("close frame: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	conn.Close()
}

func TestWebSocket_WritePumpSendClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := NewWebSocketHub()
	go hub.Run()

	router := gin.New()
	router.GET("/ws", hub.HandleWebSocket)
	server := httptest.NewServer(router)
	defer server.Close()
	wsURL := "ws" + server.URL[len("http"):] + "/ws?session_id=s-wp"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// wait for registration
	deadline := time.Now().Add(2 * time.Second)
	var client *WebSocketClient
	for time.Now().Before(deadline) {
		hub.mutex.RLock()
		for _, c := range hub.clients {
			client = c
		}
		hub.mutex.RUnlock()
		if client != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if client == nil {
		t.Fatal("client was not registered")
	}

	// flood the 256-slot send buffer; the un-read TCP connection eventually
	// blocks writePump, the hub then closes the channel on overflow, and the
	// writePump close-frame branch runs
	for i := 0; i < 4000; i++ {
		hub.broadcast <- WebSocketMessage{Type: "flood", SessionID: "s-wp", Data: strings.Repeat("x", 256)}
	}
	time.Sleep(500 * time.Millisecond)
}

func TestWebSocket_HandleTextMessage_PersistError(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	writer := &unitConversationWriter{persistErr: errors.New("db down")}
	hub.SetConversationMessageWriter(writer)
	client := &WebSocketClient{ID: "c", SessionID: "s", Send: make(chan WebSocketMessage, 2), Hub: hub}
	hub.register <- client
	time.Sleep(20 * time.Millisecond)

	client.handleTextMessage(WebSocketMessage{
		Type: "text-message", Data: map[string]interface{}{"content": "hi"}, Timestamp: time.Now(),
	})

	select {
	case <-client.Send:
	case <-time.After(1 * time.Second):
		t.Fatal("expected broadcast despite persist failure")
	}
}

func TestWebSocket_WebRTCErrorLogs(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	rtc := &unitRTCService{answerErr: errors.New("bad answer"), candErr: errors.New("bad candidate")}
	hub.SetWebRTCService(rtc)

	client := &WebSocketClient{ID: "c", SessionID: "s", Hub: hub}
	client.handleWebRTCAnswer(WebSocketMessage{
		Type: "webrtc-answer", Data: map[string]interface{}{"type": "answer", "sdp": "x"},
	})
	client.handleWebRTCCandidate(WebSocketMessage{
		Type: "webrtc-candidate", Data: map[string]interface{}{"candidate": "c"},
	})
}

func TestWebSocket_PayloadMarshalErrors(t *testing.T) {
	badMap := map[string]interface{}{"sdp": make(chan int)}
	if _, err := asSessionDescription(badMap); err == nil {
		t.Fatal("expected session description marshal error")
	}
	badCand := map[string]interface{}{"candidate": make(chan int)}
	if _, err := asICECandidate(badCand); err == nil {
		t.Fatal("expected ICE candidate marshal error")
	}
}

func TestWebSocket_HandleWebRTCAnswerUnsupportedPayload(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	hub.SetWebRTCService(&unitRTCService{})
	client := &WebSocketClient{ID: "c", SessionID: "s", Hub: hub}
	// unsupported payload type logs decode failure and returns
	client.handleWebRTCAnswer(WebSocketMessage{Type: "webrtc-answer", Data: 3.14})
	client.handleWebRTCCandidate(WebSocketMessage{Type: "webrtc-candidate", Data: 3.14})
}

// loopback WebRTC connection to exercise data-channel callbacks end-to-end
func TestWebRTC_LoopbackDataChannel(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	hub := s.wsHub

	// capture service-side signaling pushed through the hub
	signaling := make(chan WebSocketMessage, 32)
	client := &WebSocketClient{ID: "c-loop", SessionID: "sess-loop", Send: signaling, Hub: hub}
	hub.register <- client
	time.Sleep(20 * time.Millisecond)

	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("peer: %v", err)
	}
	defer peer.Close() //nolint:errcheck

	peerDC, err := peer.CreateDataChannel("loop", nil)
	if err != nil {
		t.Fatalf("data channel: %v", err)
	}
	opened := make(chan struct{})
	peerDC.OnOpen(func() { close(opened) })

	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	if err := peer.SetLocalDescription(offer); err != nil {
		t.Fatalf("local: %v", err)
	}

	answer, err := s.HandleOffer("sess-loop", *peer.LocalDescription())
	if err != nil {
		t.Fatalf("HandleOffer: %v", err)
	}
	if err := peer.SetRemoteDescription(*answer); err != nil {
		t.Fatalf("remote: %v", err)
	}

	peer.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		_ = s.HandleICECandidate("sess-loop", c.ToJSON())
	})

	// forward service-side candidates to the peer
	go func() {
		for msg := range signaling {
			if msg.Type != "webrtc-candidate" {
				continue
			}
			data, ok := msg.Data.(map[string]interface{})
			if !ok {
				continue
			}
			raw, _ := data["candidate"].(json.RawMessage)
			if len(raw) == 0 {
				continue
			}
			var init webrtc.ICECandidateInit
			if err := json.Unmarshal(raw, &init); err != nil {
				continue
			}
			_ = peer.AddICECandidate(init)
		}
	}()

	select {
	case <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("data channel did not open within timeout")
	}

	// peer sends a message; service forwards it via hub
	if err := peerDC.SendText("ping"); err != nil {
		t.Fatalf("send: %v", err)
	}
	// allow the message to be delivered so the service-side OnMessage fires
	time.Sleep(300 * time.Millisecond)

	// wait for ICE gathering to complete (end-of-candidates callback)
	conn, cerr := s.getConnectionBySessionID("sess-loop")
	if cerr == nil {
		gatherDeadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(gatherDeadline) {
			if conn.PeerConnection.ICEGatheringState() == webrtc.ICEGatheringStateComplete {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// service-side data channel should now exist and accept sends
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := s.SendDataChannelMessage("sess-loop", "pong"); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := s.CloseConnection("sess-loop"); err != nil {
		t.Fatalf("close: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
}
