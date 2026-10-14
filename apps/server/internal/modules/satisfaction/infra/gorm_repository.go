package infra

import (
	"context"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	satisfapp "servify/apps/server/internal/modules/satisfaction/application"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

// GormRepository 满意度持久化（pg/sqlite 双轨）。
// 读取面按 ctx 的租户/工作区过滤（与 assist 等模块的 applyScopeFilter 同口径）；
// not found 语义统一映射到 application 哨兵错误，跨 scope 命中与不存在同返。
type GormRepository struct {
	db *gorm.DB
}

// compile-time 端口契约校验。
var _ satisfapp.Repository = (*GormRepository)(nil)

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func tenantAndWorkspace(ctx context.Context) (string, string) {
	return platformauth.TenantIDFromContext(ctx), platformauth.WorkspaceIDFromContext(ctx)
}

func applyScopeFilter(tx *gorm.DB, ctx context.Context) *gorm.DB {
	tenantID, workspaceID := tenantAndWorkspace(ctx)
	if tenantID != "" {
		tx = tx.Where("tenant_id = ?", tenantID)
	}
	if workspaceID != "" {
		tx = tx.Where("workspace_id = ?", workspaceID)
	}
	return tx
}

func scopeAwareSatisfactionPreloads(db *gorm.DB, ctx context.Context) *gorm.DB {
	query := db.WithContext(ctx).
		Preload("Ticket", func(tx *gorm.DB) *gorm.DB {
			return applyScopeFilter(tx, ctx)
		}).
		Preload("Customer", func(tx *gorm.DB) *gorm.DB {
			return applyScopeFilter(tx, ctx)
		})

	return scopeAwareTicketAgentPreload(query, ctx)
}

func scopeAwareTicketAgentPreload(db *gorm.DB, ctx context.Context) *gorm.DB {
	tenantID, workspaceID := tenantAndWorkspace(ctx)
	return db.Preload("Agent", func(tx *gorm.DB) *gorm.DB {
		if tenantID == "" && workspaceID == "" {
			return tx
		}
		tx = tx.Joins("JOIN agents ON agents.user_id = users.id")
		if tenantID != "" {
			tx = tx.Where("agents.tenant_id = ?", tenantID)
		}
		if workspaceID != "" {
			tx = tx.Where("agents.workspace_id = ?", workspaceID)
		}
		return tx
	})
}

func (r *GormRepository) GetTicketScoped(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	var ticket models.Ticket
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).First(&ticket, ticketID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, satisfapp.ErrTicketNotFound
		}
		return nil, err
	}
	return &ticket, nil
}

func (r *GormRepository) GetTicketWithAgentScoped(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	var ticket models.Ticket
	if err := applyScopeFilter(scopeAwareTicketAgentPreload(r.db.WithContext(ctx), ctx), ctx).First(&ticket, ticketID).Error; err != nil {
		return nil, err
	}
	return &ticket, nil
}

func (r *GormRepository) GetCustomerScopedByUserID(ctx context.Context, userID uint) (*models.Customer, error) {
	var customer models.Customer
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).Where("user_id = ?", userID).First(&customer).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, satisfapp.ErrCustomerNotFound
		}
		return nil, err
	}
	return &customer, nil
}

func (r *GormRepository) GetAgentScopedByUserID(ctx context.Context, userID uint) (*models.Agent, error) {
	var agent models.Agent
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).Where("user_id = ?", userID).First(&agent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, satisfapp.ErrAgentNotFound
		}
		return nil, err
	}
	return &agent, nil
}

func (r *GormRepository) GetUserEmail(ctx context.Context, userID uint) (string, bool) {
	var user models.User
	if err := r.db.WithContext(ctx).Select("id", "email").First(&user, userID).Error; err != nil {
		return "", false
	}
	email := strings.TrimSpace(user.Email)
	if email == "" {
		return "", false
	}
	return email, true
}

func (r *GormRepository) CountSatisfactionsByTicket(ctx context.Context, ticketID uint) (int64, error) {
	var count int64
	if err := applyScopeFilter(r.db.WithContext(ctx).
		Model(&models.CustomerSatisfaction{}), ctx).
		Where("ticket_id = ?", ticketID).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *GormRepository) FindSatisfactionByTicketAndCustomer(ctx context.Context, ticketID, customerID uint) (*models.CustomerSatisfaction, error) {
	var satisfaction models.CustomerSatisfaction
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).
		Where("ticket_id = ? AND customer_id = ?", ticketID, customerID).
		First(&satisfaction).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &satisfaction, nil
}

func (r *GormRepository) FindActiveSurveyByTicket(ctx context.Context, ticketID uint) (*models.SatisfactionSurvey, error) {
	var survey models.SatisfactionSurvey
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).
		Where("ticket_id = ? AND status IN ?", ticketID, []string{"queued", "sent"}).
		Order("created_at DESC").
		First(&survey).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &survey, nil
}

func (r *GormRepository) CreateSurvey(ctx context.Context, survey *models.SatisfactionSurvey) error {
	return r.db.WithContext(ctx).Create(survey).Error
}

func (r *GormRepository) SaveSurvey(ctx context.Context, survey *models.SatisfactionSurvey) error {
	return r.db.WithContext(ctx).Save(survey).Error
}

func (r *GormRepository) ListSurveys(ctx context.Context, req *satisfapp.SatisfactionSurveyListRequest) ([]models.SatisfactionSurvey, int64, error) {
	query := applyScopeFilter(r.db.WithContext(ctx).Model(&models.SatisfactionSurvey{}), ctx)

	if req.TicketID != nil {
		query = query.Where("ticket_id = ?", *req.TicketID)
	}
	if req.CustomerID != nil {
		query = query.Where("customer_id = ?", *req.CustomerID)
	}
	if len(req.Status) > 0 {
		query = query.Where("status IN ?", req.Status)
	}
	if len(req.Channel) > 0 {
		query = query.Where("channel IN ?", req.Channel)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if req.PageSize > 0 {
		offset := (req.Page - 1) * req.PageSize
		query = query.Offset(offset).Limit(req.PageSize)
	}

	var surveys []models.SatisfactionSurvey
	if err := query.Order("created_at DESC").Find(&surveys).Error; err != nil {
		return nil, 0, err
	}
	return surveys, total, nil
}

func (r *GormRepository) GetSurveyByToken(ctx context.Context, token string) (*models.SatisfactionSurvey, error) {
	var survey models.SatisfactionSurvey
	if err := r.db.WithContext(ctx).Where("survey_token = ?", token).First(&survey).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, satisfapp.ErrSurveyNotFound
		}
		return nil, err
	}
	return &survey, nil
}

func (r *GormRepository) GetSurveyScoped(ctx context.Context, id uint) (*models.SatisfactionSurvey, error) {
	var survey models.SatisfactionSurvey
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).First(&survey, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, satisfapp.ErrSurveyNotFound
		}
		return nil, err
	}
	return &survey, nil
}

func (r *GormRepository) ExpireSurvey(ctx context.Context, surveyID uint) error {
	return applyScopeFilter(r.db.WithContext(ctx).Model(&models.SatisfactionSurvey{}), ctx).
		Where("id = ?", surveyID).
		Update("status", "expired").Error
}

func (r *GormRepository) CompleteSurvey(ctx context.Context, surveyID uint, completedAt time.Time, satisfactionID uint) error {
	return applyScopeFilter(r.db.WithContext(ctx).
		Model(&models.SatisfactionSurvey{}), ctx).
		Where("id = ?", surveyID).
		Updates(map[string]interface{}{
			"status":          "completed",
			"completed_at":    completedAt,
			"satisfaction_id": satisfactionID,
		}).Error
}

func (r *GormRepository) ExpireOverdueQueuedSurveys(ctx context.Context, now time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Model(&models.SatisfactionSurvey{}).
		Where("status = ? AND channel = ? AND expires_at IS NOT NULL AND expires_at < ?", "queued", "email", now).
		Update("status", "failed")
	return result.RowsAffected, result.Error
}

func (r *GormRepository) ListPendingEmailSurveys(ctx context.Context, now time.Time, batchSize int) ([]models.SatisfactionSurvey, error) {
	var surveys []models.SatisfactionSurvey
	if err := r.db.WithContext(ctx).
		Where("status = ? AND channel = ? AND (expires_at IS NULL OR expires_at >= ?)", "queued", "email", now).
		Order("created_at ASC").
		Limit(batchSize).
		Find(&surveys).Error; err != nil {
		return nil, err
	}
	return surveys, nil
}

func (r *GormRepository) MarkSurveyFailedIfQueued(ctx context.Context, surveyID uint) error {
	return r.db.WithContext(ctx).Model(&models.SatisfactionSurvey{}).
		Where("id = ? AND status = ?", surveyID, "queued").
		Update("status", "failed").Error
}

func (r *GormRepository) MarkSurveySentIfQueued(ctx context.Context, surveyID uint, sentAt time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Model(&models.SatisfactionSurvey{}).
		Where("id = ? AND status = ?", surveyID, "queued").
		Updates(map[string]interface{}{
			"status":  "sent",
			"sent_at": sentAt,
		})
	return result.RowsAffected, result.Error
}

func (r *GormRepository) CreateSatisfaction(ctx context.Context, satisfaction *models.CustomerSatisfaction) error {
	return r.db.Create(satisfaction).Error
}

func (r *GormRepository) GetSatisfaction(ctx context.Context, id uint) (*models.CustomerSatisfaction, error) {
	var satisfaction models.CustomerSatisfaction
	if err := applyScopeFilter(scopeAwareSatisfactionPreloads(r.db, ctx), ctx).First(&satisfaction, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, satisfapp.ErrSatisfactionNotFound
		}
		return nil, err
	}
	return &satisfaction, nil
}

func (r *GormRepository) FindSatisfactionScoped(ctx context.Context, id uint) (*models.CustomerSatisfaction, error) {
	var satisfaction models.CustomerSatisfaction
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).First(&satisfaction, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, satisfapp.ErrSatisfactionNotFound
		}
		return nil, err
	}
	return &satisfaction, nil
}

func (r *GormRepository) GetSatisfactionByTicket(ctx context.Context, ticketID uint) (*models.CustomerSatisfaction, error) {
	var satisfaction models.CustomerSatisfaction
	if err := applyScopeFilter(scopeAwareSatisfactionPreloads(r.db.WithContext(ctx).Where("ticket_id = ?", ticketID), ctx), ctx).First(&satisfaction).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &satisfaction, nil
}

func (r *GormRepository) ListSatisfactions(ctx context.Context, req *satisfapp.SatisfactionListRequest) ([]models.CustomerSatisfaction, int64, error) {
	query := applyScopeFilter(r.db.WithContext(ctx).Model(&models.CustomerSatisfaction{}), ctx)

	if req.TicketID != nil {
		query = query.Where("ticket_id = ?", *req.TicketID)
	}
	if req.CustomerID != nil {
		query = query.Where("customer_id = ?", *req.CustomerID)
	}
	if req.AgentID != nil {
		query = query.Where("agent_id = ?", *req.AgentID)
	}
	if len(req.Rating) > 0 {
		query = query.Where("rating IN ?", req.Rating)
	}
	if len(req.Category) > 0 {
		query = query.Where("category IN ?", req.Category)
	}
	if req.DateFrom != nil {
		query = query.Where("created_at >= ?", *req.DateFrom)
	}
	if req.DateTo != nil {
		query = query.Where("created_at <= ?", *req.DateTo)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	sortField := req.SortBy
	if sortField == "" {
		sortField = "created_at"
	}
	sortOrder := req.SortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}
	query = query.Order(sortField + " " + sortOrder)

	if req.PageSize > 0 {
		offset := (req.Page - 1) * req.PageSize
		query = query.Offset(offset).Limit(req.PageSize)
	}

	var satisfactions []models.CustomerSatisfaction
	if err := scopeAwareSatisfactionPreloads(query, ctx).Find(&satisfactions).Error; err != nil {
		return nil, 0, err
	}
	return satisfactions, total, nil
}

func (r *GormRepository) DeleteSatisfaction(ctx context.Context, id uint) (int64, error) {
	result := applyScopeFilter(r.db.WithContext(ctx), ctx).Delete(&models.CustomerSatisfaction{}, id)
	return result.RowsAffected, result.Error
}

func (r *GormRepository) SaveSatisfaction(ctx context.Context, satisfaction *models.CustomerSatisfaction) error {
	return r.db.Save(satisfaction).Error
}

func (r *GormRepository) CountSatisfactions(ctx context.Context, dateFrom, dateTo *time.Time) (int64, error) {
	query := applyScopeFilter(r.db.WithContext(ctx).Model(&models.CustomerSatisfaction{}), ctx)
	if dateFrom != nil {
		query = query.Where("created_at >= ?", *dateFrom)
	}
	if dateTo != nil {
		query = query.Where("created_at <= ?", *dateTo)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func (r *GormRepository) AverageSatisfactionRating(ctx context.Context, dateFrom, dateTo *time.Time) (float64, error) {
	query := applyScopeFilter(r.db.WithContext(ctx).Model(&models.CustomerSatisfaction{}), ctx)
	if dateFrom != nil {
		query = query.Where("created_at >= ?", *dateFrom)
	}
	if dateTo != nil {
		query = query.Where("created_at <= ?", *dateTo)
	}
	var avgResult struct {
		Average float64
	}
	if err := query.Select("AVG(rating) as average").Scan(&avgResult).Error; err != nil {
		return 0, err
	}
	return avgResult.Average, nil
}

func (r *GormRepository) SatisfactionRatingDistribution(ctx context.Context, dateFrom, dateTo *time.Time) (map[int]int, error) {
	query := applyScopeFilter(r.db.WithContext(ctx).Model(&models.CustomerSatisfaction{}), ctx)
	if dateFrom != nil {
		query = query.Where("created_at >= ?", *dateFrom)
	}
	if dateTo != nil {
		query = query.Where("created_at <= ?", *dateTo)
	}
	var rows []struct {
		Rating int
		Count  int
	}
	if err := query.Select("rating, COUNT(*) as count").Group("rating").Scan(&rows).Error; err != nil {
		return nil, err
	}
	distribution := make(map[int]int)
	for _, item := range rows {
		distribution[item.Rating] = item.Count
	}
	return distribution, nil
}

func (r *GormRepository) SatisfactionCategoryStats(ctx context.Context, dateFrom, dateTo *time.Time) (map[string]satisfapp.SatisfactionStat, error) {
	query := applyScopeFilter(r.db.WithContext(ctx).Model(&models.CustomerSatisfaction{}), ctx)
	if dateFrom != nil {
		query = query.Where("created_at >= ?", *dateFrom)
	}
	if dateTo != nil {
		query = query.Where("created_at <= ?", *dateTo)
	}
	var rows []struct {
		Category string
		Count    int
		Average  float64
	}
	if err := query.Select("category, COUNT(*) as count, AVG(rating) as average").Group("category").Scan(&rows).Error; err != nil {
		return nil, err
	}
	stats := make(map[string]satisfapp.SatisfactionStat)
	for _, item := range rows {
		stats[item.Category] = satisfapp.SatisfactionStat{
			Count:         item.Count,
			AverageRating: item.Average,
		}
	}
	return stats, nil
}

func (r *GormRepository) SatisfactionTrend(ctx context.Context, dateFrom, dateTo *time.Time) ([]satisfapp.SatisfactionTrend, error) {
	trendQuery := applyScopeFilter(r.db.WithContext(ctx).Model(&models.CustomerSatisfaction{}), ctx).
		Select("DATE(created_at) as date, COUNT(*) as count, AVG(rating) as average").
		Group("date").
		Order("date")
	if dateFrom != nil {
		trendQuery = trendQuery.Where("created_at >= ?", *dateFrom)
	}
	if dateTo != nil {
		trendQuery = trendQuery.Where("created_at <= ?", *dateTo)
	}
	var rows []struct {
		Date    string
		Count   int
		Average float64
	}
	if err := trendQuery.Scan(&rows).Error; err != nil {
		return nil, err
	}
	trend := make([]satisfapp.SatisfactionTrend, 0, len(rows))
	for _, item := range rows {
		trend = append(trend, satisfapp.SatisfactionTrend{
			Date:          item.Date,
			Count:         item.Count,
			AverageRating: item.Average,
		})
	}
	return trend, nil
}
