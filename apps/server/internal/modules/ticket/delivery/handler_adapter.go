package delivery

import (
	"context"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	ticketapp "servify/apps/server/internal/modules/ticket/application"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"
	ticketinfra "servify/apps/server/internal/modules/ticket/infra"
	ticketorchestration "servify/apps/server/internal/modules/ticket/orchestration"
	svcmetrics "servify/apps/server/internal/observability/metrics"
)

// HandlerServiceAdapter bridges HTTP handlers to the modular ticket stack.
type HandlerServiceAdapter struct {
	repo         *ticketinfra.GormRepository
	query        ticketStatsQueryService
	cmd          *ticketapp.CommandService
	orchestrator *ticketorchestration.TicketOrchestrator
	db           *gorm.DB
	metrics      *svcmetrics.BusinessMetrics
}

// AttachBusinessMetrics 注入进程级业务指标（nil 安全，可链式）。
// 工单创建计数 tickets_created_total{tenant_id, priority}；状态迁到
// resolved 时计数 tickets_resolved_total{tenant_id, outcome}。
func (a *HandlerServiceAdapter) AttachBusinessMetrics(m *svcmetrics.BusinessMetrics) *HandlerServiceAdapter {
	if a == nil {
		return a
	}
	a.metrics = m
	return a
}

// HandlerService is the only ticket contract that HTTP handlers should depend on.
type HandlerService interface {
	CreateTicket(ctx context.Context, req *ticketcontract.CreateTicketRequest) (*models.Ticket, error)
	GetTicketByID(ctx context.Context, ticketID uint) (*models.Ticket, error)
	UpdateTicket(ctx context.Context, ticketID uint, req *ticketcontract.UpdateTicketRequest, userID uint) (*models.Ticket, error)
	ListTickets(ctx context.Context, req *ticketcontract.ListTicketRequest) ([]models.Ticket, int64, error)
	ListTicketCustomFields(ctx context.Context, activeOnly bool) ([]models.CustomField, error)
	AssignTicket(ctx context.Context, ticketID uint, agentID uint, assignerID uint) error
	AddComment(ctx context.Context, ticketID uint, userID uint, content string, commentType string) (*models.TicketComment, error)
	CloseTicket(ctx context.Context, ticketID uint, userID uint, reason string) error
	GetTicketStats(ctx context.Context, agentID *uint) (*ticketcontract.TicketStats, error)
	BulkUpdateTickets(ctx context.Context, req *ticketcontract.BulkUpdateTicketRequest, userID uint) (*ticketcontract.BulkUpdateResult, error)
	GetRelatedConversations(ctx context.Context, ticketID uint) ([]models.Session, error)
}

type ticketStatsQueryService interface {
	GetTicketStats(ctx context.Context, agentID *uint) (*ticketapp.TicketStatsDTO, error)
}

func NewHandlerServiceAdapter(db *gorm.DB, cmd *ticketapp.CommandService, orchestrator *ticketorchestration.TicketOrchestrator) *HandlerServiceAdapter {
	repo := ticketinfra.NewGormRepository(db)
	if cmd == nil {
		cmd = ticketapp.NewCommandService(repo)
	}
	return &HandlerServiceAdapter{
		repo:         repo,
		query:        ticketapp.NewQueryService(repo),
		cmd:          cmd,
		orchestrator: orchestrator,
		db:           db,
	}
}

func (a *HandlerServiceAdapter) CreateTicket(ctx context.Context, req *ticketcontract.CreateTicketRequest) (*models.Ticket, error) {
	prepared, err := a.orchestrator.PrepareCreateTicket(ctx, req)
	if err != nil {
		return nil, err
	}
	initialStatus := &models.TicketStatus{
		UserID:     0,
		FromStatus: "",
		ToStatus:   "open",
		Reason:     "工单创建",
	}
	if err := a.repo.CreateTicketModelWithCustomFieldsAndStatus(ctx, prepared.Ticket, prepared.CustomFieldValues, initialStatus); err != nil {
		return nil, err
	}
	// tenant 体系尚未落到工单模型，先按单租户口径记 default。
	a.metrics.RecordTicketCreated("default", prepared.Ticket.Priority)
	return a.orchestrator.ApplyCreateTicketSideEffects(ctx, prepared.Ticket)
}

func (a *HandlerServiceAdapter) GetTicketByID(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	return a.repo.LoadTicketModelByID(ctx, ticketID)
}

func (a *HandlerServiceAdapter) UpdateTicket(ctx context.Context, ticketID uint, req *ticketcontract.UpdateTicketRequest, userID uint) (*models.Ticket, error) {
	prepared, err := a.orchestrator.PrepareUpdateTicket(ctx, ticketID, req, userID)
	if err != nil {
		return nil, err
	}
	clearAll := false
	deleteFieldIDs := []uint(nil)
	upserts := []models.TicketCustomFieldValue(nil)
	if prepared.Mutation != nil {
		clearAll = prepared.Mutation.ClearAll
		deleteFieldIDs = prepared.Mutation.DeleteFieldIDs
		upserts = ticketapp.MapMutationToModelValues(ticketID, prepared.Mutation)
	}
	if err := a.repo.UpdateTicketModelWithStatusAndCustomFields(
		ctx,
		ticketID,
		prepared.Updates,
		prepared.StatusChange,
		clearAll,
		deleteFieldIDs,
		upserts,
	); err != nil {
		return nil, err
	}
	if prepared.StatusChange != nil && prepared.StatusChange.ToStatus == "resolved" {
		a.metrics.RecordTicketResolved("default", "resolved")
	}
	return a.orchestrator.ApplyUpdateTicketSideEffects(ctx, prepared, ticketID)
}

func (a *HandlerServiceAdapter) ListTickets(ctx context.Context, req *ticketcontract.ListTicketRequest) ([]models.Ticket, int64, error) {
	query := ticketapp.ListTicketsQuery{
		Page:               req.Page,
		PageSize:           req.PageSize,
		Status:             req.Status,
		Priority:           req.Priority,
		Category:           req.Category,
		Source:             req.Source,
		Tag:                req.Tag,
		AgentID:            req.AgentID,
		CustomerID:         req.CustomerID,
		Search:             req.Search,
		SortBy:             ticketcontract.NormalizeTicketSortBy(req.SortBy),
		SortOrder:          req.SortOrder,
		CustomFieldFilters: req.CustomFieldFilters,
	}
	return a.repo.ListTicketModels(ctx, query)
}

func (a *HandlerServiceAdapter) ListTicketCustomFields(ctx context.Context, activeOnly bool) ([]models.CustomField, error) {
	return a.repo.ListTicketCustomFields(ctx, activeOnly)
}

func (a *HandlerServiceAdapter) AssignTicket(ctx context.Context, ticketID uint, agentID uint, assignerID uint) error {
	originalTicket, err := a.repo.LoadTicketModelByID(ctx, ticketID)
	if err != nil {
		return err
	}
	if originalTicket.AgentID != nil && *originalTicket.AgentID == agentID {
		return nil
	}
	if _, err := a.cmd.AssignTicket(ctx, ticketID, ticketapp.AssignTicketCommand{
		AgentID: agentID,
		UserID:  assignerID,
	}); err != nil {
		return err
	}
	updatedTicket, err := a.repo.LoadTicketModelByID(ctx, ticketID)
	if err != nil {
		return err
	}
	a.orchestrator.ApplyAssignTicketSideEffects(ctx, originalTicket, updatedTicket)
	return nil
}

func (a *HandlerServiceAdapter) AddComment(ctx context.Context, ticketID uint, userID uint, content string, commentType string) (*models.TicketComment, error) {
	comment, err := a.cmd.AddComment(ctx, ticketID, ticketapp.AddCommentCommand{
		UserID:      userID,
		Content:     content,
		CommentType: commentType,
	})
	if err != nil {
		return nil, err
	}
	return &models.TicketComment{
		ID:        comment.ID,
		TicketID:  ticketID,
		UserID:    comment.UserID,
		Content:   comment.Content,
		Type:      comment.Type,
		CreatedAt: comment.CreatedAt,
	}, nil
}

func (a *HandlerServiceAdapter) CloseTicket(ctx context.Context, ticketID uint, userID uint, reason string) error {
	if _, err := a.cmd.CloseTicket(ctx, ticketID, ticketapp.CloseTicketCommand{
		UserID: userID,
		Reason: reason,
	}); err != nil {
		return err
	}
	a.orchestrator.ApplyCloseTicketSideEffects(ctx, ticketID, userID, reason)
	return nil
}

func (a *HandlerServiceAdapter) GetTicketStats(ctx context.Context, agentID *uint) (*ticketcontract.TicketStats, error) {
	stats, err := a.query.GetTicketStats(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return ticketcontract.MapTicketStats(stats), nil
}

func (a *HandlerServiceAdapter) BulkUpdateTickets(ctx context.Context, req *ticketcontract.BulkUpdateTicketRequest, userID uint) (*ticketcontract.BulkUpdateResult, error) {
	result, err := a.cmd.BulkUpdateTickets(ctx, ticketapp.BulkUpdateTicketsCommand{
		TicketIDs:     req.TicketIDs,
		Status:        req.Status,
		SetTags:       req.SetTags,
		AddTags:       req.AddTags,
		RemoveTags:    req.RemoveTags,
		AgentID:       req.AgentID,
		UnassignAgent: req.UnassignAgent,
		UserID:        userID,
	})
	if err != nil {
		return nil, err
	}
	out := &ticketcontract.BulkUpdateResult{
		Updated: result.Updated,
		Failed:  make([]ticketcontract.BulkUpdateFailure, 0, len(result.Failed)),
	}
	for _, failure := range result.Failed {
		out.Failed = append(out.Failed, ticketcontract.BulkUpdateFailure{
			TicketID: failure.TicketID,
			Error:    failure.Error,
		})
	}
	return out, nil
}

var _ HandlerService = (*HandlerServiceAdapter)(nil)

func (a *HandlerServiceAdapter) GetRelatedConversations(ctx context.Context, ticketID uint) ([]models.Session, error) {
	var sessions []models.Session
	result := a.db.Preload("User").Preload("Agent").
		Where("ticket_id = ?", ticketID).
		Order("started_at DESC").
		Find(&sessions)
	if result.Error != nil {
		return nil, result.Error
	}
	return sessions, nil
}
