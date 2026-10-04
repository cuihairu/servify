package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/modules/routing/domain"
	svcmetrics "servify/apps/server/internal/observability/metrics"
)

type Service struct {
	repo      RoutingRepository
	publisher EventPublisher
	now       func() time.Time
	scorer    Scorer // B2-1：nil 时 AssignAgent 跳过评分落库、RecommendAgents 报错
	metrics   *svcmetrics.BusinessMetrics
}

func NewService(repo RoutingRepository, publisher EventPublisher) *Service {
	return &Service{repo: repo, publisher: publisher, now: time.Now}
}

// WithScorer 注入打分引擎（B2-1）。可链式；nil 输入等价未注入。
func (s *Service) WithScorer(scorer Scorer) *Service {
	if s == nil {
		return s
	}
	s.scorer = scorer
	return s
}

// AttachBusinessMetrics 注入进程级业务指标（nil 安全，可链式）。
// 路由决策计数 routing_decisions_total{tenant_id, strategy, outcome}：
// strategy ∈ handoff（入队等待）/ assign（定向指派）/ transfer（等待队列转出）。
func (s *Service) AttachBusinessMetrics(m *svcmetrics.BusinessMetrics) *Service {
	if s == nil {
		return s
	}
	s.metrics = m
	return s
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
	s.metrics.RecordRoutingDecision("default", "assign", "success")
	if cmd.Scoring != nil {
		scoring := &domain.RoutingAssignment{
			SessionID:   cmd.SessionID,
			FromAgentID: cmd.FromAgentID,
			ToAgentID:   cmd.AgentID,
			TotalScore:  cmd.Scoring.TotalScore,
			Factors:     cmd.Scoring.Factors,
			Reasons:     cmd.Scoring.Reasons,
			Strategy:    cmd.Scoring.Strategy,
			AssignedAt:  item.AssignedAt,
		}
		if err := s.repo.CreateRoutingAssignment(ctx, scoring); err != nil {
			return nil, fmt.Errorf("persist scoring audit: %w", err)
		}
	}
	s.publish(ctx, RoutingAgentAssignedEventName, cmd.SessionID, MapAssignment(*item))
	s.publish(ctx, RoutingTransferCompletedEventName, cmd.SessionID, MapAssignment(*item))
	dto := MapAssignment(*item)
	return &dto, nil
}

// RecommendAgents 返回按分排序的候选序列（B2-1 §6.2-3）：只推荐不执行，
// 与 handoff 语义一致——评分不影响人工/手动优先的分配决策。
func (s *Service) RecommendAgents(ctx context.Context, input ScoringInput) ([]ScoredCandidate, error) {
	if s.scorer == nil {
		return nil, fmt.Errorf("routing scorer is not configured")
	}
	return s.scorer.Score(ctx, input)
}

// ListRoutingAssignments 管理面读写口：按会话读评分审计（计划书 §6.3-3，
// 分数与因子可见），limit 默认 50 上限 200。
func (s *Service) ListRoutingAssignments(ctx context.Context, sessionID string, limit int) ([]RoutingAssignmentDTO, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	items, err := s.repo.ListRoutingAssignments(ctx, sessionID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]RoutingAssignmentDTO, 0, len(items))
	for _, item := range items {
		out = append(out, MapRoutingAssignment(item))
	}
	return out, nil
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
	s.metrics.RecordRoutingDecision("default", "handoff", "queued")
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
	s.metrics.RecordRoutingDecision("default", "transfer", "transferred")
	dto := MapQueueEntry(*item)
	return &dto, nil
}

func (s *Service) publish(ctx context.Context, name, sessionID string, payload interface{}) {
	if s.publisher == nil {
		return
	}
	_ = s.publisher.Publish(ctx, NewRoutingEvent(name, sessionID, payload))
}
