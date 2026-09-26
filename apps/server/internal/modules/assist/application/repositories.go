package application

import (
	"context"
	assistdomain "servify/apps/server/internal/modules/assist/domain"
)

// Repository 远程协助持久化端口（infra/gorm 实现）。
// 读取类方法按 ctx 携带的租户/工作区过滤（scope 为空 = 不过滤，
// 与 macro/satisfaction 等模块同口径）。
type Repository interface {
	CreateSession(ctx context.Context, session *assistdomain.RemoteAssistSession) error
	GetSession(ctx context.Context, id uint) (*assistdomain.RemoteAssistSession, error)
	ListSessions(ctx context.Context, conversationSessionID string, limit int) ([]assistdomain.RemoteAssistSession, error)
	SaveSession(ctx context.Context, session *assistdomain.RemoteAssistSession) error
	GetConversationSessionOwner(ctx context.Context, sessionID string) (uint, error)
	// FindActiveSessionIDByConversation 返回该客服会话当前 active 协助的
	// ID（存在即拒绝再次发起，保证单活跃）；无活跃返回 0。
	FindActiveSessionIDByConversation(ctx context.Context, conversationSessionID string) (uint, error)

	ListAnnotations(ctx context.Context, assistSessionID uint) ([]assistdomain.RemoteAssistAnnotation, error)
	CreateAnnotation(ctx context.Context, annotation *assistdomain.RemoteAssistAnnotation) error
	DeleteAnnotation(ctx context.Context, id uint) error
}
