package delivery

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationinfra "servify/apps/server/internal/modules/conversation/infra"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newVisitorMessagesEnv(t *testing.T, sessionID string, messageCount int) (*VisitorMessagesAdapter, *gorm.DB) {
	t.Helper()
	// 先例口径（runtime_adapter_test.go）：file: 前缀 + 用例名——无前缀的
	// DSN 会被 modernc 当磁盘路径落库（误产物 + 重跑撞 UNIQUE）。
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:"+t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Session{}, &models.Message{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now()
	if err := db.Create(&models.Session{
		ID: sessionID, TenantID: "tenant-a", WorkspaceID: "ws-1",
		Status: "active", Platform: "chat", StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	for i := 0; i < messageCount; i++ {
		if err := db.Create(&models.Message{
			TenantID: "tenant-a", WorkspaceID: "ws-1", SessionID: sessionID,
			Sender: "agent", Type: "text", Content: "m" + strconv.Itoa(i), CreatedAt: now.Add(time.Duration(i) * time.Second),
		}).Error; err != nil {
			t.Fatalf("seed message %d: %v", i, err)
		}
	}
	repo := conversationinfra.NewGormRepository(db)
	service := conversationapp.NewService(repo, nil)
	return NewVisitorMessagesAdapter(service, db), db
}

// TestVisitorMessagesAdapterIncrementalPage 锚定游标分页：limit+1 探测
// has_more、末条续拉到空增量收口。
func TestVisitorMessagesAdapterIncrementalPage(t *testing.T) {
	adapter, db := newVisitorMessagesEnv(t, "sess-1", 4)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")

	// 空游标：全量 + has_more=false（limit 未触顶）
	page, err := adapter.ListMessagesAfter(ctx, "sess-1", "", 10)
	if err != nil {
		t.Fatalf("full page: %v", err)
	}
	if len(page.Messages) != 4 || page.HasMore {
		t.Fatalf("unexpected full page: %d msgs has_more=%v", len(page.Messages), page.HasMore)
	}

	// limit+1 探测：limit 2 → 2 条 + has_more
	paged, err := adapter.ListMessagesAfter(ctx, "sess-1", "", 2)
	if err != nil {
		t.Fatalf("paged: %v", err)
	}
	if len(paged.Messages) != 2 || !paged.HasMore {
		t.Fatalf("expected 2 msgs + has_more, got %d/%v", len(paged.Messages), paged.HasMore)
	}

	// 以末条为游标续拉：剩余 2 条，无更多
	cursor := paged.Messages[len(paged.Messages)-1].ID
	rest, err := adapter.ListMessagesAfter(ctx, "sess-1", cursor, 10)
	if err != nil {
		t.Fatalf("rest page: %v", err)
	}
	if len(rest.Messages) != 2 || rest.HasMore {
		t.Fatalf("expected 2 rest msgs, got %d/%v", len(rest.Messages), rest.HasMore)
	}

	// 最新游标：空增量（200 语义在 handler 层）
	var latestRow models.Message
	if err := db.Where("session_id = ?", "sess-1").Order("id DESC").First(&latestRow).Error; err != nil {
		t.Fatalf("latest: %v", err)
	}
	empty, err := adapter.ListMessagesAfter(ctx, "sess-1", strconv.FormatUint(uint64(latestRow.ID), 10), 10)
	if err != nil {
		t.Fatalf("empty increment: %v", err)
	}
	if len(empty.Messages) != 0 || empty.HasMore {
		t.Fatalf("expected empty increment, got %d/%v", len(empty.Messages), empty.HasMore)
	}
}

// TestVisitorMessagesAdapterSessionBranches 锚定会话存在性区分（404 vs
// 200 空页）与游标校验透传。
func TestVisitorMessagesAdapterSessionBranches(t *testing.T) {
	adapter, _ := newVisitorMessagesEnv(t, "sess-1", 1)
	ctx := context.Background()

	// 会话不存在：404 语义（与「没有新消息」的 200 空页区分）
	if _, err := adapter.ListMessagesAfter(ctx, "sess-404", "", 10); err == nil || !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("expected session not found, got %v", err)
	}

	// 会话存在：游标非法透传 service 错误
	if _, err := adapter.ListMessagesAfter(ctx, "sess-1", "abc", 10); err == nil || !strings.Contains(err.Error(), "invalid message cursor") {
		t.Fatalf("expected invalid cursor, got %v", err)
	}
}

// TestVisitorMessagesAdapterSessionLookupFailure 锚定查询错误透传
// （非 NotFound 的失败分支）。
func TestVisitorMessagesAdapterSessionLookupFailure(t *testing.T) {
	adapter, db := newVisitorMessagesEnv(t, "sess-1", 1)
	if err := db.Migrator().DropTable(&models.Session{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if _, err := adapter.ListMessagesAfter(context.Background(), "sess-1", "", 10); err == nil || !strings.Contains(err.Error(), "session lookup failed") {
		t.Fatalf("expected lookup failure, got %v", err)
	}
}
