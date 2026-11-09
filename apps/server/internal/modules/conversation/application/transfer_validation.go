package application

import (
	"context"
	"errors"
)

// Transfer 目标校验哨兵：交付层据此映射明确结果（404/403/409），避免把
// 无效目标退化成通用 500。errors.Is 比较。
var (
	// ErrTransferTargetNotFound 目标坐席不存在。
	ErrTransferTargetNotFound = errors.New("transfer target agent not found")
	// ErrTransferTargetOutOfScope 目标坐席与会话不在同一租户/工作区。
	ErrTransferTargetOutOfScope = errors.New("transfer target agent is outside the conversation scope")
	// ErrTransferTargetUnavailable 目标坐席处于不可接状态（offline）。
	ErrTransferTargetUnavailable = errors.New("transfer target agent is not available")
	// ErrConcurrentTransfer 并发转接冲突：会话的当前坐席已被并发请求改变，
	// 本次转接的预期前置状态不再成立。
	ErrConcurrentTransfer = errors.New("concurrent transfer conflict: conversation changed")
)

// TransferTargetValidator 校验转接目标坐席（会话分配前置条件）：目标必须
// 存在、属于给定租户/工作区作用域、且处于可接状态。实现方在会话分配更新
// 前调用；失败返回带哨兵语义的错误。
type TransferTargetValidator interface {
	ValidateTransferTarget(ctx context.Context, agentID uint, tenantID, workspaceID string) error
}
