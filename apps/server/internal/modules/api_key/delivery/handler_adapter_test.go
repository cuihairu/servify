package delivery

// HandlerServiceAdapter 的转发覆盖：逐方法对账透传语义与错误传播。

import (
	"context"
	"errors"
	"testing"
	"time"

	apikeyapp "servify/apps/server/internal/modules/api_key/application"

	"servify/apps/server/internal/models"
)

type adapterStubRepo struct {
	list      []models.APIKey
	listErr   error
	createErr error
	get       *models.APIKey
	getErr    error
	revokeErr error
	deleteErr error
}

func (r *adapterStubRepo) List(ctx context.Context) ([]models.APIKey, error) {
	return r.list, r.listErr
}

func (r *adapterStubRepo) Create(ctx context.Context, row *models.APIKey) error { return r.createErr }

func (r *adapterStubRepo) Get(ctx context.Context, id uint) (*models.APIKey, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.get, nil
}

func (r *adapterStubRepo) Revoke(ctx context.Context, id uint, revokedAt time.Time) error {
	return r.revokeErr
}

func (r *adapterStubRepo) Delete(ctx context.Context, id uint) error { return r.deleteErr }

func TestHandlerServiceAdapter_AllMethods(t *testing.T) {
	repo := &adapterStubRepo{list: []models.APIKey{{Name: "k"}}, get: &models.APIKey{Name: "k"}}
	adapter := NewHandlerServiceAdapter(apikeyapp.NewService(repo))
	if adapter == nil {
		t.Fatal("expected adapter instance")
	}
	ctx := context.Background()

	keys, err := adapter.List(ctx)
	if err != nil || len(keys) != 1 {
		t.Fatalf("List: %v %+v", err, keys)
	}
	row, plaintext, err := adapter.Create(ctx, &APIKeyCreateRequest{Name: "k"}, "admin")
	if err != nil || row == nil || plaintext == "" {
		t.Fatalf("Create: %v %+v %q", err, row, plaintext)
	}
	revoked, err := adapter.Revoke(ctx, 1)
	if err != nil || revoked == nil {
		t.Fatalf("Revoke: %v %+v", err, revoked)
	}
	if err := adapter.Delete(ctx, 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestHandlerServiceAdapter_ErrorPropagation(t *testing.T) {
	boom := errors.New("boom: api key")
	adapter := NewHandlerServiceAdapter(apikeyapp.NewService(&adapterStubRepo{listErr: boom, createErr: boom, getErr: boom, deleteErr: boom}))
	ctx := context.Background()

	if _, err := adapter.List(ctx); !errors.Is(err, boom) {
		t.Fatalf("List error propagation: %v", err)
	}
	if _, _, err := adapter.Create(ctx, &APIKeyCreateRequest{Name: "k"}, "admin"); !errors.Is(err, boom) {
		t.Fatalf("Create error propagation: %v", err)
	}
	if _, err := adapter.Revoke(ctx, 1); !errors.Is(err, boom) {
		t.Fatalf("Revoke error propagation: %v", err)
	}
	if err := adapter.Delete(ctx, 1); !errors.Is(err, boom) {
		t.Fatalf("Delete error propagation: %v", err)
	}

	// not found 经 application 哨兵映射（Revoke 的 Get 失败路径）。
	notFound := NewHandlerServiceAdapter(apikeyapp.NewService(&adapterStubRepo{getErr: apikeyapp.ErrAPIKeyNotFound}))
	if _, err := notFound.Revoke(ctx, 42); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("Revoke not found: %v", err)
	}
}
