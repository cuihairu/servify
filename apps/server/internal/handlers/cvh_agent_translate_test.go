package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// historyTranslatorStub 历史批量标注替身：捕获请求文本，按序回放译文。
type historyTranslatorStub struct {
	gotSession string
	gotTexts   []string
	texts      []string
	lang       string
	err        error
}

func (h *historyTranslatorStub) TranslateHistory(_ context.Context, sessionID string, texts []string) (*translationdelivery.HistoryTranslation, error) {
	h.gotSession = sessionID
	h.gotTexts = append([]string(nil), texts...)
	if h.err != nil {
		return nil, h.err
	}
	if h.lang == "" {
		return nil, nil
	}
	return &translationdelivery.HistoryTranslation{Texts: h.texts, TargetLang: h.lang}, nil
}

func listMessages(t *testing.T, handler *ConversationWorkspaceHandler) []conversationdelivery.ConversationMessageDTO {
	t.Helper()
	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)
	req := httptest.NewRequest(http.MethodGet, "/api/omni/sessions/sess-1/messages", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []conversationdelivery.ConversationMessageDTO `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.Data
}

// TestListMessagesHistoryAnnotation Phase 1 收尾：工作台历史面按 agent 读向
// 偏好批量标注访客消息译文（§4.4 metadata 保留键），坐席消息与既有译文
// 不动；无偏好/失败静默，不影响历史加载。
func TestListMessagesHistoryAnnotation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("visitor messages annotated with translation keys", func(t *testing.T) {
		history := &historyTranslatorStub{lang: "en", texts: []string{"translated-hello"}}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, newConversationWorkspaceTestDB(t))), nil, nil).
			WithHistoryTranslator(history)

		items := listMessages(t, handler)
		annotated := 0
		for _, item := range items {
			if item.Sender == "agent" {
				if item.Metadata["translation"] != "" {
					t.Fatalf("agent message must not be annotated: %+v", item)
				}
				continue
			}
			annotated++
			if item.Metadata["translation"] != "translated-hello" || item.Metadata["translation_lang"] != "en" {
				t.Fatalf("visitor metadata = %+v", item.Metadata)
			}
		}
		if annotated != 1 {
			t.Fatalf("want exactly one visitor message annotated, got %d", annotated)
		}
		if history.gotSession != "sess-1" || len(history.gotTexts) != 1 || history.gotTexts[0] != "hello" {
			t.Fatalf("translator input = %q %v", history.gotSession, history.gotTexts)
		}
	})

	t.Run("no preference leaves response untouched", func(t *testing.T) {
		history := &historyTranslatorStub{}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, newConversationWorkspaceTestDB(t))), nil, nil).
			WithHistoryTranslator(history)
		for _, item := range listMessages(t, handler) {
			if item.Metadata["translation"] != "" {
				t.Fatalf("no preference must not annotate, got %+v", item.Metadata)
			}
		}
	})

	t.Run("translation failure leaves response untouched", func(t *testing.T) {
		history := &historyTranslatorStub{err: translationdelivery.ErrTranslationUnavailable}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, newConversationWorkspaceTestDB(t))), nil, nil).
			WithHistoryTranslator(history)
		for _, item := range listMessages(t, handler) {
			if item.Metadata["translation"] != "" {
				t.Fatalf("failure must not annotate, got %+v", item.Metadata)
			}
		}
	})

	t.Run("page with nothing to translate skips the translator", func(t *testing.T) {
		// 全页坐席消息（或全部已带译文）→ 无待译段 → 不触翻译服务。
		items := []conversationdelivery.ConversationMessageDTO{
			{Sender: "agent", Content: "agent own text"},
			{Sender: "system", Content: "  "},
		}
		history := &historyTranslatorStub{lang: "en", texts: []string{"should-not-happen"}}
		handler := NewConversationWorkspaceHandler(nil, nil, nil).WithHistoryTranslator(history)
		handler.annotateHistory(context.Background(), "sess-1", items)

		if history.gotTexts != nil {
			t.Fatalf("translator must not be called, got %v", history.gotTexts)
		}
		if items[1].Metadata["translation"] != "" {
			t.Fatalf("blank content must not be annotated, got %+v", items[1].Metadata)
		}
	})

	t.Run("failed segment skipped while others annotated", func(t *testing.T) {
		// 批量结果允许部分失败：失败段（空串/缺失）跳过标注，成功段照常。
		items := []conversationdelivery.ConversationMessageDTO{
			{Sender: "customer", Content: "first visitor text"},
			{Sender: "customer", Content: "second visitor text"},
		}
		history := &historyTranslatorStub{lang: "en", texts: []string{"only-first"}}
		handler := NewConversationWorkspaceHandler(nil, nil, nil).WithHistoryTranslator(history)
		handler.annotateHistory(context.Background(), "sess-1", items)

		if items[0].Metadata["translation"] != "only-first" {
			t.Fatalf("first segment must be annotated, got %+v", items[0].Metadata)
		}
		if len(items[1].Metadata) != 0 {
			t.Fatalf("missing segment must stay unannotated, got %+v", items[1].Metadata)
		}
	})

	t.Run("existing translation is not re-translated", func(t *testing.T) {
		// 已带译文的 DTO（同响应内二次加载等场景）跳过批量标注——直接测
		// annotateHistory 的过滤逻辑（消息存储不落 metadata，构造走内存）。
		items := []conversationdelivery.ConversationMessageDTO{
			{Sender: "customer", Content: "canned source", Metadata: map[string]string{"translation": "canned", "translation_lang": "ja"}},
			{Sender: "customer", Content: "plain visitor text"},
		}
		history := &historyTranslatorStub{lang: "en", texts: []string{"fresh"}}
		handler := NewConversationWorkspaceHandler(nil, nil, nil).WithHistoryTranslator(history)
		handler.annotateHistory(context.Background(), "sess-1", items)

		if items[0].Metadata["translation"] != "canned" || items[0].Metadata["translation_lang"] != "ja" {
			t.Fatalf("canned translation must be preserved, got %+v", items[0].Metadata)
		}
		if items[1].Metadata["translation"] != "fresh" {
			t.Fatalf("plain message must be annotated, got %+v", items[1].Metadata)
		}
		if len(history.gotTexts) != 1 || history.gotTexts[0] != "plain visitor text" {
			t.Fatalf("canned message must not be re-translated, texts=%v", history.gotTexts)
		}
	})
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

	t.Run("raw provider error warns and skips frame", func(t *testing.T) {
		gateway := &threadSafeRealtimeGateway{}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, newConversationWorkspaceTestDB(t))), gateway,
			&hookTranslator{err: errors.New("provider 502")})

		if w := postAgentMessage(t, handler); w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", w.Code)
		}
		time.Sleep(50 * time.Millisecond)
		if msg := gateway.waitFor(t, "message-translated"); msg != nil {
			t.Fatalf("provider error must not broadcast, got %+v", msg)
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
