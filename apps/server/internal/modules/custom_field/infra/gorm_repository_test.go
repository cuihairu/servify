package infra

// GormRepository 的 SQL 语义对账（自 services/custom_field_service*_test.go 下沉）：
// 排序、resource/active 过滤、租户/工作区隔离、哨兵映射与丢表/触发器错误。

import (
	"context"
	"errors"
	"strings"
	"testing"

	application "servify/apps/server/internal/modules/custom_field/application"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// scopedContext 构造带租户/工作区的上下文（驱动 applyScopeFilter）。
func scopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func newCustomFieldTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := uniqueMemDSN("file:custom_field_" + strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.CustomField{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func newCustomFieldModuleService(db *gorm.DB) *application.Service {
	return application.NewService(NewGormRepository(db))
}

func customFieldCreateReq(key string) *application.CustomFieldCreateRequest {
	return &application.CustomFieldCreateRequest{Key: key, Name: "Field " + key, Type: "select"}
}

func stringPtr(s string) *string { return &s }

func boolPtr(b bool) *bool { return &b }

// TestCustomFieldModuleCreateListDelete 对账创建默认值、列表排序与删除哨兵。
// 注意 Key 全局 unique，各测试的种子 key 不可重复。
func TestCustomFieldModuleCreateListDelete(t *testing.T) {
	db := newCustomFieldTestDB(t)
	svc := newCustomFieldModuleService(db)
	ctx := context.Background()

	f1, err := svc.Create(ctx, customFieldCreateReq("priority"))
	if err != nil {
		t.Fatalf("seed f1: %v", err)
	}
	f2, err := svc.Create(ctx, customFieldCreateReq("severity"))
	if err != nil {
		t.Fatalf("seed f2: %v", err)
	}
	if f1.Key != "priority" || f1.Type != "select" || !f1.Active {
		t.Fatalf("unexpected defaults: %+v", f1)
	}
	if f2.ID <= f1.ID || f2.CreatedAt.IsZero() {
		t.Fatalf("expected non-zero ID/CreatedAt: %+v", f2)
	}

	list, err := svc.List(ctx, "ticket", false)
	if err != nil || len(list) != 2 {
		t.Fatalf("List() = %d items, %v", len(list), err)
	}
	if list[0].ID != f1.ID {
		t.Fatal("expected custom fields sorted by id ASC")
	}
	if list, err = svc.List(ctx, "ticket", true); err != nil || len(list) != 2 {
		t.Fatalf("List activeOnly = %d items, %v", len(list), err)
	}
	if list, err = svc.List(ctx, "other", false); err != nil || len(list) != 0 {
		t.Fatalf("List other resource = %d items, %v", len(list), err)
	}

	got, err := svc.Get(ctx, f1.ID)
	if err != nil || got.Key != "priority" {
		t.Fatalf("Get() = %+v, %v", got, err)
	}
	if _, err := svc.Get(ctx, 9999); err == nil {
		t.Fatal("expected error for non-existent field")
	}

	if err := svc.Delete(ctx, f1.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := svc.Delete(ctx, f1.ID); !errors.Is(err, application.ErrCustomFieldNotFound) {
		t.Fatalf("Delete() missing err = %v, want %v", err, application.ErrCustomFieldNotFound)
	}
}

// TestCustomFieldModuleScopedByWorkspace 对账租户/工作区隔离：跨工作区的
// 读取与删除全部拒绝。
func TestCustomFieldModuleScopedByWorkspace(t *testing.T) {
	db := newCustomFieldTestDB(t)
	svc := newCustomFieldModuleService(db)
	ctxA := scopedContext("tenant-a", "workspace-a")
	ctxB := scopedContext("tenant-a", "workspace-b")

	fieldA, err := svc.Create(ctxA, customFieldCreateReq("field_a"))
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	if fieldA.TenantID != "tenant-a" || fieldA.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected scope on field A: %+v", fieldA)
	}
	if _, err := svc.Create(ctxB, customFieldCreateReq("field_b")); err != nil {
		t.Fatalf("create B: %v", err)
	}

	listA, err := svc.List(ctxA, "ticket", false)
	if err != nil || len(listA) != 1 || listA[0].WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected scoped list: %+v, %v", listA, err)
	}
	if _, err := svc.Get(ctxB, fieldA.ID); err == nil {
		t.Fatal("expected scoped get to reject cross-workspace field")
	}
	if err := svc.Delete(ctxB, fieldA.ID); err == nil {
		t.Fatal("expected scoped delete to reject cross-workspace field")
	}
}

func TestCustomFieldModuleUpdate(t *testing.T) {
	db := newCustomFieldTestDB(t)
	svc := newCustomFieldModuleService(db)
	ctx := context.Background()

	field, err := svc.Create(ctx, customFieldCreateReq("patched"))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	updated, err := svc.Update(ctx, field.ID, &application.CustomFieldUpdateRequest{
		Name:       stringPtr("New Name"),
		Type:       stringPtr("number"),
		Required:   boolPtr(true),
		Options:    `["a","b"]`,
		Validation: `{"min":5,"max":100}`,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != "New Name" || updated.Type != "number" || !updated.Required {
		t.Fatalf("patch not applied: %+v", updated)
	}
	if updated.OptionsJSON != `["a","b"]` || updated.ValidationJSON != `{"min":5,"max":100}` {
		t.Fatalf("json patch not applied: %+v", updated)
	}
	if updated.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}

	var persisted models.CustomField
	if err := db.First(&persisted, field.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if persisted.Name != "New Name" || persisted.Type != "number" {
		t.Fatalf("update not persisted: %+v", persisted)
	}

	if _, err := svc.Update(ctx, 9999, &application.CustomFieldUpdateRequest{Name: stringPtr("x")}); err == nil {
		t.Fatal("expected error for non-existent field")
	}
}

// TestCustomFieldModuleUpdateDeactivate 对账 active=false 写入：模型
// default:true 会覆盖零值插入，需要独立断言显式更新路径。
func TestCustomFieldModuleUpdateDeactivate(t *testing.T) {
	db := newCustomFieldTestDB(t)
	svc := newCustomFieldModuleService(db)
	ctx := context.Background()

	field, err := svc.Create(ctx, customFieldCreateReq("toggle"))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	updated, err := svc.Update(ctx, field.ID, &application.CustomFieldUpdateRequest{Active: boolPtr(false), ShowWhen: `{"x":1}`})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Active {
		t.Fatalf("expected active=false, got %+v", updated)
	}
	if updated.ShowWhenJSON != `{"x":1}` {
		t.Fatalf("show_when json = %q", updated.ShowWhenJSON)
	}
	list, err := svc.List(ctx, "ticket", true)
	if err != nil || len(list) != 0 {
		t.Fatalf("active-only list after deactivation = %d items, %v", len(list), err)
	}
}

func TestCustomFieldModuleDroppedTableErrors(t *testing.T) {
	db := newCustomFieldTestDB(t)
	svc := newCustomFieldModuleService(db)
	ctx := context.Background()

	if _, err := svc.Create(ctx, customFieldCreateReq("dropped")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Migrator().DropTable("custom_fields"); err != nil {
		t.Fatalf("drop custom_fields: %v", err)
	}
	if _, err := svc.List(ctx, "ticket", false); err == nil {
		t.Fatal("expected list error with missing table")
	}
	if _, err := svc.Get(ctx, 1); err == nil {
		t.Fatal("expected get error with missing table")
	}
	if _, err := svc.Create(ctx, customFieldCreateReq("dropped2")); err == nil {
		t.Fatal("expected create error with missing table")
	}
	if _, err := svc.Update(ctx, 1, &application.CustomFieldUpdateRequest{Name: stringPtr("x")}); err == nil {
		t.Fatal("expected update error with missing table")
	}
	if err := svc.Delete(ctx, 1); err == nil || errors.Is(err, application.ErrCustomFieldNotFound) {
		t.Fatalf("Delete() on dropped table err = %v, want driver error", err)
	}
}

// TestCustomFieldModuleUpdateTriggerError 触发器阻断 UPDATE 时错误透传
// （自 services/trigger_error_paths_unit_test.go 迁移）。
func TestCustomFieldModuleUpdateTriggerError(t *testing.T) {
	db := newCustomFieldTestDB(t)
	svc := newCustomFieldModuleService(db)
	ctx := context.Background()

	field, err := svc.Create(ctx, customFieldCreateReq("triggered"))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Exec("CREATE TRIGGER blk_custom_field BEFORE UPDATE ON custom_fields BEGIN SELECT RAISE(ABORT, 'update blocked'); END;").Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if _, err := svc.Update(ctx, field.ID, &application.CustomFieldUpdateRequest{Name: stringPtr("x")}); err == nil {
		t.Fatal("expected custom field update error")
	}
}
