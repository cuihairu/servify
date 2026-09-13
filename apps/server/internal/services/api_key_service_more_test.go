package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

func newAPIKeyTestService(t *testing.T) (*APIKeyService, *gorm.DB) {
	t.Helper()
	db := newServicesTestDB(t, &models.APIKey{})
	return NewAPIKeyService(db), db
}

func TestAPIKeyServiceLifecycle(t *testing.T) {
	svc, _ := newAPIKeyTestService(t)
	ctx := context.Background()

	if svc == nil {
		t.Fatal("NewAPIKeyService returned nil")
	}

	expires := time.Now().Add(24 * time.Hour)
	row, plaintext, err := svc.Create(ctx, &APIKeyCreateRequest{
		Name:        " ci key ",
		WorkspaceID: "ws-1",
		Scopes:      " tickets:read ",
		ExpiresAt:   &expires,
	}, "admin-1")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if plaintext == "" || row.Prefix == "" {
		t.Fatalf("expected plaintext and prefix, got prefix=%q", row.Prefix)
	}
	if strings.Contains(plaintext, row.KeyHash) || row.KeyHash == plaintext {
		t.Fatal("key hash must not be the plaintext")
	}
	if row.Name != "ci key" || row.WorkspaceID != "ws-1" || row.Scopes != "tickets:read" {
		t.Fatalf("unexpected row: %+v", row)
	}
	if row.RevokedAt != nil {
		t.Fatalf("fresh key revoked: %+v", row)
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
	if _, err := svc.Revoke(ctx, row.ID); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("Revoke() after delete err = %v, want %v", err, ErrAPIKeyNotFound)
	}
	if err := svc.Delete(ctx, row.ID); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("Delete() after delete err = %v, want %v", err, ErrAPIKeyNotFound)
	}
}

func TestAPIKeyServiceCreateValidation(t *testing.T) {
	svc, _ := newAPIKeyTestService(t) // db 仅用于建库，校验不触库
	ctx := context.Background()

	if _, _, err := svc.Create(ctx, nil, "admin"); err == nil {
		t.Fatal("expected error for nil request")
	}
	if _, _, err := svc.Create(ctx, &APIKeyCreateRequest{Name: "   "}, "admin"); err == nil {
		t.Fatal("expected error for blank name")
	}

	past := time.Now().Add(-time.Hour)
	if _, _, err := svc.Create(ctx, &APIKeyCreateRequest{Name: "stale", ExpiresAt: &past}, "admin"); err == nil {
		t.Fatal("expected error for past expires_at")
	}
	if _, _, err := svc.Create(ctx, &APIKeyCreateRequest{Name: "now"}, "admin"); err != nil {
		t.Fatalf("Create() without expiry error = %v", err)
	}
}

func TestAPIKeyServiceRevokeUpdateError(t *testing.T) {
	svc, db := newAPIKeyTestService(t)
	ctx := context.Background()
	row, _, err := svc.Create(ctx, &APIKeyCreateRequest{Name: "revoke-fail"}, "admin")
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

func TestAPIKeyServiceDBErrors(t *testing.T) {
	svc, db := newAPIKeyTestService(t)
	ctx := context.Background()
	if err := db.Migrator().DropTable("api_keys"); err != nil {
		t.Fatalf("drop api keys: %v", err)
	}

	if _, err := svc.List(ctx); err == nil {
		t.Fatal("expected list error on dropped table")
	}
	if _, _, err := svc.Create(ctx, &APIKeyCreateRequest{Name: "k"}, "admin"); err == nil {
		t.Fatal("expected create error on dropped table")
	}
	if _, err := svc.Revoke(ctx, 1); err == nil {
		t.Fatal("expected revoke error on dropped table")
	}
	if err := svc.Delete(ctx, 1); err == nil {
		t.Fatal("expected delete error on dropped table")
	}
}
