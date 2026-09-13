package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestAuthMiddlewareWithDBWiresAPIKeyResolver 覆盖 db 非 nil 时启用 X-API-Key
// 解析器的装配分支：中间件照常放行合法 JWT，也照常拒绝匿名请求。
func TestAuthMiddlewareWithDBWiresAPIKeyResolver(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dsn := uniqueMemDSN(fmt.Sprintf("file:%s", t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.APIKey{}); err != nil {
		t.Fatalf("migrate api keys: %v", err)
	}

	secret := "apikey-resolver-secret"
	now := time.Now()
	cfg := &config.Config{}
	cfg.JWT.Secret = secret

	router := gin.New()
	router.Use(AuthMiddleware(cfg, db)) // db != nil 分支
	router.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })

	token := createTestHS256JWT(t, map[string]interface{}{
		"token_type": "service",
		"iat":        now.Unix(),
		"exp":        now.Add(10 * time.Minute).Unix(),
	}, secret)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/ok", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d body=%s", w.Code, w.Body.String())
	}

	anon := httptest.NewRecorder()
	router.ServeHTTP(anon, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without credentials, got %d", anon.Code)
	}

	if AuthMiddleware(cfg, db) == nil {
		t.Fatal("middleware factory returned nil handler")
	}
}
