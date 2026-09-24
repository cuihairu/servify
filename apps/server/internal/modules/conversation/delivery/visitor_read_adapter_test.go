package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationinfra "servify/apps/server/internal/modules/conversation/infra"
)

func newReadAdapterDB(t *testing.T) (*VisitorReadAdapter, *gorm.DB, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:"+t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Session{}, &models.Message{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	// 会话行（首条消息持久化时建）+ 三条消息：1=customer 上行、2=agent 回复、
	// 3=system 提示（未读口径只计 2/3）。
	now := time.Now()
	if err := db.Create(&models.Session{ID: "sess-r1", Platform: "web", StartedAt: now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	for _, m := range []models.Message{
		{ID: 1, SessionID: "sess-r1", Sender: "customer", Content: "hi", CreatedAt: now},
		{ID: 2, SessionID: "sess-r1", Sender: "agent", Content: "reply", CreatedAt: now},
		{ID: 3, SessionID: "sess-r1", Sender: "system", Content: "hint", CreatedAt: now},
	} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("seed message %d: %v", m.ID, err)
		}
	}
	repo := conversationinfra.NewGormRepository(db)
	service := conversationapp.NewService(repo, nil)
	return NewVisitorReadAdapter(service, db), db, "sess-r1"
}

// TestVisitorReadAdapter_MarkReadAndUnread 闭环：初始未读 2（agent+system，
// customer 上行不计）；推进游标到 2 后未读归 1（只剩 system）。
func TestVisitorReadAdapter_MarkReadAndUnread(t *testing.T) {
	adapter, _, sessionID := newReadAdapterDB(t)
	ctx := context.Background()

	state, err := adapter.UnreadState(ctx, sessionID)
	if err != nil {
		t.Fatalf("unread: %v", err)
	}
	if state.UnreadCount != 2 || state.LastReadMessageID != "0" {
		t.Fatalf("initial state = %+v, want unread=2 cursor=0", state)
	}

	state, err = adapter.MarkRead(ctx, sessionID, "2")
	if err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if state.UnreadCount != 1 || state.LastReadMessageID != "2" {
		t.Fatalf("after mark = %+v, want unread=1 cursor=2", state)
	}
}

// TestVisitorReadAdapter_Rejections 锚定拒绝面：会话不存在 404 口径、
// 游标不存在/跨会话 400 口径、游标只前进（回退推进不报错但游标不动）。
func TestVisitorReadAdapter_Rejections(t *testing.T) {
	adapter, _, sessionID := newReadAdapterDB(t)
	ctx := context.Background()

	if _, err := adapter.UnreadState(ctx, "sess-missing"); err == nil || err.Error() != "session not found: sess-missing" {
		t.Fatalf("missing session err = %v", err)
	}
	if _, err := adapter.MarkRead(ctx, sessionID, "999"); err == nil || err.Error() != "invalid message cursor: 999" {
		t.Fatalf("missing cursor err = %v", err)
	}
	// 推进到 2 后，再提交更小的游标（1）：消息存在但已读游标不回退。
	if _, err := adapter.MarkRead(ctx, sessionID, "2"); err != nil {
		t.Fatalf("mark 2: %v", err)
	}
	state, err := adapter.MarkRead(ctx, sessionID, "1")
	if err != nil {
		t.Fatalf("late smaller cursor must be tolerated: %v", err)
	}
	if state.LastReadMessageID != "2" || state.UnreadCount != 1 {
		t.Fatalf("cursor regressed: %+v", state)
	}
}

// TestVisitorReadAdapter_AdapterErrorBranches 覆盖适配层剩余错误分支：
// MarkRead 前置会话校验、未读计数失败透传、会话查询底层故障。
func TestVisitorReadAdapter_AdapterErrorBranches(t *testing.T) {
	adapter, db, sessionID := newReadAdapterDB(t)
	ctx := context.Background()

	// MarkRead 对不存在会话：requireSession 前置拒绝（404 口径）
	if _, err := adapter.MarkRead(ctx, "sess-missing", "1"); err == nil || err.Error() != "session not found: sess-missing" {
		t.Fatalf("mark read missing session err = %v", err)
	}

	// 计数查询失败（messages 表缺失）：透传 service 错误
	if err := db.Migrator().DropTable(&models.Message{}); err != nil {
		t.Fatalf("drop messages: %v", err)
	}
	if _, err := adapter.UnreadState(ctx, sessionID); err == nil {
		t.Fatal("expected unread count error passthrough")
	}

	// 会话查询底层故障（sessions 表缺失）：session lookup failed 口径
	if err := db.Migrator().DropTable(&models.Session{}); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if _, err := adapter.UnreadState(ctx, sessionID); err == nil || !strings.HasPrefix(err.Error(), "session lookup failed") {
		t.Fatalf("expected lookup failed error, got %v", err)
	}
}
