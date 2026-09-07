package infra

import (
	"context"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	agentdomain "servify/apps/server/internal/modules/agent/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newAgentCoverageDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:agent_cov_" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Agent{}, &models.Session{}, &models.Ticket{}))
	return db
}

func seedCoverageUser(t *testing.T, db *gorm.DB, id uint, username string) {
	t.Helper()
	now := time.Now()
	require.NoError(t, db.Create(&models.User{ID: id, Username: username, Email: username + "@example.com", Name: "User " + username, Role: "customer", CreatedAt: now, UpdatedAt: now}).Error)
}

func TestGormRepositoryCreateAgent(t *testing.T) {
	ctx := context.Background()

	t.Run("user missing", func(t *testing.T) {
		repo := NewGormRepository(newAgentCoverageDB(t))
		_, err := repo.CreateAgent(ctx, 42, "support", []string{"billing"}, 5)
		require.ErrorContains(t, err, "user not found")
	})

	t.Run("success with scope and defaults", func(t *testing.T) {
		db := newAgentCoverageDB(t)
		seedCoverageUser(t, db, 1, "agent1")
		repo := NewGormRepository(db)

		scoped := platformauth.ContextWithScope(ctx, "tenant-a", "ws-a")
		profile, err := repo.CreateAgent(scoped, 1, "support", []string{"billing", "chat"}, 3)
		require.NoError(t, err)
		require.Equal(t, uint(1), profile.UserID)
		require.Equal(t, "agent1", profile.Username)
		require.Equal(t, "User agent1", profile.Name)
		require.Equal(t, "support", profile.Department)
		require.Equal(t, []string{"billing", "chat"}, profile.Skills)
		require.Equal(t, 3, profile.MaxChatConcurrency)
		require.Equal(t, 1, profile.MaxVoiceConcurrency)
		require.Equal(t, 5.0, profile.Rating)

		var stored models.Agent
		require.NoError(t, db.First(&stored, "user_id = ?", 1).Error)
		require.Equal(t, "tenant-a", stored.TenantID)
		require.Equal(t, "ws-a", stored.WorkspaceID)
		require.Equal(t, string(agentdomain.PresenceStatusOffline), stored.Status)

		var user models.User
		require.NoError(t, db.First(&user, 1).Error)
		require.Equal(t, "agent", user.Role)

		_, err = repo.CreateAgent(scoped, 1, "support", nil, 3)
		require.EqualError(t, err, "user is already an agent")
	})

	t.Run("create failure", func(t *testing.T) {
		db := newAgentCoverageDB(t)
		seedCoverageUser(t, db, 1, "agent1")
		require.NoError(t, db.Migrator().DropTable("agents"))
		repo := NewGormRepository(db)

		_, err := repo.CreateAgent(ctx, 1, "support", nil, 3)
		require.ErrorContains(t, err, "failed to create agent")
	})

	t.Run("empty skills map to nil", func(t *testing.T) {
		db := newAgentCoverageDB(t)
		seedCoverageUser(t, db, 2, "agent2")
		repo := NewGormRepository(db)

		profile, err := repo.CreateAgent(ctx, 2, "", []string{}, 0)
		require.NoError(t, err)
		require.Nil(t, profile.Skills)
		require.Equal(t, 5, profile.MaxChatConcurrency)
	})
}

func TestGormRepositoryGetAgentByUserIDWithTickets(t *testing.T) {
	db := newAgentCoverageDB(t)
	seedCoverageUser(t, db, 1, "agent1")
	now := time.Now()
	require.NoError(t, db.Create(&models.Agent{UserID: 1, TenantID: "tenant-a", WorkspaceID: "ws-a", Department: "support", Skills: "billing, chat", Status: "online", MaxConcurrent: 4, CurrentLoad: 2, Rating: 4.2, AvgResponseTime: 40, TotalTickets: 12, LastActivityAt: &now, ConnectedAt: &now, CreatedAt: now, UpdatedAt: now}).Error)
	require.NoError(t, db.Create(&[]models.Ticket{
		{TenantID: "tenant-a", WorkspaceID: "ws-a", Title: "open", CustomerID: 1, AgentID: ptrUint(1), Status: "in_progress", CreatedAt: now, UpdatedAt: now},
		{TenantID: "tenant-a", WorkspaceID: "ws-a", Title: "closed", CustomerID: 1, AgentID: ptrUint(1), Status: "closed", CreatedAt: now.Add(-time.Hour), UpdatedAt: now, ClosedAt: &now},
		{TenantID: "tenant-b", WorkspaceID: "ws-b", Title: "other tenant", CustomerID: 1, AgentID: ptrUint(1), Status: "open", CreatedAt: now, UpdatedAt: now},
	}).Error)

	repo := NewGormRepository(db)
	scoped := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-a")

	profile, agent, err := repo.GetAgentByUserID(scoped, 1)
	require.NoError(t, err)
	require.Equal(t, uint(1), agent.UserID)
	require.Equal(t, []string{"billing", "chat"}, profile.Skills)
	require.Equal(t, 4, profile.MaxChatConcurrency)
	require.Equal(t, 2, profile.CurrentChatLoad)
	require.Equal(t, 12, profile.TotalTickets)
	require.Len(t, agent.Tickets, 1, "closed and cross-tenant tickets must be filtered")
	require.Equal(t, "open", agent.Tickets[0].Title)

	_, _, err = repo.GetAgentByUserID(context.Background(), 1)
	require.NoError(t, err, "unscoped context applies no tenant filter")

	_, _, err = repo.GetAgentByUserID(scoped, 999)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestGormRepositoryListAgentsLimits(t *testing.T) {
	db := newAgentCoverageDB(t)
	now := time.Now()
	seedCoverageUser(t, db, 1, "u1")
	seedCoverageUser(t, db, 2, "u2")
	seedCoverageUser(t, db, 3, "u3")
	require.NoError(t, db.Create(&[]models.Agent{
		{UserID: 1, Status: "online", CreatedAt: now, UpdatedAt: now.Add(-time.Minute)},
		{UserID: 2, Status: "online", CreatedAt: now, UpdatedAt: now},
		{UserID: 3, Status: "online", CreatedAt: now, UpdatedAt: now.Add(time.Minute)},
	}).Error)

	repo := NewGormRepository(db)
	agents, err := repo.ListAgents(context.Background(), 2)
	require.NoError(t, err)
	require.Len(t, agents, 2)
	require.Equal(t, uint(3), agents[0].UserID, "ordered by updated_at DESC")

	agents, err = repo.ListAgents(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, agents, 3, "non-positive limit falls back to default")

	agents, err = repo.ListAgents(context.Background(), 999)
	require.NoError(t, err)
	require.Len(t, agents, 3, "oversized limit falls back to default")

	require.NoError(t, db.Migrator().DropTable("agents"))
	_, err = repo.ListAgents(context.Background(), 10)
	require.ErrorContains(t, err, "failed to list agents")
}

func TestGormRepositoryRuntimeQueries(t *testing.T) {
	db := newAgentCoverageDB(t)
	now := time.Now()
	last := now.Add(-time.Minute)
	seedCoverageUser(t, db, 1, "online-agent")
	seedCoverageUser(t, db, 2, "offline-agent")
	require.NoError(t, db.Create(&[]models.Agent{
		{UserID: 1, Skills: "billing", Status: "online", MaxConcurrent: 3, CurrentLoad: 1, Rating: 4.5, AvgResponseTime: 25, LastActivityAt: &last, ConnectedAt: &now, CreatedAt: now, UpdatedAt: now},
		{UserID: 2, Status: "offline", CreatedAt: now, UpdatedAt: now},
	}).Error)

	repo := NewGormRepository(db)

	runtime, err := repo.GetAgentRuntimeByUserID(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, uint(1), runtime.UserID)
	require.Equal(t, "online-agent", runtime.Username)
	require.Equal(t, "online", runtime.Status)
	require.Equal(t, []string{"billing"}, runtime.Skills)
	require.True(t, runtime.LastActivity.Equal(last))
	require.True(t, runtime.ConnectedAt.Equal(now))
	require.Equal(t, 1, runtime.CurrentChatLoad)

	_, err = repo.GetAgentRuntimeByUserID(context.Background(), 2)
	require.ErrorContains(t, err, "agent runtime not found")
	_, err = repo.GetAgentRuntimeByUserID(context.Background(), 999)
	require.ErrorContains(t, err, "agent runtime not found")

	runtimes, err := repo.ListActiveAgentRuntimes(context.Background())
	require.NoError(t, err)
	require.Len(t, runtimes, 1)
	require.Equal(t, uint(1), runtimes[0].UserID)

	require.NoError(t, db.Migrator().DropTable("agents"))
	_, err = repo.GetAgentRuntimeByUserID(context.Background(), 1)
	require.ErrorContains(t, err, "agent runtime not found")
	_, err = repo.ListActiveAgentRuntimes(context.Background())
	require.ErrorContains(t, err, "failed to list active agent runtimes")
}

func TestGormRepositoryAgentFieldUpdates(t *testing.T) {
	db := newAgentCoverageDB(t)
	now := time.Now()
	require.NoError(t, db.Create(&models.Agent{UserID: 7, Status: "offline", CreatedAt: now, UpdatedAt: now}).Error)
	repo := NewGormRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.UpdatePresenceStatus(ctx, 7, agentdomain.PresenceStatusBusy))
	var busy models.Agent
	require.NoError(t, db.First(&busy, "user_id = ?", 7).Error)
	require.Equal(t, "busy", busy.Status)
	require.ErrorIs(t, repo.UpdatePresenceStatus(ctx, 999, agentdomain.PresenceStatusOnline), gorm.ErrRecordNotFound)

	require.NoError(t, repo.UpdateLastActivity(ctx, 7))
	var active models.Agent
	require.NoError(t, db.First(&active, "user_id = ?", 7).Error)
	require.NotNil(t, active.LastActivityAt)
	require.ErrorIs(t, repo.UpdateLastActivity(ctx, 999), gorm.ErrRecordNotFound)

	require.NoError(t, repo.SetConnectedTime(ctx, 7))
	var connected models.Agent
	require.NoError(t, db.First(&connected, "user_id = ?", 7).Error)
	require.NotNil(t, connected.ConnectedAt)
	require.ErrorIs(t, repo.SetConnectedTime(ctx, 999), gorm.ErrRecordNotFound)

	require.NoError(t, repo.ClearConnectedTime(ctx, 7))
	var cleared models.Agent
	require.NoError(t, db.First(&cleared, "user_id = ?", 7).Error)
	require.Nil(t, cleared.ConnectedAt)
	require.ErrorIs(t, repo.ClearConnectedTime(ctx, 999), gorm.ErrRecordNotFound)

	require.NoError(t, repo.UpdateChatLoad(ctx, 7, 3))
	var loaded models.Agent
	require.NoError(t, db.First(&loaded, "user_id = ?", 7).Error)
	require.Equal(t, 3, loaded.CurrentLoad)
	require.ErrorIs(t, repo.UpdateChatLoad(ctx, 999, 0), gorm.ErrRecordNotFound)
}

func TestGormRepositorySessionLifecycle(t *testing.T) {
	db := newAgentCoverageDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	now := time.Now()

	_, err := repo.GetSessionByID(ctx, "sess-1")
	require.ErrorContains(t, err, "session not found")
	require.NoError(t, db.Create(&models.Session{ID: "sess-1", UserID: 5, Status: "active", CreatedAt: now, UpdatedAt: now}).Error)
	session, err := repo.GetSessionByID(ctx, "sess-1")
	require.NoError(t, err)
	require.Equal(t, "sess-1", session.ID)

	require.ErrorIs(t, repo.AssignSession(ctx, "missing", 7), gorm.ErrRecordNotFound)
	require.NoError(t, repo.AssignSession(ctx, "sess-1", 7))
	var assigned models.Session
	require.NoError(t, db.First(&assigned, "id = ?", "sess-1").Error)
	require.Equal(t, "active", assigned.Status)
	require.NotNil(t, assigned.AgentID)
	require.Equal(t, uint(7), *assigned.AgentID)
	require.Nil(t, assigned.EndedAt)

	require.ErrorIs(t, repo.ReleaseSession(ctx, "sess-1", 999), gorm.ErrRecordNotFound)
	require.NoError(t, db.Create(&models.Session{ID: "sess-2", UserID: 5, AgentID: ptrUint(9), Status: "active", EndedAt: &now, CreatedAt: now, UpdatedAt: now}).Error)
	require.NoError(t, repo.ReleaseSession(ctx, "sess-2", 9))
	var released models.Session
	require.NoError(t, db.First(&released, "id = ?", "sess-2").Error)
	require.Equal(t, "ended", released.Status)
	require.NotNil(t, released.EndedAt)
	require.ErrorIs(t, repo.ReleaseSession(ctx, "missing", 7), gorm.ErrRecordNotFound)
}

func TestGormRepositoryStats(t *testing.T) {
	db := newAgentCoverageDB(t)
	now := time.Now()
	seedCoverageUser(t, db, 1, "u1")
	seedCoverageUser(t, db, 2, "u2")
	require.NoError(t, db.Create(&[]models.Agent{
		{UserID: 1, TenantID: "tenant-a", WorkspaceID: "ws-a", Status: "online", AvgResponseTime: 10, Rating: 4.0, CreatedAt: now, UpdatedAt: now},
		{UserID: 2, TenantID: "tenant-a", WorkspaceID: "ws-a", Status: "online", AvgResponseTime: 20, Rating: 5.0, CreatedAt: now, UpdatedAt: now},
	}).Error)

	repo := NewGormRepository(db)
	stats, err := repo.GetStats(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), stats.Total)
	require.Equal(t, int64(15), stats.AvgResponseTime)
	require.Equal(t, 4.5, stats.AvgRating)

	scoped := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-a")
	stats, err = repo.GetStats(scoped, ptrUint(1))
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.Total)

	scopedOther := platformauth.ContextWithScope(context.Background(), "tenant-b", "ws-b")
	stats, err = repo.GetStats(scopedOther, nil)
	require.NoError(t, err)
	require.Equal(t, int64(0), stats.Total)
	require.Equal(t, int64(0), stats.AvgResponseTime)
	require.Equal(t, 0.0, stats.AvgRating)
}

func TestGormRepositoryRevokeUserTokens(t *testing.T) {
	db := newAgentCoverageDB(t)
	seedCoverageUser(t, db, 1, "u1")
	repo := NewGormRepository(db)

	version, err := repo.RevokeUserTokens(context.Background(), 1, time.Now())
	require.NoError(t, err)
	require.Equal(t, 1, version)

	_, err = repo.RevokeUserTokens(context.Background(), 999, time.Now())
	require.ErrorContains(t, err, "user not found")
}

func TestApplyAgentScopeHelpers(t *testing.T) {
	db := newAgentCoverageDB(t)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-a")

	out := applyAgentScope(db.WithContext(ctx), ctx)
	require.NotNil(t, out)
	out = applyAgentScope(db.WithContext(context.Background()), context.Background())
	require.NotNil(t, out)

	agent := &models.Agent{}
	applyAgentScopeFields(ctx, agent)
	require.Equal(t, "tenant-a", agent.TenantID)
	require.Equal(t, "ws-a", agent.WorkspaceID)

	empty := &models.Agent{}
	applyAgentScopeFields(context.Background(), empty)
	require.Empty(t, empty.TenantID)
	require.Empty(t, empty.WorkspaceID)

	applyAgentScopeFields(ctx, nil) // must not panic
}

func TestSplitSkills(t *testing.T) {
	require.Nil(t, splitSkills(""))
	require.Equal(t, []string{"a", "b", "c"}, splitSkills("a, b,,  c ,"))
	require.Equal(t, []string{"a"}, splitSkills(" a "))
}

func ptrUint(v uint) *uint { return &v }
