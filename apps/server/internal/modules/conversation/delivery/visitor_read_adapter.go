// 访客未读数/已读游标窄契约（M3 移动 SDK 配套 §10 #3）：免认证 REST 面，
// 与 §10 #1 增量补拉同构——移动 SDK 以 last_read_message_id 推进已读游标
// 并换回最新未读数（点开推送/回前台一次性对齐），服务端口径与 D7 客户端
// 推导一致（只计 agent/system 来源、游标只前进）。
package delivery

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/conversation/application"
)

// VisitorReadService 是访客未读面的窄契约（handlers 层依赖此接口）。
type VisitorReadService interface {
	// MarkRead 推进已读游标并返回推进后的未读数（省一次往返）。
	MarkRead(ctx context.Context, sessionID, lastReadMessageID string) (*VisitorReadState, error)
	// UnreadState 返回当前未读数与已读游标。
	UnreadState(ctx context.Context, sessionID string) (*VisitorReadState, error)
}

// VisitorReadState 是未读面响应体：unread_count 只计 agent/system 来源
// 且 ID 大于游标的消息；last_read_message_id 回显当前游标（客户端用它
// 对齐本地状态）。
type VisitorReadState struct {
	UnreadCount       int64  `json:"unread_count"`
	LastReadMessageID string `json:"last_read_message_id"`
}

// VisitorReadAdapter 桥接 handler 与会话服务：会话必须已存在（行在首条
// 消息持久化时建，与 §10 #1 同语义），存在性校验把「会话失效」与
// 「无未读」区分开（404 vs 200 零值）。
type VisitorReadAdapter struct {
	service *application.Service
	db      *gorm.DB
}

// NewVisitorReadAdapter 创建访客未读面适配器。
func NewVisitorReadAdapter(service *application.Service, db *gorm.DB) *VisitorReadAdapter {
	return &VisitorReadAdapter{service: service, db: db}
}

// MarkRead 推进已读游标并回读最新未读状态。游标非法（不存在/跨会话）
// 契约性拒绝（与 §10 #1 的 invalid message cursor 同文案同语义）。
func (a *VisitorReadAdapter) MarkRead(ctx context.Context, sessionID, lastReadMessageID string) (*VisitorReadState, error) {
	if err := a.requireSession(ctx, sessionID); err != nil {
		return nil, err
	}
	if err := a.service.MarkVisitorRead(ctx, sessionID, lastReadMessageID); err != nil {
		return nil, err
	}
	return a.UnreadState(ctx, sessionID)
}

// UnreadState 返回当前未读数与已读游标（会话不存在时由 requireSession
// 前置拒绝）。
func (a *VisitorReadAdapter) UnreadState(ctx context.Context, sessionID string) (*VisitorReadState, error) {
	if err := a.requireSession(ctx, sessionID); err != nil {
		return nil, err
	}
	count, cursor, err := a.service.VisitorUnreadCount(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return &VisitorReadState{UnreadCount: count, LastReadMessageID: cursor}, nil
}

// requireSession 复用 §10 #1 的会话存在性校验与错误口径。
func (a *VisitorReadAdapter) requireSession(ctx context.Context, sessionID string) error {
	var session models.Session
	if err := a.db.WithContext(ctx).First(&session, "id = ?", sessionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("session not found: %s", sessionID)
		}
		return fmt.Errorf("session lookup failed: %w", err)
	}
	return nil
}
