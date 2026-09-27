package application

import (
	"context"
	"time"

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
	// CountActiveSessionsByAgent 返回某坐席当前 active 协助数（并发上限守卫）。
	CountActiveSessionsByAgent(ctx context.Context, agentUserID uint) (int64, error)
	// ExpireStaleActiveSessions 把 started_at 早于 cutoff 的 active 会话批量
	// 置为 failed 并落 ended_at（会话 TTL 懒清扫）；返回受影响行数。
	ExpireStaleActiveSessions(ctx context.Context, cutoff time.Time) (int64, error)

	ListAnnotations(ctx context.Context, assistSessionID uint) ([]assistdomain.RemoteAssistAnnotation, error)
	CreateAnnotation(ctx context.Context, annotation *assistdomain.RemoteAssistAnnotation) error
	// DeleteAnnotation 删除标注；restricted=true 时只允许删除 created_by =
	// ownerID 的标注（非 admin 只删自己的），命中为零与不存在同返 not found。
	DeleteAnnotation(ctx context.Context, id uint, ownerID uint, restricted bool) error
}
