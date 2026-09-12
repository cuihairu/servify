package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type fakeOpenConversationReader struct {
	conversations []conversationapp.ConversationDTO
	total         int64
	getErr        error
	listErr       error
	messages      []conversationapp.ConversationMessageDTO
	lastQuery     conversationapp.OpenSessionListQuery
}

func (f *fakeOpenConversationReader) ListConversations(ctx context.Context, query conversationapp.OpenSessionListQuery) ([]conversationapp.ConversationDTO, int64, error) {
	f.lastQuery = query
	return f.conversations, f.total, f.listErr
}
func (f *fakeOpenConversationReader) GetConversation(ctx context.Context, conversationID string) (*conversationapp.ConversationDTO, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	for i := range f.conversations {
		if f.conversations[i].ID == conversationID {
			return &f.conversations[i], nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}
func (f *fakeOpenConversationReader) ListRecentMessages(ctx context.Context, conversationID string, limit int) ([]conversationapp.ConversationMessageDTO, error) {
	return f.messages, nil
}

func newOpenConversationRouter(t *testing.T, reader conversationdelivery.OpenConversationReader) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	RegisterOpenConversationRoutes(api, NewOpenConversationHandler(reader))
	return r
}

func TestOpenConversationRoutesReadOnly(t *testing.T) {
	reader := &fakeOpenConversationReader{
		conversations: []conversationapp.ConversationDTO{{ID: "s1", Status: "active", StartedAt: time.Now()}},
		total:         1,
		messages:      []conversationapp.ConversationMessageDTO{{ID: "m1", ConversationID: "s1", Sender: "customer"}},
	}
	r := newOpenConversationRouter(t, reader)

	// 列表 + 分页参数透传
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/conversations?page=2&page_size=5&status=active&channel=web", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if reader.lastQuery.Page != 2 || reader.lastQuery.PageSize != 5 || reader.lastQuery.Status != "active" || reader.lastQuery.Channel != "web" {
		t.Fatalf("query not passed through: %+v", reader.lastQuery)
	}
	var listResp struct {
		Items []conversationapp.ConversationDTO `json:"items"`
		Total int64                             `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil || listResp.Total != 1 {
		t.Fatalf("bad list response: %v %s", err, rec.Body.String())
	}

	// 详情
	rec = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/conversations/s1", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"s1"`) {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}

	// 消息
	rec = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/conversations/s1/messages?limit=10", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"m1"`) {
		t.Fatalf("messages: %d %s", rec.Code, rec.Body.String())
	}

	// 开放面只有 GET：POST 应 404/405（gin 默认 404）
	rec = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/v1/conversations", strings.NewReader("{}"))
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
		t.Fatalf("open surface must not allow POST, got %d", rec.Code)
	}
}

func TestOpenConversationNotFoundAndErrors(t *testing.T) {
	reader := &fakeOpenConversationReader{getErr: gorm.ErrRecordNotFound}
	r := newOpenConversationRouter(t, reader)

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/conversations/missing", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing conversation should 404, got %d", rec.Code)
	}

	reader.getErr = errors.New("db down")
	rec = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/conversations/s1", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("internal error should 500, got %d", rec.Code)
	}

	reader.listErr = errors.New("db down")
	rec = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("list failure should 500, got %d", rec.Code)
	}
}

var _ conversationdomain.ParticipantRole = conversationdomain.ParticipantRoleCustomer
