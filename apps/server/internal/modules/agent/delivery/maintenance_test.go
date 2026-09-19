package delivery

// 本文件覆盖 RuntimeMaintenance 的超时清理与 ticker 循环；
// repo/registry 桩同时满足 application.Repository 与 RuntimeRegistry 接口。

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

type maintenanceTestRepo struct {
	profile       *agentdomain.AgentProfile
	model         *models.Agent
	statusUpdates []string
}

func (r *maintenanceTestRepo) CreateAgent(ctx context.Context, userID uint, department string, skills []string, maxChatConcurrency int) (*agentdomain.AgentProfile, error) {
	return nil, nil
}

func (r *maintenanceTestRepo) GetAgentByUserID(ctx context.Context, userID uint) (*agentdomain.AgentProfile, *models.Agent, error) {
	if r.profile == nil || r.model == nil || r.model.UserID != userID {
		return nil, nil, gorm.ErrRecordNotFound
	}
	return r.profile, r.model, nil
}

func (r *maintenanceTestRepo) ListAgents(ctx context.Context, limit int) ([]models.Agent, error) {
	return nil, nil
}

func (r *maintenanceTestRepo) GetAgentRuntimeByUserID(ctx context.Context, userID uint) (*agentapp.AgentRuntimeDTO, error) {
	if r.profile == nil || r.model == nil || r.model.UserID != userID {
		return nil, gorm.ErrRecordNotFound
	}
	return &agentapp.AgentRuntimeDTO{
		UserID:             r.profile.UserID,
		Status:             r.model.Status,
		MaxChatConcurrency: r.profile.MaxChatConcurrency,
	}, nil
}

func (r *maintenanceTestRepo) ListActiveAgentRuntimes(ctx context.Context) ([]agentapp.AgentRuntimeDTO, error) {
	if r.profile == nil || r.model == nil {
		return nil, nil
	}
	return []agentapp.AgentRuntimeDTO{{
		UserID:             r.profile.UserID,
		Status:             string(agentdomain.PresenceStatusOnline),
		MaxChatConcurrency: r.profile.MaxChatConcurrency,
	}}, nil
}

func (r *maintenanceTestRepo) UpdatePresenceStatus(ctx context.Context, userID uint, status agentdomain.PresenceStatus) error {
	r.statusUpdates = append(r.statusUpdates, string(status))
	return nil
}

func (r *maintenanceTestRepo) UpdateChatLoad(ctx context.Context, userID uint, currentLoad int) error {
	return nil
}

func (r *maintenanceTestRepo) GetSessionByID(ctx context.Context, sessionID string) (*models.Session, error) {
	return nil, nil
}

func (r *maintenanceTestRepo) AssignSession(ctx context.Context, sessionID string, agentUserID uint) error {
	return nil
}

func (r *maintenanceTestRepo) ReleaseSession(ctx context.Context, sessionID string, agentUserID uint) error {
	return nil
}

func (r *maintenanceTestRepo) GetStats(ctx context.Context, agentUserID *uint) (*agentapp.AgentStatsDTO, error) {
	return &agentapp.AgentStatsDTO{}, nil
}

func (r *maintenanceTestRepo) RevokeUserTokens(ctx context.Context, userID uint, revokeAt time.Time) (int, error) {
	return 0, nil
}

// ---- 选坐席（selection）桩 ----

func (r *maintenanceTestRepo) GetLastAgentForCustomer(ctx context.Context, customerUserID uint, since time.Time) (*uint, error) {
	return nil, nil
}

func (r *maintenanceTestRepo) GetAgentGroup(ctx context.Context, id uint) (*models.AgentGroup, error) {
	return nil, gorm.ErrRecordNotFound
}

func (r *maintenanceTestRepo) ListAgentGroups(ctx context.Context) ([]models.AgentGroup, error) {
	return nil, nil
}

func (r *maintenanceTestRepo) CreateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return nil
}

func (r *maintenanceTestRepo) UpdateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return nil
}

func (r *maintenanceTestRepo) DeleteAgentGroup(ctx context.Context, id uint) error {
	return nil
}

func (r *maintenanceTestRepo) ReplaceGroupMembers(ctx context.Context, groupID uint, agentUserIDs []uint) error {
	return nil
}

func (r *maintenanceTestRepo) ListGroupMemberIDs(ctx context.Context, groupID uint) ([]uint, error) {
	return nil, nil
}

func (r *maintenanceTestRepo) ListEnabledGroupMemberIDs(ctx context.Context, groupID uint) ([]uint, error) {
	return nil, nil
}

func (r *maintenanceTestRepo) UpdateLastActivity(ctx context.Context, userID uint) error {
	return nil
}

func (r *maintenanceTestRepo) SetConnectedTime(ctx context.Context, userID uint) error {
	return nil
}

func (r *maintenanceTestRepo) ClearConnectedTime(ctx context.Context, userID uint) error {
	return nil
}

type maintenanceTestRegistry struct {
	items map[uint]agentapp.AgentRuntimeDTO
}

func (r *maintenanceTestRegistry) GoOnline(profile agentdomain.AgentProfile) (agentapp.AgentRuntimeDTO, error) {
	item := agentapp.AgentRuntimeDTO{
		UserID:             profile.UserID,
		Status:             string(agentdomain.PresenceStatusOnline),
		MaxChatConcurrency: profile.MaxChatConcurrency,
		LastActivity:       time.Now(),
	}
	r.items[profile.UserID] = item
	return item, nil
}

func (r *maintenanceTestRegistry) GoOffline(userID uint) {
	delete(r.items, userID)
}

func (r *maintenanceTestRegistry) UpdateStatus(userID uint, status agentdomain.PresenceStatus) {
	item := r.items[userID]
	item.Status = string(status)
	r.items[userID] = item
}

func (r *maintenanceTestRegistry) AssignSession(userID uint, session *models.Session) (agentapp.AgentRuntimeDTO, error) {
	return agentapp.AgentRuntimeDTO{}, nil
}

func (r *maintenanceTestRegistry) ReleaseSession(userID uint, sessionID string) (agentapp.AgentRuntimeDTO, bool) {
	return agentapp.AgentRuntimeDTO{}, false
}

func (r *maintenanceTestRegistry) ApplyTransfer(sessionID string, fromAgentID *uint, toAgentID uint) {}

func (r *maintenanceTestRegistry) Get(userID uint) (agentapp.AgentRuntimeDTO, bool) {
	item, ok := r.items[userID]
	return item, ok
}

func (r *maintenanceTestRegistry) List() []agentapp.AgentRuntimeDTO {
	out := make([]agentapp.AgentRuntimeDTO, 0, len(r.items))
	for _, item := range r.items {
		out = append(out, item)
	}
	return out
}

func TestRuntimeMaintenance_CleanupInactiveAgents(t *testing.T) {
	repo := &maintenanceTestRepo{
		profile: &agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 3},
		model:   &models.Agent{UserID: 7},
	}
	registry := &maintenanceTestRegistry{
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

	maintenance := NewRuntimeMaintenance(logrus.New(), module)

	maintenance.CleanupInactiveAgents(context.Background(), 5*time.Minute)

	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0] != string(agentdomain.PresenceStatusAway) {
		t.Fatalf("expected away status update, got %v", repo.statusUpdates)
	}
	if runtime, ok := registry.Get(7); !ok || runtime.Status != string(agentdomain.PresenceStatusAway) {
		t.Fatalf("expected runtime status away, got %+v ok=%v", runtime, ok)
	}
}

func TestRuntimeMaintenance_CleanupSkipsZeroActivity(t *testing.T) {
	repo := &maintenanceTestRepo{
		profile: &agentdomain.AgentProfile{UserID: 9, MaxChatConcurrency: 2},
		model:   &models.Agent{UserID: 9},
	}
	registry := &maintenanceTestRegistry{
		items: map[uint]agentapp.AgentRuntimeDTO{
			9: {UserID: 9, Status: "online"}, // zero LastActivity
		},
	}
	module := agentapp.NewService(repo, registry)
	m := NewRuntimeMaintenance(logrus.New(), module)
	m.CleanupInactiveAgents(context.Background(), time.Minute)
	if len(repo.statusUpdates) != 0 {
		t.Fatalf("expected no updates for zero activity, got %v", repo.statusUpdates)
	}
}

// TestRuntimeMaintenance_TickerCleansUp：Start 的 ticker 循环按注入的毫秒级
// interval 周期执行 cleanupInactiveAgents，超时坐席被标记 away。
func TestRuntimeMaintenance_TickerCleansUp(t *testing.T) {
	repo := &maintenanceTestRepo{
		profile: &agentdomain.AgentProfile{UserID: 11, MaxChatConcurrency: 1},
		model:   &models.Agent{UserID: 11},
	}
	registry := &maintenanceTestRegistry{items: map[uint]agentapp.AgentRuntimeDTO{
		11: {UserID: 11, LastActivity: time.Now().Add(-10 * time.Minute)},
	}}
	module := agentapp.NewService(repo, registry)
	maintenance := NewRuntimeMaintenance(logrus.New(), module)
	maintenance.interval = 2 * time.Millisecond

	go maintenance.Start()

	// 等 ≥3 轮 tick，覆盖 ticker 循环的回边。
	deadline := time.Now().Add(10 * time.Second)
	for {
		if len(repo.statusUpdates) >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("maintenance ticker ran only %d times", len(repo.statusUpdates))
		}
		time.Sleep(5 * time.Millisecond)
	}
}
