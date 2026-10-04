package application

// V1.0 收敛 B3-1b（docs/v1-convergence-plan.md §5.3/§8.3）：反馈闭环与检索
// 分析读模型的服务层行为。

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/modules/ai/domain"
)

type stubAnswerRepo struct {
	answer *domain.AIAnswer
	getErr error
	// analytics 聚合读口（生产里 GormAnswerRepository 一体两用，测试同构）。
	analytics AnswerAnalyticsRepository
}

func (s *stubAnswerRepo) Get(ctx context.Context, id uint) (*domain.AIAnswer, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.answer, nil
}

func (s *stubAnswerRepo) CountInWindow(ctx context.Context, since time.Time) (int64, int64, int64, float64, error) {
	return s.analytics.CountInWindow(ctx, since)
}
func (s *stubAnswerRepo) TopQuestions(ctx context.Context, since time.Time, limit int, hitOnly bool) ([]QuestionStat, error) {
	return s.analytics.TopQuestions(ctx, since, limit, hitOnly)
}
func (s *stubAnswerRepo) LowConfidenceQuestions(ctx context.Context, since time.Time, limit int) ([]QuestionStat, error) {
	return s.analytics.LowConfidenceQuestions(ctx, since, limit)
}
func (s *stubAnswerRepo) CountFeedback(ctx context.Context, since time.Time) (int64, int64, error) {
	return s.analytics.CountFeedback(ctx, since)
}

type stubFeedbackRepo struct {
	created *domain.AnswerFeedback
	err     error
}

func (s *stubFeedbackRepo) Create(ctx context.Context, feedback *domain.AnswerFeedback) error {
	if s.err != nil {
		return s.err
	}
	feedback.ID = 77
	cp := *feedback
	s.created = &cp
	return nil
}

type stubAnalyticsRepo struct {
	total, hit, lowConf          int64
	confSum                      float64
	top, noHit, lowConfQuestions []QuestionStat
	helpful, notHelpful          int64
}

func (s *stubAnalyticsRepo) CountInWindow(ctx context.Context, since time.Time) (int64, int64, int64, float64, error) {
	return s.total, s.hit, s.lowConf, s.confSum, nil
}
func (s *stubAnalyticsRepo) TopQuestions(ctx context.Context, since time.Time, limit int, hitOnly bool) ([]QuestionStat, error) {
	if hitOnly {
		return s.top, nil
	}
	return s.noHit, nil
}
func (s *stubAnalyticsRepo) LowConfidenceQuestions(ctx context.Context, since time.Time, limit int) ([]QuestionStat, error) {
	return s.lowConfQuestions, nil
}
func (s *stubAnalyticsRepo) CountFeedback(ctx context.Context, since time.Time) (int64, int64, error) {
	return s.helpful, s.notHelpful, nil
}

func TestSubmitFeedbackRequiresExistingAnswer(t *testing.T) {
	svc := NewAnswerFeedbackService(&stubAnswerRepo{getErr: context.DeadlineExceeded}, &stubFeedbackRepo{})
	if _, err := svc.SubmitFeedback(context.Background(), SubmitFeedbackRequest{AnswerID: 9}); err == nil {
		t.Fatal("expected missing answer to be rejected")
	}
	if _, err := svc.SubmitFeedback(context.Background(), SubmitFeedbackRequest{}); err == nil {
		t.Fatal("expected zero answer_id to be rejected")
	}
}

func TestSubmitFeedbackVisitorSessionBinding(t *testing.T) {
	answers := &stubAnswerRepo{answer: &domain.AIAnswer{ID: 1, SessionID: "conv-a", TenantID: "t"}}
	feedbacks := &stubFeedbackRepo{}
	svc := NewAnswerFeedbackService(answers, feedbacks)

	// end_user 评了别的会话的答案 → 拒绝。
	_, err := svc.SubmitFeedback(context.Background(), SubmitFeedbackRequest{
		AnswerID: 1, Helpful: true, CallerKind: "end_user", CallerSession: "conv-b",
	})
	if err == nil {
		t.Fatal("expected cross-session visitor feedback to be rejected")
	}
	// 会话一致 → 放行且落库带答案归属。
	id, err := svc.SubmitFeedback(context.Background(), SubmitFeedbackRequest{
		AnswerID: 1, Helpful: true, Comment: "很有用", CallerKind: "end_user", CallerSession: "conv-a", CreatedBy: "visitor:conv-a",
	})
	if err != nil || id != 77 {
		t.Fatalf("submit = %d err %v", id, err)
	}
	if feedbacks.created.AnswerID != 1 || !feedbacks.created.Helpful || feedbacks.created.TenantID != "t" {
		t.Fatalf("feedback = %+v", feedbacks.created)
	}
	// 坐席主体不做会话绑定。
	if _, err := svc.SubmitFeedback(context.Background(), SubmitFeedbackRequest{
		AnswerID: 1, Helpful: false, CallerKind: "agent",
	}); err != nil {
		t.Fatalf("agent feedback rejected: %v", err)
	}
}

func TestRetrievalAnalyticsAggregation(t *testing.T) {
	analytics := &stubAnalyticsRepo{
		total: 10, hit: 7, lowConf: 3, confSum: 6.5,
		top:              []QuestionStat{{Query: "退款政策", Count: 5, AvgConfidence: 0.9}},
		noHit:            []QuestionStat{{Query: "怎么开发票", Count: 3, AvgConfidence: 0}},
		lowConfQuestions: []QuestionStat{{Query: "怎么开发票", Count: 3, AvgConfidence: 0.2}},
		helpful:          4, notHelpful: 1,
	}
	svc := NewAnswerFeedbackService(&stubAnswerRepo{analytics: analytics}, &stubFeedbackRepo{})
	out, err := svc.RetrievalAnalytics(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("analytics: %v", err)
	}
	if out.WindowDays != 7 || out.TotalAnswers != 10 || out.HitAnswers != 7 || out.NoHitAnswers != 3 {
		t.Fatalf("totals = %+v", out)
	}
	if out.LowConfidenceAnswers != 3 || out.AvgConfidence < 0.649 || out.AvgConfidence > 0.651 {
		t.Fatalf("confidence = %+v", out)
	}
	if len(out.TopQuestions) != 1 || out.TopQuestions[0].Query != "退款政策" {
		t.Fatalf("top = %+v", out.TopQuestions)
	}
	if len(out.NoHitQuestions) != 1 || out.NoHitQuestions[0].Query != "怎么开发票" {
		t.Fatalf("nohit = %+v", out.NoHitQuestions)
	}
	if len(out.LowConfidenceQuestions) != 1 || out.HelpfulCount != 4 || out.NotHelpfulCount != 1 {
		t.Fatalf("lowconf/feedback = %+v %d/%d", out.LowConfidenceQuestions, out.HelpfulCount, out.NotHelpfulCount)
	}
	// 窗口上限 90。
	if clamped, _ := svc.RetrievalAnalytics(context.Background(), 365, 0); clamped.WindowDays != 90 {
		t.Fatalf("window clamp = %d want 90", clamped.WindowDays)
	}
}

func TestSourcesSnapshotRoundTrip(t *testing.T) {
	sources := []domain.SourceSnapshot{{DocumentID: "d1", Title: "退款政策", Score: 0.91}}
	raw, err := MarshalSources(sources)
	if err != nil || raw == "" {
		t.Fatalf("marshal: %q %v", raw, err)
	}
	back, err := UnmarshalSources(raw)
	if err != nil || len(back) != 1 || back[0].DocumentID != "d1" || back[0].Score != 0.91 {
		t.Fatalf("roundtrip = %+v err %v", back, err)
	}
	if empty, err := MarshalSources(nil); err != nil || empty != "" {
		t.Fatalf("empty = %q err %v", empty, err)
	}
	if back, _ := UnmarshalSources(""); back != nil {
		t.Fatalf("empty unmarshal = %+v", back)
	}
}
