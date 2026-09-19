package infra

// GormRepository 的 SQL 语义对账（自 services/shift_service*_test.go、
// error_paths/more_branches/trigger_error_paths 系列下沉）：
// 创建校验/列表过滤与 preload、租户×工作区隔离与跨 scope 防泄漏、
// 统计窗口聚合、丢表错误与触发器阻断。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"sync/atomic"

	shiftapp "servify/apps/server/internal/modules/shift/application"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// scopedContext 构造带租户/工作区的上下文（驱动 applyScopeFilter）。
func scopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func newShiftTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := uniqueMemDSN("file:shift_" + strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.ShiftSchedule{}, &models.User{}, &models.Agent{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

// newShiftModuleService 组装真仓储 + 编排服务（组合测试走完整链路）。
func newShiftModuleService(db *gorm.DB) *shiftapp.Service {
	return shiftapp.NewService(NewGormRepository(db))
}

// seedAgent 种子一个代理用户与档案，返回 user ID。
func seedAgent(t *testing.T, db *gorm.DB, tenantID, workspaceID, username string) uint {
	t.Helper()
	user := &models.User{Username: username, Email: username + "@example.com", Role: "agent"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user %s: %v", username, err)
	}
	agent := &models.Agent{UserID: user.ID, Status: "online"}
	if tenantID != "" {
		agent.TenantID = tenantID
	}
	if workspaceID != "" {
		agent.WorkspaceID = workspaceID
	}
	if err := db.Create(agent).Error; err != nil {
		t.Fatalf("seed agent %s: %v", username, err)
	}
	return user.ID
}

func TestShiftModuleCreateUpdateDelete(t *testing.T) {
	db := newShiftTestDB(t)
	svc := newShiftModuleService(db)
	ctx := scopedContext("t1", "w1")
	agentID := seedAgent(t, db, "t1", "w1", "agent")

	start := time.Date(2030, 1, 2, 8, 0, 0, 0, time.UTC)
	end := start.Add(8 * time.Hour)

	// end<=start 与 agent 不存在
	if _, err := svc.CreateShift(ctx, &shiftapp.ShiftCreateRequest{AgentID: agentID, ShiftType: "morning", StartTime: end, EndTime: start}); !errors.Is(err, shiftapp.ErrInvalidTimeRange) {
		t.Fatalf("end<=start err = %v", err)
	}
	if _, err := svc.CreateShift(ctx, &shiftapp.ShiftCreateRequest{AgentID: 999, ShiftType: "morning", StartTime: start, EndTime: end}); !errors.Is(err, shiftapp.ErrAgentNotFound) {
		t.Fatalf("agent missing err = %v", err)
	}

	shift, err := svc.CreateShift(ctx, &shiftapp.ShiftCreateRequest{AgentID: agentID, ShiftType: "morning", StartTime: start, EndTime: end, Status: "active"})
	if err != nil {
		t.Fatalf("CreateShift: %v", err)
	}
	if shift.Status != "active" || shift.TenantID != "t1" || shift.WorkspaceID != "w1" {
		t.Fatalf("unexpected shift: %+v", shift)
	}

	// 更新：not found、patch、end<=start
	if _, err := svc.UpdateShift(ctx, 999, &shiftapp.ShiftUpdateRequest{}); !errors.Is(err, shiftapp.ErrShiftNotFound) {
		t.Fatalf("update missing err = %v", err)
	}
	newStart := start.Add(2 * time.Hour)
	newEnd := newStart.Add(2 * time.Hour)
	updated, err := svc.UpdateShift(ctx, shift.ID, &shiftapp.ShiftUpdateRequest{
		ShiftType: strPtrShim("evening"),
		StartTime: &newStart,
		EndTime:   &newEnd,
		Status:    strPtrShim("active"),
	})
	if err != nil {
		t.Fatalf("UpdateShift: %v", err)
	}
	if updated.ShiftType != "evening" || updated.Status != "active" || !updated.Date.Equal(newStart.Truncate(24*time.Hour)) {
		t.Fatalf("unexpected update: %+v", updated)
	}
	if _, err := svc.UpdateShift(ctx, shift.ID, &shiftapp.ShiftUpdateRequest{EndTime: &newStart}); !errors.Is(err, shiftapp.ErrInvalidTimeRange) {
		t.Fatalf("update end<=start err = %v", err)
	}

	// 删除：成功后再删报 not found
	if err := svc.DeleteShift(ctx, shift.ID); err != nil {
		t.Fatalf("DeleteShift: %v", err)
	}
	if err := svc.DeleteShift(ctx, shift.ID); !errors.Is(err, shiftapp.ErrShiftNotFound) {
		t.Fatalf("delete missing err = %v", err)
	}
}

func TestShiftModuleListFiltersAndPreload(t *testing.T) {
	db := newShiftTestDB(t)
	svc := newShiftModuleService(db)
	ctx := scopedContext("t1", "w1")
	agentID := seedAgent(t, db, "t1", "w1", "agent")

	base := time.Date(2030, 1, 2, 8, 0, 0, 0, time.UTC)
	for i, typ := range []string{"morning", "night"} {
		if _, err := svc.CreateShift(ctx, &shiftapp.ShiftCreateRequest{
			AgentID:   agentID,
			ShiftType: typ,
			StartTime: base.Add(time.Duration(i) * 24 * time.Hour),
			EndTime:   base.Add(time.Duration(i)*24*time.Hour + 4*time.Hour),
		}); err != nil {
			t.Fatalf("CreateShift %s: %v", typ, err)
		}
	}

	items, total, err := svc.ListShifts(ctx, &shiftapp.ShiftListRequest{
		Page:      1,
		PageSize:  10,
		AgentID:   &agentID,
		ShiftType: []string{"morning"},
		Status:    []string{"scheduled"},
		DateFrom:  timePtrShim(base.Truncate(24 * time.Hour)),
		DateTo:    timePtrShim(base.Add(48 * time.Hour)),
		SortBy:    "start_time",
		SortOrder: "weird", // 非法排序回退 asc
	})
	if err != nil {
		t.Fatalf("ListShifts: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("unexpected list: %d %+v", total, items)
	}
	if items[0].Agent.ID == 0 {
		t.Fatalf("expected preloaded agent: %+v", items[0])
	}

	// desc 排序与分页参数仍可列出全部
	if _, _, err := svc.ListShifts(ctx, &shiftapp.ShiftListRequest{Page: 1, PageSize: 10, SortOrder: "desc"}); err != nil {
		t.Fatalf("ListShifts desc: %v", err)
	}
}

func TestShiftModuleStats(t *testing.T) {
	db := newShiftTestDB(t)
	svc := newShiftModuleService(db)
	ctx := scopedContext("t1", "w1")
	agentID := seedAgent(t, db, "t1", "w1", "agent")

	base := time.Date(2020, 1, 2, 8, 0, 0, 0, time.UTC)
	if _, err := svc.CreateShift(ctx, &shiftapp.ShiftCreateRequest{
		AgentID: agentID, ShiftType: "morning", StartTime: base, EndTime: base.Add(4 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateShift past: %v", err)
	}
	now := time.Now()
	if _, err := svc.CreateShift(ctx, &shiftapp.ShiftCreateRequest{
		AgentID: agentID, ShiftType: "afternoon", StartTime: now.Add(-time.Hour), EndTime: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateShift today: %v", err)
	}
	future := now.Add(48 * time.Hour)
	if _, err := svc.CreateShift(ctx, &shiftapp.ShiftCreateRequest{
		AgentID: agentID, ShiftType: "night", StartTime: future, EndTime: future.Add(4 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateShift future: %v", err)
	}

	stats, err := svc.GetShiftStats(ctx)
	if err != nil {
		t.Fatalf("GetShiftStats: %v", err)
	}
	if stats.Total != 3 || stats.Upcoming != 1 || stats.TodayActive != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.ByType["morning"] != 1 || stats.ByStatus["scheduled"] != 3 {
		t.Fatalf("unexpected aggregates: %+v", stats)
	}
}

func TestShiftModuleScopedByWorkspace(t *testing.T) {
	db := newShiftTestDB(t)
	svc := newShiftModuleService(db)

	ctxA := scopedContext("tenant-a", "workspace-a")
	ctxB := scopedContext("tenant-a", "workspace-b")
	agentA := seedAgent(t, db, "tenant-a", "workspace-a", "agenta")
	seedAgent(t, db, "tenant-a", "workspace-b", "agentb")

	now := time.Now()
	shiftA, err := svc.CreateShift(ctxA, &shiftapp.ShiftCreateRequest{
		AgentID: agentA, ShiftType: "morning", StartTime: now.Add(time.Hour), EndTime: now.Add(2 * time.Hour), Status: "scheduled",
	})
	if err != nil {
		t.Fatalf("create A failed: %v", err)
	}
	if shiftA.TenantID != "tenant-a" || shiftA.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected scope on create: %+v", shiftA)
	}
	if _, err := svc.CreateShift(ctxB, &shiftapp.ShiftCreateRequest{
		AgentID: seedAgent(t, db, "tenant-a", "workspace-b", "agentb2"), ShiftType: "night",
		StartTime: now.Add(3 * time.Hour), EndTime: now.Add(5 * time.Hour), Status: "scheduled",
	}); err != nil {
		t.Fatalf("create B failed: %v", err)
	}

	items, total, err := svc.ListShifts(ctxA, &shiftapp.ShiftListRequest{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list scoped shifts failed: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected scoped shifts: total=%d items=%+v", total, items)
	}

	stats, err := svc.GetShiftStats(ctxA)
	if err != nil {
		t.Fatalf("get scoped stats failed: %v", err)
	}
	if stats.Total != 1 || stats.ByType["morning"] != 1 {
		t.Fatalf("unexpected scoped stats: %+v", stats)
	}

	newStatus := "active"
	if _, err := svc.UpdateShift(ctxB, shiftA.ID, &shiftapp.ShiftUpdateRequest{Status: &newStatus}); !errors.Is(err, shiftapp.ErrShiftNotFound) {
		t.Fatalf("scoped update err = %v, want not found", err)
	}
	if err := svc.DeleteShift(ctxB, shiftA.ID); !errors.Is(err, shiftapp.ErrShiftNotFound) {
		t.Fatalf("scoped delete err = %v, want not found", err)
	}
}

// TestShiftModuleCrossScopeAgentGuard 对账跨 scope 防泄漏：
// create 拒绝其他 scope 的代理；列表 preload 的 JOIN 不带出跨 scope 代理；
// 无 scope 上下文（unscoped）时 preload 快照仍可加载数据。
func TestShiftModuleCrossScopeAgentGuard(t *testing.T) {
	db := newShiftTestDB(t)
	svc := newShiftModuleService(db)
	now := time.Now()

	agentUserB := &models.User{Username: "agent-b", Email: "agent-b@example.com", Name: "Agent B", Role: "agent"}
	if err := db.Create(agentUserB).Error; err != nil {
		t.Fatalf("create agent user B: %v", err)
	}
	if err := db.Create(&models.Agent{
		TenantID:    "tenant-b",
		WorkspaceID: "workspace-b",
		UserID:      agentUserB.ID,
		Status:      "online",
	}).Error; err != nil {
		t.Fatalf("create agent B: %v", err)
	}
	if err := db.Create(&models.ShiftSchedule{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		AgentID:     agentUserB.ID,
		ShiftType:   "morning",
		StartTime:   now.Add(time.Hour),
		EndTime:     now.Add(2 * time.Hour),
		Date:        now.Truncate(24 * time.Hour),
		Status:      "scheduled",
	}).Error; err != nil {
		t.Fatalf("create shift A: %v", err)
	}

	// create 拒绝跨 scope 代理
	if _, err := svc.CreateShift(scopedContext("tenant-a", "workspace-a"), &shiftapp.ShiftCreateRequest{
		AgentID: agentUserB.ID, ShiftType: "morning", StartTime: now.Add(time.Hour), EndTime: now.Add(2 * time.Hour),
	}); !errors.Is(err, shiftapp.ErrAgentNotFound) {
		t.Fatalf("cross-scope agent err = %v, want not found", err)
	}

	// 列表 preload 不带出跨 scope 代理
	items, total, err := svc.ListShifts(scopedContext("tenant-a", "workspace-a"), &shiftapp.ShiftListRequest{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListShifts failed: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("unexpected scoped shifts: total=%d items=%+v", total, items)
	}
	if items[0].Agent.ID != 0 {
		t.Fatalf("expected agent preload to stay scoped, got %+v", items[0].Agent)
	}

	// unscoped 列表可见全部数据（preload 快照分支）
	unscoped, _, err := svc.ListShifts(context.Background(), &shiftapp.ShiftListRequest{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("unscoped list: %v", err)
	}
	if len(unscoped) != 1 {
		t.Fatalf("expected 1 unscoped shift, got %d", len(unscoped))
	}
}

func TestShiftModuleDroppedTableErrors(t *testing.T) {
	db := newShiftTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	if err := db.Migrator().DropTable("shift_schedules"); err != nil {
		t.Fatalf("drop shifts: %v", err)
	}
	// 代理存在性查询不涉及 shifts 表：无 scope 时正常返回 false
	if exists, err := repo.AgentExistsByUserID(ctx, 1); err != nil || exists {
		t.Fatalf("AgentExistsByUserID() = %v, %v", exists, err)
	}
	if err := repo.CreateShift(ctx, &models.ShiftSchedule{}); err == nil {
		t.Fatal("expected create error with missing shifts table")
	}
	if _, err := repo.CountShifts(ctx, &shiftapp.ShiftListRequest{}); err == nil {
		t.Fatal("expected count error with missing shifts table")
	}
	if _, err := repo.ListShifts(ctx, &shiftapp.ShiftListRequest{}); err == nil {
		t.Fatal("expected list error with missing shifts table")
	}
	if _, err := repo.GetShift(ctx, 1); err == nil {
		t.Fatal("expected get error with missing shifts table")
	}
	if err := repo.SaveShift(ctx, &models.ShiftSchedule{}); err == nil {
		t.Fatal("expected save error with missing shifts table")
	}
	if err := repo.DeleteShift(ctx, 1); err == nil {
		t.Fatal("expected delete error with missing shifts table")
	}
	if _, err := repo.ShiftAggregates(ctx, time.Now()); err == nil || !strings.Contains(err.Error(), "failed to count shifts") {
		t.Fatalf("expected aggregates count error, got %v", err)
	}

	// agents 表缺失：代理存在性校验错误
	if err := db.Migrator().DropTable("agents"); err != nil {
		t.Fatalf("drop agents: %v", err)
	}
	if _, err := repo.AgentExistsByUserID(ctx, 1); err == nil {
		t.Fatal("expected agent exists error with missing agents table")
	}
}

// TestShiftModuleTriggerErrors 触发器阻断 INSERT/UPDATE 时错误透传
// （自 services/trigger_error_paths_unit_test.go 迁移）。
func TestShiftModuleTriggerErrors(t *testing.T) {
	db := newShiftTestDB(t)
	svc := newShiftModuleService(db)
	ctx := scopedContext("t1", "w1")
	agentID := seedAgent(t, db, "t1", "w1", "agent")
	start := time.Now().Add(time.Hour)
	req := &shiftapp.ShiftCreateRequest{AgentID: agentID, ShiftType: "morning", StartTime: start, EndTime: start.Add(time.Hour)}

	if err := db.Exec("CREATE TRIGGER blk_shift BEFORE INSERT ON shift_schedules BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;").Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if _, err := svc.CreateShift(ctx, req); err == nil {
		t.Fatal("expected shift insert error")
	}
	if err := db.Exec("DROP TRIGGER blk_shift;").Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}

	shift, err := svc.CreateShift(ctx, req)
	if err != nil {
		t.Fatalf("CreateShift: %v", err)
	}
	if err := db.Exec("CREATE TRIGGER blk_shift_upd BEFORE UPDATE ON shift_schedules BEGIN SELECT RAISE(ABORT, 'update blocked'); END;").Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if _, err := svc.UpdateShift(ctx, shift.ID, &shiftapp.ShiftUpdateRequest{Status: strPtrShim("active")}); err == nil {
		t.Fatal("expected shift update error")
	}
}

// failNthQuery 注册 gorm callback，强制第 n 次 SELECT 失败
// （自 services/sequential_errors_unit_test.go 复刻，覆盖聚合中间步错误出口）。
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

// TestShiftModuleAggregatesSequentialErrors 对账 ShiftAggregates 各步查询的
// 错误包裹文案（第 1 步 count 由丢表用例覆盖）。
func TestShiftModuleAggregatesSequentialErrors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		n          int32
		wantSubstr string
	}{
		{2, "failed to aggregate by shift_type"},
		{3, "failed to aggregate by status"},
		{4, "failed to count upcoming shifts"},
		{5, "failed to count today active shifts"},
	}
	for _, tc := range cases {
		db := newShiftTestDB(t)
		failNthQuery(db, tc.n)
		_, err := NewGormRepository(db).ShiftAggregates(ctx, time.Now())
		if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
			t.Fatalf("n=%d: expected %q, got %v", tc.n, tc.wantSubstr, err)
		}
	}
}

func strPtrShim(s string) *string { return &s }

func timePtrShim(t time.Time) *time.Time { return &t }
