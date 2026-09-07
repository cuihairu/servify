package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newRuntimeAdapterTestDB(t *testing.T, shard string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"_"+shard+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Session{}, &models.Message{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func seedRuntimeSession(t *testing.T, db *gorm.DB, session models.Session) {
	t.Helper()
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func TestRuntimeAdapterLoadTransferSessionRequiresDB(t *testing.T) {
	adapter := NewRuntimeAdapter(nil, nil)
	if _, err := adapter.LoadTransferSession(context.Background(), "sess-1"); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("expected gorm.ErrInvalidDB, got %v", err)
	}
}

func TestRuntimeAdapterLoadTransferSessionBranches(t *testing.T) {
	db := newRuntimeAdapterTestDB(t, "main")
	if err := db.Create(&models.User{ID: 7, Username: "alice", Email: "alice@example.com", Name: "Alice"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	agentID := uint(9)
	ticketID := uint(3)
	seedRuntimeSession(t, db, models.Session{
		ID:          "sess-1",
		TenantID:    "tenant-a",
		WorkspaceID: "ws-1",
		UserID:      7,
		AgentID:     &agentID,
		TicketID:    &ticketID,
		Status:      "active",
		Platform:    "web",
	})

	adapter := NewRuntimeAdapter(db, nil)

	// 带作用域命中
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")
	session, err := adapter.LoadTransferSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("load transfer session: %v", err)
	}
	if session.ID != "sess-1" || session.CustomerID != 7 || session.UserName != "Alice" || session.UserUsername != "alice" {
		t.Fatalf("unexpected transfer session: %+v", session)
	}
	if session.AgentID == nil || *session.AgentID != 9 || session.TicketID == nil || *session.TicketID != 3 {
		t.Fatalf("unexpected agent/ticket: %+v", session)
	}
	if session.Status != "active" || session.Platform != "web" {
		t.Fatalf("unexpected status/platform: %+v", session)
	}

	// 作用域不匹配 → not found
	if _, err := adapter.LoadTransferSession(platformauth.ContextWithScope(context.Background(), "tenant-b", ""), "sess-1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected scoped not found, got %v", err)
	}

	// 无作用域上下文（无租户过滤）也可命中
	session, err = adapter.LoadTransferSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("load without scope: %v", err)
	}
	if session.ID != "sess-1" {
		t.Fatalf("unexpected session: %+v", session)
	}

	// 不存在的会话
	if _, err := adapter.LoadTransferSession(context.Background(), "missing"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestRuntimeAdapterSyncTransferAssignment(t *testing.T) {
	db := newRuntimeAdapterTestDB(t, "main")
	seedRuntimeSession(t, db, models.Session{
		ID:          "sess-1",
		TenantID:    "tenant-a",
		WorkspaceID: "ws-1",
		UserID:      7,
		Status:      "waiting_human",
		Platform:    "email",
	})

	fixed := time.Now().Truncate(time.Second)
	adapter := NewRuntimeAdapter(db, nil)
	adapter.now = func() time.Time { return fixed }

	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")
	if err := adapter.SyncTransferAssignment(ctx, db, "sess-1", 7, 9); err != nil {
		t.Fatalf("sync transfer assignment: %v", err)
	}

	var stored models.Session
	if err := db.First(&stored, "id = ?", "sess-1").Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if stored.Status != "active" || stored.Platform != "web" || stored.UserID != 7 {
		t.Fatalf("unexpected synced session: %+v", stored)
	}
	if stored.AgentID == nil || *stored.AgentID != 9 {
		t.Fatalf("expected agent id 9, got %+v", stored.AgentID)
	}
	if stored.UpdatedAt.Unix() != fixed.Unix() {
		t.Fatalf("expected updated_at = stub now, got %v", stored.UpdatedAt)
	}
}

func TestRuntimeAdapterSyncWaitingAssignment(t *testing.T) {
	db := newRuntimeAdapterTestDB(t, "main")
	agentID := uint(5)
	seedRuntimeSession(t, db, models.Session{
		ID:          "sess-1",
		TenantID:    "tenant-a",
		WorkspaceID: "ws-1",
		UserID:      7,
		AgentID:     &agentID,
		Status:      "active",
	})

	fixed := time.Now().Truncate(time.Second)
	adapter := NewRuntimeAdapter(db, nil)
	adapter.now = func() time.Time { return fixed }

	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")
	if err := adapter.SyncWaitingAssignment(ctx, db, "sess-1", 7); err != nil {
		t.Fatalf("sync waiting assignment: %v", err)
	}

	var stored models.Session
	if err := db.First(&stored, "id = ?", "sess-1").Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if stored.AgentID != nil {
		t.Fatalf("expected agent id cleared, got %+v", stored.AgentID)
	}
	if stored.Status != "active" || stored.UserID != 7 || stored.UpdatedAt.Unix() != fixed.Unix() {
		t.Fatalf("unexpected synced session: %+v", stored)
	}
}

func TestRuntimeAdapterSyncAssignmentErrors(t *testing.T) {
	db := newRuntimeAdapterTestDB(t, "main")
	adapter := NewRuntimeAdapter(db, nil)
	ctx := context.Background()

	if err := db.Migrator().DropTable(&models.Session{}); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if err := adapter.SyncTransferAssignment(ctx, db, "sess-1", 7, 9); err == nil {
		t.Fatal("expected sync transfer assignment error after drop")
	}
	if err := adapter.SyncWaitingAssignment(ctx, db, "sess-1", 7); err == nil {
		t.Fatal("expected sync waiting assignment error after drop")
	}
}

func TestRuntimeAdapterAppendSystemMessage(t *testing.T) {
	db := newRuntimeAdapterTestDB(t, "main")
	seedRuntimeSession(t, db, models.Session{
		ID:          "sess-1",
		TenantID:    "tenant-a",
		WorkspaceID: "ws-1",
		UserID:      7,
		Status:      "waiting_human",
	})

	createdAt := time.Now().Truncate(time.Second).Add(time.Minute)
	adapter := NewRuntimeAdapter(db, nil)

	if err := adapter.AppendSystemMessage(context.Background(), db, "sess-1", "agent assigned", createdAt); err != nil {
		t.Fatalf("append system message: %v", err)
	}

	var stored models.Message
	if err := db.First(&stored, "session_id = ?", "sess-1").Error; err != nil {
		t.Fatalf("load message: %v", err)
	}
	if stored.Sender != "system" || stored.Type != "system" || stored.Content != "agent assigned" {
		t.Fatalf("unexpected stored message: %+v", stored)
	}
	if stored.CreatedAt.Unix() != createdAt.Unix() {
		t.Fatalf("expected explicit created_at, got %v", stored.CreatedAt)
	}

	var session models.Session
	if err := db.First(&session, "id = ?", "sess-1").Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if session.Status != "active" || session.UpdatedAt.Unix() != createdAt.Unix() {
		t.Fatalf("expected session updated, got %+v", session)
	}
}

func TestRuntimeAdapterAppendSystemMessageUsesStubNowWhenZero(t *testing.T) {
	db := newRuntimeAdapterTestDB(t, "main")
	seedRuntimeSession(t, db, models.Session{ID: "sess-1", UserID: 7, Status: "active"})

	fixed := time.Now().Truncate(time.Second).Add(2 * time.Minute)
	adapter := NewRuntimeAdapter(db, nil)
	adapter.now = func() time.Time { return fixed }

	if err := adapter.AppendSystemMessage(context.Background(), db, "sess-1", "auto time", time.Time{}); err != nil {
		t.Fatalf("append system message: %v", err)
	}

	var stored models.Message
	if err := db.First(&stored, "session_id = ?", "sess-1").Error; err != nil {
		t.Fatalf("load message: %v", err)
	}
	if stored.CreatedAt.Unix() != fixed.Unix() {
		t.Fatalf("expected stub now as created_at, got %v", stored.CreatedAt)
	}
}

func TestRuntimeAdapterAppendSystemMessageErrors(t *testing.T) {
	db := newRuntimeAdapterTestDB(t, "main")
	adapter := NewRuntimeAdapter(db, nil)

	// 会话不存在
	if err := adapter.AppendSystemMessage(context.Background(), db, "missing", "x", time.Now()); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	// 写入消息失败
	seedRuntimeSession(t, db, models.Session{ID: "sess-1", UserID: 7, Status: "active"})
	if err := db.Migrator().DropTable(&models.Message{}); err != nil {
		t.Fatalf("drop messages: %v", err)
	}
	if err := adapter.AppendSystemMessage(context.Background(), db, "sess-1", "x", time.Now()); err == nil {
		t.Fatal("expected append error after drop")
	}

	// 更新会话失败（触发器中止 UPDATE）
	db2 := newRuntimeAdapterTestDB(t, "trigger")
	seedRuntimeSession(t, db2, models.Session{ID: "sess-1", UserID: 7, Status: "active"})
	if err := db2.Exec("CREATE TRIGGER fail_sessions_update BEFORE UPDATE ON sessions BEGIN SELECT RAISE(ABORT, 'forced update failure'); END").Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := adapter.AppendSystemMessage(context.Background(), db2, "sess-1", "x", time.Now()); err == nil {
		t.Fatal("expected update error from trigger")
	}
}

func TestBuildParticipantIDAndUintPtr(t *testing.T) {
	if got := buildParticipantID("agent", 9); got != "agent:9" {
		t.Fatalf("unexpected participant id %q", got)
	}
	if got := uintPtr(3); got == nil || *got != 3 {
		t.Fatalf("unexpected uint ptr %+v", got)
	}
}
