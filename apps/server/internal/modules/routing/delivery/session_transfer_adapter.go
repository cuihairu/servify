package delivery

import (
	"context"
	"time"

	"servify/apps/server/internal/models"
	routingapp "servify/apps/server/internal/modules/routing/application"
	routinginfra "servify/apps/server/internal/modules/routing/infra"

	"gorm.io/gorm"
)

type SessionTransferAdapter struct {
	service   *routingapp.Service
	publisher routingapp.EventPublisher
}

func NewSessionTransferAdapter(service *routingapp.Service, publisher routingapp.EventPublisher) *SessionTransferAdapter {
	return &SessionTransferAdapter{service: service, publisher: publisher}
}

func (a *SessionTransferAdapter) AssignAgent(ctx context.Context, tx *gorm.DB, cmd AssignAgentCommand) (*models.TransferRecord, error) {
	svc := a.service
	if tx != nil {
		svc = routingapp.NewService(routinginfra.NewGormRepository(tx), a.publisher)
	}

	item, err := svc.AssignAgent(ctx, routingapp.AssignAgentCommand{
		SessionID:      cmd.SessionID,
		AgentID:        cmd.AgentID,
		FromAgentID:    cmd.FromAgentID,
		Reason:         cmd.Reason,
		Notes:          cmd.Notes,
		SessionSummary: cmd.SessionSummary,
		AssignedAt:     cmd.AssignedAt,
	})
	if err != nil {
		return nil, err
	}

	return &models.TransferRecord{
		SessionID:      item.SessionID,
		FromAgentID:    item.FromAgentID,
		ToAgentID:      uintPtr(item.ToAgentID),
		Reason:         item.Reason,
		Notes:          item.Notes,
		SessionSummary: item.SessionSummary,
		TransferredAt:  item.AssignedAt,
		CreatedAt:      item.AssignedAt,
	}, nil
}

func (a *SessionTransferAdapter) AddToWaitingQueue(
	ctx context.Context,
	tx *gorm.DB,
	sessionID string,
	reason string,
	targetSkills []string,
	targetGroupID uint,
	priority string,
	notes string,
) (*models.WaitingRecord, error) {
	svc := a.service
	if tx != nil {
		svc = routingapp.NewService(routinginfra.NewGormRepository(tx), a.publisher)
	}

	entry, err := svc.AddToWaitingQueue(ctx, routingapp.AddToWaitingQueueCommand{
		SessionID:     sessionID,
		Reason:        reason,
		TargetSkills:  targetSkills,
		TargetGroupID: targetGroupID,
		Priority:      priority,
		Notes:         notes,
	})
	if err != nil {
		return nil, err
	}
	return mapWaitingRecord(entry), nil
}

// ClaimWaitingRecords 认领到期等待记录（租约内不重复派发）。
func (a *SessionTransferAdapter) ClaimWaitingRecords(ctx context.Context, now, leaseBefore time.Time, limit int) ([]models.WaitingRecord, error) {
	svc := a.service
	entries, err := svc.ClaimWaitingRecords(ctx, now, leaseBefore, limit)
	if err != nil {
		return nil, err
	}
	records := make([]models.WaitingRecord, 0, len(entries))
	for _, entry := range entries {
		records = append(records, mapQueueWaitingRecord(entry))
	}
	return records, nil
}

// ReleaseWaitingClaim 处理失败归还租约。
func (a *SessionTransferAdapter) ReleaseWaitingClaim(ctx context.Context, sessionID string) error {
	return a.service.ReleaseWaitingClaim(ctx, sessionID)
}

func (a *SessionTransferAdapter) GetTransferHistory(ctx context.Context, sessionID string) ([]models.TransferRecord, error) {
	items, err := a.service.GetTransferHistory(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]models.TransferRecord, 0, len(items))
	for _, item := range items {
		out = append(out, mapTransferRecord(item))
	}
	return out, nil
}

func (a *SessionTransferAdapter) ListRecentTransferHistory(ctx context.Context, limit int) ([]models.TransferRecord, error) {
	items, err := a.service.ListRecentTransferHistory(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]models.TransferRecord, 0, len(items))
	for _, item := range items {
		out = append(out, mapTransferRecord(item))
	}
	return out, nil
}

func (a *SessionTransferAdapter) ListWaitingRecords(ctx context.Context, status string, limit int) ([]models.WaitingRecord, error) {
	items, err := a.service.ListWaitingEntries(ctx, status, limit)
	if err != nil {
		return nil, err
	}
	out := make([]models.WaitingRecord, 0, len(items))
	for _, item := range items {
		out = append(out, *mapWaitingRecord(&item))
	}
	return out, nil
}

func (a *SessionTransferAdapter) GetWaitingRecord(ctx context.Context, sessionID string) (*models.WaitingRecord, error) {
	entry, err := a.service.GetWaitingEntry(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return mapWaitingRecord(entry), nil
}

func (a *SessionTransferAdapter) CancelWaiting(ctx context.Context, tx *gorm.DB, sessionID string, reason string) (*models.WaitingRecord, error) {
	svc := a.service
	if tx != nil {
		svc = routingapp.NewService(routinginfra.NewGormRepository(tx), a.publisher)
	}

	entry, err := svc.CancelWaiting(ctx, routingapp.CancelWaitingCommand{
		SessionID: sessionID,
		Reason:    reason,
	})
	if err != nil {
		return nil, err
	}
	return mapWaitingRecord(entry), nil
}

func (a *SessionTransferAdapter) MarkWaitingTransferred(ctx context.Context, tx *gorm.DB, sessionID string, agentID uint, assignedAt time.Time) (*models.WaitingRecord, error) {
	svc := a.service
	if tx != nil {
		svc = routingapp.NewService(routinginfra.NewGormRepository(tx), a.publisher)
	}

	entry, err := svc.MarkWaitingTransferred(ctx, routingapp.MarkWaitingTransferredCommand{
		SessionID:  sessionID,
		AssignedTo: agentID,
		AssignedAt: assignedAt,
	})
	if err != nil {
		return nil, err
	}
	return mapWaitingRecord(entry), nil
}

func mapWaitingRecord(item *routingapp.QueueEntryDTO) *models.WaitingRecord {
	if item == nil {
		return nil
	}
	return &models.WaitingRecord{
		SessionID:     item.SessionID,
		Reason:        item.Reason,
		TargetSkills:  marshalSkills(item.TargetSkills),
		TargetGroupID: nilableGroupID(item.TargetGroupID),
		Priority:      item.Priority,
		Notes:         item.Notes,
		Status:        item.Status,
		QueuedAt:      item.QueuedAt,
		AssignedAt:    item.AssignedAt,
		AssignedTo:    item.AssignedTo,
		CreatedAt:     item.QueuedAt,
	}
}

// mapQueueWaitingRecord 认领结果 DTO → 持久模型（claim 流程读字段用）。
func mapQueueWaitingRecord(item routingapp.QueueEntryDTO) models.WaitingRecord {
	return *mapWaitingRecord(&item)
}

func nilableGroupID(id uint) *uint {
	if id == 0 {
		return nil
	}
	return &id
}

func marshalSkills(skills []string) string {
	if len(skills) == 0 {
		return ""
	}
	// legacy waiting records previously stored comma-separated values
	return joinSkills(skills)
}

func joinSkills(skills []string) string {
	if len(skills) == 0 {
		return ""
	}
	out := ""
	for i, skill := range skills {
		if i > 0 {
			out += ","
		}
		out += skill
	}
	return out
}

func cloneTimePtr(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	t := *in
	return &t
}

func mapTransferRecord(item routingapp.TransferRecordDTO) models.TransferRecord {
	return models.TransferRecord{
		SessionID:      item.SessionID,
		FromAgentID:    item.FromAgentID,
		ToAgentID:      item.ToAgentID,
		Reason:         item.Reason,
		Notes:          item.Notes,
		SessionSummary: item.SessionSummary,
		TransferredAt:  item.TransferredAt,
		CreatedAt:      item.TransferredAt,
	}
}

func uintPtr(v uint) *uint {
	return &v
}
