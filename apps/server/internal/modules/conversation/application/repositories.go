package application

import (
	"context"

	"servify/apps/server/internal/modules/conversation/domain"
)

type ConversationRepository interface {
	CreateConversation(ctx context.Context, conversation *domain.Conversation) error
	GetConversation(ctx context.Context, conversationID string) (*domain.Conversation, error)
	UpdateConversation(ctx context.Context, conversation *domain.Conversation) error
	AppendMessage(ctx context.Context, message *domain.ConversationMessage) error
	ListRecentMessages(ctx context.Context, conversationID string, limit int) ([]domain.ConversationMessage, error)
	ListMessagesBefore(ctx context.Context, conversationID string, beforeMessageID string, limit int) ([]domain.ConversationMessage, error)
	// ListMessagesAfter 按消息 ID 单调游标向后翻页（访客增量补拉 §10 #1）：
	// afterMessageID 为空表示从会话头开始；游标非数字返回错误（契约明确拒绝，
	// 不静默空集）。
	ListMessagesAfter(ctx context.Context, conversationID string, afterMessageID string, limit int) ([]domain.ConversationMessage, error)
	// ListSessions 开放 API 只读分页查询（沿用请求作用域的租户/工作区过滤）。
	ListSessions(ctx context.Context, query OpenSessionListQuery) ([]domain.Conversation, int64, error)
}

// OpenSessionListQuery 开放 API 会话列表查询参数。
type OpenSessionListQuery struct {
	Status   string // domain.ConversationStatus 值；空 = 全部
	Channel  string // 渠道名（session.platform）；空 = 全部
	Page     int
	PageSize int
}
