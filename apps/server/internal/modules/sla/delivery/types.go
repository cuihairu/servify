// Package delivery 转发 application 层的请求/响应类型，
// 供 handlers 与装配层按稳定契约引用。
package delivery

import "servify/apps/server/internal/modules/sla/application"

// 请求/响应类型别名（真实 struct 定义在 application 包）。
type (
	SLAConfigCreateRequest  = application.SLAConfigCreateRequest
	SLAConfigUpdateRequest  = application.SLAConfigUpdateRequest
	SLAConfigListRequest    = application.SLAConfigListRequest
	SLAViolationListRequest = application.SLAViolationListRequest
	SLAStatsResponse        = application.SLAStatsResponse
	SLAComplianceTrend      = application.SLAComplianceTrend
)
