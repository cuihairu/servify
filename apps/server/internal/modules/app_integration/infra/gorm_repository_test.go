package infra

// GormRepository 的 SQL 语义对账（自 services/app_integration_service*_test.go、
// error_paths/auth_error_paths/more_branches/sequential_errors 系列下沉）：
// 生命周期（slug 归一/去重/默认值/过滤搜索/patch）、租户×工作区隔离、
// unique name 冲突、丢表错误与顺序查询错误。

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	appintegrationapp "servify/apps/server/internal/modules/app_integration/application"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// scopedContext 构造带租户/工作区的上下文（驱动 applyScopeFilter）。
func scopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func newAppIntegrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := uniqueMemDSN("file:app_integration_" + strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.AppIntegration{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

// newAppIntegrationModuleService 组装真仓储 + 编排服务（组合测试走完整链路）。
func newAppIntegrationModuleService(db *gorm.DB) *appintegrationapp.Service {
	return appintegrationapp.NewService(NewGormRepository(db))
}

func TestAppIntegrationModuleLifecycle(t *testing.T) {
	db := newAppIntegrationTestDB(t)
	svc := newAppIntegrationModuleService(db)
	ctx := scopedContext("t1", "w1")

	created, err := svc.Create(ctx, &appintegrationapp.AppIntegrationCreateRequest{
		Name:      "Zapier",
		Slug:      " ZAPIER Tools!! ",
		Vendor:    "Zapier Inc",
		IFrameURL: "https://zapier.example.com",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Slug != "zapier-tools" {
		t.Fatalf("slug = %q", created.Slug)
	}
	if !created.Enabled || created.LastSyncStatus != "never" {
		t.Fatalf("unexpected defaults: %+v", created)
	}

	if _, err := svc.Create(ctx, &appintegrationapp.AppIntegrationCreateRequest{Name: "!!!"}); err == nil {
		t.Fatal("expected error when slug cannot be derived")
	}
	if _, err := svc.Create(ctx, &appintegrationapp.AppIntegrationCreateRequest{Name: "Dup", Slug: "zapier-tools", IFrameURL: "u"}); err == nil {
		t.Fatal("expected duplicate slug error")
	}

	// second record for list coverage
	if _, err := svc.Create(ctx, &appintegrationapp.AppIntegrationCreateRequest{
		Name: "Slack", Slug: "slack", IFrameURL: "u2",
		Enabled:      boolPtrShim(false),
		Capabilities: []string{"chat"},
		ConfigSchema: map[string]interface{}{"k": "v"},
	}); err != nil {
		t.Fatalf("Create slack: %v", err)
	}

	items, total, err := svc.List(ctx, &appintegrationapp.AppIntegrationListRequest{Page: 0, PageSize: 0})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("expected 2 items, got %d/%d", total, len(items))
	}

	// Enabled=false 经 Create 被 gorm default:true 吞零值——两条在 DB 中均 enabled
	// （既有行为对账，自 services unit_test 原断言 2/0 保持）。
	enabledItems, _, err := svc.List(ctx, &appintegrationapp.AppIntegrationListRequest{Status: []string{"enabled"}})
	if err != nil || len(enabledItems) != 2 {
		t.Fatalf("enabled filter: %v %d", err, len(enabledItems))
	}
	disabledItems, _, err := svc.List(ctx, &appintegrationapp.AppIntegrationListRequest{Status: []string{"disabled"}})
	if err != nil || len(disabledItems) != 0 {
		t.Fatalf("disabled filter: %v %d", err, len(disabledItems))
	}
	if _, _, err := svc.List(ctx, &appintegrationapp.AppIntegrationListRequest{Status: []string{"weird"}}); err != nil {
		t.Fatalf("weird status filter: %v", err)
	}

	// search spans name/vendor/summary via LOWER LIKE, consistent on sqlite and pg
	zapItems, zapTotal, err := svc.List(ctx, &appintegrationapp.AppIntegrationListRequest{Search: "zap"})
	if err != nil {
		t.Fatalf("List search: %v", err)
	}
	if zapTotal != 1 || len(zapItems) != 1 || zapItems[0].Slug != "zapier-tools" {
		t.Fatalf("expected 1 zapier match, got %d/%d", zapTotal, len(zapItems))
	}

	// forced first-SELECT failure covers the List count error branch
	errDB := newAppIntegrationTestDB(t)
	failNthQuery(errDB, 1)
	if _, _, err := newAppIntegrationModuleService(errDB).List(ctx, &appintegrationapp.AppIntegrationListRequest{Page: 1, PageSize: 20}); err == nil || !strings.Contains(err.Error(), "failed to count integrations") {
		t.Fatalf("expected count integrations error, got %v", err)
	}

	updated, err := svc.Update(ctx, created.ID, &appintegrationapp.AppIntegrationUpdateRequest{
		Name:         strPtrShim("Zapier 2"),
		Vendor:       strPtrShim("V"),
		Category:     strPtrShim("automation"),
		Summary:      strPtrShim("sum"),
		IconURL:      strPtrShim("icon"),
		Capabilities: []string{"a", "b"},
		ConfigSchema: map[string]interface{}{"x": 1},
		IFrameURL:    strPtrShim("u3"),
		Enabled:      boolPtrShim(true),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "Zapier 2" || len(updated.Capabilities) != 2 || updated.ConfigSchema == nil {
		t.Fatalf("unexpected update: %+v", updated)
	}

	if _, err := svc.Update(ctx, created.ID, nil); err == nil {
		t.Fatal("expected error for nil update request")
	}
	if _, err := svc.Update(ctx, 9999, &appintegrationapp.AppIntegrationUpdateRequest{}); !errors.Is(err, appintegrationapp.ErrIntegrationNotFound) {
		t.Fatalf("missing integration err = %v", err)
	}

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := svc.Delete(ctx, created.ID); !errors.Is(err, appintegrationapp.ErrIntegrationNotFound) {
		t.Fatalf("delete missing err = %v", err)
	}
}

// TestAppIntegrationModuleCategoryFilter 与 ListFindError（n=2）对账。
func TestAppIntegrationModuleCategoryFilterAndListFindError(t *testing.T) {
	db := newAppIntegrationTestDB(t)
	svc := newAppIntegrationModuleService(db)
	ctx := context.Background()

	if _, err := svc.Create(ctx, &appintegrationapp.AppIntegrationCreateRequest{Name: "A", Slug: "a", IFrameURL: "u", Category: "tools"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	items, _, err := svc.List(ctx, &appintegrationapp.AppIntegrationListRequest{Page: 1, PageSize: 10, Category: "tools"})
	if err != nil || len(items) != 1 {
		t.Fatalf("category filter: %v %d", err, len(items))
	}

	// find error（第 2 次 SELECT 失败）
	failNthQuery(db, 2)
	if _, _, err := svc.List(context.Background(), &appintegrationapp.AppIntegrationListRequest{Page: 1, PageSize: 10}); err == nil ||
		!strings.Contains(err.Error(), "failed to list integrations") {
		t.Fatalf("find error: %v", err)
	}
}

func TestAppIntegrationModuleCrossScope(t *testing.T) {
	db := newAppIntegrationTestDB(t)
	svc := newAppIntegrationModuleService(db)
	ctxA := scopedContext("t1", "w1")
	ctxB := scopedContext("t1", "w2")

	created, err := svc.Create(ctxA, &appintegrationapp.AppIntegrationCreateRequest{Name: "A", Slug: "a", IFrameURL: "u"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Update(ctxB, created.ID, &appintegrationapp.AppIntegrationUpdateRequest{}); !errors.Is(err, appintegrationapp.ErrIntegrationNotFound) {
		t.Fatalf("cross-scope update err = %v", err)
	}
	if err := svc.Delete(ctxB, created.ID); !errors.Is(err, appintegrationapp.ErrIntegrationNotFound) {
		t.Fatalf("cross-scope delete err = %v", err)
	}
}

// TestAppIntegrationModuleUniqueNameViolation 对账 name unique 约束：
// create 重复名插入失败；update 撞名更新失败（自 error_paths/auth_error_paths 迁移）。
func TestAppIntegrationModuleUniqueNameViolation(t *testing.T) {
	db := newAppIntegrationTestDB(t)
	svc := newAppIntegrationModuleService(db)
	ctx := context.Background()

	first, err := svc.Create(ctx, &appintegrationapp.AppIntegrationCreateRequest{Name: "One", Slug: "one", IFrameURL: "u"})
	if err != nil {
		t.Fatalf("create one: %v", err)
	}
	if _, err := svc.Create(ctx, &appintegrationapp.AppIntegrationCreateRequest{Name: "One", Slug: "other", IFrameURL: "u"}); err == nil {
		t.Fatal("expected create error from duplicate name")
	}
	if _, err := svc.Create(ctx, &appintegrationapp.AppIntegrationCreateRequest{Name: "Two", Slug: "two", IFrameURL: "u"}); err != nil {
		t.Fatalf("create two: %v", err)
	}
	if _, err := svc.Update(ctx, first.ID, &appintegrationapp.AppIntegrationUpdateRequest{Name: strPtrShim("Two")}); err == nil {
		t.Fatal("expected unique name violation on update")
	}
}

func TestAppIntegrationModuleDroppedTableErrors(t *testing.T) {
	db := newAppIntegrationTestDB(t)
	repo := NewGormRepository(db)
	svc := newAppIntegrationModuleService(db)
	ctx := context.Background()

	if err := db.Migrator().DropTable("app_integrations"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := repo.CountBySlug(ctx, "x"); err == nil {
		t.Fatal("expected slug check error with missing table")
	}
	if _, err := repo.CountIntegrations(ctx, &appintegrationapp.AppIntegrationListRequest{}); err == nil {
		t.Fatal("expected count error with missing table")
	}
	if _, err := repo.ListIntegrations(ctx, &appintegrationapp.AppIntegrationListRequest{}, 0, 10); err == nil {
		t.Fatal("expected list error with missing table")
	}
	if _, err := repo.GetIntegration(ctx, 1); err == nil {
		t.Fatal("expected get error with missing table")
	}
	if err := repo.CreateIntegration(ctx, &models.AppIntegration{}); err == nil {
		t.Fatal("expected create error with missing table")
	}
	if err := repo.SaveIntegration(ctx, &models.AppIntegration{}); err == nil {
		t.Fatal("expected save error with missing table")
	}
	if err := repo.DeleteIntegration(ctx, 1); err == nil {
		t.Fatal("expected delete error with missing table")
	}
	// 丢表后 service 层 Update 走 load 错误包裹（非 not found）
	if _, err := svc.Update(ctx, 1, &appintegrationapp.AppIntegrationUpdateRequest{}); err == nil || err.Error() == "integration not found" {
		t.Fatalf("expected load error, got %v", err)
	}
}

// failNthQuery 注册 gorm callback，强制第 n 次 SELECT 失败
// （自 services/sequential_errors_unit_test.go 复刻）。
func failNthQuery(db *gorm.DB, n int32) {
	var calls int32
	bump := func(tx *gorm.DB) {
		if atomic.AddInt32(&calls, 1) == n {
			_ = tx.AddError(errors.New("forced nth query failure"))
		}
	}
	_ = db.Callback().Query().Before("gorm:query").Register("fail_nth", bump)
	_ = db.Callback().Row().Before("gorm:row").Register("fail_nth_row", bump)
}

func strPtrShim(s string) *string { return &s }

func boolPtrShim(b bool) *bool { return &b }
