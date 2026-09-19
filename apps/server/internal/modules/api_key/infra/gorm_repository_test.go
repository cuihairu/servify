package infra

// GormRepository 的 SQL 语义对账（自 services facade 下沉）：
// 生命周期、吊销写库失败、丢表错误与租户/工作区过滤。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	application "servify/apps/server/internal/modules/api_key/application"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newApiKeyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := uniqueMemDSN("file:api_key_" + strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.APIKey{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

// scopedContext 构造带租户/工作区的上下文（驱动 applyScopeFilter）。
func scopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func TestGormRepositoryLifecycle(t *testing.T) {
	db := newApiKeyTestDB(t)
	svc := newModuleService(db)
	ctx := context.Background()

	expires := time.Now().Add(24 * time.Hour)
	row, plaintext, err := svc.Create(ctx, newCreateRequest("ci key", "ws-1", " tickets:read ", &expires), "admin-1")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if plaintext == "" || row.Prefix == "" {
		t.Fatalf("expected plaintext and prefix, got prefix=%q", row.Prefix)
	}
	if row.Name != "ci key" || row.Scopes != "tickets:read" {
		t.Fatalf("unexpected row: %+v", row)
	}

	keys, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(keys) != 1 || keys[0].ID != row.ID {
		t.Fatalf("List() = %+v", keys)
	}

	// 吊销：即时生效
	revoked, err := svc.Revoke(ctx, row.ID)
	if err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if revoked.RevokedAt == nil {
		t.Fatalf("revoked row missing RevokedAt: %+v", revoked)
	}
	// 幂等：已吊销直接返回
	again, err := svc.Revoke(ctx, row.ID)
	if err != nil || again.RevokedAt == nil {
		t.Fatalf("second Revoke() = %+v, %v", again, err)
	}

	if err := svc.Delete(ctx, row.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := svc.Revoke(ctx, row.ID); !errors.Is(err, application.ErrAPIKeyNotFound) {
		t.Fatalf("Revoke() after delete err = %v, want %v", err, application.ErrAPIKeyNotFound)
	}
	if err := svc.Delete(ctx, row.ID); !errors.Is(err, application.ErrAPIKeyNotFound) {
		t.Fatalf("Delete() after delete err = %v, want %v", err, application.ErrAPIKeyNotFound)
	}
}

func TestGormRepositoryRevokeUpdateError(t *testing.T) {
	db := newApiKeyTestDB(t)
	svc := newModuleService(db)
	ctx := context.Background()
	row, _, err := svc.Create(ctx, newCreateRequest("revoke-fail", "", "", nil), "admin")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// 吊销写库失败：查询成功但 Updates 报错
	if err := db.Callback().Update().Before("gorm:update").Register("test:fail_apikey_update", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "api_keys" {
			_ = tx.AddError(errors.New("boom revoke update"))
		}
	}); err != nil {
		t.Fatalf("register update callback: %v", err)
	}
	if _, err := svc.Revoke(ctx, row.ID); err == nil || !strings.Contains(err.Error(), "boom revoke update") {
		t.Fatalf("Revoke() err = %v, want update failure", err)
	}

	// 移除回调后吊销恢复正常
	if err := db.Callback().Update().Remove("test:fail_apikey_update"); err != nil {
		t.Fatalf("remove update callback: %v", err)
	}
	revoked, err := svc.Revoke(ctx, row.ID)
	if err != nil || revoked.RevokedAt == nil {
		t.Fatalf("Revoke() after callback removal = %+v, %v", revoked, err)
	}
}

func TestGormRepositoryDBErrors(t *testing.T) {
	db := newApiKeyTestDB(t)
	svc := newModuleService(db)
	ctx := context.Background()
	if err := db.Migrator().DropTable("api_keys"); err != nil {
		t.Fatalf("drop api keys: %v", err)
	}

	if _, err := svc.List(ctx); err == nil {
		t.Fatal("expected list error on dropped table")
	}
	if _, _, err := svc.Create(ctx, newCreateRequest("k", "", "", nil), "admin"); err == nil {
		t.Fatal("expected create error on dropped table")
	}
	if _, err := svc.Revoke(ctx, 1); err == nil {
		t.Fatal("expected revoke error on dropped table")
	}
	if err := svc.Delete(ctx, 1); err == nil {
		t.Fatal("expected delete error on dropped table")
	}
}

// TestGormRepositoryScopeFilter 对账租户/工作区过滤语义：带 scope 的上下文
// 只看到自己租户/工作区的密钥。
func TestGormRepositoryScopeFilter(t *testing.T) {
	db := newApiKeyTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	seed := func(name, tenantID, workspaceID string) {
		t.Helper()
		_, prefix, hash, err := platformauth.GenerateAPIKey()
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
		row := &models.APIKey{Name: name, Prefix: prefix, KeyHash: hash, TenantID: tenantID, WorkspaceID: workspaceID}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	seed("t1-w1", "t1", "w1")
	seed("t1-w2", "t1", "w2")
	seed("t2-w1", "t2", "w1")
	seed("no-scope", "", "")

	// 全量：无 scope 上下文可见全部（含空租户行）。
	all, err := repo.List(ctx)
	if err != nil || len(all) != 4 {
		t.Fatalf("List() unscoped = %d items, %v", len(all), err)
	}

	if got, err := repo.List(scopedContext("t1", "")); err != nil || len(got) != 2 {
		t.Fatalf("List() tenant t1 = %d items, %v", len(got), err)
	}
	if got, err := repo.List(scopedContext("", "w1")); err != nil || len(got) != 2 {
		t.Fatalf("List() workspace w1 = %d items, %v", len(got), err)
	}
	got, err := repo.List(scopedContext("t1", "w1"))
	if err != nil || len(got) != 1 || got[0].Name != "t1-w1" {
		t.Fatalf("List() t1/w1 = %+v, %v", got, err)
	}
}

// newModuleService 组装 module（application Service + 本包 repo），
// 让组合语义走完整条链路。
func newModuleService(db *gorm.DB) *application.Service {
	return application.NewService(NewGormRepository(db))
}

// newCreateRequest 构造签发请求（局部辅助，避免行宽爆炸）。
func newCreateRequest(name, workspaceID, scopes string, expires *time.Time) *application.APIKeyCreateRequest {
	req := application.APIKeyCreateRequest{Name: name, WorkspaceID: workspaceID, Scopes: scopes, ExpiresAt: expires}
	return &req
}
