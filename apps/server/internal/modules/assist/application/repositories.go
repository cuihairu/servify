package application

import (
	"context"
	assistdomain "servify/apps/server/internal/modules/assist/domain"
)

// Repository 远程协助持久化端口（infra/gorm 实现）。
type Repository interface {
	CreateSession(ctx context.Context, session *assistdomain.RemoteAssistSession) error
	GetSession(ctx context.Context, id uint) (*assistdomain.RemoteAssistSession, error)
	ListSessions(ctx context.Context, conversationSessionID string, limit int) ([]assistdomain.RemoteAssistSession, error)
	SaveSession(ctx context.Context, session *assistdomain.RemoteAssistSession) error
	GetConversationSessionOwner(ctx context.Context, sessionID string) (uint, error)

	ListAnnotations(ctx context.Context, assistSessionID uint) ([]assistdomain.RemoteAssistAnnotation, error)
	CreateAnnotation(ctx context.Context, annotation *assistdomain.RemoteAssistAnnotation) error
	DeleteAnnotation(ctx context.Context, id uint) error
}
