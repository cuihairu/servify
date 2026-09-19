package infra

// GormRepository 的 SQL 语义对账（自 services facade 下沉）：
// 列表排序、租户/工作区隔离、宏应用四路径与丢表/触发器错误。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	application "servify/apps/server/internal/modules/macro/application"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// scopedContext 构造带租户/工作区的上下文（驱动 applyScopeFilter）。
func scopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func newMacroTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := uniqueMemDSN("file:macro_" + strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Macro{}, &models.Ticket{}, &models.TicketComment{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func newMacroModuleService(db *gorm.DB) *application.Service {
	return application.NewService(NewGormRepository(db))
}

func macroCreateReq(name, content string) *application.MacroCreateRequest {
	return &application.MacroCreateRequest{Name: name, Content: content}
}

func stringPtr(s string) *string { return &s }

func boolPtr(b bool) *bool { return &b }

func TestMacroModuleCreateListDelete(t *testing.T) {
	db := newMacroTestDB(t)
	svc := newMacroModuleService(db)
	ctx := context.Background()

	m1, err := svc.Create(ctx, macroCreateReq("欢迎消息", "您好"))
	if err != nil {
		t.Fatalf("seed m1: %v", err)
	}
	m2, err := svc.Create(ctx, macroCreateReq("关闭消息", "再见"))
	if err != nil {
		t.Fatalf("seed m2: %v", err)
	}
	if m2.ID <= m1.ID {
		t.Fatalf("m2.ID (%d) should be > m1.ID (%d)", m2.ID, m1.ID)
	}
	if !m1.Active {
		t.Fatal("expected Active true by default")
	}

	list, err := svc.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("List() = %d items, %v", len(list), err)
	}
	if list[0].ID != m2.ID {
		t.Fatal("expected macros sorted by updated_at DESC, id DESC")
	}

	if err := svc.Delete(ctx, m1.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if list, _ = svc.List(ctx); len(list) != 1 {
		t.Fatalf("expected 1 macro after deletion, got %d", len(list))
	}
	if err := svc.Delete(ctx, 9999); !errors.Is(err, application.ErrMacroNotFound) {
		t.Fatalf("Delete() missing err = %v, want %v", err, application.ErrMacroNotFound)
	}
}

// TestMacroModuleScopedByWorkspace 对账租户/工作区隔离：跨工作区的
// 读取、更新、删除全部拒绝。
func TestMacroModuleScopedByWorkspace(t *testing.T) {
	db := newMacroTestDB(t)
	svc := newMacroModuleService(db)
	ctxA := scopedContext("tenant-a", "workspace-a")
	ctxB := scopedContext("tenant-a", "workspace-b")

	macroA, err := svc.Create(ctxA, macroCreateReq("宏-A", "A"))
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	if macroA.TenantID != "tenant-a" || macroA.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected scope on macro A: %+v", macroA)
	}
	if _, err := svc.Create(ctxB, macroCreateReq("宏-B", "B")); err != nil {
		t.Fatalf("create B: %v", err)
	}

	listA, err := svc.List(ctxA)
	if err != nil || len(listA) != 1 || listA[0].WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected scoped list: %+v, %v", listA, err)
	}

	if _, err := svc.Update(ctxB, macroA.ID, &application.MacroUpdateRequest{Content: stringPtr("cross")}); err == nil {
		t.Fatal("expected scoped update to reject cross-workspace macro")
	}
	if err := svc.Delete(ctxB, macroA.ID); err == nil {
		t.Fatal("expected scoped delete to reject cross-workspace macro")
	}
}

func TestMacroModuleUpdate(t *testing.T) {
	db := newMacroTestDB(t)
	svc := newMacroModuleService(db)
	ctx := context.Background()

	macro, err := svc.Create(ctx, macroCreateReq("原始宏", "原始内容"))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	updated, err := svc.Update(ctx, macro.ID, &application.MacroUpdateRequest{Content: stringPtr("新内容"), Language: stringPtr("en")})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Content != "新内容" || updated.Language != "en" {
		t.Fatalf("patch not applied: %+v", updated)
	}

	if _, err := svc.Update(ctx, 9999, &application.MacroUpdateRequest{Content: stringPtr("x")}); err == nil {
		t.Fatal("expected error for non-existent macro")
	}
}

// TestMacroModuleApplyToTicket 覆盖宏应用四路径：宏缺失、宏停用、
// 工单缺失、成功落评论（并验证持久化）。
func TestMacroModuleApplyToTicket(t *testing.T) {
	db := newMacroTestDB(t)
	svc := newMacroModuleService(db)
	ctx := context.Background()

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

	// 宏缺失
	if _, err := svc.ApplyToTicket(ctx, 4242, 1, 9); err == nil {
		t.Fatal("expected error for missing macro")
	}
	// 宏停用
	if _, err := svc.ApplyToTicket(ctx, inactive.ID, 1, 9); !errors.Is(err, application.ErrMacroInactive) {
		t.Fatalf("inactive macro err = %v, want %v", err, application.ErrMacroInactive)
	}
	// 工单缺失（宏有效）
	if _, err := svc.ApplyToTicket(ctx, active.ID, 4242, 9); !errors.Is(err, application.ErrTicketNotFound) {
		t.Fatalf("missing ticket err = %v, want %v", err, application.ErrTicketNotFound)
	}

	// 成功落评论并持久化
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

// TestMacroModuleApplyToTicket_DroppedTickets 覆盖工单查询驱动错误的
// 传播出口（非 RecordNotFound，不映射哨兵）。
func TestMacroModuleApplyToTicket_DroppedTickets(t *testing.T) {
	db := newMacroTestDB(t)
	svc := newMacroModuleService(db)
	ctx := context.Background()

	macro, err := svc.Create(ctx, macroCreateReq("m", "c"))
	if err != nil {
		t.Fatalf("seed macro: %v", err)
	}
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if _, err := svc.ApplyToTicket(ctx, macro.ID, 1, 1); err == nil || errors.Is(err, application.ErrTicketNotFound) {
		t.Fatalf("ApplyToTicket() on dropped tickets err = %v, want driver error", err)
	}
}

// TestMacroModuleApplyToTicket_RejectsCrossScopeTicket 对账跨工作区工单
// 的应用拒绝（工单读取走同一 scope 过滤）。
func TestMacroModuleApplyToTicket_RejectsCrossScopeTicket(t *testing.T) {
	db := newMacroTestDB(t)
	svc := newMacroModuleService(db)
	ctxA := scopedContext("tenant-a", "workspace-a")

	macroA, err := svc.Create(ctxA, macroCreateReq("宏-A", "A"))
	if err != nil {
		t.Fatalf("create macro A: %v", err)
	}
	ticketB := &models.Ticket{CustomerID: 1, Status: "open", Priority: "1", Title: "工单-B", TenantID: "tenant-a", WorkspaceID: "workspace-b"}
	if err := db.Create(ticketB).Error; err != nil {
		t.Fatalf("create ticket B: %v", err)
	}

	if _, err := svc.ApplyToTicket(ctxA, macroA.ID, ticketB.ID, 1); err == nil {
		t.Fatal("expected cross-scope ticket application to fail")
	}
}

func TestMacroModuleDroppedTableErrors(t *testing.T) {
	db := newMacroTestDB(t)
	svc := newMacroModuleService(db)
	ctx := context.Background()

	if _, err := svc.Create(ctx, macroCreateReq("m", "c")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Migrator().DropTable("macros"); err != nil {
		t.Fatalf("drop macros: %v", err)
	}
	if _, err := svc.List(ctx); err == nil {
		t.Fatal("expected list error with missing table")
	}
	if err := svc.Delete(ctx, 1); err == nil || errors.Is(err, application.ErrMacroNotFound) {
		t.Fatalf("Delete() on dropped table err = %v, want driver error", err)
	}
}

func TestMacroModuleApplyToTicket_DroppedComments(t *testing.T) {
	db := newMacroTestDB(t)
	svc := newMacroModuleService(db)
	ctx := context.Background()

	macro, err := svc.Create(ctx, macroCreateReq("m", "c"))
	if err != nil {
		t.Fatalf("seed macro: %v", err)
	}
	ticket := &models.Ticket{Title: "T", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db.Migrator().DropTable("ticket_comments"); err != nil {
		t.Fatalf("drop comments: %v", err)
	}
	if _, err := svc.ApplyToTicket(ctx, macro.ID, ticket.ID, 1); err == nil {
		t.Fatal("expected comment create error")
	}
}

// TestMacroModuleUpdateTriggerError 触发器阻断 UPDATE 时错误透传
// （自 services/trigger_error_paths_unit_test.go 迁移）。
func TestMacroModuleUpdateTriggerError(t *testing.T) {
	db := newMacroTestDB(t)
	svc := newMacroModuleService(db)
	ctx := context.Background()

	macro, err := svc.Create(ctx, macroCreateReq("m", "c"))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Exec("CREATE TRIGGER blk_macro BEFORE UPDATE ON macros BEGIN SELECT RAISE(ABORT, 'update blocked'); END;").Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if _, err := svc.Update(ctx, macro.ID, &application.MacroUpdateRequest{Content: stringPtr("x")}); err == nil {
		t.Fatal("expected macro update error")
	}
}
