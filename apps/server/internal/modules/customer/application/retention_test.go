package application

// V1.0 收敛 B2-2b（docs/v1-convergence-plan.md §9.3-2）：保留清理服务的
// 单元行为——Enabled 判定、未启用零值短路、截止时间换算、逐级错误传播。

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubRetentionRepo struct {
	msgs, tickets, comments, files         int64
	msgErr, ticketErr, commentErr, fileErr error
	calls                                  int
}

func (s *stubRetentionRepo) ScrubMessagesInSessionsEndedBefore(context.Context, time.Time, string) (int64, error) {
	s.calls++
	if s.msgErr != nil {
		return 0, s.msgErr
	}
	return s.msgs, nil
}

func (s *stubRetentionRepo) ScrubTicketsClosedBefore(context.Context, time.Time, string) (int64, error) {
	if s.ticketErr != nil {
		return 0, s.ticketErr
	}
	return s.tickets, nil
}

func (s *stubRetentionRepo) ScrubCommentsOnTicketsClosedBefore(context.Context, time.Time, string) (int64, error) {
	if s.commentErr != nil {
		return 0, s.commentErr
	}
	return s.comments, nil
}

func (s *stubRetentionRepo) DeleteFilesOnTicketsClosedBefore(context.Context, time.Time) (int64, error) {
	if s.fileErr != nil {
		return 0, s.fileErr
	}
	return s.files, nil
}

func TestRetentionServiceEnabled(t *testing.T) {
	if (*RetentionService)(nil).Enabled() {
		t.Fatal("nil service must be disabled")
	}
	if (*RetentionService)(nil).WithClock(func() time.Time { return time.Now() }) != nil {
		t.Fatal("nil receiver must chain nil")
	}
	if (&RetentionService{}).Enabled() {
		t.Fatal("no repo must be disabled")
	}
	if NewRetentionService(&stubRetentionRepo{}, 0).Enabled() {
		t.Fatal("retention_days=0 must be disabled")
	}
	if !NewRetentionService(&stubRetentionRepo{}, 30).Enabled() {
		t.Fatal("repo + retention_days>0 must be enabled")
	}
}

func TestRetentionServiceDisabledIsZeroValueNoTouch(t *testing.T) {
	repo := &stubRetentionRepo{}
	result, err := NewRetentionService(repo, 0).ScrubExpiredContent(context.Background())
	if err != nil {
		t.Fatalf("disabled run: %v", err)
	}
	if result == nil || result.RetentionDays != 0 || result.CutoffAt.IsZero() == false {
		t.Fatalf("disabled result = %+v（cutoff 不应被设置）", result)
	}
	if repo.calls != 0 {
		t.Fatalf("disabled run must not touch repo, got %d calls", repo.calls)
	}
}

func TestRetentionServiceScrubRunsAllSteps(t *testing.T) {
	repo := &stubRetentionRepo{msgs: 5, tickets: 2, comments: 7, files: 1}
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	svc := NewRetentionService(repo, 30).WithClock(func() time.Time { return now })
	result, err := svc.ScrubExpiredContent(context.Background())
	if err != nil {
		t.Fatalf("scrub: %v", err)
	}
	if result.RetentionDays != 30 {
		t.Fatalf("retention_days = %d", result.RetentionDays)
	}
	if !result.CutoffAt.Equal(now.AddDate(0, 0, -30)) {
		t.Fatalf("cutoff = %v", result.CutoffAt)
	}
	if result.MessagesScrubbed != 5 || result.TicketsScrubbed != 2 ||
		result.CommentsScrubbed != 7 || result.FilesDeleted != 1 {
		t.Fatalf("counts = %+v", result)
	}
}

func TestRetentionServiceErrorPropagation(t *testing.T) {
	sentinel := errors.New("boom")
	cases := []struct {
		name string
		mut  func(*stubRetentionRepo)
	}{
		{"messages", func(r *stubRetentionRepo) { r.msgErr = sentinel }},
		{"tickets", func(r *stubRetentionRepo) { r.ticketErr = sentinel }},
		{"comments", func(r *stubRetentionRepo) { r.commentErr = sentinel }},
		{"files", func(r *stubRetentionRepo) { r.fileErr = sentinel }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubRetentionRepo{}
			tc.mut(repo)
			if _, err := NewRetentionService(repo, 30).ScrubExpiredContent(context.Background()); !errors.Is(err, sentinel) {
				t.Fatalf("err = %v want sentinel", err)
			}
		})
	}
}
