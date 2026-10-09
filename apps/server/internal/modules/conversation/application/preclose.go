package application

import (
	"context"
	"errors"
)

// OpenTicketChecker 是 ticket 模块查询面的本地窄接口（依赖倒置：conversation
// 只依赖本包接口，组装层注入 ticket/application.QueryService——同 repo/
// publisher 的注入惯例）。SessionID 与 conversation.ID 同命名空间（sessions 表 ID）。
type OpenTicketChecker interface {
	CountOpenBySession(ctx context.Context, sessionID string) (int64, []uint, error)
}

// ErrOpenTicketsRemain：会话仍有未完结工单（open/assigned/in_progress）时
// 关闭被拦截。调用方可 errors.Is 判别；走 AllowOpenTickets=true 显式降级重试。
var ErrOpenTicketsRemain = errors.New("open tickets remain for conversation")
