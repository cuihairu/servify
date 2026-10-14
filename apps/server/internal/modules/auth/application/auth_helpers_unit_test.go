package application

// 服务内部分支测试（GORM 写归属下沉 infra 后，不经仓储的纯内部逻辑留本包；
// db 型测试随 SQL 细节住在 infra 包，经端口桩覆盖的分支见 auth_stub_test.go）。

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
)

var errBoom = errors.New("boom")

func testAuthConfig() *config.Config {
	cfg := config.GetDefaultConfig()
	cfg.JWT.Secret = "test-secret"
	cfg.JWT.ExpiresIn = time.Hour
	cfg.JWT.RefreshExpiresIn = 24 * time.Hour
	return cfg
}

func twoFactorTestConfig(enabled bool) *config.Config {
	cfg := testAuthConfig()
	cfg.Security.TwoFactor.Enabled = enabled
	return cfg
}

func oidcTestConfig(mutate func(*config.OIDCConfig)) *config.Config {
	cfg := testAuthConfig()
	cfg.OIDC.Enabled = true
	cfg.OIDC.AutoProvision = false
	cfg.OIDC.DefaultRole = "agent"
	cfg.OIDC.RoleMapping = map[string]string{"admin": "idp-admins"}
	if mutate != nil {
		mutate(&cfg.OIDC)
	}
	return cfg
}

// setSeam 替换一个包级变量并在测试结束后还原。
func setSeam[T any](t *testing.T, slot *T, value T) {
	t.Helper()
	old := *slot
	*slot = value
	t.Cleanup(func() { *slot = old })
}

// failingRand 让 crypto/rand 注入点按 failPlan 依次失败/成功。
func failingRand(t *testing.T, failPlan []bool, calls *int) {
	t.Helper()
	setSeam(t, &HookRandRead, func(b []byte) (int, error) {
		shouldFail := failPlan[min(*calls, len(failPlan)-1)]
		*calls++
		if shouldFail {
			return 0, errors.New("boom: rand read")
		}
		for i := range b {
			b[i] = byte(*calls)
		}
		return len(b), nil
	})
}

func TestAuthService_BuildersAndHelpers(t *testing.T) {
	svc := &AuthService{}
	if _, err := svc.buildAuthResult(context.Background(), nil, nil); err == nil {
		t.Fatal("expected nil user error")
	}
	if _, err := svc.buildAuthResult(context.Background(), &models.User{}, nil); err == nil {
		t.Fatal("expected nil session error")
	}
	if svc.refreshExpiresIn() != 7*24*time.Hour {
		t.Fatal("expected default refresh expiry")
	}

	// json.Number claim parsing
	num := json.Number("42")
	if v, ok := authNumericClaim(map[string]interface{}{"n": num}, "n"); !ok || v != 42 {
		t.Fatalf("json.Number claim: %v %v", v, ok)
	}
	badNum := json.Number("oops")
	if _, ok := authNumericClaim(map[string]interface{}{"n": badNum}, "n"); ok {
		t.Fatal("expected parse failure to continue search")
	}
	if v, ok := authNumericClaim(map[string]interface{}{"f": 1.5}, "f"); !ok || v != 1 {
		t.Fatalf("float claim: %v %v", v, ok)
	}
	if _, ok := authNumericClaim(map[string]interface{}{"x": "str"}, "x"); ok {
		t.Fatal("string claim should not parse")
	}
	if _, ok := authNumericClaim(map[string]interface{}{"x": nil}, "x"); ok {
		t.Fatal("nil claim should not parse")
	}
	if v, ok := authNumericClaim(map[string]interface{}{"a": 1.0, "b": 2.0}, "a", "b"); !ok || v != 1 {
		t.Fatalf("first key wins: %v %v", v, ok)
	}
	if v, ok := authNumericClaim(map[string]interface{}{"b": 2.0}, "a", "b"); !ok || v != 2 {
		t.Fatalf("fallback key: %v %v", v, ok)
	}
	if _, ok := authNumericClaim(nil, "x"); ok {
		t.Fatal("missing key should not parse")
	}

	if v, ok := authStringClaim(map[string]interface{}{"s": " val "}, "s"); !ok || v != "val" {
		t.Fatalf("string claim: %q %v", v, ok)
	}
	if _, ok := authStringClaim(map[string]interface{}{"s": "  "}, "s"); ok {
		t.Fatal("blank claim should not parse")
	}
	if _, ok := authStringClaim(map[string]interface{}{"s": 42}, "s"); ok {
		t.Fatal("non-string claim should not parse")
	}
	if _, ok := authStringClaim(map[string]interface{}{"s": nil}, "s"); ok {
		t.Fatal("nil claim should not parse")
	}
	if v, ok := authStringClaim(map[string]interface{}{"b": "two"}, "a", "b"); !ok || v != "two" {
		t.Fatalf("fallback key: %q %v", v, ok)
	}

	if got := normalizeAuthSessionField("  padded  ", 6); got != "padded" {
		t.Fatalf("normalize: %q", got)
	}
	if got := normalizeAuthSessionField("longvalue", 4); got != "long" {
		t.Fatalf("truncate: %q", got)
	}
	if got := normalizeAuthSessionField("  x  ", 0); got != "x" {
		t.Fatalf("no-limit: %q", got)
	}

	if id := newAuthSessionID(); !strings.HasPrefix(id, "auth_") {
		t.Fatalf("session id: %q", id)
	}
	if id := newAuthTokenID(); !strings.HasPrefix(id, "jti_") {
		t.Fatalf("token id: %q", id)
	}

	token, err := createHS256JWT(map[string]interface{}{"k": "v"}, "secret")
	if err != nil || strings.Count(token, ".") != 2 {
		t.Fatalf("createHS256JWT: %v %q", err, token)
	}
}

func TestAuthSeamsRandomHexRandError(t *testing.T) {
	calls := 0
	failingRand(t, []bool{true}, &calls)
	if _, err := randomHex(8); err == nil || !strings.Contains(err.Error(), "generate random") {
		t.Fatalf("expected wrapped rand error, got %v", err)
	}
}

func TestRandomHexOk(t *testing.T) {
	hex, err := randomHex(8)
	if err != nil || len(hex) != 16 {
		t.Fatalf("randomHex: %q %v", hex, err)
	}
}

func TestAuthSeamsAuthSessionAndTokenIDFallback(t *testing.T) {
	calls := 0
	failingRand(t, []bool{true}, &calls)
	numericTimestamp := func(id, prefix string) bool {
		ts, ok := strings.CutPrefix(id, prefix)
		if !ok {
			return false
		}
		_, err := strconv.ParseInt(ts, 10, 64)
		return err == nil
	}
	if id := newAuthSessionID(); !numericTimestamp(id, "auth_") {
		t.Fatalf("expected timestamp fallback session id, got %q", id)
	}
	if id := newAuthTokenID(); !numericTimestamp(id, "jti_") {
		t.Fatalf("expected timestamp fallback token id, got %q", id)
	}
}

func TestAuthSeamsOIDCUsernameSuffixRandError(t *testing.T) {
	calls := 0
	failingRand(t, []bool{true}, &calls)
	// suffix 生成失败时回退为纯 local part（不会 panic，也不会带随机后缀）。
	if name := oidcUsername(OIDCIdentity{Email: "Ada.Lovelace@example.com"}); name != "ada.lovelace" {
		t.Fatalf("expected bare local part fallback, got %q", name)
	}
}

func TestOIDCUsernameShapes(t *testing.T) {
	if name := oidcUsername(OIDCIdentity{Email: "Ada.Lovelace@example.com"}); !strings.HasPrefix(name, "ada.lovelace_") {
		t.Fatalf("expected local part with random suffix, got %q", name)
	}
	if name := oidcUsername(OIDCIdentity{Email: "no-at-sign"}); !strings.HasPrefix(name, "sso_") {
		t.Fatalf("expected sso prefix for missing @, got %q", name)
	}
	if name := oidcUsername(OIDCIdentity{Email: "@example.com"}); !strings.HasPrefix(name, "sso_") {
		t.Fatalf("expected sso prefix for empty local part, got %q", name)
	}
}

func TestAuthSeamsProvisionOIDCUserRandError(t *testing.T) {
	// randomHex(32) 失败必须中断本地账号创建（不触达仓储）。
	calls := 0
	failingRand(t, []bool{true, true}, &calls)
	svc := NewService(nil, testAuthConfig())
	if _, err := svc.provisionOIDCUser(context.Background(), OIDCIdentity{
		Subject: "s1", Email: "s1@example.com", EmailVerified: true, Name: "S One",
	}, "agent"); err == nil || !strings.Contains(err.Error(), "generate random") {
		t.Fatalf("expected rand error to abort provisioning, got %v", err)
	}
}

func TestOIDCHelpersFallbacks(t *testing.T) {
	// 非 admin/agent 的默认角色一律归一化为 agent
	if got := mapOIDCRole(nil, nil, "owner"); got != "agent" {
		t.Fatalf("mapOIDCRole(default=owner) = %q, want agent", got)
	}
	// 域白名单开启时，无 @ 的邮箱直接拒绝
	if oidcEmailAllowed([]string{"example.com"}, "no-at-sign") {
		t.Fatal("email without @ must be rejected under domain allowlist")
	}
	// 空白名单放行所有邮箱（对照）
	if !oidcEmailAllowed(nil, "any@x.com") {
		t.Fatal("empty allowlist must allow any email")
	}
}

func TestMapOIDCRole(t *testing.T) {
	mapping := map[string]string{"admin": "idp-admins", "agent": "idp-agents", "customer": "idp-customers"}
	if got := mapOIDCRole([]string{"idp-admins"}, mapping, "agent"); got != "admin" {
		t.Fatalf("mapping hit admin: %q", got)
	}
	if got := mapOIDCRole([]string{" idp-agents "}, mapping, "agent"); got != "agent" {
		t.Fatalf("mapping hit agent: %q", got)
	}
	if got := mapOIDCRole([]string{"IDP-ADMINS"}, mapping, "agent"); got != "admin" {
		t.Fatalf("case-insensitive hit: %q", got)
	}
	if got := mapOIDCRole([]string{"idp-customers"}, mapping, "agent"); got != "agent" {
		t.Fatalf("non-local role mapping ignored: %q", got)
	}
	if got := mapOIDCRole([]string{"unknown-role"}, mapping, "agent"); got != "agent" {
		t.Fatalf("miss falls to default: %q", got)
	}
	if got := mapOIDCRole([]string{" ", ""}, mapping, "admin"); got != "admin" {
		t.Fatalf("blank raw values skipped: %q", got)
	}
}

func TestOIDCEmailAllowed(t *testing.T) {
	if !oidcEmailAllowed([]string{" Example.COM "}, "user@example.com") {
		t.Fatal("case/whitespace-insensitive domain match")
	}
	if oidcEmailAllowed([]string{"example.com"}, "user@other.com") {
		t.Fatal("unlisted domain must be rejected")
	}
}

func TestValidateChallengeTokenZeroUser(t *testing.T) {
	svc := NewService(nil, twoFactorTestConfig(true))

	token, err := createHS256JWT(map[string]interface{}{
		"exp":       time.Now().Add(time.Minute).Unix(),
		"token_use": challengeTokenUse,
		"user_id":   0,
	}, testAuthConfig().JWT.Secret)
	if err != nil {
		t.Fatalf("craft challenge token: %v", err)
	}
	if _, err := svc.validateChallengeToken(token, AuthSessionMetadata{}); !errors.Is(err, ErrAuthInvalid2FAChallenge) {
		t.Fatalf("zero-user challenge err = %v, want %v", err, ErrAuthInvalid2FAChallenge)
	}
}

func TestTwoFactorNilConfigFallbacks(t *testing.T) {
	svc := &AuthService{}
	if got := svc.twoFactorIssuer(); got != "Servify" {
		t.Fatalf("twoFactorIssuer() = %q, want Servify", got)
	}
	if got := svc.challengeTTL(); got != 5*time.Minute {
		t.Fatalf("challengeTTL() = %v, want 5m", got)
	}
	if err := svc.requireTwoFactorEnabled(); !errors.Is(err, ErrTwoFactorDisabled) {
		t.Fatalf("nil config requireTwoFactorEnabled = %v", err)
	}

	cfg := twoFactorTestConfig(true)
	cfg.Security.TwoFactor.Issuer = " Custom "
	cfg.Security.TwoFactor.ChallengeTTL = time.Minute
	svc2 := NewService(nil, cfg)
	if got := svc2.twoFactorIssuer(); got != "Custom" {
		t.Fatalf("custom issuer = %q", got)
	}
	if got := svc2.challengeTTL(); got != time.Minute {
		t.Fatalf("custom ttl = %v", got)
	}
}

func TestTwoFactorNormalizeRecoveryCode(t *testing.T) {
	cases := map[string]string{
		"ab12-cd34": "ab12cd34",
		"AB12CD34":  "ab12cd34",
		" ab12cd34": "ab12cd34",
		"ab12cd3":   "",
		"zz12cd34":  "",
		"":          "",
	}
	for input, want := range cases {
		if got := normalizeRecoveryCode(input); got != want {
			t.Fatalf("normalizeRecoveryCode(%q) = %q want %q", input, got, want)
		}
	}
}

func TestNormalizeTotpSecret(t *testing.T) {
	if got := normalizeTotpSecret(" jbsw-y3dp "); got != "JBSWY3DP" {
		t.Fatalf("normalizeTotpSecret = %q", got)
	}
}

func TestChallengeFingerprint(t *testing.T) {
	a := challengeFingerprint("ip", "ua")
	if a == "" || len(a) != 16 {
		t.Fatalf("fingerprint: %q", a)
	}
	if a != challengeFingerprint("ip", "ua") || a == challengeFingerprint("ip2", "ua") {
		t.Fatal("fingerprint must be deterministic and input-sensitive")
	}
}

func TestGenerateRecoveryCodesOk(t *testing.T) {
	codes, hashes, err := GenerateRecoveryCodes(3)
	if err != nil || len(codes) != 3 || len(hashes) != 3 {
		t.Fatalf("GenerateRecoveryCodes: %v %v %v", codes, hashes, err)
	}
	for i, code := range codes {
		if len(code) != 9 || code[4] != '-' {
			t.Fatalf("code format: %q", code)
		}
		if hashes[i] == "" {
			t.Fatalf("missing hash for %q", code)
		}
	}
}

// --- 登录风险执行：档位归一化与风险判定（不经仓储） ---

type stubRiskIntel struct {
	label string
}

func (s stubRiskIntel) LoginNetworkLabel(_ context.Context, _ string) string {
	return s.label
}

func TestLoginRiskEnforcementNormalization(t *testing.T) {
	cases := map[string]string{
		"STEP_UP ":  "step_up",
		" block":    "block",
		"":          "",
		"off":       "",
		"bogus":     "",
		"  BLOCK  ": "block",
	}
	for raw, want := range cases {
		svc := NewService(nil, testAuthConfig())
		svc.WithLoginRiskEnforcement(stubRiskIntel{label: "public"}, raw)
		if svc.loginEnforcement != want {
			t.Fatalf("enforcement %q normalized to %q, want %q", raw, svc.loginEnforcement, want)
		}
	}

	var nilSvc *AuthService
	if got := nilSvc.WithLoginRiskEnforcement(stubRiskIntel{label: "hosting"}, "block"); got != nil {
		t.Fatal("nil receiver must return nil")
	}
}

func TestLoginRiskHighRiskClassification(t *testing.T) {
	svc := NewService(nil, testAuthConfig())

	if svc.loginRiskHighRisk(context.Background(), "203.0.113.50") {
		t.Fatal("nil intel must not be high risk")
	}

	svc.WithLoginRiskEnforcement(stubRiskIntel{label: "public"}, "block")
	if svc.loginRiskHighRisk(context.Background(), "  ") {
		t.Fatal("blank ip must not be high risk")
	}
	for _, safe := range []string{"public", "private", "loopback", "unknown", "  HOSTING  "} {
		label := strings.ToLower(strings.TrimSpace(safe))
		svc.WithLoginRiskEnforcement(stubRiskIntel{label: safe}, "block")
		got := svc.loginRiskHighRisk(context.Background(), "203.0.113.50")
		want := label == "hosting"
		if got != want {
			t.Fatalf("label %q high risk = %v, want %v", safe, got, want)
		}
	}

	var nilSvc *AuthService
	if nilSvc.loginRiskHighRisk(context.Background(), "203.0.113.50") {
		t.Fatal("nil receiver must not be high risk")
	}
}

func TestWithRefreshReusePolicyNormalization(t *testing.T) {
	for raw, want := range map[string]bool{
		"revoke_family":   true,
		" REVOKE_FAMILY ": true,
		"":                false,
		"off":             false,
		"block":           false,
		"bogus":           false,
	} {
		svc := NewService(nil, testAuthConfig())
		svc.WithRefreshReusePolicy(raw)
		if svc.refreshReuseRevocation != want {
			t.Fatalf("policy %q revocation = %v, want %v", raw, svc.refreshReuseRevocation, want)
		}
	}

	var nilSvc *AuthService
	if got := nilSvc.WithRefreshReusePolicy("revoke_family"); got != nil {
		t.Fatal("nil receiver must return nil")
	}
}

// TestAuthServiceCreateAuthSessionNilRepo 覆盖会话创建入口的 nil 仓储守卫。
func TestAuthServiceCreateAuthSessionNilRepo(t *testing.T) {
	nilSvc := &AuthService{}
	if _, err := nilSvc.createAuthSession(context.Background(), 1, AuthSessionMetadata{}); err != ErrAuthInvalidDB {
		t.Fatalf("expected ErrAuthInvalidDB, got %v", err)
	}
}
