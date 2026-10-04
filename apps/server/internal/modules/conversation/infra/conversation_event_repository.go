package infra

import (
	"context"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/conversation/domain"
	platformauth "servify/apps/server/internal/platform/auth"
)

// ConversationEventRepository 持久化会话服务过程事件流水（Service
// Timeline，V1.0 收敛 B1，docs/v1-convergence-plan.md §3.1）：
// 事件总线投影写入，业务模块不经仓储反向消费。
type ConversationEventRepository struct {
	db *gorm.DB
}

func NewConversationEventRepository(db *gorm.DB) *ConversationEventRepository {
	return &ConversationEventRepository{db: db}
}

// Append 投影写入一条事件；tenant/workspace 从请求上下文带入
// （与 conversation 主仓储写入口同口径）。
func (r *ConversationEventRepository) Append(ctx context.Context, event domain.ConversationEvent) error {
	model := toConversationEventModel(event)
	applyConversationEventScopeFields(ctx, &model)
	return r.db.WithContext(ctx).Create(&model).Error
}

// ListByConversation 按会话返回事件流水（新→旧）。
func (r *ConversationEventRepository) ListByConversation(ctx context.Context, conversationID string, limit, offset int) ([]domain.ConversationEvent, error) {
	var records []models.ConversationEvent
	db := r.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("occurred_at DESC, id DESC")
	db = applyConversationScope(db, ctx)
	if limit > 0 {
		db = db.Limit(limit)
	}
	if offset > 0 {
		db = db.Offset(offset)
	}
	if err := db.Find(&records).Error; err != nil {
		return nil, err
	}
	events := make([]domain.ConversationEvent, 0, len(records))
	for _, record := range records {
		events = append(events, fromConversationEventModel(record))
	}
	return events, nil
}

func toConversationEventModel(event domain.ConversationEvent) models.ConversationEvent {
	return models.ConversationEvent{
		ID:             event.ID,
		ConversationID: event.ConversationID,
		EventType:      event.EventType,
		ActorType:      event.ActorType,
		ActorID:        event.ActorID,
		Summary:        event.Summary,
		Payload:        event.Payload,
		OccurredAt:     event.OccurredAt,
	}
}

func fromConversationEventModel(record models.ConversationEvent) domain.ConversationEvent {
	return domain.ConversationEvent{
		ID:             record.ID,
		ConversationID: record.ConversationID,
		EventType:      record.EventType,
		ActorType:      record.ActorType,
		ActorID:        record.ActorID,
		Summary:        record.Summary,
		Payload:        record.Payload,
		OccurredAt:     record.OccurredAt,
	}
}

func applyConversationEventScopeFields(ctx context.Context, event *models.ConversationEvent) {
	if event == nil {
		return
	}
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		event.TenantID = tenantID
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		event.WorkspaceID = workspaceID
	}
}
