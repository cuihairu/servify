package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/modules/routing/domain"
)

type Service struct {
	repo      RoutingRepository
	publisher EventPublisher
	now       func() time.Time
}

func NewService(repo RoutingRepository, publisher EventPublisher) *Service {
	return &Service{repo: repo, publisher: publisher, now: time.Now}
}

func (s *Service) RequestHumanHandoff(ctx context.Context, cmd RequestHumanHandoffCommand) (*QueueEntryDTO, error) {
	return s.AddToWaitingQueue(ctx, AddToWaitingQueueCommand{
		SessionID:    cmd.SessionID,
		Reason:       cmd.Reason,
		TargetSkills: cmd.TargetSkills,
		Priority:     cmd.Priority,
		Notes:        cmd.Notes,
	})
}

// ClaimWaitingRecords 认领一批到期等待记录（claim-then-process）：
// 原子置 claimed_at=now，仅命中 status=waiting 且无租约或租约已过期（claimed_at < leaseBefore）
// 的记录；崩溃后租约到期自动复活。按优先级（urgent→low）+ 入队时间排序。
func (s *Service) ClaimWaitingRecords(ctx context.Context, now, leaseBefore time.Time, limit int) ([]QueueEntryDTO, error) {
	entries, err := s.repo.ClaimQueueEntries(ctx, now, leaseBefore, limit)
	if err != nil {
		return nil, err
	}
	out := make([]QueueEntryDTO, 0, len(entries))
	for _, entry := range entries {
		out = append(out, MapQueueEntry(entry))
	}
	return out, nil
}

// ReleaseQueueClaim 处理失败时归还租约（claimed_at 清空），下一轮立即可再认领。
func (s *Service) ReleaseWaitingClaim(ctx context.Context, sessionID string) error {
	return s.repo.ReleaseQueueClaim(ctx, sessionID)
}

func (s *Service) AssignAgent(ctx context.Context, cmd AssignAgentCommand) (*AssignmentDTO, error) {
	if strings.TrimSpace(cmd.SessionID) == "" {
		return nil, fmt.Errorf("session_id required")
	}
	if cmd.AgentID == 0 {
		return nil, fmt.Errorf("agent_id required")
	}
	item := &domain.Assignment{
		SessionID:      cmd.SessionID,
		FromAgentID:    cmd.FromAgentID,
		ToAgentID:      cmd.AgentID,
		Reason:         strings.TrimSpace(cmd.Reason),
		Notes:          strings.TrimSpace(cmd.Notes),
		SessionSummary: strings.TrimSpace(cmd.SessionSummary),
		AssignedAt:     cmd.AssignedAt,
	}
	if item.AssignedAt.IsZero() {
		item.AssignedAt = s.now()
	}
	if err := s.repo.CreateAssignment(ctx, item); err != nil {
		return nil, err
	}
	s.publish(ctx, RoutingAgentAssignedEventName, cmd.SessionID, MapAssignment(*item))
	s.publish(ctx, RoutingTransferCompletedEventName, cmd.SessionID, MapAssignment(*item))
	dto := MapAssignment(*item)
	return &dto, nil
}

func (s *Service) GetTransferHistory(ctx context.Context, sessionID string) ([]TransferRecordDTO, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id required")
	}
	items, err := s.repo.ListAssignments(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]TransferRecordDTO, 0, len(items))
	for _, item := range items {
		out = append(out, MapTransferRecord(item))
	}
	return out, nil
}

func (s *Service) ListRecentTransferHistory(ctx context.Context, limit int) ([]TransferRecordDTO, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	items, err := s.repo.ListRecentAssignments(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]TransferRecordDTO, 0, len(items))
	for _, item := range items {
		out = append(out, MapTransferRecord(item))
	}
	return out, nil
}

func (s *Service) AddToWaitingQueue(ctx context.Context, cmd AddToWaitingQueueCommand) (*QueueEntryDTO, error) {
	if strings.TrimSpace(cmd.SessionID) == "" {
		return nil, fmt.Errorf("session_id required")
	}
	item := &domain.QueueEntry{
		SessionID:     cmd.SessionID,
		Reason:        strings.TrimSpace(cmd.Reason),
		TargetSkills:  append([]string(nil), cmd.TargetSkills...),
		TargetGroupID: cmd.TargetGroupID,
		Priority:      strings.TrimSpace(cmd.Priority),
		Notes:         strings.TrimSpace(cmd.Notes),
		Status:        domain.QueueStatusWaiting,
		QueuedAt:      s.now(),
	}
	if err := s.repo.CreateQueueEntry(ctx, item); err != nil {
		return nil, err
	}
	dto := MapQueueEntry(*item)
	return &dto, nil
}

func (s *Service) CancelWaiting(ctx context.Context, cmd CancelWaitingCommand) (*QueueEntryDTO, error) {
	if strings.TrimSpace(cmd.SessionID) == "" {
		return nil, fmt.Errorf("session_id required")
	}
	item, err := s.repo.GetQueueEntry(ctx, cmd.SessionID)
	if err != nil {
		return nil, err
	}
	item.Status = domain.QueueStatusCancelled
	if strings.TrimSpace(cmd.Reason) != "" {
		item.Notes = strings.TrimSpace(cmd.Reason)
	}
	if err := s.repo.UpdateQueueEntry(ctx, item); err != nil {
		return nil, err
	}
	dto := MapQueueEntry(*item)
	return &dto, nil
}

func (s *Service) ListWaitingEntries(ctx context.Context, status string, limit int) ([]QueueEntryDTO, error) {
	if strings.TrimSpace(status) == "" {
		status = string(domain.QueueStatusWaiting)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	items, err := s.repo.ListQueueEntries(ctx, status, limit)
	if err != nil {
		return nil, err
	}
	out := make([]QueueEntryDTO, 0, len(items))
	for _, item := range items {
		out = append(out, MapQueueEntry(item))
	}
	return out, nil
}

func (s *Service) GetWaitingEntry(ctx context.Context, sessionID string) (*QueueEntryDTO, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id required")
	}
	item, err := s.repo.GetQueueEntry(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	dto := MapQueueEntry(*item)
	return &dto, nil
}

func (s *Service) MarkWaitingTransferred(ctx context.Context, cmd MarkWaitingTransferredCommand) (*QueueEntryDTO, error) {
	if strings.TrimSpace(cmd.SessionID) == "" {
		return nil, fmt.Errorf("session_id required")
	}
	if cmd.AssignedTo == 0 {
		return nil, fmt.Errorf("assigned_to required")
	}
	at := cmd.AssignedAt
	if at.IsZero() {
		at = s.now()
	}
	item, err := s.repo.MarkQueueEntryTransferred(ctx, cmd.SessionID, cmd.AssignedTo, at)
	if err != nil {
		return nil, err
	}
	dto := MapQueueEntry(*item)
	return &dto, nil
}

func (s *Service) publish(ctx context.Context, name, sessionID string, payload interface{}) {
	if s.publisher == nil {
		return
	}
	_ = s.publisher.Publish(ctx, NewRoutingEvent(name, sessionID, payload))
}
