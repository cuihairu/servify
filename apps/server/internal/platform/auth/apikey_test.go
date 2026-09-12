package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestGenerateAPIKeyFormat(t *testing.T) {
	plaintext, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}
	if !strings.HasPrefix(plaintext, APIKeyPrefix) {
		t.Fatalf("plaintext should start with %q: %s", APIKeyPrefix, plaintext)
	}
	if len(plaintext) != len(APIKeyPrefix)+40 {
		t.Fatalf("plaintext should be sv_ + 40 hex chars, got len %d", len(plaintext))
	}
	if len(prefix) != 11 {
		t.Fatalf("prefix should be 11 chars, got %q", prefix)
	}
	if hash != APIKeyHash(plaintext) {
		t.Fatal("hash should match APIKeyHash(plaintext)")
	}
	other, _, otherHash, _ := GenerateAPIKey()
	if other == plaintext || otherHash == hash {
		t.Fatal("keys must be unique")
	}
}

func TestAPIKeyScopesOrDefault(t *testing.T) {
	if got := APIKeyScopesOrDefault(nil); len(got) != 2 || got[0] != PermissionTicketsRead {
		t.Fatalf("nil scopes should fall back to defaults, got %v", got)
	}
	if got := APIKeyScopesOrDefault([]string{"", "  "}); len(got) != 2 {
		t.Fatalf("blank scopes should fall back to defaults, got %v", got)
	}
	if got := APIKeyScopesOrDefault([]string{"tickets.read", "custom.scope"}); len(got) != 2 || got[1] != "custom.scope" {
		t.Fatalf("explicit scopes should be kept, got %v", got)
	}
}

func newAPIKeyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:apikey_auth")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.APIKey{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Migrator().DropTable(&models.APIKey{}) })
	return db
}

func TestGormAPIKeyResolverLifecycle(t *testing.T) {
	db := newAPIKeyTestDB(t)
	resolver := NewGormAPIKeyResolver(db)
	ctx := context.Background()

	plaintext, _, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	row := &models.APIKey{Name: "ops", Prefix: plaintext[:11], KeyHash: APIKeyHash(plaintext), TenantID: "t1", WorkspaceID: "w1"}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	record, err := resolver.ResolveAPIKey(ctx, plaintext)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if record.ID != row.ID || record.TenantID != "t1" || record.WorkspaceID != "w1" {
		t.Fatalf("unexpected record: %+v", record)
	}
	if len(record.Scopes) != 2 || record.Scopes[0] != PermissionTicketsRead {
		t.Fatalf("default scopes expected, got %v", record.Scopes)
	}
	var after models.APIKey
	db.First(&after, row.ID)
	if after.LastUsedAt == nil {
		t.Fatal("last_used_at should be touched on first resolve")
	}

	// 节流：紧随其后的解析不更新 last_used_at。
	touched := *after.LastUsedAt
	resolver.Now = func() time.Time { return time.Now().Add(10 * time.Second) }
	if _, err := resolver.ResolveAPIKey(ctx, plaintext); err != nil {
		t.Fatalf("resolve within throttle: %v", err)
	}
	var throttled models.APIKey
	db.First(&throttled, row.ID)
	if !throttled.LastUsedAt.Equal(touched) {
		t.Fatal("last_used_at should be throttled within 60s")
	}

	// 吊销即时生效。
	if _, err := resolver.ResolveAPIKey(ctx, plaintext); err != nil {
		t.Fatalf("resolve after throttle window: %v", err)
	}
	db.Model(&models.APIKey{}).Where("id = ?", row.ID).Update("revoked_at", time.Now())
	if _, err := resolver.ResolveAPIKey(ctx, plaintext); err != ErrAPIKeyInvalid {
		t.Fatalf("revoked key should be invalid, got %v", err)
	}
}

func TestGormAPIKeyResolverRejects(t *testing.T) {
	db := newAPIKeyTestDB(t)
	resolver := NewGormAPIKeyResolver(db)
	ctx := context.Background()

	if _, err := resolver.ResolveAPIKey(ctx, "not-a-key"); err != ErrAPIKeyInvalid {
		t.Fatalf("malformed key should be invalid, got %v", err)
	}
	if _, err := resolver.ResolveAPIKey(ctx, APIKeyPrefix+"ffffffff"); err != ErrAPIKeyInvalid {
		t.Fatalf("unknown key should be invalid, got %v", err)
	}

	plaintext, _, _, _ := GenerateAPIKey()
	expired := &models.APIKey{Name: "old", KeyHash: APIKeyHash(plaintext), ExpiresAt: ptrTime(time.Now().Add(-time.Hour))}
	if err := db.Create(expired).Error; err != nil {
		t.Fatalf("seed expired: %v", err)
	}
	if _, err := resolver.ResolveAPIKey(ctx, plaintext); err != ErrAPIKeyInvalid {
		t.Fatalf("expired key should be invalid, got %v", err)
	}
}

func TestAuthMiddleware_APIKeyBranch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newAPIKeyTestDB(t)
	plaintext, _, _, _ := GenerateAPIKey()
	if err := db.Create(&models.APIKey{Name: "ci", KeyHash: APIKeyHash(plaintext), TenantID: "t9"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	build := func(resolver APIKeyResolver) *gin.Engine {
		r := gin.New()
		r.Use(AuthMiddleware(MiddlewareConfig{Secret: "jwt-secret", APIKeyResolver: resolver}))
		r.GET("/ping", func(c *gin.Context) {
			kind, _ := c.Get("principal_kind")
			perms, _ := c.Get("permissions")
			c.JSON(http.StatusOK, gin.H{"kind": kind, "perms": perms})
		})
		return r
	}

	r := build(NewGormAPIKeyResolver(db))
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("X-API-Key", plaintext)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("valid api key expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"kind":"service"`) {
		t.Fatalf("expected service principal, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), PermissionTicketsRead) || !strings.Contains(w.Body.String(), PermissionConversationsRead) {
		t.Fatalf("expected default readonly perms, got %s", w.Body.String())
	}

	// 无效密钥 401
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/ping", nil)
	req2.Header.Set("X-API-Key", APIKeyPrefix+"bogus")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("invalid api key expected 401 got %d", w2.Code)
	}

	// resolver 未配置时带 X-API-Key 也 401，且不影响无头请求的原有语义
	r3 := build(nil)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodGet, "/ping", nil)
	req3.Header.Set("X-API-Key", plaintext)
	r3.ServeHTTP(w3, req3)
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("missing resolver expected 401 got %d", w3.Code)
	}
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest(http.MethodGet, "/ping", nil)
	r3.ServeHTTP(w4, req4)
	if w4.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer (legacy path) expected 401 got %d", w4.Code)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
