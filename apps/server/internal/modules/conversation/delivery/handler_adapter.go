package delivery

import (
	"context"
	"errors"
	"time"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"

	"gorm.io/gorm"
)

type HandlerServiceAdapter struct {
	service *conversationapp.Service
}

func NewHandlerService(service *conversationapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{service: service}
}

func (a *HandlerServiceAdapter) GetConversation(ctx context.Context, sessionID string) (*conversationapp.ConversationDTO, error) {
	dto, err := a.service.GetConversation(ctx, sessionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			items, listErr := a.service.ListRecentMessages(ctx, sessionID, 1)
			if listErr == nil && len(items) > 0 {
				return synthesizeConversationDTO(sessionID, items[0].CreatedAt), nil
			}
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	return dto, nil
}

func (a *HandlerServiceAdapter) ListMessages(ctx context.Context, sessionID string, limit int) ([]conversationapp.ConversationMessageDTO, error) {
	if _, err := a.service.GetConversation(ctx, sessionID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	items, err := a.service.ListRecentMessages(ctx, sessionID, limit)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	return items, nil
}

func (a *HandlerServiceAdapter) ListMessagesBefore(ctx context.Context, sessionID string, beforeMessageID string, limit int) ([]conversationapp.ConversationMessageDTO, error) {
	if _, err := a.service.GetConversation(ctx, sessionID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	items, err := a.service.ListMessagesBefore(ctx, sessionID, beforeMessageID, limit)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	return items, nil
}

func (a *HandlerServiceAdapter) SendAgentMessage(ctx context.Context, sessionID string, content string) (*conversationapp.ConversationMessageDTO, error) {
	if _, err := a.service.GetConversation(ctx, sessionID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}

	item, err := a.service.IngestTextMessage(ctx, conversationapp.IngestTextMessageCommand{
		ConversationID: sessionID,
		Sender:         conversationdomain.ParticipantRoleAgent,
		Content:        content,
		Metadata: map[string]string{
			"source": "admin",
		},
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (a *HandlerServiceAdapter) AssignAgent(ctx context.Context, sessionID string, agentID uint) (*conversationapp.ConversationDTO, error) {
	dto, err := a.service.AssignAgent(ctx, sessionID, agentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	return dto, nil
}

func (a *HandlerServiceAdapter) Transfer(ctx context.Context, sessionID string, toAgentID uint) (*conversationapp.ConversationDTO, error) {
	dto, err := a.service.Transfer(ctx, sessionID, toAgentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	return dto, nil
}

func (a *HandlerServiceAdapter) Close(ctx context.Context, sessionID string) (*conversationapp.ConversationDTO, error) {
	dto, err := a.service.Close(ctx, sessionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	return dto, nil
}

// CloseWithOptions 透传关闭选项（未结工单拦截 / AllowOpenTickets 显式降级，
// v1-convergence-plan §7.1）；ErrOpenTicketsRemain 原样上抛由 HTTP 层映射 409。
func (a *HandlerServiceAdapter) CloseWithOptions(ctx context.Context, sessionID string, opts conversationapp.CloseOptions) (*conversationapp.ConversationDTO, error) {
	dto, err := a.service.CloseWithOptions(ctx, sessionID, opts)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	return dto, nil
}

var _ HandlerService = (*HandlerServiceAdapter)(nil)

func synthesizeConversationDTO(sessionID string, lastMessageAt time.Time) *conversationapp.ConversationDTO {
	return &conversationapp.ConversationDTO{
		ID:     sessionID,
		Status: string(conversationdomain.ConversationStatusActive),
		Channel: conversationdomain.ChannelBinding{
			Channel:   "web",
			SessionID: sessionID,
		},
		StartedAt:     lastMessageAt,
		LastMessageAt: &lastMessageAt,
	}
}
