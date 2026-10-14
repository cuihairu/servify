package application

import "errors"

// SLA 模块哨兵错误：文案与原服务内 inline fmt.Errorf 逐字一致（handlers 按
// 字符串映射，改文案会破坏 HTTP 语义）。不存在统一返回应用哨兵而非裸 SQL 错误。
var (
	ErrSLAConfigNotFound    = errors.New("SLA config not found")
	ErrSLAViolationNotFound = errors.New("SLA violation not found")
	ErrTicketNotFound       = errors.New("ticket not found")
)
