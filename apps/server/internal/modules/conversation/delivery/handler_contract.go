package delivery

import (
	"context"
	"errors"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
)

var ErrConversationNotFound = errors.New("conversation not found")

// Re-export DTO types so handlers do not need to import the application layer directly.
type ConversationDTO = conversationapp.ConversationDTO
type ConversationMessageDTO = conversationapp.ConversationMessageDTO

type HandlerService interface {
	GetConversation(ctx context.Context, sessionID string) (*ConversationDTO, error)
	ListMessages(ctx context.Context, sessionID string, limit int) ([]ConversationMessageDTO, error)
	ListMessagesBefore(ctx context.Context, sessionID string, beforeMessageID string, limit int) ([]ConversationMessageDTO, error)
	SendAgentMessage(ctx context.Context, sessionID string, content string) (*ConversationMessageDTO, error)
	AssignAgent(ctx context.Context, sessionID string, agentID uint) (*ConversationDTO, error)
	Transfer(ctx context.Context, sessionID string, toAgentID uint) (*ConversationDTO, error)
	Close(ctx context.Context, sessionID string) (*ConversationDTO, error)
	CloseWithOptions(ctx context.Context, sessionID string, opts conversationapp.CloseOptions) (*ConversationDTO, error)
}
