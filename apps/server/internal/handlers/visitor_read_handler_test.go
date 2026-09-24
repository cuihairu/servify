package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type fakeVisitorReadService struct {
	state   *conversationdelivery.VisitorReadState
	err     error
	gotSess string
	gotCur  string
}

func (f *fakeVisitorReadService) MarkRead(_ context.Context, sessionID, lastReadMessageID string) (*conversationdelivery.VisitorReadState, error) {
	f.gotSess, f.gotCur = sessionID, lastReadMessageID
	if f.err != nil {
		return nil, f.err
	}
	return f.state, nil
}

func (f *fakeVisitorReadService) UnreadState(_ context.Context, sessionID string) (*conversationdelivery.VisitorReadState, error) {
	f.gotSess = sessionID
	if f.err != nil {
		return nil, f.err
	}
	return f.state, nil
}

func newVisitorReadRouter(svc *fakeVisitorReadService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	r := gin.New()
	r.POST("/api/v1/sessions/:session_id/read", NewVisitorReadHandler(svc, logger).MarkRead)
	r.GET("/api/v1/sessions/:session_id/unread", NewVisitorReadHandler(svc, logger).Unread)
	return r
}

func TestVisitorReadHandlerMarkRead(t *testing.T) {
	svc := &fakeVisitorReadService{state: &conversationdelivery.VisitorReadState{UnreadCount: 1, LastReadMessageID: "2"}}
	r := newVisitorReadRouter(svc)

	body := `{"last_read_message_id": " 2 "}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/sess-1/read", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	// 游标入参 TrimSpace 后透传
	if svc.gotSess != "sess-1" || svc.gotCur != "2" {
		t.Fatalf("args = %s/%s", svc.gotSess, svc.gotCur)
	}
	var state conversationdelivery.VisitorReadState
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if state.UnreadCount != 1 || state.LastReadMessageID != "2" {
		t.Fatalf("state = %+v", state)
	}

	// 缺 body 字段 → 400
	w400 := httptest.NewRecorder()
	r.ServeHTTP(w400, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/sess-1/read", strings.NewReader(`{}`)))
	if w400.Code != http.StatusBadRequest {
		t.Fatalf("missing field status = %d", w400.Code)
	}
	// 非法 JSON → 400
	wbad := httptest.NewRecorder()
	r.ServeHTTP(wbad, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/sess-1/read", strings.NewReader(`not-json`)))
	if wbad.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d", wbad.Code)
	}
}

func TestVisitorReadHandlerUnread(t *testing.T) {
	svc := &fakeVisitorReadService{state: &conversationdelivery.VisitorReadState{UnreadCount: 3, LastReadMessageID: "9"}}
	r := newVisitorReadRouter(svc)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-1/unread", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if svc.gotSess != "sess-1" {
		t.Fatalf("session arg = %s", svc.gotSess)
	}
	var state conversationdelivery.VisitorReadState
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if state.UnreadCount != 3 || state.LastReadMessageID != "9" {
		t.Fatalf("state = %+v", state)
	}
}

func TestVisitorReadHandlerErrorMapping(t *testing.T) {
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
		svc := &fakeVisitorReadService{err: tc.err}
		r := newVisitorReadRouter(svc)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-1/unread", nil))
		if w.Code != tc.want {
			t.Fatalf("%s: status = %d, want %d", tc.name, w.Code, tc.want)
		}
	}

	// POST 面同一映射
	svc := &fakeVisitorReadService{err: errors.New("session not found: sess-x")}
	r := newVisitorReadRouter(svc)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/sess-1/read", strings.NewReader(`{"last_read_message_id":"1"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("post mapping status = %d", w.Code)
	}
}
