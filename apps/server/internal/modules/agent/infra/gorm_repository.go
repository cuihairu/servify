package infra

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdomain "servify/apps/server/internal/modules/agent/domain"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/usersecurity"
)

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) CreateAgent(ctx context.Context, userID uint, department string, skills []string, maxChatConcurrency int) (*agentdomain.AgentProfile, error) {
	var user models.User
	if err := r.db.WithContext(ctx).First(&user, userID).Error; err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}
	var existing models.Agent
	if err := applyAgentScope(r.db.WithContext(ctx), ctx).Where("user_id = ?", userID).First(&existing).Error; err == nil {
		return nil, fmt.Errorf("user is already an agent")
	}
	agent := &models.Agent{
		UserID:        userID,
		Department:    department,
		Skills:        strings.Join(skills, ","),
		Status:        string(agentdomain.PresenceStatusOffline),
		MaxConcurrent: maxChatConcurrency,
		Rating:        5,
	}
	applyAgentScopeFields(ctx, agent)
	if err := r.db.WithContext(ctx).Create(agent).Error; err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}
	_ = r.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Update("role", "agent").Error
	return mapProfile(user, *agent), nil
}

func (r *GormRepository) GetAgentByUserID(ctx context.Context, userID uint) (*agentdomain.AgentProfile, *models.Agent, error) {
	var agent models.Agent
	if err := applyAgentScope(r.db.WithContext(ctx), ctx).Preload("User").Preload("Tickets", func(db *gorm.DB) *gorm.DB {
		db = db.Where("status NOT IN ?", []string{"closed"}).Order("created_at DESC")
		if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
			db = db.Where("tenant_id = ?", tenantID)
		}
		if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
			db = db.Where("workspace_id = ?", workspaceID)
		}
		return db
	}).Where("user_id = ?", userID).First(&agent).Error; err != nil {
		return nil, nil, fmt.Errorf("agent not found: %w", err)
	}
	profile := mapProfile(agent.User, agent)
	return profile, &agent, nil
}

func (r *GormRepository) ListAgents(ctx context.Context, limit int) ([]models.Agent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var agents []models.Agent
	if err := applyAgentScope(r.db.WithContext(ctx), ctx).Preload("User").Order("updated_at DESC").Limit(limit).Find(&agents).Error; err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}
	return agents, nil
}

func (r *GormRepository) GetAgentRuntimeByUserID(ctx context.Context, userID uint) (*agentapp.AgentRuntimeDTO, error) {
	var agent models.Agent
	if err := applyAgentScope(r.db.WithContext(ctx), ctx).
		Preload("User").
		Where("user_id = ? AND status <> ?", userID, string(agentdomain.PresenceStatusOffline)).
		First(&agent).Error; err != nil {
		return nil, fmt.Errorf("agent runtime not found: %w", err)
	}
	runtime := mapRuntime(agent.User, agent)
	return &runtime, nil
}

func (r *GormRepository) ListActiveAgentRuntimes(ctx context.Context) ([]agentapp.AgentRuntimeDTO, error) {
	var agents []models.Agent
	if err := applyAgentScope(r.db.WithContext(ctx), ctx).
		Preload("User").
		Where("status <> ?", string(agentdomain.PresenceStatusOffline)).
		Order("user_id ASC").
		Find(&agents).Error; err != nil {
		return nil, fmt.Errorf("failed to list active agent runtimes: %w", err)
	}
	runtimes := make([]agentapp.AgentRuntimeDTO, 0, len(agents))
	for _, agent := range agents {
		runtimes = append(runtimes, mapRuntime(agent.User, agent))
	}
	return runtimes, nil
}

func (r *GormRepository) UpdatePresenceStatus(ctx context.Context, userID uint, status agentdomain.PresenceStatus) error {
	result := applyAgentScope(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).Where("user_id = ?", userID).Update("status", string(status))
	if result.Error != nil {
		return fmt.Errorf("failed to update agent status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("agent not found: %w", gorm.ErrRecordNotFound)
	}
	return nil
}

// UpdateLastActivity updates the agent's last activity timestamp (persisted).
func (r *GormRepository) UpdateLastActivity(ctx context.Context, userID uint) error {
	now := time.Now()
	result := applyAgentScope(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).Where("user_id = ?", userID).Update("last_activity_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// SetConnectedTime sets the agent's connected timestamp (persisted).
func (r *GormRepository) SetConnectedTime(ctx context.Context, userID uint) error {
	now := time.Now()
	result := applyAgentScope(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).Where("user_id = ?", userID).Update("connected_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ClearConnectedTime clears the agent's connected timestamp when going offline.
func (r *GormRepository) ClearConnectedTime(ctx context.Context, userID uint) error {
	result := applyAgentScope(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).Where("user_id = ?", userID).Update("connected_at", nil)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *GormRepository) UpdateChatLoad(ctx context.Context, userID uint, currentLoad int) error {
	result := applyAgentScope(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).Where("user_id = ?", userID).Update("current_load", currentLoad)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *GormRepository) GetSessionByID(ctx context.Context, sessionID string) (*models.Session, error) {
	var session models.Session
	if err := r.db.WithContext(ctx).First(&session, "id = ?", sessionID).Error; err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	return &session, nil
}

func (r *GormRepository) AssignSession(ctx context.Context, sessionID string, agentUserID uint) error {
	result := r.db.WithContext(ctx).Model(&models.Session{}).Where("id = ?", sessionID).Updates(map[string]interface{}{
		"agent_id": agentUserID,
		"status":   "active",
		"ended_at": nil,
	})
	if result.Error != nil {
		return fmt.Errorf("failed to assign session: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("session not found: %w", gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *GormRepository) ReleaseSession(ctx context.Context, sessionID string, agentUserID uint) error {
	result := r.db.WithContext(ctx).Model(&models.Session{}).Where("id = ? AND agent_id = ?", sessionID, agentUserID).Updates(map[string]interface{}{
		"status":   "ended",
		"ended_at": time.Now(),
	})
	if result.Error != nil {
		return fmt.Errorf("failed to release session: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("session not found or not assigned to agent: %w", gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *GormRepository) GetStats(ctx context.Context, agentUserID *uint) (*agentapp.AgentStatsDTO, error) {
	stats := &agentapp.AgentStatsDTO{}
	query := applyAgentScope(r.db.WithContext(ctx).Model(&models.Agent{}), ctx)
	if agentUserID != nil {
		query = query.Where("user_id = ?", *agentUserID)
	}
	query.Count(&stats.Total)
	var avgResponseTime float64
	applyAgentScope(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).Select("AVG(avg_response_time)").Row().Scan(&avgResponseTime)
	stats.AvgResponseTime = int64(avgResponseTime)
	var avgRating float64
	applyAgentScope(r.db.WithContext(ctx).Model(&models.Agent{}), ctx).Select("AVG(rating)").Row().Scan(&avgRating)
	stats.AvgRating = avgRating
	return stats, nil
}

func (r *GormRepository) RevokeUserTokens(ctx context.Context, userID uint, revokeAt time.Time) (int, error) {
	return usersecurity.RevokeUserTokens(ctx, r.db, userID, revokeAt)
}

// GetLastAgentForCustomer 亲和路由：客户在 since 之后最近一次被坐席服务的 agent_id。
func (r *GormRepository) GetLastAgentForCustomer(ctx context.Context, customerUserID uint, since time.Time) (*uint, error) {
	var session models.Session
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND agent_id IS NOT NULL AND created_at >= ?", customerUserID, since).
		Order("created_at DESC").
		First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find last agent for customer: %w", err)
	}
	return session.AgentID, nil
}

// GetAgentGroup 取单个组（软删过滤，组员操作不需要 scope）。
func (r *GormRepository) GetAgentGroup(ctx context.Context, id uint) (*models.AgentGroup, error) {
	var group models.AgentGroup
	if err := r.db.WithContext(ctx).First(&group, id).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

// ListAgentGroups 返回全部组（软删过滤，系统级读取不加 scope）。
func (r *GormRepository) ListAgentGroups(ctx context.Context) ([]models.AgentGroup, error) {
	var groups []models.AgentGroup
	if err := r.db.WithContext(ctx).
		Order("priority DESC, created_at ASC").
		Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("failed to list agent groups: %w", err)
	}
	return groups, nil
}

// CreateAgentGroup 创建组；tenant/workspace 从 ctx scope 注入（有则写）。
func (r *GormRepository) CreateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	if group.OverflowPolicy == "" {
		group.OverflowPolicy = "global"
	}
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		group.TenantID = tenantID
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		group.WorkspaceID = workspaceID
	}
	if err := r.db.WithContext(ctx).Create(group).Error; err != nil {
		return fmt.Errorf("failed to create agent group: %w", err)
	}
	return nil
}

// UpdateAgentGroup 更新组基础字段（按主键，不整行覆盖）。
func (r *GormRepository) UpdateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	updates := map[string]interface{}{
		"name":            group.Name,
		"description":     group.Description,
		"priority":        group.Priority,
		"overflow_policy": group.OverflowPolicy,
		"enabled":         group.Enabled,
	}
	if err := r.db.WithContext(ctx).Model(&models.AgentGroup{}).Where("id = ?", group.ID).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update agent group: %w", err)
	}
	return nil
}

// DeleteAgentGroup 软删组并清空成员（事务）。
func (r *GormRepository) DeleteAgentGroup(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.AgentGroup{}, id).Error; err != nil {
			return fmt.Errorf("failed to delete agent group: %w", err)
		}
		if err := tx.Where("group_id = ?", id).Delete(&models.AgentGroupMember{}).Error; err != nil {
			return fmt.Errorf("failed to clear group members: %w", err)
		}
		return nil
	})
}

// ReplaceGroupMembers 全量替换组成员（事务；去重入参）。
func (r *GormRepository) ReplaceGroupMembers(ctx context.Context, groupID uint, agentUserIDs []uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", groupID).Delete(&models.AgentGroupMember{}).Error; err != nil {
			return fmt.Errorf("failed to clear group members: %w", err)
		}
		seen := make(map[uint]struct{}, len(agentUserIDs))
		for _, userID := range agentUserIDs {
			if userID == 0 {
				continue
			}
			if _, dup := seen[userID]; dup {
				continue
			}
			seen[userID] = struct{}{}
			member := models.AgentGroupMember{GroupID: groupID, AgentUserID: userID}
			if err := tx.Create(&member).Error; err != nil {
				return fmt.Errorf("failed to add group member %d: %w", userID, err)
			}
		}
		return nil
	})
}

// ListEnabledGroupMemberIDs 启用组的成员 user_id；组不存在或禁用返回空切片。
func (r *GormRepository) ListEnabledGroupMemberIDs(ctx context.Context, groupID uint) ([]uint, error) {
	var group models.AgentGroup
	if err := r.db.WithContext(ctx).First(&group, groupID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get agent group: %w", err)
	}
	if !group.Enabled {
		return nil, nil
	}
	var ids []uint
	if err := r.db.WithContext(ctx).Model(&models.AgentGroupMember{}).
		Where("group_id = ?", groupID).
		Pluck("agent_user_id", &ids).Error; err != nil {
		return nil, fmt.Errorf("failed to list group members: %w", err)
	}
	return ids, nil
}

func applyAgentScope(db *gorm.DB, ctx context.Context) *gorm.DB {
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		db = db.Where("tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	return db
}

func applyAgentScopeFields(ctx context.Context, agent *models.Agent) {
	if agent == nil {
		return
	}
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		agent.TenantID = tenantID
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		agent.WorkspaceID = workspaceID
	}
}

func mapProfile(user models.User, agent models.Agent) *agentdomain.AgentProfile {
	return &agentdomain.AgentProfile{
		UserID:              agent.UserID,
		Username:            user.Username,
		Name:                user.Name,
		Department:          agent.Department,
		Skills:              splitSkills(agent.Skills),
		MaxChatConcurrency:  agent.MaxConcurrent,
		MaxVoiceConcurrency: 1,
		CurrentChatLoad:     agent.CurrentLoad,
		CurrentVoiceLoad:    0,
		Rating:              agent.Rating,
		AvgResponseTime:     agent.AvgResponseTime,
		TotalTickets:        agent.TotalTickets,
	}
}

func mapRuntime(user models.User, agent models.Agent) agentapp.AgentRuntimeDTO {
	runtime := agentapp.AgentRuntimeDTO{
		UserID:              agent.UserID,
		Username:            user.Username,
		Name:                user.Name,
		Department:          agent.Department,
		Skills:              splitSkills(agent.Skills),
		Status:              agent.Status,
		MaxChatConcurrency:  agent.MaxConcurrent,
		MaxVoiceConcurrency: 1,
		CurrentChatLoad:     agent.CurrentLoad,
		CurrentVoiceLoad:    0,
		Rating:              agent.Rating,
		AvgResponseTime:     agent.AvgResponseTime,
	}
	// Use persisted LastActivityAt/ConnectedAt if available
	if agent.LastActivityAt != nil {
		runtime.LastActivity = *agent.LastActivityAt
	}
	if agent.ConnectedAt != nil {
		runtime.ConnectedAt = *agent.ConnectedAt
	}
	return runtime
}

func splitSkills(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
