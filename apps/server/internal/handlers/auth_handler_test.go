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

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/configscope"
	"servify/apps/server/internal/services"

	"github.com/gin-gonic/gin"
)

type stubAuthService struct {
	refreshToken string
	meta         services.AuthSessionMetadata
	sessions     []models.UserAuthSession
	revoked      *models.UserAuthSession
	revokeCount  int
	refreshResp  *services.AuthResult
	refreshErr   error
	loginOutcome *services.LoginOutcome
	loginErr     error
}

type stubAuthSessionRiskProvider struct {
	value config.SessionRiskPolicyConfig
	ok    bool
	err   error
}

type stubSessionIPIntelligence struct {
	desc sessionIPDescription
}

func (s stubSessionIPIntelligence) DescribeIP(ip string) sessionIPDescription {
	return s.desc
}

func (s stubAuthSessionRiskProvider) LoadSessionRiskConfig(ctx context.Context) (config.SessionRiskPolicyConfig, bool, error) {
	return s.value, s.ok, s.err
}

func (s *stubAuthService) Register(ctx context.Context, req services.RegisterInput, meta services.AuthSessionMetadata) (*services.AuthResult, error) {
	s.meta = meta
	return nil, errors.New("not implemented")
}

func (s *stubAuthService) Login(ctx context.Context, req services.LoginInput, meta services.AuthSessionMetadata) (*services.LoginOutcome, error) {
	s.meta = meta
	if s.loginErr != nil || s.loginOutcome != nil {
		return s.loginOutcome, s.loginErr
	}
	return nil, errors.New("not implemented")
}

func (s *stubAuthService) GetCurrentUser(ctx context.Context, userID uint) (*models.User, error) {
	return nil, errors.New("not implemented")
}

func (s *stubAuthService) ListAuthSessions(ctx context.Context, userID uint) ([]models.UserAuthSession, error) {
	return s.sessions, nil
}

func (s *stubAuthService) RevokeCurrentSession(ctx context.Context, userID uint, sessionID string) (*models.UserAuthSession, error) {
	if s.revoked != nil {
		return s.revoked, nil
	}
	return &models.UserAuthSession{ID: sessionID, Status: "revoked", TokenVersion: 2}, nil
}

func (s *stubAuthService) RevokeOtherSessions(ctx context.Context, userID uint, currentSessionID string) (int, error) {
	return s.revokeCount, nil
}

func (s *stubAuthService) RefreshToken(ctx context.Context, refreshToken string, meta services.AuthSessionMetadata) (*services.AuthResult, error) {
	s.refreshToken = refreshToken
	s.meta = meta
	return s.refreshResp, s.refreshErr
}

func TestAuthHandlerRefreshToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("accepts refresh token from body", func(t *testing.T) {
		svc := &stubAuthService{
			refreshResp: &services.AuthResult{
				Token:            "access-2",
				ExpiresIn:        3600,
				RefreshToken:     "refresh-2",
				RefreshExpiresIn: 86400,
				User:             &models.User{ID: 7, Username: "demo", Role: "admin", Status: "active"},
			},
		}
		handler := NewAuthHandler(svc)
		r := gin.New()
		r.POST("/api/v1/auth/refresh", handler.RefreshToken)

		body, _ := json.Marshal(map[string]string{"refresh_token": "refresh-1"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "servify-test/1.0")
		req.Header.Set("X-Device-ID", "device-explicit-1")
		req.RemoteAddr = "203.0.113.9:4567"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if svc.refreshToken != "refresh-1" {
			t.Fatalf("refresh token = %q want refresh-1", svc.refreshToken)
		}
		if svc.meta.UserAgent != "servify-test/1.0" || svc.meta.ClientIP != "203.0.113.9" || svc.meta.DeviceFingerprint != "device-explicit-1" {
			t.Fatalf("unexpected session metadata: %+v", svc.meta)
		}
		if !bytes.Contains(w.Body.Bytes(), []byte(`"refresh_token":"refresh-2"`)) {
			t.Fatalf("expected refresh token in response: %s", w.Body.String())
		}
	})

	t.Run("falls back to bearer token", func(t *testing.T) {
		svc := &stubAuthService{
			refreshResp: &services.AuthResult{
				Token:            "access-2",
				ExpiresIn:        3600,
				RefreshToken:     "refresh-2",
				RefreshExpiresIn: 86400,
				User:             &models.User{ID: 7, Username: "demo", Role: "admin", Status: "active"},
			},
		}
		handler := NewAuthHandler(svc)
		r := gin.New()
		r.POST("/api/v1/auth/refresh", handler.RefreshToken)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
		req.Header.Set("Authorization", "Bearer refresh-bearer")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if svc.refreshToken != "refresh-bearer" {
			t.Fatalf("refresh token = %q want refresh-bearer", svc.refreshToken)
		}
	})

	t.Run("invalid refresh token returns unauthorized", func(t *testing.T) {
		svc := &stubAuthService{refreshErr: services.ErrAuthInvalidRefreshToken}
		handler := NewAuthHandler(svc)
		r := gin.New()
		r.POST("/api/v1/auth/refresh", handler.RefreshToken)

		body, _ := json.Marshal(map[string]string{"refresh_token": "bad"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestAuthHandlerSelfServiceSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &stubAuthService{
		sessions: []models.UserAuthSession{
			{ID: "sess-a", Status: "active", TokenVersion: 1, DeviceFingerprint: "fp-a", UserAgent: "browser-a", ClientIP: "203.0.113.10", LastSeenAt: ptrTime(time.Now().UTC().Add(-2 * time.Hour)), LastRefreshedAt: ptrTime(time.Now().UTC().Add(-10 * time.Minute))},
			{ID: "sess-b", Status: "active", TokenVersion: 2, DeviceFingerprint: "fp-b", UserAgent: "browser-b", ClientIP: "203.0.113.11", LastSeenAt: ptrTime(time.Now().UTC().Add(-1 * time.Hour)), LastRefreshedAt: ptrTime(time.Now().UTC().Add(-5 * time.Minute))},
		},
		revoked:     &models.UserAuthSession{ID: "sess-a", Status: "revoked", TokenVersion: 2},
		revokeCount: 1,
	}
	handler := NewAuthHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(7))
		c.Set("session_id", "sess-a")
		c.Next()
	})
	r.GET("/api/v1/auth/sessions", handler.ListSessions)
	r.POST("/api/v1/auth/sessions/logout-current", handler.LogoutCurrentSession)
	r.POST("/api/v1/auth/sessions/logout-others", handler.LogoutOtherSessions)

	t.Run("list sessions includes current marker", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if !bytes.Contains(w.Body.Bytes(), []byte(`"session_id":"sess-a"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"is_current":true`)) {
			t.Fatalf("expected current session in response: %s", w.Body.String())
		}
		if !bytes.Contains(w.Body.Bytes(), []byte(`"device_fingerprint":"fp-a"`)) {
			t.Fatalf("expected device fingerprint in response: %s", w.Body.String())
		}
		if !bytes.Contains(w.Body.Bytes(), []byte(`"network_label":"public"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"location_label":"documentation"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"family_public_ip_count":2`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"active_session_count":2`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"family_hot_refresh_count":2`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"reference_session_id":"sess-b"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"ip_drift":true`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"device_drift":true`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"rapid_ip_change":true`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"rapid_device_change":true`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"refresh_recency":"hot"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"rapid_refresh_activity":true`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"multi_public_ip_family"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"rapid_ip_change"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"rapid_device_change"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"rapid_refresh_activity"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"risk_score":8`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"risk_level":"high"`)) {
			t.Fatalf("expected risk fields in response: %s", w.Body.String())
		}
		if !bytes.Contains(w.Body.Bytes(), []byte(`"user_agent":"browser-a"`)) {
			t.Fatalf("expected session metadata in response: %s", w.Body.String())
		}
	})

	t.Run("logout current session", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sessions/logout-current", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if !bytes.Contains(w.Body.Bytes(), []byte(`"session_id":"sess-a"`)) {
			t.Fatalf("expected revoked current session: %s", w.Body.String())
		}
	})

	t.Run("logout other sessions", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sessions/logout-others", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if !bytes.Contains(w.Body.Bytes(), []byte(`"count":1`)) {
			t.Fatalf("expected count=1: %s", w.Body.String())
		}
	})
}

func TestAuthHandlerSelfServiceSessionsUsesScopedRiskPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &stubAuthService{
		sessions: []models.UserAuthSession{
			{ID: "sess-a", Status: "active", TokenVersion: 1, DeviceFingerprint: "fp-a", UserAgent: "browser-a", ClientIP: "203.0.113.10", LastSeenAt: ptrTime(time.Now().UTC().Add(-2 * time.Hour)), LastRefreshedAt: ptrTime(time.Now().UTC().Add(-10 * time.Minute))},
			{ID: "sess-b", Status: "active", TokenVersion: 2, DeviceFingerprint: "fp-b", UserAgent: "browser-b", ClientIP: "203.0.113.11", LastSeenAt: ptrTime(time.Now().UTC().Add(-1 * time.Hour)), LastRefreshedAt: ptrTime(time.Now().UTC().Add(-5 * time.Minute))},
		},
	}
	resolver := configscope.NewResolver(
		&config.Config{
			Security: config.SecurityConfig{
				SessionRisk: config.SessionRiskPolicyConfig{
					MediumRiskScore: 2,
					HighRiskScore:   4,
				},
			},
		},
		configscope.WithTenantSessionRiskProvider(stubAuthSessionRiskProvider{
			ok: true,
			value: config.SessionRiskPolicyConfig{
				HighRiskScore: 10,
			},
		}),
	)
	handler := NewAuthHandler(svc).WithSessionRiskResolver(resolver)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(7))
		c.Set("session_id", "sess-a")
		c.Request = c.Request.WithContext(platformauth.ContextWithScope(c.Request.Context(), "tenant-a", "workspace-1"))
		c.Next()
	})
	r.GET("/api/v1/auth/sessions", handler.ListSessions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"risk_score":8`)) {
		t.Fatalf("expected unchanged risk score in response: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"risk_level":"medium"`)) {
		t.Fatalf("expected scoped risk policy to downgrade high threshold: %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`"risk_level":"high"`)) {
		t.Fatalf("expected no high risk level after scoped override: %s", w.Body.String())
	}
}

func TestAuthHandlerSelfServiceSessionsUsesEnvironmentRiskProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &stubAuthService{
		sessions: []models.UserAuthSession{
			{ID: "sess-a", Status: "active", TokenVersion: 1, DeviceFingerprint: "fp-a", UserAgent: "browser-a", ClientIP: "203.0.113.10", LastSeenAt: ptrTime(time.Now().UTC().Add(-2 * time.Hour)), LastRefreshedAt: ptrTime(time.Now().UTC().Add(-10 * time.Minute))},
			{ID: "sess-b", Status: "active", TokenVersion: 2, DeviceFingerprint: "fp-b", UserAgent: "browser-b", ClientIP: "203.0.113.11", LastSeenAt: ptrTime(time.Now().UTC().Add(-1 * time.Hour)), LastRefreshedAt: ptrTime(time.Now().UTC().Add(-5 * time.Minute))},
		},
	}
	resolver := configscope.NewResolver(
		&config.Config{
			Server: config.ServerConfig{
				Environment: "staging",
			},
			Security: config.SecurityConfig{
				SessionRisk: config.SessionRiskPolicyConfig{
					MediumRiskScore: 2,
					HighRiskScore:   4,
				},
				SessionRiskProfiles: map[string]config.SessionRiskPolicyConfig{
					"staging": {
						HighRiskScore: 10,
					},
				},
			},
		},
	)
	handler := NewAuthHandler(svc).WithSessionRiskResolver(resolver)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(7))
		c.Set("session_id", "sess-a")
		c.Next()
	})
	r.GET("/api/v1/auth/sessions", handler.ListSessions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"risk_score":8`)) {
		t.Fatalf("expected unchanged risk score in response: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"risk_level":"medium"`)) {
		t.Fatalf("expected environment risk profile to downgrade high threshold: %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`"risk_level":"high"`)) {
		t.Fatalf("expected no high risk level after environment profile: %s", w.Body.String())
	}
}

func TestAuthHandlerSelfServiceSessionsUsesInjectedIPIntelligence(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &stubAuthService{
		sessions: []models.UserAuthSession{
			{ID: "sess-a", Status: "active", TokenVersion: 1, DeviceFingerprint: "fp-a", UserAgent: "browser-a", ClientIP: "8.8.8.8", LastSeenAt: ptrTime(time.Now().UTC().Add(-10 * time.Minute))},
		},
	}
	handler := NewAuthHandler(svc).WithSessionIPIntelligence(stubSessionIPIntelligence{
		desc: sessionIPDescription{
			NetworkLabel:  "public",
			LocationLabel: "geo:cn-zj",
		},
	})
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(7))
		c.Set("session_id", "sess-a")
		c.Next()
	})
	r.GET("/api/v1/auth/sessions", handler.ListSessions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"location_label":"geo:cn-zj"`)) {
		t.Fatalf("expected injected location label in response: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"network_label":"public"`)) {
		t.Fatalf("expected injected network label in response: %s", w.Body.String())
	}
}

func ptrTime(v time.Time) *time.Time {
	return &v
}
