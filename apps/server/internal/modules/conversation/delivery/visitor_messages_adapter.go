// 访客会话消息增量拉取窄契约（M3 移动 SDK 配套 §10 #1）：免认证 REST 通道
// 唯一可用的消息读入口，与 push PushRegistrationService / ticket
// VisitorTicketService 同构——移动 SDK 重连/回前台后以 lastMessageId 游标
// 补拉断连期间增量，合并本地后按 agent/system 来源累加未读。
package delivery

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	// canonical 包名 import（非别名）：本文件定义的 VisitorMessagesPage 被
	// handlers 层 swagger 注解引用，swag 按 canonical 名解析嵌套字段类型，
	// 别名引用（conversationapp.X）会报 cannot find type definition。
	"servify/apps/server/internal/modules/conversation/application"
)

// VisitorMessagesService 是访客消息增量拉取的窄契约（§10 #1）。
type VisitorMessagesService interface {
	ListMessagesAfter(ctx context.Context, sessionID, afterMessageID string, limit int) (*VisitorMessagesPage, error)
}

// VisitorMessagesPage 是增量拉取的单页：升序消息 + 是否可能还有更多
// （has_more 为 true 时客户端以末条消息 id 续拉）。
type VisitorMessagesPage struct {
	Messages []application.ConversationMessageDTO
	HasMore  bool
}

// VisitorMessagesAdapter 桥接 handler 与会话服务：会话必须已存在（行在
// 首条消息持久化时建——PersistTextMessage 的 Resume 未命中即 Create，
// 而非 WS 握手时），存在性校验把「会话失效」与「没有新消息」区分开
// （404 vs 200 空页）；has_more 以 limit+1 探测。
type VisitorMessagesAdapter struct {
	service *application.Service
	db      *gorm.DB
}

// NewVisitorMessagesAdapter 创建访客消息适配器。
func NewVisitorMessagesAdapter(service *application.Service, db *gorm.DB) *VisitorMessagesAdapter {
	return &VisitorMessagesAdapter{service: service, db: db}
}

// ListMessagesAfter 拉取 session 内晚于游标的消息（升序）。afterMessageID
// 为空表示从会话头拉全量；非数字游标契约性拒绝（游标只能来自本端点或
// WS 消息 id，客户端不需要自己构造）。
func (a *VisitorMessagesAdapter) ListMessagesAfter(ctx context.Context, sessionID, afterMessageID string, limit int) (*VisitorMessagesPage, error) {
	var session models.Session
	if err := a.db.WithContext(ctx).First(&session, "id = ?", sessionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		return nil, fmt.Errorf("session lookup failed: %w", err)
	}

	// limit+1 探测是否有下一页；本层截断保证返回面 ≤ limit。
	items, err := a.service.ListMessagesAfter(ctx, sessionID, afterMessageID, limit+1)
	if err != nil {
		return nil, err
	}
	hasMore := false
	if len(items) > limit {
		hasMore = true
		items = items[:limit]
	}
	return &VisitorMessagesPage{Messages: items, HasMore: hasMore}, nil
}
