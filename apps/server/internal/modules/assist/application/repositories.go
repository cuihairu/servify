package application

import (
	"context"

	"servify/apps/server/internal/models"
)

// Repository 远程协助持久化端口（infra/gorm 实现）。
type Repository interface {
	CreateSession(ctx context.Context, session *models.RemoteAssistSession) error
	GetSession(ctx context.Context, id uint) (*models.RemoteAssistSession, error)
	ListSessions(ctx context.Context, conversationSessionID string, limit int) ([]models.RemoteAssistSession, error)
	SaveSession(ctx context.Context, session *models.RemoteAssistSession) error
	GetConversationSessionOwner(ctx context.Context, sessionID string) (uint, error)

	ListAnnotations(ctx context.Context, assistSessionID uint) ([]models.RemoteAssistAnnotation, error)
	CreateAnnotation(ctx context.Context, annotation *models.RemoteAssistAnnotation) error
	GetAnnotation(ctx context.Context, id uint) (*models.RemoteAssistAnnotation, error)
	DeleteAnnotation(ctx context.Context, id uint) error
}
