package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"

	"servify/apps/server/internal/models"
	routingcontract "servify/apps/server/internal/modules/routing/contract"
	translationdelivery "servify/apps/server/internal/modules/translation/delivery"
	"servify/apps/server/internal/platform/iceturn"
)

// ---- stubs (names distinct from integration-tagged test helpers) ----

type unitConversationWriter struct {
	persistErr     error
	assigned       bool
	assignedErr    error
	recentMessages []models.Message
	persisted      []string
}

func (w *unitConversationWriter) PersistTextMessage(ctx context.Context, sessionID string, content string) error {
	if w.persistErr != nil {
		return w.persistErr
	}
	w.persisted = append(w.persisted, content)
	return nil
}

func (w *unitConversationWriter) HasActiveHumanAgent(ctx context.Context, sessionID string) (bool, error) {
	return w.assigned, w.assignedErr
}

func (w *unitConversationWriter) ListRecentMessages(ctx context.Context, sessionID string, limit int) ([]models.Message, error) {
	return w.recentMessages, nil
}

type unitRTCService struct {
	offer     webrtc.SessionDescription
	offerErr  error
	answerErr error
	candErr   error
}

func (s *unitRTCService) HandleOffer(sessionID string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	s.offer = offer
	if s.offerErr != nil {
		return nil, s.offerErr
	}
	return &webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: "answer-sdp"}, nil
}

func (s *unitRTCService) HandleAnswer(sessionID string, answer webrtc.SessionDescription) error {
	return s.answerErr
}

func (s *unitRTCService) HandleICECandidate(sessionID string, candidate webrtc.ICECandidateInit) error {
	return s.candErr
}

type unitTransferService struct {
	result  *routingcontract.TransferResult
	err     error
	request *routingcontract.TransferRequest
}

func (s *unitTransferService) TransferToHuman(ctx context.Context, req *routingcontract.TransferRequest) (*routingcontract.TransferResult, error) {
	s.request = req
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

type unitAIService struct {
	queryErr       error
	shouldTransfer bool
	processCalls   atomic.Int64
}

func (a *unitAIService) ProcessQuery(ctx context.Context, query string, sessionID string) (*aidelivery.AIResponse, error) {
	a.processCalls.Add(1)
	if a.queryErr != nil {
		return nil, a.queryErr
	}
	return &aidelivery.AIResponse{Content: "ai:" + query, Confidence: 0.5, Source: "test"}, nil
}

func (a *unitAIService) ShouldTransferToHuman(query string, sessionHistory []models.Message) bool {
	return a.shouldTransfer
}

func (a *unitAIService) GetSessionSummary(messages []models.Message) (string, error) {
	return "summary", nil
}

func waitForMessage(t *testing.T, ch chan WebSocketMessage) WebSocketMessage {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message")
		return WebSocketMessage{}
	}
}

// ---- tests ----

func TestWebSocketHub_RunLifecycle(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()

	c1 := &WebSocketClient{ID: "c1", SessionID: "s1", Send: make(chan WebSocketMessage, 1), Hub: hub}
	hub.register <- c1
	time.Sleep(20 * time.Millisecond)
	if hub.GetClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", hub.GetClientCount())
	}

	hub.SendToSession("s1", WebSocketMessage{Type: "hello"})
	got := waitForMessage(t, c1.Send)
	if got.Type != "hello" {
		t.Fatalf("unexpected message: %+v", got)
	}

	hub.unregister <- c1
	time.Sleep(20 * time.Millisecond)
	if hub.GetClientCount() != 0 {
		t.Fatalf("expected 0 clients, got %d", hub.GetClientCount())
	}
	hub.unregister <- c1 // second unregister must not panic

	// full client channel is closed and dropped by Run
	c2 := &WebSocketClient{ID: "c2", SessionID: "s2", Send: make(chan WebSocketMessage), Hub: hub}
	hub.register <- c2
	time.Sleep(20 * time.Millisecond)
	hub.SendToSession("s2", WebSocketMessage{Type: "full"})
	time.Sleep(50 * time.Millisecond)
	if hub.GetClientCount() != 0 {
		t.Fatalf("expected full client to be dropped, got %d", hub.GetClientCount())
	}
}

func TestWebSocket_Setters(t *testing.T) {
	hub := NewWebSocketHub()
	hub.SetAIService(&unitAIService{})
	hub.SetSessionTransferService(&unitTransferService{})
	hub.SetConversationMessageWriter(&unitConversationWriter{})
	hub.SetWebRTCService(&unitRTCService{})
	if hub.aiService == nil || hub.transferService == nil || hub.conversationWriter == nil || hub.rtcService == nil {
		t.Fatal("expected all dependencies injected")
	}
}

func TestWebSocket_AsSessionDescriptionVariants(t *testing.T) {
	desc := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "sdp"}
	if got, err := asSessionDescription(desc); err != nil || got.SDP != "sdp" {
		t.Fatalf("value: %v %v", got, err)
	}
	if got, err := asSessionDescription(&desc); err != nil || got.SDP != "sdp" {
		t.Fatalf("pointer: %v %v", got, err)
	}
	if _, err := asSessionDescription((*webrtc.SessionDescription)(nil)); err == nil {
		t.Fatal("expected nil pointer error")
	}
	if got, err := asSessionDescription(map[string]interface{}{"type": "offer", "sdp": "map-sdp"}); err != nil || got.SDP != "map-sdp" {
		t.Fatalf("map: %v %v", got, err)
	}
	if _, err := asSessionDescription(map[string]interface{}{"type": 1}); err == nil {
		t.Fatal("expected decode error for bad map")
	}
	if _, err := asSessionDescription(42); err == nil {
		t.Fatal("expected unsupported payload error")
	}
}

func TestWebSocket_AsICECandidateVariants(t *testing.T) {
	cand := webrtc.ICECandidateInit{Candidate: "c1"}
	if got, err := asICECandidate(cand); err != nil || got.Candidate != "c1" {
		t.Fatalf("value: %v %v", got, err)
	}
	if got, err := asICECandidate(&cand); err != nil || got.Candidate != "c1" {
		t.Fatalf("pointer: %v %v", got, err)
	}
	if _, err := asICECandidate((*webrtc.ICECandidateInit)(nil)); err == nil {
		t.Fatal("expected nil pointer error")
	}
	if got, err := asICECandidate(map[string]interface{}{"candidate": "mapped"}); err != nil || got.Candidate != "mapped" {
		t.Fatalf("map: %v %v", got, err)
	}
	if _, err := asICECandidate(map[string]interface{}{"candidate": make(chan int)}); err == nil {
		t.Fatal("expected decode error for bad map")
	}
	if _, err := asICECandidate("nope"); err == nil {
		t.Fatal("expected unsupported payload error")
	}
}

func TestWebSocket_PersistTextMessageVariants(t *testing.T) {
	hub := NewWebSocketHub()
	writer := &unitConversationWriter{}
	hub.SetConversationMessageWriter(writer)
	client := &WebSocketClient{ID: "c", SessionID: "s", Hub: hub}

	cases := []struct {
		name string
		data interface{}
		want []string
	}{
		{"map content", map[string]interface{}{"content": "hello"}, []string{"hello"}},
		{"string content", "plain", []string{"plain"}},
		{"other type", 42, nil},
		{"empty content", map[string]interface{}{"content": "  "}, nil},
	}
	for _, tc := range cases {
		if err := client.persistTextMessage(WebSocketMessage{Type: "text-message", Data: tc.data, Timestamp: time.Now()}); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	if len(writer.persisted) != 2 || writer.persisted[0] != "hello" || writer.persisted[1] != "plain" {
		t.Fatalf("unexpected persisted: %+v", writer.persisted)
	}

	// writer error surfaces
	writer.persistErr = errors.New("db down")
	if err := client.persistTextMessage(WebSocketMessage{Type: "text-message", Data: "x", Timestamp: time.Now()}); err == nil {
		t.Fatal("expected persist error")
	}

	// no writer configured -> no-op
	hub2 := NewWebSocketHub()
	client2 := &WebSocketClient{ID: "c", SessionID: "s", Hub: hub2}
	if err := client2.persistTextMessage(WebSocketMessage{Type: "text-message", Data: "x"}); err != nil {
		t.Fatalf("no writer: %v", err)
	}
}

func TestWebSocket_HandleTextMessageFlow(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()

	ai := &unitAIService{}
	transfer := &unitTransferService{}
	writer := &unitConversationWriter{}
	hub.SetAIService(ai)
	hub.SetSessionTransferService(transfer)
	hub.SetConversationMessageWriter(writer)

	client := &WebSocketClient{ID: "c", SessionID: "s-flow", Send: make(chan WebSocketMessage, 4), Hub: hub}
	hub.register <- client
	time.Sleep(20 * time.Millisecond)

	client.handleTextMessage(WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "hi"}})

	// broadcast echo 与 ai-response 分属 hub 中转与 AI goroutine 两条路径，
	// 到达顺序不受保证，按集合断言两类消息各一条。
	seen := map[string]int{}
	for i := 0; i < 2; i++ {
		seen[waitForMessage(t, client.Send).Type]++
	}
	if seen["text-message"] != 1 || seen["ai-response"] != 1 {
		t.Fatalf("expected one echo and one ai-response, got %v", seen)
	}
	if ai.processCalls.Load() != 1 {
		t.Fatalf("expected 1 ai call, got %d", ai.processCalls.Load())
	}
}

// unitConversionService 客户侧推荐转化匹配的测试替身。
type unitConversionService struct {
	calls     atomic.Int64
	sessionID string
	content   string
	err       error
}

func (u *unitConversionService) MatchSuggestionConversion(ctx context.Context, sessionID string, content string) error {
	u.calls.Add(1)
	u.sessionID = sessionID
	u.content = content
	return u.err
}

func TestWebSocket_HandleTextMessageSuggestionConversion(t *testing.T) {
	t.Run("matched after persist with broadcast intact", func(t *testing.T) {
		hub := NewWebSocketHub()
		go hub.Run()
		conv := &unitConversionService{}
		hub.SetSuggestionConversionService(conv)

		client := &WebSocketClient{ID: "c", SessionID: "s-conv", Send: make(chan WebSocketMessage, 2), Hub: hub}
		hub.register <- client
		time.Sleep(20 * time.Millisecond)

		client.handleTextMessage(WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "重置密码"}})

		if conv.calls.Load() != 1 {
			t.Fatalf("conversion calls = %d, want 1", conv.calls.Load())
		}
		if conv.sessionID != "s-conv" || conv.content != "重置密码" {
			t.Fatalf("conversion args = (%q, %q)", conv.sessionID, conv.content)
		}
		if waitForMessage(t, client.Send).Type != "text-message" {
			t.Fatal("expected broadcast echo")
		}
	})

	t.Run("converter error does not break message flow", func(t *testing.T) {
		hub := NewWebSocketHub()
		go hub.Run()
		conv := &unitConversionService{err: errors.New("boom")}
		hub.SetSuggestionConversionService(conv)

		client := &WebSocketClient{ID: "c", SessionID: "s-conv", Send: make(chan WebSocketMessage, 2), Hub: hub}
		hub.register <- client
		time.Sleep(20 * time.Millisecond)

		client.handleTextMessage(WebSocketMessage{Type: "text-message", Data: "plain"})

		if conv.calls.Load() != 1 {
			t.Fatalf("conversion calls = %d, want 1", conv.calls.Load())
		}
		if waitForMessage(t, client.Send).Type != "text-message" {
			t.Fatal("broadcast must survive conversion failure")
		}
	})

	t.Run("textMessageContent extraction variants", func(t *testing.T) {
		cases := []struct {
			name string
			data interface{}
			want string
		}{
			{"map content", map[string]interface{}{"content": "hi"}, "hi"},
			{"map without content", map[string]interface{}{"other": "hi"}, ""},
			{"plain string", "raw", "raw"},
			{"unsupported type", 12345, ""},
			{"nil", nil, ""},
		}
		for _, tc := range cases {
			if got := textMessageContent(tc.data); got != tc.want {
				t.Fatalf("%s: textMessageContent = %q, want %q", tc.name, got, tc.want)
			}
		}
	})
}

func TestWebSocket_ProcessMessageWithAI_Variants(t *testing.T) {
	t.Run("blank content", func(t *testing.T) {
		hub := NewWebSocketHub()
		go hub.Run()
		ai := &unitAIService{}
		hub.SetAIService(ai)
		c := &WebSocketClient{ID: "c", SessionID: "s", Hub: hub}
		c.processMessageWithAI(WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "   "}})
		if ai.processCalls.Load() != 0 {
			t.Fatal("blank content should not reach AI")
		}
	})

	t.Run("assigned to human skips AI", func(t *testing.T) {
		hub := NewWebSocketHub()
		go hub.Run()
		ai := &unitAIService{}
		writer := &unitConversationWriter{assigned: true}
		hub.SetAIService(ai)
		hub.SetConversationMessageWriter(writer)
		c := &WebSocketClient{ID: "c", SessionID: "s", Hub: hub}
		c.processMessageWithAI(WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "hi"}})
		time.Sleep(50 * time.Millisecond)
		if ai.processCalls.Load() != 0 {
			t.Fatalf("expected skip, got %d calls", ai.processCalls.Load())
		}
	})

	t.Run("assigned lookup error falls through", func(t *testing.T) {
		hub := NewWebSocketHub()
		go hub.Run()
		ai := &unitAIService{}
		writer := &unitConversationWriter{assignedErr: errors.New("boom")}
		hub.SetAIService(ai)
		hub.SetConversationMessageWriter(writer)
		c := &WebSocketClient{ID: "c", SessionID: "s", Hub: hub}
		c.processMessageWithAI(WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "hi"}})
		time.Sleep(50 * time.Millisecond)
		if ai.processCalls.Load() != 1 {
			t.Fatalf("expected ai call after lookup error, got %d", ai.processCalls.Load())
		}
	})

	t.Run("transfer waiting", func(t *testing.T) {
		hub := NewWebSocketHub()
		go hub.Run()
		ai := &unitAIService{shouldTransfer: true}
		transfer := &unitTransferService{result: &routingcontract.TransferResult{IsWaiting: true}}
		writer := &unitConversationWriter{recentMessages: []models.Message{{Sender: "user", Content: "x"}}}
		hub.SetAIService(ai)
		hub.SetSessionTransferService(transfer)
		hub.SetConversationMessageWriter(writer)

		c := &WebSocketClient{ID: "c", SessionID: "s-wait", Send: make(chan WebSocketMessage, 2), Hub: hub}
		hub.register <- c
		time.Sleep(20 * time.Millisecond)

		c.processMessageWithAI(WebSocketMessage{Type: "text-message", Data: "transfer me"})
		msg := waitForMessage(t, c.Send)
		if !strings.Contains(msg.Data.(map[string]interface{})["content"].(string), "等待队列") {
			t.Fatalf("unexpected waiting message: %+v", msg.Data)
		}
		if ai.processCalls.Load() != 0 {
			t.Fatal("transfer path must not call AI")
		}
	})

	t.Run("transfer assigned agent", func(t *testing.T) {
		hub := NewWebSocketHub()
		go hub.Run()
		ai := &unitAIService{shouldTransfer: true}
		transfer := &unitTransferService{result: &routingcontract.TransferResult{NewAgentID: 7}}
		hub.SetAIService(ai)
		hub.SetSessionTransferService(transfer)

		c := &WebSocketClient{ID: "c", SessionID: "s-agent", Send: make(chan WebSocketMessage, 2), Hub: hub}
		hub.register <- c
		time.Sleep(20 * time.Millisecond)

		c.processMessageWithAI(WebSocketMessage{Type: "text-message", Data: "transfer me"})
		msg := waitForMessage(t, c.Send)
		if !strings.Contains(msg.Data.(map[string]interface{})["content"].(string), "ID=7") {
			t.Fatalf("unexpected assigned message: %+v", msg.Data)
		}
	})

	t.Run("transfer failure", func(t *testing.T) {
		hub := NewWebSocketHub()
		go hub.Run()
		ai := &unitAIService{shouldTransfer: true}
		transfer := &unitTransferService{err: errors.New("no agents")}
		hub.SetAIService(ai)
		hub.SetSessionTransferService(transfer)

		c := &WebSocketClient{ID: "c", SessionID: "s-fail", Send: make(chan WebSocketMessage, 2), Hub: hub}
		hub.register <- c
		time.Sleep(20 * time.Millisecond)

		c.processMessageWithAI(WebSocketMessage{Type: "text-message", Data: "transfer me"})
		msg := waitForMessage(t, c.Send)
		if !strings.Contains(msg.Data.(map[string]interface{})["content"].(string), "转接人工客服失败") {
			t.Fatalf("unexpected failure message: %+v", msg.Data)
		}
	})

	t.Run("ai error is swallowed", func(t *testing.T) {
		hub := NewWebSocketHub()
		go hub.Run()
		ai := &unitAIService{queryErr: errors.New("boom")}
		hub.SetAIService(ai)
		c := &WebSocketClient{ID: "c", SessionID: "s", Hub: hub}
		c.processMessageWithAI(WebSocketMessage{Type: "text-message", Data: "hello"})
		time.Sleep(100 * time.Millisecond)
	})
}

func TestWebSocket_HandleWebRTCFlows(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()

	rtc := &unitRTCService{}
	hub.SetWebRTCService(rtc)

	offerClient := &WebSocketClient{ID: "c-offer", SessionID: "s-offer", Send: make(chan WebSocketMessage, 2), Hub: hub}
	hub.register <- offerClient
	time.Sleep(20 * time.Millisecond)

	offerClient.handleWebRTCOffer(WebSocketMessage{
		Type: "webrtc-offer",
		Data: map[string]interface{}{"type": "offer", "sdp": "offer-sdp"},
	})
	msg := waitForMessage(t, offerClient.Send)
	if msg.Type != "webrtc-answer" {
		t.Fatalf("expected webrtc-answer, got %s", msg.Type)
	}
	if rtc.offer.SDP != "offer-sdp" {
		t.Fatalf("unexpected offer: %+v", rtc.offer)
	}

	// bad offer payload -> broadcast fallback
	offerClient.handleWebRTCOffer(WebSocketMessage{Type: "webrtc-offer", Data: 42})

	// offer handler error path
	rtc.offerErr = errors.New("boom")
	offerClient.handleWebRTCOffer(WebSocketMessage{
		Type: "webrtc-offer",
		Data: map[string]interface{}{"type": "offer", "sdp": "offer-sdp"},
	})

	// answer flows
	answerClient := &WebSocketClient{ID: "c-ans", SessionID: "s-ans", Hub: hub}
	answerClient.handleWebRTCAnswer(WebSocketMessage{
		Type: "webrtc-answer",
		Data: map[string]interface{}{"type": "answer", "sdp": "answer-sdp"},
	})
	answerClient.handleWebRTCAnswer(WebSocketMessage{Type: "webrtc-answer", Data: "bad"}) // unsupported payload

	// candidate flows
	candClient := &WebSocketClient{ID: "c-cand", SessionID: "s-cand", Hub: hub}
	candClient.handleWebRTCCandidate(WebSocketMessage{
		Type: "webrtc-candidate",
		Data: map[string]interface{}{"candidate": "candidate-string"},
	})
	candClient.handleWebRTCCandidate(WebSocketMessage{Type: "webrtc-candidate", Data: "bad"})

	// no rtc service configured: offer broadcasts, answer/candidate return early
	hub2 := NewWebSocketHub()
	go hub2.Run()
	hub2.SetWebRTCService(nil)
	broadcastClient := &WebSocketClient{ID: "c-b", SessionID: "s-b", Send: make(chan WebSocketMessage, 2), Hub: hub2}
	hub2.register <- broadcastClient
	time.Sleep(20 * time.Millisecond)
	broadcastClient.handleWebRTCOffer(WebSocketMessage{Type: "webrtc-offer", Data: map[string]interface{}{"x": 1}})
	if msg := waitForMessage(t, broadcastClient.Send); msg.Type != "webrtc-offer" {
		t.Fatalf("expected broadcast fallback, got %s", msg.Type)
	}
	plain := &WebSocketClient{ID: "c-plain", SessionID: "s", Hub: hub2}
	plain.handleWebRTCAnswer(WebSocketMessage{Type: "webrtc-answer"})
	plain.handleWebRTCCandidate(WebSocketMessage{Type: "webrtc-candidate"})
}

func TestWebSocket_HandleWebSocketHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := NewWebSocketHub()
	go hub.Run()

	// missing session_id
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	hub.HandleWebSocket(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	// successful upgrade + message roundtrip
	router := gin.New()
	router.GET("/ws", hub.HandleWebSocket)
	server := httptest.NewServer(router)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?session_id=s-http"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// invalid json is ignored
	if err := conn.WriteMessage(websocket.TextMessage, []byte("not-json")); err != nil {
		t.Fatalf("write invalid: %v", err)
	}
	// unknown type logs a warning
	if err := conn.WriteJSON(WebSocketMessage{Type: "unknown-type"}); err != nil {
		t.Fatalf("write unknown: %v", err)
	}
	// text message triggers persistence (no writer configured -> no-op) and broadcast
	if err := conn.WriteJSON(WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "hi"}}); err != nil {
		t.Fatalf("write text: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var echo WebSocketMessage
	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
			t.Fatalf("read deadline: %v", err)
		}
		if err := conn.ReadJSON(&echo); err == nil {
			if echo.Type == "text-message" {
				break
			}
		}
	}
	if echo.Type != "text-message" {
		t.Fatalf("expected text-message echo, got %+v", echo)
	}

	// webrtc signaling message types over the wire
	offerMsg, _ := json.Marshal(WebSocketMessage{Type: "webrtc-offer", Data: map[string]interface{}{"type": "offer", "sdp": "x"}})
	if err := conn.WriteMessage(websocket.TextMessage, offerMsg); err != nil {
		t.Fatalf("write offer: %v", err)
	}
	ansMsg, _ := json.Marshal(WebSocketMessage{Type: "webrtc-answer", Data: map[string]interface{}{"type": "answer", "sdp": "x"}})
	if err := conn.WriteMessage(websocket.TextMessage, ansMsg); err != nil {
		t.Fatalf("write answer: %v", err)
	}
	candMsg, _ := json.Marshal(WebSocketMessage{Type: "webrtc-candidate", Data: map[string]interface{}{"candidate": "x"}})
	if err := conn.WriteMessage(websocket.TextMessage, candMsg); err != nil {
		t.Fatalf("write candidate: %v", err)
	}

	// closing the connection triggers unregister in readPump
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
}

func TestWebSocket_HandleWebSocketUpgradeFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := NewWebSocketHub()
	go hub.Run()

	// plain HTTP request cannot be upgraded
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/ws?session_id=s1", nil)
	hub.HandleWebSocket(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected upgrade failure (400), got %d", w.Code)
	}
}

func TestWebSocketHub_RegisterPushesICEConfig(t *testing.T) {
	hub := NewWebSocketHub()
	hub.SetWebRTCService(NewWebRTCService(iceturn.ICEConfig{STUNServers: []string{"stun:a:3478"}}, hub))
	go hub.Run()

	client := &WebSocketClient{ID: "ice-1", SessionID: "s1", Send: make(chan WebSocketMessage, 1), Hub: hub}
	hub.register <- client
	got := waitForMessage(t, client.Send)
	if got.Type != "webrtc-ice-config" || got.SessionID != "s1" {
		t.Fatalf("register push = %+v, want webrtc-ice-config for s1", got)
	}
	entries := got.Data.(map[string]interface{})["ice_servers"].([]map[string]interface{})
	if len(entries) != 1 || entries[0]["urls"].([]string)[0] != "stun:a:3478" {
		t.Fatalf("ice_servers payload = %+v, want single STUN entry", entries)
	}
}

func TestWebSocketHub_RegisterICEConfigBranches(t *testing.T) {
	// 空 ICE 配置：payload ok=false，注册后不应有下发。
	emptyHub := NewWebSocketHub()
	emptyHub.SetWebRTCService(NewWebRTCService(iceturn.ICEConfig{}, emptyHub))
	go emptyHub.Run()
	quiet := &WebSocketClient{ID: "ice-quiet", SessionID: "s", Send: make(chan WebSocketMessage, 1), Hub: emptyHub}
	emptyHub.register <- quiet
	time.Sleep(20 * time.Millisecond)
	select {
	case msg := <-quiet.Send:
		t.Fatalf("empty ICE config must not push, got %+v", msg)
	default:
	}

	// 发送队列满（无缓冲且无接收者）：走 default 放弃下发，客户端保留。
	fullHub := NewWebSocketHub()
	fullHub.SetWebRTCService(NewWebRTCService(iceturn.ICEConfig{STUNServers: []string{"stun:a:3478"}}, fullHub))
	go fullHub.Run()
	full := &WebSocketClient{ID: "ice-full", SessionID: "s", Send: make(chan WebSocketMessage), Hub: fullHub}
	fullHub.register <- full
	time.Sleep(20 * time.Millisecond)
	if fullHub.GetClientCount() != 1 {
		t.Fatalf("congested client should stay registered, got %d", fullHub.GetClientCount())
	}
}

// unitSessionTranslationService 会话消息自动翻译的测试替身（刀二）。
type unitSessionTranslationService struct {
	calls   atomic.Int64
	gotText string
	result  *translationdelivery.SessionTranslation
	err     error
}

func (u *unitSessionTranslationService) TranslateSessionMessage(_ context.Context, sessionID, text string) (*translationdelivery.SessionTranslation, error) {
	u.calls.Add(1)
	u.gotText = text
	return u.result, u.err
}

// TestWebSocket_HandleTextMessageSessionTranslation 刀二：文本落库后按会话
// 偏好异步翻译并广播 message-translated 帧；无偏好/provider 未配置静默，
// 其余失败不回帧且不影响消息主链路。
func TestWebSocket_HandleTextMessageSessionTranslation(t *testing.T) {
	newTranslationHub := func(svc *unitSessionTranslationService) (*WebSocketHub, *WebSocketClient) {
		hub := NewWebSocketHub()
		go hub.Run()
		hub.SetSessionTranslationService(svc)
		client := &WebSocketClient{ID: "c", SessionID: "s-trans", Send: make(chan WebSocketMessage, 4), Hub: hub}
		hub.register <- client
		time.Sleep(20 * time.Millisecond)
		return hub, client
	}

	t.Run("translated frame broadcast with payload", func(t *testing.T) {
		svc := &unitSessionTranslationService{result: &translationdelivery.SessionTranslation{
			Original: "bonjour", Content: "hello", SourceLang: "fr", TargetLang: "en",
		}}
		_, client := newTranslationHub(svc)

		client.handleTextMessage(WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "bonjour"}})

		seen := map[string]int{}
		var translated *WebSocketMessage
		for i := 0; i < 2; i++ {
			msg := waitForMessage(t, client.Send)
			seen[msg.Type]++
			if msg.Type == "message-translated" {
				translated = &msg
			}
		}
		if seen["text-message"] != 1 || seen["message-translated"] != 1 {
			t.Fatalf("expected echo + translation, got %v", seen)
		}
		if svc.calls.Load() != 1 || svc.gotText != "bonjour" {
			t.Fatalf("translate calls=%d text=%q", svc.calls.Load(), svc.gotText)
		}
		if translated.SessionID != "s-trans" {
			t.Fatalf("frame session = %q", translated.SessionID)
		}
		payload, ok := translated.Data.(*translationdelivery.SessionTranslation)
		if !ok || payload.Content != "hello" || payload.Original != "bonjour" || payload.SourceLang != "fr" || payload.TargetLang != "en" {
			t.Fatalf("frame data = %+v", translated.Data)
		}
	})

	t.Run("no preference skips silently", func(t *testing.T) {
		svc := &unitSessionTranslationService{}
		_, client := newTranslationHub(svc)

		client.handleTextMessage(WebSocketMessage{Type: "text-message", Data: "hello"})

		if waitForMessage(t, client.Send).Type != "text-message" {
			t.Fatal("expected broadcast echo")
		}
		time.Sleep(40 * time.Millisecond)
		if svc.calls.Load() != 1 {
			t.Fatalf("translate calls = %d, want 1 (nil result path)", svc.calls.Load())
		}
		select {
		case extra := <-client.Send:
			t.Fatalf("no frame expected for unset preference, got %s", extra.Type)
		default:
		}
	})

	t.Run("unavailable and raw errors emit no frame", func(t *testing.T) {
		for name, svc := range map[string]*unitSessionTranslationService{
			"unavailable": {err: translationdelivery.ErrTranslationUnavailable},
			"raw error":   {err: errors.New("provider 502")},
		} {
			t.Run(name, func(t *testing.T) {
				_, client := newTranslationHub(svc)
				client.handleTextMessage(WebSocketMessage{Type: "text-message", Data: "hello"})
				if waitForMessage(t, client.Send).Type != "text-message" {
					t.Fatal("broadcast must survive translation failure")
				}
				time.Sleep(40 * time.Millisecond)
				select {
				case extra := <-client.Send:
					t.Fatalf("failure must not emit %s frame", extra.Type)
				default:
				}
			})
		}
	})

	t.Run("unwired service and blank content skip", func(t *testing.T) {
		hub := NewWebSocketHub()
		go hub.Run()
		client := &WebSocketClient{ID: "c", SessionID: "s-none", Send: make(chan WebSocketMessage, 2), Hub: hub}
		hub.register <- client
		time.Sleep(20 * time.Millisecond)

		client.handleTextMessage(WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "  "}})
		if waitForMessage(t, client.Send).Type != "text-message" {
			t.Fatal("expected broadcast echo")
		}
		time.Sleep(40 * time.Millisecond)
		select {
		case extra := <-client.Send:
			t.Fatalf("blank content must stay pure broadcast, got %s", extra.Type)
		default:
		}
	})
}
