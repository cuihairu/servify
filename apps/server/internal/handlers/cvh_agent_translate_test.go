package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	translationdelivery "servify/apps/server/internal/modules/translation/delivery"
	realtimeplatform "servify/apps/server/internal/platform/realtime"

	"github.com/gin-gonic/gin"
)

// threadSafeRealtimeGateway 并发安全的网关替身：translateAgentMessage 在
// goroutine 里异步广播，测试侧轮询读取需要互斥（-race 干净）。
type threadSafeRealtimeGateway struct {
	mu       sync.Mutex
	messages []realtimeplatform.Message
}

func (g *threadSafeRealtimeGateway) HandleWebSocket(*gin.Context) {}
func (g *threadSafeRealtimeGateway) ClientCount() int             { return 0 }

func (g *threadSafeRealtimeGateway) SendToSession(sessionID string, message realtimeplatform.Message) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.messages = append(g.messages, message)
}

func (g *threadSafeRealtimeGateway) waitFor(t *testing.T, kind string) *realtimeplatform.Message {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		g.mu.Lock()
		for i := range g.messages {
			if g.messages[i].Type == kind {
				msg := g.messages[i]
				g.mu.Unlock()
				return &msg
			}
		}
		g.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

// hookTranslator 翻译服务替身：捕获查询的会话与文本。
type hookTranslator struct {
	gotSession string
	gotText    string
	result     *translationdelivery.SessionTranslation
	err        error
}

func (h *hookTranslator) TranslateSessionMessage(_ context.Context, sessionID, text string) (*translationdelivery.SessionTranslation, error) {
	h.gotSession = sessionID
	h.gotText = text
	return h.result, h.err
}

func postAgentMessage(t *testing.T, handler *ConversationWorkspaceHandler) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)
	body, _ := json.Marshal(map[string]string{"content": "admin reply"})
	req := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/sess-1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestSendMessageAgentTranslationHook 刀三坐席发送口翻译钩子：坐席消息
// 落库广播后异步消费 visitor 读向偏好，产出与刀二同契约的
// message-translated 帧；失败/未装配静默，不影响消息主链路。
func TestSendMessageAgentTranslationHook(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("translated frame broadcast with visitor preference set", func(t *testing.T) {
		gateway := &threadSafeRealtimeGateway{}
		translator := &hookTranslator{result: &translationdelivery.SessionTranslation{
			Original: "admin reply", Content: "回复内容", SourceLang: "en", TargetLang: "zh-cn",
		}}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, newConversationWorkspaceTestDB(t))), gateway, translator)

		w := postAgentMessage(t, handler)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		msg := gateway.waitFor(t, "message-translated")
		if msg == nil {
			t.Fatal("expected message-translated broadcast")
		}
		if msg.SessionID != "sess-1" {
			t.Fatalf("frame must target the session, got %q", msg.SessionID)
		}
		data, ok := msg.Data.(*translationdelivery.SessionTranslation)
		if !ok {
			t.Fatalf("frame data must be SessionTranslation, got %T", msg.Data)
		}
		if data.Original != "admin reply" || data.Content != "回复内容" || data.TargetLang != "zh-cn" {
			t.Fatalf("payload = %+v", data)
		}
		if translator.gotSession != "sess-1" || translator.gotText != "admin reply" {
			t.Fatalf("translator input = %q/%q", translator.gotSession, translator.gotText)
		}
	})

	t.Run("no preference yields no frame", func(t *testing.T) {
		gateway := &threadSafeRealtimeGateway{}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, newConversationWorkspaceTestDB(t))), gateway, &hookTranslator{})

		if w := postAgentMessage(t, handler); w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", w.Code)
		}
		time.Sleep(50 * time.Millisecond)
		if msg := gateway.waitFor(t, "message-translated"); msg != nil {
			t.Fatalf("nil result must not broadcast, got %+v", msg)
		}
	})

	t.Run("translation unavailable stays silent", func(t *testing.T) {
		gateway := &threadSafeRealtimeGateway{}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, newConversationWorkspaceTestDB(t))), gateway,
			&hookTranslator{err: translationdelivery.ErrTranslationUnavailable})

		if w := postAgentMessage(t, handler); w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", w.Code)
		}
		time.Sleep(50 * time.Millisecond)
		if msg := gateway.waitFor(t, "message-translated"); msg != nil {
			t.Fatalf("unavailable must not broadcast, got %+v", msg)
		}
	})

	t.Run("nil translator keeps primary chain intact", func(t *testing.T) {
		gateway := &threadSafeRealtimeGateway{}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, newConversationWorkspaceTestDB(t))), gateway, nil)

		if w := postAgentMessage(t, handler); w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", w.Code)
		}
		time.Sleep(50 * time.Millisecond)
		gateway.mu.Lock()
		defer gateway.mu.Unlock()
		if len(gateway.messages) != 1 || gateway.messages[0].Type != "agent-message" {
			t.Fatalf("expected only agent-message frame, got %+v", gateway.messages)
		}
	})
}
