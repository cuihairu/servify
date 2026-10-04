package application

// V1.0 收敛 B3-1b 补齐：反馈闭环与检索分析的校验/错误分支——反馈落库失败、
// limit 上限钳制、聚合读口逐段报错、聚合读口缺位降级空实现、来源快照序列
// 化失败。

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"servify/apps/server/internal/modules/ai/domain"
)

// covBareAnswerRepo 只实现 AnswerRepository（不具备聚合读口能力），驱动
// repo() 的降级断言路径。
type covBareAnswerRepo struct {
	answer *domain.AIAnswer
}

func (r *covBareAnswerRepo) Get(ctx context.Context, id uint) (*domain.AIAnswer, error) {
	return r.answer, nil
}

// covErrAnalyticsRepo 每个聚合方法可独立注入错误（nil=成功返回）。
type covErrAnalyticsRepo struct {
	countErr    error
	topHitErr   error
	topNoHitErr error
	lowConfErr  error
	feedbackErr error
}

func (r *covErrAnalyticsRepo) CountInWindow(ctx context.Context, since time.Time) (int64, int64, int64, float64, error) {
	if r.countErr != nil {
		return 0, 0, 0, 0, r.countErr
	}
	return 10, 6, 3, 6.0, nil
}

func (r *covErrAnalyticsRepo) TopQuestions(ctx context.Context, since time.Time, limit int, hitOnly bool) ([]QuestionStat, error) {
	if hitOnly {
		if r.topHitErr != nil {
			return nil, r.topHitErr
		}
	} else if r.topNoHitErr != nil {
		return nil, r.topNoHitErr
	}
	return nil, nil
}

func (r *covErrAnalyticsRepo) LowConfidenceQuestions(ctx context.Context, since time.Time, limit int) ([]QuestionStat, error) {
	if r.lowConfErr != nil {
		return nil, r.lowConfErr
	}
	return nil, nil
}

func (r *covErrAnalyticsRepo) CountFeedback(ctx context.Context, since time.Time) (int64, int64, error) {
	if r.feedbackErr != nil {
		return 0, 0, r.feedbackErr
	}
	return 0, 0, nil
}

func TestSubmitFeedbackPersistError(t *testing.T) {
	svc := NewAnswerFeedbackService(
		&stubAnswerRepo{answer: &domain.AIAnswer{ID: 1, SessionID: "conv-a", TenantID: "t"}},
		&stubFeedbackRepo{err: errors.New("persist down")},
	)
	if _, err := svc.SubmitFeedback(context.Background(), SubmitFeedbackRequest{AnswerID: 1}); err == nil {
		t.Fatal("expected feedback persist failure to surface")
	}
}

func TestRetrievalAnalyticsLimitClamp(t *testing.T) {
	svc := NewAnswerFeedbackService(&stubAnswerRepo{analytics: &stubAnalyticsRepo{}}, &stubFeedbackRepo{})
	out, err := svc.RetrievalAnalytics(context.Background(), 7, 100)
	if err != nil || out.WindowDays != 7 {
		t.Fatalf("analytics = %+v err %v", out, err)
	}
}

func TestRetrievalAnalyticsReadErrors(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("analytics unavailable")

	cases := []struct {
		name string
		repo *covErrAnalyticsRepo
	}{
		{"窗口聚合失败", &covErrAnalyticsRepo{countErr: boom}},
		{"top 问答失败", &covErrAnalyticsRepo{topHitErr: boom}},
		{"无命中榜失败", &covErrAnalyticsRepo{topNoHitErr: boom}},
		{"低置信榜失败", &covErrAnalyticsRepo{lowConfErr: boom}},
		{"反馈计数失败", &covErrAnalyticsRepo{feedbackErr: boom}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewAnswerFeedbackService(&stubAnswerRepo{analytics: tc.repo}, &stubFeedbackRepo{})
			if _, err := svc.RetrievalAnalytics(ctx, 7, 10); !errors.Is(err, boom) {
				t.Fatalf("err = %v want %v", err, boom)
			}
		})
	}
}

func TestRetrievalAnalyticsDegradesToEmpty(t *testing.T) {
	// 答案仓储不具备聚合读口（如仅实现 Get 的装配）→ 零值聚合不报错。
	svc := NewAnswerFeedbackService(&covBareAnswerRepo{answer: &domain.AIAnswer{ID: 1}}, &stubFeedbackRepo{})
	out, err := svc.RetrievalAnalytics(context.Background(), 7, 10)
	if err != nil {
		t.Fatalf("degraded analytics: %v", err)
	}
	if out.WindowDays != 7 || out.TotalAnswers != 0 || out.HitAnswers != 0 || out.NoHitAnswers != 0 {
		t.Fatalf("totals = %+v", out)
	}
	if out.AvgConfidence != 0 || out.HelpfulCount != 0 || out.NotHelpfulCount != 0 {
		t.Fatalf("zeros = %+v", out)
	}
	if out.TopQuestions != nil || out.NoHitQuestions != nil || out.LowConfidenceQuestions != nil {
		t.Fatalf("questions = %+v/%+v/%+v want nil", out.TopQuestions, out.NoHitQuestions, out.LowConfidenceQuestions)
	}
}

func TestSourcesSnapshotMarshalErrors(t *testing.T) {
	// NaN 置信分无法序列化。
	if raw, err := MarshalSources([]domain.SourceSnapshot{{Score: math.NaN()}}); err == nil || raw != "" {
		t.Fatalf("nan score = %q err %v", raw, err)
	}
	// 非法 JSON 反解失败。
	if out, err := UnmarshalSources("{not-json"); err == nil || out != nil {
		t.Fatalf("bad json = %+v err %v", out, err)
	}
}
