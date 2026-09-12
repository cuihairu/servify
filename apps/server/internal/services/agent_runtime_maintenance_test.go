package services

import (
	"context"
	"testing"
	"time"

	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdomain "servify/apps/server/internal/modules/agent/domain"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
)

type maintenanceRepo struct {
	profile       *agentdomain.AgentProfile
	model         *models.Agent
	statusUpdates []string
}

func (r *maintenanceRepo) CreateAgent(ctx context.Context, userID uint, department string, skills []string, maxChatConcurrency int) (*agentdomain.AgentProfile, error) {
	return nil, nil
}

func (r *maintenanceRepo) GetAgentByUserID(ctx context.Context, userID uint) (*agentdomain.AgentProfile, *models.Agent, error) {
	if r.profile == nil || r.model == nil || r.model.UserID != userID {
		return nil, nil, gorm.ErrRecordNotFound
	}
	return r.profile, r.model, nil
}

func (r *maintenanceRepo) ListAgents(ctx context.Context, limit int) ([]models.Agent, error) {
	return nil, nil
}

func (r *maintenanceRepo) GetAgentRuntimeByUserID(ctx context.Context, userID uint) (*agentapp.AgentRuntimeDTO, error) {
	if r.profile == nil || r.model == nil || r.model.UserID != userID {
		return nil, gorm.ErrRecordNotFound
	}
	return &agentapp.AgentRuntimeDTO{
		UserID:             r.profile.UserID,
		Status:             r.model.Status,
		MaxChatConcurrency: r.profile.MaxChatConcurrency,
	}, nil
}

func (r *maintenanceRepo) ListActiveAgentRuntimes(ctx context.Context) ([]agentapp.AgentRuntimeDTO, error) {
	if r.profile == nil || r.model == nil {
		return nil, nil
	}
	return []agentapp.AgentRuntimeDTO{{
		UserID:             r.profile.UserID,
		Status:             string(agentdomain.PresenceStatusOnline),
		MaxChatConcurrency: r.profile.MaxChatConcurrency,
	}}, nil
}

func (r *maintenanceRepo) UpdatePresenceStatus(ctx context.Context, userID uint, status agentdomain.PresenceStatus) error {
	r.statusUpdates = append(r.statusUpdates, string(status))
	return nil
}

func (r *maintenanceRepo) UpdateChatLoad(ctx context.Context, userID uint, currentLoad int) error {
	return nil
}

func (r *maintenanceRepo) GetSessionByID(ctx context.Context, sessionID string) (*models.Session, error) {
	return nil, nil
}

func (r *maintenanceRepo) AssignSession(ctx context.Context, sessionID string, agentUserID uint) error {
	return nil
}

func (r *maintenanceRepo) ReleaseSession(ctx context.Context, sessionID string, agentUserID uint) error {
	return nil
}

func (r *maintenanceRepo) GetStats(ctx context.Context, agentUserID *uint) (*agentapp.AgentStatsDTO, error) {
	return &agentapp.AgentStatsDTO{}, nil
}

func (r *maintenanceRepo) RevokeUserTokens(ctx context.Context, userID uint, revokeAt time.Time) (int, error) {
	return 0, nil
}

// ---- 选坐席（selection）桩 ----

func (r *maintenanceRepo) GetLastAgentForCustomer(ctx context.Context, customerUserID uint, since time.Time) (*uint, error) {
	return nil, nil
}

func (r *maintenanceRepo) GetAgentGroup(ctx context.Context, id uint) (*models.AgentGroup, error) {
	return nil, gorm.ErrRecordNotFound
}

func (r *maintenanceRepo) ListAgentGroups(ctx context.Context) ([]models.AgentGroup, error) {
	return nil, nil
}

func (r *maintenanceRepo) CreateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return nil
}

func (r *maintenanceRepo) UpdateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return nil
}

func (r *maintenanceRepo) DeleteAgentGroup(ctx context.Context, id uint) error {
	return nil
}

func (r *maintenanceRepo) ReplaceGroupMembers(ctx context.Context, groupID uint, agentUserIDs []uint) error {
	return nil
}

func (r *maintenanceRepo) ListEnabledGroupMemberIDs(ctx context.Context, groupID uint) ([]uint, error) {
	return nil, nil
}

func (r *maintenanceRepo) UpdateLastActivity(ctx context.Context, userID uint) error {
	return nil
}

func (r *maintenanceRepo) SetConnectedTime(ctx context.Context, userID uint) error {
	return nil
}

func (r *maintenanceRepo) ClearConnectedTime(ctx context.Context, userID uint) error {
	return nil
}

type maintenanceRegistry struct {
	items map[uint]agentapp.AgentRuntimeDTO
}

func (r *maintenanceRegistry) GoOnline(profile agentdomain.AgentProfile) (agentapp.AgentRuntimeDTO, error) {
	item := agentapp.AgentRuntimeDTO{
		UserID:             profile.UserID,
		Status:             string(agentdomain.PresenceStatusOnline),
		MaxChatConcurrency: profile.MaxChatConcurrency,
		LastActivity:       time.Now(),
	}
	r.items[profile.UserID] = item
	return item, nil
}

func (r *maintenanceRegistry) GoOffline(userID uint) {
	delete(r.items, userID)
}

func (r *maintenanceRegistry) UpdateStatus(userID uint, status agentdomain.PresenceStatus) {
	item := r.items[userID]
	item.Status = string(status)
	r.items[userID] = item
}

func (r *maintenanceRegistry) AssignSession(userID uint, session *models.Session) (agentapp.AgentRuntimeDTO, error) {
	return agentapp.AgentRuntimeDTO{}, nil
}

func (r *maintenanceRegistry) ReleaseSession(userID uint, sessionID string) (agentapp.AgentRuntimeDTO, bool) {
	return agentapp.AgentRuntimeDTO{}, false
}

func (r *maintenanceRegistry) ApplyTransfer(sessionID string, fromAgentID *uint, toAgentID uint) {}

func (r *maintenanceRegistry) Get(userID uint) (agentapp.AgentRuntimeDTO, bool) {
	item, ok := r.items[userID]
	return item, ok
}

func (r *maintenanceRegistry) List() []agentapp.AgentRuntimeDTO {
	out := make([]agentapp.AgentRuntimeDTO, 0, len(r.items))
	for _, item := range r.items {
		out = append(out, item)
	}
	return out
}

func TestAgentRuntimeMaintenance_CleanupInactiveAgents(t *testing.T) {
	repo := &maintenanceRepo{
		profile: &agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 3},
		model:   &models.Agent{UserID: 7},
	}
	registry := &maintenanceRegistry{
		items: map[uint]agentapp.AgentRuntimeDTO{
			7: {
				UserID:             7,
				Status:             string(agentdomain.PresenceStatusOnline),
				MaxChatConcurrency: 3,
				LastActivity:       time.Now().Add(-10 * time.Minute),
			},
		},
	}
	module := agentapp.NewService(repo, registry)

	maintenance := newAgentRuntimeMaintenance(logrus.New(), module)

	maintenance.cleanupInactiveAgents(context.Background(), 5*time.Minute)

	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0] != string(agentdomain.PresenceStatusAway) {
		t.Fatalf("expected away status update, got %v", repo.statusUpdates)
	}
	if runtime, ok := registry.Get(7); !ok || runtime.Status != string(agentdomain.PresenceStatusAway) {
		t.Fatalf("expected runtime status away, got %+v ok=%v", runtime, ok)
	}
}
