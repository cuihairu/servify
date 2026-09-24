package realtime

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// recordingValidator 记录型校验器：记录入参并返回预设错误/nil。
type recordingValidator struct {
	err            error
	gotSessionID   string
	gotAccessToken string
	calls          int
}

func (r *recordingValidator) validate(sessionID, accessToken string) error {
	r.calls++
	r.gotSessionID, r.gotAccessToken = sessionID, accessToken
	return r.err
}

func newValidatorRequest(sessionID, accessToken string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	url := "/api/v1/ws?session_id=" + sessionID + "&access_token=" + accessToken
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	c.Request = req
	return c, recorder
}

// TestWebSocketHub_TokenValidatorRejects：校验器非 nil 且返回错误时，
// 握手 401 拒绝升级、不注册客户端（CreateTestContext 不依赖本地端口
// 绑定，确定性覆盖拒绝分支——dial 套件挂在 integration 标签下，未打
// 标的覆盖率跑不经过它）。
func TestWebSocketHub_TokenValidatorRejects(t *testing.T) {
	hub := NewWebSocketHub()
	rec := &recordingValidator{err: errors.New("token expired")}
	hub.SetTokenValidator(rec.validate)
	if hub.CurrentTokenValidator() == nil {
		t.Fatal("exported validator accessor must surface the mounted validator")
	}

	c, recorder := newValidatorRequest("sess-1", "bad")
	hub.HandleWebSocket(c)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if rec.calls != 1 || rec.gotSessionID != "sess-1" || rec.gotAccessToken != "bad" {
		t.Fatalf("validator args = %s/%s calls=%d", rec.gotSessionID, rec.gotAccessToken, rec.calls)
	}
	if hub.GetClientCount() != 0 {
		t.Fatalf("rejected handshake must not register a client, got %d", hub.GetClientCount())
	}
}

// TestWebSocketHub_TokenValidatorAcceptsFallsThrough：校验通过（nil）时
// 放行到升级阶段——recorder 非 Hijacker，gorilla 升级必然失败（既有行
// 为面），但断言锚定的是"询过校验器且未写 401"，即放行分支可达。
func TestWebSocketHub_TokenValidatorAcceptsFallsThrough(t *testing.T) {
	hub := NewWebSocketHub()
	rec := &recordingValidator{err: nil}
	hub.SetTokenValidator(rec.validate)

	c, recorder := newValidatorRequest("sess-ok", "valid")
	hub.HandleWebSocket(c) // 升级失败仅记日志，不 panic

	if recorder.Code == http.StatusUnauthorized {
		t.Fatalf("valid token must not be rejected, body = %s", recorder.Body.String())
	}
	if rec.calls != 1 || rec.gotSessionID != "sess-ok" || rec.gotAccessToken != "valid" {
		t.Fatalf("validator args = %s/%s calls=%d", rec.gotSessionID, rec.gotAccessToken, rec.calls)
	}
}

// TestWebSocketHub_TokenValidatorNilRestoresLegacy：nil 校验器（装配态
// required 关闭）恢复免校验行为——快照读取随 Set 翻转。
func TestWebSocketHub_TokenValidatorNilRestoresLegacy(t *testing.T) {
	hub := NewWebSocketHub()
	rec := &recordingValidator{err: nil}
	hub.SetTokenValidator(rec.validate)
	if hub.currentTokenValidator() == nil {
		t.Fatal("validator must be readable after Set")
	}
	hub.SetTokenValidator(nil)
	if hub.currentTokenValidator() != nil {
		t.Fatal("Set(nil) must restore legacy no-validation state")
	}
}
