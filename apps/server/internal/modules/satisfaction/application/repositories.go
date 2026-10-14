package application

import (
	"context"
	"time"

	"servify/apps/server/internal/models"
)

// Repository 满意度持久化端口（infra/gorm 实现）。
// 读取类方法按 ctx 携带的租户/工作区过滤（scope 为空 = 不过滤，与 assist 等
// 模块同口径）；跨 scope 命中与不存在同返模块哨兵错误（404 语义，不回显存在性）。
type Repository interface {
	// GetTicketScoped 按 scope 载入工单；不存在/跨 scope 返回 ErrTicketNotFound。
	GetTicketScoped(ctx context.Context, ticketID uint) (*models.Ticket, error)
	// GetTicketWithAgentScoped 按 scope 载入工单（含 agent 关联）；错误原样返回。
	GetTicketWithAgentScoped(ctx context.Context, ticketID uint) (*models.Ticket, error)
	// GetCustomerScopedByUserID 按 scope 载入客户档案；不存在/跨 scope 返回 ErrCustomerNotFound。
	GetCustomerScopedByUserID(ctx context.Context, userID uint) (*models.Customer, error)
	// GetAgentScopedByUserID 按 scope 校验客服；不存在/跨 scope 返回 ErrAgentNotFound。
	GetAgentScopedByUserID(ctx context.Context, userID uint) (*models.Agent, error)
	// GetUserEmail 返回用户邮箱（trim 后空串或不存在视为不可投递）。
	GetUserEmail(ctx context.Context, userID uint) (string, bool)

	// CountSatisfactionsByTicket 统计某工单已有评价数（scope 过滤）。
	CountSatisfactionsByTicket(ctx context.Context, ticketID uint) (int64, error)
	// FindSatisfactionByTicketAndCustomer 返回已存在的评价；无则 (nil, nil)。
	FindSatisfactionByTicketAndCustomer(ctx context.Context, ticketID, customerID uint) (*models.CustomerSatisfaction, error)

	// FindActiveSurveyByTicket 返回工单最近的 queued/sent 调查；无则 (nil, nil)。
	FindActiveSurveyByTicket(ctx context.Context, ticketID uint) (*models.SatisfactionSurvey, error)
	// CreateSurvey 新建调查记录。
	CreateSurvey(ctx context.Context, survey *models.SatisfactionSurvey) error
	// SaveSurvey 全量保存调查（resend 等业务态改写）。
	SaveSurvey(ctx context.Context, survey *models.SatisfactionSurvey) error

	// ListSurveys 按过滤条件分页列出调查；返回页数据与总数。
	ListSurveys(ctx context.Context, req *SatisfactionSurveyListRequest) ([]models.SatisfactionSurvey, int64, error)
	// GetSurveyByToken 按 token 载入调查；不存在返回 ErrSurveyNotFound。
	GetSurveyByToken(ctx context.Context, token string) (*models.SatisfactionSurvey, error)
	// GetSurveyScoped 按 scope 载入调查；不存在/跨 scope 返回 ErrSurveyNotFound。
	GetSurveyScoped(ctx context.Context, id uint) (*models.SatisfactionSurvey, error)
	// ExpireSurvey 把调查置为 expired（best-effort，调用方可忽略错误）。
	ExpireSurvey(ctx context.Context, surveyID uint) error
	// CompleteSurvey 标记调查 completed 并回填 satisfaction_id（survey_id 关联列）。
	CompleteSurvey(ctx context.Context, surveyID uint, completedAt time.Time, satisfactionID uint) error

	// ExpireOverdueQueuedSurveys 把过期的 queued email 调查置 failed；返回受影响行数。
	ExpireOverdueQueuedSurveys(ctx context.Context, now time.Time) (int64, error)
	// ListPendingEmailSurveys 返回未过期 queued email 调查（created_at 升序，限 batchSize）。
	ListPendingEmailSurveys(ctx context.Context, now time.Time, batchSize int) ([]models.SatisfactionSurvey, error)
	// MarkSurveyFailedIfQueued 缺收件人等永久失败的 CAS 置 failed（queued→failed）。
	MarkSurveyFailedIfQueued(ctx context.Context, surveyID uint) error
	// MarkSurveySentIfQueued 投递成功 CAS 置 sent（queued→sent）；返回受影响行数（0 = 已被并发改写）。
	MarkSurveySentIfQueued(ctx context.Context, surveyID uint, sentAt time.Time) (int64, error)

	// CreateSatisfaction 落库评价记录。
	CreateSatisfaction(ctx context.Context, satisfaction *models.CustomerSatisfaction) error
	// GetSatisfaction 按 scope 载入评价（含预加载关联）；不存在/跨 scope 返回 ErrSatisfactionNotFound。
	GetSatisfaction(ctx context.Context, id uint) (*models.CustomerSatisfaction, error)
	// FindSatisfactionScoped 按 scope 载入评价（不预加载关联）；不存在/跨 scope 返回 ErrSatisfactionNotFound。
	FindSatisfactionScoped(ctx context.Context, id uint) (*models.CustomerSatisfaction, error)
	// GetSatisfactionByTicket 载入工单评价（含预加载关联）；无则 (nil, nil)。
	GetSatisfactionByTicket(ctx context.Context, ticketID uint) (*models.CustomerSatisfaction, error)
	// ListSatisfactions 按过滤条件分页列出评价；返回页数据与总数。
	ListSatisfactions(ctx context.Context, req *SatisfactionListRequest) ([]models.CustomerSatisfaction, int64, error)
	// DeleteSatisfaction 删除评价；返回删除行数。
	DeleteSatisfaction(ctx context.Context, id uint) (int64, error)
	// SaveSatisfaction 全量保存评价（仅允许更新评论列，由调用方保证只改 comment）。
	SaveSatisfaction(ctx context.Context, satisfaction *models.CustomerSatisfaction) error

	// 统计面（scope + 可选日期区间聚合，供 GetSatisfactionStats 组合）。
	CountSatisfactions(ctx context.Context, dateFrom, dateTo *time.Time) (int64, error)
	AverageSatisfactionRating(ctx context.Context, dateFrom, dateTo *time.Time) (float64, error)
	SatisfactionRatingDistribution(ctx context.Context, dateFrom, dateTo *time.Time) (map[int]int, error)
	SatisfactionCategoryStats(ctx context.Context, dateFrom, dateTo *time.Time) (map[string]SatisfactionStat, error)
	SatisfactionTrend(ctx context.Context, dateFrom, dateTo *time.Time) ([]SatisfactionTrend, error)
}
