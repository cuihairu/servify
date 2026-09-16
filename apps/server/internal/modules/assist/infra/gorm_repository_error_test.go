package infra

import (
	"context"
	assistdomain "servify/apps/server/internal/modules/assist/domain"
	"strings"
	"testing"
)

// TestGormRepositoryGetAnnotationRoundTrip 覆盖 GetAnnotation 的命中与未命中分支。
func TestGormRepositoryGetAnnotationRoundTrip(t *testing.T) {
	db := newAssistUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	if _, err := repo.GetAnnotation(ctx, 7); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing annotation error = %v, want record not found", err)
	}

	annotation := &assistdomain.RemoteAssistAnnotation{AssistSessionID: 1, Shape: "rect", Payload: "{}"}
	if err := repo.CreateAnnotation(ctx, annotation); err != nil {
		t.Fatalf("CreateAnnotation() error = %v", err)
	}
	got, err := repo.GetAnnotation(ctx, annotation.ID)
	if err != nil {
		t.Fatalf("GetAnnotation() error = %v", err)
	}
	if got.ID != annotation.ID || got.Shape != "rect" || got.AssistSessionID != 1 {
		t.Fatalf("unexpected annotation: %+v", got)
	}
}

// TestGormRepositoryQueryErrorBranches 用删表触发 Find 错误分支。
func TestGormRepositoryQueryErrorBranches(t *testing.T) {
	db := newAssistUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	if err := db.Migrator().DropTable(&assistdomain.RemoteAssistSession{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if _, err := repo.ListSessions(ctx, "", 10); err == nil {
		t.Fatal("ListSessions() on dropped table should fail")
	}

	if err := db.Migrator().DropTable(&assistdomain.RemoteAssistAnnotation{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if _, err := repo.ListAnnotations(ctx, 1); err == nil {
		t.Fatal("ListAnnotations() on dropped table should fail")
	}
	if _, err := repo.GetAnnotation(ctx, 1); err == nil {
		t.Fatal("GetAnnotation() on dropped table should fail")
	}
	if err := repo.DeleteAnnotation(ctx, 1); err == nil {
		t.Fatal("DeleteAnnotation() on dropped table should fail")
	}
}
