package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"

	"github.com/gin-gonic/gin"
)

func TestAuthMiddleware_RBACRoleExpansion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Unix(1_700_000_000, 0)
	secret := "test-secret"
	token := createTestHS256JWT(t, map[string]interface{}{
		"user_id": 1,
		"roles":   []string{"viewer"},
		"iat":     now.Unix(),
		"exp":     now.Add(10 * time.Minute).Unix(),
	}, secret)

	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{
		Secret: secret,
		RBAC: config.RBACConfig{
			Enabled: true,
			Roles: map[string][]string{
				"viewer": {"tickets.read"},
			},
		},
		Now: func() time.Time { return now },
	}))
	r.Use(RequireResourcePermission("tickets"))
	r.GET("/tickets", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	r.POST("/tickets", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/tickets", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost, "/tickets", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("POST expected 403 got %d body=%s", w2.Code, w2.Body.String())
	}
}

func TestAuthMiddleware_PrincipalKindProjectedAndGuarded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Unix(1_700_000_000, 0)
	secret := "test-secret"

	t.Run("service token allowed", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"token_type": "service",
			"iat":        now.Unix(),
			"exp":        now.Add(10 * time.Minute).Unix(),
		}, secret)

		r := gin.New()
		r.Use(AuthMiddleware(MiddlewareConfig{
			Secret: secret,
			Now:    func() time.Time { return now },
		}))
		r.Use(RequirePrincipalKinds(PrincipalService, PrincipalAdmin))
		r.GET("/management", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"principal_kind": c.MustGet("principal_kind"),
			})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/management", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}
		if body := w.Body.String(); body == "" || !strings.Contains(body, PrincipalService) {
			t.Fatalf("expected principal kind in response, body=%s", body)
		}
	})

	t.Run("end user denied", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"sub": "customer-1",
			"iat": now.Unix(),
			"exp": now.Add(10 * time.Minute).Unix(),
		}, secret)

		r := gin.New()
		r.Use(AuthMiddleware(MiddlewareConfig{
			Secret: secret,
			Now:    func() time.Time { return now },
		}))
		r.Use(RequirePrincipalKinds(PrincipalAgent, PrincipalAdmin, PrincipalService))
		r.GET("/management", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/management", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestAuthMiddleware_ProjectsTenantAndWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Unix(1_700_000_000, 0)
	secret := "test-secret"
	token := createTestHS256JWT(t, map[string]interface{}{
		"user_id":      1,
		"roles":        []string{"agent"},
		"tenant_id":    "tenant-a",
		"workspace_id": "workspace-1",
		"iat":          now.Unix(),
		"exp":          now.Add(10 * time.Minute).Unix(),
	}, secret)

	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{
		Secret: secret,
		Now:    func() time.Time { return now },
	}))
	r.GET("/claims", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"tenant_id":    c.MustGet("tenant_id"),
			"workspace_id": c.MustGet("workspace_id"),
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/claims", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "tenant-a") || !strings.Contains(body, "workspace-1") {
		t.Fatalf("expected tenant/workspace in response, body=%s", body)
	}
}

func TestAuthMiddleware_RejectsNonAccessTokenUse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Unix(1_700_000_000, 0)
	secret := "test-secret"

	r := gin.New()
	r.Use(AuthMiddleware(MiddlewareConfig{
		Secret: secret,
		Now:    func() time.Time { return now },
	}))
	r.GET("/protected", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// 历史 access token 无 token_use claim → 放行；显式 access 同样放行
	legacyToken := createTestHS256JWT(t, map[string]interface{}{
		"user_id": 1,
		"iat":     now.Unix(),
		"exp":     now.Add(10 * time.Minute).Unix(),
	}, secret)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+legacyToken)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("legacy token expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	// 2fa_challenge / refresh 等 token_use 一律拒绝——挑战 token 换不到任何受保护资源
	for _, use := range []string{"2fa_challenge", "refresh"} {
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":   1,
			"token_use": use,
			"iat":       now.Unix(),
			"exp":       now.Add(10 * time.Minute).Unix(),
		}, secret)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("token_use=%q expected 401 got %d body=%s", use, w.Code, w.Body.String())
		}
	}
}
