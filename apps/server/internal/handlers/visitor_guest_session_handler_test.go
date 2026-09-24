package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type stubGuestIssuer struct {
	token      string
	expiresAt  int64
	err        error
	gotSession string
}

func (s *stubGuestIssuer) IssueGuestToken(sessionID string) (string, int64, error) {
	s.gotSession = sessionID
	return s.token, s.expiresAt, s.err
}

func newGuestSessionContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/guest/session", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, recorder
}

// TestVisitorGuestSessionIssueOK 锚定 201 闭环：透传 session_id、响应体
// 带 access_token/token_type/expires_at。
func TestVisitorGuestSessionIssueOK(t *testing.T) {
	stub := &stubGuestIssuer{token: "tok-1", expiresAt: 1727000000}
	h := NewVisitorGuestSessionHandler(stub, nil)

	c, recorder := newGuestSessionContext(`{"session_id":"sess-9"}`)
	h.Issue(c)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if stub.gotSession != "sess-9" {
		t.Fatalf("issuer got session %q", stub.gotSession)
	}
	var resp GuestSessionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken != "tok-1" || resp.TokenType != "bearer" || resp.ExpiresAt != 1727000000 {
		t.Fatalf("response = %+v", resp)
	}
}

// TestVisitorGuestSessionIssueBadRequests 锚定 400 面：绑定失败与空白
// session_id（绕过 binding 的纯空白输入）。
func TestVisitorGuestSessionIssueBadRequests(t *testing.T) {
	h := NewVisitorGuestSessionHandler(&stubGuestIssuer{}, nil)

	c, recorder := newGuestSessionContext(`{}`)
	h.Issue(c)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing field: status = %d", recorder.Code)
	}

	c, recorder = newGuestSessionContext(`{"session_id":"   "}`)
	h.Issue(c)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("blank session_id: status = %d", recorder.Code)
	}
}

// TestVisitorGuestSessionIssueIssuerError 锚定签发失败 500 兜底。
func TestVisitorGuestSessionIssueIssuerError(t *testing.T) {
	h := NewVisitorGuestSessionHandler(&stubGuestIssuer{err: errors.New("boom")}, nil)

	c, recorder := newGuestSessionContext(`{"session_id":"sess-9"}`)
	h.Issue(c)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
