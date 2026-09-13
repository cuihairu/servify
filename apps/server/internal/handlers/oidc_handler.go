package handlers

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"servify/apps/server/internal/config"
	oidcplatform "servify/apps/server/internal/platform/auth/oidc"
	"servify/apps/server/internal/services"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

const (
	oidcFlowCookie = "oidc_flow"
	// flow 状态的有效期：覆盖一次授权码往返即可
	oidcFlowCookieMaxAge = 600
)

// OIDCHandler serves the admin SSO endpoints under /api/v1/auth/oidc/*.
type OIDCHandler struct {
	provider *oidcplatform.Provider // nil when SSO disabled
	cfg      config.OIDCConfig
	auth     *services.AuthService
	secure   bool // Secure cookie flag (production)
}

// NewOIDCHandler wires the handler. provider may be nil (SSO disabled) —
// Status keeps answering so the frontend button logic works.
func NewOIDCHandler(provider *oidcplatform.Provider, cfg config.OIDCConfig, authService *services.AuthService, environment string) *OIDCHandler {
	return &OIDCHandler{
		provider: provider,
		cfg:      cfg,
		auth:     authService,
		secure:   strings.EqualFold(strings.TrimSpace(environment), "production"),
	}
}

// Status GET /api/v1/auth/oidc/status → {"enabled":bool,"issuer_host":string}
func (h *OIDCHandler) Status(c *gin.Context) {
	resp := gin.H{"enabled": h.provider != nil}
	if h.provider != nil {
		resp["issuer_host"] = h.provider.IssuerHost()
	}
	c.JSON(http.StatusOK, resp)
}

// Start GET /api/v1/auth/oidc/start → 302 IdP authorization URL.
// state/nonce/PKCE-verifier travel in a short-lived HttpOnly cookie.
func (h *OIDCHandler) Start(c *gin.Context) {
	if h.provider == nil {
		redirectOIDCError(c, h.cfg.FrontendBaseURL, "oidc_disabled")
		return
	}
	state, err := randomToken(16)
	if err != nil {
		redirectOIDCError(c, h.cfg.FrontendBaseURL, "oidc_failed")
		return
	}
	nonce, err := randomToken(16)
	if err != nil {
		redirectOIDCError(c, h.cfg.FrontendBaseURL, "oidc_failed")
		return
	}
	verifier := oauth2.GenerateVerifier()

	// oidcFlow 仅含字符串字段，json.Marshal 不可能失败（无 chan/func/环引用），
	// 错误分支为不可达死代码。
	flow, _ := json.Marshal(oidcFlow{State: state, Nonce: nonce, Verifier: verifier})
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oidcFlowCookie,
		Value:    base64.RawURLEncoding.EncodeToString(flow),
		MaxAge:   oidcFlowCookieMaxAge,
		Path:     "/api/v1/auth/oidc",
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	})
	c.Redirect(http.StatusFound, h.provider.AuthCodeURL(state, nonce, verifier))
}

// Callback GET /api/v1/auth/oidc/callback.
// Verifies state, exchanges the code, validates the ID token, signs a local
// session and hands the tokens to the frontend via URL fragment (fragments
// never reach server logs or Referer headers). The frontend clears the hash
// immediately after reading.
func (h *OIDCHandler) Callback(c *gin.Context) {
	// 无论成败都清掉 flow cookie；必须在任何 WriteHeader 之前设置。
	http.SetCookie(c.Writer, h.expiredFlowCookie())

	if target, errCode := h.resolveCallback(c); errCode != "" {
		redirectOIDCError(c, h.cfg.FrontendBaseURL, errCode)
		return
	} else if target != "" {
		c.Redirect(http.StatusFound, target)
	}
}

// resolveCallback runs the protocol steps and returns either the frontend
// success URL or a failure code for redirectOIDCError.
func (h *OIDCHandler) resolveCallback(c *gin.Context) (target, errCode string) {
	frontend := h.cfg.FrontendBaseURL
	if h.provider == nil {
		return "", "oidc_disabled"
	}
	var flow oidcFlow
	if cookie, err := c.Cookie(oidcFlowCookie); err != nil {
		return "", "oidc_state"
	} else if raw, err := base64.RawURLEncoding.DecodeString(cookie); err != nil {
		return "", "oidc_state"
	} else if err := json.Unmarshal(raw, &flow); err != nil {
		return "", "oidc_state"
	}

	if c.Query("state") == "" || c.Query("state") != flow.State {
		return "", "oidc_state"
	}
	if code := c.Query("code"); code == "" {
		return "", "oidc_failed"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	rawIDToken, err := h.provider.Exchange(ctx, c.Query("code"), flow.Verifier)
	if err != nil {
		return "", "oidc_failed"
	}
	claims, err := h.provider.Verify(ctx, rawIDToken, flow.Nonce)
	if err != nil {
		return "", "oidc_failed"
	}

	result, err := h.auth.LoginWithOIDC(ctx, services.OIDCIdentity{
		Subject:       claims.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		Name:          claims.Name,
		Roles:         claims.Roles,
	}, services.AuthSessionMetadata{
		UserAgent: c.Request.UserAgent(),
		ClientIP:  c.ClientIP(),
	})
	if err != nil {
		// 不区分内部失败与拒绝原因，避免向 URL 泄露账号状态细节。
		return "", "oidc_login_denied"
	}

	fragment := url.Values{
		"token":         []string{result.Token},
		"refresh_token": []string{result.RefreshToken},
	}.Encode()
	return strings.TrimRight(frontend, "/") + "/login/oidc/callback#" + fragment, ""
}

func (h *OIDCHandler) expiredFlowCookie() *http.Cookie {
	return &http.Cookie{
		Name:     oidcFlowCookie,
		Value:    "expired",
		MaxAge:   -1,
		Path:     "/api/v1/auth/oidc",
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

type oidcFlow struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
}

func redirectOIDCError(c *gin.Context, frontend, code string) {
	base := strings.TrimRight(strings.TrimSpace(frontend), "/")
	if base == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": code})
		return
	}
	c.Redirect(http.StatusFound, base+"/login?error="+code)
}

func randomToken(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := oidcRandRead(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
