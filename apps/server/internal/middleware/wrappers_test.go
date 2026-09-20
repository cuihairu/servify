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

// TestAuditMiddlewareWithFailuresAudit4xx 直测 auth 公开面的失败留痕：
// AuditFailures 开启时 4xx 同样落审计（Success=false），默认中间件不留痕。
func TestAuditMiddlewareWithFailuresAudit4xx(t *testing.T) {
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
	r.Use(AuditMiddlewareWithFailures(db))
	r.POST("/api/auth/login", func(c *gin.Context) { c.JSON(http.StatusUnauthorized, gin.H{"error": "bad credentials"}) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"u"}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}

	var log models.AuditLog
	if err := db.First(&log, "action = ?", "auth.login").Error; err != nil {
		t.Fatalf("failure audit log missing: %v", err)
	}
	if log.Success || log.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected failure entry: success=%v status=%d", log.Success, log.StatusCode)
	}
}

// 默认中间件（AuditFailures 关闭）对 4xx 不留痕，与上一测试互为对照。
func TestAuditMiddlewareSkips4xxByDefault(t *testing.T) {
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
	r.POST("/api/auth/login", func(c *gin.Context) { c.JSON(http.StatusUnauthorized, gin.H{"error": "bad credentials"}) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}

	var count int64
	if err := db.Model(&models.AuditLog{}).Count(&count).Error; err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no audit entries for 4xx by default, got %d", count)
	}
}

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
		c.Set("principal_kind", "agent")
		c.Next()
	})
	r.GET("/kinds-ok", RequirePrincipalKinds("agent"))
	r.GET("/kinds-deny", RequirePrincipalKinds("admin"))

	cases := map[string]int{
		"/kinds-ok":   http.StatusOK,
		"/kinds-deny": http.StatusForbidden,
	}
	for path, want := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Fatalf("%s = %d want %d", path, w.Code, want)
		}
	}
}
