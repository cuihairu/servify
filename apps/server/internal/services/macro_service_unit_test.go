package services

import (
	"testing"

	"gorm.io/gorm"
	"servify/apps/server/internal/models"
)

func newMacroTestService(t *testing.T) (*MacroService, *gorm.DB) {
	t.Helper()
	db := newServicesTestDB(t, &models.Macro{}, &models.Ticket{}, &models.TicketComment{})
	return NewMacroService(db), db
}

func TestMacroService_CreateAndUpdate(t *testing.T) {
	svc, _ := newMacroTestService(t)
	ctx := unitScopedContext("t1", "w1")

	m, err := svc.Create(ctx, &MacroCreateRequest{Name: "greet", Content: "hello"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if m.Language != "zh" {
		t.Fatalf("default language = %q", m.Language)
	}
	if m.TenantID != "t1" || m.WorkspaceID != "w1" {
		t.Fatalf("unexpected scope: %+v", m)
	}

	if _, err := svc.Create(ctx, nil); err == nil {
		t.Fatal("expected error for nil request")
	}

	updated, err := svc.Update(ctx, m.ID, &MacroUpdateRequest{
		Description: stringPtr("desc"),
		Content:     stringPtr("new"),
		Language:    stringPtr("en"),
		Active:      boolPtr(false),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Description != "desc" || updated.Content != "new" || updated.Language != "en" || updated.Active {
		t.Fatalf("unexpected update result: %+v", updated)
	}

	if _, err := svc.Update(ctx, 9999, &MacroUpdateRequest{}); err == nil {
		t.Fatal("expected error updating missing macro")
	}
	if _, err := svc.Update(ctx, m.ID, nil); err == nil {
		t.Fatal("expected error for nil update request")
	}
}

func TestMacroService_ListAndDelete(t *testing.T) {
	svc, _ := newMacroTestService(t)
	ctx := unitScopedContext("t1", "w1")

	for _, name := range []string{"a", "b"} {
		if _, err := svc.Create(ctx, &MacroCreateRequest{Name: name, Content: "c"}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	items, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 macros, got %d", len(items))
	}

	if err := svc.Delete(ctx, items[0].ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := svc.Delete(ctx, items[0].ID); err == nil {
		t.Fatal("expected error deleting missing macro")
	}
}

func TestDefaultLang(t *testing.T) {
	if defaultLang("") != "zh" {
		t.Fatal("expected zh default")
	}
	if defaultLang("en") != "en" {
		t.Fatal("expected passthrough")
	}
}
