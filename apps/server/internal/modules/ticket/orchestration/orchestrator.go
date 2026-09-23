package orchestration

import (
	"context"
	"fmt"
	"time"

	"servify/apps/server/internal/models"
	ticketapp "servify/apps/server/internal/modules/ticket/application"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type SLAService interface {
	CheckSLAViolation(context.Context, *models.Ticket) (*models.SLAViolation, error)
	ResolveViolationsByTicket(context.Context, uint, []string) error
}

type SatisfactionService interface {
	ScheduleSurvey(context.Context, *models.Ticket) (*models.SatisfactionSurvey, error)
}

type TicketCreatePreparation struct {
	Ticket            *models.Ticket
	CustomFieldValues []models.TicketCustomFieldValue
}

type TicketUpdatePreparation struct {
	OldTicket     *models.Ticket
	Updates       map[string]interface{}
	StatusChange  *models.TicketStatus
	Mutation      *ticketapp.CustomFieldMutation
	StatusChanged bool
	AgentChanged  bool
}

type TicketOrchestrator struct {
	db                       *gorm.DB
	logger                   *logrus.Logger
	slaService               SLAService
	satisfaction             SatisfactionService
	bus                      eventbus.Bus
	customerExists           func(context.Context, uint) (bool, error)
	findAutoAssignee         func(context.Context) (*models.Agent, error)
	buildCustomFieldValues   func(context.Context, map[string]interface{}, map[string]interface{}, bool) ([]models.TicketCustomFieldValue, error)
	prepareCustomFieldUpdate func(context.Context, uint, map[string]interface{}, map[string]interface{}) (*ticketapp.CustomFieldMutation, error)
	loadTicket               func(context.Context, uint) (*models.Ticket, error)
	assignTicket             func(context.Context, uint, uint, uint) error
	addComment               func(context.Context, uint, uint, string, string) (*models.TicketComment, error)
}

func NewTicketOrchestrator(
	db *gorm.DB,
	logger *logrus.Logger,
	slaService SLAService,
	satisfaction SatisfactionService,
	bus eventbus.Bus,
	customerExists func(context.Context, uint) (bool, error),
	findAutoAssignee func(context.Context) (*models.Agent, error),
	buildCustomFieldValues func(context.Context, map[string]interface{}, map[string]interface{}, bool) ([]models.TicketCustomFieldValue, error),
	prepareCustomFieldUpdate func(context.Context, uint, map[string]interface{}, map[string]interface{}) (*ticketapp.CustomFieldMutation, error),
	loadTicket func(context.Context, uint) (*models.Ticket, error),
	assignTicket func(context.Context, uint, uint, uint) error,
	addComment func(context.Context, uint, uint, string, string) (*models.TicketComment, error),
) *TicketOrchestrator {
	return &TicketOrchestrator{
		db:                       db,
		logger:                   logger,
		slaService:               slaService,
		satisfaction:             satisfaction,
		bus:                      bus,
		customerExists:           customerExists,
		findAutoAssignee:         findAutoAssignee,
		buildCustomFieldValues:   buildCustomFieldValues,
		prepareCustomFieldUpdate: prepareCustomFieldUpdate,
		loadTicket:               loadTicket,
		assignTicket:             assignTicket,
		addComment:               addComment,
	}
}

func (o *TicketOrchestrator) PrepareCreateTicket(ctx context.Context, req *ticketcontract.CreateTicketRequest) (*TicketCreatePreparation, error) {
	if o.customerExists != nil {
		exists, err := o.customerExists(ctx, req.CustomerID)
		if err != nil {
			return nil, fmt.Errorf("customer lookup failed: %w", err)
		}
		if !exists {
			return nil, fmt.Errorf("customer not found")
		}
	} else {
		var customer models.User
		if err := o.db.First(&customer, req.CustomerID).Error; err != nil {
			return nil, fmt.Errorf("customer not found: %w", err)
		}
	}

	category := req.Category
	if category == "" {
		category = "general"
	}
	priority := req.Priority
	if priority == "" {
		priority = "normal"
	}
	source := req.Source
	if source == "" {
		source = "web"
	}

	cfValues, err := o.buildCustomFieldValues(ctx, req.CustomFields, map[string]interface{}{
		"ticket.category": category,
		"ticket.priority": priority,
		"ticket.source":   source,
		"ticket.status":   "open",
	}, true)
	if err != nil {
		return nil, err
	}

	ticket := &models.Ticket{
		Title:       req.Title,
		Description: req.Description,
		CustomerID:  req.CustomerID,
		Category:    category,
		Priority:    priority,
		Status:      "open",
		Source:      source,
		Tags:        req.Tags,
	}
	if req.SessionID != "" {
		ticket.SessionID = &req.SessionID
	}

	return &TicketCreatePreparation{
		Ticket:            ticket,
		CustomFieldValues: cfValues,
	}, nil
}

// PrepareCreateVisitorTicket 准备访客（免认证）工单（M3 移动 SDK 配套 §10 #4）：
// 会话必须已存在（工单从会话页发起，消息往来已建 session 行），租户 scope 从
// session 行原样继承（与 realtime 侧消息持久化同一口径；scope 落在 ticket 上，
// repo 的 applyTicketScopeFields 不覆盖非空值）。访客无 customer 主体
// （CustomerID=0，与 messages.UserID=0 同语义），分类/优先级/来源由服务端
// 固定默认值，自定义字段与标签不开放。
func (o *TicketOrchestrator) PrepareCreateVisitorTicket(ctx context.Context, req *ticketcontract.CreateVisitorTicketRequest) (*TicketCreatePreparation, error) {
	if req == nil {
		return nil, fmt.Errorf("request required")
	}
	if req.Title == "" {
		return nil, fmt.Errorf("title required")
	}
	var session models.Session
	if err := o.db.First(&session, "id = ?", req.SessionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("session not found: %s", req.SessionID)
		}
		return nil, fmt.Errorf("session lookup failed: %w", err)
	}

	ticket := &models.Ticket{
		Title:       req.Title,
		Description: req.Description,
		AISummary:   req.AISummary,
		CustomerID:  0,
		Category:    "general",
		Priority:    "normal",
		Status:      "open",
		Source:      "chat",
		TenantID:    session.TenantID,
		WorkspaceID: session.WorkspaceID,
		SessionID:   &req.SessionID,
	}
	return &TicketCreatePreparation{Ticket: ticket}, nil
}

func (o *TicketOrchestrator) ApplyCreateTicketSideEffects(ctx context.Context, ticket *models.Ticket) (*models.Ticket, error) {
	if ticket == nil {
		return nil, fmt.Errorf("ticket required")
	}
	go o.autoAssignAgent(ticket.ID)

	o.logger.Infof("Created ticket %d for customer %d", ticket.ID, ticket.CustomerID)

	createdTicket, err := o.loadTicket(ctx, ticket.ID)
	if err != nil {
		return nil, err
	}

	o.publishTicketModuleEvent(ctx, ticketapp.TicketCreatedEventName, createdTicket)
	o.evaluateTicketSLA(ctx, createdTicket, false, false)
	return createdTicket, nil
}

func (o *TicketOrchestrator) PrepareUpdateTicket(ctx context.Context, ticketID uint, req *ticketcontract.UpdateTicketRequest, userID uint) (*TicketUpdatePreparation, error) {
	oldTicket, err := o.loadTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}

	statusChanged := req.Status != nil && *req.Status != oldTicket.Status
	agentChanged := false
	if req.AgentID != nil {
		if (oldTicket.AgentID == nil && *req.AgentID != 0) || (oldTicket.AgentID != nil && *oldTicket.AgentID != *req.AgentID) {
			agentChanged = true
		}
	}

	updates := make(map[string]interface{})
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.AgentID != nil {
		updates["agent_id"] = *req.AgentID
	}
	if req.Category != nil {
		updates["category"] = *req.Category
	}
	if req.Priority != nil {
		updates["priority"] = *req.Priority
	}
	if req.Tags != nil {
		updates["tags"] = *req.Tags
	}
	if req.DueDate != nil {
		updates["due_date"] = *req.DueDate
	}

	targetCategory := oldTicket.Category
	targetPriority := oldTicket.Priority
	targetSource := oldTicket.Source
	targetStatus := oldTicket.Status
	if req.Category != nil {
		targetCategory = *req.Category
	}
	if req.Priority != nil {
		targetPriority = *req.Priority
	}

	var statusChange *models.TicketStatus
	if statusChanged {
		policy := ticketapp.NewStatusTransitionPolicy()
		if err := policy.Validate(oldTicket.Status, *req.Status); err != nil {
			return nil, err
		}
		targetStatus = *req.Status
		updates["status"] = *req.Status
		changeTime := time.Now()
		switch *req.Status {
		case "resolved":
			updates["resolved_at"] = &changeTime
		case "closed":
			updates["closed_at"] = &changeTime
		}
		statusChange = &models.TicketStatus{
			UserID:     userID,
			FromStatus: oldTicket.Status,
			ToStatus:   *req.Status,
			Reason:     "状态更新",
			CreatedAt:  changeTime,
		}
	}

	var mutation *ticketapp.CustomFieldMutation
	if req.CustomFields != nil {
		mutation, err = o.prepareCustomFieldUpdate(ctx, ticketID, req.CustomFields, map[string]interface{}{
			"ticket.category": targetCategory,
			"ticket.priority": targetPriority,
			"ticket.source":   targetSource,
			"ticket.status":   targetStatus,
		})
		if err != nil {
			return nil, err
		}
	}

	updates["updated_at"] = time.Now()
	return &TicketUpdatePreparation{
		OldTicket:     oldTicket,
		Updates:       updates,
		StatusChange:  statusChange,
		Mutation:      mutation,
		StatusChanged: statusChanged,
		AgentChanged:  agentChanged,
	}, nil
}

func (o *TicketOrchestrator) ApplyUpdateTicketSideEffects(ctx context.Context, prepared *TicketUpdatePreparation, ticketID uint) (*models.Ticket, error) {
	o.logger.Infof("Updated ticket %d by user flow", ticketID)
	updatedTicket, err := o.loadTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if prepared != nil && prepared.AgentChanged {
		o.publishTicketModuleEvent(ctx, ticketapp.TicketAssignedEventName, updatedTicket)
	}
	if prepared != nil {
		o.evaluateTicketSLA(ctx, updatedTicket, prepared.StatusChanged, prepared.AgentChanged)
	}
	return updatedTicket, nil
}

func (o *TicketOrchestrator) ApplyAssignTicketSideEffects(ctx context.Context, originalTicket, updatedTicket *models.Ticket) {
	if updatedTicket == nil {
		return
	}
	if originalTicket != nil && originalTicket.AgentID == nil {
		o.resolveTicketSLAViolations(ctx, updatedTicket.ID, []string{"first_response"})
	}
	statusChanged := false
	if originalTicket != nil {
		statusChanged = originalTicket.Status != updatedTicket.Status
	}
	o.evaluateTicketSLA(ctx, updatedTicket, statusChanged, true)
}

func (o *TicketOrchestrator) ApplyCloseTicketSideEffects(ctx context.Context, ticketID uint, userID uint, reason string) {
	if _, err := o.addComment(ctx, ticketID, userID, fmt.Sprintf("工单已关闭。原因：%s", reason), "system"); err != nil {
		o.logger.Warnf("Failed to add system close comment for ticket %d: %v", ticketID, err)
	}
	o.resolveTicketSLAViolations(ctx, ticketID, []string{"resolution"})
	o.logger.Infof("Closed ticket %d by user %d", ticketID, userID)

	if o.satisfaction != nil {
		closedTicket, getErr := o.loadTicket(ctx, ticketID)
		if getErr != nil {
			o.logger.Warnf("Failed to fetch closed ticket %d for CSAT survey: %v", ticketID, getErr)
		} else if _, err := o.satisfaction.ScheduleSurvey(ctx, closedTicket); err != nil {
			o.logger.Warnf("Failed to schedule CSAT survey for ticket %d: %v", ticketID, err)
		}
	}
}

func (o *TicketOrchestrator) autoAssignAgent(ticketID uint) {
	agent, err := o.selectAutoAssignee(context.Background())
	if err != nil {
		o.logger.Debugf("No available agent for auto-assignment of ticket %d", ticketID)
		return
	}
	if err := o.assignTicket(context.Background(), ticketID, agent.UserID, 0); err != nil {
		o.logger.Errorf("Failed to auto-assign ticket %d to agent %d: %v", ticketID, agent.UserID, err)
	} else {
		o.logger.Infof("Auto-assigned ticket %d to agent %d", ticketID, agent.UserID)
	}
}

func (o *TicketOrchestrator) selectAutoAssignee(ctx context.Context) (*models.Agent, error) {
	if o.findAutoAssignee != nil {
		return o.findAutoAssignee(ctx)
	}
	if o.db == nil {
		return nil, fmt.Errorf("no db or findAutoAssignee configured")
	}

	var agent models.Agent
	err := o.db.WithContext(ctx).
		Where("status = ? AND current_load < max_concurrent", "online").
		Order("current_load ASC, avg_response_time ASC").
		First(&agent).Error
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

func (o *TicketOrchestrator) publishTicketModuleEvent(ctx context.Context, name string, ticket *models.Ticket) {
	if o.bus == nil || ticket == nil {
		return
	}
	dto := ticketapp.TicketDTO{
		ID:         ticket.ID,
		Title:      ticket.Title,
		CustomerID: ticket.CustomerID,
		AgentID:    ticket.AgentID,
		Category:   ticket.Category,
		Priority:   ticket.Priority,
		Status:     ticket.Status,
		Source:     ticket.Source,
		Tags:       ticket.Tags,
		CreatedAt:  ticket.CreatedAt,
		UpdatedAt:  ticket.UpdatedAt,
		ResolvedAt: ticket.ResolvedAt,
		ClosedAt:   ticket.ClosedAt,
	}
	if err := o.bus.Publish(ctx, ticketapp.NewTicketEvent(name, dto)); err != nil {
		o.logger.Warnf("Failed to publish ticket event %s for ticket %d: %v", name, ticket.ID, err)
	}
}

func (o *TicketOrchestrator) evaluateTicketSLA(ctx context.Context, ticket *models.Ticket, statusChanged, agentChanged bool) {
	if o.slaService == nil || ticket == nil {
		return
	}
	if statusChanged && (ticket.Status == "resolved" || ticket.Status == "closed") {
		if err := o.slaService.ResolveViolationsByTicket(ctx, ticket.ID, []string{"resolution"}); err != nil {
			o.logger.Warnf("Failed to resolve SLA resolution violations for ticket %d: %v", ticket.ID, err)
		}
	}
	if agentChanged && ticket.AgentID != nil {
		if err := o.slaService.ResolveViolationsByTicket(ctx, ticket.ID, []string{"first_response"}); err != nil {
			o.logger.Warnf("Failed to resolve SLA first response violations for ticket %d: %v", ticket.ID, err)
		}
	}
	if _, err := o.slaService.CheckSLAViolation(ctx, ticket); err != nil {
		o.logger.Warnf("Failed to evaluate SLA violation for ticket %d: %v", ticket.ID, err)
	}
}

func (o *TicketOrchestrator) resolveTicketSLAViolations(ctx context.Context, ticketID uint, types []string) {
	if o.slaService == nil {
		return
	}
	if err := o.slaService.ResolveViolationsByTicket(ctx, ticketID, types); err != nil {
		o.logger.Warnf("Failed to resolve SLA violations for ticket %d: %v", ticketID, err)
	}
}
