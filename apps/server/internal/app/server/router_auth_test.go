package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	"servify/apps/server/internal/services"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type stubAIHandlerService struct{}

func TestSessionIPIntelligenceFromConfig(t *testing.T) {
	t.Run("returns nil when disabled or empty", func(t *testing.T) {
		if got := sessionIPIntelligenceFromConfig(nil); got != nil {
			t.Fatalf("expected nil provider for nil config")
		}

		cfg := testRouterConfig()
		cfg.Security.SessionIPIntelligence.Enabled = false
		cfg.Security.SessionIPIntelligence.BaseURL = "https://geo.example.com/lookup/{ip}"
		if got := sessionIPIntelligenceFromConfig(cfg); got != nil {
			t.Fatalf("expected nil provider when feature disabled")
		}

		cfg.Security.SessionIPIntelligence.Enabled = true
		cfg.Security.SessionIPIntelligence.BaseURL = "   "
		if got := sessionIPIntelligenceFromConfig(cfg); got != nil {
			t.Fatalf("expected nil provider for blank base url")
		}
	})

	t.Run("builds provider from config", func(t *testing.T) {
		ipSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/lookup/8.8.8.8" {
				t.Fatalf("unexpected lookup path %q", r.URL.Path)
			}
			if got := r.Header.Get("X-Geo-Key"); got != "geo-secret" {
				t.Fatalf("unexpected auth header %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"network_label":"public","location_label":"geo:test-config"}`))
		}))
		defer ipSrv.Close()

		cfg := testRouterConfig()
		cfg.Security.SessionIPIntelligence.Enabled = true
		cfg.Security.SessionIPIntelligence.BaseURL = ipSrv.URL + "/lookup/{ip}"
		cfg.Security.SessionIPIntelligence.APIKey = "geo-secret"
		cfg.Security.SessionIPIntelligence.AuthHeader = "X-Geo-Key"
		cfg.Security.SessionIPIntelligence.TimeoutMs = 2300

		provider := sessionIPIntelligenceFromConfig(cfg)
		if provider == nil {
			t.Fatal("expected provider")
		}
		desc := provider.DescribeIP("8.8.8.8")
		if desc.NetworkLabel != "public" || desc.LocationLabel != "geo:test-config" {
			t.Fatalf("unexpected description %+v", desc)
		}
	})
}

func (stubAIHandlerService) ProcessQuery(_ context.Context, _ string, _ string) (interface{}, error) {
	return nil, nil
}

func (stubAIHandlerService) GetStatus(_ context.Context) map[string]interface{} {
	return map[string]interface{}{"status": "ok"}
}

func (stubAIHandlerService) GetMetrics() (*services.AIMetrics, bool) {
	return nil, false
}

func (stubAIHandlerService) UploadKnowledgeDocument(_ context.Context, _, _ string, _ []string) error {
	return nil
}

func (stubAIHandlerService) SyncKnowledgeBase(_ context.Context) error {
	return nil
}

func (stubAIHandlerService) SetKnowledgeProviderEnabled(bool) bool {
	return true
}

func (stubAIHandlerService) ResetCircuitBreaker() bool {
	return true
}

func createTestHS256JWT(t *testing.T, payload map[string]interface{}, secret string) string {
	t.Helper()

	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	enc := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	h := enc(headerJSON)
	p := enc(payloadJSON)
	signing := h + "." + p

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	return signing + "." + enc(mac.Sum(nil))
}

func testRouterConfig() *config.Config {
	cfg := config.GetDefaultConfig()
	cfg.JWT.Secret = "test-secret"
	return cfg
}

func newRouterAuthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:router_auth_"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.UserAuthSession{}, &models.RevokedToken{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestBuildRouter_AIV1RequiresManagementPrincipal(t *testing.T) {
	router := BuildRouter(Dependencies{
		Config:           testRouterConfig(),
		AIHandlerService: stubAIHandlerService{},
	})

	t.Run("missing token rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/status", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("end user token rejected", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"sub":            "customer-1",
			"principal_kind": "end_user",
		}, "test-secret")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/status", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("agent token allowed", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        7,
			"principal_kind": "agent",
			"roles":          []string{"agent"},
		}, "test-secret")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/status", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"success":true`) {
			t.Fatalf("expected success response, body=%s", w.Body.String())
		}
	})
}

func TestBuildRouter_MetricsIngestIsServiceOnly(t *testing.T) {
	router := BuildRouter(Dependencies{
		Config: testRouterConfig(),
	})

	t.Run("agent token rejected", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        9,
			"principal_kind": "agent",
			"roles":          []string{"agent"},
		}, "test-secret")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/metrics/ingest", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("service token allowed through auth layer", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"token_type": "service",
		}, "test-secret")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/metrics/ingest", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 after passing auth, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestBuildRouter_PublicRoutesRemainAnonymous(t *testing.T) {
	router := BuildRouter(Dependencies{
		Config: testRouterConfig(),
	})

	req := httptest.NewRequest(http.MethodGet, "/public/portal/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildRouter_UserSecurityRouteRequiresSecurityPermission(t *testing.T) {
	router := BuildRouter(Dependencies{
		Config: testRouterConfig(),
	})

	t.Run("agent token without security permission rejected on revoke", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        7,
			"principal_kind": "agent",
			"roles":          []string{"agent"},
		}, "test-secret")

		req := httptest.NewRequest(http.MethodPost, "/api/security/users/1/revoke-tokens", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("agent token without security permission rejected on read", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        7,
			"principal_kind": "agent",
			"roles":          []string{"agent"},
		}, "test-secret")

		req := httptest.NewRequest(http.MethodGet, "/api/security/users/1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("admin token passes authz layer on revoke", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        1,
			"principal_kind": "admin",
			"roles":          []string{"admin"},
		}, "test-secret")

		req := httptest.NewRequest(http.MethodPost, "/api/security/users/1/revoke-tokens", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound && w.Code != http.StatusOK {
			t.Fatalf("expected authz to pass and reach handler, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("admin token passes authz layer on read", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        1,
			"principal_kind": "admin",
			"roles":          []string{"admin"},
		}, "test-secret")

		req := httptest.NewRequest(http.MethodGet, "/api/security/users/1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound && w.Code != http.StatusOK {
			t.Fatalf("expected authz to pass and reach handler, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestBuildRouter_VoiceRoutesRequireVoicePermission(t *testing.T) {
	router := BuildRouter(Dependencies{
		Config: testRouterConfig(),
	})

	t.Run("assist permission alone is rejected", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        7,
			"principal_kind": "agent",
			"permissions":    []string{"assist.read"},
		}, "test-secret")

		req := httptest.NewRequest(http.MethodGet, "/api/voice/protocols", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("voice permission reaches handler", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        7,
			"principal_kind": "agent",
			"permissions":    []string{"voice.read"},
		}, "test-secret")

		req := httptest.NewRequest(http.MethodGet, "/api/voice/protocols", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected authz to pass and reach handler, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestBuildRouter_AuthSessionsUsesConfiguredSessionIPIntelligence(t *testing.T) {
	db := newRouterAuthTestDB(t)
	now := time.Now().UTC()
	if err := db.Create(&models.User{
		ID:       7,
		Username: "router-user",
		Email:    "router-user@example.com",
		Role:     "admin",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{
		ID:                "sess-router",
		UserID:            7,
		Status:            "active",
		TokenVersion:      1,
		DeviceFingerprint: "fp-router",
		UserAgent:         "router-browser/1.0",
		ClientIP:          "8.8.8.8",
		LastSeenAt:        &now,
		LastRefreshedAt:   &now,
	}).Error; err != nil {
		t.Fatalf("seed auth session: %v", err)
	}

	ipSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lookup/8.8.8.8" {
			t.Fatalf("unexpected lookup path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"network_label":"public","location_label":"geo:test-router"}`))
	}))
	defer ipSrv.Close()

	cfg := testRouterConfig()
	cfg.Security.SessionIPIntelligence.Enabled = true
	cfg.Security.SessionIPIntelligence.BaseURL = ipSrv.URL + "/lookup/{ip}"
	cfg.Security.SessionIPIntelligence.TimeoutMs = 1000

	router := BuildRouter(Dependencies{
		Config: cfg,
		DB:     db,
	})

	token := createTestHS256JWT(t, map[string]interface{}{
		"user_id":               7,
		"principal_kind":        "admin",
		"roles":                 []string{"admin"},
		"session_id":            "sess-router",
		"session_token_version": 1,
	}, "test-secret")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"location_label":"geo:test-router"`) {
		t.Fatalf("expected configured IP intelligence output, body=%s", w.Body.String())
	}
}
