package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

var guestTokenNow = time.Unix(1727000000, 0)

// TestSignAndParseGuestTokenRoundtrip 锚定签发/校验闭环：sid 绑定、
// exp = 签发时刻 + ttl、有效期内可解析。
func TestSignAndParseGuestTokenRoundtrip(t *testing.T) {
	tok, exp, err := SignGuestToken("secret-1", "sess-1", time.Hour, guestTokenNow)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if exp != guestTokenNow.Add(time.Hour).Unix() {
		t.Fatalf("exp = %d, want %d", exp, guestTokenNow.Add(time.Hour).Unix())
	}

	claims, err := ParseGuestToken("secret-1", tok, guestTokenNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.SessionID != "sess-1" || claims.ExpiresAt != exp {
		t.Fatalf("claims = %+v", claims)
	}
}

// TestParseGuestTokenRejections 锚定校验拒绝面：过期、密钥不符、
// typ 非 guest、缺 sid、缺 exp、畸形 token、空 secret。
func TestParseGuestTokenRejections(t *testing.T) {
	tok, _, err := SignGuestToken("secret-1", "sess-1", time.Hour, guestTokenNow)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	cases := []struct {
		name    string
		secret  string
		token   string
		now     time.Time
		wantErr string
	}{
		{"expired", "secret-1", tok, guestTokenNow.Add(time.Hour + time.Second), "time constraint"},
		{"wrong secret", "secret-2", tok, guestTokenNow, "invalid signature"},
		{"malformed", "secret-1", "not-a-jwt", guestTokenNow, "invalid token format"},
		{"empty secret", "", tok, guestTokenNow, "secret is required"},
	}
	for _, tc := range cases {
		if _, err := ParseGuestToken(tc.secret, tc.token, tc.now); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("%s: expected %q, got %v", tc.name, tc.wantErr, err)
		}
	}

	// typ 非 guest：手搓同签名形态的 agent token
	if _, err := ParseGuestToken("secret-1", signRawClaims(t, "secret-1", map[string]interface{}{"typ": "agent", "sid": "sess-1", "exp": float64(guestTokenNow.Add(time.Hour).Unix())}), guestTokenNow); err == nil || !strings.Contains(err.Error(), "not a guest token") {
		t.Fatalf("expected non-guest rejection, got %v", err)
	}
	// 缺 sid
	if _, err := ParseGuestToken("secret-1", signRawClaims(t, "secret-1", map[string]interface{}{"typ": "guest", "exp": float64(guestTokenNow.Add(time.Hour).Unix())}), guestTokenNow); err == nil || !strings.Contains(err.Error(), "missing session binding") {
		t.Fatalf("expected missing sid rejection, got %v", err)
	}
	// 缺 exp
	if _, err := ParseGuestToken("secret-1", signRawClaims(t, "secret-1", map[string]interface{}{"typ": "guest", "sid": "sess-1"}), guestTokenNow); err == nil || !strings.Contains(err.Error(), "missing expiry") {
		t.Fatalf("expected missing expiry rejection, got %v", err)
	}
}

// TestSignGuestTokenGuards 锚定签发入参守卫。
func TestSignGuestTokenGuards(t *testing.T) {
	cases := []struct {
		name    string
		secret  string
		session string
		ttl     time.Duration
		wantErr string
	}{
		{"blank secret", "  ", "sess-1", time.Hour, "secret is required"},
		{"blank session", "secret-1", " ", time.Hour, "session_id is required"},
		{"zero ttl", "secret-1", "sess-1", 0, "ttl must be positive"},
	}
	for _, tc := range cases {
		if _, _, err := SignGuestToken(tc.secret, tc.session, tc.ttl, guestTokenNow); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("%s: expected %q, got %v", tc.name, tc.wantErr, err)
		}
	}
}

// TestGuestTokenViaAuthMiddleware 锚定 REST 面推导（V1.0 收敛 B3-2）：
// guest token 走 AuthMiddleware 时 principal_kind 推导为 end_user、sid
// 提取为 session_id——POST /api/v1/ai/feedback 的访客会话绑定校验依赖
// 这两个上下文键；WS 握手仍走独立 validator，不受影响。
func TestGuestTokenViaAuthMiddleware(t *testing.T) {
	tok, _, err := SignGuestToken("secret-1", "sess-9", time.Hour, guestTokenNow)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{
		Secret: "secret-1",
		Now:    func() time.Time { return guestTokenNow.Add(time.Minute) },
	}))
	r.POST("/ai/feedback", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"principal_kind": c.GetString("principal_kind"),
			"session_id":     c.GetString("session_id"),
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/ai/feedback", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["principal_kind"] != PrincipalEndUser || out["session_id"] != "sess-9" {
		t.Fatalf("context = %v（必须 end_user + sid 绑定）", out)
	}
}

// signRawClaims 以与生产一致的 HS256 形态签任意 payload（构造
// 校验拒绝面的非法声明组合用：typ 非 guest / 缺 sid / 缺 exp）。
func signRawClaims(t *testing.T, secret string, payload map[string]interface{}) string {
	t.Helper()
	headerJSON, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payloadJSON, _ := json.Marshal(payload)
	enc := base64.RawURLEncoding.EncodeToString
	signing := enc(headerJSON) + "." + enc(payloadJSON)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	return signing + "." + enc(mac.Sum(nil))
}
