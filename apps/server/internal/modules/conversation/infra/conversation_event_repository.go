package infra

import (
	"context"
	"errors"

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
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		// 同 event_id 重复投递（outbox 崩溃重发）幂等忽略：Timeline 不产生
		// 重复行，消费者去重由事件唯一标识承担。
		if errors.Is(err, gorm.ErrDuplicatedKey) && model.EventID != nil && *model.EventID != "" {
			return nil
		}
		return err
	}
	return nil
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
	var eventID *string
	if event.EventID != "" {
		v := event.EventID
		eventID = &v
	}
	return models.ConversationEvent{
		ID:             event.ID,
		EventID:        eventID,
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
	eventID := ""
	if record.EventID != nil {
		eventID = *record.EventID
	}
	return domain.ConversationEvent{
		ID:             record.ID,
		EventID:        eventID,
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
