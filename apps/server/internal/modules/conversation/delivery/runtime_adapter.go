package delivery

import (
	"context"
	"fmt"
	"time"

	"servify/apps/server/internal/models"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"
	conversationinfra "servify/apps/server/internal/modules/conversation/infra"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/eventbus"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RuntimeAdapter struct {
	publisher eventbus.Bus
	db        *gorm.DB
	now       func() time.Time
}

func NewRuntimeAdapter(db *gorm.DB, publisher eventbus.Bus) *RuntimeAdapter {
	return &RuntimeAdapter{publisher: publisher, db: db, now: time.Now}
}

func (a *RuntimeAdapter) LoadTransferSession(ctx context.Context, sessionID string) (*TransferSession, error) {
	if a.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var model models.Session
	query := a.db.WithContext(ctx)
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		query = query.Where("workspace_id = ?", workspaceID)
	}
	if err := query.
		Preload("User").
		First(&model, "id = ?", sessionID).Error; err != nil {
		return nil, err
	}
	return &TransferSession{
		ID:           model.ID,
		CustomerID:   model.UserID,
		AgentID:      model.AgentID,
		TicketID:     model.TicketID,
		Status:       model.Status,
		Platform:     model.Platform,
		UserName:     model.User.Name,
		UserUsername: model.User.Username,
	}, nil
}

func (a *RuntimeAdapter) LockedTransferAgentID(ctx context.Context, tx *gorm.DB, sessionID string) (*uint, error) {
	if tx == nil {
		return nil, gorm.ErrInvalidDB
	}
	if sessionID == "" {
		return nil, nil
	}
	query := tx.WithContext(ctx).Model(&models.Session{}).Select("agent_id")
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		query = query.Where("workspace_id = ?", workspaceID)
	}
	if dialector := tx.Name(); dialector == "postgres" || dialector == "mysql" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row struct {
		AgentID *uint `gorm:"column:agent_id"`
	}
	if err := query.Where("id = ?", sessionID).Take(&row).Error; err != nil {
		return nil, err
	}
	return row.AgentID, nil
}

func (a *RuntimeAdapter) SyncTransferAssignment(ctx context.Context, tx *gorm.DB, sessionID string, customerID uint, agentID uint) error {
	now := a.now()
	item := &conversationdomain.Conversation{
		ID:         sessionID,
		CustomerID: uintPtr(customerID),
		Status:     conversationdomain.ConversationStatusActive,
		Channel: conversationdomain.ChannelBinding{
			Channel:   "web",
			SessionID: sessionID,
		},
		Participants: []conversationdomain.Participant{
			{
				ID:     buildParticipantID("customer", customerID),
				UserID: uintPtr(customerID),
				Role:   conversationdomain.ParticipantRoleCustomer,
			},
			{
				ID:     buildParticipantID("agent", agentID),
				UserID: uintPtr(agentID),
				Role:   conversationdomain.ParticipantRoleAgent,
			},
		},
		StartedAt:     now,
		LastMessageAt: &now,
	}
	return conversationinfra.NewGormRepository(tx).UpdateConversation(ctx, item)
}

func (a *RuntimeAdapter) SyncWaitingAssignment(ctx context.Context, tx *gorm.DB, sessionID string, customerID uint) error {
	now := a.now()
	item := &conversationdomain.Conversation{
		ID:         sessionID,
		CustomerID: uintPtr(customerID),
		Status:     conversationdomain.ConversationStatusActive,
		Channel: conversationdomain.ChannelBinding{
			Channel:   "web",
			SessionID: sessionID,
		},
		Participants: []conversationdomain.Participant{
			{
				ID:     buildParticipantID("customer", customerID),
				UserID: uintPtr(customerID),
				Role:   conversationdomain.ParticipantRoleCustomer,
			},
			{
				ID:   buildParticipantID("agent", 0),
				Role: conversationdomain.ParticipantRoleAgent,
			},
		},
		StartedAt:     now,
		LastMessageAt: &now,
	}
	return conversationinfra.NewGormRepository(tx).UpdateConversation(ctx, item)
}

func (a *RuntimeAdapter) AppendSystemMessage(ctx context.Context, tx *gorm.DB, sessionID string, content string, createdAt time.Time) error {
	repo := conversationinfra.NewGormRepository(tx)
	conversation, err := repo.GetConversation(ctx, sessionID)
	if err != nil {
		return err
	}

	if createdAt.IsZero() {
		createdAt = a.now()
	}

	message := &conversationdomain.ConversationMessage{
		ID:             fmt.Sprintf("%s-system-%d", sessionID, createdAt.UnixNano()),
		ConversationID: sessionID,
		Sender:         conversationdomain.ParticipantRoleSystem,
		Kind:           conversationdomain.MessageKindSystem,
		Content:        content,
		CreatedAt:      createdAt,
	}
	if err := repo.AppendMessage(ctx, message); err != nil {
		return err
	}

	item := &conversationdomain.Conversation{
		ID:            conversation.ID,
		CustomerID:    conversation.CustomerID,
		Status:        conversationdomain.ConversationStatusActive,
		Subject:       conversation.Subject,
		Channel:       conversation.Channel,
		Participants:  conversation.Participants,
		StartedAt:     conversation.StartedAt,
		LastMessageAt: &createdAt,
		EndedAt:       conversation.EndedAt,
	}
	return repo.UpdateConversation(ctx, item)
}

func buildParticipantID(prefix string, userID uint) string {
	return fmt.Sprintf("%s:%d", prefix, userID)
}

func uintPtr(v uint) *uint {
	return &v
}

var _ RuntimeService = (*RuntimeAdapter)(nil)
