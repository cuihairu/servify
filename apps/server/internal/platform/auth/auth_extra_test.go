package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"

	"github.com/gin-gonic/gin"
)

func rawB64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func TestContextScopeRoundTrip(t *testing.T) {
	if TenantIDFromContext(nil) != "" || WorkspaceIDFromContext(nil) != "" {
		t.Fatal("nil context should yield empty scope")
	}
	if TenantIDFromContext(context.Background()) != "" {
		t.Fatal("background context should yield empty tenant")
	}
	ctx := ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	if TenantIDFromContext(ctx) != "tenant-a" || WorkspaceIDFromContext(ctx) != "workspace-1" {
		t.Fatalf("unexpected scope values: %q %q", TenantIDFromContext(ctx), WorkspaceIDFromContext(ctx))
	}
}

func TestSubjectFromGinVariants(t *testing.T) {
	if got := SubjectFromGin(nil); got.HasUserID || got.TenantID != "" || got.PrincipalType != "" {
		t.Fatalf("nil context = %+v", got)
	}

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ContextUserID, uint(9))
	c.Set(ContextUserIDRaw, "9")
	c.Set(ContextTenantID, "tenant-a")
	c.Set(ContextWorkspaceID, "ws-1")
	c.Set(ContextTokenType, "refresh")
	c.Set(ContextPrincipalType, PrincipalService)
	c.Set(ContextRoles, []interface{}{"ops", 7, " "})
	c.Set(ContextPermissions, []string{"tickets.read", ""})

	subject := SubjectFromGin(c)
	if !subject.HasUserID || subject.UserID != 9 || subject.UserIDRaw != "9" {
		t.Fatalf("unexpected subject id fields: %+v", subject)
	}
	if subject.TenantID != "tenant-a" || subject.WorkspaceID != "ws-1" || subject.TokenType != "refresh" {
		t.Fatalf("unexpected scope fields: %+v", subject)
	}
	if len(subject.Roles) != 2 || subject.Roles[0] != "ops" {
		t.Fatalf("unexpected roles: %+v", subject.Roles)
	}
	if len(subject.Permissions) != 2 || subject.Permissions[0] != "tickets.read" {
		t.Fatalf("unexpected permissions: %+v", subject.Permissions)
	}

	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Set(ContextUserID, "not-uint")
	if subject := SubjectFromGin(c2); subject.HasUserID {
		t.Fatalf("expected HasUserID=false, got %+v", subject)
	}
}

func TestIsInternalPrincipalType(t *testing.T) {
	if !IsInternalPrincipalType(PrincipalService) || !IsInternalPrincipalType(PrincipalAdmin) {
		t.Fatal("service/admin should be internal")
	}
	if IsInternalPrincipalType(PrincipalAgent) || IsInternalPrincipalType("bogus") {
		t.Fatal("agent/bogus should not be internal")
	}
	if !(Subject{PrincipalType: PrincipalAdmin}).IsInternalPrincipal() {
		t.Fatal("admin subject should be internal")
	}
	if (Subject{PrincipalType: PrincipalAgent}).IsInternalPrincipal() {
		t.Fatal("agent subject should not be internal")
	}
}

func TestScopeHelpers(t *testing.T) {
	cases := []struct {
		scope  Scope
		mode   string
		valid  bool
		tenant bool
		ws     bool
	}{
		{Scope{}, ScopeModeGlobal, true, false, false},
		{Scope{TenantID: "t"}, ScopeModeTenant, true, true, false},
		{Scope{TenantID: "t", WorkspaceID: "w"}, ScopeModeWorkspace, true, true, true},
		{Scope{WorkspaceID: "w"}, ScopeModeWorkspace, false, false, true},
	}
	for _, tc := range cases {
		if got := tc.scope.Mode(); got != tc.mode {
			t.Fatalf("Mode() = %q want %q", got, tc.mode)
		}
		if err := tc.scope.Validate(); (err == nil) != tc.valid {
			t.Fatalf("Validate() = %v for %+v", err, tc.scope)
		}
		if tc.scope.HasTenant() != tc.tenant || tc.scope.HasWorkspace() != tc.ws {
			t.Fatalf("HasTenant/HasWorkspace mismatch for %+v", tc.scope)
		}
	}

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ContextTenantID, "t2")
	c.Set(ContextWorkspaceID, "w2")
	if s := ScopeFromGin(c); s.TenantID != "t2" || s.WorkspaceID != "w2" {
		t.Fatalf("ScopeFromGin = %+v", s)
	}
	if s := ScopeFromSubject(Subject{TenantID: "t3"}); s.TenantID != "t3" || s.HasWorkspace() {
		t.Fatalf("ScopeFromSubject = %+v", s)
	}
}

func TestMiddlewareConfigFromApp(t *testing.T) {
	if got := MiddlewareConfigFromApp(nil); got.Secret != "" {
		t.Fatalf("nil config should give empty middleware config, got %+v", got)
	}
	cfg := &config.Config{}
	cfg.JWT.Secret = "jwt-secret"
	cfg.Security.RBAC.Enabled = true
	got := MiddlewareConfigFromApp(cfg)
	if got.Secret != "jwt-secret" || !got.RBAC.Enabled {
		t.Fatalf("unexpected config projection: %+v", got)
	}
}

func TestAuthMiddlewareFailurePaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Unix(1_700_000_000, 0)
	secret := "test-secret"

	build := func(cfg MiddlewareConfig) *gin.Engine {
		r := gin.New()
		r.Use(AuthMiddleware(cfg))
		r.GET("/secure", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
		return r
	}

	do := func(r *gin.Engine, auth string) int {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/secure", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		r.ServeHTTP(w, req)
		return w.Code
	}

	validCfg := MiddlewareConfig{Secret: secret, Now: func() time.Time { return now }}
	token := createTestHS256JWT(t, map[string]interface{}{
		"token_type": "service",
		"iat":        now.Unix(),
		"exp":        now.Add(10 * time.Minute).Unix(),
	}, secret)

	if code := do(build(validCfg), ""); code != http.StatusUnauthorized {
		t.Fatalf("no auth header = %d want 401", code)
	}
	if code := do(build(validCfg), "Basic abc"); code != http.StatusUnauthorized {
		t.Fatalf("non-bearer = %d want 401", code)
	}
	if code := do(build(validCfg), "Bearer   "); code != http.StatusUnauthorized {
		t.Fatalf("empty token = %d want 401", code)
	}
	if code := do(build(MiddlewareConfig{Now: func() time.Time { return now }}), "Bearer "+token); code != http.StatusUnauthorized {
		t.Fatalf("empty secret = %d want 401", code)
	}
	badSig := createTestHS256JWT(t, map[string]interface{}{"exp": now.Add(time.Hour).Unix()}, "other")
	if code := do(build(validCfg), "Bearer "+badSig); code != http.StatusUnauthorized {
		t.Fatalf("bad signature = %d want 401", code)
	}
	expired := createTestHS256JWT(t, map[string]interface{}{"exp": now.Add(-time.Hour).Unix()}, secret)
	if code := do(build(validCfg), "Bearer "+expired); code != http.StatusUnauthorized {
		t.Fatalf("expired token = %d want 401", code)
	}

	policyCfg := validCfg
	policyCfg.Policy = func(_ map[string]interface{}, _ Claims, _ time.Time) error {
		return context.DeadlineExceeded
	}
	if code := do(build(policyCfg), "Bearer "+token); code != http.StatusUnauthorized {
		t.Fatalf("policy rejection = %d want 401", code)
	}
}

func TestAuthMiddlewareProjectsOptionalClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Unix(1_700_000_000, 0)
	secret := "test-secret"
	token := createTestHS256JWT(t, map[string]interface{}{
		"user_id":     3.0,
		"roles":       []interface{}{"agent", "lead"},
		"perms":       []string{"tickets.read"},
		"tenant":      "tenant-x",
		"workspace":   "ws-x",
		"jti":         "tok-1",
		"session_id":  "sess-1",
		"token_type":  "access",
		"token_kind2": "ignored",
		"iat":         now.Unix(),
		"exp":         now.Add(10 * time.Minute).Unix(),
	}, secret)

	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{Secret: secret, Now: func() time.Time { return now }}))
	r.GET("/secure", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"session_id": c.GetString("session_id"),
			"user_id":    c.GetUint("user_id"),
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "sess-1") {
		t.Fatalf("expected session_id projected, body=%s", body)
	}
}

func TestRequirePermissionsAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("permissions", []string{"tickets.read", "tickets.write"})
		c.Next()
	})
	r.GET("/all", RequirePermissionsAll("tickets.read", "tickets.write"))
	r.GET("/missing", RequirePermissionsAll("tickets.read", "tickets.delete"))
	r.GET("/blank", RequirePermissionsAll("  ", "tickets.read"))
	r.GET("/none", RequirePermissionsAll())

	cases := map[string]int{
		"/all":     http.StatusOK,
		"/missing": http.StatusForbidden,
		"/blank":   http.StatusOK,
		"/none":    http.StatusOK,
	}
	for path, want := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Fatalf("%s = %d want %d", path, w.Code, want)
		}
	}
}

func TestRequirePermissionsAnyDeny(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/deny", RequirePermissionsAny("tickets.read"))
	r.GET("/blank", RequirePermissionsAny("   "))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/deny", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("deny = %d want 403", w.Code)
	}
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/blank", nil))
	if w2.Code != http.StatusForbidden {
		t.Fatalf("blank = %d want 403", w2.Code)
	}
}

func TestRequireRolesAny(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("roles", []string{"agent"})
		c.Next()
	})
	r.GET("/ok", RequireRolesAny("admin", " agent "))
	r.GET("/deny", RequireRolesAny("admin"))
	r.GET("/blank", RequireRolesAny("  "))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ok = %d want 200", w.Code)
	}
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/deny", nil))
	if w2.Code != http.StatusForbidden {
		t.Fatalf("deny = %d want 403", w2.Code)
	}
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/blank", nil))
	if w3.Code != http.StatusForbidden {
		t.Fatalf("blank = %d want 403", w3.Code)
	}
}

func TestGetGrantedRolesAndPrincipalKindVariants(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c1, _ := gin.CreateTestContext(httptest.NewRecorder())
	c1.Set(ContextRoles, "single-role")
	if roles := getGrantedRoles(c1); len(roles) != 1 || roles[0] != "single-role" {
		t.Fatalf("string roles = %+v", roles)
	}

	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Set(ContextRoles, "")
	if roles := getGrantedRoles(c2); roles != nil {
		t.Fatalf("empty string roles = %+v", roles)
	}

	c3, _ := gin.CreateTestContext(httptest.NewRecorder())
	c3.Set(ContextRoles, 42)
	if roles := getGrantedRoles(c3); roles != nil {
		t.Fatalf("int roles = %+v", roles)
	}

	c4, _ := gin.CreateTestContext(httptest.NewRecorder())
	if roles := getGrantedRoles(c4); roles != nil {
		t.Fatalf("no roles = %+v", roles)
	}

	c5, _ := gin.CreateTestContext(httptest.NewRecorder())
	c5.Set(ContextPermissions, "not-a-list")
	if perms := getGrantedPermissions(c5); perms != nil {
		t.Fatalf("string permissions = %+v", perms)
	}

	c6, _ := gin.CreateTestContext(httptest.NewRecorder())
	c6.Set("principal_kind", "service")
	if kind := getPrincipalKind(c6); kind != PrincipalService {
		t.Fatalf("normalized kind = %q", kind)
	}

	c6b, _ := gin.CreateTestContext(httptest.NewRecorder())
	c6b.Set("principal_kind", "bogus")
	if kind := getPrincipalKind(c6b); kind != PrincipalUnknown {
		t.Fatalf("bogus kind = %q", kind)
	}

	c7, _ := gin.CreateTestContext(httptest.NewRecorder())
	c7.Set(ContextPrincipalType, 7)
	if kind := getPrincipalKind(c7); kind != PrincipalUnknown {
		t.Fatalf("int kind = %q", kind)
	}

	c8, _ := gin.CreateTestContext(httptest.NewRecorder())
	if kind := getPrincipalKind(c8); kind != PrincipalUnknown {
		t.Fatalf("missing kind = %q", kind)
	}
}

func TestExtractClaimsVariants(t *testing.T) {
	resolver := Resolver{}
	claims := extractClaims(map[string]interface{}{
		"user_id":     json.Number("not-int"),
		"tid":         "tenant-n",
		"wid":         "ws-n",
		"token_id":    "tok-n",
		"sid":         "sess-n",
		"roles":       "a, b ,,",
		"permissions": []interface{}{"p1", 2, " "},
	}, resolver)
	if claims.UserIDRaw == nil || claims.HasUserID {
		t.Fatalf("invalid json.Number should fall back to raw: %+v", claims)
	}
	if claims.TenantID != "tenant-n" || claims.WorkspaceID != "ws-n" {
		t.Fatalf("alias claims = %+v", claims)
	}
	if claims.TokenID != "tok-n" || claims.SessionID != "sess-n" {
		t.Fatalf("token/session = %+v", claims)
	}
	if len(claims.Roles) != 2 || claims.Roles[0] != "a" || claims.Roles[1] != "b" {
		t.Fatalf("csv roles = %+v", claims.Roles)
	}

	numClaims := extractClaims(map[string]interface{}{
		"user_id": json.Number("12"),
		"roles":   []interface{}{"x"},
	}, resolver)
	if !numClaims.HasUserID || numClaims.UserID != 12 {
		t.Fatalf("json.Number user_id = %+v", numClaims)
	}

	subClaims := extractClaims(map[string]interface{}{
		"sub": "customer-9",
	}, resolver)
	if subClaims.HasUserID || subClaims.UserIDRaw != "customer-9" {
		t.Fatalf("string sub = %+v", subClaims)
	}

	dict := map[string]interface{}{"roles": map[string]string{"a": "b"}}
	if roles := normalizeStringList(dict["roles"]); roles != nil {
		t.Fatalf("map roles = %+v", roles)
	}
	if roles := normalizeStringList(nil); roles != nil {
		t.Fatalf("nil roles = %+v", roles)
	}
	if roles := normalizeStringList([]string{"  ", ""}); len(roles) != 0 {
		t.Fatalf("blank roles = %+v", roles)
	}
	if roles := normalizeStringList([]interface{}{1, 2}); len(roles) != 0 {
		t.Fatalf("int list roles = %+v", roles)
	}
	if roles := normalizeStringList("   "); roles != nil {
		t.Fatalf("blank string roles = %+v", roles)
	}
	if out := dedupeStrings([]string{" a ", "a", "", "b"}); len(out) != 2 || out[0] != "a" || out[1] != "b" {
		t.Fatalf("dedupe = %+v", out)
	}
}

func TestEnforceRequestScopeMoreBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Unix(1_700_000_000, 0)
	secret := "test-secret"

	adminToken := createTestHS256JWT(t, map[string]interface{}{
		"token_type": "admin",
		"tenant_id":  "tenant-a",
		"iat":        now.Unix(),
		"exp":        now.Add(10 * time.Minute).Unix(),
	}, secret)

	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{Secret: secret, Now: func() time.Time { return now }}))
	r.Use(EnforceRequestScope())
	r.GET("/scoped", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	// admin conflicting workspace selectors
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/scoped?workspace_id=ws-a&workspace=ws-b", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("conflicting workspace = %d want 400", w.Code)
	}

	// admin requesting mismatched tenant scope
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/scoped?tenant_id=tenant-b", nil)
	req2.Header.Set("Authorization", "Bearer "+adminToken)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("admin mismatched tenant = %d want 403", w2.Code)
	}

	// admin matching tenant scope passes
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodGet, "/scoped?tenant_id=tenant-a", nil)
	req3.Header.Set("Authorization", "Bearer "+adminToken)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("admin matching tenant = %d want 200", w3.Code)
	}

	// agent widening scope
	agentToken := createTestHS256JWT(t, map[string]interface{}{
		"user_id":   1,
		"roles":     []string{"agent"},
		"tenant_id": "tenant-a",
		"iat":       now.Unix(),
		"exp":       now.Add(10 * time.Minute).Unix(),
	}, secret)
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest(http.MethodGet, "/scoped?workspace_id=ws-9", nil)
	req4.Header.Set("Authorization", "Bearer "+agentToken)
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusForbidden {
		t.Fatalf("agent widen = %d want 403", w4.Code)
	}

	// agent matching scope passes
	w5 := httptest.NewRecorder()
	req5, _ := http.NewRequest(http.MethodGet, "/scoped", nil)
	req5.Header.Set("Authorization", "Bearer "+agentToken)
	r.ServeHTTP(w5, req5)
	if w5.Code != http.StatusOK {
		t.Fatalf("agent matching = %d want 200", w5.Code)
	}
}

func TestValidatorMoreFailurePaths(t *testing.T) {
	v := Validator{Secret: "s", Now: func() time.Time { return time.Unix(1_700_000_000, 0) }}
	if _, err := v.ValidateToken("a.b"); err == nil {
		t.Fatal("2-part token should fail")
	}
	if _, err := v.ValidateToken("!!!.e30.fmq"); err == nil {
		t.Fatal("bad header b64 should fail")
	}
	if _, err := v.ValidateToken("aGk.bW9t.fmq"); err == nil {
		t.Fatal("bad header json should fail")
	}
	header := base64URL(map[string]string{"alg": "RS256", "typ": "JWT"})
	if _, err := v.ValidateToken(header + ".e30.fmq"); err == nil {
		t.Fatal("unsupported alg should fail")
	}
	validHeader := base64URL(map[string]string{"alg": "HS256", "typ": "JWT"})
	if _, err := v.ValidateToken(validHeader + ".e30.!!!"); err == nil {
		t.Fatal("bad signature b64 should fail")
	}
	if _, err := v.ValidateToken(validHeader + ".!!!.aa"); err == nil {
		t.Fatal("bad payload b64 should fail")
	}
	if _, err := v.ValidateToken(validHeader + ".aGk.aa"); err == nil {
		t.Fatal("bad payload json should fail")
	}
}

func base64URL(v interface{}) string {
	data, _ := json.Marshal(v)
	return rawB64(data)
}
func TestValidatorTimeClaimVariants(t *testing.T) {
	nowSec := int64(1_700_000_000)
	if err := validateTimeClaims(map[string]interface{}{"nbf": float64(nowSec + 100)}, nowSec); err == nil {
		t.Fatal("future nbf should fail")
	}
	if err := validateTimeClaims(map[string]interface{}{"iat": json.Number(strconv.FormatInt(nowSec+100, 10))}, nowSec); err == nil {
		t.Fatal("future iat should fail")
	}
	if err := validateTimeClaims(map[string]interface{}{"nbf": "text", "iat": nil}, nowSec); err != nil {
		t.Fatalf("ignored claim types should pass, got %v", err)
	}
	if err := validateTimeClaims(map[string]interface{}{"exp": json.Number("not-a-number")}, nowSec); err == nil {
		t.Fatal("unparseable exp should fail")
	}
	if err := validateTimeClaims(map[string]interface{}{"nbf": float64(nowSec), "iat": float64(nowSec), "exp": float64(nowSec + 1)}, nowSec); err != nil {
		t.Fatalf("valid claims should pass, got %v", err)
	}
}

func TestTokenPoliciesMoreBranches(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	if err := RejectIssuedBefore(0)(nil, Claims{}, now); err != nil {
		t.Fatalf("disabled policy = %v", err)
	}
	if err := RejectIssuedBefore(100)(map[string]interface{}{}, Claims{}, now); err == nil {
		t.Fatal("missing iat should fail")
	}
	if err := RejectIssuedBefore(100)(map[string]interface{}{"iat": float64(50)}, Claims{}, now); err == nil {
		t.Fatal("revoked iat should fail")
	}
	if err := RejectIssuedBefore(100)(map[string]interface{}{"iat": json.Number("150")}, Claims{}, now); err != nil {
		t.Fatalf("valid iat = %v", err)
	}

	if err := RequireMinimumTokenVersion(0)(nil, Claims{}, now); err != nil {
		t.Fatalf("disabled version policy = %v", err)
	}
	if err := RequireMinimumTokenVersion(2)(map[string]interface{}{}, Claims{}, now); err == nil {
		t.Fatal("missing version should fail")
	}
	if err := RequireMinimumTokenVersion(2)(map[string]interface{}{"token_version": float64(1)}, Claims{}, now); err == nil {
		t.Fatal("stale version should fail")
	}
	if err := RequireMinimumTokenVersion(2)(map[string]interface{}{"ver": json.Number("3")}, Claims{}, now); err != nil {
		t.Fatalf("valid version = %v", err)
	}

	if v, ok := int64Claim(map[string]interface{}{"a": nil, "b": json.Number("bad")}, "a", "b"); ok {
		t.Fatalf("unparseable claims = (%d, %v)", v, ok)
	}

	composed := ComposeTokenPolicies(nil, RequireMinimumTokenVersion(1))
	if err := composed(map[string]interface{}{"ver": float64(2)}, Claims{}, now); err != nil {
		t.Fatalf("composed policy = %v", err)
	}
}
