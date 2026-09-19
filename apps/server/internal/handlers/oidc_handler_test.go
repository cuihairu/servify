package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	authapp "servify/apps/server/internal/modules/auth/application"
	oidcplatform "servify/apps/server/internal/platform/auth/oidc"
)

// stubOIDCVerifier replays fixed claims so handler tests need no signing chain.
type stubOIDCVerifier struct {
	claims *oidcplatform.Claims
	err    error
}

func (s *stubOIDCVerifier) Verify(context.Context, string, string) (*oidcplatform.Claims, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.claims, nil
}

func newOIDCHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:oidc_handler")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.UserAuthSession{}); err != nil {
		t.Fatalf("migrate models: %v", err)
	}
	return db
}

// newOIDCHandler builds a handler around a stub verifier and a fake token endpoint.
func newOIDCHandler(t *testing.T, mutate func(*config.OIDCConfig, *stubOIDCVerifier)) (*OIDCHandler, *config.Config) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "at",
			"token_type":   "bearer",
			"id_token":     "raw-id-token",
		})
	}))
	t.Cleanup(tokenSrv.Close)

	cfg := config.GetDefaultConfig()
	cfg.JWT.Secret = "test-secret"
	cfg.JWT.ExpiresIn = time.Hour
	cfg.JWT.RefreshExpiresIn = 24 * time.Hour
	cfg.OIDC = config.OIDCConfig{
		Enabled:         true,
		Issuer:          "https://idp.example.com",
		ClientID:        "servify-admin",
		ClientSecret:    "secret",
		RedirectURL:     "http://127.0.0.1:8080/api/v1/auth/oidc/callback",
		FrontendBaseURL: "http://127.0.0.1:3000",
		DefaultRole:     "agent",
	}
	verifier := &stubOIDCVerifier{claims: &oidcplatform.Claims{
		Subject:       "sub-1",
		Email:         "sso@example.com",
		EmailVerified: true,
		Name:          "SSO User",
	}}
	if mutate != nil {
		mutate(&cfg.OIDC, verifier)
	}
	provider := oidcplatform.New(verifier, &oauth2.Config{
		ClientID:     cfg.OIDC.ClientID,
		ClientSecret: cfg.OIDC.ClientSecret,
		RedirectURL:  cfg.OIDC.RedirectURL,
		Endpoint: oauth2.Endpoint{
			AuthURL:  cfg.OIDC.Issuer + "/authorize",
			TokenURL: tokenSrv.URL + "/token",
		},
	}, nil)
	authSvc := authapp.NewService(newOIDCHandlerTestDB(t), cfg)
	return NewOIDCHandler(provider, cfg.OIDC, authSvc, cfg.Server.Environment), cfg
}

func performOIDC(t *testing.T, h *OIDCHandler, target string, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if headers != nil {
		req.Header = headers
	}
	c.Request = req
	switch {
	case strings.HasPrefix(target, "/status"):
		h.Status(c)
	case strings.HasPrefix(target, "/start"):
		h.Start(c)
	case strings.HasPrefix(target, "/callback"):
		h.Callback(c)
	default:
		t.Fatalf("unknown route %q", target)
	}
	return w
}

// oidcFlowCookieFor builds the flow cookie the Start endpoint would set.
func oidcFlowCookieFor(t *testing.T, state, nonce, verifier string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"state": state, "nonce": nonce, "verifier": verifier})
	if err != nil {
		t.Fatalf("marshal flow: %v", err)
	}
	return oidcFlowCookie + "=" + base64.RawURLEncoding.EncodeToString(raw)
}

func TestOIDCStatus(t *testing.T) {
	h, _ := newOIDCHandler(t, nil)
	w := performOIDC(t, h, "/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp struct {
		Enabled    bool   `json:"enabled"`
		IssuerHost string `json:"issuer_host"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Enabled || resp.IssuerHost != "idp.example.com" {
		t.Fatalf("resp = %+v", resp)
	}

	// provider 为 nil（SSO 关闭）时 status 仍必须应答，前端据此隐藏按钮
	disabled := NewOIDCHandler(nil, config.OIDCConfig{}, nil, "development")
	w = performOIDC(t, disabled, "/status", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("disabled status: code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestOIDCStartSetsCookieAndRedirects(t *testing.T) {
	h, cfg := newOIDCHandler(t, nil)
	w := performOIDC(t, h, "/start", nil)
	if w.Code != http.StatusFound {
		t.Fatalf("code = %d", w.Code)
	}
	location := w.Header().Get("Location")
	for _, part := range []string{cfg.OIDC.Issuer + "/authorize", "state=", "nonce=", "code_challenge="} {
		if !strings.Contains(location, part) {
			t.Fatalf("location missing %q: %s", part, location)
		}
	}
	var flow *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == oidcFlowCookie {
			flow = c
		}
	}
	if flow == nil {
		t.Fatal("flow cookie not set")
	}
	if !flow.HttpOnly || flow.MaxAge != oidcFlowCookieMaxAge || flow.Path != "/api/v1/auth/oidc" || flow.SameSite != http.SameSiteLaxMode {
		t.Fatalf("flow cookie attributes: %+v", flow)
	}
	if flow.Secure {
		t.Fatal("Secure must be off outside production")
	}
}

func TestOIDCStartWhenDisabled(t *testing.T) {
	disabled := NewOIDCHandler(nil, config.OIDCConfig{FrontendBaseURL: "http://127.0.0.1:3000"}, nil, "development")
	w := performOIDC(t, disabled, "/start", nil)
	if w.Code != http.StatusFound {
		t.Fatalf("code = %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasSuffix(loc, "/login?error=oidc_disabled") {
		t.Fatalf("location = %q", loc)
	}
}

func TestOIDCCallbackWithoutCookie(t *testing.T) {
	h, _ := newOIDCHandler(t, nil)
	w := performOIDC(t, h, "/callback?state=x&code=y", nil)
	if w.Code != http.StatusFound {
		t.Fatalf("code = %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasSuffix(loc, "http://127.0.0.1:3000/login?error=oidc_state") {
		t.Fatalf("location = %q", loc)
	}
}

func TestOIDCCallbackRejectsStateMismatch(t *testing.T) {
	h, _ := newOIDCHandler(t, nil)
	cookie := oidcFlowCookieFor(t, "real-state", "nonce", "verifier")
	w := performOIDC(t, h, "/callback?state=forged-state&code=code", http.Header{"Cookie": []string{cookie + "; other=1"}})
	if w.Code != http.StatusFound {
		t.Fatalf("code = %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasSuffix(loc, "/login?error=oidc_state") {
		t.Fatalf("location = %q", loc)
	}
	// 失败路径同样要清掉 flow cookie
	cleared := false
	for _, c := range w.Result().Cookies() {
		if c.Name == oidcFlowCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("flow cookie must be cleared on failure")
	}
}

func TestOIDCCallbackHappyPath(t *testing.T) {
	// auto_provision 开启：IdP 首次登录即建账号
	h, _ := newOIDCHandler(t, func(c *config.OIDCConfig, _ *stubOIDCVerifier) {
		c.AutoProvision = true
	})
	cookie := oidcFlowCookieFor(t, "real-state", "nonce", "verifier")
	w := performOIDC(t, h, "/callback?state=real-state&code=auth-code", http.Header{"Cookie": []string{cookie}})
	if w.Code != http.StatusFound {
		t.Fatalf("code = %d body = %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	const frontendPrefix = "http://127.0.0.1:3000/login/oidc/callback#"
	if !strings.HasPrefix(loc, frontendPrefix) {
		t.Fatalf("location = %q", loc)
	}
	// token 只能出现在 fragment 里（# 之后）
	values, err := url.ParseQuery(strings.TrimPrefix(loc, frontendPrefix))
	if err != nil {
		t.Fatalf("fragment: %v", err)
	}
	if values.Get("token") == "" || values.Get("refresh_token") == "" {
		t.Fatalf("fragment missing tokens: %q", loc)
	}
}

func TestOIDCCallbackLoginDenied(t *testing.T) {
	// 未验证邮箱：LoginWithOIDC 拒绝 → 统一 oidc_login_denied，不泄露细节
	h, _ := newOIDCHandler(t, func(c *config.OIDCConfig, v *stubOIDCVerifier) {
		v.claims.EmailVerified = false
	})
	cookie := oidcFlowCookieFor(t, "real-state", "nonce", "verifier")
	w := performOIDC(t, h, "/callback?state=real-state&code=auth-code", http.Header{"Cookie": []string{cookie}})
	if w.Code != http.StatusFound {
		t.Fatalf("code = %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasSuffix(loc, "/login?error=oidc_login_denied") {
		t.Fatalf("location = %q", loc)
	}
}

func TestOIDCCallbackWithoutFrontendBaseURL(t *testing.T) {
	h, _ := newOIDCHandler(t, func(c *config.OIDCConfig, _ *stubOIDCVerifier) {
		c.FrontendBaseURL = ""
	})
	w := performOIDC(t, h, "/callback?state=x", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d", w.Code)
	}
}
