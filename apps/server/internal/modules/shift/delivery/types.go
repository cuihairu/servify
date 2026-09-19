package delivery

// types.go re-export 应用层类型：handler 只 import delivery 一包。

import (
	shiftapp "servify/apps/server/internal/modules/shift/application"
)

type (
	// ShiftCreateRequest 创建班次请求
	ShiftCreateRequest = shiftapp.ShiftCreateRequest
	// ShiftListRequest 班次列表请求
	ShiftListRequest = shiftapp.ShiftListRequest
	// ShiftUpdateRequest 更新班次请求
	ShiftUpdateRequest = shiftapp.ShiftUpdateRequest
	// ShiftStatsResponse 班次统计响应
	ShiftStatsResponse = shiftapp.ShiftStatsResponse
)

// 错误转发（保持 services 原文案，handler 按文本匹配状态码）。
var (
	// ErrShiftNotFound 班次不存在
	ErrShiftNotFound = shiftapp.ErrShiftNotFound
	// ErrAgentNotFound 代理不存在
	ErrAgentNotFound = shiftapp.ErrAgentNotFound
	// ErrInvalidTimeRange 结束时间必须晚于开始时间
	ErrInvalidTimeRange = shiftapp.ErrInvalidTimeRange
)
