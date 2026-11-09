package delivery

import (
	"context"
	"errors"
	"net/http"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
)

var ErrConversationNotFound = errors.New("conversation not found")

// Re-export DTO types so handlers do not need to import the application layer directly.
type ConversationDTO = conversationapp.ConversationDTO
type ConversationMessageDTO = conversationapp.ConversationMessageDTO

// CloseOptions / ErrOpenTicketsRemain 供 handlers 消费关闭拦截面（模块边界：
// handlers 只 import delivery，不 import application；errors.Is 对 sentinel
// 语义经别名保持不变）。
type CloseOptions = conversationapp.CloseOptions

var ErrOpenTicketsRemain = conversationapp.ErrOpenTicketsRemain

// Transfer 错误面 sentinel 再导出（模块边界同上：handlers 经 delivery 消费）。
var (
	ErrTransferTargetNotFound    = conversationapp.ErrTransferTargetNotFound
	ErrTransferTargetOutOfScope  = conversationapp.ErrTransferTargetOutOfScope
	ErrTransferTargetUnavailable = conversationapp.ErrTransferTargetUnavailable
	ErrConcurrentTransfer        = conversationapp.ErrConcurrentTransfer
)

// TransferHTTPStatus 转接失败面到 HTTP 状态的统一映射：目标不存在/会话不存在
// 404、跨作用域 403、不可接/并发抢占 409，其余 500——失败必须有明确结果而非
// 笼统错误。nil 错误不在契约内（调用方只在 err != nil 分支映射）。
func TransferHTTPStatus(err error) int {
	switch {
	case errors.Is(err, ErrConversationNotFound),
		errors.Is(err, ErrTransferTargetNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrTransferTargetOutOfScope):
		return http.StatusForbidden
	case errors.Is(err, ErrTransferTargetUnavailable),
		errors.Is(err, ErrConcurrentTransfer):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

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
