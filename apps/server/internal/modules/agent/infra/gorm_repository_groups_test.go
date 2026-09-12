package infra

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var agentGroupDBSeq uint32

func newAgentGroupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:agent_groups_%s_%d?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"), atomic.AddUint32(&agentGroupDBSeq, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.AgentGroup{}, &models.AgentGroupMember{}, &models.Session{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestAgentGroupCRUDLifecycle(t *testing.T) {
	db := newAgentGroupTestDB(t)
	repo := NewGormRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-a")

	group := &models.AgentGroup{Name: "billing", Description: "账单组", Priority: 5, Enabled: true}
	if err := repo.CreateAgentGroup(ctx, group); err != nil {
		t.Fatalf("create: %v", err)
	}
	if group.ID == 0 {
		t.Fatal("id must be set")
	}
	// scope 注入 + policy 空值兜底
	if group.TenantID != "tenant-a" || group.WorkspaceID != "ws-a" {
		t.Fatalf("scope fields expected: %+v", group)
	}
	if group.OverflowPolicy != "global" || !group.Enabled {
		t.Fatalf("defaults expected: %+v", group)
	}
	// 显式禁用必须原样落库（gorm default tag 会吞零值——回归防线）
	disabled := &models.AgentGroup{Name: "quiet", Enabled: false}
	if err := repo.CreateAgentGroup(ctx, disabled); err != nil {
		t.Fatalf("create disabled: %v", err)
	}
	if disabled.Enabled {
		t.Fatal("explicit enabled=false must persist")
	}

	// 同 scope 同名冲突（uniq tenant+ws+name）
	dup := &models.AgentGroup{Name: "billing"}
	if err := repo.CreateAgentGroup(ctx, dup); err == nil {
		t.Fatal("duplicate name must fail")
	}

	// 更新
	group.Enabled = false
	group.OverflowPolicy = "none"
	if err := repo.UpdateAgentGroup(ctx, group); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := repo.GetAgentGroup(ctx, group.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Enabled || got.OverflowPolicy != "none" {
		t.Fatalf("update not persisted: %+v", got)
	}

	// 列表（billing + quiet + vip）
	if err := repo.CreateAgentGroup(ctx, &models.AgentGroup{Name: "vip", Priority: 9, Enabled: true}); err != nil {
		t.Fatalf("create second: %v", err)
	}
	groups, err := repo.ListAgentGroups(ctx)
	if err != nil || len(groups) != 3 {
		t.Fatalf("list: n=%d err=%v", len(groups), err)
	}
	if groups[0].Name != "vip" {
		t.Fatalf("priority order expected, got %s", groups[0].Name)
	}

	// 软删：成员清空 + 记录不再可见
	if err := repo.ReplaceGroupMembers(ctx, group.ID, []uint{1, 2, 3}); err != nil {
		t.Fatalf("replace members: %v", err)
	}
	if err := repo.DeleteAgentGroup(ctx, group.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetAgentGroup(ctx, group.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("want ErrRecordNotFound, got %v", err)
	}
	var memberCount int64
	db.Model(&models.AgentGroupMember{}).Where("group_id = ?", group.ID).Count(&memberCount)
	if memberCount != 0 {
		t.Fatalf("members must be cleared, got %d", memberCount)
	}
}

func TestReplaceGroupMembersDedupesAndReplaces(t *testing.T) {
	db := newAgentGroupTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	group := &models.AgentGroup{Name: "g1", Enabled: true}
	if err := repo.CreateAgentGroup(ctx, group); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.ReplaceGroupMembers(ctx, group.ID, []uint{1, 2, 2, 0, 3}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	ids, err := repo.ListEnabledGroupMemberIDs(ctx, group.ID)
	if err != nil || len(ids) != 3 {
		t.Fatalf("deduped members expected: %v err=%v", ids, err)
	}

	// 全量替换语义：旧成员清掉
	if err := repo.ReplaceGroupMembers(ctx, group.ID, []uint{7}); err != nil {
		t.Fatalf("replace 2nd: %v", err)
	}
	ids, _ = repo.ListEnabledGroupMemberIDs(ctx, group.ID)
	if len(ids) != 1 || ids[0] != 7 {
		t.Fatalf("full replace expected: %v", ids)
	}
}

func TestListEnabledGroupMemberIDsGatesOnEnabled(t *testing.T) {
	db := newAgentGroupTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	disabled := &models.AgentGroup{Name: "off", Enabled: false}
	if err := repo.CreateAgentGroup(ctx, disabled); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Create(&models.AgentGroupMember{GroupID: disabled.ID, AgentUserID: 5}).Error; err != nil {
		t.Fatalf("seed member: %v", err)
	}
	ids, err := repo.ListEnabledGroupMemberIDs(ctx, disabled.ID)
	if err != nil || len(ids) != 0 {
		t.Fatalf("disabled group must yield no members: %v err=%v", ids, err)
	}
	if ids, err := repo.ListEnabledGroupMemberIDs(ctx, 99999); err != nil || len(ids) != 0 {
		t.Fatalf("missing group must be a clean empty: %v err=%v", ids, err)
	}
}

func TestGetLastAgentForCustomerAffinityLookup(t *testing.T) {
	db := newAgentGroupTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	now := time.Now()

	seed := func(sessionID string, userID uint, agentID *uint, at time.Time) {
		s := models.Session{ID: sessionID, Status: "ended", StartedAt: at, UserID: userID, AgentID: agentID, CreatedAt: at, UpdatedAt: at}
		if err := db.Create(&s).Error; err != nil {
			t.Fatalf("seed session: %v", err)
		}
	}
	a2 := uint(2)
	a3 := uint(3)

	// 无记录
	if id, err := repo.GetLastAgentForCustomer(ctx, 100, now.AddDate(0, 0, -30)); err != nil || id != nil {
		t.Fatalf("no history expected: %v err=%v", id, err)
	}

	seed("s-old", 100, &a2, now.Add(-72*time.Hour))
	seed("s-new", 100, &a3, now.Add(-1*time.Hour))
	// 其他客户不干扰
	seed("s-other", 200, &a2, now.Add(-time.Minute))

	// 最近一次是 agent 3
	id, err := repo.GetLastAgentForCustomer(ctx, 100, now.AddDate(0, 0, -30))
	if err != nil || id == nil || *id != 3 {
		t.Fatalf("latest agent expected, got %v err=%v", id, err)
	}

	// 窗口收紧到 2 小时：只有 s-new 在窗口内，仍命中 3；收紧到 30 分钟则无
	id, err = repo.GetLastAgentForCustomer(ctx, 100, now.Add(-2*time.Hour))
	if err != nil || id == nil || *id != 3 {
		t.Fatalf("windowed lookup expected agent 3: %v err=%v", id, err)
	}
	if id, err = repo.GetLastAgentForCustomer(ctx, 100, now.Add(-30*time.Minute)); err != nil || id != nil {
		t.Fatalf("expired window must be empty: %v err=%v", id, err)
	}
}
