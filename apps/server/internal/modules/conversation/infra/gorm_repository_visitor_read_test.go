package infra

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

// newVisitorReadFixture 种 tenant-a 的会话 conv-1 与四条消息：1=customer
// 上行、2=agent 回复、3=system 提示（未读口径只计 2/3），4 是同会话 ID 下
// 跨租户（tenant-b）的 agent 消息，用于验证未读计数的租户 scope 隔离。
func newVisitorReadFixture(t *testing.T) (*GormRepository, *gorm.DB, context.Context) {
	t.Helper()
	db := newConversationUnitTestDB(t)
	now := time.Now()
	s := models.Session{ID: "conv-1", Platform: "web", TenantID: "tenant-a", WorkspaceID: "ws-1", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	for _, m := range []models.Message{
		{ID: 1, SessionID: "conv-1", TenantID: "tenant-a", WorkspaceID: "ws-1", Sender: "customer", Content: "hi", CreatedAt: now},
		{ID: 2, SessionID: "conv-1", TenantID: "tenant-a", WorkspaceID: "ws-1", Sender: "agent", Content: "reply", CreatedAt: now},
		{ID: 3, SessionID: "conv-1", TenantID: "tenant-a", WorkspaceID: "ws-1", Sender: "system", Content: "hint", CreatedAt: now},
		{ID: 4, SessionID: "conv-1", TenantID: "tenant-b", WorkspaceID: "ws-2", Sender: "agent", Content: "other tenant", CreatedAt: now},
	} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("seed message %d: %v", m.ID, err)
		}
	}
	return NewGormRepository(db), db, platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")
}

// TestGormConversationRepositoryVisitorReadFlow 锚定 §10 #3 仓储面语义：
// 未读口径只计 agent/system 且 ID 大于游标、游标只前进不回退、消息必须
// 属于该会话、租户 scope 隔离。
func TestGormConversationRepositoryVisitorReadFlow(t *testing.T) {
	repo, db, ctx := newVisitorReadFixture(t)

	// 初始：未读 2（agent+system），游标 0；跨租户消息不计
	count, cursor, err := repo.VisitorUnreadCount(ctx, "conv-1")
	if err != nil {
		t.Fatalf("initial unread: %v", err)
	}
	if count != 2 || cursor != "0" {
		t.Fatalf("initial state = count %d cursor %s, want 2/0", count, cursor)
	}

	// 推进到 2：未读归 1（只剩 system）
	if err := repo.MarkVisitorRead(ctx, "conv-1", "2"); err != nil {
		t.Fatalf("mark read 2: %v", err)
	}
	count, cursor, err = repo.VisitorUnreadCount(ctx, "conv-1")
	if err != nil {
		t.Fatalf("unread after mark: %v", err)
	}
	if count != 1 || cursor != "2" {
		t.Fatalf("after mark = count %d cursor %s, want 1/2", count, cursor)
	}

	// 回退推进（提交更小游标 1）：不报错、游标不动
	if err := repo.MarkVisitorRead(ctx, "conv-1", "1"); err != nil {
		t.Fatalf("late smaller cursor must be tolerated: %v", err)
	}
	_, cursor, _ = repo.VisitorUnreadCount(ctx, "conv-1")
	if cursor != "2" {
		t.Fatalf("cursor regressed to %s, want 2", cursor)
	}

	// 不存在的消息：契约性拒绝
	if err := repo.MarkVisitorRead(ctx, "conv-1", "999"); err == nil || err.Error() != "invalid message cursor: 999" {
		t.Fatalf("expected invalid cursor error, got %v", err)
	}
	// 游标锚定必须在会话内：种一条其他会话的消息，锚定它应被拒绝
	other := models.Message{ID: 5, SessionID: "conv-other", TenantID: "tenant-a", WorkspaceID: "ws-1", Sender: "agent", Content: "elsewhere", CreatedAt: time.Now()}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("seed other message: %v", err)
	}
	if err := repo.MarkVisitorRead(ctx, "conv-1", "5"); err == nil || err.Error() != "invalid message cursor: 5" {
		t.Fatalf("expected cross-session cursor rejection, got %v", err)
	}
}

// TestGormConversationRepositoryVisitorReadErrors 锚定错误分支透传：
// 会话不存在、pivot/计数查询失败、游标更新失败。
func TestGormConversationRepositoryVisitorReadErrors(t *testing.T) {
	repo, db, ctx := newVisitorReadFixture(t)

	// 会话不存在：透传 gorm 未命中错误
	if _, _, err := repo.VisitorUnreadCount(ctx, "conv-missing"); err == nil {
		t.Fatal("expected missing session error")
	}

	// 游标查询失败（messages 表缺失）
	if err := db.Migrator().DropTable(&models.Message{}); err != nil {
		t.Fatalf("drop messages: %v", err)
	}
	if err := repo.MarkVisitorRead(ctx, "conv-1", "2"); err == nil {
		t.Fatal("expected pivot lookup error")
	}
	if _, _, err := repo.VisitorUnreadCount(ctx, "conv-1"); err == nil {
		t.Fatal("expected count query error")
	}
}

// TestGormConversationRepositoryMarkVisitorReadUpdateError 锚定游标更新
// 失败分支：pivot 查询成功但 sessions 表更新失败。
func TestGormConversationRepositoryMarkVisitorReadUpdateError(t *testing.T) {
	repo, db, ctx := newVisitorReadFixture(t)

	if err := db.Migrator().DropTable(&models.Session{}); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if err := repo.MarkVisitorRead(ctx, "conv-1", "2"); err == nil {
		t.Fatal("expected update error after sessions table drop")
	}
}
