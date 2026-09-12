package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestAuditMiddlewareRecordsWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dsn := uniqueMemDSN(fmt.Sprintf("file:%s", t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	r := gin.New()
	r.Use(AuditMiddleware(db))
	r.POST("/api/tickets/:id/assign", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/tickets/9/assign", strings.NewReader(`{}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}

	var log models.AuditLog
	if err := db.First(&log, "action = ?", "tickets.assign").Error; err != nil {
		t.Fatalf("audit log missing: %v", err)
	}
	if log.ResourceID != "9" {
		t.Fatalf("resource id = %q", log.ResourceID)
	}
}

func TestEnforceRequestScopeWrapper(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now()
	secret := "wrapper-secret"
	token := createTestHS256JWT(t, map[string]interface{}{
		"token_type": "service",
		"iat":        now.Unix(),
		"exp":        now.Add(10 * time.Minute).Unix(),
	}, secret)

	r := gin.New()
	cfg := &config.Config{}
	cfg.JWT.Secret = secret
	r.Use(AuthMiddleware(cfg, nil, platformauth.RejectIssuedBefore(0)))
	r.Use(EnforceRequestScope())
	r.GET("/scoped", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"tenant_id": c.MustGet("tenant_id")}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/scoped?tenant_id=tenant-z", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "tenant-z") {
		t.Fatalf("expected projected tenant, body=%s", w.Body.String())
	}
}

func TestAuthorizeWrappers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("roles", []string{"agent"})
		c.Set("principal_kind", "agent")
		c.Set("permissions", []string{"tickets.read"})
		c.Next()
	})
	r.GET("/roles-ok", RequireRolesAny("admin", "agent"))
	r.GET("/roles-deny", RequireRolesAny("admin"))
	r.GET("/kinds-ok", RequirePrincipalKinds("agent"))
	r.GET("/kinds-deny", RequirePrincipalKinds("admin"))
	r.GET("/perm-any-ok", RequirePermissionsAny("tickets.read", "tickets.write"))
	r.GET("/perm-any-deny", RequirePermissionsAny("tickets.write"))
	r.GET("/perm-all-ok", RequirePermissionsAll("tickets.read"))
	r.GET("/perm-all-deny", RequirePermissionsAll("tickets.read", "tickets.write"))

	cases := map[string]int{
		"/roles-ok":      http.StatusOK,
		"/roles-deny":    http.StatusForbidden,
		"/kinds-ok":      http.StatusOK,
		"/kinds-deny":    http.StatusForbidden,
		"/perm-any-ok":   http.StatusOK,
		"/perm-any-deny": http.StatusForbidden,
		"/perm-all-ok":   http.StatusOK,
		"/perm-all-deny": http.StatusForbidden,
	}
	for path, want := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Fatalf("%s = %d want %d", path, w.Code, want)
		}
	}

	if !HasPermission([]string{"tickets.*"}, "tickets.read") {
		t.Fatal("HasPermission wildcard failed")
	}
}
