package infra

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/ticket/application"
	"servify/apps/server/internal/modules/ticket/domain"
	platformauth "servify/apps/server/internal/platform/auth"
)

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

// ticketAgentIDWithLock 事务内锁定工单行（postgres/mysql 加 FOR UPDATE 串行化
// 并发派单；sqlite 无该语法，依赖单写者语义），返回库内 agent_id。
// 派单/取消/关闭路径的负载增减一律以该库内值为准——调用方事务外的预读
// 在并发下可能 stale，按它增减会漂移坐席 current_load。行不存在报 ErrRecordNotFound。
func ticketAgentIDWithLock(tx *gorm.DB, ctx context.Context, ticketID uint) (*uint, error) {
	q := applyTicketScope(tx.Model(&models.Ticket{}), ctx).Where("id = ?", ticketID)
	if dialector := tx.Name(); dialector == "postgres" || dialector == "mysql" {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row struct {
		AgentID *uint `gorm:"column:agent_id"`
	}
	if err := q.Take(&row).Error; err != nil {
		return nil, err
	}
	return row.AgentID, nil
}

// ticketAgentFromRaw 归一化 updates map / 库值里的 agent_id：nil 与 unassign
// 哨兵 0 都归一为 nil（未指派），指针值原样返回。
func ticketAgentFromRaw(v interface{}) *uint {
	switch x := v.(type) {
	case nil:
		return nil
	case uint:
		if x == 0 {
			return nil
		}
		return &x
	case *uint:
		if x == nil || *x == 0 {
			return nil
		}
		return x
	default:
		return nil
	}
}

// syncAgentLoadDelta 按锁定库值算坐席负载增减：来源与目标不同则来源减 1
// （不低于 0），目标非空则加 1。两个参数皆 nil 表示未改派，是无操作。
func syncAgentLoadDelta(tx *gorm.DB, fromAgentID *uint, toAgentID *uint) error {
	if fromAgentID != nil && (toAgentID == nil || *fromAgentID != *toAgentID) {
		if err := tx.Exec(
			`UPDATE agents SET current_load = CASE WHEN current_load > 0 THEN current_load - 1 ELSE 0 END WHERE user_id = ?`,
			*fromAgentID,
		).Error; err != nil {
			return err
		}
	}
	if toAgentID != nil && (fromAgentID == nil || *toAgentID != *fromAgentID) {
		if err := tx.Model(&models.Agent{}).Where("user_id = ?", *toAgentID).
			UpdateColumn("current_load", gorm.Expr("current_load + 1")).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *GormRepository) GetTicketByID(ctx context.Context, ticketID uint) (*domain.TicketDetails, error) {
	ticket, err := r.LoadTicketModelByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	return mapTicketDetails(*ticket), nil
}

func (r *GormRepository) LoadTicketModelByID(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	var ticket models.Ticket
	err := applyTicketScope(r.db.WithContext(ctx), ctx).
		Preload("Customer").
		Preload("Agent").
		Preload("Session").
		Preload("Comments", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at ASC")
		}).
		Preload("StatusHistory", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at ASC")
		}).
		Preload("CustomFieldValues", func(db *gorm.DB) *gorm.DB {
			return db.Order("custom_field_id ASC").Preload("CustomField")
		}).
		Preload("Attachments").
		First(&ticket, ticketID).Error
	if err != nil {
		return nil, fmt.Errorf("ticket not found: %w", err)
	}
	return &ticket, nil
}

func (r *GormRepository) ListTickets(ctx context.Context, query application.ListTicketsQuery) ([]domain.Ticket, int64, error) {
	tickets, total, err := r.ListTicketModels(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	items := make([]domain.Ticket, 0, len(tickets))
	for _, ticket := range tickets {
		items = append(items, mapTicket(ticket))
	}
	return items, total, nil
}

func (r *GormRepository) ListTicketModels(ctx context.Context, query application.ListTicketsQuery) ([]models.Ticket, int64, error) {
	db := applyTicketScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx)
	db = applyListTicketFilters(db, query)

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	orderBy := "created_at DESC"
	if strings.TrimSpace(query.SortBy) != "" {
		direction := "ASC"
		if strings.EqualFold(query.SortOrder, "desc") {
			direction = "DESC"
		}
		orderBy = fmt.Sprintf("%s %s", query.SortBy, direction)
	}

	var tickets []models.Ticket
	err := db.
		Preload("Customer").
		Preload("Agent").
		Preload("CustomFieldValues", func(db *gorm.DB) *gorm.DB {
			return db.Order("custom_field_id ASC").Preload("CustomField")
		}).
		Order(orderBy).
		Offset((query.Page - 1) * query.PageSize).
		Limit(query.PageSize).
		Find(&tickets).Error
	if err != nil {
		return nil, 0, err
	}
	return tickets, total, nil
}

func (r *GormRepository) GetTicketStats(ctx context.Context, agentID *uint) (*application.TicketStatsDTO, error) {
	stats := &application.TicketStatsDTO{}

	query := applyTicketScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx)
	if agentID != nil {
		query = query.Where("agent_id = ?", *agentID)
	}
	if err := query.Count(&stats.Total).Error; err != nil {
		return nil, err
	}

	statusQuery := applyTicketScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx)
	priorityQuery := applyTicketScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx)
	todayQuery := applyTicketScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx)
	pendingQuery := applyTicketScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx)
	resolvedQuery := applyTicketScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx)
	if agentID != nil {
		statusQuery = statusQuery.Where("agent_id = ?", *agentID)
		priorityQuery = priorityQuery.Where("agent_id = ?", *agentID)
		todayQuery = todayQuery.Where("agent_id = ?", *agentID)
		pendingQuery = pendingQuery.Where("agent_id = ?", *agentID)
		resolvedQuery = resolvedQuery.Where("agent_id = ?", *agentID)
	}

	if err := statusQuery.Select("status, COUNT(*) as count").Group("status").Scan(&stats.ByStatus).Error; err != nil {
		return nil, err
	}
	if err := priorityQuery.Select("priority, COUNT(*) as count").Group("priority").Scan(&stats.ByPriority).Error; err != nil {
		return nil, err
	}

	today := time.Now().Truncate(24 * time.Hour)
	if err := todayQuery.Where("created_at >= ?", today).Count(&stats.TodayCreated).Error; err != nil {
		return nil, err
	}
	if err := pendingQuery.Where("status IN ?", []string{"open", "assigned"}).Count(&stats.Pending).Error; err != nil {
		return nil, err
	}
	if err := resolvedQuery.Where("status = ?", "resolved").Count(&stats.Resolved).Error; err != nil {
		return nil, err
	}

	return stats, nil
}

func (r *GormRepository) ListTicketCustomFields(ctx context.Context, activeOnly bool) ([]models.CustomField, error) {
	q := r.db.WithContext(ctx).Model(&models.CustomField{}).Where("resource = ?", "ticket").Order("id ASC")
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if activeOnly {
		q = q.Where("active = ?", true)
	}

	var fields []models.CustomField
	if err := q.Find(&fields).Error; err != nil {
		return nil, err
	}
	return fields, nil
}

func (r *GormRepository) CreateTicket(ctx context.Context, ticket *domain.Ticket) error {
	model := mapTicketModel(*ticket)
	applyTicketScopeFields(ctx, &model)
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return err
	}
	*ticket = mapTicket(model)
	return nil
}

func (r *GormRepository) CreateTicketModelWithCustomFieldsAndStatus(
	ctx context.Context,
	ticket *models.Ticket,
	values []models.TicketCustomFieldValue,
	initialStatus *models.TicketStatus,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		applyTicketScopeFields(ctx, ticket)
		if err := tx.Create(ticket).Error; err != nil {
			return err
		}
		if len(values) > 0 {
			for i := range values {
				values[i].TicketID = ticket.ID
			}
			if err := tx.Create(&values).Error; err != nil {
				return err
			}
		}
		if initialStatus == nil {
			return nil
		}
		initialStatus.TicketID = ticket.ID
		return tx.Create(initialStatus).Error
	})
}

func (r *GormRepository) CreateTicketModelWithCustomFields(ctx context.Context, ticket *models.Ticket, values []models.TicketCustomFieldValue) error {
	return r.CreateTicketModelWithCustomFieldsAndStatus(ctx, ticket, values, nil)
}

func (r *GormRepository) UpdateTicket(ctx context.Context, ticket *domain.Ticket) error {
	model := mapTicketModel(*ticket)
	if err := r.UpdateTicketModel(ctx, ticket.ID, map[string]interface{}{
		"title":       model.Title,
		"description": model.Description,
		"customer_id": model.CustomerID,
		"agent_id":    model.AgentID,
		"session_id":  model.SessionID,
		"category":    model.Category,
		"priority":    model.Priority,
		"status":      model.Status,
		"source":      model.Source,
		"tags":        model.Tags,
		"due_date":    model.DueDate,
		"resolved_at": model.ResolvedAt,
		"closed_at":   model.ClosedAt,
		"updated_at":  model.UpdatedAt,
	}); err != nil {
		return err
	}
	return nil
}

func (r *GormRepository) UpdateTicketWithStatus(
	ctx context.Context,
	ticket *domain.Ticket,
	fromStatus string,
	userID uint,
	reason string,
) error {
	model := mapTicketModel(*ticket)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Ticket{}).Where("id = ?", ticket.ID).Updates(map[string]interface{}{
			"title":       model.Title,
			"description": model.Description,
			"customer_id": model.CustomerID,
			"agent_id":    model.AgentID,
			"session_id":  model.SessionID,
			"category":    model.Category,
			"priority":    model.Priority,
			"status":      model.Status,
			"source":      model.Source,
			"tags":        model.Tags,
			"due_date":    model.DueDate,
			"resolved_at": model.ResolvedAt,
			"closed_at":   model.ClosedAt,
			"updated_at":  model.UpdatedAt,
		}).Error; err != nil {
			return err
		}

		change := models.TicketStatus{
			TicketID:   ticket.ID,
			UserID:     userID,
			FromStatus: fromStatus,
			ToStatus:   model.Status,
			Reason:     reason,
			CreatedAt:  model.UpdatedAt,
		}
		return tx.Create(&change).Error
	})
}

func (r *GormRepository) UpdateTicketModelWithStatusAndCustomFields(
	ctx context.Context,
	ticketID uint,
	updates map[string]interface{},
	statusChange *models.TicketStatus,
	clearAll bool,
	deleteFieldIDs []uint,
	upserts []models.TicketCustomFieldValue,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 改派要同步坐席负载，且必须按事务内锁定的库内 agent_id 算增减——
		// 调用方事务外预读的工单可能已被并发流转，按 stale 值各减会漂移
		// current_load（AssignTicketModel 同一口径）。
		var fromAgentID, toAgentID *uint
		if raw, ok := updates["agent_id"]; ok {
			target := ticketAgentFromRaw(raw)
			if target == nil {
				updates["agent_id"] = nil // unassign 哨兵 0 → NULL，与 UnassignTicketModel 一致
			}
			locked, err := ticketAgentIDWithLock(tx, ctx, ticketID)
			if err != nil {
				return err
			}
			if target == nil || locked == nil || *locked != *target {
				fromAgentID = ticketAgentFromRaw(locked)
				toAgentID = target
			}
		}

		if len(updates) > 0 {
			if err := applyTicketScope(tx.Model(&models.Ticket{}), ctx).Where("id = ?", ticketID).Updates(updates).Error; err != nil {
				return err
			}
		}
		if statusChange != nil {
			statusChange.TicketID = ticketID
			if err := tx.Create(statusChange).Error; err != nil {
				return err
			}
		}
		if clearAll {
			return tx.Where("ticket_id = ?", ticketID).Delete(&models.TicketCustomFieldValue{}).Error
		}
		if len(deleteFieldIDs) > 0 {
			if err := tx.Where("ticket_id = ? AND custom_field_id IN ?", ticketID, deleteFieldIDs).Delete(&models.TicketCustomFieldValue{}).Error; err != nil {
				return err
			}
		}
		for _, value := range upserts {
			var existing models.TicketCustomFieldValue
			err := tx.Where("ticket_id = ? AND custom_field_id = ?", ticketID, value.CustomFieldID).First(&existing).Error
			if err == nil {
				existing.Value = value.Value
				existing.UpdatedAt = value.UpdatedAt
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
				continue
			}
			if err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
			value.TicketID = ticketID
			if err := tx.Create(&value).Error; err != nil {
				return err
			}
		}
		return syncAgentLoadDelta(tx, fromAgentID, toAgentID)
	})
}

func (r *GormRepository) AssignTicket(
	ctx context.Context,
	ticket *domain.Ticket,
	previousAgentID *uint,
	fromStatus string,
	userID uint,
	reason string,
) error {
	agentID := uint(0)
	if ticket.AgentID != nil {
		agentID = *ticket.AgentID
	}
	return r.AssignTicketModel(ctx, ticket.ID, agentID, previousAgentID, fromStatus, ticket.Status, userID, reason)
}

func (r *GormRepository) UnassignTicket(
	ctx context.Context,
	ticket *domain.Ticket,
	previousAgentID uint,
	fromStatus string,
	userID uint,
	reason string,
) error {
	return r.UnassignTicketModel(ctx, ticket.ID, previousAgentID, fromStatus, ticket.Status, userID, reason)
}

func (r *GormRepository) CloseTicket(ctx context.Context, ticket *domain.Ticket, fromStatus string, userID uint, reason string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先锁工单行；ticket.AgentID 是事务外的预读，减 1 以库值为准。
		currentAgentID, err := ticketAgentIDWithLock(tx, ctx, ticket.ID)
		if err != nil {
			return err
		}

		updates := map[string]interface{}{
			"status":     ticket.Status,
			"closed_at":  ticket.ClosedAt,
			"updated_at": ticket.UpdatedAt,
		}
		if err := applyTicketScope(tx.Model(&models.Ticket{}), ctx).Where("id = ?", ticket.ID).Updates(updates).Error; err != nil {
			return err
		}

		if currentAgentID != nil {
			if err := tx.Exec(
				`UPDATE agents SET current_load = CASE WHEN current_load > 0 THEN current_load - 1 ELSE 0 END WHERE user_id = ?`,
				*currentAgentID,
			).Error; err != nil {
				return err
			}
		}

		change := models.TicketStatus{
			TicketID:   ticket.ID,
			UserID:     userID,
			FromStatus: fromStatus,
			ToStatus:   "closed",
			Reason:     reason,
			CreatedAt:  ticket.UpdatedAt,
		}
		return tx.Create(&change).Error
	})
}

func (r *GormRepository) UpdateTicketModel(ctx context.Context, ticketID uint, updates map[string]interface{}) error {
	result := applyTicketScope(r.db.WithContext(ctx).Model(&models.Ticket{}), ctx).Where("id = ?", ticketID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *GormRepository) SyncTicketCustomFieldValues(
	ctx context.Context,
	ticketID uint,
	clearAll bool,
	deleteFieldIDs []uint,
	upserts []models.TicketCustomFieldValue,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if clearAll {
			if err := tx.Where("ticket_id = ?", ticketID).Delete(&models.TicketCustomFieldValue{}).Error; err != nil {
				return err
			}
			return nil
		}

		if len(deleteFieldIDs) > 0 {
			if err := tx.Where("ticket_id = ? AND custom_field_id IN ?", ticketID, deleteFieldIDs).Delete(&models.TicketCustomFieldValue{}).Error; err != nil {
				return err
			}
		}

		for _, value := range upserts {
			var existing models.TicketCustomFieldValue
			err := tx.Where("ticket_id = ? AND custom_field_id = ?", ticketID, value.CustomFieldID).First(&existing).Error
			if err == nil {
				existing.Value = value.Value
				existing.UpdatedAt = value.UpdatedAt
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
				continue
			}
			if err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
			value.TicketID = ticketID
			if err := tx.Create(&value).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

func (r *GormRepository) AssignTicketModel(
	ctx context.Context,
	ticketID uint,
	agentID uint,
	previousAgentID *uint,
	fromStatus string,
	toStatus string,
	operatorID uint,
	reason string,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// previousAgentID 是调用方事务外的预读，并发下可能 stale；减 1 一律
		// 按锁定行内的库值执行，避免两流按同一 stale 值各减/漏减。
		currentAgentID, err := ticketAgentIDWithLock(tx, ctx, ticketID)
		if err != nil {
			return err
		}
		if currentAgentID != nil && *currentAgentID != agentID {
			if err := tx.Exec(
				`UPDATE agents SET current_load = CASE WHEN current_load > 0 THEN current_load - 1 ELSE 0 END WHERE user_id = ?`,
				*currentAgentID,
			).Error; err != nil {
				return err
			}
		}

		updates := map[string]interface{}{
			"agent_id": agentID,
		}
		if toStatus != fromStatus {
			updates["status"] = toStatus
		}
		result := applyTicketScope(tx.Model(&models.Ticket{}), ctx).Where("id = ?", ticketID).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if currentAgentID == nil || *currentAgentID != agentID {
			if err := tx.Model(&models.Agent{}).Where("user_id = ?", agentID).UpdateColumn("current_load", gorm.Expr("current_load + 1")).Error; err != nil {
				return err
			}
		}

		change := models.TicketStatus{
			TicketID:   ticketID,
			UserID:     operatorID,
			FromStatus: fromStatus,
			ToStatus:   toStatus,
			Reason:     reason,
		}
		return tx.Create(&change).Error
	})
}

func (r *GormRepository) UnassignTicketModel(
	ctx context.Context,
	ticketID uint,
	agentID uint,
	fromStatus string,
	toStatus string,
	operatorID uint,
	reason string,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// agentID 参数是调用方事务外的预读（可能与库值不一致）；减 1 按锁定
		// 行内的库值执行——工单已被并发流转走时不再对 stale 坐席重复减 1。
		currentAgentID, err := ticketAgentIDWithLock(tx, ctx, ticketID)
		if err != nil {
			return err
		}
		if currentAgentID != nil {
			if err := tx.Exec(
				`UPDATE agents SET current_load = CASE WHEN current_load > 0 THEN current_load - 1 ELSE 0 END WHERE user_id = ?`,
				*currentAgentID,
			).Error; err != nil {
				return err
			}
		}

		updates := map[string]interface{}{
			"agent_id": nil,
		}
		if toStatus != fromStatus {
			updates["status"] = toStatus
		}
		result := applyTicketScope(tx.Model(&models.Ticket{}), ctx).Where("id = ?", ticketID).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		change := models.TicketStatus{
			TicketID:   ticketID,
			UserID:     operatorID,
			FromStatus: fromStatus,
			ToStatus:   toStatus,
			Reason:     reason,
		}
		return tx.Create(&change).Error
	})
}

func (r *GormRepository) AddComment(ctx context.Context, ticketID uint, comment *domain.Comment) error {
	model := models.TicketComment{
		TicketID:  ticketID,
		UserID:    comment.UserID,
		Content:   comment.Content,
		Type:      comment.Type,
		CreatedAt: comment.CreatedAt,
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return err
	}
	comment.ID = model.ID
	return nil
}

func (r *GormRepository) RecordStatusChange(ctx context.Context, ticketID uint, change *domain.StatusChange) error {
	model := models.TicketStatus{
		TicketID:   ticketID,
		UserID:     change.UserID,
		FromStatus: change.FromStatus,
		ToStatus:   change.ToStatus,
		Reason:     change.Reason,
		CreatedAt:  change.CreatedAt,
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return err
	}
	change.ID = model.ID
	return nil
}

func (r *GormRepository) GetTicket(ctx context.Context, ticketID uint) (*domain.Ticket, error) {
	var ticket models.Ticket
	if err := applyTicketScope(r.db.WithContext(ctx), ctx).First(&ticket, ticketID).Error; err != nil {
		return nil, err
	}
	result := mapTicket(ticket)
	return &result, nil
}

func (r *GormRepository) CustomerExists(ctx context.Context, customerID uint) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", customerID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *GormRepository) AgentAssignable(ctx context.Context, agentID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.Agent{}).
		Where("user_id = ? AND status IN ? AND current_load < max_concurrent", agentID, []string{"online", "busy"}).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *GormRepository) FindAutoAssignableAgent(ctx context.Context) (*models.Agent, error) {
	var agent models.Agent
	err := r.db.WithContext(ctx).
		Where("status = ? AND current_load < max_concurrent", "online").
		Order("current_load ASC, avg_response_time ASC").
		First(&agent).Error
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

func applyTicketScope(db *gorm.DB, ctx context.Context) *gorm.DB {
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		db = db.Where("tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	return db
}

func applyTicketScopeFields(ctx context.Context, model *models.Ticket) {
	if model == nil {
		return
	}
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		model.TenantID = tenantID
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		model.WorkspaceID = workspaceID
	}
}

func applyListTicketFilters(db *gorm.DB, query application.ListTicketsQuery) *gorm.DB {
	if len(query.Status) > 0 {
		db = db.Where("status IN ?", query.Status)
	}
	if len(query.Priority) > 0 {
		db = db.Where("priority IN ?", query.Priority)
	}
	if len(query.Category) > 0 {
		db = db.Where("category IN ?", query.Category)
	}
	if len(query.Source) > 0 {
		db = db.Where("source IN ?", query.Source)
	}
	if tag := strings.TrimSpace(query.Tag); tag != "" {
		db = db.Where("tags LIKE ?", "%"+tag+"%")
	}
	if query.AgentID != nil {
		db = db.Where("agent_id = ?", *query.AgentID)
	}
	if query.CustomerID != nil {
		db = db.Where("customer_id = ?", *query.CustomerID)
	}
	if query.SessionID != nil {
		db = db.Where("session_id = ?", *query.SessionID)
	}
	if search := strings.TrimSpace(query.Search); search != "" {
		like := "%" + search + "%"
		db = db.Where("title LIKE ? OR description LIKE ?", like, like)
	}
	if len(query.CustomFieldFilters) > 0 {
		index := 0
		for key, value := range query.CustomFieldFilters {
			index++
			aliasValue := fmt.Sprintf("tcfv_%d", index)
			aliasField := fmt.Sprintf("cf_%d", index)
			db = db.
				Joins(fmt.Sprintf("JOIN ticket_custom_field_values %s ON %s.ticket_id = tickets.id", aliasValue, aliasValue)).
				Joins(fmt.Sprintf("JOIN custom_fields %s ON %s.id = %s.custom_field_id", aliasField, aliasField, aliasValue)).
				Where(fmt.Sprintf("%s.key = ? AND %s.value = ?", aliasField, aliasValue), key, value)
		}
	}
	return db
}

func mapTicket(model models.Ticket) domain.Ticket {
	return domain.Ticket{
		ID:          model.ID,
		Title:       model.Title,
		Description: model.Description,
		CustomerID:  model.CustomerID,
		AgentID:     model.AgentID,
		SessionID:   model.SessionID,
		Category:    model.Category,
		Priority:    model.Priority,
		Status:      model.Status,
		Source:      model.Source,
		Tags:        model.Tags,
		DueDate:     model.DueDate,
		ResolvedAt:  model.ResolvedAt,
		ClosedAt:    model.ClosedAt,
		CreatedAt:   model.CreatedAt,
		UpdatedAt:   model.UpdatedAt,
	}
}

func mapTicketModel(ticket domain.Ticket) models.Ticket {
	return models.Ticket{
		ID:          ticket.ID,
		Title:       ticket.Title,
		Description: ticket.Description,
		CustomerID:  ticket.CustomerID,
		AgentID:     ticket.AgentID,
		SessionID:   ticket.SessionID,
		Category:    ticket.Category,
		Priority:    ticket.Priority,
		Status:      ticket.Status,
		Source:      ticket.Source,
		Tags:        ticket.Tags,
		DueDate:     ticket.DueDate,
		ResolvedAt:  ticket.ResolvedAt,
		ClosedAt:    ticket.ClosedAt,
		CreatedAt:   ticket.CreatedAt,
		UpdatedAt:   ticket.UpdatedAt,
	}
}

func mapTicketDetails(model models.Ticket) *domain.TicketDetails {
	details := &domain.TicketDetails{
		Ticket: mapTicket(model),
	}
	for _, value := range model.CustomFieldValues {
		details.CustomFieldValues = append(details.CustomFieldValues, domain.CustomFieldValue{
			CustomFieldID: value.CustomFieldID,
			Key:           value.CustomField.Key,
			Value:         value.Value,
		})
	}
	for _, comment := range model.Comments {
		details.Comments = append(details.Comments, domain.Comment{
			ID:        comment.ID,
			UserID:    comment.UserID,
			Content:   comment.Content,
			Type:      comment.Type,
			CreatedAt: comment.CreatedAt,
		})
	}
	for _, change := range model.StatusHistory {
		details.StatusHistory = append(details.StatusHistory, domain.StatusChange{
			ID:         change.ID,
			UserID:     change.UserID,
			FromStatus: change.FromStatus,
			ToStatus:   change.ToStatus,
			Reason:     change.Reason,
			CreatedAt:  change.CreatedAt,
		})
	}
	return details
}
