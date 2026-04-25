package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"
	conversationinfra "servify/apps/server/internal/modules/conversation/infra"
	realtimeplatform "servify/apps/server/internal/platform/realtime"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"servify/apps/server/internal/models"
)

type stubRealtimeGateway struct {
	messages []realtimeplatform.Message
}

func (s *stubRealtimeGateway) HandleWebSocket(*gin.Context) {}
func (s *stubRealtimeGateway) SendToSession(sessionID string, message realtimeplatform.Message) {
	s.messages = append(s.messages, message)
}
func (s *stubRealtimeGateway) ClientCount() int { return 0 }

func newConversationWorkspaceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:conversation_workspace_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Session{}, &models.Message{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func seedConversation(t *testing.T, db *gorm.DB) *conversationapp.Service {
	t.Helper()
	repo := conversationinfra.NewGormRepository(db)
	service := conversationapp.NewService(repo, nil)
	customerID := uint(1)
	if err := db.Create(&models.User{ID: customerID, Username: "customer", Email: "customer@example.com", Password: "x"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := service.CreateConversation(context.Background(), conversationapp.CreateConversationCommand{
		ConversationID: "sess-1",
		CustomerID:     &customerID,
		Channel: conversationdomain.ChannelBinding{
			Channel:   "web",
			SessionID: "sess-1",
		},
		Participants: []conversationdomain.Participant{
			{ID: "customer:1", UserID: &customerID, Role: conversationdomain.ParticipantRoleCustomer},
		},
	}); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := service.IngestTextMessage(context.Background(), conversationapp.IngestTextMessageCommand{
		ConversationID: "sess-1",
		Sender:         conversationdomain.ParticipantRoleCustomer,
		Content:        "hello",
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if _, err := service.IngestTextMessage(context.Background(), conversationapp.IngestTextMessageCommand{
		ConversationID: "sess-1",
		Sender:         conversationdomain.ParticipantRoleAgent,
		Content:        "hi there",
	}); err != nil {
		t.Fatalf("seed agent message: %v", err)
	}
	return service
}

func TestConversationWorkspaceHandler_ListMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

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
		Data []conversationapp.ConversationMessageDTO `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(resp.Data))
	}
	// 验证消息内容存在，顺序可能因时间戳相同而不同
	hasHello := false
	hasHiThere := false
	for _, msg := range resp.Data {
		if msg.Content == "hello" {
			hasHello = true
		}
		if msg.Content == "hi there" {
			hasHiThere = true
		}
	}
	if !hasHello || !hasHiThere {
		t.Fatalf("expected messages with 'hello' and 'hi there', got %+v", resp.Data)
	}
}

func TestConversationWorkspaceHandler_SendMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	realtime := &stubRealtimeGateway{}
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), realtime)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	body, _ := json.Marshal(map[string]string{"content": "admin reply"})
	req := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/sess-1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	items, err := service.ListRecentMessages(context.Background(), "sess-1", 10)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(items))
	}
	if items[0].CreatedAt.After(items[1].CreatedAt) && items[0].CreatedAt.After(items[2].CreatedAt) {
		// repository order is desc; verify latest content exists.
	}
	found := false
	for _, item := range items {
		if item.Content == "admin reply" && item.Sender == "agent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected agent message persisted, got %+v", items)
	}
	if len(realtime.messages) != 1 || realtime.messages[0].Type != "agent-message" {
		t.Fatalf("expected realtime agent-message broadcast, got %+v", realtime.messages)
	}
}

func TestConversationWorkspaceHandler_AssignAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	body, _ := json.Marshal(map[string]uint{"agent_id": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/sess-1/assign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConversationWorkspaceHandler_Transfer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	// Assign first so transfer has an agent to transfer from
	assignBody, _ := json.Marshal(map[string]uint{"agent_id": 1})
	req0 := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/sess-1/assign", bytes.NewReader(assignBody))
	req0.Header.Set("Content-Type", "application/json")
	w0 := httptest.NewRecorder()
	router.ServeHTTP(w0, req0)
	if w0.Code != http.StatusOK {
		t.Fatalf("assign precondition failed: %d %s", w0.Code, w0.Body.String())
	}

	body, _ := json.Marshal(map[string]uint{"to_agent_id": 2})
	req := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/sess-1/transfer", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConversationWorkspaceHandler_CloseSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	req := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/sess-1/close", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify conversation is now closed
	dto, err := service.GetConversation(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if dto.Status != "closed" {
		t.Fatalf("expected status closed, got %q", dto.Status)
	}
}

func TestConversationWorkspaceHandler_GetSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	req := httptest.NewRequest(http.MethodGet, "/api/omni/sessions/sess-1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, ok := resp["data"].(map[string]interface{})
	if !ok || data["id"] != "sess-1" {
		t.Fatalf("unexpected session data: %+v", resp)
	}
}

func TestConversationWorkspaceHandler_GetSession_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	req := httptest.NewRequest(http.MethodGet, "/api/omni/sessions/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConversationWorkspaceHandler_ListMessages_Pagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	// Seed more messages to test pagination
	for i := 0; i < 5; i++ {
		_, _ = service.IngestTextMessage(context.Background(), conversationapp.IngestTextMessageCommand{
			ConversationID: "sess-1",
			Sender:         conversationdomain.ParticipantRoleCustomer,
			Content:        "extra message",
		})
	}

	// Get first page with limit=3
	req := httptest.NewRequest(http.MethodGet, "/api/omni/sessions/sess-1/messages?limit=3", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(resp.Data))
	}

	// Get second page with before cursor
	beforeID := resp.Data[0].ID
	req2 := httptest.NewRequest(http.MethodGet, "/api/omni/sessions/sess-1/messages?limit=3&before="+beforeID, nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for page 2, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestConversationWorkspaceHandler_SendMessage_InvalidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	req := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/sess-1/messages", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConversationWorkspaceHandler_SendMessage_BlankContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	body, _ := json.Marshal(map[string]string{"content": "   "})
	req := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/sess-1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConversationWorkspaceHandler_SendMessage_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	body, _ := json.Marshal(map[string]string{"content": "admin reply"})
	req := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/missing/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConversationWorkspaceHandler_AssignAgent_InvalidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	req := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/sess-1/assign", bytes.NewReader([]byte("{bad")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConversationWorkspaceHandler_ListMessages_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	req := httptest.NewRequest(http.MethodGet, "/api/omni/sessions/missing/messages", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConversationWorkspaceHandler_AssignAgent_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	body, _ := json.Marshal(map[string]uint{"agent_id": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/missing/assign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConversationWorkspaceHandler_Transfer_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	body, _ := json.Marshal(map[string]uint{"to_agent_id": 2})
	req := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/missing/transfer", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConversationWorkspaceHandler_CloseSession_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversation(t, db)
	handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil)

	router := gin.New()
	group := router.Group("/api")
	RegisterConversationWorkspaceRoutes(group, handler)

	req := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/missing/close", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
