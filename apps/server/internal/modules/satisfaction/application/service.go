package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// SurveyMailer CSAT 邮件投递的窄接口（由 email 渠道的 SMTP 适配器实现）。
// 未注入时 ScheduleSurvey 维持旧行为：直接置 sent（视为站内送达）。
type SurveyMailer interface {
	SendSurveyEmail(ctx context.Context, to, subject, textBody string) error
}

// SurveyEmailProcessor 是后台扫描投递 queued CSAT 邮件所需的窄接口
// （app/worker 的 SurveyEmailWorker 经 worker deps 引用）。
type SurveyEmailProcessor interface {
	ProcessPendingSurveyEmails(ctx context.Context, batchSize int) (int, error)
}

// SatisfactionService 客户满意度管理服务
type SatisfactionService struct {
	repo   Repository
	logger *logrus.Logger
	// mailer 为 nil 时 CSAT 邮件不投递（queued 分支不生效）
	mailer SurveyMailer
	// surveyLinkBaseURL 是评分页基地址（如 https://support.example.com），空则正文用相对路径
	surveyLinkBaseURL string
}

// NewSatisfactionService 创建满意度服务
func NewService(repo Repository, logger *logrus.Logger) *SatisfactionService {
	if logger == nil {
		logger = logrus.New()
	}

	return &SatisfactionService{
		repo:   repo,
		logger: logger,
	}
}

// SetSurveyMailer 注入邮件投递器（email 渠道启用时由装配层注入）
func (s *SatisfactionService) SetSurveyMailer(mailer SurveyMailer) {
	s.mailer = mailer
}

// SetSurveyLinkBaseURL 设置评分页链接基地址
func (s *SatisfactionService) SetSurveyLinkBaseURL(base string) {
	s.surveyLinkBaseURL = strings.TrimRight(strings.TrimSpace(base), "/")
}

// SatisfactionCreateRequest 创建满意度评价请求
type SatisfactionCreateRequest struct {
	TicketID   uint   `json:"ticket_id" binding:"required"`
	CustomerID uint   `json:"customer_id" binding:"required"`
	AgentID    *uint  `json:"agent_id"`
	Rating     int    `json:"rating" binding:"required,min=1,max=5"`
	Comment    string `json:"comment"`
	Category   string `json:"category"` // service_quality, response_time, resolution_quality, overall
}

// SatisfactionListRequest 满意度评价列表请求
type SatisfactionListRequest struct {
	Page       int        `form:"page,default=1"`
	PageSize   int        `form:"page_size,default=20"`
	TicketID   *uint      `form:"ticket_id"`
	CustomerID *uint      `form:"customer_id"`
	AgentID    *uint      `form:"agent_id"`
	Rating     []int      `form:"rating"`
	Category   []string   `form:"category"`
	DateFrom   *time.Time `form:"date_from"`
	DateTo     *time.Time `form:"date_to"`
	SortBy     string     `form:"sort_by,default=created_at"`
	SortOrder  string     `form:"sort_order,default=desc"`
}

// SatisfactionStatsResponse 满意度统计响应
type SatisfactionStatsResponse struct {
	TotalRatings       int                         `json:"total_ratings"`
	AverageRating      float64                     `json:"average_rating"`
	RatingDistribution map[int]int                 `json:"rating_distribution"` // rating -> count
	CategoryStats      map[string]SatisfactionStat `json:"category_stats"`
	TrendData          []SatisfactionTrend         `json:"trend_data"`
}

// SatisfactionStat 满意度统计
type SatisfactionStat struct {
	Count         int     `json:"count"`
	AverageRating float64 `json:"average_rating"`
}

// SatisfactionTrend 满意度趋势数据
type SatisfactionTrend struct {
	Date          string  `json:"date"`
	Count         int     `json:"count"`
	AverageRating float64 `json:"average_rating"`
}

// SatisfactionSurveyListRequest CSAT 调查分页查询参数
type SatisfactionSurveyListRequest struct {
	Page       int      `form:"page,default=1"`
	PageSize   int      `form:"page_size,default=20"`
	TicketID   *uint    `form:"ticket_id"`
	CustomerID *uint    `form:"customer_id"`
	Status     []string `form:"status"`
	Channel    []string `form:"channel"`
}

// SatisfactionSurveyPreview 提供公共页面展示所需的信息
type SatisfactionSurveyPreview struct {
	TicketID    uint       `json:"ticket_id"`
	TicketTitle string     `json:"ticket_title"`
	AgentName   string     `json:"agent_name"`
	Status      string     `json:"status"`
	Channel     string     `json:"channel"`
	ResolvedAt  *time.Time `json:"resolved_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

const (
	defaultSurveyTTL = 7 * 24 * time.Hour
)

// ScheduleSurvey 在工单关闭后调度并发送满意度调查
func (s *SatisfactionService) ScheduleSurvey(ctx context.Context, ticket *models.Ticket) (*models.SatisfactionSurvey, error) {
	if ticket == nil {
		return nil, fmt.Errorf("ticket required for survey scheduling")
	}
	scopeCtx := contextWithRecordScope(ctx, ticket.TenantID, ticket.WorkspaceID)
	scopedTicket, err := s.repo.GetTicketScoped(ctx, ticket.ID)
	if err != nil {
		if err == ErrTicketNotFound {
			return nil, ErrTicketNotFound
		}
		return nil, fmt.Errorf("failed to validate ticket for survey: %w", err)
	}

	// 如果已有评价则不再发送调查
	satisfactionCount, err := s.repo.CountSatisfactionsByTicket(ctx, scopedTicket.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to check satisfaction status: %w", err)
	}
	if satisfactionCount > 0 {
		return nil, nil
	}

	// 如果存在待发送调查则直接复用
	existing, err := s.repo.FindActiveSurveyByTicket(ctx, scopedTicket.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load existing survey: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	now := time.Now()
	expires := now.Add(defaultSurveyTTL)
	channel := detectSurveyChannel(scopedTicket.Source)

	// email 渠道且已配置投递器 → 入队由后台 worker 真实发送；否则维持直接置 sent
	emailDelivery := channel == "email" && s.mailer != nil
	survey := &models.SatisfactionSurvey{
		TenantID:    platformauth.TenantIDFromContext(scopeCtx),
		WorkspaceID: platformauth.WorkspaceIDFromContext(scopeCtx),
		TicketID:    scopedTicket.ID,
		CustomerID:  scopedTicket.CustomerID,
		AgentID:     scopedTicket.AgentID,
		Channel:     channel,
		Status:      "sent",
		SurveyToken: uuid.NewString(),
		SentAt:      &now,
		ExpiresAt:   &expires,
	}
	if emailDelivery {
		survey.Status = "queued"
		survey.SentAt = nil
	}

	if err := s.repo.CreateSurvey(ctx, survey); err != nil {
		return nil, fmt.Errorf("failed to schedule satisfaction survey: %w", err)
	}

	if emailDelivery {
		s.logger.Infof("Queued CSAT survey email for ticket %d (token=%s)", ticket.ID, survey.SurveyToken)
	} else {
		s.logger.Infof("Scheduled CSAT survey for ticket %d (token=%s)", ticket.ID, survey.SurveyToken)
	}
	return survey, nil
}

// ListSurveys 获取 CSAT 调查列表
func (s *SatisfactionService) ListSurveys(ctx context.Context, req *SatisfactionSurveyListRequest) ([]models.SatisfactionSurvey, int64, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}

	surveys, total, err := s.repo.ListSurveys(ctx, req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list satisfaction surveys: %w", err)
	}
	return surveys, total, nil
}

// GetSurveyPreviewByToken 获取展示用的调查信息
func (s *SatisfactionService) GetSurveyPreviewByToken(ctx context.Context, token string) (*SatisfactionSurveyPreview, error) {
	if token == "" {
		return nil, ErrSurveyNotFound
	}

	survey, err := s.repo.GetSurveyByToken(ctx, token)
	if err != nil {
		if err == ErrSurveyNotFound {
			return nil, ErrSurveyNotFound
		}
		return nil, fmt.Errorf("failed to load survey: %w", err)
	}
	scopeCtx := contextWithRecordScope(ctx, survey.TenantID, survey.WorkspaceID)

	ticket, err := s.repo.GetTicketWithAgentScoped(scopeCtx, survey.TicketID)
	if err != nil {
		return nil, fmt.Errorf("failed to load ticket for survey: %w", err)
	}

	agentName := ""
	if ticket.Agent != nil {
		agentName = ticket.Agent.Name
	}

	return &SatisfactionSurveyPreview{
		TicketID:    ticket.ID,
		TicketTitle: ticket.Title,
		AgentName:   agentName,
		Status:      survey.Status,
		Channel:     survey.Channel,
		ResolvedAt:  ticket.ResolvedAt,
		ExpiresAt:   survey.ExpiresAt,
		CompletedAt: survey.CompletedAt,
	}, nil
}

// RespondSurvey 根据 token 记录客户满意度评价
func (s *SatisfactionService) RespondSurvey(ctx context.Context, token string, rating int, comment string) (*models.CustomerSatisfaction, error) {
	if token == "" {
		return nil, ErrSurveyNotFound
	}

	survey, err := s.repo.GetSurveyByToken(ctx, token)
	if err != nil {
		if err == ErrSurveyNotFound {
			return nil, ErrSurveyNotFound
		}
		return nil, fmt.Errorf("failed to load survey: %w", err)
	}
	scopeCtx := contextWithRecordScope(ctx, survey.TenantID, survey.WorkspaceID)

	if survey.Status == "completed" {
		return nil, ErrSurveyCompleted
	}
	if survey.ExpiresAt != nil && time.Now().After(*survey.ExpiresAt) {
		_ = s.repo.ExpireSurvey(scopeCtx, survey.ID)
		return nil, ErrSurveyExpired
	}

	req := &SatisfactionCreateRequest{
		TicketID:   survey.TicketID,
		CustomerID: survey.CustomerID,
		AgentID:    survey.AgentID,
		Rating:     rating,
		Comment:    comment,
		Category:   "overall",
	}

	satisfaction, err := s.CreateSatisfaction(scopeCtx, req)
	if err != nil {
		// 若已有评价则返回已存在的记录
		if strings.Contains(err.Error(), "already exists") {
			existing, getErr := s.GetSatisfactionByTicket(ctx, survey.TicketID)
			if getErr == nil && existing != nil {
				return existing, nil
			}
		}
		return nil, err
	}

	completed := time.Now()
	if err := s.repo.CompleteSurvey(scopeCtx, survey.ID, completed, satisfaction.ID); err != nil {
		s.logger.Warnf("Failed to update survey %d after response: %v", survey.ID, err)
	}

	return satisfaction, nil
}

// ResendSurvey 重新发送调查邮件/链接
func (s *SatisfactionService) ResendSurvey(ctx context.Context, id uint) (*models.SatisfactionSurvey, error) {
	survey, err := s.repo.GetSurveyScoped(ctx, id)
	if err != nil {
		if err == ErrSurveyNotFound {
			return nil, ErrSurveyNotFound
		}
		return nil, fmt.Errorf("failed to load survey: %w", err)
	}

	if survey.Status == "completed" && survey.SatisfactionID != nil {
		return nil, ErrSurveyCompleted
	}

	originalCompleted := survey.Status == "completed"
	now := time.Now()
	expires := now.Add(defaultSurveyTTL)
	if survey.SurveyToken == "" {
		survey.SurveyToken = uuid.NewString()
	}

	survey.Status = "sent"
	survey.SentAt = &now
	survey.ExpiresAt = &expires
	// 与 ScheduleSurvey 一致：email 渠道且已配置投递器时重新入队
	if survey.Channel == "email" && s.mailer != nil {
		survey.Status = "queued"
		survey.SentAt = nil
	}
	if !originalCompleted {
		survey.CompletedAt = nil
		survey.SatisfactionID = nil
	}

	if err := s.repo.SaveSurvey(ctx, survey); err != nil {
		return nil, fmt.Errorf("failed to resend survey: %w", err)
	}

	s.logger.Infof("Resent CSAT survey for ticket %d", survey.TicketID)
	return survey, nil
}

// ProcessPendingSurveyEmails 扫描 queued 的 email 渠道调查并真实投递。
// 成功 → sent+sent_at（CAS 只覆盖 queued，防并发重复发送）；
// 暂时失败（SMTP 抖动）保留 queued 下一轮重试；过期/收件人缺失置 failed。
func (s *SatisfactionService) ProcessPendingSurveyEmails(ctx context.Context, batchSize int) (int, error) {
	if s.mailer == nil {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = 50
	}
	now := time.Now()

	// 过期兜底：过期 queued 置 failed（无重试列，靠 expires_at 收敛）
	if _, err := s.repo.ExpireOverdueQueuedSurveys(ctx, now); err != nil {
		return 0, fmt.Errorf("failed to expire overdue surveys: %w", err)
	}

	surveys, err := s.repo.ListPendingEmailSurveys(ctx, now, batchSize)
	if err != nil {
		return 0, fmt.Errorf("failed to list pending surveys: %w", err)
	}

	sent := 0
	for _, survey := range surveys {
		recipient, ok := s.repo.GetUserEmail(ctx, survey.CustomerID)
		if !ok {
			// 收件人缺失是永久性失败，置 failed 避免无限重试
			_ = s.repo.MarkSurveyFailedIfQueued(ctx, survey.ID)
			s.logger.Warnf("Survey %d has no deliverable recipient (customer_id=%d), marked failed", survey.ID, survey.CustomerID)
			continue
		}

		subject, body := buildSurveyEmailContent(&survey, s.surveyLinkBaseURL)
		if err := s.mailer.SendSurveyEmail(ctx, recipient, subject, body); err != nil {
			// 暂时性失败：保留 queued，下一轮扫描重试
			s.logger.WithError(err).Warnf("Failed to send survey email %d to %s", survey.ID, recipient)
			continue
		}

		rows, err := s.repo.MarkSurveySentIfQueued(ctx, survey.ID, time.Now())
		if err != nil {
			s.logger.WithError(err).Warnf("Failed to mark survey %d as sent", survey.ID)
			continue
		}
		if rows > 0 {
			sent++
		}
	}
	return sent, nil
}

func buildSurveyEmailContent(survey *models.SatisfactionSurvey, baseURL string) (string, string) {
	link := fmt.Sprintf("%s/csat/%s", baseURL, survey.SurveyToken)
	if baseURL == "" {
		link = fmt.Sprintf("/csat/%s", survey.SurveyToken)
	}
	subject := fmt.Sprintf("您的工单 #%d 服务满意度评价", survey.TicketID)
	body := strings.Join([]string{
		"尊敬的客户，您好：",
		"",
		fmt.Sprintf("您的工单 #%d 已处理完毕，恳请您对本次服务做出评价。", survey.TicketID),
		"",
		fmt.Sprintf("评分链接：%s", link),
		"",
		"链接 7 天内有效，感谢您的反馈！",
	}, "\n")
	return subject, body
}

// CreateSatisfaction 创建满意度评价
func (s *SatisfactionService) CreateSatisfaction(ctx context.Context, req *SatisfactionCreateRequest) (*models.CustomerSatisfaction, error) {
	// 验证工单是否存在
	ticket, err := s.repo.GetTicketScoped(ctx, req.TicketID)
	if err != nil {
		if err == ErrTicketNotFound {
			return nil, ErrTicketNotFound
		}
		return nil, fmt.Errorf("failed to validate ticket: %w", err)
	}

	// 验证客户是否在当前 scope 内存在
	if _, err := s.repo.GetCustomerScopedByUserID(ctx, req.CustomerID); err != nil {
		if err == ErrCustomerNotFound {
			return nil, ErrCustomerNotFound
		}
		return nil, fmt.Errorf("failed to validate customer: %w", err)
	}

	// 验证客户是否是工单的所有者
	if ticket.CustomerID != req.CustomerID {
		return nil, fmt.Errorf("customer is not the owner of this ticket")
	}

	// 检查是否已经有评价（查询失败沿用旧行为：继续走创建让落库错误暴露）
	existing, _ := s.repo.FindSatisfactionByTicketAndCustomer(ctx, req.TicketID, req.CustomerID)
	if existing != nil {
		return nil, fmt.Errorf("satisfaction rating already exists for this ticket")
	}

	// 验证客服（如果提供）
	if req.AgentID != nil {
		if _, err := s.repo.GetAgentScopedByUserID(ctx, *req.AgentID); err != nil {
			if err == ErrAgentNotFound {
				return nil, ErrAgentNotFound
			}
			return nil, fmt.Errorf("failed to validate agent: %w", err)
		}
	}

	// 设置默认分类
	if req.Category == "" {
		req.Category = "overall"
	}

	// 创建满意度评价
	satisfaction := &models.CustomerSatisfaction{
		TenantID:    ticket.TenantID,
		WorkspaceID: ticket.WorkspaceID,
		TicketID:    req.TicketID,
		CustomerID:  req.CustomerID,
		AgentID:     req.AgentID,
		Rating:      req.Rating,
		Comment:     req.Comment,
		Category:    req.Category,
		CreatedAt:   time.Now(),
	}

	if err := s.repo.CreateSatisfaction(ctx, satisfaction); err != nil {
		s.logger.Errorf("Failed to create satisfaction: %v", err)
		return nil, fmt.Errorf("failed to create satisfaction: %w", err)
	}

	// 预加载关联数据
	if preloaded, err := s.repo.GetSatisfaction(ctx, satisfaction.ID); err == nil {
		satisfaction = preloaded
	} else {
		s.logger.Warnf("Failed to preload satisfaction data: %v", err)
	}

	s.logger.Infof("Created satisfaction rating: ticket_id=%d, customer_id=%d, rating=%d",
		req.TicketID, req.CustomerID, req.Rating)

	return satisfaction, nil
}

// GetSatisfaction 获取满意度评价
func (s *SatisfactionService) GetSatisfaction(ctx context.Context, id uint) (*models.CustomerSatisfaction, error) {
	satisfaction, err := s.repo.GetSatisfaction(ctx, id)
	if err != nil {
		if err == ErrSatisfactionNotFound {
			return nil, fmt.Errorf("satisfaction not found")
		}
		return nil, fmt.Errorf("failed to get satisfaction: %w", err)
	}
	return satisfaction, nil
}

// ListSatisfactions 获取满意度评价列表
func (s *SatisfactionService) ListSatisfactions(ctx context.Context, req *SatisfactionListRequest) ([]models.CustomerSatisfaction, int64, error) {
	satisfactions, total, err := s.repo.ListSatisfactions(ctx, req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list satisfactions: %w", err)
	}
	return satisfactions, total, nil
}

// GetSatisfactionByTicket 根据工单获取满意度评价
func (s *SatisfactionService) GetSatisfactionByTicket(ctx context.Context, ticketID uint) (*models.CustomerSatisfaction, error) {
	satisfaction, err := s.repo.GetSatisfactionByTicket(ctx, ticketID)
	if err != nil {
		return nil, fmt.Errorf("failed to get satisfaction by ticket: %w", err)
	}
	return satisfaction, nil
}

// GetSatisfactionStats 获取满意度统计
func (s *SatisfactionService) GetSatisfactionStats(ctx context.Context, dateFrom, dateTo *time.Time) (*SatisfactionStatsResponse, error) {
	totalRatings, err := s.repo.CountSatisfactions(ctx, dateFrom, dateTo)
	if err != nil {
		return nil, fmt.Errorf("failed to count ratings: %w", err)
	}

	avgRating, err := s.repo.AverageSatisfactionRating(ctx, dateFrom, dateTo)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate average rating: %w", err)
	}

	ratingDistribution, err := s.repo.SatisfactionRatingDistribution(ctx, dateFrom, dateTo)
	if err != nil {
		return nil, fmt.Errorf("failed to get rating distribution: %w", err)
	}

	categoryStats, err := s.repo.SatisfactionCategoryStats(ctx, dateFrom, dateTo)
	if err != nil {
		return nil, fmt.Errorf("failed to get category stats: %w", err)
	}

	// 获取趋势数据（失败不阻断，仅缺趋势）
	trendData, err := s.repo.SatisfactionTrend(ctx, dateFrom, dateTo)
	if err != nil {
		s.logger.Warnf("Failed to get trend data: %v", err)
		trendData = nil
	}

	return &SatisfactionStatsResponse{
		TotalRatings:       int(totalRatings),
		AverageRating:      avgRating,
		RatingDistribution: ratingDistribution,
		CategoryStats:      categoryStats,
		TrendData:          trendData,
	}, nil
}

// DeleteSatisfaction 删除满意度评价
func (s *SatisfactionService) DeleteSatisfaction(ctx context.Context, id uint) error {
	rows, err := s.repo.DeleteSatisfaction(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete satisfaction: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("satisfaction not found")
	}

	s.logger.Infof("Deleted satisfaction rating: id=%d", id)
	return nil
}

// UpdateSatisfaction 更新满意度评价（仅允许更新评论）
func (s *SatisfactionService) UpdateSatisfaction(ctx context.Context, id uint, comment string) (*models.CustomerSatisfaction, error) {
	satisfaction, err := s.repo.FindSatisfactionScoped(ctx, id)
	if err != nil {
		if err == ErrSatisfactionNotFound {
			return nil, fmt.Errorf("satisfaction not found")
		}
		return nil, fmt.Errorf("failed to find satisfaction: %w", err)
	}

	// 只允许更新评论
	satisfaction.Comment = comment

	if err := s.repo.SaveSatisfaction(ctx, satisfaction); err != nil {
		s.logger.Errorf("Failed to update satisfaction: %v", err)
		return nil, fmt.Errorf("failed to update satisfaction: %w", err)
	}

	// 重新加载关联数据
	if reloaded, err := s.repo.GetSatisfaction(ctx, id); err == nil {
		satisfaction = reloaded
	} else {
		s.logger.Warnf("Failed to preload satisfaction data: %v", err)
	}

	s.logger.Infof("Updated satisfaction comment: id=%d", id)
	return satisfaction, nil
}

func detectSurveyChannel(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "chat", "im":
		return "chat"
	case "voice", "phone":
		return "voice"
	case "email":
		return "email"
	default:
		return "email"
	}
}

func contextWithRecordScope(ctx context.Context, tenantID, workspaceID string) context.Context {
	currentTenant, currentWorkspace := tenantAndWorkspace(ctx)
	if currentTenant == "" {
		currentTenant = tenantID
	}
	if currentWorkspace == "" {
		currentWorkspace = workspaceID
	}
	return platformauth.ContextWithScope(ctx, currentTenant, currentWorkspace)
}
