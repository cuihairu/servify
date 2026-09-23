package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pushcontract "servify/apps/server/internal/modules/push/contract"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type fakePushRegistrationService struct {
	registration *pushcontract.PushTokenRegistration
	err          error
	gotReq       *pushcontract.RegisterPushTokenRequest
}

func (f *fakePushRegistrationService) RegisterPushToken(ctx context.Context, req *pushcontract.RegisterPushTokenRequest) (*pushcontract.PushTokenRegistration, error) {
	f.gotReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.registration, nil
}

func newVisitorPushRouter(svc *fakePushRegistrationService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	r := gin.New()
	r.POST("/api/v1/push/register", NewVisitorPushHandler(svc, logger).RegisterPushToken)
	return r
}

func visitorPushRequest(r *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/register", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestVisitorPushHandlerCreatesRegistration(t *testing.T) {
	svc := &fakePushRegistrationService{registration: &pushcontract.PushTokenRegistration{
		ID: 7, SessionID: "m-1", Platform: "ios", UpdatedAt: time.Unix(1727000000, 0),
	}}
	r := newVisitorPushRouter(svc)

	w := visitorPushRequest(r, `{"session_id":"m-1","platform":"ios","token":"apns-token"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if svc.gotReq == nil || svc.gotReq.SessionID != "m-1" || svc.gotReq.Platform != "ios" || svc.gotReq.Token != "apns-token" {
		t.Fatalf("unexpected request passthrough: %+v", svc.gotReq)
	}
	var resp pushcontract.PushTokenRegistration
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != 7 || resp.SessionID != "m-1" || resp.Platform != "ios" {
		t.Fatalf("unexpected response registration: %+v", resp)
	}
	// token 为敏感面不回显（models.PushToken json:"-" + 摘要 DTO 双保险）。
	if bytes.Contains(w.Body.Bytes(), []byte("apns-token")) {
		t.Fatalf("response must not echo token: %s", w.Body.String())
	}
}

func TestVisitorPushHandlerRejectsInvalidBody(t *testing.T) {
	r := newVisitorPushRouter(&fakePushRegistrationService{})
	w := visitorPushRequest(r, `{"session_id":"m-1"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestVisitorPushHandlerMapsSessionNotFound(t *testing.T) {
	r := newVisitorPushRouter(&fakePushRegistrationService{err: errors.New("session not found: m-gone")})
	w := visitorPushRequest(r, `{"session_id":"m-gone","platform":"ios","token":"tok"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestVisitorPushHandlerMapsUnsupportedPlatform(t *testing.T) {
	r := newVisitorPushRouter(&fakePushRegistrationService{err: errors.New("unsupported platform: web")})
	w := visitorPushRequest(r, `{"session_id":"m-1","platform":"web","token":"tok"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestVisitorPushHandlerMapsInternalError(t *testing.T) {
	r := newVisitorPushRouter(&fakePushRegistrationService{err: errors.New("db down")})
	w := visitorPushRequest(r, `{"session_id":"m-1","platform":"ios","token":"tok"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}
}
