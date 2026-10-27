package channel

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

func cursorScope() CursorScope {
	return CursorScope{
		TenantID:       "tenant-1",
		WorkspaceID:    "workspace-1",
		Platform:       "memex",
		InstallationID: "inst-1",
	}
}

func TestCursorSaveLoadAndClear(t *testing.T) {
	store := NewCursorStore(newChannelTestDB(t))
	ctx := context.Background()

	got, err := store.Cursor(ctx, cursorScope())
	if err != nil {
		t.Fatalf("cursor before save: %v", err)
	}
	if got != "" {
		t.Errorf("cursor before save = %q, want empty (fresh poll)", got)
	}

	if err := store.Save(ctx, cursorScope(), "offset-42"); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err = store.Cursor(ctx, cursorScope())
	if err != nil {
		t.Fatalf("cursor after save: %v", err)
	}
	if got != "offset-42" {
		t.Errorf("cursor = %q, want offset-42", got)
	}

	if err := store.Save(ctx, cursorScope(), "offset-99"); err != nil {
		t.Fatalf("save overwrite: %v", err)
	}
	got, err = store.Cursor(ctx, cursorScope())
	if err != nil {
		t.Fatalf("cursor after overwrite: %v", err)
	}
	if got != "offset-99" {
		t.Errorf("cursor = %q, want offset-99", got)
	}

	other := cursorScope()
	other.InstallationID = "inst-2"
	if err := store.Save(ctx, other, "offset-other"); err != nil {
		t.Fatalf("save other installation: %v", err)
	}
	got, err = store.Cursor(ctx, cursorScope())
	if err != nil {
		t.Fatalf("cursor after other save: %v", err)
	}
	if got != "offset-99" {
		t.Errorf("cursor leaked across installations: %q", got)
	}

	if err := store.Save(ctx, cursorScope(), ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, err = store.Cursor(ctx, cursorScope())
	if err != nil {
		t.Fatalf("cursor after clear: %v", err)
	}
	if got != "" {
		t.Errorf("cursor after clear = %q, want empty", got)
	}
	otherGot, err := store.Cursor(ctx, other)
	if err != nil || otherGot != "offset-other" {
		t.Errorf("other installation cursor = %q (%v), want offset-other", otherGot, err)
	}
}

func TestCursorSaveFirstReadError(t *testing.T) {
	db := newChannelTestDB(t)
	store := NewCursorStore(db)
	if err := db.Callback().Query().Before("gorm:query").Register("cursor_read_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if err := store.Save(context.Background(), cursorScope(), "offset-1"); err == nil {
		t.Error("save: expected read error")
	}
}

func TestCursorSaveUpdateError(t *testing.T) {
	db := newChannelTestDB(t)
	store := NewCursorStore(db)
	ctx := context.Background()
	if err := store.Save(ctx, cursorScope(), "offset-1"); err != nil {
		t.Fatalf("save first: %v", err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("cursor_update_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if err := store.Save(ctx, cursorScope(), "offset-2"); err == nil {
		t.Error("save overwrite: expected update error")
	}
}

func TestCursorSaveCreateError(t *testing.T) {
	db := newChannelTestDB(t)
	store := NewCursorStore(db)
	if err := db.Callback().Create().Before("gorm:create").Register("cursor_create_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if err := store.Save(context.Background(), cursorScope(), "offset-1"); err == nil {
		t.Error("save: expected create error")
	}
}

func TestCursorClearDeleteError(t *testing.T) {
	db := newChannelTestDB(t)
	store := NewCursorStore(db)
	ctx := context.Background()
	if err := store.Save(ctx, cursorScope(), "offset-1"); err != nil {
		t.Fatalf("save first: %v", err)
	}
	if err := db.Callback().Delete().Before("gorm:delete").Register("cursor_delete_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if err := store.Save(ctx, cursorScope(), ""); err == nil {
		t.Error("clear: expected delete error")
	}
}

func TestCursorLoadQueryError(t *testing.T) {
	db := newChannelTestDB(t)
	store := NewCursorStore(db)
	if err := db.Callback().Query().Before("gorm:query").Register("cursor_load_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if _, err := store.Cursor(context.Background(), cursorScope()); err == nil {
		t.Error("cursor: expected query error")
	}
}
