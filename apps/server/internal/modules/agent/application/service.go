package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"servify/apps/server/internal/models"
	agentdomain "servify/apps/server/internal/modules/agent/domain"
)

type Service struct {
	repo     Repository
	registry RuntimeRegistry
}

func NewService(repo Repository, registry RuntimeRegistry) *Service {
	return &Service{repo: repo, registry: registry}
}

func (s *Service) CreateAgent(ctx context.Context, cmd CreateAgentCommand) (*models.Agent, error) {
	if cmd.UserID == 0 {
		return nil, fmt.Errorf("user_id required")
	}
	profile, model, err := s.repo.GetAgentByUserID(ctx, cmd.UserID)
	if err == nil && profile != nil && model != nil {
		return nil, fmt.Errorf("user is already an agent")
	}
	_, err = s.repo.CreateAgent(ctx, cmd.UserID, cmd.Department, sanitizeSkills(cmd.Skills), normalizeChatConcurrency(cmd.MaxChatConcurrency))
	if err != nil {
		return nil, err
	}
	_, model, err = s.repo.GetAgentByUserID(ctx, cmd.UserID)
	return model, err
}

func (s *Service) GetAgentByUserID(ctx context.Context, userID uint) (*models.Agent, error) {
	_, model, err := s.repo.GetAgentByUserID(ctx, userID)
	return model, err
}

func (s *Service) ListAgents(ctx context.Context, limit int) ([]models.Agent, error) {
	return s.repo.ListAgents(ctx, limit)
}

func (s *Service) GoOnline(ctx context.Context, userID uint) error {
	profile, _, err := s.repo.GetAgentByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.repo.UpdatePresenceStatus(ctx, userID, agentdomain.PresenceStatusOnline); err != nil {
		return err
	}
	// Persist connection time and last activity
	_ = s.repo.SetConnectedTime(ctx, userID)
	_ = s.repo.UpdateLastActivity(ctx, userID)
	// Still update in-memory registry for transient metadata (session cache)
	_, err = s.registry.GoOnline(*profile)
	return err
}

func (s *Service) GoOffline(ctx context.Context, userID uint) error {
	if err := s.repo.UpdatePresenceStatus(ctx, userID, agentdomain.PresenceStatusOffline); err != nil {
		return err
	}
	// Clear persisted connection time
	_ = s.repo.ClearConnectedTime(ctx, userID)
	// Clear in-memory registry
	s.registry.GoOffline(userID)
	return nil
}

func (s *Service) UpdateStatus(ctx context.Context, userID uint, status string) error {
	next, err := parsePresenceStatus(status)
	if err != nil {
		return err
	}
	if err := s.repo.UpdatePresenceStatus(ctx, userID, next); err != nil {
		return err
	}
	// Persist last activity
	_ = s.repo.UpdateLastActivity(ctx, userID)
	s.registry.UpdateStatus(userID, next)
	return nil
}

func (s *Service) MarkBusy(ctx context.Context, userID uint) error {
	return s.UpdateStatus(ctx, userID, string(agentdomain.PresenceStatusBusy))
}

func (s *Service) MarkAway(ctx context.Context, userID uint) error {
	return s.UpdateStatus(ctx, userID, string(agentdomain.PresenceStatusAway))
}

func (s *Service) AssignSession(ctx context.Context, sessionID string, agentUserID uint) error {
	runtime, err := s.repo.GetAgentRuntimeByUserID(ctx, agentUserID)
	if err != nil {
		if _, _, lookupErr := s.repo.GetAgentByUserID(ctx, agentUserID); lookupErr != nil {
			return lookupErr
		}
		return fmt.Errorf("agent %d is not online", agentUserID)
	}
	if runtime.CurrentChatLoad >= runtime.MaxChatConcurrency {
		return fmt.Errorf("agent %d is at maximum capacity", agentUserID)
	}
	session, err := s.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		return err
	}
	updatedRuntime, err := s.registry.AssignSession(agentUserID, session)
	if err != nil {
		updatedRuntime = *runtime
		updatedRuntime.CurrentChatLoad++
	}
	if err := s.repo.AssignSession(ctx, sessionID, agentUserID); err != nil {
		return err
	}
	return s.repo.UpdateChatLoad(ctx, agentUserID, updatedRuntime.CurrentChatLoad)
}

func (s *Service) ReleaseSession(ctx context.Context, sessionID string, agentUserID uint) error {
	if err := s.repo.ReleaseSession(ctx, sessionID, agentUserID); err != nil {
		return err
	}
	runtime, ok := s.registry.ReleaseSession(agentUserID, sessionID)
	if ok {
		return s.repo.UpdateChatLoad(ctx, agentUserID, runtime.CurrentChatLoad)
	}
	currentRuntime, err := s.repo.GetAgentRuntimeByUserID(ctx, agentUserID)
	if err != nil {
		return nil
	}
	nextLoad := currentRuntime.CurrentChatLoad - 1
	if nextLoad < 0 {
		nextLoad = 0
	}
	return s.repo.UpdateChatLoad(ctx, agentUserID, nextLoad)
}

func (s *Service) FindAvailableAgent(ctx context.Context, skills []string, priority string) (*AgentRuntimeDTO, error) {
	result, err := s.SelectAgent(ctx, SelectionRequest{Skills: skills, Priority: priority})
	if err != nil {
		return nil, err
	}
	return result.Agent, nil
}

func (s *Service) GetOnlineAgents(ctx context.Context) []AgentRuntimeDTO {
	runtimes, err := s.repo.ListActiveAgentRuntimes(ctx)
	if err != nil {
		return nil
	}
	return s.mergeRuntimeMetadata(runtimes)
}

func (s *Service) GetOnlineAgent(ctx context.Context, userID uint) (*AgentRuntimeDTO, error) {
	runtime, err := s.repo.GetAgentRuntimeByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	// mergeRuntimeMetadata 对非空输入恒返回等长切片（registry 为 nil 时原样
	// 返回，否则仅就地下标覆写），len==1 输入后恒 len==1，len==0 守卫不可达，
	// 已删除。
	return &s.mergeRuntimeMetadata([]AgentRuntimeDTO{*runtime})[0], nil
}

func (s *Service) GetStats(ctx context.Context, agentUserID *uint) (*AgentStatsDTO, error) {
	stats, err := s.repo.GetStats(ctx, agentUserID)
	if err != nil {
		return nil, err
	}
	runtimes, err := s.repo.ListActiveAgentRuntimes(ctx)
	if err != nil {
		return nil, err
	}
	for _, candidate := range runtimes {
		stats.Online++
		if candidate.Status == string(agentdomain.PresenceStatusBusy) || candidate.CurrentChatLoad >= candidate.MaxChatConcurrency {
			stats.Busy++
		}
	}
	return stats, nil
}

func (s *Service) RevokeUserTokens(ctx context.Context, userID uint, revokeAt time.Time) (int, error) {
	if userID == 0 {
		return 0, fmt.Errorf("user_id required")
	}
	if revokeAt.IsZero() {
		revokeAt = time.Now().UTC()
	}
	return s.repo.RevokeUserTokens(ctx, userID, revokeAt)
}

func (s *Service) ApplySessionTransfer(ctx context.Context, sessionID string, fromAgentID *uint, toAgentID uint) error {
	s.registry.ApplyTransfer(sessionID, fromAgentID, toAgentID)
	if runtime, ok := s.registry.Get(toAgentID); ok {
		if err := s.repo.UpdateChatLoad(ctx, toAgentID, runtime.CurrentChatLoad); err != nil {
			return err
		}
	}
	if fromAgentID != nil {
		if runtime, ok := s.registry.Get(*fromAgentID); ok {
			if err := s.repo.UpdateChatLoad(ctx, *fromAgentID, runtime.CurrentChatLoad); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) mergeRuntimeMetadata(runtimes []AgentRuntimeDTO) []AgentRuntimeDTO {
	if len(runtimes) == 0 || s.registry == nil {
		return runtimes
	}
	// In-memory registry provides transient metadata (e.g., cached sessions).
	// LastActivity/ConnectedAt are now persisted in the database, so they will
	// be available even after restart. The in-memory values only override if
	// more recent (e.g., during active session without DB flush).
	metadata := make(map[uint]AgentRuntimeDTO, len(runtimes))
	for _, runtime := range s.registry.List() {
		metadata[runtime.UserID] = runtime
	}
	for i := range runtimes {
		item, ok := metadata[runtimes[i].UserID]
		if !ok {
			continue
		}
		// Only override if in-memory value is more recent than persisted value
		if item.LastActivity.IsZero() || item.LastActivity.After(runtimes[i].LastActivity) {
			runtimes[i].LastActivity = item.LastActivity
		}
		if item.ConnectedAt.IsZero() || (!runtimes[i].ConnectedAt.IsZero() && item.ConnectedAt.Before(runtimes[i].ConnectedAt)) {
			runtimes[i].ConnectedAt = item.ConnectedAt
		}
	}
	return runtimes
}

func sanitizeSkills(skills []string) []string {
	out := make([]string, 0, len(skills))
	for _, skill := range skills {
		if skill == "" {
			continue
		}
		if !slices.Contains(out, skill) {
			out = append(out, skill)
		}
	}
	return out
}

func normalizeChatConcurrency(v int) int {
	if v <= 0 {
		return 5
	}
	return v
}

func parsePresenceStatus(value string) (agentdomain.PresenceStatus, error) {
	switch agentdomain.PresenceStatus(value) {
	case agentdomain.PresenceStatusOnline, agentdomain.PresenceStatusBusy, agentdomain.PresenceStatusAway, agentdomain.PresenceStatusOffline:
		return agentdomain.PresenceStatus(value), nil
	default:
		return "", fmt.Errorf("invalid status: %s", value)
	}
}

func calculateScore(agent AgentRuntimeDTO, requiredSkills []string, priority string) float64 {
	score := agent.Rating
	if agent.MaxChatConcurrency > 0 {
		loadRatio := float64(agent.CurrentChatLoad) / float64(agent.MaxChatConcurrency)
		score += (1 - loadRatio) * 3
	}
	if agent.AvgResponseTime > 0 {
		responseScore := 300.0 / float64(agent.AvgResponseTime)
		if responseScore > 2 {
			responseScore = 2
		}
		score += responseScore
	}
	if len(requiredSkills) > 0 {
		matched := 0
		for _, required := range requiredSkills {
			if slices.Contains(agent.Skills, required) {
				matched++
			}
		}
		score += float64(matched) / float64(len(requiredSkills)) * 2
	}
	switch priority {
	case "urgent":
		score += 0.2
	case "high":
		score += 0.1
	}
	return score
}
