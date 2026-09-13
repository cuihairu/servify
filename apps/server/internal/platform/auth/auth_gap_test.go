package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/gin-gonic/gin"
)

// TestHasPermissionBlankRequiredAndBlankGranted 覆盖空 required 与
// granted 列表中的空白项跳过分支。
func TestHasPermissionBlankRequiredAndBlankGranted(t *testing.T) {
	// 空 required 一律放行
	if !HasPermission(nil, "   ") {
		t.Fatal("blank required permission should be allowed")
	}

	// granted 中的空白项被跳过,不等于匹配
	if HasPermission([]string{"", "   "}, "tickets.read") {
		t.Fatal("blank granted entries must not satisfy a permission")
	}
	if !HasPermission([]string{" ", "tickets.read"}, "tickets.read") {
		t.Fatal("exact match after blank entries should be allowed")
	}
}

// TestComposeTokenPoliciesStopsOnError 覆盖组合策略的错误透传分支。
func TestComposeTokenPoliciesStopsOnError(t *testing.T) {
	wantErr := errors.New("policy failed")
	composed := ComposeTokenPolicies(
		nil, // nil 策略直接跳过
		func(map[string]interface{}, Claims, time.Time) error { return wantErr },
		func(map[string]interface{}, Claims, time.Time) error {
			t.Fatal("policies after a failure must not run")
			return nil
		},
	)

	if err := composed(map[string]interface{}{}, Claims{}, time.Now()); !errors.Is(err, wantErr) {
		t.Fatalf("composed policy error = %v, want %v", err, wantErr)
	}
}

// signRawHS256 用指定密钥对任意 signing input 生成 HS256 签名段。
func signRawHS256(secret, signingInput string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// TestValidateTokenBadPayloadEncoding 覆盖 payload 段 base64 解码失败分支:
// 签名本身合法(对原始 payload 段计算),但内容不是合法 base64。
func TestValidateTokenBadPayloadEncoding(t *testing.T) {
	v := Validator{Secret: "secret"}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	payload := "!!not-base64!!"
	token := header + "." + payload + "." + signRawHS256("secret", header+"."+payload)

	if _, err := v.ValidateToken(token); err == nil || !strings.Contains(err.Error(), "invalid payload encoding") {
		t.Fatalf("err = %v, want invalid payload encoding", err)
	}
}

// TestValidateTokenBadPayloadJSON 覆盖 payload JSON 解析失败分支。
func TestValidateTokenBadPayloadJSON(t *testing.T) {
	v := Validator{Secret: "secret"}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{not-json`))
	token := header + "." + payload + "." + signRawHS256("secret", header+"."+payload)

	if _, err := v.ValidateToken(token); err == nil || !strings.Contains(err.Error(), "invalid payload json") {
		t.Fatalf("err = %v, want invalid payload json", err)
	}
}

// TestProjectRequestScopeNoopWhenNothingResolved 覆盖租户与工作区都解析为空
// 时的直接返回分支。
func TestProjectRequestScopeNoopWhenNothingResolved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	c.Request = req

	projectRequestScope(c, "", "", "", "")

	if got, ok := c.Get("tenant_id"); ok {
		t.Fatalf("tenant_id should not be set, got %v", got)
	}
	if got, ok := c.Get("workspace_id"); ok {
		t.Fatalf("workspace_id should not be set, got %v", got)
	}
	if c.Request != req {
		t.Fatal("request must stay untouched when nothing is resolved")
	}
}

// TestGormAPIKeyResolverUnexpectedDBError 覆盖非 RecordNotFound 的查询错误分支。
func TestGormAPIKeyResolverUnexpectedDBError(t *testing.T) {
	db := newAPIKeyTestDB(t)
	if err := db.Migrator().DropTable(&models.APIKey{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	resolver := NewGormAPIKeyResolver(db)
	plaintext, _, _, _ := GenerateAPIKey()
	_, err := resolver.ResolveAPIKey(t.Context(), plaintext)
	if err == nil || errors.Is(err, ErrAPIKeyInvalid) {
		t.Fatalf("err = %v, want a raw DB error", err)
	}
}

// TestGormAPIKeyResolverEmptyScopesColumn 覆盖 splitScopes 的空串与逗号
// 分割两个分支:scopes 列为空时回落默认集,非空时按逗号拆分。
func TestGormAPIKeyResolverEmptyScopesColumn(t *testing.T) {
	db := newAPIKeyTestDB(t)
	resolver := NewGormAPIKeyResolver(db)

	plaintext, _, _, _ := GenerateAPIKey()
	if err := db.Create(&models.APIKey{Name: "nos", KeyHash: APIKeyHash(plaintext), Scopes: ""}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	record, err := resolver.ResolveAPIKey(t.Context(), plaintext)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if splitScopes("") != nil {
		t.Fatal("splitScopes(\"\") must return nil")
	}
	if len(record.Scopes) != len(APIKeyDefaultPermissions) {
		t.Fatalf("expected default scopes, got %v", record.Scopes)
	}

	// scopes 列带逗号:按逗号拆分并保留原始顺序
	if got := splitScopes("a.read,b.write"); len(got) != 2 || got[0] != "a.read" || got[1] != "b.write" {
		t.Fatalf("splitScopes(csv) = %v", got)
	}

	plaintext2, _, _, _ := GenerateAPIKey()
	if err := db.Create(&models.APIKey{Name: "csv", KeyHash: APIKeyHash(plaintext2), Scopes: "custom.a,custom.b"}).Error; err != nil {
		t.Fatalf("seed csv: %v", err)
	}
	record2, err := resolver.ResolveAPIKey(t.Context(), plaintext2)
	if err != nil {
		t.Fatalf("resolve csv: %v", err)
	}
	if len(record2.Scopes) != 2 || record2.Scopes[0] != "custom.a" {
		t.Fatalf("csv scopes not resolved: %v", record2.Scopes)
	}
}

// TestAuthMiddleware_APIKeyWorkspaceProjected 覆盖 handleAPIKeyAuth 的
// workspace_id 注入分支。
func TestAuthMiddleware_APIKeyWorkspaceProjected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newAPIKeyTestDB(t)
	plaintext, _, _, _ := GenerateAPIKey()
	if err := db.Create(&models.APIKey{Name: "ws", KeyHash: APIKeyHash(plaintext), TenantID: "t-ws", WorkspaceID: "w-ws"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	var tenantID, workspaceID interface{}
	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{Secret: "jwt-secret", APIKeyResolver: NewGormAPIKeyResolver(db)}))
	r.GET("/ping", func(c *gin.Context) {
		tenantID, _ = c.Get("tenant_id")
		workspaceID, _ = c.Get("workspace_id")
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("X-API-Key", plaintext)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if tenantID != "t-ws" || workspaceID != "w-ws" {
		t.Fatalf("scope not projected: tenant=%v workspace=%v", tenantID, workspaceID)
	}
}

// TestUserStateTokenPolicySessionQueryFailure 覆盖会话查询的非 NotFound 错误分支
// (sessions 表缺失)。
func TestUserStateTokenPolicySessionQueryFailure(t *testing.T) {
	db := testAuthDB(t) // 只迁移了 User / RevokedToken,没有 UserAuthSession
	if err := db.Create(&models.User{ID: 42, Username: "u42", Email: "u42@example.com", Status: "active"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	policy := NewUserStateTokenPolicy(db)
	err := policy(
		map[string]interface{}{"iat": float64(time.Now().Unix())},
		Claims{HasUserID: true, UserID: 42, SessionID: "sess-gone"},
		time.Now(),
	)
	if err == nil || !strings.Contains(err.Error(), "failed to evaluate token session state") {
		t.Fatalf("err = %v, want session state evaluation failure", err)
	}
}
