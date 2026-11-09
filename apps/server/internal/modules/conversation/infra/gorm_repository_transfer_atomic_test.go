package infra

// TransferAgentAtomic 的 CAS 行为（并发转接 gate）：预期坐席命中才更新、
// 败者 RowsAffected=false、无坐席预期要求 IS NULL、作用域外会话不可转。

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/conversation/domain"
	platformauth "servify/apps/server/internal/platform/auth"
)

func TestGormTransferAgentAtomicHitMissAndScope(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	agentID := uint(9)
	session := models.Session{
		ID:          "conv-ta",
		UserID:      7,
		AgentID:     &agentID,
		Status:      "active",
		Platform:    "web",
		TenantID:    "tenant-1",
		WorkspaceID: "ws-1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	scoped := platformauth.ContextWithScope(ctx, "tenant-1", "ws-1")

	// CAS 命中：当前坐席等于预期 → agent_id/status 一并更新。
	hit, err := repo.TransferAgentAtomic(scoped, "conv-ta", &agentID, 11, domain.ConversationStatusTransferred)
	if err != nil {
		t.Fatalf("atomic transfer: %v", err)
	}
	if !hit {
		t.Fatal("expected CAS hit")
	}
	var stored models.Session
	if err := db.First(&stored, "id = ?", "conv-ta").Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if stored.AgentID == nil || *stored.AgentID != 11 || stored.Status != "transferred" {
		t.Fatalf("assignment not updated: %+v", stored)
	}

	// CAS 未命中：预期坐席 9 已被并发改为 11 → false，库内不再变化。
	stale := uint(9)
	miss, err := repo.TransferAgentAtomic(scoped, "conv-ta", &stale, 12, domain.ConversationStatusTransferred)
	if err != nil {
		t.Fatalf("atomic transfer miss: %v", err)
	}
	if miss {
		t.Fatal("expected CAS miss on stale expectation")
	}
	if err := db.First(&stored, "id = ?", "conv-ta").Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.AgentID == nil || *stored.AgentID != 11 {
		t.Fatalf("loser must not change assignment, got %+v", stored.AgentID)
	}

	// 预期无坐席：agent_id 非空时 IS NULL 谓词不命中。
	nilExpectMiss, err := repo.TransferAgentAtomic(scoped, "conv-ta", nil, 12, domain.ConversationStatusTransferred)
	if err != nil || nilExpectMiss {
		t.Fatalf("expected miss for non-null agent, got hit=%v err=%v", nilExpectMiss, err)
	}

	// 跨作用域：请求作用域与行不符时不可转（租户隔离）。
	crossScoped := platformauth.ContextWithScope(ctx, "tenant-2", "ws-1")
	crossHit, err := repo.TransferAgentAtomic(crossScoped, "conv-ta", &agentID, 12, domain.ConversationStatusTransferred)
	if err != nil {
		t.Fatalf("cross scope transfer: %v", err)
	}
	// 注意：stale 预期 9 也无法命中（当前 11），用当前值再验一次作用域拦截。
	hit11 := uint(11)
	crossHitCurrent, err := repo.TransferAgentAtomic(crossScoped, "conv-ta", &hit11, 12, domain.ConversationStatusTransferred)
	if err != nil || crossHitCurrent || crossHit {
		t.Fatalf("cross-scope transfer must miss: hit=%v current=%v err=%v", crossHit, crossHitCurrent, err)
	}

	// 会话不存在：miss 而非错误。
	missing, err := repo.TransferAgentAtomic(scoped, "conv-missing", &hit11, 12, domain.ConversationStatusTransferred)
	if err != nil || missing {
		t.Fatalf("expected miss for missing conversation, got hit=%v err=%v", missing, err)
	}

	// DB 故障：错误上抛（非 miss），交付层得到明确失败。
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := repo.TransferAgentAtomic(scoped, "conv-ta", &hit11, 12, domain.ConversationStatusTransferred); err == nil {
		t.Fatal("transfer on closed db must fail")
	}
}
