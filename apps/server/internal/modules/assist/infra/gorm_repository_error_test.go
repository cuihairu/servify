package infra

import (
	"context"
	assistdomain "servify/apps/server/internal/modules/assist/domain"
	"testing"
)

// TestGormRepositoryCreateAnnotationRoundTrip 覆盖标注落库并经 ListAnnotations 读回对账。
func TestGormRepositoryCreateAnnotationRoundTrip(t *testing.T) {
	db := newAssistUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	annotation := &assistdomain.RemoteAssistAnnotation{AssistSessionID: 1, Shape: "rect", Payload: "{}"}
	if err := repo.CreateAnnotation(ctx, annotation); err != nil {
		t.Fatalf("CreateAnnotation() error = %v", err)
	}
	list, err := repo.ListAnnotations(ctx, 1)
	if err != nil {
		t.Fatalf("ListAnnotations() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != annotation.ID || list[0].Shape != "rect" || list[0].AssistSessionID != 1 {
		t.Fatalf("unexpected annotations: %+v", list)
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
	if err := repo.DeleteAnnotation(ctx, 1); err == nil {
		t.Fatal("DeleteAnnotation() on dropped table should fail")
	}
}
