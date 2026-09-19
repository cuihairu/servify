package application

// Service 的校验、签发（seam 注入）与吊销幂等分支；
// SQL 语义由 infra 包的组合测试对账。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

type stubRepo struct {
	list      []models.APIKey
	listErr   error
	createErr error
	get       *models.APIKey
	getErr    error
	revokeErr error
	deleteErr error
}

func (r *stubRepo) List(ctx context.Context) ([]models.APIKey, error) {
	return r.list, r.listErr
}

func (r *stubRepo) Create(ctx context.Context, row *models.APIKey) error { return r.createErr }

func (r *stubRepo) Get(ctx context.Context, id uint) (*models.APIKey, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.get, nil
}

func (r *stubRepo) Revoke(ctx context.Context, id uint, revokedAt time.Time) error {
	if r.revokeErr != nil {
		return r.revokeErr
	}
	// 模拟吊销落库后的状态变化：再次 Get 可见 RevokedAt。
	if r.get != nil {
		revoked := *r.get
		revoked.RevokedAt = &revokedAt
		r.get = &revoked
	}
	return nil
}

func (r *stubRepo) Delete(ctx context.Context, id uint) error { return r.deleteErr }

func TestServiceCreateSuccess(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	expires := time.Now().Add(24 * time.Hour)

	row, plaintext, err := svc.Create(context.Background(), &APIKeyCreateRequest{
		Name:        " ci key ",
		WorkspaceID: "ws-1",
		Scopes:      " tickets:read ",
		ExpiresAt:   &expires,
	}, "admin-1")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if plaintext == "" || row.Prefix == "" || row.KeyHash == "" {
		t.Fatalf("expected plaintext/prefix/hash, got %+v", row)
	}
	if strings.Contains(plaintext, row.KeyHash) || row.KeyHash == plaintext {
		t.Fatal("key hash must not be the plaintext")
	}
	if row.Name != "ci key" || row.WorkspaceID != "ws-1" || row.Scopes != "tickets:read" || row.CreatedBy != "admin-1" {
		t.Fatalf("unexpected row: %+v", row)
	}
	if row.RevokedAt != nil {
		t.Fatalf("fresh key revoked: %+v", row)
	}
}

func TestServiceCreateValidation(t *testing.T) {
	svc := NewService(&stubRepo{})
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
}

func TestServiceCreateKeygenError(t *testing.T) {
	restore := hookGenerateAPIKey
	hookGenerateAPIKey = func() (string, string, string, error) {
		return "", "", "", errors.New("boom: keygen")
	}
	defer func() { hookGenerateAPIKey = restore }()

	svc := NewService(&stubRepo{})
	if _, _, err := svc.Create(context.Background(), &APIKeyCreateRequest{Name: "k"}, "admin"); err == nil || !strings.Contains(err.Error(), "keygen") {
		t.Fatalf("expected keygen error, got %v", err)
	}
}

func TestServiceCreateRepoError(t *testing.T) {
	svc := NewService(&stubRepo{createErr: errors.New("boom create")})
	if _, _, err := svc.Create(context.Background(), &APIKeyCreateRequest{Name: "k"}, "admin"); err == nil {
		t.Fatal("expected repo create error")
	}
}

func TestServiceList(t *testing.T) {
	want := []models.APIKey{{Name: "k"}}
	svc := NewService(&stubRepo{list: want})
	got, err := svc.List(context.Background())
	if err != nil || len(got) != 1 || got[0].Name != "k" {
		t.Fatalf("List() = %+v, %v", got, err)
	}

	failing := NewService(&stubRepo{listErr: errors.New("boom list")})
	if _, err := failing.List(context.Background()); err == nil {
		t.Fatal("expected list error")
	}
}

func TestServiceRevoke(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	revokedRow := &models.APIKey{Name: "k", RevokedAt: &now}

	// 幂等：已吊销直接返回
	svc := NewService(&stubRepo{get: revokedRow})
	got, err := svc.Revoke(ctx, 1)
	if err != nil || got.RevokedAt == nil {
		t.Fatalf("Revoke() idempotent = %+v, %v", got, err)
	}

	// 正常吊销：首次 Get 未吊销，Revoke 落库后再次 Get 可见 RevokedAt
	repo := &stubRepo{get: &models.APIKey{Name: "k"}}
	svc = NewService(repo)
	got, err = svc.Revoke(ctx, 1)
	if err != nil || got.RevokedAt == nil {
		t.Fatalf("Revoke() = %+v, %v", got, err)
	}

	// get 错误传播（含 not found）
	if _, err := NewService(&stubRepo{getErr: ErrAPIKeyNotFound}).Revoke(ctx, 42); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("Revoke() err = %v, want %v", err, ErrAPIKeyNotFound)
	}
	// update 错误传播
	if _, err := NewService(&stubRepo{get: &models.APIKey{Name: "k"}, revokeErr: errors.New("boom revoke")}).Revoke(ctx, 1); err == nil {
		t.Fatal("expected revoke update error")
	}
}

func TestServiceDelete(t *testing.T) {
	if err := NewService(&stubRepo{}).Delete(context.Background(), 1); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := NewService(&stubRepo{deleteErr: ErrAPIKeyNotFound}).Delete(context.Background(), 1); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("Delete() err = %v, want %v", err, ErrAPIKeyNotFound)
	}
}
