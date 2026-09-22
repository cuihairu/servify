package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"servify/apps/server/internal/models"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	llm "servify/apps/server/internal/platform/llm"
	llmmock "servify/apps/server/internal/platform/llm/mock"
)

// fakeCopilotHistory 坐席 handler 测试用历史桩（DESC 序，最新在前）。
type fakeCopilotHistory struct {
	msgs []models.Message
}

func (f *fakeCopilotHistory) ListRecentMessages(ctx context.Context, sessionID string, limit int) ([]models.Message, error) {
	return f.msgs, nil
}

func postCopilot(t *testing.T, h *AICopilotHandler, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/ai/copilot", h.Copilot)
	buf, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/ai/copilot", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestAICopilotHandler_HappyPath(t *testing.T) {
	provider := &llmmock.Provider{ChatResponse: llm.ChatResponse{Content: "已为您加急，今日发货。"}}
	svc := aidelivery.NewAgentCopilotService(provider, aidelivery.AIRuntimeParams{}).
		WithSessionHistory(&fakeCopilotHistory{msgs: []models.Message{
			{Sender: "customer", Content: "怎么还不发货"},
		}})
	h := NewAICopilotHandler(svc)

	w := postCopilot(t, h, map[string]string{"action": "suggest_reply", "session_id": "s-1"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			Action    string `json:"action"`
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !envelope.Success || envelope.Data.Action != "suggest_reply" || envelope.Data.SessionID != "s-1" || envelope.Data.Text == "" {
		t.Fatalf("envelope = %+v", envelope)
	}
}

func TestAICopilotHandler_ErrorMapping(t *testing.T) {
	provider := &llmmock.Provider{}
	svc := aidelivery.NewAgentCopilotService(provider, aidelivery.AIRuntimeParams{})
	h := NewAICopilotHandler(svc)

	cases := []struct {
		name string
		body map[string]string
		code int
	}{
		{"bad json", nil, http.StatusBadRequest}, // 缺 action（binding:required）
		{"unsupported action", map[string]string{"action": "bogus"}, http.StatusBadRequest},
		{"session required", map[string]string{"action": "suggest_reply"}, http.StatusBadRequest},
		{"draft required", map[string]string{"action": "rewrite", "draft": "  "}, http.StatusBadRequest},
		{"empty history", map[string]string{"action": "session_summary", "session_id": "s-404"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		w := postCopilot(t, h, tc.body)
		if w.Code != tc.code {
			t.Fatalf("%s: status = %d, want %d, body=%s", tc.name, w.Code, tc.code, w.Body.String())
		}
	}

	// 模型故障 → 500。
	chatErrSvc := aidelivery.NewAgentCopilotService(&llmmock.Provider{ChatError: errors.New("boom")}, aidelivery.AIRuntimeParams{}).
		WithSessionHistory(&fakeCopilotHistory{msgs: []models.Message{{Sender: "customer", Content: "hi"}}})
	w := postCopilot(t, NewAICopilotHandler(chatErrSvc), map[string]string{"action": "suggest_reply", "session_id": "s-1"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("chat error status = %d, want 500", w.Code)
	}

	// AI 未装配（nil 服务）→ 503。
	w2 := postCopilot(t, NewAICopilotHandler(nil), map[string]string{"action": "rewrite", "draft": "x"})
	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service status = %d, want 503", w2.Code)
	}
}
