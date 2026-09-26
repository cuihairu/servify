package infra

import (
	"context"
	"errors"
	assistapp "servify/apps/server/internal/modules/assist/application"
	assistdomain "servify/apps/server/internal/modules/assist/domain"
	"testing"
)

// TestGormRepositoryCreateAnnotationRoundTrip 覆盖标注落库并经 ListAnnotations 读回对账。
// 标注查询带 sessions 子查询守卫，先落父会话再挂标注。
func TestGormRepositoryCreateAnnotationRoundTrip(t *testing.T) {
	db := newAssistUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	if err := repo.CreateSession(ctx, &assistdomain.RemoteAssistSession{ID: 1, ConversationSessionID: "sess-1", Status: "active"}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
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
	if _, err := repo.FindActiveSessionIDByConversation(ctx, "sess-1"); err == nil {
		t.Fatal("FindActiveSessionIDByConversation() on dropped table should fail")
	}
	// GetSession 非 not-found 错误原样透传（not found 分支由 scope 隔离测试覆盖）
	if _, err := repo.GetSession(ctx, 1); err == nil || errors.Is(err, assistapp.ErrAssistNotFound) {
		t.Fatalf("GetSession() on dropped table = %v, want raw error", err)
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
