package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/configscope"
	"servify/apps/server/internal/platform/usersecurity"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func axcNewSecurityDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := uniqueMemDSN("file:axc_user_security_" + name + "")
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
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func axcSecurityLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	return logger
}

func axcFailQueriesWithDest(db *gorm.DB, dest any) {
	target := reflect.TypeOf(dest)
	db.Callback().Query().Before("gorm:query").Register("axc:fail_dest_query", func(tx *gorm.DB) {
		if tx.Statement.Dest == nil || reflect.TypeOf(tx.Statement.Dest) != target {
			return
		}
		_ = tx.AddError(errors.New("axc injected query failure"))
	})
}

func axcFailNthUserQuery(db *gorm.DB, n int) {
	db.Callback().Query().Before("gorm:query").Register("axc:fail_nth_user_query", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*models.User); !ok {
			return
		}
		n--
		if n == 0 {
			_ = tx.AddError(errors.New("axc injected nth user query failure"))
		}
	})
}

func axcFailUpdatesOnModel(db *gorm.DB, model any) {
	target := reflect.TypeOf(model)
	db.Callback().Update().Before("gorm:update").Register("axc:fail_model_update", func(tx *gorm.DB) {
		if tx.Statement.Model == nil || reflect.TypeOf(tx.Statement.Model) != target {
			return
		}
		_ = tx.AddError(errors.New("axc injected update failure"))
	})
}

func axcClosedSecurityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := axcNewSecurityDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	return db
}

func axcMakeJWT(t *testing.T, payload map[string]interface{}, secret string) string {
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

func axcSecurityRouter(h *UserSecurityHandler) *gin.Engine {
	r := gin.New()
	r.GET("/api/security/users/:id", h.GetUserSecurity)
	r.GET("/api/security/users/:id/sessions", h.ListUserSessions)
	r.POST("/api/security/users/:id/sessions/revoke", h.RevokeSession)
	r.POST("/api/security/users/:id/sessions/revoke-all", h.RevokeAllSessions)
	r.POST("/api/security/users/:id/revoke-tokens", h.RevokeTokens)
	r.POST("/api/security/users/query", h.QueryUsersSecurity)
	r.POST("/api/security/users/revoke-tokens", h.BatchRevokeTokens)
	r.POST("/api/security/tokens/revoke", h.RevokeToken)
	r.GET("/api/security/tokens/revoked", h.ListRevokedTokens)
	return r
}

func axcSecurityDo(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAxcUserSecurityConstructors(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("nil logger falls back to standard", func(t *testing.T) {
		h := NewUserSecurityHandler(nil, nil)
		if h == nil || h.logger != logrus.StandardLogger() {
			t.Fatalf("expected standard logger, got %+v", h.logger)
		}
	})
	t.Run("with methods trim and store", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		h := NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger())
		h.WithJWTSecret("  sec  ")
		if h.jwtSecret != "sec" {
			t.Fatalf("jwt secret=%q", h.jwtSecret)
		}
		h.WithSessionIPIntelligence(axcFixedIPIntel{})
		if _, ok := h.ipIntel.(axcFixedIPIntel); !ok {
			t.Fatalf("expected injected ip intel, got %T", h.ipIntel)
		}
		h.WithSessionIPIntelligence(nil)
		if _, ok := h.ipIntel.(axcFixedIPIntel); !ok {
			t.Fatalf("nil provider should be ignored, got %T", h.ipIntel)
		}
	})
	t.Run("nil receiver withers", func(t *testing.T) {
		var h *UserSecurityHandler
		if h.WithJWTSecret("x") != nil || h.WithSessionRiskResolver(nil) != nil ||
			h.WithSessionIPIntelligence(nil) != nil {
			t.Fatal("expected nil handlers")
		}
	})
	t.Run("session risk policy resolution", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		var nilHandler *UserSecurityHandler
		if got := nilHandler.sessionRiskPolicy(context.Background()); got != defaultSessionRiskPolicy() {
			t.Fatalf("expected default policy, got %+v", got)
		}
		h := NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger())
		resolver := configscope.NewResolver(&config.Config{Security: config.SecurityConfig{SessionRisk: config.SessionRiskPolicyConfig{MediumRiskScore: 5}}})
		h.WithSessionRiskResolver(resolver)
		if got := h.sessionRiskPolicy(context.Background()); got.MediumRiskScore != 5 {
			t.Fatalf("expected resolver policy, got %+v", got)
		}
	})
	t.Run("register routes", func(t *testing.T) {
		RegisterUserSecurityRoutes(&gin.RouterGroup{}, nil)

		db := axcNewSecurityDB(t)
		h := NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger())
		r := gin.New()
		RegisterUserSecurityRoutes(&r.RouterGroup, h)
		w := axcSecurityDo(r, http.MethodGet, "/security/users/3", "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestAxcUserSecurityGetUserSecurity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("invalid id", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodGet, "/api/security/users/abc", ""); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("nil service", func(t *testing.T) {
		r := axcSecurityRouter(NewUserSecurityHandler(nil, axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodGet, "/api/security/users/5", ""); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("user missing", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodGet, "/api/security/users/999", ""); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("success", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		lastLogin := time.Now().UTC().Add(-time.Hour).Round(time.Second)
		if err := db.Create(&models.User{ID: 101, Username: "axc-get", Email: "axc-get@example.com", Role: "admin", Status: "active", TokenVersion: 2, LastLogin: &lastLogin}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		w := axcSecurityDo(r, http.MethodGet, "/api/security/users/101", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		for _, want := range []string{`"user_id":101`, `"role":"admin"`, `"token_version":2`, `"last_login":"`} {
			if !strings.Contains(w.Body.String(), want) {
				t.Fatalf("body missing %q: %s", want, w.Body.String())
			}
		}
	})
}

func TestAxcUserSecurityRevokeTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("invalid id", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/x1/revoke-tokens", ""); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("nil service", func(t *testing.T) {
		r := axcSecurityRouter(NewUserSecurityHandler(nil, axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/5/revoke-tokens", ""); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("user missing", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/424242/revoke-tokens", ""); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("service revoke failure", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create(&models.User{ID: 111, Username: "axc-rv", Email: "axc-rv@example.com", Role: "agent", Status: "active", TokenVersion: 1}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		axcFailUpdatesOnModel(db, &models.User{})
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/111/revoke-tokens", ""); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("success writes snapshots", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create(&models.User{ID: 112, Username: "axc-rv2", Email: "axc-rv2@example.com", Role: "admin", Status: "active", TokenVersion: 4}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		var afterSnapshots []gin.H
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Next()
			if raw, ok := c.Get("audit.after"); ok {
				if typed, ok := raw.(gin.H); ok {
					afterSnapshots = append(afterSnapshots, typed)
				}
			}
		})
		r.POST("/api/security/users/:id/revoke-tokens", NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()).RevokeTokens)
		w := axcSecurityDo(r, http.MethodPost, "/api/security/users/112/revoke-tokens", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"token_version":5`) || !strings.Contains(w.Body.String(), `"role":"admin"`) {
			t.Fatalf("unexpected body: %s", w.Body.String())
		}
		if len(afterSnapshots) != 1 || afterSnapshots[0]["user_id"] != uint(112) {
			t.Fatalf("unexpected after snapshots: %+v", afterSnapshots)
		}
	})
	t.Run("after snapshot skipped on reload failure", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create(&models.User{ID: 113, Username: "axc-rv3", Email: "axc-rv3@example.com", Role: "admin", Status: "active", TokenVersion: 0}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		axcFailNthUserQuery(db, 3)
		afterSet := false
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Next()
			_, afterSet = c.Get("audit.after")
		})
		r.POST("/api/security/users/:id/revoke-tokens", NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()).RevokeTokens)
		w := axcSecurityDo(r, http.MethodPost, "/api/security/users/113/revoke-tokens", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if afterSet {
			t.Fatal("expected after snapshot to be skipped")
		}
	})
}

func TestAxcUserSecurityBatchRevokeTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("nil service", func(t *testing.T) {
		r := axcSecurityRouter(NewUserSecurityHandler(nil, axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/revoke-tokens", `{"user_ids":[1]}`); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("invalid body", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/revoke-tokens", "{}"); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/revoke-tokens", "not-json"); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("missing user in before loop", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create(&models.User{ID: 121, Username: "axc-b1", Email: "axc-b1@example.com", Role: "agent", Status: "active"}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/revoke-tokens", `{"user_ids":[121,999]}`); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("service batch failure", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create(&models.User{ID: 122, Username: "axc-b2", Email: "axc-b2@example.com", Role: "agent", Status: "active"}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		axcFailUpdatesOnModel(db, &models.User{})
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/revoke-tokens", `{"user_ids":[122]}`); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("success returns versions", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create([]models.User{
			{ID: 123, Username: "axc-b3", Email: "axc-b3@example.com", Role: "admin", Status: "active", TokenVersion: 1},
			{ID: 124, Username: "axc-b4", Email: "axc-b4@example.com", Role: "agent", Status: "active", TokenVersion: 6},
		}).Error; err != nil {
			t.Fatalf("seed users: %v", err)
		}
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		w := axcSecurityDo(r, http.MethodPost, "/api/security/users/revoke-tokens", `{"user_ids":[123,124]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, `"count":2`) || !strings.Contains(body, `"user_id":123`) || !strings.Contains(body, `"token_version":2`) || !strings.Contains(body, `"token_version":7`) {
			t.Fatalf("unexpected body: %s", body)
		}
	})
	t.Run("after snapshot skips users that fail reload", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create([]models.User{
			{ID: 125, Username: "axc-b5", Email: "axc-b5@example.com", Role: "admin", Status: "active"},
			{ID: 126, Username: "axc-b6", Email: "axc-b6@example.com", Role: "agent", Status: "active"},
		}).Error; err != nil {
			t.Fatalf("seed users: %v", err)
		}
		axcFailNthUserQuery(db, 5)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		w := axcSecurityDo(r, http.MethodPost, "/api/security/users/revoke-tokens", `{"user_ids":[125,126]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"count":2`) {
			t.Fatalf("unexpected body: %s", w.Body.String())
		}
	})
}

func TestAxcUserSecurityQueryUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("nil service", func(t *testing.T) {
		r := axcSecurityRouter(NewUserSecurityHandler(nil, axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/query", `{"user_ids":[1]}`); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("invalid body", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/query", "[]"); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("missing user", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/query", `{"user_ids":[777]}`); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("success", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create(&models.User{ID: 131, Username: "axc-q1", Email: "axc-q1@example.com", Name: "Q1", Role: "admin", Status: "active", TokenVersion: 3}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		w := axcSecurityDo(r, http.MethodPost, "/api/security/users/query", `{"user_ids":[131]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, `"count":1`) || !strings.Contains(body, `"username":"axc-q1"`) || !strings.Contains(body, `"token_version":3`) || !strings.Contains(body, `"next_token_version":4`) {
			t.Fatalf("unexpected body: %s", body)
		}
	})
}

func TestAxcUserSecurityListUserSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("invalid id", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodGet, "/api/security/users/nope/sessions", ""); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("nil service", func(t *testing.T) {
		r := axcSecurityRouter(NewUserSecurityHandler(nil, axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodGet, "/api/security/users/5/sessions", ""); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("user missing", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodGet, "/api/security/users/888/sessions", ""); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("list failure", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create(&models.User{ID: 141, Username: "axc-ls", Email: "axc-ls@example.com", Role: "admin", Status: "active"}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		axcFailQueriesWithDest(db, &[]models.UserAuthSession{})
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodGet, "/api/security/users/141/sessions", ""); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("success maps risk fields", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create(&models.User{ID: 142, Username: "axc-ls2", Email: "axc-ls2@example.com", Role: "admin", Status: "active"}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if err := db.Create(&models.UserAuthSession{
			ID: "axc-sess-142", UserID: 142, Status: "active", TokenVersion: 1,
			DeviceFingerprint: "fp-142", UserAgent: "ua-142", ClientIP: "10.1.2.3",
			LastSeenAt: ptrTime(time.Now().UTC()),
		}).Error; err != nil {
			t.Fatalf("seed session: %v", err)
		}
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		w := axcSecurityDo(r, http.MethodGet, "/api/security/users/142/sessions", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, `"session_id":"axc-sess-142"`) || !strings.Contains(body, `"user_id":142`) || !strings.Contains(body, `"network_label":"private"`) || !strings.Contains(body, `"is_current":false`) {
			t.Fatalf("unexpected body: %s", body)
		}
	})
}

func TestAxcUserSecurityRevokeSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("invalid id", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/zz/sessions/revoke", `{"session_id":"s"}`); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("nil service", func(t *testing.T) {
		r := axcSecurityRouter(NewUserSecurityHandler(nil, axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/5/sessions/revoke", `{"session_id":"s"}`); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("invalid body", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create(&models.User{ID: 151, Username: "axc-rs", Email: "axc-rs@example.com", Role: "admin", Status: "active"}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/151/sessions/revoke", "{}"); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("list sessions failure returns not found", func(t *testing.T) {
		db := axcClosedSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/152/sessions/revoke", `{"session_id":"s"}`); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("session missing returns not found", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create(&models.User{ID: 153, Username: "axc-rs2", Email: "axc-rs2@example.com", Role: "admin", Status: "active"}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/153/sessions/revoke", `{"session_id":"nope"}`); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("success writes before and after snapshots", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create(&models.User{ID: 154, Username: "axc-rs3", Email: "axc-rs3@example.com", Role: "admin", Status: "active"}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if err := db.Create(&models.UserAuthSession{ID: "axc-sess-154", UserID: 154, Status: "active", TokenVersion: 2}).Error; err != nil {
			t.Fatalf("seed session: %v", err)
		}
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		w := axcSecurityDo(r, http.MethodPost, "/api/security/users/154/sessions/revoke", `{"session_id":"axc-sess-154"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, `"status":"revoked"`) || !strings.Contains(body, `"token_version":3`) || !strings.Contains(body, `"session_id":"axc-sess-154"`) {
			t.Fatalf("unexpected body: %s", body)
		}
	})
}

func TestAxcUserSecurityRevokeAllSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	seed := func(t *testing.T, db *gorm.DB) {
		t.Helper()
		if err := db.Create(&models.User{ID: 161, Username: "axc-ra", Email: "axc-ra@example.com", Role: "admin", Status: "active"}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}
	t.Run("invalid id", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/1.5/sessions/revoke-all", ""); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("nil service", func(t *testing.T) {
		r := axcSecurityRouter(NewUserSecurityHandler(nil, axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/161/sessions/revoke-all", ""); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("user missing", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/616/sessions/revoke-all", ""); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("invalid body", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		seed(t, db)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/161/sessions/revoke-all", "not-json"); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("list sessions failure", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		seed(t, db)
		axcFailQueriesWithDest(db, &[]models.UserAuthSession{})
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/161/sessions/revoke-all", ""); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("service revoke failure", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		seed(t, db)
		if err := db.Create(&models.UserAuthSession{ID: "axc-sess-161", UserID: 161, Status: "active", TokenVersion: 1}).Error; err != nil {
			t.Fatalf("seed session: %v", err)
		}
		axcFailUpdatesOnModel(db, &models.UserAuthSession{})
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/users/161/sessions/revoke-all", ""); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("success with except and filtered snapshots", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		seed(t, db)
		revokedAt := time.Now().UTC().Add(-time.Hour)
		if err := db.Create([]models.UserAuthSession{
			{ID: "axc-keep", UserID: 161, Status: "active", TokenVersion: 5},
			{ID: "axc-revoke", UserID: 161, Status: "active", TokenVersion: 1},
			{ID: "axc-already", UserID: 161, Status: "revoked", TokenVersion: 1, RevokedAt: &revokedAt},
		}).Error; err != nil {
			t.Fatalf("seed sessions: %v", err)
		}
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		w := axcSecurityDo(r, http.MethodPost, "/api/security/users/161/sessions/revoke-all", `{"except_session_id":" axc-keep ","reason":"drill"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, `"count":1`) || !strings.Contains(body, `"except_session_id":"axc-keep"`) || !strings.Contains(body, `"session_id":"axc-revoke"`) {
			t.Fatalf("unexpected body: %s", body)
		}
		var kept models.UserAuthSession
		if err := db.First(&kept, "id = ?", "axc-keep").Error; err != nil {
			t.Fatalf("reload kept: %v", err)
		}
		if kept.Status != "active" {
			t.Fatalf("kept session revoked: %+v", kept)
		}
	})
	t.Run("success without body revokes everything", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		seed(t, db)
		if err := db.Create(&models.UserAuthSession{ID: "axc-revoke-b", UserID: 161, Status: "active", TokenVersion: 0}).Error; err != nil {
			t.Fatalf("seed session: %v", err)
		}
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		w := axcSecurityDo(r, http.MethodPost, "/api/security/users/161/sessions/revoke-all", "")
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"count":1`) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestAxcUserSecurityRevokeToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("nil service", func(t *testing.T) {
		r := axcSecurityRouter(NewUserSecurityHandler(nil, axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/tokens/revoke", `{"token":"t"}`); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("missing jwt secret", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/tokens/revoke", `{"token":"t"}`); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("invalid body", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		h := NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()).WithJWTSecret("axc-secret")
		r := axcSecurityRouter(h)
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/tokens/revoke", "{}"); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("malformed token returns bad request", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		h := NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()).WithJWTSecret("axc-secret")
		r := axcSecurityRouter(h)
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/tokens/revoke", `{"token":"not-a-jwt"}`); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("cross scope token returns not found", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		if err := db.Create(&models.User{ID: 171, Username: "axc-tok-a", Email: "a@example.com", Role: "agent", Status: "active"}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if err := db.Create(&models.Agent{UserID: 171, TenantID: "tenant-axc", WorkspaceID: "ws-axc"}).Error; err != nil {
			t.Fatalf("seed agent: %v", err)
		}
		if err := db.Create(&models.User{ID: 172, Username: "axc-tok-b", Email: "b@example.com", Role: "customer", Status: "active"}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if err := db.Create(&models.Customer{UserID: 172, TenantID: "tenant-other", WorkspaceID: "ws-other"}).Error; err != nil {
			t.Fatalf("seed customer: %v", err)
		}
		h := NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()).WithJWTSecret("axc-secret")
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(platformauth.ContextWithScope(c.Request.Context(), "tenant-axc", "ws-axc"))
			c.Next()
		})
		r.POST("/api/security/tokens/revoke", h.RevokeToken)
		now := time.Now().UTC()
		token := axcMakeJWT(t, map[string]interface{}{
			"jti": "axc-jti-cross", "user_id": 172, "session_id": "sess", "token_use": "refresh",
			"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		}, "axc-secret")
		if w := axcSecurityDo(r, http.MethodPost, "/api/security/tokens/revoke", `{"token":"`+token+`"}`); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("success persists revocation", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		h := NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()).WithJWTSecret("axc-secret")
		r := axcSecurityRouter(h)
		now := time.Now().UTC()
		token := axcMakeJWT(t, map[string]interface{}{
			"jti": "axc-jti-ok", "user_id": 173, "session_id": "axc-sess", "token_use": "access",
			"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		}, "axc-secret")
		w := axcSecurityDo(r, http.MethodPost, "/api/security/tokens/revoke", `{"token":"`+token+`","reason":" drill "}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, `"jti":"axc-jti-ok"`) || !strings.Contains(body, `"token_use":"access"`) || !strings.Contains(body, `"session_id":"axc-sess"`) {
			t.Fatalf("unexpected body: %s", body)
		}
		var record models.RevokedToken
		if err := db.First(&record, "jti = ?", "axc-jti-ok").Error; err != nil {
			t.Fatalf("load revoked token: %v", err)
		}
		if record.Reason != "drill" || record.UserID != 173 {
			t.Fatalf("unexpected record: %+v", record)
		}
	})
}

func TestAxcUserSecurityListRevokedTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("nil service", func(t *testing.T) {
		r := axcSecurityRouter(NewUserSecurityHandler(nil, axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodGet, "/api/security/tokens/revoked", ""); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("invalid user id filter", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodGet, "/api/security/tokens/revoked?user_id=abc", ""); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("list failure", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		axcFailQueriesWithDest(db, &[]models.RevokedToken{})
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		if w := axcSecurityDo(r, http.MethodGet, "/api/security/tokens/revoked", ""); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("success applies filters", func(t *testing.T) {
		db := axcNewSecurityDB(t)
		now := time.Now().UTC()
		activeExp := now.Add(time.Hour)
		pastExp := now.Add(-time.Hour)
		if err := db.Create([]models.RevokedToken{
			{JTI: "axc-jti-a", UserID: 181, SessionID: "axc-sess-a", TokenUse: "access", Reason: "manual", ExpiresAt: &activeExp, RevokedAt: now},
			{JTI: "axc-jti-b", UserID: 182, SessionID: "axc-sess-b", TokenUse: "refresh", ExpiresAt: &pastExp, RevokedAt: now},
		}).Error; err != nil {
			t.Fatalf("seed revoked tokens: %v", err)
		}
		r := axcSecurityRouter(NewUserSecurityHandler(usersecurity.NewService(db, axcSecurityLogger()), axcSecurityLogger()))
		w := axcSecurityDo(r, http.MethodGet, "/api/security/tokens/revoked?jti=axc-jti-a&user_id=181&session_id=axc-sess-a&token_use=access&active_only=true&page=1&page_size=5", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"jti":"axc-jti-a"`) || strings.Contains(w.Body.String(), `"jti":"axc-jti-b"`) {
			t.Fatalf("unexpected body: %s", w.Body.String())
		}
		w = axcSecurityDo(r, http.MethodGet, "/api/security/tokens/revoked?active_only=false&page=2&page_size=1", "")
		page2 := w.Body.String()
		if w.Code != http.StatusOK || !strings.Contains(page2, `"count":2`) || strings.Contains(page2, `"jti":"axc-jti-b"`) {
			t.Fatalf("unexpected page 2 response: %d %s", w.Code, page2)
		}
		w = axcSecurityDo(r, http.MethodGet, "/api/security/tokens/revoked?page=bad&page_size=bad", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}
