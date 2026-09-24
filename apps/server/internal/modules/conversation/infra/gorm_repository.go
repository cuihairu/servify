package infra

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	"servify/apps/server/internal/modules/conversation/domain"
	platformauth "servify/apps/server/internal/platform/auth"
)

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) CreateConversation(ctx context.Context, conversation *domain.Conversation) error {
	if conversation == nil {
		return fmt.Errorf("conversation required")
	}

	model := mapConversationModel(*conversation)
	applyConversationScopeFields(ctx, &model)
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return err
	}
	*conversation = mapConversation(model)
	return nil
}

func (r *GormRepository) GetConversation(ctx context.Context, conversationID string) (*domain.Conversation, error) {
	var model models.Session
	err := applyConversationScope(r.db.WithContext(ctx), ctx).
		Preload("User").
		Preload("Agent").
		First(&model, "id = ?", conversationID).Error
	if err != nil {
		return nil, err
	}
	result := mapConversation(model)
	return &result, nil
}

func (r *GormRepository) UpdateConversation(ctx context.Context, conversation *domain.Conversation) error {
	if conversation == nil {
		return fmt.Errorf("conversation required")
	}

	updates := map[string]interface{}{
		"status":     mapConversationStatusToSessionStatus(conversation.Status),
		"updated_at": conversation.StartedAt,
	}
	if conversation.CustomerID != nil {
		updates["user_id"] = *conversation.CustomerID
	}
	if conversation.LastMessageAt != nil {
		updates["updated_at"] = *conversation.LastMessageAt
	}
	if conversation.EndedAt != nil {
		updates["ended_at"] = conversation.EndedAt
		updates["updated_at"] = *conversation.EndedAt
	}
	if conversation.Channel.Channel != "" {
		updates["platform"] = conversation.Channel.Channel
	}
	if agentID := resolveConversationAgentID(conversation.Participants); agentID != nil {
		updates["agent_id"] = *agentID
	} else if hasExplicitAgentParticipant(conversation.Participants) {
		updates["agent_id"] = nil
	}

	return applyConversationScope(r.db.WithContext(ctx).Model(&models.Session{}), ctx).Where("id = ?", conversation.ID).Updates(updates).Error
}

func (r *GormRepository) AppendMessage(ctx context.Context, message *domain.ConversationMessage) error {
	if message == nil {
		return fmt.Errorf("message required")
	}

	model := mapMessageModel(*message)
	applyMessageScopeFields(ctx, &model)
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return err
	}
	*message = mapMessage(model)
	return nil
}

func (r *GormRepository) ListRecentMessages(ctx context.Context, conversationID string, limit int) ([]domain.ConversationMessage, error) {
	if limit <= 0 {
		limit = 10
	}
	var items []models.Message
	if err := applyConversationScope(r.db.WithContext(ctx), ctx).
		Where("session_id = ?", conversationID).
		Order("created_at DESC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]domain.ConversationMessage, 0, len(items))
	for _, item := range items {
		out = append(out, mapMessage(item))
	}
	return out, nil
}

func (r *GormRepository) ListMessagesBefore(ctx context.Context, conversationID string, beforeMessageID string, limit int) ([]domain.ConversationMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	q := applyConversationScope(r.db.WithContext(ctx), ctx).
		Where("session_id = ?", conversationID)
	if beforeMessageID != "" {
		var pivot models.Message
		if err := applyConversationScope(r.db.WithContext(ctx), ctx).
			Where("session_id = ?", conversationID).
			Where("id = ?", beforeMessageID).
			First(&pivot).Error; err != nil {
			return nil, err
		}
		q = q.Where("created_at < ?", pivot.CreatedAt)
	}
	var items []models.Message
	if err := q.Order("created_at DESC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]domain.ConversationMessage, 0, len(items))
	for _, item := range items {
		out = append(out, mapMessage(item))
	}
	return out, nil
}

// ListMessagesAfter 按消息 ID 单调游标向后翻页（访客增量补拉 §10 #1）：
// 自增主键序保证严格追加序（created_at 同秒多条会错序漏拉）；空游标从会话
// 头开始；非数字游标契约性拒绝（客户端游标只能来自本端点/WS 消息 id）。
func (r *GormRepository) ListMessagesAfter(ctx context.Context, conversationID string, afterMessageID string, limit int) ([]domain.ConversationMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	var afterID int64
	if afterMessageID != "" {
		parsed, err := strconv.ParseInt(strings.TrimSpace(afterMessageID), 10, 64)
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("invalid message cursor: %s", afterMessageID)
		}
		afterID = parsed
	}
	var items []models.Message
	if err := applyConversationScope(r.db.WithContext(ctx), ctx).
		Where("session_id = ?", conversationID).
		Where("id > ?", afterID).
		Order("id ASC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]domain.ConversationMessage, 0, len(items))
	for _, item := range items {
		out = append(out, mapMessage(item))
	}
	return out, nil
}

// ListSessions 开放 API 只读分页查询；沿用请求作用域（租户/工作区）过滤。
func (r *GormRepository) ListSessions(ctx context.Context, query conversationapp.OpenSessionListQuery) ([]domain.Conversation, int64, error) {
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}
	q := applyConversationScope(r.db.WithContext(ctx).Model(&models.Session{}), ctx)
	if query.Status != "" {
		q = q.Where("status = ?", mapConversationStatusToSessionStatus(domain.ConversationStatus(strings.ToLower(strings.TrimSpace(query.Status)))))
	}
	if query.Channel != "" {
		q = q.Where("platform = ?", strings.TrimSpace(query.Channel))
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []models.Session
	if err := q.Order("created_at DESC").
		Limit(query.PageSize).
		Offset((query.Page - 1) * query.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	out := make([]domain.Conversation, 0, len(items))
	for _, item := range items {
		out = append(out, mapConversation(item))
	}
	return out, total, nil
}

func mapConversation(model models.Session) domain.Conversation {
	participants := make([]domain.Participant, 0, 2)
	if model.UserID != 0 {
		name := firstNonEmpty(model.User.Name, model.User.Username)
		participants = append(participants, domain.Participant{
			ID:          fmt.Sprintf("user:%d", model.UserID),
			UserID:      &model.UserID,
			Role:        domain.ParticipantRoleCustomer,
			DisplayName: name,
		})
	}
	if model.AgentID != nil {
		agentID := *model.AgentID
		name := ""
		if model.Agent != nil {
			name = firstNonEmpty(model.Agent.Name, model.Agent.Username)
		}
		participants = append(participants, domain.Participant{
			ID:          fmt.Sprintf("agent:%d", agentID),
			UserID:      &agentID,
			Role:        domain.ParticipantRoleAgent,
			DisplayName: name,
		})
	}

	var lastMessageAt *time.Time
	if !model.UpdatedAt.IsZero() {
		t := model.UpdatedAt
		lastMessageAt = &t
	}

	var endedAt *time.Time
	if model.EndedAt != nil {
		t := *model.EndedAt
		endedAt = &t
	}

	var customerID *uint
	if model.UserID != 0 {
		customerID = &model.UserID
	}

	return domain.Conversation{
		ID:         model.ID,
		CustomerID: customerID,
		Status:     mapSessionStatusToConversationStatus(model.Status),
		Channel: domain.ChannelBinding{
			Channel:   model.Platform,
			SessionID: model.ID,
		},
		Participants:  participants,
		StartedAt:     model.StartedAt,
		LastMessageAt: lastMessageAt,
		EndedAt:       endedAt,
	}
}

func mapConversationModel(conversation domain.Conversation) models.Session {
	model := models.Session{
		ID:        conversation.ID,
		Status:    mapConversationStatusToSessionStatus(conversation.Status),
		Platform:  firstNonEmpty(conversation.Channel.Channel, "web"),
		StartedAt: conversation.StartedAt,
		UpdatedAt: conversation.StartedAt,
	}
	if conversation.CustomerID != nil {
		model.UserID = *conversation.CustomerID
	}
	if agentID := resolveConversationAgentID(conversation.Participants); agentID != nil {
		model.AgentID = agentID
	}
	if conversation.EndedAt != nil {
		model.EndedAt = conversation.EndedAt
		model.UpdatedAt = *conversation.EndedAt
	}
	if conversation.LastMessageAt != nil {
		model.UpdatedAt = *conversation.LastMessageAt
	}
	return model
}

func applyConversationScope(db *gorm.DB, ctx context.Context) *gorm.DB {
	tenantID := platformauth.TenantIDFromContext(ctx)
	workspaceID := platformauth.WorkspaceIDFromContext(ctx)
	if tenantID != "" {
		db = db.Where("tenant_id = ?", tenantID)
	}
	if workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	return db
}

func applyConversationScopeFields(ctx context.Context, model *models.Session) {
	if model == nil {
		return
	}
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		model.TenantID = tenantID
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		model.WorkspaceID = workspaceID
	}
}

func applyMessageScopeFields(ctx context.Context, model *models.Message) {
	if model == nil {
		return
	}
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		model.TenantID = tenantID
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		model.WorkspaceID = workspaceID
	}
}

func mapMessage(model models.Message) domain.ConversationMessage {
	return domain.ConversationMessage{
		ID:             fmt.Sprintf("%d", model.ID),
		ConversationID: model.SessionID,
		Sender:         mapMessageSender(model.Sender),
		Kind:           mapMessageKind(model.Type),
		Content:        model.Content,
		CreatedAt:      model.CreatedAt,
	}
}

func mapMessageModel(message domain.ConversationMessage) models.Message {
	model := models.Message{
		SessionID: message.ConversationID,
		Content:   message.Content,
		Type:      mapMessageKindToModel(message.Kind),
		Sender:    mapParticipantRoleToSender(message.Sender),
		CreatedAt: message.CreatedAt,
	}
	if userID := resolveMessageUserID(message); userID != 0 {
		model.UserID = userID
	}
	return model
}

func mapSessionStatusToConversationStatus(status string) domain.ConversationStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ended":
		return domain.ConversationStatusClosed
	case "transferred":
		return domain.ConversationStatusTransferred
	case "waiting_human":
		return domain.ConversationStatusWaitingHuman
	default:
		return domain.ConversationStatusActive
	}
}

func mapConversationStatusToSessionStatus(status domain.ConversationStatus) string {
	switch status {
	case domain.ConversationStatusClosed:
		return "ended"
	case domain.ConversationStatusTransferred:
		return "transferred"
	case domain.ConversationStatusWaitingHuman:
		return "waiting_human"
	default:
		return "active"
	}
}

func mapMessageSender(sender string) domain.ParticipantRole {
	switch strings.ToLower(strings.TrimSpace(sender)) {
	case "agent":
		return domain.ParticipantRoleAgent
	case "ai":
		return domain.ParticipantRoleAI
	case "system":
		return domain.ParticipantRoleSystem
	default:
		return domain.ParticipantRoleCustomer
	}
}

func mapParticipantRoleToSender(role domain.ParticipantRole) string {
	switch role {
	case domain.ParticipantRoleAgent:
		return "agent"
	case domain.ParticipantRoleAI:
		return "ai"
	case domain.ParticipantRoleSystem:
		return "system"
	default:
		return "user"
	}
}

func mapMessageKind(kind string) domain.MessageKind {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "system":
		return domain.MessageKindSystem
	default:
		return domain.MessageKindText
	}
}

func mapMessageKindToModel(kind domain.MessageKind) string {
	switch kind {
	case domain.MessageKindSystem:
		return "system"
	default:
		return "text"
	}
}

func resolveConversationAgentID(items []domain.Participant) *uint {
	for _, item := range items {
		if item.Role == domain.ParticipantRoleAgent && item.UserID != nil {
			return item.UserID
		}
	}
	return nil
}

func hasExplicitAgentParticipant(items []domain.Participant) bool {
	for _, item := range items {
		if item.Role == domain.ParticipantRoleAgent {
			return true
		}
	}
	return false
}

// resolveMessageUserID 从消息解析所属用户。当前为存根（恒 0），
// 包级 var seam 供测试注入非零值覆盖 mapMessageModel 的赋值分支。
var resolveMessageUserID = func(message domain.ConversationMessage) uint {
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
