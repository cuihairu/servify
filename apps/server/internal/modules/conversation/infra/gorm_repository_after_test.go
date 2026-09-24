package infra

import (
	"context"
	"strconv"
	"testing"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"
)

// TestGormConversationRepositoryListMessagesAfter 锚定访客增量补拉（§10 #1）
// 的游标语义：消息 ID 单调升序、空游标从头、租户 scope 隔离、契约性拒绝
// 非法游标。
func TestGormConversationRepositoryListMessagesAfter(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")

	seedMessages(t, db, 5, "conv-1", "tenant-a", "ws-1")
	seedMessages(t, db, 1, "conv-1", "tenant-b", "ws-2")

	// 空游标：从会话头拉全量（本租户 scope，跨租户隔离），ID 升序
	items, err := repo.ListMessagesAfter(ctx, "conv-1", "", 10)
	if err != nil {
		t.Fatalf("list from head: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(items))
	}
	for i := 1; i < len(items); i++ {
		prev, _ := strconv.Atoi(items[i-1].ID)
		cur, _ := strconv.Atoi(items[i].ID)
		if cur <= prev {
			t.Fatalf("expected ascending id order, got %s then %s", items[i-1].ID, items[i].ID)
		}
	}

	// 游标 = 第 2 条：只拉其后 3 条
	pivot := items[1]
	tail, err := repo.ListMessagesAfter(ctx, "conv-1", pivot.ID, 10)
	if err != nil {
		t.Fatalf("list after pivot: %v", err)
	}
	if len(tail) != 3 || tail[0].ID == pivot.ID {
		t.Fatalf("expected 3 messages after pivot %s, got %d (first %s)", pivot.ID, len(tail), tail[0].ID)
	}

	// 非数字游标：契约性拒绝
	if _, err := repo.ListMessagesAfter(ctx, "conv-1", "abc", 10); err == nil || err.Error() != "invalid message cursor: abc" {
		t.Fatalf("expected invalid cursor error, got %v", err)
	}
	// 负数游标：同样拒绝
	if _, err := repo.ListMessagesAfter(ctx, "conv-1", "-1", 10); err == nil || err.Error() != "invalid message cursor: -1" {
		t.Fatalf("expected invalid cursor error for negative, got %v", err)
	}

	// 游标带空白：TrimSpace 后合法
	trimmed, err := repo.ListMessagesAfter(ctx, "conv-1", " "+pivot.ID+" ", 10)
	if err != nil {
		t.Fatalf("trimmed cursor: %v", err)
	}
	if len(trimmed) != 3 {
		t.Fatalf("expected 3 messages for trimmed cursor, got %d", len(trimmed))
	}

	// limit 截断与默认值
	capped, err := repo.ListMessagesAfter(ctx, "conv-1", "", 2)
	if err != nil {
		t.Fatalf("limit capped: %v", err)
	}
	if len(capped) != 2 {
		t.Fatalf("expected 2 messages with limit, got %d", len(capped))
	}
	if _, err := repo.ListMessagesAfter(ctx, "conv-1", "", 0); err != nil {
		t.Fatalf("default limit: %v", err)
	}

	// 超前游标：空列表不报错（幂等读）
	ahead, err := repo.ListMessagesAfter(ctx, "conv-1", "999999", 10)
	if err != nil {
		t.Fatalf("ahead cursor: %v", err)
	}
	if len(ahead) != 0 {
		t.Fatalf("expected empty page, got %d", len(ahead))
	}
}

// TestGormConversationRepositoryListMessagesAfterLookupError 锚定查询错误透传。
func TestGormConversationRepositoryListMessagesAfterLookupError(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")

	if err := db.Migrator().DropTable(&models.Message{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if _, err := repo.ListMessagesAfter(ctx, "conv-1", "", 10); err == nil {
		t.Fatal("expected query error after table drop")
	}
}
