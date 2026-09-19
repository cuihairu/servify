package delivery

// HandlerServiceAdapter 纯转发对账 + 错误传播。

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/models"
	appintegrationapp "servify/apps/server/internal/modules/app_integration/application"
)

type adapterStubRepo struct {
	err error
}

func (r *adapterStubRepo) CountBySlug(ctx context.Context, slug string) (int64, error) {
	return 0, r.err
}

func (r *adapterStubRepo) CountIntegrations(ctx context.Context, req *AppIntegrationListRequest) (int64, error) {
	return 0, r.err
}

func (r *adapterStubRepo) ListIntegrations(ctx context.Context, req *AppIntegrationListRequest, offset, limit int) ([]models.AppIntegration, error) {
	return nil, r.err
}

func (r *adapterStubRepo) GetIntegration(ctx context.Context, id uint) (*models.AppIntegration, error) {
	return nil, r.err
}

func (r *adapterStubRepo) CreateIntegration(ctx context.Context, model *models.AppIntegration) error {
	return r.err
}

func (r *adapterStubRepo) SaveIntegration(ctx context.Context, model *models.AppIntegration) error {
	return r.err
}

func (r *adapterStubRepo) DeleteIntegration(ctx context.Context, id uint) error {
	return r.err
}

func TestHandlerServiceAdapter(t *testing.T) {
	ctx := context.Background()
	want := errors.New("boom")
	repo := &adapterStubRepo{err: want}
	svc := appintegrationapp.NewService(repo)
	var handler HandlerService = NewHandlerServiceAdapter(svc)

	if _, _, err := handler.List(ctx, &AppIntegrationListRequest{}); !errors.Is(err, want) {
		t.Fatalf("List() err = %v, want %v", err, want)
	}
	if _, err := handler.Create(ctx, &AppIntegrationCreateRequest{Name: "X", Slug: "x"}); !errors.Is(err, want) {
		t.Fatalf("Create() err = %v, want %v", err, want)
	}
	if _, err := handler.Update(ctx, 1, &AppIntegrationUpdateRequest{}); !errors.Is(err, want) {
		t.Fatalf("Update() err = %v, want %v", err, want)
	}
	if err := handler.Delete(ctx, 1); !errors.Is(err, want) {
		t.Fatalf("Delete() err = %v, want %v", err, want)
	}

	// 无错误时纯转发（Create nil 请求走 request required 校验，错误一致）
	repo.err = nil
	if _, err := handler.Create(ctx, nil); err == nil || err.Error() != "request required" {
		t.Fatalf("Create(nil) err = %v", err)
	}
	items, total, err := handler.List(ctx, &AppIntegrationListRequest{})
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("List() = %+v, %d, %v", items, total, err)
	}
}
