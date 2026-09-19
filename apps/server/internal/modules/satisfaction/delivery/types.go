// Package delivery 转发 application 层的请求/响应类型与哨兵错误，
// 供 handlers 与装配层按稳定契约引用。
package delivery

import "servify/apps/server/internal/modules/satisfaction/application"

// 请求/响应类型别名（真实 struct 定义在 application 包）。
type (
	SatisfactionCreateRequest     = application.SatisfactionCreateRequest
	SatisfactionListRequest       = application.SatisfactionListRequest
	SatisfactionStatsResponse     = application.SatisfactionStatsResponse
	SatisfactionStat              = application.SatisfactionStat
	SatisfactionTrend             = application.SatisfactionTrend
	SatisfactionSurveyListRequest = application.SatisfactionSurveyListRequest
	SatisfactionSurveyPreview     = application.SatisfactionSurveyPreview
	SurveyMailer                  = application.SurveyMailer
)

// 哨兵错误转发。
var (
	ErrSurveyNotFound  = application.ErrSurveyNotFound
	ErrSurveyExpired   = application.ErrSurveyExpired
	ErrSurveyCompleted = application.ErrSurveyCompleted
)
