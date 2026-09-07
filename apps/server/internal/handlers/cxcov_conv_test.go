package handlers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"

	"github.com/gin-gonic/gin"
)

type cxcConversationService struct{}

func (s *cxcConversationService) GetConversation(ctx context.Context, sessionID string) (*conversationdelivery.ConversationDTO, error) {
	return &conversationdelivery.ConversationDTO{}, nil
}
func (s *cxcConversationService) ListMessages(ctx context.Context, sessionID string, limit int) ([]conversationdelivery.ConversationMessageDTO, error) {
	return nil, nil
}
func (s *cxcConversationService) ListMessagesBefore(ctx context.Context, sessionID, beforeMessageID string, limit int) ([]conversationdelivery.ConversationMessageDTO, error) {
	return nil, nil
}
func (s *cxcConversationService) SendAgentMessage(ctx context.Context, sessionID, content string) (*conversationdelivery.ConversationMessageDTO, error) {
	return &conversationdelivery.ConversationMessageDTO{}, nil
}
func (s *cxcConversationService) AssignAgent(ctx context.Context, sessionID string, agentID uint) (*conversationdelivery.ConversationDTO, error) {
	return &conversationdelivery.ConversationDTO{}, nil
}
func (s *cxcConversationService) Transfer(ctx context.Context, sessionID string, toAgentID uint) (*conversationdelivery.ConversationDTO, error) {
	return &conversationdelivery.ConversationDTO{}, nil
}
func (s *cxcConversationService) Close(ctx context.Context, sessionID string) (*conversationdelivery.ConversationDTO, error) {
	return &conversationdelivery.ConversationDTO{}, nil
}

func TestCxcConversationWorkspaceTransferInvalidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewConversationWorkspaceHandler(&cxcConversationService{}, nil)
	r := gin.New()
	r.POST("/api/omni/sessions/:id/transfer", handler.Transfer)

	if w := cxcPerform(r, http.MethodPost, "/api/omni/sessions/s1/transfer", strings.NewReader("{bad"), "application/json"); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if w := cxcPerform(r, http.MethodPost, "/api/omni/sessions/s1/transfer", cxcJSONBody(map[string]string{"content": "no agent"}), "application/json"); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing to_agent_id, got %d body=%s", w.Code, w.Body.String())
	}
	if w := cxcPerform(r, http.MethodPost, "/api/omni/sessions/s1/transfer", cxcJSONBody(map[string]uint{"to_agent_id": 2}), "application/json"); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCxcConversationWorkspaceServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewConversationWorkspaceHandler(nil, nil)
	r := gin.New()
	RegisterConversationWorkspaceRoutes(r.Group("/api"), handler)

	cases := []struct {
		method string
		path   string
		body   io.Reader
	}{
		{http.MethodGet, "/api/omni/sessions/s1", nil},
		{http.MethodGet, "/api/omni/sessions/s1/messages", nil},
		{http.MethodPost, "/api/omni/sessions/s1/messages", strings.NewReader(`{"content":"hi"}`)},
		{http.MethodPost, "/api/omni/sessions/s1/assign", strings.NewReader(`{"agent_id":1}`)},
		{http.MethodPost, "/api/omni/sessions/s1/transfer", strings.NewReader(`{"to_agent_id":2}`)},
		{http.MethodPost, "/api/omni/sessions/s1/close", nil},
	}
	for _, tc := range cases {
		rec := cxcPerform(r, tc.method, tc.path, tc.body, "application/json")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s expected 503, got %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "conversation service is not configured") {
			t.Fatalf("%s %s unexpected body: %s", tc.method, tc.path, rec.Body.String())
		}
	}
}
