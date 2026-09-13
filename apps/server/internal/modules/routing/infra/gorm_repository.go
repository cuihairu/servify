package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/routing/domain"
	platformauth "servify/apps/server/internal/platform/auth"
)

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) CreateAssignment(ctx context.Context, assignment *domain.Assignment) error {
	if assignment == nil {
		return fmt.Errorf("assignment required")
	}
	model := mapTransferRecordModel(*assignment)
	applyRoutingTransferScopeFields(ctx, &model)
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return err
	}
	assignment.AssignedAt = model.TransferredAt
	return nil
}

func (r *GormRepository) ListAssignments(ctx context.Context, sessionID string) ([]domain.TransferRecord, error) {
	var items []models.TransferRecord
	if err := applyRoutingScope(r.db.WithContext(ctx), ctx).
		Where("session_id = ?", sessionID).
		Order("transferred_at DESC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]domain.TransferRecord, 0, len(items))
	for _, item := range items {
		out = append(out, mapTransferRecord(item))
	}
	return out, nil
}

func (r *GormRepository) ListRecentAssignments(ctx context.Context, limit int) ([]domain.TransferRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var items []models.TransferRecord
	if err := applyRoutingScope(r.db.WithContext(ctx), ctx).
		Order("transferred_at DESC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]domain.TransferRecord, 0, len(items))
	for _, item := range items {
		out = append(out, mapTransferRecord(item))
	}
	return out, nil
}

func (r *GormRepository) CreateQueueEntry(ctx context.Context, entry *domain.QueueEntry) error {
	if entry == nil {
		return fmt.Errorf("queue entry required")
	}
	model := mapWaitingRecordModel(*entry)
	applyRoutingWaitingScopeFields(ctx, &model)
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return err
	}
	*entry = mapQueueEntry(model)
	return nil
}

func (r *GormRepository) GetQueueEntry(ctx context.Context, sessionID string) (*domain.QueueEntry, error) {
	var model models.WaitingRecord
	if err := applyRoutingScope(r.db.WithContext(ctx), ctx).
		Order("queued_at DESC").
		First(&model, "session_id = ?", sessionID).Error; err != nil {
		return nil, err
	}
	item := mapQueueEntry(model)
	return &item, nil
}

func (r *GormRepository) ListQueueEntries(ctx context.Context, status string, limit int) ([]domain.QueueEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var items []models.WaitingRecord
	if err := applyRoutingScope(r.db.WithContext(ctx), ctx).
		Where("status = ?", status).
		Order("priority DESC, queued_at ASC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]domain.QueueEntry, 0, len(items))
	for _, item := range items {
		out = append(out, mapQueueEntry(item))
	}
	return out, nil
}

func (r *GormRepository) UpdateQueueEntry(ctx context.Context, entry *domain.QueueEntry) error {
	if entry == nil {
		return fmt.Errorf("queue entry required")
	}
	updates := map[string]interface{}{
		"reason":        entry.Reason,
		"target_skills": marshalSkills(entry.TargetSkills),
		"priority":      entry.Priority,
		"notes":         entry.Notes,
		"status":        string(entry.Status),
		"queued_at":     entry.QueuedAt,
		"assigned_at":   entry.AssignedAt,
		"assigned_to":   entry.AssignedTo,
	}
	return applyRoutingScope(r.db.WithContext(ctx).Model(&models.WaitingRecord{}), ctx).
		Model(&models.WaitingRecord{}).
		Where("session_id = ?", entry.SessionID).
		Updates(updates).Error
}

// ClaimQueueEntries 原子认领：单条 UPDATE ... WHERE id IN (子查询) RETURNING *，
// pg 与 sqlite（3.35+）通用；并发 worker 只会命中其中之一（行级 UPDATE 串行化）。
func (r *GormRepository) ClaimQueueEntries(ctx context.Context, now, leaseBefore time.Time, limit int) ([]domain.QueueEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 10
	}
	var items []models.WaitingRecord
	sql := `UPDATE waiting_records SET claimed_at = ?
		WHERE id IN (
			SELECT id FROM waiting_records
			WHERE status = 'waiting'
			  AND (claimed_at IS NULL OR claimed_at < ?)
			ORDER BY CASE priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END, queued_at ASC
			LIMIT ?
		)
		RETURNING *`
	if err := r.db.WithContext(ctx).Raw(sql, now, leaseBefore, limit).Scan(&items).Error; err != nil {
		return nil, fmt.Errorf("claim waiting records: %w", err)
	}
	out := make([]domain.QueueEntry, 0, len(items))
	for _, item := range items {
		out = append(out, mapQueueEntry(item))
	}
	return out, nil
}

// ReleaseQueueClaim 处理失败归还租约；仅清 claimed_at，不动 status（waiting 原样）。
func (r *GormRepository) ReleaseQueueClaim(ctx context.Context, sessionID string) error {
	return r.db.WithContext(ctx).Model(&models.WaitingRecord{}).
		Where("session_id = ? AND status = ?", sessionID, string(domain.QueueStatusWaiting)).
		Update("claimed_at", nil).Error
}

func (r *GormRepository) MarkQueueEntryTransferred(ctx context.Context, sessionID string, agentID uint, assignedAt time.Time) (*domain.QueueEntry, error) {
	result := applyRoutingScope(r.db.WithContext(ctx).Model(&models.WaitingRecord{}), ctx).
		Where("session_id = ? AND status = ?", sessionID, "waiting").
		Updates(map[string]interface{}{
			"status":      string(domain.QueueStatusTransferred),
			"assigned_at": assignedAt,
			"assigned_to": agentID,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return r.GetQueueEntry(ctx, sessionID)
}

func mapTransferRecordModel(item domain.Assignment) models.TransferRecord {
	return models.TransferRecord{
		SessionID:      item.SessionID,
		FromAgentID:    item.FromAgentID,
		ToAgentID:      uintPtr(item.ToAgentID),
		Reason:         item.Reason,
		Notes:          item.Notes,
		SessionSummary: item.SessionSummary,
		TransferredAt:  item.AssignedAt,
		CreatedAt:      item.AssignedAt,
	}
}

func mapTransferRecord(model models.TransferRecord) domain.TransferRecord {
	return domain.TransferRecord{
		SessionID:      model.SessionID,
		FromAgentID:    model.FromAgentID,
		ToAgentID:      model.ToAgentID,
		Reason:         model.Reason,
		Notes:          model.Notes,
		SessionSummary: model.SessionSummary,
		TransferredAt:  model.TransferredAt,
	}
}

func mapQueueEntry(model models.WaitingRecord) domain.QueueEntry {
	return domain.QueueEntry{
		SessionID:     model.SessionID,
		Reason:        model.Reason,
		TargetSkills:  unmarshalSkills(model.TargetSkills),
		TargetGroupID: derefGroupID(model.TargetGroupID),
		Priority:      model.Priority,
		Notes:         model.Notes,
		Status:        mapQueueStatus(model.Status),
		QueuedAt:      model.QueuedAt,
		ClaimedAt:     model.ClaimedAt,
		AssignedAt:    model.AssignedAt,
		AssignedTo:    model.AssignedTo,
	}
}

func mapWaitingRecordModel(item domain.QueueEntry) models.WaitingRecord {
	return models.WaitingRecord{
		SessionID:     item.SessionID,
		Reason:        item.Reason,
		TargetSkills:  marshalSkills(item.TargetSkills),
		TargetGroupID: nilableGroupID(item.TargetGroupID),
		Priority:      item.Priority,
		Notes:         item.Notes,
		Status:        string(item.Status),
		QueuedAt:      item.QueuedAt,
		ClaimedAt:     item.ClaimedAt,
		AssignedAt:    item.AssignedAt,
		AssignedTo:    item.AssignedTo,
		CreatedAt:     item.QueuedAt,
	}
}

func nilableGroupID(id uint) *uint {
	if id == 0 {
		return nil
	}
	return &id
}

func derefGroupID(id *uint) uint {
	if id == nil {
		return 0
	}
	return *id
}

func mapQueueStatus(status string) domain.QueueStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "transferred":
		return domain.QueueStatusTransferred
	case "cancelled":
		return domain.QueueStatusCancelled
	default:
		return domain.QueueStatusWaiting
	}
}

func applyRoutingScope(db *gorm.DB, ctx context.Context) *gorm.DB {
	tenantID := platformauth.TenantIDFromContext(ctx)
	workspaceID := platformauth.WorkspaceIDFromContext(ctx)
	if tenantID != "" {
		db = db.Where("tenant_id = ?", tenantID)
	}
	if workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	return db
}

func applyRoutingTransferScopeFields(ctx context.Context, model *models.TransferRecord) {
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

func applyRoutingWaitingScopeFields(ctx context.Context, model *models.WaitingRecord) {
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

// marshalSkillsJSON 为测试注入点（默认即 json.Marshal）：
// []string 序列化恒成功，错误分支仅经注入触发。
var marshalSkillsJSON = json.Marshal

func marshalSkills(skills []string) string {
	if len(skills) == 0 {
		return ""
	}
	data, err := marshalSkillsJSON(skills)
	if err != nil {
		return ""
	}
	return string(data)
}

func unmarshalSkills(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var skills []string
	if err := json.Unmarshal([]byte(raw), &skills); err == nil && len(skills) > 0 {
		return skills
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func uintPtr(v uint) *uint {
	return &v
}
