package application

import (
	"context"
	"errors"
	"hash/fnv"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

// scriptedQualityRepo 组合 fakeRepo 并支持按方法注入错误，用于覆盖服务层错误分支。
type scriptedQualityRepo struct {
	*fakeRepo

	candidatesErr   error
	retryErr        error
	messagesErr     error
	insertErr       error
	insertAbsent    bool
	markFailedErr   error
	markFailedCalls int
	getErr          error
	confirmErr      error
	rescheduleErr   error
	rescheduleOK    bool
	listErr         error
}

func (s *scriptedQualityRepo) ListReviewCandidates(ctx context.Context, lookback time.Time, limit int) ([]models.Session, error) {
	if s.candidatesErr != nil {
		return nil, s.candidatesErr
	}
	return s.fakeRepo.ListReviewCandidates(ctx, lookback, limit)
}

func (s *scriptedQualityRepo) ListReviewsForRetry(ctx context.Context, maxAttempts int, now time.Time, limit int) ([]models.QualityReview, error) {
	if s.retryErr != nil {
		return nil, s.retryErr
	}
	return s.fakeRepo.ListReviewsForRetry(ctx, maxAttempts, now, limit)
}

func (s *scriptedQualityRepo) ListMessages(ctx context.Context, sessionID string) ([]models.Message, error) {
	if s.messagesErr != nil {
		return nil, s.messagesErr
	}
	return s.fakeRepo.ListMessages(ctx, sessionID)
}

func (s *scriptedQualityRepo) InsertReviewIfAbsent(ctx context.Context, review *models.QualityReview) (bool, error) {
	if s.insertErr != nil {
		return false, s.insertErr
	}
	if s.insertAbsent {
		return false, nil
	}
	return s.fakeRepo.InsertReviewIfAbsent(ctx, review)
}

func (s *scriptedQualityRepo) MarkReviewFailed(ctx context.Context, sessionID string, allowedFrom []string, attempts int, nextRetry time.Time, lastErr string) (bool, error) {
	s.markFailedCalls++
	if s.markFailedErr != nil {
		return false, s.markFailedErr
	}
	return s.fakeRepo.MarkReviewFailed(ctx, sessionID, allowedFrom, attempts, nextRetry, lastErr)
}

func (s *scriptedQualityRepo) GetReviewBySession(ctx context.Context, sessionID string) (*models.QualityReview, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.fakeRepo.GetReviewBySession(ctx, sessionID)
}

func (s *scriptedQualityRepo) ConfirmReview(ctx context.Context, sessionID string, cmd ConfirmCommand) (bool, error) {
	if s.confirmErr != nil {
		return false, s.confirmErr
	}
	return s.fakeRepo.ConfirmReview(ctx, sessionID, cmd)
}

func (s *scriptedQualityRepo) RescheduleReview(ctx context.Context, sessionID string, force bool) (bool, error) {
	if s.rescheduleErr != nil {
		return false, s.rescheduleErr
	}
	if !s.rescheduleOK {
		return false, nil
	}
	return s.fakeRepo.RescheduleReview(ctx, sessionID, force)
}

func (s *scriptedQualityRepo) ListReviews(ctx context.Context, query ReviewListQuery) ([]models.QualityReview, int64, error) {
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	return s.fakeRepo.ListReviews(ctx, query)
}

func TestRunScanCandidateListingError(t *testing.T) {
	f := &scriptedQualityRepo{fakeRepo: newFakeRepo(), candidatesErr: errors.New("candidates boom")}
	svc := NewQualityService(f, nil, testConfig(), nil)

	n, err := svc.RunScan(context.Background())
	if n != 0 || err == nil || err.Error() != "list review candidates: candidates boom" {
		t.Fatalf("RunScan() = %d, %v; want wrapped candidate listing error", n, err)
	}
}

func TestRunScanRetryListingError(t *testing.T) {
	f := &scriptedQualityRepo{fakeRepo: newFakeRepo(), retryErr: errors.New("retry boom")}
	svc := NewQualityService(f, nil, testConfig(), nil)

	n, err := svc.RunScan(context.Background())
	if n != 0 || err == nil || err.Error() != "list reviews for retry: retry boom" {
		t.Fatalf("RunScan() = %d, %v; want wrapped retry listing error", n, err)
	}
}

func TestServiceListReviewsAndGetReviewDelegation(t *testing.T) {
	ctx := context.Background()
	f := &scriptedQualityRepo{fakeRepo: newFakeRepo()}
	svc := NewQualityService(f, nil, testConfig(), nil)

	items, total, err := svc.ListReviews(ctx, ReviewListQuery{Status: StatusPending})
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("ListReviews() = %+v, %d, %v", items, total, err)
	}
	if _, err := svc.GetReviewBySession(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetReviewBySession() error = %v, want ErrNotFound", err)
	}

	seedNormalConversation(f.fakeRepo, "s-list", time.Now().Add(-time.Hour))
	if _, err := svc.RunScan(ctx); err != nil {
		t.Fatalf("RunScan() error = %v", err)
	}
	review, err := svc.GetReviewBySession(ctx, "s-list")
	if err != nil || review.SessionID != "s-list" {
		t.Fatalf("GetReviewBySession() = %+v, %v", review, err)
	}
	items, total, err = svc.ListReviews(ctx, ReviewListQuery{})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("ListReviews() = %+v, %d, %v", items, total, err)
	}
}

func TestServiceListReviewsErrorPropagates(t *testing.T) {
	f := &scriptedQualityRepo{fakeRepo: newFakeRepo(), listErr: errors.New("list boom")}
	svc := NewQualityService(f, nil, testConfig(), nil)
	if _, _, err := svc.ListReviews(context.Background(), ReviewListQuery{}); err == nil || err.Error() != "list boom" {
		t.Fatalf("want raw list error, got %v", err)
	}
}

func TestConfirmReviewRepoError(t *testing.T) {
	f := &scriptedQualityRepo{fakeRepo: newFakeRepo(), confirmErr: errors.New("confirm boom")}
	seedNormalConversation(f.fakeRepo, "s-confirm-err", time.Now().Add(-time.Hour))
	svc := NewQualityService(f, nil, testConfig(), nil)
	ctx := context.Background()

	if _, err := svc.RunScan(ctx); err != nil {
		t.Fatalf("RunScan() error = %v", err)
	}
	if err := svc.ConfirmReview(ctx, "s-confirm-err", ConfirmCommand{}); err == nil || err.Error() != "confirm boom" {
		t.Fatalf("want raw confirm error, got %v", err)
	}
}

func TestRescoreReviewBranches(t *testing.T) {
	ctx := context.Background()
	f := &scriptedQualityRepo{fakeRepo: newFakeRepo()}
	seedNormalConversation(f.fakeRepo, "s-rescore-err", time.Now().Add(-time.Hour))
	svc := NewQualityService(f, nil, testConfig(), nil)
	if _, err := svc.RunScan(ctx); err != nil {
		t.Fatalf("RunScan() error = %v", err)
	}

	// GetReviewBySession 失败透传
	f.getErr = errors.New("get boom")
	if err := svc.RescoreReview(ctx, "s-rescore-err", false); err == nil || err.Error() != "get boom" {
		t.Fatalf("want raw get error, got %v", err)
	}
	f.getErr = nil

	// RescheduleReview 失败透传
	f.rescheduleErr = errors.New("reschedule boom")
	if err := svc.RescoreReview(ctx, "s-rescore-err", false); err == nil || err.Error() != "reschedule boom" {
		t.Fatalf("want raw reschedule error, got %v", err)
	}
	f.rescheduleErr = nil

	// RescheduleReview 未命中 → ErrNotFound
	f.rescheduleOK = false
	if err := svc.RescoreReview(ctx, "s-rescore-err", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestProcessCandidateMessageListingError(t *testing.T) {
	f := &scriptedQualityRepo{fakeRepo: newFakeRepo(), messagesErr: errors.New("messages boom")}
	seedNormalConversation(f.fakeRepo, "s-msg-err", time.Now().Add(-time.Hour))
	svc := NewQualityService(f, nil, testConfig(), nil)

	n, err := svc.RunScan(context.Background())
	if n != 0 || err != nil {
		t.Fatalf("candidate failure must be logged not returned: n=%d err=%v", n, err)
	}
}

func TestProcessCandidateInsertError(t *testing.T) {
	f := &scriptedQualityRepo{fakeRepo: newFakeRepo(), insertErr: errors.New("insert boom")}
	seedNormalConversation(f.fakeRepo, "s-insert-err", time.Now().Add(-time.Hour))
	svc := NewQualityService(f, nil, testConfig(), nil)

	n, err := svc.RunScan(context.Background())
	if n != 0 || err != nil {
		t.Fatalf("insert failure must be logged not returned: n=%d err=%v", n, err)
	}
}

func TestProcessCandidateRaceLost(t *testing.T) {
	f := &scriptedQualityRepo{fakeRepo: newFakeRepo(), insertAbsent: true}
	seedNormalConversation(f.fakeRepo, "s-race", time.Now().Add(-time.Hour))
	svc := NewQualityService(f, nil, testConfig(), nil)

	// 并发竞争落败：不报错且计入推进，但不会打分
	n, err := svc.RunScan(context.Background())
	if n != 1 || err != nil {
		t.Fatalf("race-lost candidate must count as processed: n=%d err=%v", n, err)
	}
	if len(f.fakeRepo.reviews) != 0 {
		t.Fatalf("no review row must be written by the loser: %+v", f.fakeRepo.reviews)
	}
}

func TestRescoreMessageListingError(t *testing.T) {
	f := &scriptedQualityRepo{fakeRepo: newFakeRepo(), messagesErr: errors.New("messages boom")}
	now := time.Now()
	f.fakeRepo.reviews["s-retry-err"] = &models.QualityReview{
		SessionID: "s-retry-err", Status: StatusFailed, AttemptCount: 1, NextRetryAt: &now,
	}
	svc := NewQualityService(f, goodScorer(), testConfig(), nil)

	n, err := svc.RunScan(context.Background())
	if n != 0 || err != nil {
		t.Fatalf("rescore failure must be logged not returned: n=%d err=%v", n, err)
	}
}

func TestScoreAndPersistMarkFailedError(t *testing.T) {
	f := &scriptedQualityRepo{fakeRepo: newFakeRepo(), markFailedErr: errors.New("mark boom")}
	seedNormalConversation(f.fakeRepo, "s-mark-err", time.Now().Add(-time.Hour))
	svc := NewQualityService(f, &stubScorer{err: errLLMDown}, testConfig(), nil)

	n, err := svc.RunScan(context.Background())
	if n != 0 || err != nil {
		t.Fatalf("mark-failed bookkeeping error must be logged not returned: n=%d err=%v", n, err)
	}
	if f.markFailedCalls == 0 {
		t.Fatal("MarkReviewFailed must have been attempted")
	}
}

func TestDurationSeconds(t *testing.T) {
	if got := durationSeconds(nil); got != 0 {
		t.Fatalf("nil session must yield 0, got %d", got)
	}
	if got := durationSeconds(&models.Session{}); got != 0 {
		t.Fatalf("session without end must yield 0, got %d", got)
	}
	end := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	if got := durationSeconds(&models.Session{StartedAt: end.Add(time.Minute), EndedAt: &end}); got != 0 {
		t.Fatalf("negative duration must clamp to 0, got %d", got)
	}
	if got := durationSeconds(&models.Session{StartedAt: end.Add(-90 * time.Second), EndedAt: &end}); got != 90 {
		t.Fatalf("duration = %d, want 90", got)
	}
}

func TestSelectedBySample(t *testing.T) {
	if !selectedBySample("any", 100) {
		t.Fatal("sample rate 100 must select everything")
	}
	if selectedBySample("any", 0) {
		t.Fatal("sample rate 0 must select nothing")
	}
	if selectedBySample("any", -1) {
		t.Fatal("negative sample rate must select nothing")
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte("sess-x"))
	want := int(h.Sum32()%10000) < int(50*100)
	if got := selectedBySample("sess-x", 50); got != want {
		t.Fatalf("selectedBySample(\"sess-x\", 50) = %v, want %v (deterministic fnv bucket)", got, want)
	}
	if selectedBySample("sess-x", 50) != selectedBySample("sess-x", 50) {
		t.Fatal("sampling must be deterministic for the same session id")
	}
}

func TestFirstResponseRuleSkipsNilSession(t *testing.T) {
	rule := &FirstResponseRule{threshold: 60}
	if vs := rule.Evaluate(context.Background(), RuleInput{Messages: []models.Message{
		ruleMsg("user", "你好", time.Now()),
		ruleMsg("agent", "您好", time.Now().Add(time.Minute)),
	}}); len(vs) != 0 {
		t.Fatalf("nil session must skip the rule: %+v", vs)
	}
}

func TestSnippetAroundMissingWord(t *testing.T) {
	if s := snippetAround("short content", "zzz"); s != "" {
		t.Fatalf("missing word must yield empty snippet, got %q", s)
	}
}
