package application

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

// fakeRepo 内存仓储：覆盖 RunScan 的全部读写路径。
type fakeRepo struct {
	sessions []models.Session
	messages map[string][]models.Message
	reviews  map[string]*models.QualityReview

	scoredCalls  int
	failedCalls  int
	nextReviewID uint
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{messages: map[string][]models.Message{}, reviews: map[string]*models.QualityReview{}}
}

func (f *fakeRepo) seedSession(id, status string, endedAt time.Time, msgs ...models.Message) {
	f.sessions = append(f.sessions, models.Session{
		ID: id, Status: status, Platform: "web",
		StartedAt: endedAt.Add(-10 * time.Minute), EndedAt: &endedAt,
	})
	for i := range msgs {
		msgs[i].SessionID = id
		f.messages[id] = append(f.messages[id], msgs[i])
	}
}

func (f *fakeRepo) ListReviewCandidates(_ context.Context, lookback time.Time, limit int) ([]models.Session, error) {
	var out []models.Session
	for _, s := range f.sessions {
		if s.Status != "ended" || s.EndedAt == nil || s.EndedAt.Before(lookback) {
			continue
		}
		if _, ok := f.reviews[s.ID]; ok {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EndedAt.Before(*out[j].EndedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeRepo) ListReviewsForRetry(_ context.Context, maxAttempts int, now time.Time, limit int) ([]models.QualityReview, error) {
	var out []models.QualityReview
	for _, r := range f.reviews {
		if (r.Status == StatusPending || r.Status == StatusFailed) && r.AttemptCount < maxAttempts &&
			(r.NextRetryAt == nil || !r.NextRetryAt.After(now)) {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].NextRetryAt.Before(*out[j].NextRetryAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeRepo) GetReviewBySession(_ context.Context, sessionID string) (*models.QualityReview, error) {
	if r, ok := f.reviews[sessionID]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, ErrNotFound
}

func (f *fakeRepo) ListMessages(_ context.Context, sessionID string) ([]models.Message, error) {
	return f.messages[sessionID], nil
}

func (f *fakeRepo) InsertReviewIfAbsent(_ context.Context, review *models.QualityReview) (bool, error) {
	if _, ok := f.reviews[review.SessionID]; ok {
		return false, nil
	}
	f.nextReviewID++
	review.ID = f.nextReviewID
	cp := *review
	f.reviews[review.SessionID] = &cp
	return true, nil
}

func (f *fakeRepo) MarkReviewScored(_ context.Context, sessionID string, allowedFrom []string, fields ScoredFields) (bool, error) {
	f.scoredCalls++
	r, ok := f.reviews[sessionID]
	if !ok {
		return false, nil
	}
	for _, st := range allowedFrom {
		if r.Status == st {
			r.Status = "scored"
			r.DimensionsJSON = fields.DimensionsJSON
			r.LLMTotalScore = fields.TotalScore
			r.LLMSummary = fields.Summary
			r.LLMProvider = fields.Provider
			r.LLMModel = fields.Model
			r.ScoredAt = &fields.ScoredAt
			r.NextRetryAt = nil
			r.LastError = ""
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeRepo) MarkReviewFailed(_ context.Context, sessionID string, allowedFrom []string, attempts int, nextRetry time.Time, lastErr string) (bool, error) {
	f.failedCalls++
	r, ok := f.reviews[sessionID]
	if !ok {
		return false, nil
	}
	for _, st := range allowedFrom {
		if r.Status == st {
			r.Status = "failed"
			r.AttemptCount = attempts
			t := nextRetry
			r.NextRetryAt = &t
			r.LastError = lastErr
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeRepo) ListReviews(_ context.Context, query ReviewListQuery) ([]models.QualityReview, int64, error) {
	var out []models.QualityReview
	for _, r := range f.reviews {
		if query.Status == "" || r.Status == query.Status {
			out = append(out, *r)
		}
	}
	return out, int64(len(out)), nil
}

func (f *fakeRepo) ConfirmReview(_ context.Context, sessionID string, cmd ConfirmCommand) (bool, error) {
	r, ok := f.reviews[sessionID]
	if !ok || r.Status != StatusScored {
		return false, nil
	}
	r.Status = StatusConfirmed
	r.ManualScore = cmd.ManualScore
	r.ManualResult = cmd.ManualResult
	r.ReviewNote = cmd.ReviewNote
	reviewedBy := cmd.ReviewedBy
	r.ReviewedBy = &reviewedBy
	return true, nil
}

func (f *fakeRepo) RescheduleReview(_ context.Context, sessionID string, force bool) (bool, error) {
	r, ok := f.reviews[sessionID]
	if !ok {
		return false, nil
	}
	if r.Status == StatusConfirmed && !force {
		return false, nil
	}
	r.Status = StatusPending
	r.Trigger = "rescore"
	r.AttemptCount = 0
	r.NextRetryAt = nil
	r.LastError = ""
	return true, nil
}

// ---- stubs ----

type stubScorer struct {
	result ScoreResult
	err    error
	calls  int
}

func (s *stubScorer) ScoreSession(_ context.Context, _ ScoreRequest) (ScoreResult, error) {
	s.calls++
	return s.result, s.err
}

var errLLMDown = errors.New("llm down")

func goodScorer() *stubScorer {
	return &stubScorer{result: ScoreResult{
		TotalScore: 8.5,
		Dimensions: map[string]DimensionScore{
			"attitude":   {Score: 9, Weight: 0.3, Reason: "礼貌"},
			"resolution": {Score: 8, Weight: 0.5, Reason: "解决了"},
			"timeliness": {Score: 9, Weight: 0.2, Reason: "及时"},
		},
		Summary: "服务良好", Provider: "mock", Model: "mock-1",
	}}
}

func testConfig() ServiceConfig {
	return ServiceConfig{
		SampleRate:          100, // 默认全选；抽样路径单独置 0
		BatchSize:           10,
		LookbackDays:        7,
		MinMessages:         2,
		MaxAttempts:         3,
		RetryBackoffSeconds: 600,
		LLMEnabled:          true,
		Rules:               RuleConfig{BannedWords: []string{"退款保证"}, ResponseTimeoutSeconds: 60, FirstResponseTimeoutSeconds: 60},
	}
}

func seedNormalConversation(f *fakeRepo, id string, endedAt time.Time) {
	f.seedSession(id, "ended", endedAt,
		models.Message{Sender: "user", Content: "你好", CreatedAt: endedAt.Add(-4 * time.Minute)},
		models.Message{Sender: "agent", Content: "您好，请问有什么可以帮您", CreatedAt: endedAt.Add(-3 * time.Minute)},
		models.Message{Sender: "user", Content: "能退款吗", CreatedAt: endedAt.Add(-2 * time.Minute)},
		models.Message{Sender: "agent", Content: "请放心，我们承诺退款保证", CreatedAt: endedAt.Add(-time.Minute)},
	)
}

func TestRunScanRulesOnlyScoresWithViolations(t *testing.T) {
	f := newFakeRepo()
	ended := time.Now().Add(-time.Hour)
	seedNormalConversation(f, "s-rules", ended)
	cfg := testConfig()
	cfg.LLMEnabled = false // rules-only 降级
	svc := NewQualityService(f, nil, cfg, nil)

	n, err := svc.RunScan(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("scan: n=%d err=%v", n, err)
	}
	review, err := f.GetReviewBySession(context.Background(), "s-rules")
	if err != nil {
		t.Fatalf("get review: %v", err)
	}
	if review.Status != "scored" {
		t.Fatalf("rules-only must end scored, got %s", review.Status)
	}
	if review.ViolationCount != 1 || review.MaxSeverity != SeverityHigh {
		t.Fatalf("banned_words violation expected: %+v", review)
	}
	if review.MessageCount != 4 || review.DurationSeconds != 600 {
		t.Fatalf("stats wrong: count=%d duration=%d", review.MessageCount, review.DurationSeconds)
	}
	if review.LLMSummary != "" || review.LLMTotalScore != nil {
		t.Fatalf("rules-only must leave llm fields empty: %+v", review)
	}
	if review.CustomerID == nil || *review.CustomerID != 0 {
		t.Fatalf("customer id from session user: %+v", review.CustomerID)
	}

	// 幂等：下轮不再产生新记录
	n, err = svc.RunScan(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("second scan must be a no-op: n=%d err=%v", n, err)
	}
}

func TestRunScanSampledOutLeavesSkippedRow(t *testing.T) {
	f := newFakeRepo()
	f.seedSession("s-skip", "ended", time.Now().Add(-time.Hour),
		models.Message{Sender: "user", Content: "hi", CreatedAt: time.Now().Add(-time.Hour)},
		models.Message{Sender: "agent", Content: "hello", CreatedAt: time.Now().Add(-30 * time.Minute)},
	)
	cfg := testConfig()
	cfg.SampleRate = 0 // 全部落榜
	svc := NewQualityService(f, goodScorer(), cfg, nil)

	if n, err := svc.RunScan(context.Background()); err != nil || n != 1 {
		t.Fatalf("scan: n=%d err=%v", n, err)
	}
	review, _ := f.GetReviewBySession(context.Background(), "s-skip")
	if review == nil || review.Status != "skipped" {
		t.Fatalf("sampled-out session must leave a skipped row: %+v", review)
	}

	// skipped 行把会话排除出候选集：下轮零推进
	if n, err := svc.RunScan(context.Background()); err != nil || n != 0 {
		t.Fatalf("skipped row must gate rescans: n=%d err=%v", n, err)
	}
}

func TestRunScanBelowMinMessagesSkipped(t *testing.T) {
	f := newFakeRepo()
	f.seedSession("s-thin", "ended", time.Now().Add(-time.Hour),
		models.Message{Sender: "user", Content: "hi", CreatedAt: time.Now().Add(-time.Hour)},
	)
	svc := NewQualityService(f, goodScorer(), testConfig(), nil)

	if n, _ := svc.RunScan(context.Background()); n != 1 {
		t.Fatalf("scan must consume the candidate: n=%d", n)
	}
	review, _ := f.GetReviewBySession(context.Background(), "s-thin")
	if review == nil || review.Status != "skipped" {
		t.Fatalf("thin session must be skipped: %+v", review)
	}
}

func TestRunScanLLMSuccessPersistsScore(t *testing.T) {
	f := newFakeRepo()
	seedNormalConversation(f, "s-llm", time.Now().Add(-time.Hour))
	scorer := goodScorer()
	svc := NewQualityService(f, scorer, testConfig(), nil)

	if n, err := svc.RunScan(context.Background()); err != nil || n != 1 {
		t.Fatalf("scan: n=%d err=%v", n, err)
	}
	review, _ := f.GetReviewBySession(context.Background(), "s-llm")
	if review.Status != "scored" || review.LLMTotalScore == nil || *review.LLMTotalScore != 8.5 {
		t.Fatalf("scored review expected: %+v", review)
	}
	if review.LLMProvider != "mock" || review.LLMModel != "mock-1" || review.LLMSummary != "服务良好" {
		t.Fatalf("llm metadata missing: %+v", review)
	}
	if review.DimensionsJSON == "" {
		t.Fatal("dimensions json must be persisted")
	}
	if scorer.calls != 1 {
		t.Fatalf("scorer must be called once, got %d", scorer.calls)
	}
}

func TestRunScanLLMFailureBooksRetryThenRevives(t *testing.T) {
	f := newFakeRepo()
	now := time.Now()
	seedNormalConversation(f, "s-retry", now.Add(-time.Hour))
	scorer := &stubScorer{err: errLLMDown}
	cfg := testConfig()
	svc := NewQualityService(f, scorer, cfg, nil)
	svc.SetNowFunc(func() time.Time { return now })

	// 第一轮：打分失败 → failed + 退避记账
	if n, err := svc.RunScan(context.Background()); err != nil || n != 0 {
		t.Fatalf("failing scan must not count as processed: n=%d err=%v", n, err)
	}
	review, _ := f.GetReviewBySession(context.Background(), "s-retry")
	if review == nil || review.Status != "failed" || review.AttemptCount != 1 {
		t.Fatalf("failed bookkeeping expected: %+v", review)
	}
	if review.NextRetryAt == nil || !review.NextRetryAt.Equal(now.Add(600*time.Second)) {
		t.Fatalf("backoff expected: %+v", review.NextRetryAt)
	}

	// 退避未到期：不复活
	if n, _ := svc.RunScan(context.Background()); n != 0 {
		t.Fatal("must respect retry backoff")
	}

	// 到期且 scorer 恢复：复活重打分到 scored
	scorer.err = nil
	scorer.result = goodScorer().result
	svc.SetNowFunc(func() time.Time { return now.Add(601 * time.Second) })
	if n, err := svc.RunScan(context.Background()); err != nil || n != 1 {
		t.Fatalf("revive scan: n=%d err=%v", n, err)
	}
	review, _ = f.GetReviewBySession(context.Background(), "s-retry")
	if review.Status != "scored" || review.LastError != "" {
		t.Fatalf("revived review expected: %+v", review)
	}
}

func TestRunScanGivesUpAfterMaxAttempts(t *testing.T) {
	f := newFakeRepo()
	now := time.Now()
	seedNormalConversation(f, "s-dead", now.Add(-time.Hour))
	scorer := &stubScorer{err: errLLMDown}
	cfg := testConfig()
	cfg.MaxAttempts = 2
	svc := NewQualityService(f, scorer, cfg, nil)
	svc.SetNowFunc(func() time.Time { return now })

	// 连续两轮失败后 attempt=2 == max，不再复活
	for i := 0; i < 2; i++ {
		now = now.Add(601 * time.Second)
		svc.SetNowFunc(func() time.Time { return now })
		svc.RunScan(context.Background())
	}
	review, _ := f.GetReviewBySession(context.Background(), "s-dead")
	if review.AttemptCount != 2 {
		t.Fatalf("attempts expected 2, got %d", review.AttemptCount)
	}
	now = now.Add(601 * time.Second)
	svc.SetNowFunc(func() time.Time { return now })
	if n, _ := svc.RunScan(context.Background()); n != 0 {
		t.Fatal("exhausted review must not revive")
	}
}

func TestConfirmReviewLifecycle(t *testing.T) {
	f := newFakeRepo()
	seedNormalConversation(f, "s-confirm", time.Now().Add(-time.Hour))
	svc := NewQualityService(f, nil, testConfig(), nil)
	ctx := context.Background()

	// 不存在的会话
	if err := svc.ConfirmReview(ctx, "nope", ConfirmCommand{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// 记录先打分（rules-only）
	svc.RunScan(ctx)
	// 确认成功
	score := 7.5
	if err := svc.ConfirmReview(ctx, "s-confirm", ConfirmCommand{
		ManualScore: &score, ManualResult: "pass", ReviewNote: "抽检通过", ReviewedBy: 42,
	}); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	review, _ := f.GetReviewBySession(ctx, "s-confirm")
	if review.Status != StatusConfirmed || review.ManualScore == nil || *review.ManualScore != 7.5 {
		t.Fatalf("confirmed review expected: %+v", review)
	}
	if review.ReviewedBy == nil || *review.ReviewedBy != 42 {
		t.Fatalf("reviewed_by expected: %+v", review.ReviewedBy)
	}
	// 重复确认：confirmed 不可再确认
	if err := svc.ConfirmReview(ctx, "s-confirm", ConfirmCommand{}); !errors.Is(err, ErrNotScoreable) {
		t.Fatalf("want ErrNotScoreable, got %v", err)
	}
}

func TestRescoreReviewForceGate(t *testing.T) {
	f := newFakeRepo()
	seedNormalConversation(f, "s-rescore", time.Now().Add(-time.Hour))
	svc := NewQualityService(f, nil, testConfig(), nil)
	ctx := context.Background()

	svc.RunScan(ctx) // rules-only → scored
	if err := svc.ConfirmReview(ctx, "s-rescore", ConfirmCommand{}); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	// confirmed 不带 force：拒绝
	if err := svc.RescoreReview(ctx, "s-rescore", false); !errors.Is(err, ErrConfirmedNeedsForce) {
		t.Fatalf("want ErrConfirmedNeedsForce, got %v", err)
	}
	// force：重置 pending（attempt 清零，trigger=rescore）
	if err := svc.RescoreReview(ctx, "s-rescore", true); err != nil {
		t.Fatalf("force rescore: %v", err)
	}
	review, _ := f.GetReviewBySession(ctx, "s-rescore")
	if review.Status != StatusPending || review.AttemptCount != 0 || review.Trigger != "rescore" {
		t.Fatalf("rescheduled review expected: %+v", review)
	}
	// 下一轮扫描：pending 滞留进入复活集重新打分
	if n, err := svc.RunScan(ctx); err != nil || n != 1 {
		t.Fatalf("rescheduled review must be re-processed: n=%d err=%v", n, err)
	}
	review, _ = f.GetReviewBySession(ctx, "s-rescore")
	if review.Status != StatusScored {
		t.Fatalf("rescored to scored expected, got %s", review.Status)
	}
}

func TestScorerEnabledGate(t *testing.T) {
	f := newFakeRepo()
	cfg := testConfig()

	cfg.LLMEnabled = false
	if (NewQualityService(f, goodScorer(), cfg, nil)).ScorerEnabled() {
		t.Fatal("disabled flag must gate the scorer")
	}
	cfg.LLMEnabled = true
	if !(NewQualityService(f, goodScorer(), cfg, nil)).ScorerEnabled() {
		t.Fatal("enabled flag with provider must score")
	}
	if (NewQualityService(f, nil, cfg, nil)).ScorerEnabled() {
		t.Fatal("missing provider must degrade to rules-only")
	}
}
