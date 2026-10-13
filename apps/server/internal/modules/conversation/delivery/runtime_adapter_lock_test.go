package delivery

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

// LockedTransferAgentID 回归（转派负载竞态：坐席负载增减以事务内锁行的库值为准，
// 不以调用方事务外预读的会话 agent_id 为准）。

func TestLockedTransferAgentIDRequiresTX(t *testing.T) {
	adapter := NewRuntimeAdapter(newRuntimeAdapterTestDB(t, "niltx"), nil)
	if _, err := adapter.LockedTransferAgentID(context.Background(), nil, "sess-1"); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("expected gorm.ErrInvalidDB, got %v", err)
	}
}

func TestLockedTransferAgentIDEmptySessionID(t *testing.T) {
	adapter := NewRuntimeAdapter(newRuntimeAdapterTestDB(t, "empty"), nil)
	agentID, err := adapter.LockedTransferAgentID(context.Background(), adapter.db, "")
	if err != nil {
		t.Fatalf("empty session id must not touch the DB: %v", err)
	}
	if agentID != nil {
		t.Fatalf("empty session id must return nil, got %v", *agentID)
	}
}

func TestLockedTransferAgentIDReadsDBValue(t *testing.T) {
	db := newRuntimeAdapterTestDB(t, "dbvalue")
	adapter := NewRuntimeAdapter(db, nil)
	ctx := context.Background()

	agentID := uint(11)
	seedRuntimeSession(t, db, models.Session{ID: "sess-assigned", UserID: 5, AgentID: &agentID, Status: "active"})
	seedRuntimeSession(t, db, models.Session{ID: "sess-unassigned", UserID: 5, Status: "active"})

	got, err := adapter.LockedTransferAgentID(ctx, db, "sess-assigned")
	if err != nil {
		t.Fatalf("locked read: %v", err)
	}
	if got == nil || *got != 11 {
		t.Fatalf("agent id = %v, want 11", got)
	}

	// 会话未指派（agent_id 为空）返回 nil，不报错。
	got, err = adapter.LockedTransferAgentID(ctx, db, "sess-unassigned")
	if err != nil {
		t.Fatalf("locked read unassigned: %v", err)
	}
	if got != nil {
		t.Fatalf("unassigned session agent = %v, want nil", *got)
	}

	// 会话行不存在报 ErrRecordNotFound（调用方按未查到处理）。
	if _, err = adapter.LockedTransferAgentID(ctx, db, "sess-missing"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing session must return ErrRecordNotFound, got %v", err)
	}
}

func TestLockedTransferAgentIDRespectsScope(t *testing.T) {
	db := newRuntimeAdapterTestDB(t, "scope")
	adapter := NewRuntimeAdapter(db, nil)

	seedRuntimeSession(t, db, models.Session{ID: "sess-scope", TenantID: "tenant-b", WorkspaceID: "workspace-b", UserID: 5, Status: "active"})

	// 作用域上下文读不中别的租户的行。
	scoped := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-a")
	if _, err := adapter.LockedTransferAgentID(scoped, db, "sess-scope"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("scoped read must not cross tenant boundaries, got %v", err)
	}

	got, err := adapter.LockedTransferAgentID(platformauth.ContextWithScope(context.Background(), "tenant-b", "workspace-b"), db, "sess-scope")
	if err != nil {
		t.Fatalf("in-scope read: %v", err)
	}
	if got != nil {
		t.Fatalf("unassigned agent = %v, want nil", *got)
	}
}
