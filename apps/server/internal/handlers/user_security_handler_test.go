//go:build integration
// +build integration

package handlers

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

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	auditplatform "servify/apps/server/internal/platform/audit"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/configscope"
	"servify/apps/server/internal/platform/usersecurity"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newTestDBForUserSecurity(t *testing.T) *gorm.DB {
	t.Helper()

	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := uniqueMemDSN("file:user_security_handler_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(&models.User{}, &models.Agent{}, &models.Customer{}, &models.UserAuthSession{}, &models.RevokedToken{}); err != nil {
		t.Fatalf("automigrate user/auth session/revoked token: %v", err)
	}

	return db
}

func TestUserSecurityHandler_GetAndRevokeTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForUserSecurity(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	lastLogin := time.Now().UTC().Add(-15 * time.Minute).Round(time.Second)
	if err := db.Create(&models.User{
		ID:           21,
		Username:     "security-user",
		Email:        "security-user@example.com",
		Name:         "Security User",
		Role:         "admin",
		Status:       "active",
		LastLogin:    &lastLogin,
		TokenVersion: 0,
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	h := NewUserSecurityHandler(usersecurity.NewService(db, logger), logger)
	r := gin.New()
	r.GET("/api/security/users/:id", h.GetUserSecurity)
	r.POST("/api/security/users/:id/revoke-tokens", h.RevokeTokens)

	t.Run("get security state", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/security/users/21", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}

		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal response: %v body=%s", err, w.Body.String())
		}
		if got := int(body["user_id"].(float64)); got != 21 {
			t.Fatalf("user_id = %d want 21", got)
		}
		if got := body["role"].(string); got != "admin" {
			t.Fatalf("role = %q want admin", got)
		}
		if got := int(body["token_version"].(float64)); got != 0 {
			t.Fatalf("token_version = %d want 0", got)
		}
	})

	t.Run("revoke tokens increments version", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/security/users/21/revoke-tokens", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}

		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal response: %v body=%s", err, w.Body.String())
		}
		if got := int(body["token_version"].(float64)); got != 1 {
			t.Fatalf("token_version = %d want 1", got)
		}

		var user models.User
		if err := db.First(&user, 21).Error; err != nil {
			t.Fatalf("reload user: %v", err)
		}
		if user.TokenVersion != 1 {
			t.Fatalf("stored token_version = %d want 1", user.TokenVersion)
		}
		if user.TokenValidAfter == nil || user.TokenValidAfter.IsZero() {
			t.Fatalf("expected token_valid_after to be set")
		}
	})
}

func TestUserSecurityHandler_InvalidUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	h := NewUserSecurityHandler(nil, logger)
	r := gin.New()
	r.GET("/api/security/users/:id", h.GetUserSecurity)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/security/users/not-a-number", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestUserSecurityHandler_RevokeTokensWritesAuditSnapshots(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForUserSecurity(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{
		ID:           31,
		Username:     "audit-user",
		Email:        "audit-user@example.com",
		Name:         "Audit User",
		Role:         "admin",
		Status:       "active",
		TokenVersion: 2,
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	recorder := &ticketAuditRecorder{}
	h := NewUserSecurityHandler(usersecurity.NewService(db, logger), logger)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(99))
		c.Set("principal_kind", "admin")
		c.Next()
	})
	r.Use(auditplatform.Middleware(recorder))
	r.POST("/api/security/users/:id/revoke-tokens", h.RevokeTokens)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/security/users/31/revoke-tokens", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if len(recorder.entries) != 1 {
		t.Fatalf("expected 1 audit entry got %d", len(recorder.entries))
	}
	entry := recorder.entries[0]
	if entry.BeforeJSON == "" || entry.AfterJSON == "" {
		t.Fatalf("expected before/after snapshots, got before=%q after=%q", entry.BeforeJSON, entry.AfterJSON)
	}
	if !strings.Contains(entry.BeforeJSON, `"token_version":2`) {
		t.Fatalf("unexpected before snapshot: %s", entry.BeforeJSON)
	}
	if !strings.Contains(entry.AfterJSON, `"token_version":3`) {
		t.Fatalf("unexpected after snapshot: %s", entry.AfterJSON)
	}
	if !strings.Contains(entry.AfterJSON, `"user_id":31`) {
		t.Fatalf("unexpected after snapshot user id: %s", entry.AfterJSON)
	}
}

func TestUserSecurityHandler_BatchRevokeTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForUserSecurity(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	users := []models.User{
		{ID: 41, Username: "batch-1", Email: "batch-1@example.com", Name: "Batch 1", Role: "admin", Status: "active", TokenVersion: 1},
		{ID: 42, Username: "batch-2", Email: "batch-2@example.com", Name: "Batch 2", Role: "agent", Status: "active", TokenVersion: 3},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}

	recorder := &ticketAuditRecorder{}
	h := NewUserSecurityHandler(usersecurity.NewService(db, logger), logger)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(99))
		c.Set("principal_kind", "admin")
		c.Next()
	})
	r.Use(auditplatform.Middleware(recorder))
	r.POST("/api/security/users/revoke-tokens", h.BatchRevokeTokens)

	body := strings.NewReader(`{"user_ids":[41,42]}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/security/users/revoke-tokens", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, w.Body.String())
	}
	if got := int(resp["count"].(float64)); got != 2 {
		t.Fatalf("count = %d want 2", got)
	}

	var updated []models.User
	if err := db.Order("id asc").Find(&updated, []uint{41, 42}).Error; err != nil {
		t.Fatalf("reload users: %v", err)
	}
	if updated[0].TokenVersion != 2 || updated[1].TokenVersion != 4 {
		t.Fatalf("unexpected token versions: %+v", updated)
	}

	if len(recorder.entries) != 1 {
		t.Fatalf("expected 1 audit entry got %d", len(recorder.entries))
	}
	entry := recorder.entries[0]
	if !strings.Contains(entry.BeforeJSON, `"user_id":41`) || !strings.Contains(entry.BeforeJSON, `"token_version":1`) {
		t.Fatalf("unexpected before snapshot: %s", entry.BeforeJSON)
	}
	if !strings.Contains(entry.AfterJSON, `"user_id":42`) || !strings.Contains(entry.AfterJSON, `"token_version":4`) {
		t.Fatalf("unexpected after snapshot: %s", entry.AfterJSON)
	}
}

func TestUserSecurityHandler_QueryUsersSecurity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForUserSecurity(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	validAfter := time.Now().UTC().Add(-2 * time.Hour).Round(time.Second)
	lastLogin := time.Now().UTC().Add(-30 * time.Minute).Round(time.Second)
	users := []models.User{
		{ID: 51, Username: "query-1", Email: "query-1@example.com", Name: "Query 1", Role: "admin", Status: "active", TokenVersion: 2, LastLogin: &lastLogin, TokenValidAfter: &validAfter},
		{ID: 52, Username: "query-2", Email: "query-2@example.com", Name: "Query 2", Role: "agent", Status: "inactive", TokenVersion: 0},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}

	h := NewUserSecurityHandler(usersecurity.NewService(db, logger), logger)
	r := gin.New()
	r.POST("/api/security/users/query", h.QueryUsersSecurity)

	t.Run("returns ordered preview items", func(t *testing.T) {
		body := strings.NewReader(`{"user_ids":[52,51,52]}`)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/security/users/query", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}

		var resp struct {
			Count int `json:"count"`
			Items []struct {
				UserID           int    `json:"user_id"`
				Username         string `json:"username"`
				Status           string `json:"status"`
				TokenVersion     int    `json:"token_version"`
				NextTokenVersion int    `json:"next_token_version"`
			} `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v body=%s", err, w.Body.String())
		}
		if resp.Count != 3 {
			t.Fatalf("count = %d want 3", resp.Count)
		}
		if len(resp.Items) != 3 {
			t.Fatalf("len(items) = %d want 3", len(resp.Items))
		}
		if resp.Items[0].UserID != 52 || resp.Items[1].UserID != 51 || resp.Items[2].UserID != 52 {
			t.Fatalf("unexpected item order: %+v", resp.Items)
		}
		if resp.Items[0].TokenVersion != 0 || resp.Items[0].NextTokenVersion != 1 {
			t.Fatalf("unexpected preview for first item: %+v", resp.Items[0])
		}
		if resp.Items[1].Username != "query-1" || resp.Items[1].Status != "active" {
			t.Fatalf("unexpected second item: %+v", resp.Items[1])
		}
	})

	t.Run("returns not found when any user missing", func(t *testing.T) {
		body := strings.NewReader(`{"user_ids":[51,999]}`)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/security/users/query", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 got %d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestUserSecurityHandler_ListAndRevokeSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForUserSecurity(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{
		ID:       61,
		Username: "session-user",
		Email:    "session-user@example.com",
		Name:     "Session User",
		Role:     "admin",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{
		ID:                "auth-session-61",
		UserID:            61,
		Status:            "active",
		TokenVersion:      1,
		DeviceFingerprint: "fp-61",
		UserAgent:         "servify-browser/1.0",
		ClientIP:          "203.0.113.61",
		LastSeenAt:        ptrTime(time.Now().UTC().Add(-2 * time.Hour)),
		LastRefreshedAt:   ptrTime(time.Now().UTC().Add(-10 * time.Minute)),
	}).Error; err != nil {
		t.Fatalf("seed auth session: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{
		ID:                "auth-session-61-b",
		UserID:            61,
		Status:            "active",
		TokenVersion:      2,
		DeviceFingerprint: "fp-61-b",
		UserAgent:         "servify-browser/2.0",
		ClientIP:          "203.0.113.62",
		LastSeenAt:        ptrTime(time.Now().UTC().Add(-1 * time.Hour)),
		LastRefreshedAt:   ptrTime(time.Now().UTC().Add(-5 * time.Minute)),
	}).Error; err != nil {
		t.Fatalf("seed auth session: %v", err)
	}

	recorder := &ticketAuditRecorder{}
	h := NewUserSecurityHandler(usersecurity.NewService(db, logger), logger)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(99))
		c.Set("principal_kind", "admin")
		c.Next()
	})
	r.Use(auditplatform.Middleware(recorder))
	r.GET("/api/security/users/:id/sessions", h.ListUserSessions)
	r.POST("/api/security/users/:id/sessions/revoke", h.RevokeSession)

	t.Run("list sessions", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/security/users/61/sessions", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"session_id":"auth-session-61"`) {
			t.Fatalf("unexpected body: %s", w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"device_fingerprint":"fp-61"`) || !strings.Contains(w.Body.String(), `"network_label":"public"`) || !strings.Contains(w.Body.String(), `"location_label":"documentation"`) || !strings.Contains(w.Body.String(), `"family_public_ip_count":2`) || !strings.Contains(w.Body.String(), `"active_session_count":2`) || !strings.Contains(w.Body.String(), `"family_hot_refresh_count":2`) || !strings.Contains(w.Body.String(), `"reference_session_id":"auth-session-61-b"`) || !strings.Contains(w.Body.String(), `"ip_drift":true`) || !strings.Contains(w.Body.String(), `"device_drift":true`) || !strings.Contains(w.Body.String(), `"rapid_ip_change":true`) || !strings.Contains(w.Body.String(), `"rapid_device_change":true`) || !strings.Contains(w.Body.String(), `"refresh_recency":"hot"`) || !strings.Contains(w.Body.String(), `"rapid_refresh_activity":true`) || !strings.Contains(w.Body.String(), `"risk_score":9`) || !strings.Contains(w.Body.String(), `"risk_level":"high"`) || !strings.Contains(w.Body.String(), `"user_agent":"servify-browser/1.0"`) || !strings.Contains(w.Body.String(), `"client_ip":"203.0.113.61"`) {
			t.Fatalf("expected session metadata in body: %s", w.Body.String())
		}
	})

	t.Run("revoke session writes audit snapshots", func(t *testing.T) {
		body := strings.NewReader(`{"session_id":"auth-session-61"}`)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/security/users/61/sessions/revoke", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"status":"revoked"`) {
			t.Fatalf("unexpected body: %s", w.Body.String())
		}

		var session models.UserAuthSession
		if err := db.First(&session, "id = ?", "auth-session-61").Error; err != nil {
			t.Fatalf("reload session: %v", err)
		}
		if session.Status != "revoked" || session.TokenVersion != 2 {
			t.Fatalf("unexpected session state: %+v", session)
		}

		if len(recorder.entries) == 0 {
			t.Fatalf("expected audit entries")
		}
		entry := recorder.entries[len(recorder.entries)-1]
		if !strings.Contains(entry.BeforeJSON, `"session_id":"auth-session-61"`) {
			t.Fatalf("unexpected before snapshot: %s", entry.BeforeJSON)
		}
		if !strings.Contains(entry.AfterJSON, `"status":"revoked"`) {
			t.Fatalf("unexpected after snapshot: %s", entry.AfterJSON)
		}
	})
}

func TestUserSecurityHandler_ListUserSessionsUsesScopedRiskPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForUserSecurity(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.AutoMigrate(&models.TenantConfig{}); err != nil {
		t.Fatalf("automigrate tenant config: %v", err)
	}
	if err := db.Create(&models.TenantConfig{
		TenantID:        "tenant-a",
		SessionRiskJSON: "high_risk_score: 10\n",
	}).Error; err != nil {
		t.Fatalf("seed tenant config: %v", err)
	}
	if err := db.Create(&models.User{
		ID:       71,
		Username: "scoped-session-user",
		Email:    "scoped-session-user@example.com",
		Name:     "Scoped Session User",
		Role:     "admin",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.Agent{
		UserID:     71,
		TenantID:   "tenant-a",
		Department: "security",
		Status:     "online",
	}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{
		ID:                "auth-session-71",
		UserID:            71,
		Status:            "active",
		TokenVersion:      1,
		DeviceFingerprint: "fp-71",
		UserAgent:         "servify-browser/1.0",
		ClientIP:          "203.0.113.71",
		LastSeenAt:        ptrTime(time.Now().UTC().Add(-2 * time.Hour)),
		LastRefreshedAt:   ptrTime(time.Now().UTC().Add(-10 * time.Minute)),
	}).Error; err != nil {
		t.Fatalf("seed auth session: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{
		ID:                "auth-session-71-b",
		UserID:            71,
		Status:            "active",
		TokenVersion:      2,
		DeviceFingerprint: "fp-71-b",
		UserAgent:         "servify-browser/2.0",
		ClientIP:          "203.0.113.72",
		LastSeenAt:        ptrTime(time.Now().UTC().Add(-1 * time.Hour)),
		LastRefreshedAt:   ptrTime(time.Now().UTC().Add(-5 * time.Minute)),
	}).Error; err != nil {
		t.Fatalf("seed auth session: %v", err)
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
		configscope.WithTenantSessionRiskProvider(configscope.NewGormTenantConfigProvider(db)),
	)
	h := NewUserSecurityHandler(usersecurity.NewService(db, logger), logger).WithSessionRiskResolver(resolver)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(platformauth.ContextWithScope(c.Request.Context(), "tenant-a", ""))
		c.Next()
	})
	r.GET("/api/security/users/:id/sessions", h.ListUserSessions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/security/users/71/sessions", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"risk_level":"medium"`) {
		t.Fatalf("expected scoped risk policy to downgrade high threshold: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"risk_level":"high"`) {
		t.Fatalf("expected no high risk level after scoped override: %s", w.Body.String())
	}
}

func TestUserSecurityHandler_ListUserSessionsUsesEnvironmentRiskProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForUserSecurity(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{
		ID:       72,
		Username: "env-session-user",
		Email:    "env-session-user@example.com",
		Name:     "Env Session User",
		Role:     "admin",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{
		ID:                "auth-session-72",
		UserID:            72,
		Status:            "active",
		TokenVersion:      1,
		DeviceFingerprint: "fp-72",
		UserAgent:         "servify-browser/1.0",
		ClientIP:          "203.0.113.81",
		LastSeenAt:        ptrTime(time.Now().UTC().Add(-2 * time.Hour)),
		LastRefreshedAt:   ptrTime(time.Now().UTC().Add(-10 * time.Minute)),
	}).Error; err != nil {
		t.Fatalf("seed auth session: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{
		ID:                "auth-session-72-b",
		UserID:            72,
		Status:            "active",
		TokenVersion:      2,
		DeviceFingerprint: "fp-72-b",
		UserAgent:         "servify-browser/2.0",
		ClientIP:          "203.0.113.82",
		LastSeenAt:        ptrTime(time.Now().UTC().Add(-1 * time.Hour)),
		LastRefreshedAt:   ptrTime(time.Now().UTC().Add(-5 * time.Minute)),
	}).Error; err != nil {
		t.Fatalf("seed auth session: %v", err)
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
	h := NewUserSecurityHandler(usersecurity.NewService(db, logger), logger).WithSessionRiskResolver(resolver)
	r := gin.New()
	r.GET("/api/security/users/:id/sessions", h.ListUserSessions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/security/users/72/sessions", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"risk_level":"medium"`) {
		t.Fatalf("expected environment risk profile to downgrade high threshold: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"risk_level":"high"`) {
		t.Fatalf("expected no high risk level after environment profile: %s", w.Body.String())
	}
}

func TestUserSecurityHandler_ListUserSessionsUsesInjectedIPIntelligence(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForUserSecurity(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{
		ID:       73,
		Username: "intel-session-user",
		Email:    "intel-session-user@example.com",
		Name:     "Intel Session User",
		Role:     "admin",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.UserAuthSession{
		ID:                "auth-session-73",
		UserID:            73,
		Status:            "active",
		TokenVersion:      1,
		DeviceFingerprint: "fp-73",
		UserAgent:         "servify-browser/1.0",
		ClientIP:          "8.8.8.8",
		LastSeenAt:        ptrTime(time.Now().UTC().Add(-10 * time.Minute)),
	}).Error; err != nil {
		t.Fatalf("seed auth session: %v", err)
	}

	h := NewUserSecurityHandler(usersecurity.NewService(db, logger), logger).WithSessionIPIntelligence(stubSessionIPIntelligence{
		desc: sessionIPDescription{
			NetworkLabel:  "public",
			LocationLabel: "geo:cn-zj",
		},
	})
	r := gin.New()
	r.GET("/api/security/users/:id/sessions", h.ListUserSessions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/security/users/73/sessions", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"network_label":"public"`) {
		t.Fatalf("expected injected network label body=%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"location_label":"geo:cn-zj"`) {
		t.Fatalf("expected injected location label body=%s", w.Body.String())
	}
}

func TestUserSecurityHandler_RevokeAllSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForUserSecurity(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{
		ID:       62,
		Username: "session-family-user",
		Email:    "session-family-user@example.com",
		Name:     "Session Family User",
		Role:     "admin",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create([]models.UserAuthSession{
		{ID: "auth-session-62-a", UserID: 62, Status: "active", TokenVersion: 1},
		{ID: "auth-session-62-b", UserID: 62, Status: "active", TokenVersion: 4},
	}).Error; err != nil {
		t.Fatalf("seed auth sessions: %v", err)
	}

	recorder := &ticketAuditRecorder{}
	h := NewUserSecurityHandler(usersecurity.NewService(db, logger), logger)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(99))
		c.Set("principal_kind", "admin")
		c.Next()
	})
	r.Use(auditplatform.Middleware(recorder))
	r.POST("/api/security/users/:id/sessions/revoke-all", h.RevokeAllSessions)

	body := strings.NewReader(`{"except_session_id":"auth-session-62-b","reason":"logout-other-devices"}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/security/users/62/sessions/revoke-all", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"count":1`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"except_session_id":"auth-session-62-b"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}

	var sessions []models.UserAuthSession
	if err := db.Order("id asc").Find(&sessions, "user_id = ?", 62).Error; err != nil {
		t.Fatalf("reload sessions: %v", err)
	}
	if sessions[0].Status != "revoked" || sessions[0].TokenVersion != 2 {
		t.Fatalf("unexpected first session state: %+v", sessions[0])
	}
	if sessions[1].Status != "active" || sessions[1].TokenVersion != 4 {
		t.Fatalf("unexpected second session state: %+v", sessions[1])
	}

	if len(recorder.entries) == 0 {
		t.Fatalf("expected audit entries")
	}
	entry := recorder.entries[len(recorder.entries)-1]
	if !strings.Contains(entry.BeforeJSON, `"session_id":"auth-session-62-a"`) {
		t.Fatalf("unexpected before snapshot: %s", entry.BeforeJSON)
	}
	if strings.Contains(entry.BeforeJSON, `"session_id":"auth-session-62-b"`) {
		t.Fatalf("excepted session should not be in before snapshot: %s", entry.BeforeJSON)
	}
	if !strings.Contains(entry.AfterJSON, `"status":"revoked"`) {
		t.Fatalf("unexpected after snapshot: %s", entry.AfterJSON)
	}
}

func TestUserSecurityHandler_RejectsCrossScopeUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForUserSecurity(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.AutoMigrate(&models.Agent{}, &models.Customer{}); err != nil {
		t.Fatalf("automigrate scoped user tables: %v", err)
	}
	if err := db.Create([]models.User{
		{ID: 81, Username: "scope-agent", Email: "scope-agent@example.com", Name: "Scope Agent", Role: "agent", Status: "active"},
		{ID: 82, Username: "scope-customer", Email: "scope-customer@example.com", Name: "Scope Customer", Role: "customer", Status: "active"},
	}).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: 81, TenantID: "tenant-a", WorkspaceID: "workspace-a"}).Error; err != nil {
		t.Fatalf("seed scoped agent: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: 82, TenantID: "tenant-b", WorkspaceID: "workspace-b"}).Error; err != nil {
		t.Fatalf("seed cross-scope customer: %v", err)
	}

	h := NewUserSecurityHandler(usersecurity.NewService(db, logger), logger)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(platformauth.ContextWithScope(c.Request.Context(), "tenant-a", "workspace-a"))
		c.Next()
	})
	r.GET("/api/security/users/:id", h.GetUserSecurity)
	r.POST("/api/security/users/revoke-tokens", h.BatchRevokeTokens)

	t.Run("single user lookup returns 404 for cross scope target", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/security/users/82", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("batch revoke rejects mixed scope targets", func(t *testing.T) {
		body := strings.NewReader(`{"user_ids":[81,82]}`)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/security/users/revoke-tokens", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 got %d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestUserSecurityHandler_RevokeToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForUserSecurity(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	recorder := &ticketAuditRecorder{}
	h := NewUserSecurityHandler(usersecurity.NewService(db, logger), logger).WithJWTSecret("test-secret")
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(99))
		c.Set("principal_kind", "admin")
		c.Next()
	})
	r.Use(auditplatform.Middleware(recorder))
	r.POST("/api/security/tokens/revoke", h.RevokeToken)

	now := time.Now().UTC()
	token := createTestHS256JWT(t, map[string]interface{}{
		"jti":        "jti-handler-1",
		"user_id":    88,
		"session_id": "auth-session-88",
		"token_use":  "refresh",
		"iat":        now.Unix(),
		"exp":        now.Add(15 * time.Minute).Unix(),
	}, "test-secret")

	body := strings.NewReader(`{"token":"` + token + `","reason":"incident-response"}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/security/tokens/revoke", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"jti":"jti-handler-1"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}

	var revoked models.RevokedToken
	if err := db.First(&revoked, "jti = ?", "jti-handler-1").Error; err != nil {
		t.Fatalf("load revoked token: %v", err)
	}
	if revoked.TokenUse != "refresh" || revoked.UserID != 88 {
		t.Fatalf("unexpected revoked token: %+v", revoked)
	}
	if len(recorder.entries) == 0 {
		t.Fatalf("expected audit entries")
	}
	entry := recorder.entries[len(recorder.entries)-1]
	if !strings.Contains(entry.AfterJSON, `"jti":"jti-handler-1"`) {
		t.Fatalf("unexpected audit snapshot: %s", entry.AfterJSON)
	}
}

func TestUserSecurityHandler_ListRevokedTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForUserSecurity(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	now := time.Now().UTC()
	expiredAt := now.Add(-1 * time.Hour)
	activeAt := now.Add(2 * time.Hour)
	records := []models.RevokedToken{
		{JTI: "jti-list-1", UserID: 71, SessionID: "sess-71", TokenUse: "access", Reason: "manual", ExpiresAt: &activeAt, RevokedAt: now.Add(-2 * time.Minute)},
		{JTI: "jti-list-2", UserID: 72, SessionID: "sess-72", TokenUse: "refresh", Reason: "expired", ExpiresAt: &expiredAt, RevokedAt: now.Add(-3 * time.Minute)},
	}
	if err := db.Create(&records).Error; err != nil {
		t.Fatalf("seed revoked tokens: %v", err)
	}

	h := NewUserSecurityHandler(usersecurity.NewService(db, logger), logger)
	r := gin.New()
	r.GET("/api/security/tokens/revoked", h.ListRevokedTokens)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/security/tokens/revoked?active_only=true", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"jti":"jti-list-1"`) {
		t.Fatalf("expected active token in body: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"jti":"jti-list-2"`) {
		t.Fatalf("did not expect expired token in body: %s", w.Body.String())
	}
}

func TestUserSecurityHandler_TokenSurfaceHonorsScope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForUserSecurity(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.AutoMigrate(&models.Agent{}, &models.Customer{}); err != nil {
		t.Fatalf("automigrate scoped user tables: %v", err)
	}
	if err := db.Create([]models.User{
		{ID: 91, Username: "token-scope-agent", Email: "token-scope-agent@example.com", Name: "Token Scope Agent", Role: "agent", Status: "active"},
		{ID: 92, Username: "token-scope-customer", Email: "token-scope-customer@example.com", Name: "Token Scope Customer", Role: "customer", Status: "active"},
	}).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: 91, TenantID: "tenant-a", WorkspaceID: "workspace-a"}).Error; err != nil {
		t.Fatalf("seed scoped agent: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: 92, TenantID: "tenant-b", WorkspaceID: "workspace-b"}).Error; err != nil {
		t.Fatalf("seed cross-scope customer: %v", err)
	}

	now := time.Now().UTC()
	expiry := now.Add(30 * time.Minute)
	if err := db.Create([]models.RevokedToken{
		{JTI: "jti-scope-list-91", UserID: 91, SessionID: "sess-91", TokenUse: "access", ExpiresAt: &expiry, RevokedAt: now},
		{JTI: "jti-scope-list-92", UserID: 92, SessionID: "sess-92", TokenUse: "access", ExpiresAt: &expiry, RevokedAt: now},
	}).Error; err != nil {
		t.Fatalf("seed revoked tokens: %v", err)
	}

	h := NewUserSecurityHandler(usersecurity.NewService(db, logger), logger).WithJWTSecret("test-secret")
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(platformauth.ContextWithScope(c.Request.Context(), "tenant-a", "workspace-a"))
		c.Next()
	})
	r.GET("/api/security/tokens/revoked", h.ListRevokedTokens)
	r.POST("/api/security/tokens/revoke", h.RevokeToken)

	t.Run("revoked token list is scope filtered", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/security/tokens/revoked", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"jti":"jti-scope-list-91"`) {
			t.Fatalf("expected scoped token in body: %s", w.Body.String())
		}
		if strings.Contains(w.Body.String(), `"jti":"jti-scope-list-92"`) {
			t.Fatalf("did not expect cross-scope token in body: %s", w.Body.String())
		}
	})

	t.Run("token revoke returns 404 for cross scope target", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"jti":        "jti-token-scope-92",
			"user_id":    92,
			"session_id": "auth-92",
			"token_use":  "refresh",
			"iat":        now.Unix(),
			"exp":        expiry.Unix(),
		}, "test-secret")
		body := strings.NewReader(`{"token":"` + token + `"}`)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/security/tokens/revoke", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 got %d body=%s", w.Code, w.Body.String())
		}
	})
}

func createTestHS256JWT(t *testing.T, payload map[string]interface{}, secret string) string {
	t.Helper()

	headerJSON, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	enc := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	unsigned := enc(headerJSON) + "." + enc(payloadJSON)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsigned))
	return unsigned + "." + enc(mac.Sum(nil))
}
