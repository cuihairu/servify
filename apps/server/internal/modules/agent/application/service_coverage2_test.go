package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	agentdomain "servify/apps/server/internal/modules/agent/domain"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// mockAgentRepo is a configurable Repository used to exercise every Service branch.
type mockAgentRepo struct {
	profile     *agentdomain.AgentProfile
	model       *models.Agent
	lookupErr   error
	createErr   error
	created     *createCall
	runtime     *AgentRuntimeDTO
	runtimeErr  error
	runtimes    []AgentRuntimeDTO
	listActErr  error
	listResult  []models.Agent
	listErr     error
	statusErr   error
	chatLoadErr error
	session     *models.Session
	sessionErr  error
	assignErr   error
	releaseErr  error
	stats       *AgentStatsDTO
	statsErr    error

	statuses  []agentdomain.PresenceStatus
	chatLoads []chatLoadCall
	revokedAt time.Time
	revoked   int
}

type createCall struct {
	userID      uint
	department  string
	skills      []string
	concurrency int
}

type chatLoadCall struct {
	userID uint
	load   int
}

func (m *mockAgentRepo) CreateAgent(ctx context.Context, userID uint, department string, skills []string, maxChatConcurrency int) (*agentdomain.AgentProfile, error) {
	m.created = &createCall{userID: userID, department: department, skills: skills, concurrency: maxChatConcurrency}
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.profile = &agentdomain.AgentProfile{UserID: userID, Department: department, Skills: skills, MaxChatConcurrency: maxChatConcurrency}
	m.model = &models.Agent{UserID: userID, Department: department, MaxConcurrent: maxChatConcurrency}
	return m.profile, nil
}

func (m *mockAgentRepo) GetAgentByUserID(ctx context.Context, userID uint) (*agentdomain.AgentProfile, *models.Agent, error) {
	if m.lookupErr != nil {
		return nil, nil, m.lookupErr
	}
	if m.profile == nil || m.model == nil || m.model.UserID != userID {
		return nil, nil, gorm.ErrRecordNotFound
	}
	return m.profile, m.model, nil
}

func (m *mockAgentRepo) GetAgentRuntimeByUserID(ctx context.Context, userID uint) (*AgentRuntimeDTO, error) {
	if m.runtimeErr != nil {
		return nil, m.runtimeErr
	}
	if m.runtime != nil && m.runtime.UserID == userID {
		copyRT := *m.runtime
		return &copyRT, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *mockAgentRepo) ListActiveAgentRuntimes(ctx context.Context) ([]AgentRuntimeDTO, error) {
	if m.listActErr != nil {
		return nil, m.listActErr
	}
	out := make([]AgentRuntimeDTO, len(m.runtimes))
	copy(out, m.runtimes)
	return out, nil
}

func (m *mockAgentRepo) ListAgents(ctx context.Context, limit int) ([]models.Agent, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listResult, nil
}

func (m *mockAgentRepo) UpdatePresenceStatus(ctx context.Context, userID uint, status agentdomain.PresenceStatus) error {
	if m.statusErr != nil {
		return m.statusErr
	}
	m.statuses = append(m.statuses, status)
	return nil
}

func (m *mockAgentRepo) UpdateChatLoad(ctx context.Context, userID uint, currentLoad int) error {
	if m.chatLoadErr != nil {
		return m.chatLoadErr
	}
	m.chatLoads = append(m.chatLoads, chatLoadCall{userID: userID, load: currentLoad})
	return nil
}

func (m *mockAgentRepo) UpdateLastActivity(ctx context.Context, userID uint) error { return nil }

func (m *mockAgentRepo) SetConnectedTime(ctx context.Context, userID uint) error { return nil }

func (m *mockAgentRepo) ClearConnectedTime(ctx context.Context, userID uint) error { return nil }

func (m *mockAgentRepo) GetSessionByID(ctx context.Context, sessionID string) (*models.Session, error) {
	if m.sessionErr != nil {
		return nil, m.sessionErr
	}
	return m.session, nil
}

func (m *mockAgentRepo) AssignSession(ctx context.Context, sessionID string, agentUserID uint) error {
	return m.assignErr
}

func (m *mockAgentRepo) ReleaseSession(ctx context.Context, sessionID string, agentUserID uint) error {
	return m.releaseErr
}

func (m *mockAgentRepo) GetStats(ctx context.Context, agentUserID *uint) (*AgentStatsDTO, error) {
	if m.statsErr != nil {
		return nil, m.statsErr
	}
	if m.stats == nil {
		m.stats = &AgentStatsDTO{Total: 3}
	}
	return m.stats, nil
}

func (m *mockAgentRepo) RevokeUserTokens(ctx context.Context, userID uint, revokeAt time.Time) (int, error) {
	m.revokedAt = revokeAt
	m.revoked++
	return m.revoked, nil
}

// ---- 选坐席（selection）桩 ----

func (m *mockAgentRepo) GetLastAgentForCustomer(ctx context.Context, customerUserID uint, since time.Time) (*uint, error) {
	return nil, nil
}

func (m *mockAgentRepo) GetAgentGroup(ctx context.Context, id uint) (*models.AgentGroup, error) {
	return nil, gorm.ErrRecordNotFound
}

func (m *mockAgentRepo) ListAgentGroups(ctx context.Context) ([]models.AgentGroup, error) {
	return nil, nil
}

func (m *mockAgentRepo) CreateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return nil
}

func (m *mockAgentRepo) UpdateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return nil
}

func (m *mockAgentRepo) DeleteAgentGroup(ctx context.Context, id uint) error {
	return nil
}

func (m *mockAgentRepo) ReplaceGroupMembers(ctx context.Context, groupID uint, agentUserIDs []uint) error {
	return nil
}

func (m *mockAgentRepo) ListEnabledGroupMemberIDs(ctx context.Context, groupID uint) ([]uint, error) {
	return nil, nil
}

// mockRegistry is a configurable RuntimeRegistry.
type mockRegistry struct {
	items       map[uint]AgentRuntimeDTO
	goOnlineErr error
	assignErr   error
	releaseOK   bool
	offlineHit  bool
	statuses    []agentdomain.PresenceStatus
	transfers   int
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{items: map[uint]AgentRuntimeDTO{}, releaseOK: true}
}

func (m *mockRegistry) GoOnline(profile agentdomain.AgentProfile) (AgentRuntimeDTO, error) {
	if m.goOnlineErr != nil {
		return AgentRuntimeDTO{}, m.goOnlineErr
	}
	dto := AgentRuntimeDTO{UserID: profile.UserID, Status: string(agentdomain.PresenceStatusOnline), MaxChatConcurrency: profile.MaxChatConcurrency, CurrentChatLoad: profile.CurrentChatLoad}
	m.items[profile.UserID] = dto
	return dto, nil
}

func (m *mockRegistry) GoOffline(userID uint) {
	m.offlineHit = true
	delete(m.items, userID)
}

func (m *mockRegistry) UpdateStatus(userID uint, status agentdomain.PresenceStatus) {
	m.statuses = append(m.statuses, status)
	if item, ok := m.items[userID]; ok {
		item.Status = string(status)
		m.items[userID] = item
	}
}

func (m *mockRegistry) AssignSession(userID uint, session *models.Session) (AgentRuntimeDTO, error) {
	if m.assignErr != nil {
		return AgentRuntimeDTO{}, m.assignErr
	}
	item := m.items[userID]
	item.CurrentChatLoad++
	m.items[userID] = item
	return item, nil
}

func (m *mockRegistry) ReleaseSession(userID uint, sessionID string) (AgentRuntimeDTO, bool) {
	if !m.releaseOK {
		return AgentRuntimeDTO{}, false
	}
	item := m.items[userID]
	if item.CurrentChatLoad > 0 {
		item.CurrentChatLoad--
	}
	m.items[userID] = item
	return item, true
}

func (m *mockRegistry) ApplyTransfer(sessionID string, fromAgentID *uint, toAgentID uint) {
	m.transfers++
}

func (m *mockRegistry) Get(userID uint) (AgentRuntimeDTO, bool) {
	item, ok := m.items[userID]
	return item, ok
}

func (m *mockRegistry) List() []AgentRuntimeDTO {
	out := make([]AgentRuntimeDTO, 0, len(m.items))
	for _, item := range m.items {
		out = append(out, item)
	}
	return out
}

func onlineRuntime(userID uint, load, max int) AgentRuntimeDTO {
	return AgentRuntimeDTO{UserID: userID, Status: string(agentdomain.PresenceStatusOnline), CurrentChatLoad: load, MaxChatConcurrency: max, Rating: 4.0}
}

func onlineRuntimePtr(userID uint, load, max int) *AgentRuntimeDTO {
	dto := onlineRuntime(userID, load, max)
	return &dto
}

func TestServiceCreateAgentValidationAndPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("requires user id", func(t *testing.T) {
		svc := NewService(&mockAgentRepo{}, newMockRegistry())
		_, err := svc.CreateAgent(ctx, CreateAgentCommand{})
		require.EqualError(t, err, "user_id required")
	})

	t.Run("rejects existing agent", func(t *testing.T) {
		repo := &mockAgentRepo{
			profile: &agentdomain.AgentProfile{UserID: 7},
			model:   &models.Agent{UserID: 7},
		}
		svc := NewService(repo, newMockRegistry())
		_, err := svc.CreateAgent(ctx, CreateAgentCommand{UserID: 7})
		require.EqualError(t, err, "user is already an agent")
	})

	t.Run("propagates create error", func(t *testing.T) {
		repo := &mockAgentRepo{createErr: errors.New("boom")}
		svc := NewService(repo, newMockRegistry())
		_, err := svc.CreateAgent(ctx, CreateAgentCommand{UserID: 7})
		require.EqualError(t, err, "boom")
	})

	t.Run("creates agent with sanitized inputs", func(t *testing.T) {
		repo := &mockAgentRepo{}
		svc := NewService(repo, newMockRegistry())
		model, err := svc.CreateAgent(ctx, CreateAgentCommand{
			UserID:             7,
			Department:         "support",
			Skills:             []string{"billing", "", "billing", "chat"},
			MaxChatConcurrency: 0,
		})
		require.NoError(t, err)
		require.NotNil(t, model)
		require.Equal(t, uint(7), repo.created.userID)
		require.Equal(t, []string{"billing", "chat"}, repo.created.skills)
		require.Equal(t, 5, repo.created.concurrency)
	})
}

func TestServiceGetAndListAgents(t *testing.T) {
	ctx := context.Background()
	repo := &mockAgentRepo{
		profile:    &agentdomain.AgentProfile{UserID: 7},
		model:      &models.Agent{UserID: 7, Department: "support"},
		listResult: []models.Agent{{UserID: 1}, {UserID: 2}},
	}
	svc := NewService(repo, newMockRegistry())

	model, err := svc.GetAgentByUserID(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, uint(7), model.UserID)

	_, err = svc.GetAgentByUserID(ctx, 9)
	require.Error(t, err)

	agents, err := svc.ListAgents(ctx, 10)
	require.NoError(t, err)
	require.Len(t, agents, 2)

	repo.listErr = errors.New("list failed")
	_, err = svc.ListAgents(ctx, 10)
	require.EqualError(t, err, "list failed")
}

func TestServicePresenceTransitions(t *testing.T) {
	ctx := context.Background()
	profile := &agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 2}
	model := &models.Agent{UserID: 7}

	t.Run("go online", func(t *testing.T) {
		repo := &mockAgentRepo{profile: profile, model: model}
		registry := newMockRegistry()
		svc := NewService(repo, registry)
		require.NoError(t, svc.GoOnline(ctx, 7))
		require.Equal(t, []agentdomain.PresenceStatus{agentdomain.PresenceStatusOnline}, repo.statuses)
		_, ok := registry.Get(7)
		require.True(t, ok)
	})

	t.Run("go online without profile", func(t *testing.T) {
		repo := &mockAgentRepo{lookupErr: errors.New("no agent")}
		svc := NewService(repo, newMockRegistry())
		require.EqualError(t, svc.GoOnline(ctx, 7), "no agent")
	})

	t.Run("go online status failure", func(t *testing.T) {
		repo := &mockAgentRepo{profile: profile, model: model, statusErr: errors.New("db down")}
		svc := NewService(repo, newMockRegistry())
		require.EqualError(t, svc.GoOnline(ctx, 7), "db down")
	})

	t.Run("go online registry failure", func(t *testing.T) {
		repo := &mockAgentRepo{profile: profile, model: model}
		registry := newMockRegistry()
		registry.goOnlineErr = errors.New("registry down")
		svc := NewService(repo, registry)
		require.EqualError(t, svc.GoOnline(ctx, 7), "registry down")
	})

	t.Run("go offline", func(t *testing.T) {
		repo := &mockAgentRepo{profile: profile, model: model}
		registry := newMockRegistry()
		svc := NewService(repo, registry)
		require.NoError(t, svc.GoOffline(ctx, 7))
		require.Equal(t, []agentdomain.PresenceStatus{agentdomain.PresenceStatusOffline}, repo.statuses)
		require.True(t, registry.offlineHit)
	})

	t.Run("go offline status failure", func(t *testing.T) {
		repo := &mockAgentRepo{statusErr: errors.New("db down")}
		svc := NewService(repo, newMockRegistry())
		require.EqualError(t, svc.GoOffline(ctx, 7), "db down")
	})

	t.Run("update status invalid value", func(t *testing.T) {
		svc := NewService(&mockAgentRepo{}, newMockRegistry())
		require.EqualError(t, svc.UpdateStatus(ctx, 7, "sleeping"), "invalid status: sleeping")
	})

	t.Run("update status failure", func(t *testing.T) {
		repo := &mockAgentRepo{statusErr: errors.New("db down")}
		svc := NewService(repo, newMockRegistry())
		require.EqualError(t, svc.UpdateStatus(ctx, 7, "busy"), "db down")
	})

	t.Run("update status success", func(t *testing.T) {
		repo := &mockAgentRepo{profile: profile, model: model}
		registry := newMockRegistry()
		svc := NewService(repo, registry)
		require.NoError(t, svc.UpdateStatus(ctx, 7, "away"))
		require.Equal(t, []agentdomain.PresenceStatus{agentdomain.PresenceStatusAway}, repo.statuses)
		require.Equal(t, []agentdomain.PresenceStatus{agentdomain.PresenceStatusAway}, registry.statuses)
	})

	t.Run("mark busy and away", func(t *testing.T) {
		repo := &mockAgentRepo{profile: profile, model: model}
		svc := NewService(repo, newMockRegistry())
		require.NoError(t, svc.MarkBusy(ctx, 7))
		require.NoError(t, svc.MarkAway(ctx, 7))
		require.Equal(t, []agentdomain.PresenceStatus{agentdomain.PresenceStatusBusy, agentdomain.PresenceStatusAway}, repo.statuses)
	})
}

func TestServiceAssignSessionBranches(t *testing.T) {
	ctx := context.Background()
	session := &models.Session{ID: "sess-1"}

	t.Run("agent not found", func(t *testing.T) {
		repo := &mockAgentRepo{runtimeErr: gorm.ErrRecordNotFound, lookupErr: errors.New("agent missing")}
		svc := NewService(repo, newMockRegistry())
		require.EqualError(t, svc.AssignSession(ctx, "sess-1", 7), "agent missing")
	})

	t.Run("agent not online", func(t *testing.T) {
		repo := &mockAgentRepo{runtimeErr: gorm.ErrRecordNotFound, profile: &agentdomain.AgentProfile{UserID: 7}, model: &models.Agent{UserID: 7}}
		svc := NewService(repo, newMockRegistry())
		require.EqualError(t, svc.AssignSession(ctx, "sess-1", 7), "agent 7 is not online")
	})

	t.Run("agent at capacity", func(t *testing.T) {
		repo := &mockAgentRepo{runtime: &AgentRuntimeDTO{UserID: 7, CurrentChatLoad: 2, MaxChatConcurrency: 2}}
		svc := NewService(repo, newMockRegistry())
		require.EqualError(t, svc.AssignSession(ctx, "sess-1", 7), "agent 7 is at maximum capacity")
	})

	t.Run("session lookup failure", func(t *testing.T) {
		repo := &mockAgentRepo{runtime: onlineRuntimePtr(7, 0, 2), sessionErr: errors.New("no session")}
		svc := NewService(repo, newMockRegistry())
		require.EqualError(t, svc.AssignSession(ctx, "sess-1", 7), "no session")
	})

	t.Run("registry failure falls back to persisted load", func(t *testing.T) {
		repo := &mockAgentRepo{runtime: onlineRuntimePtr(7, 1, 2), session: session}
		registry := newMockRegistry()
		registry.assignErr = errors.New("registry down")
		svc := NewService(repo, registry)
		require.NoError(t, svc.AssignSession(ctx, "sess-1", 7))
		require.Equal(t, []chatLoadCall{{userID: 7, load: 2}}, repo.chatLoads)
	})

	t.Run("repo assign failure", func(t *testing.T) {
		repo := &mockAgentRepo{runtime: onlineRuntimePtr(7, 0, 2), session: session, assignErr: errors.New("assign failed")}
		svc := NewService(repo, newMockRegistry())
		require.EqualError(t, svc.AssignSession(ctx, "sess-1", 7), "assign failed")
	})

	t.Run("chat load update failure", func(t *testing.T) {
		repo := &mockAgentRepo{runtime: onlineRuntimePtr(7, 0, 2), session: session, chatLoadErr: errors.New("load failed")}
		svc := NewService(repo, newMockRegistry())
		require.EqualError(t, svc.AssignSession(ctx, "sess-1", 7), "load failed")
	})

	t.Run("success", func(t *testing.T) {
		repo := &mockAgentRepo{runtime: onlineRuntimePtr(7, 0, 2), session: session}
		registry := newMockRegistry()
		registry.items[7] = onlineRuntime(7, 0, 2)
		svc := NewService(repo, registry)
		require.NoError(t, svc.AssignSession(ctx, "sess-1", 7))
		require.Equal(t, []chatLoadCall{{userID: 7, load: 1}}, repo.chatLoads)
	})
}

func TestServiceReleaseSessionBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("repo release failure", func(t *testing.T) {
		repo := &mockAgentRepo{releaseErr: errors.New("release failed")}
		svc := NewService(repo, newMockRegistry())
		require.EqualError(t, svc.ReleaseSession(ctx, "sess-1", 7), "release failed")
	})

	t.Run("registry hit updates load", func(t *testing.T) {
		repo := &mockAgentRepo{}
		registry := newMockRegistry()
		registry.items[7] = onlineRuntime(7, 2, 2)
		svc := NewService(repo, registry)
		require.NoError(t, svc.ReleaseSession(ctx, "sess-1", 7))
		require.Equal(t, []chatLoadCall{{userID: 7, load: 1}}, repo.chatLoads)
	})

	t.Run("registry miss without runtime", func(t *testing.T) {
		repo := &mockAgentRepo{}
		registry := newMockRegistry()
		registry.releaseOK = false
		svc := NewService(repo, registry)
		require.NoError(t, svc.ReleaseSession(ctx, "sess-1", 7))
		require.Empty(t, repo.chatLoads)
	})

	t.Run("registry miss clamps negative load", func(t *testing.T) {
		repo := &mockAgentRepo{runtime: onlineRuntimePtr(7, 0, 2)}
		registry := newMockRegistry()
		registry.releaseOK = false
		svc := NewService(repo, registry)
		require.NoError(t, svc.ReleaseSession(ctx, "sess-1", 7))
		require.Equal(t, []chatLoadCall{{userID: 7, load: 0}}, repo.chatLoads)
	})

	t.Run("registry miss decrements persisted load", func(t *testing.T) {
		repo := &mockAgentRepo{runtime: onlineRuntimePtr(7, 2, 2)}
		registry := newMockRegistry()
		registry.releaseOK = false
		svc := NewService(repo, registry)
		require.NoError(t, svc.ReleaseSession(ctx, "sess-1", 7))
		require.Equal(t, []chatLoadCall{{userID: 7, load: 1}}, repo.chatLoads)
	})
}

func TestServiceFindAvailableAgentBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("list failure", func(t *testing.T) {
		repo := &mockAgentRepo{listActErr: errors.New("db down")}
		svc := NewService(repo, newMockRegistry())
		_, err := svc.FindAvailableAgent(ctx, nil, "")
		require.EqualError(t, err, "db down")
	})

	t.Run("no candidates", func(t *testing.T) {
		repo := &mockAgentRepo{}
		svc := NewService(repo, newMockRegistry())
		_, err := svc.FindAvailableAgent(ctx, nil, "")
		require.EqualError(t, err, "no available agent found")
	})

	t.Run("skips offline and saturated agents", func(t *testing.T) {
		repo := &mockAgentRepo{runtimes: []AgentRuntimeDTO{
			{UserID: 1, Status: string(agentdomain.PresenceStatusOffline), CurrentChatLoad: 0, MaxChatConcurrency: 2},
			{UserID: 2, Status: string(agentdomain.PresenceStatusOnline), CurrentChatLoad: 2, MaxChatConcurrency: 2},
		}}
		svc := NewService(repo, newMockRegistry())
		_, err := svc.FindAvailableAgent(ctx, nil, "")
		require.EqualError(t, err, "no available agent found")
	})

	t.Run("picks highest score", func(t *testing.T) {
		repo := &mockAgentRepo{runtimes: []AgentRuntimeDTO{
			{UserID: 1, Status: string(agentdomain.PresenceStatusOnline), CurrentChatLoad: 1, MaxChatConcurrency: 2, Rating: 3.0, Skills: []string{"billing"}},
			{UserID: 2, Status: string(agentdomain.PresenceStatusOnline), CurrentChatLoad: 0, MaxChatConcurrency: 2, Rating: 4.5, Skills: []string{"billing", "chat"}, AvgResponseTime: 300},
		}}
		svc := NewService(repo, newMockRegistry())
		got, err := svc.FindAvailableAgent(ctx, []string{"billing", "chat"}, "urgent")
		require.NoError(t, err)
		require.Equal(t, uint(2), got.UserID)
	})
}

func TestServiceOnlineAgentQueries(t *testing.T) {
	ctx := context.Background()

	t.Run("online agents list failure", func(t *testing.T) {
		repo := &mockAgentRepo{listActErr: errors.New("db down")}
		svc := NewService(repo, newMockRegistry())
		require.Nil(t, svc.GetOnlineAgents(ctx))
	})

	t.Run("online agents merges registry metadata", func(t *testing.T) {
		persisted := time.Now().Add(-2 * time.Hour)
		fresher := time.Now().Add(-1 * time.Hour)
		older := time.Now().Add(-3 * time.Hour)
		repo := &mockAgentRepo{runtimes: []AgentRuntimeDTO{
			{UserID: 1, Status: string(agentdomain.PresenceStatusOnline), LastActivity: persisted, ConnectedAt: persisted},
			{UserID: 2, Status: string(agentdomain.PresenceStatusOnline), LastActivity: persisted, ConnectedAt: persisted},
		}}
		registry := newMockRegistry()
		registry.items[1] = AgentRuntimeDTO{UserID: 1, LastActivity: fresher, ConnectedAt: older}
		registry.items[2] = AgentRuntimeDTO{UserID: 2}
		svc := NewService(repo, registry)

		agents := svc.GetOnlineAgents(ctx)
		require.Len(t, agents, 2)
		require.True(t, agents[0].LastActivity.Equal(fresher), "expected fresher registry activity to win")
		require.True(t, agents[0].ConnectedAt.Equal(older), "expected older registry connection time to win")
		require.True(t, agents[1].LastActivity.IsZero(), "expected zero registry activity to be ignored")
	})

	t.Run("single online agent failure", func(t *testing.T) {
		repo := &mockAgentRepo{runtimeErr: errors.New("db down")}
		svc := NewService(repo, newMockRegistry())
		_, err := svc.GetOnlineAgent(ctx, 7)
		require.EqualError(t, err, "db down")
	})

	t.Run("single online agent success", func(t *testing.T) {
		repo := &mockAgentRepo{runtime: onlineRuntimePtr(7, 1, 2)}
		svc := NewService(repo, newMockRegistry())
		got, err := svc.GetOnlineAgent(ctx, 7)
		require.NoError(t, err)
		require.Equal(t, uint(7), got.UserID)
	})
}

func TestServiceStatsBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("stats failure", func(t *testing.T) {
		repo := &mockAgentRepo{statsErr: errors.New("db down")}
		svc := NewService(repo, newMockRegistry())
		_, err := svc.GetStats(ctx, nil)
		require.EqualError(t, err, "db down")
	})

	t.Run("runtimes failure", func(t *testing.T) {
		repo := &mockAgentRepo{stats: &AgentStatsDTO{Total: 3}, listActErr: errors.New("db down")}
		svc := NewService(repo, newMockRegistry())
		_, err := svc.GetStats(ctx, nil)
		require.EqualError(t, err, "db down")
	})

	t.Run("counts online and busy agents", func(t *testing.T) {
		repo := &mockAgentRepo{stats: &AgentStatsDTO{Total: 3, AvgResponseTime: 15, AvgRating: 4.5}, runtimes: []AgentRuntimeDTO{
			{UserID: 1, Status: string(agentdomain.PresenceStatusOnline), CurrentChatLoad: 0, MaxChatConcurrency: 2},
			{UserID: 2, Status: string(agentdomain.PresenceStatusBusy), CurrentChatLoad: 0, MaxChatConcurrency: 2},
			{UserID: 3, Status: string(agentdomain.PresenceStatusOnline), CurrentChatLoad: 2, MaxChatConcurrency: 2},
		}}
		svc := NewService(repo, newMockRegistry())
		stats, err := svc.GetStats(ctx, nil)
		require.NoError(t, err)
		require.Equal(t, int64(3), stats.Total)
		require.Equal(t, int64(3), stats.Online)
		require.Equal(t, int64(2), stats.Busy)
		require.Equal(t, int64(15), stats.AvgResponseTime)
		require.Equal(t, 4.5, stats.AvgRating)
	})
}

func TestServiceRevokeUserTokensBranches(t *testing.T) {
	ctx := context.Background()

	svc := NewService(&mockAgentRepo{}, newMockRegistry())
	_, err := svc.RevokeUserTokens(ctx, 0, time.Now())
	require.EqualError(t, err, "user_id required")

	repo := &mockAgentRepo{}
	svc = NewService(repo, newMockRegistry())
	count, err := svc.RevokeUserTokens(ctx, 7, time.Time{})
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.False(t, repo.revokedAt.IsZero(), "expected zero revoke time to default to now")
}

func TestServiceApplySessionTransferBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("target only", func(t *testing.T) {
		repo := &mockAgentRepo{}
		registry := newMockRegistry()
		registry.items[9] = onlineRuntime(9, 1, 2)
		svc := NewService(repo, registry)
		require.NoError(t, svc.ApplySessionTransfer(ctx, "sess-1", nil, 9))
		require.Equal(t, 1, registry.transfers)
		require.Equal(t, []chatLoadCall{{userID: 9, load: 1}}, repo.chatLoads)
	})

	t.Run("target load update failure", func(t *testing.T) {
		repo := &mockAgentRepo{chatLoadErr: errors.New("db down")}
		registry := newMockRegistry()
		registry.items[9] = onlineRuntime(9, 1, 2)
		svc := NewService(repo, registry)
		require.EqualError(t, svc.ApplySessionTransfer(ctx, "sess-1", nil, 9), "db down")
	})

	t.Run("target unknown skips load update", func(t *testing.T) {
		repo := &mockAgentRepo{}
		registry := newMockRegistry()
		svc := NewService(repo, registry)
		require.NoError(t, svc.ApplySessionTransfer(ctx, "sess-1", nil, 9))
		require.Empty(t, repo.chatLoads)
	})

	t.Run("source and target", func(t *testing.T) {
		repo := &mockAgentRepo{}
		registry := newMockRegistry()
		registry.items[7] = onlineRuntime(7, 2, 2)
		registry.items[9] = onlineRuntime(9, 1, 2)
		svc := NewService(repo, registry)
		from := uint(7)
		require.NoError(t, svc.ApplySessionTransfer(ctx, "sess-1", &from, 9))
		require.Equal(t, []chatLoadCall{
			{userID: 9, load: 1},
			{userID: 7, load: 2},
		}, repo.chatLoads)
	})

	t.Run("source load update failure", func(t *testing.T) {
		repo := &mockAgentRepo{chatLoadErr: errors.New("db down")}
		registry := newMockRegistry()
		registry.items[7] = onlineRuntime(7, 2, 2)
		svc := NewService(repo, registry)
		from := uint(7)
		require.EqualError(t, svc.ApplySessionTransfer(ctx, "sess-1", &from, 9), "db down")
	})

	t.Run("source unknown skips load update", func(t *testing.T) {
		repo := &mockAgentRepo{}
		registry := newMockRegistry()
		registry.items[9] = onlineRuntime(9, 1, 2)
		svc := NewService(repo, registry)
		from := uint(7)
		require.NoError(t, svc.ApplySessionTransfer(ctx, "sess-1", &from, 9))
		require.Equal(t, []chatLoadCall{{userID: 9, load: 1}}, repo.chatLoads)
	})
}

func TestSanitizeSkillsDeduplicatesAndDropsEmpty(t *testing.T) {
	require.Empty(t, sanitizeSkills(nil))
	require.Equal(t, []string{"a", "b"}, sanitizeSkills([]string{"a", "", "a", "b", ""}))
}

func TestNormalizeChatConcurrency(t *testing.T) {
	require.Equal(t, 5, normalizeChatConcurrency(0))
	require.Equal(t, 5, normalizeChatConcurrency(-3))
	require.Equal(t, 7, normalizeChatConcurrency(7))
}

func TestParsePresenceStatus(t *testing.T) {
	for _, value := range []string{"online", "busy", "away", "offline"} {
		got, err := parsePresenceStatus(value)
		require.NoError(t, err)
		require.Equal(t, agentdomain.PresenceStatus(value), got)
	}
	_, err := parsePresenceStatus("napping")
	require.EqualError(t, err, "invalid status: napping")
}

func TestCalculateScore(t *testing.T) {
	zeroConcurrency := calculateScore(AgentRuntimeDTO{Rating: 4}, nil, "")
	require.Equal(t, 4.0, zeroConcurrency)

	fast := calculateScore(AgentRuntimeDTO{Rating: 4, MaxChatConcurrency: 2, CurrentChatLoad: 0, AvgResponseTime: 10}, nil, "")
	require.Equal(t, 4.0+3.0+2.0, fast)

	slow := calculateScore(AgentRuntimeDTO{Rating: 4, MaxChatConcurrency: 2, CurrentChatLoad: 2, AvgResponseTime: 300}, nil, "")
	require.Equal(t, 4.0+1.0, slow)

	matched := calculateScore(AgentRuntimeDTO{Rating: 4, Skills: []string{"billing"}}, []string{"billing", "chat"}, "")
	require.Equal(t, 4.0+1.0, matched)

	urgent := calculateScore(AgentRuntimeDTO{Rating: 4}, nil, "urgent")
	require.Equal(t, 4.2, urgent)

	high := calculateScore(AgentRuntimeDTO{Rating: 4}, nil, "high")
	require.Equal(t, 4.1, high)
}
