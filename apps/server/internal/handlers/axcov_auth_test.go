package handlers

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	authdelivery "servify/apps/server/internal/modules/auth/delivery"
)

type axcAuthService struct {
	registerResp *authdelivery.AuthResult
	registerErr  error
	loginResp    *authdelivery.AuthResult
	loginErr     error
	currentUser  *models.User
	currentErr   error
	sessions     []models.UserAuthSession
	listErr      error
	revokeCur    *models.UserAuthSession
	revokeCurErr error
	revokeOther  int
	revokeOthErr error
	refreshResp  *authdelivery.AuthResult
	refreshErr   error
	refreshTok   string
	loginInput   authdelivery.LoginInput
	regInput     authdelivery.RegisterInput
}

func (s *axcAuthService) Register(ctx context.Context, req authdelivery.RegisterInput, meta authdelivery.AuthSessionMetadata) (*authdelivery.AuthResult, error) {
	s.regInput = req
	return s.registerResp, s.registerErr
}

func (s *axcAuthService) Login(ctx context.Context, req authdelivery.LoginInput, meta authdelivery.AuthSessionMetadata) (*authdelivery.LoginOutcome, error) {
	s.loginInput = req
	if s.loginResp == nil {
		return nil, s.loginErr
	}
	return &authdelivery.LoginOutcome{Result: s.loginResp}, s.loginErr
}

func (s *axcAuthService) GetCurrentUser(ctx context.Context, userID uint) (*models.User, error) {
	return s.currentUser, s.currentErr
}

func (s *axcAuthService) ListAuthSessions(ctx context.Context, userID uint) ([]models.UserAuthSession, error) {
	return s.sessions, s.listErr
}

func (s *axcAuthService) RevokeCurrentSession(ctx context.Context, userID uint, sessionID string) (*models.UserAuthSession, error) {
	if s.revokeCurErr != nil {
		return nil, s.revokeCurErr
	}
	if s.revokeCur != nil {
		return s.revokeCur, nil
	}
	return &models.UserAuthSession{ID: sessionID, Status: "revoked", TokenVersion: 3}, nil
}

func (s *axcAuthService) RevokeOtherSessions(ctx context.Context, userID uint, currentSessionID string) (int, error) {
	return s.revokeOther, s.revokeOthErr
}

func (s *axcAuthService) RefreshToken(ctx context.Context, refreshToken string, meta authdelivery.AuthSessionMetadata) (*authdelivery.AuthResult, error) {
	s.refreshTok = refreshToken
	return s.refreshResp, s.refreshErr
}

// LoginWithOIDC 补齐 delivery.HandlerService 契约（复用 register 字段）。
func (s *axcAuthService) LoginWithOIDC(_ context.Context, _ authdelivery.OIDCIdentity, _ authdelivery.AuthSessionMetadata) (*authdelivery.AuthResult, error) {
	return s.registerResp, s.registerErr
}

type axcFixedIPIntel struct {
	desc sessionIPDescription
}

func (p axcFixedIPIntel) DescribeIP(ip string) sessionIPDescription {
	return p.desc
}

func axcAuthResult() *authdelivery.AuthResult {
	return &authdelivery.AuthResult{
		Token:            "tok-1",
		ExpiresIn:        3600,
		RefreshToken:     "rt-1",
		RefreshExpiresIn: 86400,
		User: &models.User{
			ID:       7,
			Username: "axc-user",
			Email:    "axc@example.com",
			Name:     "Axc User",
			Phone:    "123",
			Avatar:   "a.png",
			Role:     "admin",
			Status:   "active",
		},
	}
}

func axcAuthRouter(h *AuthHandler) *gin.Engine {
	r := gin.New()
	r.POST("/auth/register", h.Register)
	r.POST("/auth/login", h.Login)
	r.GET("/auth/me", h.GetCurrentUser)
	r.POST("/auth/refresh", h.RefreshToken)
	r.GET("/auth/sessions", h.ListSessions)
	r.POST("/auth/logout-current", h.LogoutCurrentSession)
	r.POST("/auth/logout-others", h.LogoutOtherSessions)
	return r
}

func axcDo(r *gin.Engine, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAxcAuthRegister(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name   string
		svc    *axcAuthService
		body   string
		want   int
		wantIn string
	}{
		{name: "invalid json", svc: &axcAuthService{}, body: "not-json", want: http.StatusBadRequest, wantIn: "请求参数无效"},
		{name: "blank username", svc: &axcAuthService{}, body: `{"username":"  ","email":"a@b.c","password":"p"}`, want: http.StatusBadRequest, wantIn: "不能为空"},
		{name: "blank email", svc: &axcAuthService{}, body: `{"username":"u","email":"","password":"p"}`, want: http.StatusBadRequest, wantIn: "不能为空"},
		{name: "empty password", svc: &axcAuthService{}, body: `{"username":"u","email":"a@b.c","password":""}`, want: http.StatusBadRequest, wantIn: "不能为空"},
		{
			name: "success",
			svc:  &axcAuthService{registerResp: axcAuthResult()},
			body: `{"username":"  axc-user  ","email":" axc@example.com ","password":"p","name":"n","phone":"1","role":"admin"}`,
			want: http.StatusCreated, wantIn: `"token":"tok-1"`,
		},
		{name: "invalid input err", svc: &axcAuthService{registerErr: authdelivery.ErrInvalidAuthInput}, body: `{"username":"u","email":"a@b.c","password":"p"}`, want: http.StatusBadRequest},
		{name: "already exists", svc: &axcAuthService{registerErr: authdelivery.ErrAuthUserAlreadyExists}, body: `{"username":"u","email":"a@b.c","password":"p"}`, want: http.StatusConflict},
		{name: "internal err", svc: &axcAuthService{registerErr: context.DeadlineExceeded}, body: `{"username":"u","email":"a@b.c","password":"p"}`, want: http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := axcDo(axcAuthRouter(NewAuthHandler(tc.svc)), http.MethodPost, "/auth/register", tc.body, nil)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
			if tc.wantIn != "" && !strings.Contains(w.Body.String(), tc.wantIn) {
				t.Fatalf("body missing %q: %s", tc.wantIn, w.Body.String())
			}
		})
	}
}

func TestAxcAuthRegisterTrimsInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &axcAuthService{registerResp: axcAuthResult()}
	w := axcDo(axcAuthRouter(NewAuthHandler(svc)), http.MethodPost, "/auth/register", `{"username":"  u  ","email":"  a@b.c  ","password":"p"}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if svc.regInput.Username != "u" || svc.regInput.Email != "a@b.c" {
		t.Fatalf("input not trimmed: %+v", svc.regInput)
	}
}

func TestAxcAuthLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name string
		svc  *axcAuthService
		body string
		want int
	}{
		{name: "invalid json", svc: &axcAuthService{}, body: "{", want: http.StatusBadRequest},
		{name: "success", svc: &axcAuthService{loginResp: axcAuthResult()}, body: `{"username":"u","password":"p"}`, want: http.StatusOK},
		{name: "bad credentials", svc: &axcAuthService{loginErr: authdelivery.ErrAuthInvalidCredentials}, body: `{"username":"u","password":"p"}`, want: http.StatusUnauthorized},
		{name: "disabled", svc: &axcAuthService{loginErr: authdelivery.ErrAuthUserDisabled}, body: `{"username":"u","password":"p"}`, want: http.StatusForbidden},
		{name: "internal err", svc: &axcAuthService{loginErr: context.Canceled}, body: `{"username":"u","password":"p"}`, want: http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := axcDo(axcAuthRouter(NewAuthHandler(tc.svc)), http.MethodPost, "/auth/login", tc.body, nil)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestAxcAuthCurrentUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mk := func(userID any) *gin.Engine {
		r := gin.New()
		if userID != nil {
			r.Use(func(c *gin.Context) {
				c.Set("user_id", userID)
				c.Next()
			})
		}
		svc := &axcAuthService{currentUser: &models.User{ID: 7, Username: "u", Role: "admin", Status: "active"}}
		r.GET("/auth/me", NewAuthHandler(svc).GetCurrentUser)
		return r
	}

	t.Run("missing user id", func(t *testing.T) {
		if w := axcDo(mk(nil), http.MethodGet, "/auth/me", "", nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("string user id rejected", func(t *testing.T) {
		if w := axcDo(mk("7"), http.MethodGet, "/auth/me", "", nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("float user id", func(t *testing.T) {
		if w := axcDo(mk(float64(7)), http.MethodGet, "/auth/me", "", nil); w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("int user id", func(t *testing.T) {
		if w := axcDo(mk(7), http.MethodGet, "/auth/me", "", nil); w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("nil user maps empty response", func(t *testing.T) {
		svc := &axcAuthService{}
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set("user_id", uint(7)); c.Next() })
		r.GET("/auth/me", NewAuthHandler(svc).GetCurrentUser)
		w := axcDo(r, http.MethodGet, "/auth/me", "", nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"username":""`) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("service error", func(t *testing.T) {
		svc := &axcAuthService{currentErr: context.Canceled}
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set("user_id", uint(7)); c.Next() })
		r.GET("/auth/me", NewAuthHandler(svc).GetCurrentUser)
		if w := axcDo(r, http.MethodGet, "/auth/me", "", nil); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestAxcAuthRefreshTokenBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("disabled returns forbidden", func(t *testing.T) {
		svc := &axcAuthService{refreshErr: authdelivery.ErrAuthUserDisabled}
		w := axcDo(axcAuthRouter(NewAuthHandler(svc)), http.MethodPost, "/auth/refresh", `{"refresh_token":"r"}`, nil)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("generic error returns not found", func(t *testing.T) {
		svc := &axcAuthService{refreshErr: context.Canceled}
		w := axcDo(axcAuthRouter(NewAuthHandler(svc)), http.MethodPost, "/auth/refresh", `{"refresh_token":"r"}`, nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("no body no header yields empty token", func(t *testing.T) {
		svc := &axcAuthService{refreshErr: context.Canceled}
		w := axcDo(axcAuthRouter(NewAuthHandler(svc)), http.MethodPost, "/auth/refresh", "", nil)
		if w.Code != http.StatusNotFound || svc.refreshTok != "" {
			t.Fatalf("status=%d token=%q body=%s", w.Code, svc.refreshTok, w.Body.String())
		}
	})
	t.Run("non bearer header ignored", func(t *testing.T) {
		svc := &axcAuthService{refreshErr: context.Canceled}
		w := axcDo(axcAuthRouter(NewAuthHandler(svc)), http.MethodPost, "/auth/refresh", "", map[string]string{"Authorization": "Basic abc"})
		if w.Code != http.StatusNotFound || svc.refreshTok != "" {
			t.Fatalf("status=%d token=%q", w.Code, svc.refreshTok)
		}
	})
	t.Run("lowercase bearer header accepted", func(t *testing.T) {
		svc := &axcAuthService{refreshResp: axcAuthResult()}
		w := axcDo(axcAuthRouter(NewAuthHandler(svc)), http.MethodPost, "/auth/refresh", "", map[string]string{"Authorization": "bearer  rt-x  "})
		if w.Code != http.StatusOK || svc.refreshTok != "rt-x" {
			t.Fatalf("status=%d token=%q body=%s", w.Code, svc.refreshTok, w.Body.String())
		}
	})
	t.Run("whitespace body token falls back to header", func(t *testing.T) {
		svc := &axcAuthService{refreshResp: axcAuthResult()}
		w := axcDo(axcAuthRouter(NewAuthHandler(svc)), http.MethodPost, "/auth/refresh", `{"refresh_token":"   "}`, map[string]string{"Authorization": "Bearer hdr-1"})
		if w.Code != http.StatusOK || svc.refreshTok != "hdr-1" {
			t.Fatalf("status=%d token=%q", w.Code, svc.refreshTok)
		}
	})
}

func axcAuthSessionRouter(svc *axcAuthService, userID any, sessionID string) *gin.Engine {
	r := gin.New()
	if userID != nil || sessionID != "" {
		r.Use(func(c *gin.Context) {
			if userID != nil {
				c.Set("user_id", userID)
			}
			if sessionID != "" {
				c.Set("session_id", sessionID)
			}
			c.Next()
		})
	}
	h := NewAuthHandler(svc)
	r.GET("/auth/sessions", h.ListSessions)
	r.POST("/auth/logout-current", h.LogoutCurrentSession)
	r.POST("/auth/logout-others", h.LogoutOtherSessions)
	return r
}

func TestAxcAuthListSessionsBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("unauthorized without user id", func(t *testing.T) {
		w := axcDo(axcAuthSessionRouter(&axcAuthService{}, nil, ""), http.MethodGet, "/auth/sessions", "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("list error returns 500", func(t *testing.T) {
		svc := &axcAuthService{listErr: context.Canceled}
		w := axcDo(axcAuthSessionRouter(svc, uint(7), "s1"), http.MethodGet, "/auth/sessions", "", nil)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("empty sessions returns empty list", func(t *testing.T) {
		svc := &axcAuthService{}
		w := axcDo(axcAuthSessionRouter(svc, uint(7), "s1"), http.MethodGet, "/auth/sessions", "", nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"count":0`) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestAxcAuthLogoutBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("current unauthorized without user", func(t *testing.T) {
		w := axcDo(axcAuthSessionRouter(&axcAuthService{}, nil, ""), http.MethodPost, "/auth/logout-current", "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d", w.Code)
		}
	})
	t.Run("current unauthorized without session", func(t *testing.T) {
		w := axcDo(axcAuthSessionRouter(&axcAuthService{}, uint(7), ""), http.MethodPost, "/auth/logout-current", "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d", w.Code)
		}
	})
	t.Run("current revoke error", func(t *testing.T) {
		svc := &axcAuthService{revokeCurErr: context.Canceled}
		w := axcDo(axcAuthSessionRouter(svc, uint(7), "s1"), http.MethodPost, "/auth/logout-current", "", nil)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d", w.Code)
		}
	})
	t.Run("current success", func(t *testing.T) {
		svc := &axcAuthService{}
		w := axcDo(axcAuthSessionRouter(svc, uint(7), "s1"), http.MethodPost, "/auth/logout-current", "", nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"token_version":3`) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("others unauthorized without user", func(t *testing.T) {
		w := axcDo(axcAuthSessionRouter(&axcAuthService{}, nil, ""), http.MethodPost, "/auth/logout-others", "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d", w.Code)
		}
	})
	t.Run("others unauthorized without session", func(t *testing.T) {
		w := axcDo(axcAuthSessionRouter(&axcAuthService{}, uint(7), ""), http.MethodPost, "/auth/logout-others", "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d", w.Code)
		}
	})
	t.Run("others revoke error", func(t *testing.T) {
		svc := &axcAuthService{revokeOthErr: context.Canceled}
		w := axcDo(axcAuthSessionRouter(svc, uint(7), "s1"), http.MethodPost, "/auth/logout-others", "", nil)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d", w.Code)
		}
	})
	t.Run("others success", func(t *testing.T) {
		svc := &axcAuthService{revokeOther: 4}
		w := axcDo(axcAuthSessionRouter(svc, uint(7), "s1"), http.MethodPost, "/auth/logout-others", "", nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"count":4`) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestAxcAuthOptionBuilders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("nil auth handler ignores options", func(t *testing.T) {
		var h *AuthHandler
		if h.WithSessionRiskResolver(nil) != nil {
			t.Fatal("expected nil handler")
		}
		if h.WithSessionIPIntelligence(nil) != nil {
			t.Fatal("expected nil handler")
		}
		if got := h.sessionRiskPolicy(context.Background()); got != defaultSessionRiskPolicy() {
			t.Fatalf("expected default policy, got %+v", got)
		}
	})
	t.Run("nil ip intel provider ignored", func(t *testing.T) {
		h := NewAuthHandler(&axcAuthService{})
		h.WithSessionIPIntelligence(nil)
		if _, ok := h.ipIntel.(heuristicSessionIPIntelligence); !ok {
			t.Fatalf("expected heuristic provider, got %T", h.ipIntel)
		}
	})
	t.Run("empty config keeps defaults", func(t *testing.T) {
		got := sessionRiskPolicyFromConfig(config.SessionRiskPolicyConfig{})
		if got != defaultSessionRiskPolicy() {
			t.Fatalf("expected default policy, got %+v", got)
		}
	})
}

func TestAxcAuthHelperFunctions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("authUserID variants", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		if id, ok := authUserID(c); ok || id != 0 {
			t.Fatalf("missing value: id=%d ok=%v", id, ok)
		}
		c.Set("user_id", float64(12.7))
		if id, ok := authUserID(c); !ok || id != 12 {
			t.Fatalf("float64: id=%d ok=%v", id, ok)
		}
		c.Set("user_id", uint(9))
		if id, ok := authUserID(c); !ok || id != 9 {
			t.Fatalf("uint: id=%d ok=%v", id, ok)
		}
		c.Set("user_id", 11)
		if id, ok := authUserID(c); !ok || id != 11 {
			t.Fatalf("int: id=%d ok=%v", id, ok)
		}
		c.Set("user_id", "11")
		if _, ok := authUserID(c); ok {
			t.Fatal("string should not be accepted")
		}
	})

	t.Run("authSessionID variants", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		if got := authSessionID(c); got != "" {
			t.Fatalf("missing value: %q", got)
		}
		c.Set("session_id", 42)
		if got := authSessionID(c); got != "" {
			t.Fatalf("non-string value: %q", got)
		}
		c.Set("session_id", "  s-1  ")
		if got := authSessionID(c); got != "s-1" {
			t.Fatalf("string value: %q", got)
		}
	})

	t.Run("authSessionMetadataFromRequest nil context", func(t *testing.T) {
		if got := authSessionMetadataFromRequest(nil); got != (authdelivery.AuthSessionMetadata{}) {
			t.Fatalf("expected zero metadata, got %+v", got)
		}
	})

	t.Run("authSessionMetadataFromRequest captures headers", func(t *testing.T) {
		var captured authdelivery.AuthSessionMetadata
		r2 := gin.New()
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"u","password":"p"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "ua-1")
		req.Header.Set("X-Device-ID", "dev-1")
		req.RemoteAddr = "203.0.113.5:77"
		r2.POST("/login", func(c *gin.Context) {
			captured = authSessionMetadataFromRequest(c)
			c.Status(http.StatusOK)
		})
		w := httptest.NewRecorder()
		r2.ServeHTTP(w, req)
		if captured.UserAgent != "ua-1" || captured.ClientIP != "203.0.113.5" || captured.DeviceFingerprint != "dev-1" {
			t.Fatalf("unexpected metadata: %+v", captured)
		}
	})

	t.Run("authDeviceFingerprint nil context", func(t *testing.T) {
		if got := authDeviceFingerprint(nil); got != "" {
			t.Fatalf("expected empty fingerprint, got %q", got)
		}
	})

	t.Run("authDeviceFingerprint truncates long device id", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
		c.Request.Header.Set("X-Device-ID", strings.Repeat("d", 140))
		if got := authDeviceFingerprint(c); len(got) != 128 {
			t.Fatalf("expected 128 chars, got %d", len(got))
		}
	})

	t.Run("authDeviceFingerprint hashes user agent and ip", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
		c.Request.Header.Set("User-Agent", "ua-hash")
		c.Request.RemoteAddr = "198.51.100.3:9"
		got := authDeviceFingerprint(c)
		if len(got) != 32 {
			t.Fatalf("expected sha256 prefix hex of 32 chars, got %q", got)
		}
	})

	t.Run("authDeviceFingerprint empty when nothing available", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
		c.Request.RemoteAddr = ""
		if got := authDeviceFingerprint(c); got != "" {
			t.Fatalf("expected empty fingerprint, got %q", got)
		}
	})
}

func TestAxcSessionRecencyAndRefresh(t *testing.T) {
	now := time.Now().UTC()
	policy := defaultSessionRiskPolicy()

	t.Run("sessionRecencyTime fallback chain", func(t *testing.T) {
		lastSeen := now.Add(-time.Hour)
		lastRefresh := now.Add(-2 * time.Hour)
		updatedAt := now.Add(-3 * time.Hour)
		createdAt := now.Add(-4 * time.Hour)
		if got := sessionRecencyTime(models.UserAuthSession{LastSeenAt: &lastSeen, LastRefreshedAt: &lastRefresh, UpdatedAt: updatedAt, CreatedAt: createdAt}); !got.Equal(lastSeen) {
			t.Fatalf("expected last seen, got %v", got)
		}
		if got := sessionRecencyTime(models.UserAuthSession{LastRefreshedAt: &lastRefresh, UpdatedAt: updatedAt, CreatedAt: createdAt}); !got.Equal(lastRefresh) {
			t.Fatalf("expected last refreshed, got %v", got)
		}
		zero := time.Time{}
		if got := sessionRecencyTime(models.UserAuthSession{LastSeenAt: &zero, UpdatedAt: updatedAt, CreatedAt: createdAt}); !got.Equal(updatedAt) {
			t.Fatalf("expected updated at, got %v", got)
		}
		if got := sessionRecencyTime(models.UserAuthSession{CreatedAt: createdAt}); !got.Equal(createdAt) {
			t.Fatalf("expected created at, got %v", got)
		}
	})

	t.Run("classifyRefreshActivity buckets", func(t *testing.T) {
		if label, rapid := classifyRefreshActivity(models.UserAuthSession{}, sessionRiskContext{}, policy); label != "unknown" || rapid {
			t.Fatalf("nil refresh: %q %v", label, rapid)
		}
		zero := time.Time{}
		if label, rapid := classifyRefreshActivity(models.UserAuthSession{LastRefreshedAt: &zero}, sessionRiskContext{}, policy); label != "unknown" || rapid {
			t.Fatalf("zero refresh: %q %v", label, rapid)
		}
		hot := now.Add(-5 * time.Minute)
		if label, rapid := classifyRefreshActivity(models.UserAuthSession{LastRefreshedAt: &hot}, sessionRiskContext{HotRefreshCount: 2}, policy); label != "hot" || !rapid {
			t.Fatalf("hot family: %q %v", label, rapid)
		}
		if label, rapid := classifyRefreshActivity(models.UserAuthSession{LastRefreshedAt: &hot}, sessionRiskContext{HotRefreshCount: 1}, policy); label != "hot" || rapid {
			t.Fatalf("hot single: %q %v", label, rapid)
		}
		recent := now.Add(-30 * time.Minute)
		if label, rapid := classifyRefreshActivity(models.UserAuthSession{LastRefreshedAt: &recent}, sessionRiskContext{HotRefreshCount: 9}, policy); label != "recent" || rapid {
			t.Fatalf("recent: %q %v", label, rapid)
		}
		today := now.Add(-3 * time.Hour)
		if label, rapid := classifyRefreshActivity(models.UserAuthSession{LastRefreshedAt: &today}, sessionRiskContext{}, policy); label != "today" || rapid {
			t.Fatalf("today: %q %v", label, rapid)
		}
		stale := now.Add(-48 * time.Hour)
		if label, rapid := classifyRefreshActivity(models.UserAuthSession{LastRefreshedAt: &stale}, sessionRiskContext{}, policy); label != "stale" || rapid {
			t.Fatalf("stale: %q %v", label, rapid)
		}
	})

	t.Run("isSessionMoreRecent", func(t *testing.T) {
		if !isSessionMoreRecent(models.UserAuthSession{}, nil) {
			t.Fatal("nil current should lose")
		}
		older := models.UserAuthSession{UpdatedAt: now.Add(-time.Hour)}
		newer := models.UserAuthSession{UpdatedAt: now}
		if !isSessionMoreRecent(newer, &older) {
			t.Fatal("newer candidate should win")
		}
		if isSessionMoreRecent(older, &newer) {
			t.Fatal("older candidate should lose")
		}
	})
}

func TestAxcDetectSessionDrift(t *testing.T) {
	policy := defaultSessionRiskPolicy()
	now := time.Now().UTC()
	older := now.Add(-2 * time.Hour)

	t.Run("no reference session", func(t *testing.T) {
		if got := detectSessionDrift(models.UserAuthSession{ID: "a"}, sessionRiskContext{}, policy); got != (sessionDriftSignals{}) {
			t.Fatalf("expected zero drift, got %+v", got)
		}
		if got := detectSessionDrift(models.UserAuthSession{ID: "a"}, sessionRiskContext{LatestActive: &models.UserAuthSession{ID: ""}}, policy); got != (sessionDriftSignals{}) {
			t.Fatalf("expected zero drift, got %+v", got)
		}
		ref := models.UserAuthSession{ID: "a"}
		if got := detectSessionDrift(models.UserAuthSession{ID: "a"}, sessionRiskContext{LatestActive: &ref}, policy); got != (sessionDriftSignals{}) {
			t.Fatalf("expected zero drift for self reference, got %+v", got)
		}
	})

	t.Run("drift with older candidate computes absolute window", func(t *testing.T) {
		ref := models.UserAuthSession{ID: "ref", ClientIP: "8.8.8.8", DeviceFingerprint: "fp-ref", LastSeenAt: &now}
		session := models.UserAuthSession{ID: "cand", ClientIP: "1.1.1.1", DeviceFingerprint: "fp-cand", LastSeenAt: &older}
		got := detectSessionDrift(session, sessionRiskContext{LatestActive: &ref}, policy)
		if !got.IPDrift || !got.DeviceDrift || !got.RapidIPChange || !got.RapidDeviceChange || got.ReferenceSessionID != "ref" {
			t.Fatalf("unexpected drift: %+v", got)
		}
	})

	t.Run("drift with newer candidate keeps positive window", func(t *testing.T) {
		refOlder := now.Add(-2 * time.Hour)
		ref := models.UserAuthSession{ID: "ref", ClientIP: "8.8.8.8", DeviceFingerprint: "fp-ref", LastSeenAt: &refOlder}
		session := models.UserAuthSession{ID: "cand", ClientIP: "1.1.1.1", DeviceFingerprint: "fp-cand", LastSeenAt: &now}
		got := detectSessionDrift(session, sessionRiskContext{LatestActive: &ref}, policy)
		if !got.IPDrift || !got.RapidIPChange {
			t.Fatalf("unexpected drift: %+v", got)
		}
	})

	t.Run("empty client values disable drift", func(t *testing.T) {
		ref := models.UserAuthSession{ID: "ref", ClientIP: "", DeviceFingerprint: "fp-ref", LastSeenAt: &now}
		session := models.UserAuthSession{ID: "cand", ClientIP: "1.1.1.1", DeviceFingerprint: "fp-cand", LastSeenAt: &now}
		got := detectSessionDrift(session, sessionRiskContext{LatestActive: &ref}, policy)
		if got.IPDrift {
			t.Fatalf("expected no ip drift: %+v", got)
		}
		ref2 := models.UserAuthSession{ID: "ref", ClientIP: "8.8.8.8", DeviceFingerprint: "", LastSeenAt: &now}
		got = detectSessionDrift(session, sessionRiskContext{LatestActive: &ref2}, policy)
		if got.DeviceDrift {
			t.Fatalf("expected no device drift: %+v", got)
		}
	})
}

func TestAxcBuildSessionRiskContext(t *testing.T) {
	now := time.Now().UTC()
	hot := now.Add(-2 * time.Minute)
	revokedAt := now.Add(-time.Hour)
	sessions := []models.UserAuthSession{
		{ID: "revoked-at", Status: "active", ClientIP: "8.8.8.8", DeviceFingerprint: "fp-1", RevokedAt: &revokedAt},
		{ID: "revoked-status", Status: "REVOKED", ClientIP: "8.8.8.8", DeviceFingerprint: "fp-1"},
		{ID: "public-a", Status: "active", ClientIP: "203.0.113.5", DeviceFingerprint: "fp-1", LastSeenAt: &now, LastRefreshedAt: &hot},
		{ID: "public-blank-ip", Status: "active", ClientIP: "   ", DeviceFingerprint: "fp-2", LastSeenAt: ptrTime(now.Add(time.Minute))},
		{ID: "private", Status: "active", ClientIP: "10.0.0.9", DeviceFingerprint: "fp-2", LastSeenAt: &now},
	}
	got := buildSessionRiskContext(sessions, defaultSessionRiskPolicy(), nil)
	if got.ActiveSessionCount != 3 {
		t.Fatalf("active count=%d want 3", got.ActiveSessionCount)
	}
	if got.PublicIPCount != 1 {
		t.Fatalf("public ip count=%d want 1", got.PublicIPCount)
	}
	if got.DeviceCount != 2 {
		t.Fatalf("device count=%d want 2", got.DeviceCount)
	}
	if got.HotRefreshCount != 1 {
		t.Fatalf("hot refresh count=%d want 1", got.HotRefreshCount)
	}
	if got.LatestActive == nil || got.LatestActive.ID != "public-blank-ip" {
		t.Fatalf("unexpected latest active: %+v", got.LatestActive)
	}
	publicProvider := axcFixedIPIntel{desc: sessionIPDescription{NetworkLabel: "public", LocationLabel: "geo:x"}}
	got = buildSessionRiskContext([]models.UserAuthSession{{ID: "blank", Status: "active", ClientIP: " "}}, defaultSessionRiskPolicy(), publicProvider)
	if got.PublicIPCount != 0 {
		t.Fatalf("blank public ip should not count, got %d", got.PublicIPCount)
	}
}

func TestAxcDescribeSessionRisk(t *testing.T) {
	now := time.Now().UTC()
	stale := now.Add(-60 * 24 * time.Hour)
	hot := now.Add(-time.Minute)
	policy := defaultSessionRiskPolicy()

	t.Run("high risk accumulates all reasons", func(t *testing.T) {
		ref := models.UserAuthSession{ID: "ref", ClientIP: "9.9.9.9", DeviceFingerprint: "fp-ref", LastSeenAt: &now}
		session := models.UserAuthSession{
			ID: "s1", Status: "revoked", ClientIP: "8.8.8.8", DeviceFingerprint: "fp-cand",
			LastRefreshedAt: &hot, RevokedAt: &stale,
		}
		ctx := sessionRiskContext{
			ActiveSessionCount: 3, PublicIPCount: 2, DeviceCount: 3, HotRefreshCount: 2, LatestActive: &ref,
		}
		score, level, reasons, network, location, drift := describeSessionRisk(session, false, ctx, policy, nil)
		if level != "high" || score < policy.HighRiskScore {
			t.Fatalf("level=%q score=%d", level, score)
		}
		if network != "public" || location != "public_unknown" {
			t.Fatalf("network=%q location=%q", network, location)
		}
		if !drift.IPDrift || !drift.DeviceDrift || !drift.RapidIPChange || !drift.RapidDeviceChange {
			t.Fatalf("expected drift signals: %+v", drift)
		}
		joined := strings.Join(reasons, ",")
		for _, want := range []string{"revoked_session", "not_current_session", "public_network", "multi_public_ip_family", "many_active_sessions", "ip_drift", "device_drift", "rapid_ip_change", "rapid_device_change", "rapid_refresh_activity", "stale_activity"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("missing reason %q in %v", want, reasons)
			}
		}
	})

	t.Run("missing device fingerprint flagged", func(t *testing.T) {
		session := models.UserAuthSession{ID: "s1b", Status: "active", ClientIP: "10.1.2.3", LastSeenAt: &now}
		_, level, reasons, _, _, _ := describeSessionRisk(session, true, sessionRiskContext{ActiveSessionCount: 1}, policy, nil)
		if !strings.Contains(strings.Join(reasons, ","), "missing_device_fingerprint") {
			t.Fatalf("reasons=%v", reasons)
		}
		if level != "low" {
			t.Fatalf("level=%q", level)
		}
	})

	t.Run("documentation location flagged", func(t *testing.T) {
		session := models.UserAuthSession{ID: "s2", Status: "active", ClientIP: "192.0.2.10", DeviceFingerprint: "fp", LastSeenAt: &now}
		_, level, reasons, _, location, _ := describeSessionRisk(session, false, sessionRiskContext{}, policy, nil)
		if location != "documentation" {
			t.Fatalf("location=%q", location)
		}
		if !strings.Contains(strings.Join(reasons, ","), "non_geolocatable_network") {
			t.Fatalf("reasons=%v", reasons)
		}
		if level != "medium" {
			t.Fatalf("level=%q", level)
		}
	})

	t.Run("shared address space location flagged", func(t *testing.T) {
		session := models.UserAuthSession{ID: "s3", Status: "active", ClientIP: "100.64.1.1", DeviceFingerprint: "fp", LastSeenAt: &now}
		_, level, reasons, network, location, _ := describeSessionRisk(session, false, sessionRiskContext{}, policy, nil)
		if network != "private" || location != "shared_address_space" {
			t.Fatalf("network=%q location=%q", network, location)
		}
		if !strings.Contains(strings.Join(reasons, ","), "non_geolocatable_network") {
			t.Fatalf("reasons=%v", reasons)
		}
		if level != "medium" {
			t.Fatalf("level=%q", level)
		}
	})

	t.Run("healthy current session stays low", func(t *testing.T) {
		session := models.UserAuthSession{ID: "s4", Status: "active", ClientIP: "10.1.2.3", DeviceFingerprint: "fp", LastSeenAt: &now}
		score, level, reasons, network, location, _ := describeSessionRisk(session, true, sessionRiskContext{ActiveSessionCount: 1}, policy, nil)
		if level != "low" || score != 0 || len(reasons) != 0 {
			t.Fatalf("level=%q score=%d reasons=%v", level, score, reasons)
		}
		if network != "private" || location != "private" {
			t.Fatalf("network=%q location=%q", network, location)
		}
	})
}

func TestAxcIPClassification(t *testing.T) {
	t.Run("classifyNetworkLabel", func(t *testing.T) {
		cases := map[string]string{
			"":               "unknown",
			"   ":            "unknown",
			"not-an-ip":      "unknown",
			"127.0.0.1":      "loopback",
			"::1":            "loopback",
			"100.64.0.1":     "private",
			"10.0.0.1":       "private",
			"172.16.0.1":     "private",
			"172.31.255.255": "private",
			"172.32.0.1":     "public",
			"192.168.1.1":    "private",
			"8.8.8.8":        "public",
			"fd00::1":        "private",
			"2001:db8::1":    "public",
			"  10.0.0.5  ":   "private",
		}
		for ip, want := range cases {
			if got := classifyNetworkLabel(ip); got != want {
				t.Fatalf("classifyNetworkLabel(%q)=%q want %q", ip, got, want)
			}
		}
	})

	t.Run("classifyLocationLabel", func(t *testing.T) {
		cases := map[string]string{
			"":              "unknown",
			"bogus":         "unknown",
			"127.0.0.1":     "loopback",
			"192.0.2.1":     "documentation",
			"198.51.100.9":  "documentation",
			"203.0.113.9":   "documentation",
			"100.64.1.1":    "shared_address_space",
			"10.2.3.4":      "private",
			"172.20.1.1":    "private",
			"192.169.1.1":   "public_unknown",
			"8.8.8.8":       "public_unknown",
			"fd12::1":       "private",
			" 203.0.113.1 ": "documentation",
		}
		for ip, want := range cases {
			if got := classifyLocationLabel(ip); got != want {
				t.Fatalf("classifyLocationLabel(%q)=%q want %q", ip, got, want)
			}
		}
	})

	t.Run("isPrivateIP", func(t *testing.T) {
		cases := map[string]bool{
			"10.1.1.1":    true,
			"172.16.1.1":  true,
			"172.31.1.1":  true,
			"172.15.1.1":  false,
			"172.32.1.1":  false,
			"192.168.0.1": true,
			"192.167.0.1": false,
			"203.0.113.1": false,
			"100.64.0.1":  false,
			"fd00::1":     true,
			"2001:db8::1": false,
		}
		for ip, want := range cases {
			if got := isPrivateIP(axcParseIP(t, ip)); got != want {
				t.Fatalf("isPrivateIP(%q)=%v want %v", ip, got, want)
			}
		}
	})

	t.Run("isDocumentationIP", func(t *testing.T) {
		cases := map[string]bool{
			"192.0.2.1":    true,
			"198.51.100.1": true,
			"203.0.113.1":  true,
			"8.8.8.8":      false,
			"192.0.3.1":    false,
			"::1":          false,
		}
		for ip, want := range cases {
			if got := isDocumentationIP(axcParseIP(t, ip)); got != want {
				t.Fatalf("isDocumentationIP(%q)=%v want %v", ip, got, want)
			}
		}
	})

	t.Run("isSharedAddressSpaceIP", func(t *testing.T) {
		cases := map[string]bool{
			"100.64.0.1":    true,
			"100.127.255.1": true,
			"100.63.255.1":  false,
			"100.128.0.1":   false,
			"101.0.0.1":     false,
			"2001:db8::1":   false,
		}
		for ip, want := range cases {
			if got := isSharedAddressSpaceIP(axcParseIP(t, ip)); got != want {
				t.Fatalf("isSharedAddressSpaceIP(%q)=%v want %v", ip, got, want)
			}
		}
	})
}

func axcParseIP(t *testing.T, ip string) net.IP {
	t.Helper()
	parsed := net.ParseIP(ip)
	if parsed == nil {
		t.Fatalf("invalid test ip %q", ip)
	}
	return parsed
}

func TestAxcMapUserResponse(t *testing.T) {
	if got := mapUserResponse(nil); got != (userResponse{}) {
		t.Fatalf("expected zero response, got %+v", got)
	}
	user := &models.User{ID: 3, Username: "u", Email: "e", Name: "n", Phone: "p", Avatar: "av", Role: "r", Status: "s"}
	got := mapUserResponse(user)
	if got.ID != 3 || got.Username != "u" || got.Email != "e" || got.Name != "n" || got.Phone != "p" || got.Avatar != "av" || got.Role != "r" || got.Status != "s" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

// TestAxcSessionRiskPolicyFromConfigAllFields 直测 config→policy 全字段映射
// （原经 WithSessionRiskPolicyConfig 间接覆盖，该死入口删除后改为直测）。
func TestAxcSessionRiskPolicyFromConfigAllFields(t *testing.T) {
	got := sessionRiskPolicyFromConfig(config.SessionRiskPolicyConfig{
		HotRefreshWindowMinutes:    5,
		RecentRefreshWindowMinutes: 30,
		TodayRefreshWindowHours:    12,
		RapidChangeWindowHours:     6,
		StaleActivityWindowDays:    7,
		MultiPublicIPThreshold:     4,
		ManySessionsThreshold:      5,
		HotRefreshFamilyThreshold:  3,
		MediumRiskScore:            6,
		HighRiskScore:              9,
	})
	want := sessionRiskPolicy{
		HotRefreshWindow:          5 * time.Minute,
		RecentRefreshWindow:       30 * time.Minute,
		TodayRefreshWindow:        12 * time.Hour,
		RapidChangeWindow:         6 * time.Hour,
		StaleActivityWindow:       7 * 24 * time.Hour,
		MultiPublicIPThreshold:    4,
		ManySessionsThreshold:     5,
		HotRefreshFamilyThreshold: 3,
		MediumRiskScore:           6,
		HighRiskScore:             9,
	}
	if got != want {
		t.Fatalf("unexpected policy: got %+v want %+v", got, want)
	}
}
