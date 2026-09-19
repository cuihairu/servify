package application

// Service 校验链与 patch 语义（自 services/custom_field_service*_test.go 下沉）。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"servify/apps/server/internal/models"
)

type stubRepo struct {
	list      []models.CustomField
	get       *models.CustomField
	listErr   error
	getErr    error
	createErr error
	saveErr   error
	deleteErr error
}

func (r *stubRepo) List(ctx context.Context, resource string, activeOnly bool) ([]models.CustomField, error) {
	return r.list, r.listErr
}

func (r *stubRepo) GetScoped(ctx context.Context, id uint) (*models.CustomField, error) {
	return r.get, r.getErr
}

func (r *stubRepo) Create(ctx context.Context, field *models.CustomField) error {
	if r.createErr != nil {
		return r.createErr
	}
	field.ID = 1
	r.get = field
	return nil
}

func (r *stubRepo) Save(ctx context.Context, field *models.CustomField) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.get = field
	return nil
}

func (r *stubRepo) Delete(ctx context.Context, id uint) error {
	return r.deleteErr
}

func stringPtr(s string) *string { return &s }

func boolPtr(b bool) *bool { return &b }

func TestServiceListDefaultsResource(t *testing.T) {
	repo := &stubRepo{listErr: errors.New("boom")}
	svc := NewService(repo)
	if _, err := svc.List(context.Background(), "", false); err == nil || err.Error() != "boom" {
		t.Fatalf("List() err = %v, want boom", err)
	}
}

func TestServiceGet(t *testing.T) {
	svc := NewService(&stubRepo{getErr: errors.New("boom")})
	if _, err := svc.Get(context.Background(), 1); err == nil || err.Error() != "boom" {
		t.Fatalf("Get() err = %v, want boom", err)
	}
}

func TestServiceCreate(t *testing.T) {
	base := &CustomFieldCreateRequest{Key: "priority", Name: "优先级", Type: "select"}
	cases := []struct {
		name    string
		req     *CustomFieldCreateRequest
		wantErr string
		check   func(*testing.T, *models.CustomField)
	}{
		{"valid", base, "", func(t *testing.T, f *models.CustomField) {
			if f.Key != "priority" || f.Resource != "ticket" || !f.Active {
				t.Fatalf("unexpected field: %+v", f)
			}
		}},
		{"defaults", &CustomFieldCreateRequest{Key: "k", Name: "n", Type: "string"}, "", func(t *testing.T, f *models.CustomField) {
			if f.Resource != "ticket" || !f.Active {
				t.Fatalf("expected ticket resource + active default: %+v", f)
			}
		}},
		{"nil", nil, "request required", nil},
		{"bad resource", &CustomFieldCreateRequest{Resource: "customer", Key: "k", Name: "n", Type: "string"}, "unsupported resource", nil},
		{"bad key", &CustomFieldCreateRequest{Key: "1K", Name: "n", Type: "string"}, "invalid key", nil},
		{"bad type", &CustomFieldCreateRequest{Key: "k", Name: "n", Type: "blob"}, "invalid type", nil},
		{"bad options", &CustomFieldCreateRequest{Key: "k", Name: "n", Type: "string", Options: make(chan int)}, "invalid options", nil},
		{"bad validation", &CustomFieldCreateRequest{Key: "k", Name: "n", Type: "string", Validation: "{oops"}, "invalid validation", nil},
		{"bad showwhen", &CustomFieldCreateRequest{Key: "k", Name: "n", Type: "string", ShowWhen: "[bad"}, "invalid show_when", nil},
		{"empty name", &CustomFieldCreateRequest{Key: "k", Name: "  ", Type: "string"}, "name required", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubRepo{}
			svc := NewService(repo)
			field, err := svc.Create(context.Background(), tc.req)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Create() error = %v", err)
				}
				if tc.check != nil {
					tc.check(t, field)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Create() err = %v, want contains %q", err, tc.wantErr)
			}
		})
	}
}

func TestServiceCreateActivePointerAndRepoError(t *testing.T) {
	repo := &stubRepo{createErr: errors.New("db down")}
	svc := NewService(repo)
	if _, err := svc.Create(context.Background(), &CustomFieldCreateRequest{Key: "k", Name: "n", Type: "string", Active: boolPtr(false), Required: true}); err == nil || err.Error() != "db down" {
		t.Fatalf("Create() err = %v, want db down", err)
	}
}

func TestServiceUpdate(t *testing.T) {
	existing := &models.CustomField{ID: 7, Key: "k", Name: "n", Type: "string", Active: true}
	base := func() *stubRepo {
		return &stubRepo{get: existing}
	}

	t.Run("full patch", func(t *testing.T) {
		svc := NewService(base())
		updated, err := svc.Update(context.Background(), 7, &CustomFieldUpdateRequest{
			Name:       stringPtr("New Name"),
			Type:       stringPtr("select"),
			Required:   boolPtr(true),
			Active:     boolPtr(false),
			Options:    `["a","b"]`,
			Validation: map[string]interface{}{"max": 10},
			ShowWhen:   "  ",
		})
		if err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if updated.Name != "New Name" || updated.Type != "select" || !updated.Required || updated.Active {
			t.Fatalf("patch not applied: %+v", updated)
		}
		if updated.OptionsJSON != `["a","b"]` {
			t.Fatalf("options json = %q", updated.OptionsJSON)
		}
		if updated.ValidationJSON != `{"max":10}` {
			t.Fatalf("validation json = %q", updated.ValidationJSON)
		}
		if updated.ShowWhenJSON != "" {
			t.Fatalf("blank show_when should be empty, got %q", updated.ShowWhenJSON)
		}
	})
	t.Run("nil request", func(t *testing.T) {
		if _, err := NewService(base()).Update(context.Background(), 7, nil); err == nil {
			t.Fatal("expected error for nil update")
		}
	})
	t.Run("get error", func(t *testing.T) {
		svc := NewService(&stubRepo{getErr: errors.New("record not found")})
		if _, err := svc.Update(context.Background(), 7, &CustomFieldUpdateRequest{}); err == nil {
			t.Fatal("expected get error propagation")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		if _, err := NewService(base()).Update(context.Background(), 7, &CustomFieldUpdateRequest{Type: stringPtr("bad")}); err == nil {
			t.Fatal("expected error for invalid type")
		}
	})
	t.Run("invalid options", func(t *testing.T) {
		if _, err := NewService(base()).Update(context.Background(), 7, &CustomFieldUpdateRequest{Options: make(chan int)}); err == nil {
			t.Fatal("expected error for unserializable options")
		}
	})
	t.Run("invalid validation", func(t *testing.T) {
		if _, err := NewService(base()).Update(context.Background(), 7, &CustomFieldUpdateRequest{Validation: "{oops"}); err == nil {
			t.Fatal("expected error for invalid validation")
		}
	})
	t.Run("invalid showwhen", func(t *testing.T) {
		if _, err := NewService(base()).Update(context.Background(), 7, &CustomFieldUpdateRequest{ShowWhen: "[bad"}); err == nil {
			t.Fatal("expected error for invalid show_when")
		}
	})
	t.Run("save error", func(t *testing.T) {
		svc := NewService(&stubRepo{get: existing, saveErr: errors.New("save down")})
		if _, err := svc.Update(context.Background(), 7, &CustomFieldUpdateRequest{Name: stringPtr("x")}); err == nil || err.Error() != "save down" {
			t.Fatalf("Update() err = %v, want save down", err)
		}
	})
}

func TestServiceDelete(t *testing.T) {
	svc := NewService(&stubRepo{deleteErr: errors.New("custom field not found")})
	if err := svc.Delete(context.Background(), 1); err == nil || err.Error() != "custom field not found" {
		t.Fatalf("Delete() err = %v, want not found", err)
	}
}

func TestCustomFieldHelpers(t *testing.T) {
	for _, typ := range []string{"string", "number", "boolean", "date", "select", "multiselect"} {
		if !isAllowedCustomFieldType(typ) {
			t.Fatalf("%s should be allowed", typ)
		}
	}
	if isAllowedCustomFieldType("text") {
		t.Fatal("text should not be allowed")
	}

	if v, err := marshalOptionalJSON(nil); v != "" || err != nil {
		t.Fatalf("nil json: %q %v", v, err)
	}
	if v, err := marshalOptionalJSON("   "); v != "" || err != nil {
		t.Fatalf("blank json: %q %v", v, err)
	}
	if v, err := marshalOptionalJSON(`{"a":1}`); v != `{"a":1}` || err != nil {
		t.Fatalf("raw json: %q %v", v, err)
	}
	if _, err := marshalOptionalJSON("not-json"); err == nil {
		t.Fatal("expected error for invalid json string")
	}
	if v, err := marshalOptionalJSON([]string{"x"}); v != `["x"]` || err != nil {
		t.Fatalf("slice json: %q %v", v, err)
	}
	if _, err := marshalOptionalJSON(make(chan int)); err == nil {
		t.Fatal("expected error for unserializable value")
	}
}
