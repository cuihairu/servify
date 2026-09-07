package services

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"
)

func TestCustomFieldService_CRUD(t *testing.T) {
	db := newServicesTestDB(t, &models.CustomField{})
	svc := NewCustomFieldService(db)
	ctx := unitScopedContext("t1", "w1")

	created, err := svc.Create(ctx, &CustomFieldCreateRequest{
		Key:     " Priority_Score ",
		Name:    "Priority Score",
		Type:    " number ",
		Options: map[string]interface{}{"min": 0},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Key != "priority_score" || created.Type != "number" || !created.Active {
		t.Fatalf("unexpected field: %+v", created)
	}
	if created.TenantID != "t1" || created.WorkspaceID != "w1" {
		t.Fatalf("unexpected scope: %+v", created)
	}

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("Get returned %+v", got)
	}
	if _, err := svc.Get(ctx, 999); err == nil {
		t.Fatal("expected error for missing field")
	}

	fields, err := svc.List(ctx, "", true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(fields) != 1 {
		t.Fatalf("expected active field, got %d", len(fields))
	}
	fields, err = svc.List(ctx, "ticket", false)
	if err != nil || len(fields) != 1 {
		t.Fatalf("List ticket: %v %d", err, len(fields))
	}
	if _, err = svc.List(ctx, "other", false); err != nil {
		t.Fatalf("List other: %v", err)
	}

	updated, err := svc.Update(ctx, created.ID, &CustomFieldUpdateRequest{
		Name:       stringPtr("New Name"),
		Type:       stringPtr("select"),
		Required:   boolPtr(true),
		Active:     boolPtr(true),
		Options:    `["a","b"]`,
		Validation: map[string]interface{}{"max": 10},
		ShowWhen:   "  ",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "New Name" || updated.Type != "select" || !updated.Required || !updated.Active {
		t.Fatalf("unexpected update: %+v", updated)
	}
	if updated.OptionsJSON != `["a","b"]` {
		t.Fatalf("options json = %q", updated.OptionsJSON)
	}
	if updated.ShowWhenJSON != "" {
		t.Fatalf("blank show_when should be empty, got %q", updated.ShowWhenJSON)
	}

	if _, err := svc.Update(ctx, created.ID, nil); err == nil {
		t.Fatal("expected error for nil update")
	}
	if _, err := svc.Update(ctx, 999, &CustomFieldUpdateRequest{}); err == nil {
		t.Fatal("expected error updating missing field")
	}
	if _, err := svc.Update(ctx, created.ID, &CustomFieldUpdateRequest{Type: stringPtr("bad")}); err == nil {
		t.Fatal("expected error for invalid type")
	}
	if _, err := svc.Update(ctx, created.ID, &CustomFieldUpdateRequest{Options: make(chan int)}); err == nil {
		t.Fatal("expected error for unserializable options")
	}

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := svc.Delete(ctx, created.ID); err == nil {
		t.Fatal("expected error deleting missing field")
	}
}

func TestCustomFieldService_CreateValidation(t *testing.T) {
	db := newServicesTestDB(t, &models.CustomField{})
	svc := NewCustomFieldService(db)
	ctx := context.Background()

	cases := []struct {
		name string
		req  *CustomFieldCreateRequest
	}{
		{"nil", nil},
		{"bad resource", &CustomFieldCreateRequest{Resource: "customer", Key: "k", Name: "n", Type: "string"}},
		{"bad key", &CustomFieldCreateRequest{Key: "1K", Name: "n", Type: "string"}},
		{"bad type", &CustomFieldCreateRequest{Key: "k", Name: "n", Type: "blob"}},
		{"bad options", &CustomFieldCreateRequest{Key: "k", Name: "n", Type: "string", Options: make(chan int)}},
		{"bad validation", &CustomFieldCreateRequest{Key: "k", Name: "n", Type: "string", Validation: "{oops"}},
		{"bad showwhen", &CustomFieldCreateRequest{Key: "k", Name: "n", Type: "string", ShowWhen: "[bad"}},
		{"empty name", &CustomFieldCreateRequest{Key: "k", Name: "  ", Type: "string"}},
	}
	for _, tc := range cases {
		if _, err := svc.Create(ctx, tc.req); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
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
