package services

import (
	"context"
	"testing"
	"time"

	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdomain "servify/apps/server/internal/modules/agent/domain"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

type legacyAdapterRepo struct {
	runtimes map[uint]agentapp.AgentRuntimeDTO
}

func (r *legacyAdapterRepo) CreateAgent(ctx context.Context, userID uint, department string, skills []string, maxChatConcurrency int) (*agentdomain.AgentProfile, error) {
	return nil, nil
}

func (r *legacyAdapterRepo) GetAgentByUserID(ctx context.Context, userID uint) (*agentdomain.AgentProfile, *models.Agent, error) {
	return nil, nil, nil
}

func (r *legacyAdapterRepo) ListAgents(ctx context.Context, limit int) ([]models.Agent, error) {
	return nil, nil
}

func (r *legacyAdapterRepo) GetAgentRuntimeByUserID(ctx context.Context, userID uint) (*agentapp.AgentRuntimeDTO, error) {
	if runtime, ok := r.runtimes[userID]; ok {
		copy := runtime
		return &copy, nil
	}
	return nil, nil
}

func (r *legacyAdapterRepo) ListActiveAgentRuntimes(ctx context.Context) ([]agentapp.AgentRuntimeDTO, error) {
	out := make([]agentapp.AgentRuntimeDTO, 0, len(r.runtimes))
	for _, runtime := range r.runtimes {
		out = append(out, runtime)
	}
	return out, nil
}

func (r *legacyAdapterRepo) UpdatePresenceStatus(ctx context.Context, userID uint, status agentdomain.PresenceStatus) error {
	return nil
}

func (r *legacyAdapterRepo) UpdateChatLoad(ctx context.Context, userID uint, currentLoad int) error {
	return nil
}

func (r *legacyAdapterRepo) GetSessionByID(ctx context.Context, sessionID string) (*models.Session, error) {
	return nil, nil
}

func (r *legacyAdapterRepo) AssignSession(ctx context.Context, sessionID string, agentUserID uint) error {
	return nil
}

func (r *legacyAdapterRepo) ReleaseSession(ctx context.Context, sessionID string, agentUserID uint) error {
	return nil
}

func (r *legacyAdapterRepo) GetStats(ctx context.Context, agentUserID *uint) (*agentapp.AgentStatsDTO, error) {
	return &agentapp.AgentStatsDTO{}, nil
}

func (r *legacyAdapterRepo) RevokeUserTokens(ctx context.Context, userID uint, revokeAt time.Time) (int, error) {
	return 0, nil
}

// ---- 选坐席（selection）桩 ----

func (r *legacyAdapterRepo) GetLastAgentForCustomer(ctx context.Context, customerUserID uint, since time.Time) (*uint, error) {
	return nil, nil
}

func (r *legacyAdapterRepo) GetAgentGroup(ctx context.Context, id uint) (*models.AgentGroup, error) {
	return nil, gorm.ErrRecordNotFound
}

func (r *legacyAdapterRepo) ListAgentGroups(ctx context.Context) ([]models.AgentGroup, error) {
	return nil, nil
}

func (r *legacyAdapterRepo) CreateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return nil
}

func (r *legacyAdapterRepo) UpdateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return nil
}

func (r *legacyAdapterRepo) DeleteAgentGroup(ctx context.Context, id uint) error {
	return nil
}

func (r *legacyAdapterRepo) ReplaceGroupMembers(ctx context.Context, groupID uint, agentUserIDs []uint) error {
	return nil
}

func (r *legacyAdapterRepo) ListEnabledGroupMemberIDs(ctx context.Context, groupID uint) ([]uint, error) {
	return nil, nil
}

func (r *legacyAdapterRepo) UpdateLastActivity(ctx context.Context, userID uint) error {
	return nil
}

func (r *legacyAdapterRepo) SetConnectedTime(ctx context.Context, userID uint) error {
	return nil
}

func (r *legacyAdapterRepo) ClearConnectedTime(ctx context.Context, userID uint) error {
	return nil
}

type legacyAdapterRegistry struct {
	items map[uint]agentapp.AgentRuntimeDTO
}

func (r *legacyAdapterRegistry) GoOnline(profile agentdomain.AgentProfile) (agentapp.AgentRuntimeDTO, error) {
	return agentapp.AgentRuntimeDTO{}, nil
}

func (r *legacyAdapterRegistry) GoOffline(userID uint) {}

func (r *legacyAdapterRegistry) UpdateStatus(userID uint, status agentdomain.PresenceStatus) {}

func (r *legacyAdapterRegistry) AssignSession(userID uint, session *models.Session) (agentapp.AgentRuntimeDTO, error) {
	return agentapp.AgentRuntimeDTO{}, nil
}

func (r *legacyAdapterRegistry) ReleaseSession(userID uint, sessionID string) (agentapp.AgentRuntimeDTO, bool) {
	return agentapp.AgentRuntimeDTO{}, false
}

func (r *legacyAdapterRegistry) ApplyTransfer(sessionID string, fromAgentID *uint, toAgentID uint) {}

func (r *legacyAdapterRegistry) Get(userID uint) (agentapp.AgentRuntimeDTO, bool) {
	item, ok := r.items[userID]
	return item, ok
}

func (r *legacyAdapterRegistry) List() []agentapp.AgentRuntimeDTO {
	out := make([]agentapp.AgentRuntimeDTO, 0, len(r.items))
	for _, item := range r.items {
		out = append(out, item)
	}
	return out
}

func TestAgentLegacyRuntimeAdapter_Sync(t *testing.T) {
	cache := &agentRuntimeCache{}
	adapter := newAgentLegacyRuntimeAdapter(cache)
	module := agentapp.NewService(&legacyAdapterRepo{runtimes: map[uint]agentapp.AgentRuntimeDTO{
		1: {
			UserID:             1,
			Username:           "agent1",
			Name:               "Agent One",
			Department:         "support",
			Status:             string(agentdomain.PresenceStatusOnline),
			MaxChatConcurrency: 3,
			CurrentChatLoad:    1,
		},
	}}, &legacyAdapterRegistry{
		items: map[uint]agentapp.AgentRuntimeDTO{
			1: {
				UserID:             1,
				Username:           "agent1",
				Name:               "Agent One",
				Department:         "support",
				Status:             string(agentdomain.PresenceStatusOnline),
				MaxChatConcurrency: 3,
				CurrentChatLoad:    1,
				LastActivity:       time.Now(),
			},
		},
	})

	adapter.Sync(context.Background(), module)

	agent, ok := adapter.GetOnlineAgent(1)
	if !ok {
		t.Fatal("expected online agent in legacy cache")
	}
	if agent.UserID != 1 || agent.CurrentLoad != 1 || agent.MaxConcurrent != 3 {
		t.Fatalf("unexpected cached agent: %+v", agent)
	}
}
