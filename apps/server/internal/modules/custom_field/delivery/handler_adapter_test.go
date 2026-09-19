package delivery

// HandlerServiceAdapter 纯转发对账 + 错误传播。

import (
	"context"
	"errors"
	"testing"

	customfieldapp "servify/apps/server/internal/modules/custom_field/application"

	"servify/apps/server/internal/models"
)

type adapterStubRepo struct {
	get       *models.CustomField
	list      []models.CustomField
	listErr   error
	getErr    error
	createErr error
	saveErr   error
	deleteErr error
}

func (r *adapterStubRepo) List(ctx context.Context, resource string, activeOnly bool) ([]models.CustomField, error) {
	return r.list, r.listErr
}

func (r *adapterStubRepo) GetScoped(ctx context.Context, id uint) (*models.CustomField, error) {
	return r.get, r.getErr
}

func (r *adapterStubRepo) Create(ctx context.Context, field *models.CustomField) error {
	if r.createErr != nil {
		return r.createErr
	}
	field.ID = 1
	r.get = field
	return nil
}

func (r *adapterStubRepo) Save(ctx context.Context, field *models.CustomField) error {
	return r.saveErr
}

func (r *adapterStubRepo) Delete(ctx context.Context, id uint) error {
	return r.deleteErr
}

func stringPtr(s string) *string { return &s }

func TestHandlerServiceAdapter(t *testing.T) {
	ctx := context.Background()
	repo := &adapterStubRepo{
		list: []models.CustomField{{ID: 2, Key: "k2"}},
		get:  &models.CustomField{ID: 1, Key: "k1"},
	}
	svc := customfieldapp.NewService(repo)
	var handler HandlerService = NewHandlerServiceAdapter(svc)

	fields, err := handler.List(ctx, "ticket", false)
	if err != nil || len(fields) != 1 || fields[0].ID != 2 {
		t.Fatalf("List() = %+v, %v", fields, err)
	}

	field, err := handler.Get(ctx, 1)
	if err != nil || field.ID != 1 {
		t.Fatalf("Get() = %+v, %v", field, err)
	}

	created, err := handler.Create(ctx, &CustomFieldCreateRequest{Key: "new", Name: "n", Type: "string"})
	if err != nil || created.ID != 1 {
		t.Fatalf("Create() = %+v, %v", created, err)
	}

	updated, err := handler.Update(ctx, 1, &CustomFieldUpdateRequest{Name: stringPtr("x")})
	if err != nil || updated.ID != 1 || updated.Name != "x" {
		t.Fatalf("Update() = %+v, %v", updated, err)
	}

	if err := handler.Delete(ctx, 1); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestHandlerServiceAdapterErrors(t *testing.T) {
	ctx := context.Background()
	want := errors.New("boom")
	repo := &adapterStubRepo{
		listErr:   want,
		getErr:    want,
		createErr: want,
		saveErr:   want,
		deleteErr: want,
	}
	svc := customfieldapp.NewService(repo)
	handler := NewHandlerServiceAdapter(svc)

	if _, err := handler.List(ctx, "ticket", false); !errors.Is(err, want) {
		t.Fatalf("List() err = %v, want %v", err, want)
	}
	if _, err := handler.Get(ctx, 1); !errors.Is(err, want) {
		t.Fatalf("Get() err = %v, want %v", err, want)
	}
	if _, err := handler.Create(ctx, &CustomFieldCreateRequest{Key: "k", Name: "n", Type: "string"}); !errors.Is(err, want) {
		t.Fatalf("Create() err = %v, want %v", err, want)
	}
	if _, err := handler.Update(ctx, 1, &CustomFieldUpdateRequest{Name: stringPtr("x")}); !errors.Is(err, want) {
		t.Fatalf("Update() err = %v, want %v", err, want)
	}
	if err := handler.Delete(ctx, 1); !errors.Is(err, want) {
		t.Fatalf("Delete() err = %v, want %v", err, want)
	}
}
