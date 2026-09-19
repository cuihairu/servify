package infra

// GormRepository 的 SQL 语义对账（自 services/workspace_service*_test.go 下沉）：
// 计数/渠道聚合/最近会话 JOIN、租户×工作区隔离与跨 scope JOIN 防泄漏、丢表错误。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	application "servify/apps/server/internal/modules/workspace/application"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// scopedContext 构造带租户/工作区的上下文（驱动 applyScopeFilter）。
func scopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func newWorkspaceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := uniqueMemDSN("file:workspace_" + strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Session{},
		&models.Agent{},
		&models.User{},
		&models.Ticket{},
		&models.Customer{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func newWorkspaceModuleRepo(db *gorm.DB) *GormRepository {
	return NewGormRepository(db)
}

func TestWorkspaceModuleCountsAndChannels(t *testing.T) {
	db := newWorkspaceTestDB(t)
	repo := newWorkspaceModuleRepo(db)
	ctx := context.Background()

	now := time.Now()
	assigned := uint(77)
	for _, s := range []models.Session{
		{ID: "sess-1", Platform: "web", Status: "active", AgentID: &assigned, StartedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "sess-2", Platform: "web", Status: "active", AgentID: nil, StartedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "sess-3", Platform: "telegram", Status: "closed", StartedAt: now, CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatalf("seed session: %v", err)
		}
	}

	total, err := repo.CountActiveSessions(ctx)
	if err != nil || total != 2 {
		t.Fatalf("CountActiveSessions() = %d, %v", total, err)
	}
	waiting, err := repo.CountWaitingSessions(ctx)
	if err != nil || waiting != 1 {
		t.Fatalf("CountWaitingSessions() = %d, %v", waiting, err)
	}
	channels, err := repo.AggregateChannels(ctx)
	if err != nil {
		t.Fatalf("AggregateChannels() error = %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("unexpected channels: %+v", channels)
	}
	// 平台名归组：web 2 active/1 waiting；closed 不计 active
	byPlatform := map[string]application.ChannelRow{}
	for _, c := range channels {
		byPlatform[c.Platform] = c
	}
	web := byPlatform["web"]
	if web.Active != 2 || web.Waiting != 1 {
		t.Fatalf("unexpected web channel: %+v", web)
	}
	if tg := byPlatform["telegram"]; tg.Active != 0 || tg.Waiting != 0 {
		t.Fatalf("unexpected telegram channel: %+v", tg)
	}
}

func TestWorkspaceModuleAgentCounters(t *testing.T) {
	db := newWorkspaceTestDB(t)
	repo := newWorkspaceModuleRepo(db)
	ctx := context.Background()

	// online 1；busy 语义：status=busy 或 current_load >= max_concurrent
	seed := []models.Agent{
		{UserID: 1, Status: "online", CurrentLoad: 1, MaxConcurrent: 5, AvgResponseTime: 10},
		{UserID: 2, Status: "busy", CurrentLoad: 1, MaxConcurrent: 5, AvgResponseTime: 20},
		{UserID: 3, Status: "online", CurrentLoad: 5, MaxConcurrent: 5, AvgResponseTime: 30},
		{UserID: 4, Status: "offline", CurrentLoad: 9, MaxConcurrent: 5, AvgResponseTime: 40},
	}
	for i := range seed {
		if err := db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed agent %d: %v", i, err)
		}
	}

	online, err := repo.CountOnlineAgents(ctx)
	if err != nil || online != 2 {
		t.Fatalf("CountOnlineAgents() = %d, %v", online, err)
	}
	busy, err := repo.CountBusyAgents(ctx)
	if err != nil || busy != 3 {
		t.Fatalf("CountBusyAgents() = %d, %v", busy, err)
	}
	if avg := repo.AvgAgentResponseTime(ctx); avg != 25 {
		t.Fatalf("AvgAgentResponseTime() = %v, want 25", avg)
	}
}

// TestWorkspaceModuleRecentSessions 对账最近会话 JOIN 装配（agent/customer 名字）。
func TestWorkspaceModuleRecentSessions(t *testing.T) {
	db := newWorkspaceTestDB(t)
	repo := newWorkspaceModuleRepo(db)
	ctx := scopedContext("t1", "w1")

	agentUser := &models.User{Username: "u", Email: "u@x.com", Role: "agent", Name: "User One"}
	if err := db.Create(agentUser).Error; err != nil {
		t.Fatalf("seed agent user: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: agentUser.ID, TenantID: "t1", WorkspaceID: "w1", Status: "online"}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	customer := &models.User{Username: "c", Email: "c@x.com", Role: "customer", Name: "Customer One"}
	if err := db.Create(customer).Error; err != nil {
		t.Fatalf("seed customer user: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: customer.ID, TenantID: "t1", WorkspaceID: "w1"}).Error; err != nil {
		t.Fatalf("seed customer profile: %v", err)
	}
	ticket := &models.Ticket{Title: "T", CustomerID: customer.ID, TenantID: "t1", WorkspaceID: "w1", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	now := time.Now()
	agentID := agentUser.ID
	if err := db.Create(&models.Session{
		ID: "sess-1", TenantID: "t1", WorkspaceID: "w1", Platform: "web",
		Status: "active", StartedAt: now, CreatedAt: now, UpdatedAt: now, AgentID: &agentID, TicketID: &ticket.ID,
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := db.Create(&models.Session{
		ID: "sess-2", TenantID: "t1", WorkspaceID: "w1", Platform: "telegram",
		Status: "active", StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed waiting session: %v", err)
	}

	sessions, err := repo.RecentSessions(ctx, 10)
	if err != nil {
		t.Fatalf("RecentSessions() error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}
	byID := map[string]application.WorkspaceSession{}
	for _, s := range sessions {
		byID[s.ID] = s
	}
	s1 := byID["sess-1"]
	if s1.AgentName != "User One" || s1.CustomerName != "Customer One" || s1.CustomerID == nil {
		t.Fatalf("unexpected joined session: %+v", s1)
	}
	s2 := byID["sess-2"]
	if s2.AgentName != "" || s2.CustomerName != "" {
		t.Fatalf("unexpected bare session: %+v", s2)
	}

	// limit 生效
	if sessions, err = repo.RecentSessions(ctx, 1); err != nil || len(sessions) != 1 {
		t.Fatalf("limit session count = %d, %v", len(sessions), err)
	}
}

func TestWorkspaceModuleScopedByWorkspace(t *testing.T) {
	db := newWorkspaceTestDB(t)
	repo := newWorkspaceModuleRepo(db)

	db.Create(&models.Agent{UserID: 1, TenantID: "tenant-a", WorkspaceID: "workspace-a", Status: "online", AvgResponseTime: 10})
	db.Create(&models.Agent{UserID: 2, TenantID: "tenant-b", WorkspaceID: "workspace-b", Status: "busy", AvgResponseTime: 20, CurrentLoad: 5, MaxConcurrent: 5})
	db.Create(&models.Session{ID: "sess-a", TenantID: "tenant-a", WorkspaceID: "workspace-a", Platform: "web", Status: "active", StartedAt: time.Now()})
	db.Create(&models.Session{ID: "sess-b", TenantID: "tenant-b", WorkspaceID: "workspace-b", Platform: "api", Status: "active", StartedAt: time.Now()})

	ctxA := scopedContext("tenant-a", "workspace-a")
	total, err := repo.CountActiveSessions(ctxA)
	if err != nil || total != 1 {
		t.Fatalf("scoped CountActiveSessions() = %d, %v", total, err)
	}
	online, err := repo.CountOnlineAgents(ctxA)
	if err != nil || online != 1 {
		t.Fatalf("scoped CountOnlineAgents() = %d, %v", online, err)
	}
	busy, err := repo.CountBusyAgents(ctxA)
	if err != nil || busy != 0 {
		t.Fatalf("scoped CountBusyAgents() = %d, %v", busy, err)
	}
	channels, err := repo.AggregateChannels(ctxA)
	if err != nil || len(channels) != 1 || channels[0].Platform != "web" {
		t.Fatalf("scoped channels: %+v, %v", channels, err)
	}
}

// TestWorkspaceModuleRecentSessionsCrossScopeJoins 对账 JOIN 的 scope 保护：
// 跨租户/工作区的 ticket/customer/agent 不得泄漏进本 scope 的会话视图。
func TestWorkspaceModuleRecentSessionsCrossScopeJoins(t *testing.T) {
	db := newWorkspaceTestDB(t)
	repo := newWorkspaceModuleRepo(db)

	customerBUser := &models.User{Username: "customer-b", Email: "customer-b@test.com", Name: "Customer B", Role: "customer"}
	agentBUser := &models.User{Username: "agent-b", Email: "agent-b@test.com", Name: "Agent B", Role: "agent"}
	if err := db.Create(customerBUser).Error; err != nil {
		t.Fatalf("create customer B user: %v", err)
	}
	if err := db.Create(agentBUser).Error; err != nil {
		t.Fatalf("create agent B user: %v", err)
	}
	customerB := &models.Customer{TenantID: "tenant-b", WorkspaceID: "workspace-b", UserID: customerBUser.ID}
	if err := db.Create(customerB).Error; err != nil {
		t.Fatalf("create customer B: %v", err)
	}
	agentB := &models.Agent{TenantID: "tenant-b", WorkspaceID: "workspace-b", UserID: agentBUser.ID, Status: "online"}
	if err := db.Create(agentB).Error; err != nil {
		t.Fatalf("create agent B: %v", err)
	}
	ticketB := &models.Ticket{TenantID: "tenant-b", WorkspaceID: "workspace-b", Title: "cross-scope ticket", CustomerID: customerBUser.ID}
	if err := db.Create(ticketB).Error; err != nil {
		t.Fatalf("create ticket B: %v", err)
	}
	sessionA := &models.Session{
		ID: "sess-cross-scope", TenantID: "tenant-a", WorkspaceID: "workspace-a",
		TicketID: &ticketB.ID, AgentID: &agentBUser.ID,
		Platform: "web", Status: "active", StartedAt: time.Now(),
	}
	if err := db.Create(sessionA).Error; err != nil {
		t.Fatalf("create session A: %v", err)
	}

	sessions, err := repo.RecentSessions(scopedContext("tenant-a", "workspace-a"), 10)
	if err != nil {
		t.Fatalf("RecentSessions() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 recent session, got %d", len(sessions))
	}
	recent := sessions[0]
	if recent.CustomerName != "" || recent.CustomerID != nil {
		t.Fatalf("expected customer join to be scope-protected, got %+v", recent)
	}
	if recent.AgentName != "" {
		t.Fatalf("expected agent join to be scope-protected, got %+v", recent)
	}
}

func TestWorkspaceModuleDroppedTableErrors(t *testing.T) {
	db := newWorkspaceTestDB(t)
	repo := newWorkspaceModuleRepo(db)
	ctx := context.Background()

	if err := db.Migrator().DropTable("sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if _, err := repo.CountActiveSessions(ctx); err == nil {
		t.Fatal("expected count active error with missing table")
	}
	if _, err := repo.CountWaitingSessions(ctx); err == nil {
		t.Fatal("expected count waiting error with missing table")
	}
	if _, err := repo.AggregateChannels(ctx); err == nil {
		t.Fatal("expected aggregate channels error with missing table")
	}
	if _, err := repo.RecentSessions(ctx, 10); !errors.Is(err, gorm.ErrRecordNotFound) && err == nil {
		t.Fatalf("expected recent sessions error, got %v", err)
	}
	// agents 表错误出口
	if err := db.Migrator().DropTable("agents"); err != nil {
		t.Fatalf("drop agents: %v", err)
	}
	if _, err := repo.CountOnlineAgents(ctx); err == nil {
		t.Fatal("expected online agents error with missing table")
	}
	if _, err := repo.CountBusyAgents(ctx); err == nil {
		t.Fatal("expected busy agents error with missing table")
	}
	// AvgAgentResponseTime 旧语义：错误忽略返回 0
	if avg := repo.AvgAgentResponseTime(ctx); avg != 0 {
		t.Fatalf("AvgAgentResponseTime() on dropped table = %v, want 0", avg)
	}
}
