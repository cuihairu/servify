package infra

import (
	"context"
	"time"

	aiapp "servify/apps/server/internal/modules/ai/application"
	"servify/apps/server/internal/modules/ai/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

// V1.0 收敛 B3-1b：ai_answers / answer_feedback 的 GORM 仓储。与 knowledge
// 仓储同款 scope 过滤（tenant/workspace 经 platformauth 上下文推导）。

type GormAnswerRepository struct {
	db *gorm.DB
	// analytics 聚合读口（同一 db）：GormAnswerRepository 同时实现
	// aiapp.AnswerAnalyticsRepository，装配侧传一个仓储即可（服务层按接口
	// 断言取聚合能力，缺位时降级空聚合）。
	analytics *GormAnswerAnalyticsRepository
}

func NewGormAnswerRepository(db *gorm.DB) *GormAnswerRepository {
	return &GormAnswerRepository{db: db, analytics: NewGormAnswerAnalyticsRepository(db)}
}

// CountInWindow 窗口聚合（委托 analytics）。
func (r *GormAnswerRepository) CountInWindow(ctx context.Context, since time.Time) (int64, int64, int64, float64, error) {
	return r.analytics.CountInWindow(ctx, since)
}

// TopQuestions 问题榜（委托 analytics）。
func (r *GormAnswerRepository) TopQuestions(ctx context.Context, since time.Time, limit int, hitOnly bool) ([]aiapp.QuestionStat, error) {
	return r.analytics.TopQuestions(ctx, since, limit, hitOnly)
}

// LowConfidenceQuestions 低置信问题榜（委托 analytics）。
func (r *GormAnswerRepository) LowConfidenceQuestions(ctx context.Context, since time.Time, limit int) ([]aiapp.QuestionStat, error) {
	return r.analytics.LowConfidenceQuestions(ctx, since, limit)
}

// CountFeedback 反馈计数（委托 analytics）。
func (r *GormAnswerRepository) CountFeedback(ctx context.Context, since time.Time) (int64, int64, error) {
	return r.analytics.CountFeedback(ctx, since)
}

// Create 落一条首答（scope 取自请求上下文）。
func (r *GormAnswerRepository) Create(ctx context.Context, answer *domain.AIAnswer) error {
	if answer == nil {
		return gorm.ErrInvalidValue
	}
	model := *answer
	model.TenantID = platformauth.TenantIDFromContext(ctx)
	model.WorkspaceID = platformauth.WorkspaceIDFromContext(ctx)
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return err
	}
	*answer = model
	return nil
}

// Get 按主键读（scope 过滤：跨租户不可见）。
func (r *GormAnswerRepository) Get(ctx context.Context, id uint) (*domain.AIAnswer, error) {
	var model domain.AIAnswer
	if err := applyAIScope(r.db.WithContext(ctx), ctx).First(&model, id).Error; err != nil {
		return nil, err
	}
	return &model, nil
}

type GormAnswerFeedbackRepository struct {
	db *gorm.DB
}

func NewGormAnswerFeedbackRepository(db *gorm.DB) *GormAnswerFeedbackRepository {
	return &GormAnswerFeedbackRepository{db: db}
}

func (r *GormAnswerFeedbackRepository) Create(ctx context.Context, feedback *domain.AnswerFeedback) error {
	if feedback == nil {
		return gorm.ErrInvalidValue
	}
	model := *feedback
	if model.TenantID == "" {
		model.TenantID = platformauth.TenantIDFromContext(ctx)
	}
	if model.WorkspaceID == "" {
		model.WorkspaceID = platformauth.WorkspaceIDFromContext(ctx)
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return err
	}
	*feedback = model
	return nil
}

type GormAnswerAnalyticsRepository struct {
	db *gorm.DB
}

func NewGormAnswerAnalyticsRepository(db *gorm.DB) *GormAnswerAnalyticsRepository {
	return &GormAnswerAnalyticsRepository{db: db}
}

// CountInWindow 窗口内总数 / 带引用命中数 / 低置信数 / 置信合计。
// 注意：GORM 链式 Where 会累积到同一 statement，四个聚合各自从 scoped 基
// 查询重建，条件互不串染。
func (r *GormAnswerAnalyticsRepository) CountInWindow(ctx context.Context, since time.Time) (int64, int64, int64, float64, error) {
	scoped := func() *gorm.DB {
		return applyAIScope(r.db.WithContext(ctx).Model(&domain.AIAnswer{}), ctx).Where("created_at >= ?", since)
	}
	var total, hit, lowConf int64
	var confSum float64
	if err := scoped().Count(&total).Error; err != nil {
		return 0, 0, 0, 0, err
	}
	if err := scoped().Where("sources_json <> ''").Count(&hit).Error; err != nil {
		return 0, 0, 0, 0, err
	}
	if err := scoped().Where("confidence < ?", aiapp.LowConfidenceThreshold).Count(&lowConf).Error; err != nil {
		return 0, 0, 0, 0, err
	}
	if total > 0 {
		if err := scoped().Select("COALESCE(SUM(confidence), 0)").Scan(&confSum).Error; err != nil {
			return 0, 0, 0, 0, err
		}
	}
	return total, hit, lowConf, confSum, nil
}

// TopQuestions 窗口内按出现次数排的问题榜：hitOnly=true 只统计带引用命中的
// 提问（top 问答），false 只统计零命中的提问（无命中率——知识库缺口信号）。
func (r *GormAnswerAnalyticsRepository) TopQuestions(ctx context.Context, since time.Time, limit int, hitOnly bool) ([]aiapp.QuestionStat, error) {
	q := applyAIScope(r.db.WithContext(ctx).Model(&domain.AIAnswer{}), ctx).
		Where("created_at >= ?", since).
		Select("query, COUNT(*) AS count, AVG(confidence) AS avg_confidence").
		Group("query").
		Order("count DESC").
		Limit(limit)
	if hitOnly {
		q = q.Having("sources_json <> ''")
	} else {
		q = q.Where("sources_json = ''")
	}
	var rows []aiapp.QuestionStat
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// LowConfidenceQuestions 窗口内平均置信低于阈值的问题榜（置信升序）。
func (r *GormAnswerAnalyticsRepository) LowConfidenceQuestions(ctx context.Context, since time.Time, limit int) ([]aiapp.QuestionStat, error) {
	var rows []aiapp.QuestionStat
	err := applyAIScope(r.db.WithContext(ctx).Model(&domain.AIAnswer{}), ctx).
		Where("created_at >= ? AND confidence < ?", since, aiapp.LowConfidenceThreshold).
		Select("query, COUNT(*) AS count, AVG(confidence) AS avg_confidence").
		Group("query").
		Order("avg_confidence ASC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// CountFeedback 窗口内反馈计数（有帮助 / 没帮助）。两次计数各自从 scoped
// 基查询重建（GORM 链式 Where 累积，同 CountInWindow）。
func (r *GormAnswerAnalyticsRepository) CountFeedback(ctx context.Context, since time.Time) (int64, int64, error) {
	scoped := func() *gorm.DB {
		return applyAIScope(r.db.WithContext(ctx).Model(&domain.AnswerFeedback{}), ctx).Where("created_at >= ?", since)
	}
	var helpful, notHelpful int64
	if err := scoped().Where("helpful = ?", true).Count(&helpful).Error; err != nil {
		return 0, 0, err
	}
	if err := scoped().Where("helpful = ?", false).Count(&notHelpful).Error; err != nil {
		return 0, 0, err
	}
	return helpful, notHelpful, nil
}

func applyAIScope(tx *gorm.DB, ctx context.Context) *gorm.DB {
	tenantID := platformauth.TenantIDFromContext(ctx)
	workspaceID := platformauth.WorkspaceIDFromContext(ctx)
	if tenantID != "" {
		tx = tx.Where("tenant_id = ?", tenantID)
	}
	if workspaceID != "" {
		tx = tx.Where("workspace_id = ?", workspaceID)
	}
	return tx
}

// ensureGormAnswerRepositories 编译期接口核对。
var (
	_ aiapp.AnswerRepository          = (*GormAnswerRepository)(nil)
	_ aiapp.AnswerFeedbackRepository  = (*GormAnswerFeedbackRepository)(nil)
	_ aiapp.AnswerAnalyticsRepository = (*GormAnswerAnalyticsRepository)(nil)
)
