package server

// 转接目标校验适配器（并发转接 gate 的组合根面）：直查 agents 表区分
// 404/403，可接判定对齐 routing 手动转接口径（非 offline 且未满载）。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTransferValidatorDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:transfer_validator_"+strings.ReplaceAll(t.Name(), "/", "_")+"")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Agent{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestAgentTransferTargetValidatorSemantics(t *testing.T) {
	db := newTransferValidatorDB(t)
	validator := agentTransferTargetValidator{db: db}
	ctx := context.Background()

	online := models.Agent{TenantID: "tenant-1", WorkspaceID: "ws-1", UserID: 7, Status: "online", MaxConcurrent: 5, CurrentLoad: 2}
	// MaxConcurrent=0（未配置并发上限）：跳过容量判定，仅按在场状态判可接。
	// 列默认值 5 会吞掉零值，故 seed 后用 UpdateColumn 显式写 0。
	noLimit := models.Agent{TenantID: "tenant-1", WorkspaceID: "ws-1", UserID: 13, Status: "busy", CurrentLoad: 9}
	busy := models.Agent{TenantID: "tenant-1", WorkspaceID: "ws-1", UserID: 8, Status: "busy", MaxConcurrent: 5, CurrentLoad: 3}
	atCapacity := models.Agent{TenantID: "tenant-1", WorkspaceID: "ws-1", UserID: 9, Status: "busy", MaxConcurrent: 5, CurrentLoad: 5}
	offline := models.Agent{TenantID: "tenant-1", WorkspaceID: "ws-1", UserID: 10, Status: "offline", MaxConcurrent: 5}
	otherTenant := models.Agent{TenantID: "tenant-2", WorkspaceID: "ws-1", UserID: 11, Status: "online", MaxConcurrent: 5}
	otherWorkspace := models.Agent{TenantID: "tenant-1", WorkspaceID: "ws-2", UserID: 12, Status: "online", MaxConcurrent: 5}
	for _, agent := range []models.Agent{online, busy, atCapacity, offline, otherTenant, otherWorkspace, noLimit} {
		row := agent
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed agent %d: %v", agent.UserID, err)
		}
	}
	if err := db.Model(&models.Agent{}).Where("user_id = ?", noLimit.UserID).
		UpdateColumn("max_concurrent", 0).Error; err != nil {
		t.Fatalf("clear concurrency limit: %v", err)
	}

	if err := validator.ValidateTransferTarget(ctx, 7, "tenant-1", "ws-1"); err != nil {
		t.Fatalf("online agent must be transferable, got %v", err)
	}
	if err := validator.ValidateTransferTarget(ctx, 13, "tenant-1", "ws-1"); err != nil {
		t.Fatalf("agent without concurrency limit skips capacity check, got %v", err)
	}
	if err := validator.ValidateTransferTarget(ctx, 8, "tenant-1", "ws-1"); err != nil {
		t.Fatalf("busy agent with capacity must be transferable, got %v", err)
	}
	if err := validator.ValidateTransferTarget(ctx, 9, "tenant-1", "ws-1"); !errors.Is(err, conversationapp.ErrTransferTargetUnavailable) {
		t.Fatalf("at-capacity agent must be unavailable, got %v", err)
	}
	if err := validator.ValidateTransferTarget(ctx, 10, "tenant-1", "ws-1"); !errors.Is(err, conversationapp.ErrTransferTargetUnavailable) {
		t.Fatalf("offline agent must be unavailable, got %v", err)
	}
	if err := validator.ValidateTransferTarget(ctx, 11, "tenant-1", "ws-1"); !errors.Is(err, conversationapp.ErrTransferTargetOutOfScope) {
		t.Fatalf("cross-tenant agent must be out of scope, got %v", err)
	}
	if err := validator.ValidateTransferTarget(ctx, 12, "tenant-1", "ws-1"); !errors.Is(err, conversationapp.ErrTransferTargetOutOfScope) {
		t.Fatalf("cross-workspace agent must be out of scope, got %v", err)
	}
	if err := validator.ValidateTransferTarget(ctx, 999, "tenant-1", "ws-1"); !errors.Is(err, conversationapp.ErrTransferTargetNotFound) {
		t.Fatalf("missing agent must be not found, got %v", err)
	}

	// DB 错误原样上抛（非哨兵），交付层回落 500。
	if err := db.Migrator().DropTable("agents"); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	err := validator.ValidateTransferTarget(ctx, 7, "tenant-1", "ws-1")
	if err == nil || errors.Is(err, conversationapp.ErrTransferTargetNotFound) {
		t.Fatalf("db failure must not collapse to not-found sentinel, got %v", err)
	}
}
