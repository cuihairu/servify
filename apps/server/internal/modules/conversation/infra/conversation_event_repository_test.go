package infra

// Service Timeline 事件流水的仓储行为（V1.0 收敛 B1）：投影写入带
// tenant/workspace 作用域字段、按会话新→旧读取、limit/offset 分页、
// 错误直传。sqlite 内存库自足。

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/conversation/domain"
	"servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newConversationEventDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.ConversationEvent{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestConversationEventRepositoryAppendScopesAndLists(t *testing.T) {
	db := newConversationEventDB(t)
	repo := NewConversationEventRepository(db)
	if repo == nil {
		t.Fatal("constructor returned nil")
	}

	base := time.Now()
	scoped := auth.ContextWithScope(context.Background(), "tenant-1", "ws-1")
	// 两条事件：旧在前写，读出应新→旧。
	for i, event := range []domain.ConversationEvent{
		{ID: 1, ConversationID: "conv-1", EventType: "conversation.created", ActorType: "system", OccurredAt: base},
		{ID: 2, ConversationID: "conv-1", EventType: "routing.agent_assigned", ActorType: "routing", OccurredAt: base.Add(time.Minute)},
	} {
		if err := repo.Append(scoped, event); err != nil {
			t.Fatalf("append #%d: %v", i, err)
		}
	}

	var row models.ConversationEvent
	if err := db.Where("id = ?", 1).First(&row).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if row.TenantID != "tenant-1" || row.WorkspaceID != "ws-1" {
		t.Fatalf("scope fields missing: %+v", row)
	}

	events, err := repo.ListByConversation(scoped, "conv-1", 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 2 || events[0].ID != 2 || events[1].ID != 1 {
		t.Fatalf("order should be newest-first: %+v", events)
	}
	if events[0].EventType != "routing.agent_assigned" || events[0].ActorType != "routing" {
		t.Fatalf("projection fields wrong: %+v", events[0])
	}

	// limit + offset 分页。
	paged, err := repo.ListByConversation(scoped, "conv-1", 1, 1)
	if err != nil {
		t.Fatalf("paged list: %v", err)
	}
	if len(paged) != 1 || paged[0].ID != 1 {
		t.Fatalf("page window wrong: %+v", paged)
	}

	// 其他会话不串。
	if others, _ := repo.ListByConversation(scoped, "conv-2", 10, 0); len(others) != 0 {
		t.Fatalf("cross conversation leak: %+v", others)
	}
}

func TestConversationEventRepositoryAppendWithoutScope(t *testing.T) {
	db := newConversationEventDB(t)
	repo := NewConversationEventRepository(db)
	if err := repo.Append(context.Background(), domain.ConversationEvent{
		ID: 9, ConversationID: "conv-1", EventType: "x", OccurredAt: time.Now(),
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	var row models.ConversationEvent
	if err := db.First(&row, "id = ?", 9).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
}

func TestConversationEventScopeFieldsNilModelNoop(t *testing.T) {
	applyConversationEventScopeFields(context.Background(), nil) // 不 panic 即可
}

func TestConversationEventRepositoryErrorsPropagate(t *testing.T) {
	db := newConversationEventDB(t)
	repo := NewConversationEventRepository(db)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := repo.Append(context.Background(), domain.ConversationEvent{
		ID: 1, ConversationID: "conv-1", OccurredAt: time.Now(),
	}); err == nil {
		t.Fatal("append on closed db must fail")
	}
	if _, err := repo.ListByConversation(context.Background(), "conv-1", 10, 0); err == nil {
		t.Fatal("list on closed db must fail")
	}
}
