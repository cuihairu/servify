package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"servify/apps/server/internal/modules/ai/domain"
)

// V1.0 收敛 B3-1b（docs/v1-convergence-plan.md §5.3/§8.3）：反馈闭环与
// 检索分析读模型。写侧（首答落库）在 ai/delivery 的记录路径，读侧（
// Knowledge 管理页的 top 问答/无命中率/低置信率）聚合自 ai_answers。

// LowConfidenceThreshold 检索分析的"低置信"判定线。与编排默认转人工阈
// 值（HandoffConfidenceThreshold 默认 0.65）同口径：低于它即"知识库答不
// 上来"的那类答案。
const LowConfidenceThreshold = 0.65

// AnswerRepository 首答读口（反馈回看与访客会话绑定校验）。
type AnswerRepository interface {
	Get(ctx context.Context, id uint) (*domain.AIAnswer, error)
}

// AnswerFeedbackRepository 反馈写口。
type AnswerFeedbackRepository interface {
	Create(ctx context.Context, feedback *domain.AnswerFeedback) error
}

// AnswerAnalyticsRepository 检索分析聚合读口（只读投影，不进业务写路径）。
type AnswerAnalyticsRepository interface {
	CountInWindow(ctx context.Context, since time.Time) (total int64, hit int64, lowConfidence int64, confidenceSum float64, err error)
	TopQuestions(ctx context.Context, since time.Time, limit int, hitOnly bool) ([]QuestionStat, error)
	// LowConfidenceQuestions 低置信问题榜（平均置信升序）。
	LowConfidenceQuestions(ctx context.Context, since time.Time, limit int) ([]QuestionStat, error)
	CountFeedback(ctx context.Context, since time.Time) (helpful int64, notHelpful int64, err error)
}

// QuestionStat 检索分析单条问题统计。
type QuestionStat struct {
	Query         string  `json:"query"`
	Count         int64   `json:"count"`
	AvgConfidence float64 `json:"avg_confidence"`
}

// SubmitFeedbackRequest 提交反馈入参。
type SubmitFeedbackRequest struct {
	AnswerID uint
	Helpful  bool
	Comment  string
	// CallerKind 认证主体种类：end_user（访客）强制会话绑定。
	CallerKind string
	// CallerSession 访客 token 绑定的会话；end_user 时必须与答案一致。
	CallerSession string
	// CreatedBy 提交主体标识（审计回看）。
	CreatedBy string
}

// AnswerFeedbackService 反馈闭环应用服务。
type AnswerFeedbackService struct {
	answers   AnswerRepository
	feedbacks AnswerFeedbackRepository
}

func NewAnswerFeedbackService(answers AnswerRepository, feedbacks AnswerFeedbackRepository) *AnswerFeedbackService {
	return &AnswerFeedbackService{answers: answers, feedbacks: feedbacks}
}

// SubmitFeedback 落一条反馈：答案必须存在；end_user 主体强制会话绑定
// （访客只能评价自己会话内的答案，与翻译偏好读向的绑定口径一致）。
func (s *AnswerFeedbackService) SubmitFeedback(ctx context.Context, req SubmitFeedbackRequest) (uint, error) {
	if req.AnswerID == 0 {
		return 0, fmt.Errorf("answer_id required")
	}
	answer, err := s.answers.Get(ctx, req.AnswerID)
	if err != nil {
		return 0, fmt.Errorf("answer %d not found", req.AnswerID)
	}
	if req.CallerKind == "end_user" && req.CallerSession != "" && answer.SessionID != req.CallerSession {
		return 0, fmt.Errorf("answer %d does not belong to session", req.AnswerID)
	}
	feedback := &domain.AnswerFeedback{
		TenantID:    answer.TenantID,
		WorkspaceID: answer.WorkspaceID,
		AnswerID:    req.AnswerID,
		Helpful:     req.Helpful,
		Comment:     req.Comment,
		CreatedBy:   req.CreatedBy,
		CreatedAt:   time.Now(),
	}
	if err := s.feedbacks.Create(ctx, feedback); err != nil {
		return 0, err
	}
	return feedback.ID, nil
}

// RetrievalAnalytics 检索分析读模型（§8.3：top 问答、无命中率、低置信率
// + 反馈计数）。
type RetrievalAnalytics struct {
	WindowDays             int            `json:"window_days"`
	TotalAnswers           int64          `json:"total_answers"`
	HitAnswers             int64          `json:"hit_answers"`
	NoHitAnswers           int64          `json:"no_hit_answers"`
	LowConfidenceAnswers   int64          `json:"low_confidence_answers"`
	AvgConfidence          float64        `json:"avg_confidence"`
	HelpfulCount           int64          `json:"helpful_count"`
	NotHelpfulCount        int64          `json:"not_helpful_count"`
	TopQuestions           []QuestionStat `json:"top_questions"`
	NoHitQuestions         []QuestionStat `json:"no_hit_questions"`
	LowConfidenceQuestions []QuestionStat `json:"low_confidence_questions"`
}

// RetrievalAnalytics 读窗口聚合（days 默认 7 上限 90；limit 默认 10 上限 50）。
func (s *AnswerFeedbackService) RetrievalAnalytics(ctx context.Context, days, limit int) (*RetrievalAnalytics, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	since := time.Now().AddDate(0, 0, -days)
	out := &RetrievalAnalytics{WindowDays: days}

	total, hit, lowConf, confSum, err := s.repo().CountInWindow(ctx, since)
	if err != nil {
		return nil, err
	}
	out.TotalAnswers = total
	out.HitAnswers = hit
	out.NoHitAnswers = total - hit
	out.LowConfidenceAnswers = lowConf
	if total > 0 {
		out.AvgConfidence = confSum / float64(total)
	}
	if out.TopQuestions, err = s.repo().TopQuestions(ctx, since, limit, true); err != nil {
		return nil, err
	}
	if out.NoHitQuestions, err = s.repo().TopQuestions(ctx, since, limit, false); err != nil {
		return nil, err
	}
	// 低置信榜：按平均置信升序取最差的一批（count>=1 即可）。
	if out.LowConfidenceQuestions, err = s.repo().LowConfidenceQuestions(ctx, since, limit); err != nil {
		return nil, err
	}
	if out.HelpfulCount, out.NotHelpfulCount, err = s.repo().CountFeedback(ctx, since); err != nil {
		return nil, err
	}
	return out, nil
}

// repo 断言聚合读口（与仓储同对象实现；nil 防御走空聚合）。
func (s *AnswerFeedbackService) repo() AnswerAnalyticsRepository {
	if analyticsRepo, ok := s.answers.(AnswerAnalyticsRepository); ok {
		return analyticsRepo
	}
	return emptyAnalyticsRepository{}
}

// emptyAnalyticsRepository 聚合读口缺位时的空实现（读模型降级为零值，
// 不让管理页因仓储能力差异而报错）。
type emptyAnalyticsRepository struct{}

func (emptyAnalyticsRepository) CountInWindow(ctx context.Context, since time.Time) (int64, int64, int64, float64, error) {
	return 0, 0, 0, 0, nil
}
func (emptyAnalyticsRepository) TopQuestions(ctx context.Context, since time.Time, limit int, hitOnly bool) ([]QuestionStat, error) {
	return nil, nil
}
func (emptyAnalyticsRepository) LowConfidenceQuestions(ctx context.Context, since time.Time, limit int) ([]QuestionStat, error) {
	return nil, nil
}
func (emptyAnalyticsRepository) CountFeedback(ctx context.Context, since time.Time) (int64, int64, error) {
	return 0, 0, nil
}

// MarshalSources 来源快照序列化（落库与展示共用形状）。
func MarshalSources(sources []domain.SourceSnapshot) (string, error) {
	if len(sources) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(sources)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// UnmarshalSources 反解来源快照（反馈回看/管理页展示）。
func UnmarshalSources(raw string) ([]domain.SourceSnapshot, error) {
	if raw == "" {
		return nil, nil
	}
	out := make([]domain.SourceSnapshot, 0)
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}
