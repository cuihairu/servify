package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

// TestMacroServiceApplyToTicketPaths 覆盖宏应用的四条路径：宏缺失、宏停用、
// 工单缺失、成功落评论。
func TestMacroServiceApplyToTicketPaths(t *testing.T) {
	svc, db := newMacroTestService(t)
	ctx := context.Background()

	// 宏缺失
	if _, err := svc.ApplyToTicket(ctx, 4242, 1, 9); err == nil {
		t.Fatal("expected error for missing macro")
	}

	active := &models.Macro{Name: "greet", Content: "hello world", Active: true, Language: "zh"}
	if err := db.Create(active).Error; err != nil {
		t.Fatalf("seed active macro: %v", err)
	}
	inactive := &models.Macro{Name: "stale", Content: "old", Active: false, Language: "zh"}
	if err := db.Create(inactive).Error; err != nil {
		t.Fatalf("seed inactive macro: %v", err)
	}
	// Active 带 default:true，零值插入会被默认值覆盖，显式改回停用
	if err := db.Model(&models.Macro{}).Where("id = ?", inactive.ID).Update("active", false).Error; err != nil {
		t.Fatalf("deactivate macro: %v", err)
	}

	// 宏停用
	if _, err := svc.ApplyToTicket(ctx, inactive.ID, 1, 9); err == nil || !strings.Contains(err.Error(), "macro inactive") {
		t.Fatalf("inactive macro err = %v, want macro inactive", err)
	}

	// 工单缺失（宏有效）
	if _, err := svc.ApplyToTicket(ctx, active.ID, 4242, 9); err == nil || !strings.Contains(err.Error(), "ticket not found") {
		t.Fatalf("missing ticket err = %v, want ticket not found", err)
	}

	// 成功
	ticket := &models.Ticket{Title: "t", Status: "open", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	comment, err := svc.ApplyToTicket(ctx, active.ID, ticket.ID, 9)
	if err != nil {
		t.Fatalf("ApplyToTicket() error = %v", err)
	}
	if comment.TicketID != ticket.ID || comment.Content != "hello world" || comment.Type != "system" {
		t.Fatalf("unexpected comment: %+v", comment)
	}
	var persisted models.TicketComment
	if err := db.First(&persisted, comment.ID).Error; err != nil {
		t.Fatalf("comment not persisted: %v", err)
	}
	if persisted.UserID != 9 {
		t.Fatalf("comment actor = %d, want 9", persisted.UserID)
	}
}

// TestMacroServiceDeleteError 覆盖删除失败（表不存在）时的错误透传。
func TestMacroServiceDeleteError(t *testing.T) {
	svc, db := newMacroTestService(t)
	if err := db.Migrator().DropTable("macros"); err != nil {
		t.Fatalf("drop macros: %v", err)
	}
	if err := svc.Delete(context.Background(), 1); err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("Delete() on dropped table err = %v, want driver error", err)
	}
}
