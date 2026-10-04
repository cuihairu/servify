package application

import (
	"context"
	"time"
)

// RetentionRepository 数据保留清理口（V1.0 B2-2b，计划书 §9.3-2）：
// 按时间窗口批量擦除已结束会话的消息内容与已关闭工单的内容，
// 不硬删业务行（与数据主体删除面同一口径）。
type RetentionRepository interface {
	// ScrubMessagesInSessionsEndedBefore 擦除 ended_at 早于 before 的
	// 会话下非空消息内容，返回实际擦除行数。
	ScrubMessagesInSessionsEndedBefore(ctx context.Context, before time.Time, replacement string) (int64, error)
	// ScrubTicketsClosedBefore 擦除 closed_at 早于 before 的工单标题/
	// 描述/AI 摘要/标签，返回实际擦除行数。
	ScrubTicketsClosedBefore(ctx context.Context, before time.Time, replacement string) (int64, error)
	// ScrubCommentsOnTicketsClosedBefore 擦除上述工单的评论内容。
	ScrubCommentsOnTicketsClosedBefore(ctx context.Context, before time.Time, replacement string) (int64, error)
	// DeleteFilesOnTicketsClosedBefore 删除上述工单的附件元数据。
	DeleteFilesOnTicketsClosedBefore(ctx context.Context, before time.Time) (int64, error)
}

// RetentionResult 单轮保留清理的命中统计。
type RetentionResult struct {
	RetentionDays    int       `json:"retention_days"`
	CutoffAt         time.Time `json:"cutoff_at"`
	MessagesScrubbed int64     `json:"messages_scrubbed"`
	TicketsScrubbed  int64     `json:"tickets_scrubbed"`
	CommentsScrubbed int64     `json:"comments_scrubbed"`
	FilesDeleted     int64     `json:"files_deleted"`
}

// RetentionService 时间驱动的数据保留清理（B2-2b）：privacy.retention_days
// 开启后由后台 worker 周期调用 ScrubExpiredContent。
type RetentionService struct {
	repo          RetentionRepository
	retentionDays int
	now           func() time.Time
}

func NewRetentionService(repo RetentionRepository, retentionDays int) *RetentionService {
	return &RetentionService{repo: repo, retentionDays: retentionDays, now: time.Now}
}

// WithClock 注入时钟（测试用）；可链式。
func (s *RetentionService) WithClock(now func() time.Time) *RetentionService {
	if s == nil {
		return s
	}
	s.now = now
	return s
}

// Enabled 保留策略是否开启（RetentionDays>0 且仓储可用）。
func (s *RetentionService) Enabled() bool {
	return s != nil && s.repo != nil && s.retentionDays > 0
}

// ScrubExpiredContent 执行一轮过期内容清理；未启用时返回零值结果不触碰库。
func (s *RetentionService) ScrubExpiredContent(ctx context.Context) (*RetentionResult, error) {
	result := &RetentionResult{RetentionDays: s.retentionDays}
	if !s.Enabled() {
		return result, nil
	}
	cutoff := s.now().AddDate(0, 0, -s.retentionDays)
	result.CutoffAt = cutoff
	var err error
	if result.MessagesScrubbed, err = s.repo.ScrubMessagesInSessionsEndedBefore(ctx, cutoff, ErasureErasedText); err != nil {
		return nil, err
	}
	if result.TicketsScrubbed, err = s.repo.ScrubTicketsClosedBefore(ctx, cutoff, ErasureErasedText); err != nil {
		return nil, err
	}
	if result.CommentsScrubbed, err = s.repo.ScrubCommentsOnTicketsClosedBefore(ctx, cutoff, ErasureErasedText); err != nil {
		return nil, err
	}
	if result.FilesDeleted, err = s.repo.DeleteFilesOnTicketsClosedBefore(ctx, cutoff); err != nil {
		return nil, err
	}
	return result, nil
}
