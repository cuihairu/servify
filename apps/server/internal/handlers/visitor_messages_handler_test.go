package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type fakeVisitorMessagesService struct {
	page     *conversationdelivery.VisitorMessagesPage
	err      error
	gotSess  string
	gotCur   string
	gotLimit int
}

func (f *fakeVisitorMessagesService) ListMessagesAfter(_ context.Context, sessionID, afterMessageID string, limit int) (*conversationdelivery.VisitorMessagesPage, error) {
	f.gotSess, f.gotCur, f.gotLimit = sessionID, afterMessageID, limit
	if f.err != nil {
		return nil, f.err
	}
	return f.page, nil
}

func newVisitorMessagesRouter(svc *fakeVisitorMessagesService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	r := gin.New()
	r.GET("/api/v1/sessions/:session_id/messages", NewVisitorMessagesHandler(svc, logger).ListAfter)
	return r
}

func TestVisitorMessagesHandlerListAfter(t *testing.T) {
	svc := &fakeVisitorMessagesService{page: &conversationdelivery.VisitorMessagesPage{
		Messages: []conversationapp.ConversationMessageDTO{
			{ID: "7", ConversationID: "sess-1", Sender: "agent", Kind: "text", Content: "回复", CreatedAt: time.Unix(1727000000, 0)},
		},
		HasMore: false,
	}}
	r := newVisitorMessagesRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-1/messages?after_id=5&limit=20", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if svc.gotSess != "sess-1" || svc.gotCur != "5" || svc.gotLimit != 20 {
		t.Fatalf("args = %s/%s/%d", svc.gotSess, svc.gotCur, svc.gotLimit)
	}
	var page conversationdelivery.VisitorMessagesPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != "7" || page.HasMore {
		t.Fatalf("page = %+v", page)
	}

	// 默认 limit（不带参）
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-1/messages", nil))
	if svc.gotLimit != visitorMessagesDefaultLimit {
		t.Fatalf("default limit = %d", svc.gotLimit)
	}

	// limit 上限 clamp（300 → 200）
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-1/messages?limit=300", nil))
	if svc.gotLimit != visitorMessagesMaxLimit {
		t.Fatalf("clamped limit = %d", svc.gotLimit)
	}

	// limit 非法 → 400
	w400 := httptest.NewRecorder()
	r.ServeHTTP(w400, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-1/messages?limit=abc", nil))
	if w400.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit status = %d", w400.Code)
	}
	// limit 非正数 → 400
	w0 := httptest.NewRecorder()
	r.ServeHTTP(w0, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-1/messages?limit=0", nil))
	if w0.Code != http.StatusBadRequest {
		t.Fatalf("zero limit status = %d", w0.Code)
	}
}

func TestVisitorMessagesHandlerErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"session not found", errors.New("session not found: sess-x"), http.StatusNotFound},
		{"invalid cursor", errors.New("invalid message cursor: abc"), http.StatusBadRequest},
		{"internal", errors.New("db exploded"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		svc := &fakeVisitorMessagesService{err: tc.err}
		r := newVisitorMessagesRouter(svc)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-1/messages", nil))
		if w.Code != tc.want {
			t.Fatalf("%s: status = %d, want %d", tc.name, w.Code, tc.want)
		}
	}
}
