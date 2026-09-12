package delivery

import (
	"context"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
)

// OpenSessionListQuery 与 conversationapp.OpenSessionListQuery 同形，
// handlers 经 delivery 契约引用，不直接 import application 层。
type OpenSessionListQuery = conversationapp.OpenSessionListQuery

// OpenConversationReader 开放 API（X-API-Key service principal）只读能力的窄接口，
// 由 conversationapp.Service 满足；写操作一律不暴露。
type OpenConversationReader interface {
	ListConversations(ctx context.Context, query OpenSessionListQuery) ([]conversationapp.ConversationDTO, int64, error)
	GetConversation(ctx context.Context, conversationID string) (*conversationapp.ConversationDTO, error)
	ListRecentMessages(ctx context.Context, conversationID string, limit int) ([]conversationapp.ConversationMessageDTO, error)
}

// OpenConversationAdapter 将 conversationapp.Service 适配为开放 API 只读面。
type OpenConversationAdapter struct {
	service *conversationapp.Service
}

var _ OpenConversationReader = (*OpenConversationAdapter)(nil)

func NewOpenConversationAdapter(service *conversationapp.Service) *OpenConversationAdapter {
	return &OpenConversationAdapter{service: service}
}

func (a *OpenConversationAdapter) ListConversations(ctx context.Context, query conversationapp.OpenSessionListQuery) ([]conversationapp.ConversationDTO, int64, error) {
	return a.service.ListConversations(ctx, query)
}

func (a *OpenConversationAdapter) GetConversation(ctx context.Context, conversationID string) (*conversationapp.ConversationDTO, error) {
	return a.service.GetConversation(ctx, conversationID)
}

func (a *OpenConversationAdapter) ListRecentMessages(ctx context.Context, conversationID string, limit int) ([]conversationapp.ConversationMessageDTO, error) {
	return a.service.ListRecentMessages(ctx, conversationID, limit)
}
