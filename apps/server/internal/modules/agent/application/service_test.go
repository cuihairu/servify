package application

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	agentdomain "servify/apps/server/internal/modules/agent/domain"

	"gorm.io/gorm"
)

type stubRepo struct {
	profile         *agentdomain.AgentProfile
	model           *models.Agent
	stats           *AgentStatsDTO
	runtimes        []AgentRuntimeDTO
	statusUpdates   []string
	lastChatLoad    int
	sessionAssigned string
	tokenVersion    int
	tokenRevokedAt  time.Time

	lastAgentForCustomer *uint
	group                *models.AgentGroup
	groupMembers         []uint
}

func (s *stubRepo) CreateAgent(ctx context.Context, userID uint, department string, skills []string, maxChatConcurrency int) (*agentdomain.AgentProfile, error) {
	s.profile = &agentdomain.AgentProfile{
		UserID:              userID,
		Department:          department,
		Skills:              append([]string(nil), skills...),
		MaxChatConcurrency:  maxChatConcurrency,
		MaxVoiceConcurrency: 1,
		Rating:              5,
	}
	s.model = &models.Agent{UserID: userID, Department: department, MaxConcurrent: maxChatConcurrency}
	return s.profile, nil
}

func (s *stubRepo) GetAgentByUserID(ctx context.Context, userID uint) (*agentdomain.AgentProfile, *models.Agent, error) {
	if s.profile == nil || s.model == nil || s.model.UserID != userID {
		return nil, nil, gorm.ErrRecordNotFound
	}
	return s.profile, s.model, nil
}

func (s *stubRepo) ListAgents(ctx context.Context, limit int) ([]models.Agent, error) {
	if s.model == nil {
		return nil, nil
	}
	return []models.Agent{*s.model}, nil
}

func (s *stubRepo) GetAgentRuntimeByUserID(ctx context.Context, userID uint) (*AgentRuntimeDTO, error) {
	for _, runtime := range s.runtimes {
		if runtime.UserID == userID {
			copy := runtime
			return &copy, nil
		}
	}
	if s.profile == nil || s.model == nil || s.model.UserID != userID {
		return nil, gorm.ErrRecordNotFound
	}
	return &AgentRuntimeDTO{
		UserID:             s.profile.UserID,
		Username:           s.profile.Username,
		Name:               s.profile.Name,
		Department:         s.profile.Department,
		Skills:             append([]string(nil), s.profile.Skills...),
		Status:             string(agentdomain.PresenceStatusOnline),
		MaxChatConcurrency: s.profile.MaxChatConcurrency,
		CurrentChatLoad:    s.lastChatLoad,
		Rating:             s.profile.Rating,
		AvgResponseTime:    s.profile.AvgResponseTime,
	}, nil
}

func (s *stubRepo) ListActiveAgentRuntimes(ctx context.Context) ([]AgentRuntimeDTO, error) {
	if len(s.runtimes) > 0 {
		out := make([]AgentRuntimeDTO, len(s.runtimes))
		copy(out, s.runtimes)
		return out, nil
	}
	if s.profile == nil {
		return nil, nil
	}
	return []AgentRuntimeDTO{{
		UserID:             s.profile.UserID,
		Username:           s.profile.Username,
		Name:               s.profile.Name,
		Department:         s.profile.Department,
		Skills:             append([]string(nil), s.profile.Skills...),
		Status:             string(agentdomain.PresenceStatusOnline),
		MaxChatConcurrency: s.profile.MaxChatConcurrency,
		CurrentChatLoad:    s.lastChatLoad,
		Rating:             s.profile.Rating,
		AvgResponseTime:    s.profile.AvgResponseTime,
	}}, nil
}

func (s *stubRepo) UpdatePresenceStatus(ctx context.Context, userID uint, status agentdomain.PresenceStatus) error {
	s.statusUpdates = append(s.statusUpdates, string(status))
	return nil
}

func (s *stubRepo) UpdateChatLoad(ctx context.Context, userID uint, currentLoad int) error {
	s.lastChatLoad = currentLoad
	for i := range s.runtimes {
		if s.runtimes[i].UserID == userID {
			s.runtimes[i].CurrentChatLoad = currentLoad
		}
	}
	return nil
}

func (s *stubRepo) GetSessionByID(ctx context.Context, sessionID string) (*models.Session, error) {
	return &models.Session{ID: sessionID}, nil
}

func (s *stubRepo) AssignSession(ctx context.Context, sessionID string, agentUserID uint) error {
	s.sessionAssigned = sessionID
	return nil
}

func (s *stubRepo) ReleaseSession(ctx context.Context, sessionID string, agentUserID uint) error {
	return nil
}

func (s *stubRepo) GetStats(ctx context.Context, agentUserID *uint) (*AgentStatsDTO, error) {
	if s.stats == nil {
		s.stats = &AgentStatsDTO{Total: 1}
	}
	return s.stats, nil
}

func (s *stubRepo) RevokeUserTokens(ctx context.Context, userID uint, revokeAt time.Time) (int, error) {
	s.tokenRevokedAt = revokeAt
	s.tokenVersion++
	return s.tokenVersion, nil
}

// New persisted runtime metadata methods
func (s *stubRepo) UpdateLastActivity(ctx context.Context, userID uint) error {
	return nil
}

func (s *stubRepo) SetConnectedTime(ctx context.Context, userID uint) error {
	return nil
}

func (s *stubRepo) ClearConnectedTime(ctx context.Context, userID uint) error {
	return nil
}

// ---- 选坐席（selection）桩 ----

func (s *stubRepo) GetLastAgentForCustomer(ctx context.Context, customerUserID uint, since time.Time) (*uint, error) {
	if s.lastAgentForCustomer == nil {
		return nil, nil
	}
	id := *s.lastAgentForCustomer
	return &id, nil
}

func (s *stubRepo) GetAgentGroup(ctx context.Context, id uint) (*models.AgentGroup, error) {
	if s.group == nil || s.group.ID != id {
		return nil, gorm.ErrRecordNotFound
	}
	return s.group, nil
}

func (s *stubRepo) ListAgentGroups(ctx context.Context) ([]models.AgentGroup, error) {
	if s.group == nil {
		return nil, nil
	}
	return []models.AgentGroup{*s.group}, nil
}

func (s *stubRepo) CreateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return nil
}

func (s *stubRepo) UpdateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return nil
}

func (s *stubRepo) DeleteAgentGroup(ctx context.Context, id uint) error {
	return nil
}

func (s *stubRepo) ReplaceGroupMembers(ctx context.Context, groupID uint, agentUserIDs []uint) error {
	return nil
}

func (s *stubRepo) ListGroupMemberIDs(ctx context.Context, groupID uint) ([]uint, error) {
	return s.groupMembers, nil
}

func (s *stubRepo) ListEnabledGroupMemberIDs(ctx context.Context, groupID uint) ([]uint, error) {
	return s.groupMembers, nil
}

func TestServiceGoOnlineAndAssignSession(t *testing.T) {
	repo := &stubRepo{
		profile: &agentdomain.AgentProfile{
			UserID:              7,
			Username:            "agent7",
			Name:                "Agent Seven",
			Skills:              []string{"billing", "chat"},
			MaxChatConcurrency:  2,
			MaxVoiceConcurrency: 1,
			Rating:              4.8,
		},
		model: &models.Agent{UserID: 7, MaxConcurrent: 2},
		runtimes: []AgentRuntimeDTO{{
			UserID:             7,
			Username:           "agent7",
			Name:               "Agent Seven",
			Department:         "",
			Skills:             []string{"billing", "chat"},
			Status:             string(agentdomain.PresenceStatusOnline),
			MaxChatConcurrency: 2,
			CurrentChatLoad:    0,
			Rating:             4.8,
		}},
	}
	registry := newStubRegistry()
	svc := NewService(repo, registry)

	if err := svc.GoOnline(context.Background(), 7); err != nil {
		t.Fatalf("GoOnline() error = %v", err)
	}
	if err := svc.AssignSession(context.Background(), "sess-1", 7); err != nil {
		t.Fatalf("AssignSession() error = %v", err)
	}
	got, err := svc.FindAvailableAgent(context.Background(), []string{"billing"}, "high")
	if err != nil {
		t.Fatalf("FindAvailableAgent() error = %v", err)
	}
	if got.UserID != 7 {
		t.Fatalf("FindAvailableAgent() user_id = %d, want 7", got.UserID)
	}
	if repo.lastChatLoad != 1 {
		t.Fatalf("UpdateChatLoad() = %d, want 1", repo.lastChatLoad)
	}
}

func TestServiceFindAvailableAgent_UsesDatabaseRuntimeWithoutRegistryState(t *testing.T) {
	repo := &stubRepo{
		runtimes: []AgentRuntimeDTO{
			{
				UserID:             9,
				Username:           "db-agent",
				Name:               "DB Agent",
				Skills:             []string{"billing"},
				Status:             string(agentdomain.PresenceStatusOnline),
				MaxChatConcurrency: 2,
				CurrentChatLoad:    1,
				Rating:             4.5,
			},
			{
				UserID:             10,
				Username:           "busy-agent",
				Name:               "Busy Agent",
				Skills:             []string{"billing"},
				Status:             string(agentdomain.PresenceStatusBusy),
				MaxChatConcurrency: 1,
				CurrentChatLoad:    1,
				Rating:             5.0,
			},
		},
	}
	svc := NewService(repo, newStubRegistry())

	got, err := svc.FindAvailableAgent(context.Background(), []string{"billing"}, "high")
	if err != nil {
		t.Fatalf("FindAvailableAgent() error = %v", err)
	}
	if got.UserID != 9 {
		t.Fatalf("FindAvailableAgent() user_id = %d, want 9", got.UserID)
	}
}

func TestServiceRevokeUserTokens(t *testing.T) {
	repo := &stubRepo{tokenVersion: 1}
	svc := NewService(repo, newStubRegistry())

	version, err := svc.RevokeUserTokens(context.Background(), 7, time.Time{})
	if err != nil {
		t.Fatalf("RevokeUserTokens() error = %v", err)
	}
	if version != 2 {
		t.Fatalf("RevokeUserTokens() version = %d, want 2", version)
	}
	if repo.tokenRevokedAt.IsZero() {
		t.Fatal("expected token revoke time to be set")
	}
}

type stubRegistry struct {
	items map[uint]AgentRuntimeDTO
}

func newStubRegistry() *stubRegistry {
	return &stubRegistry{items: make(map[uint]AgentRuntimeDTO)}
}

func (s *stubRegistry) GoOnline(profile agentdomain.AgentProfile) (AgentRuntimeDTO, error) {
	item := AgentRuntimeDTO{
		UserID:              profile.UserID,
		Username:            profile.Username,
		Name:                profile.Name,
		Department:          profile.Department,
		Skills:              append([]string(nil), profile.Skills...),
		Status:              "online",
		MaxChatConcurrency:  profile.MaxChatConcurrency,
		MaxVoiceConcurrency: profile.MaxVoiceConcurrency,
		CurrentChatLoad:     profile.CurrentChatLoad,
		CurrentVoiceLoad:    profile.CurrentVoiceLoad,
		Rating:              profile.Rating,
		AvgResponseTime:     profile.AvgResponseTime,
	}
	s.items[profile.UserID] = item
	return item, nil
}

func (s *stubRegistry) GoOffline(userID uint) {
	delete(s.items, userID)
}

func (s *stubRegistry) UpdateStatus(userID uint, status agentdomain.PresenceStatus) {
	item := s.items[userID]
	item.Status = string(status)
	s.items[userID] = item
}

func (s *stubRegistry) AssignSession(userID uint, session *models.Session) (AgentRuntimeDTO, error) {
	item := s.items[userID]
	item.CurrentChatLoad++
	s.items[userID] = item
	return item, nil
}

func (s *stubRegistry) ReleaseSession(userID uint, sessionID string) (AgentRuntimeDTO, bool) {
	item, ok := s.items[userID]
	if !ok {
		return AgentRuntimeDTO{}, false
	}
	if item.CurrentChatLoad > 0 {
		item.CurrentChatLoad--
	}
	s.items[userID] = item
	return item, true
}

func (s *stubRegistry) ApplyTransfer(sessionID string, fromAgentID *uint, toAgentID uint) {}

func (s *stubRegistry) Get(userID uint) (AgentRuntimeDTO, bool) {
	item, ok := s.items[userID]
	return item, ok
}

func (s *stubRegistry) List() []AgentRuntimeDTO {
	out := make([]AgentRuntimeDTO, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, item)
	}
	return out
}
