package application

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
)

// 质检记录状态（models.QualityReview.Status 的取值域）。
const (
	StatusPending   = "pending"
	StatusSkipped   = "skipped"
	StatusScored    = "scored"
	StatusFailed    = "failed"
	StatusConfirmed = "confirmed"
)

// ServiceConfig 是 RunScan 的运行参数（来自 config.QualityConfig）。
type ServiceConfig struct {
	SampleRate          float64 // 0-100
	BatchSize           int
	LookbackDays        int
	MinMessages         int
	MaxAttempts         int
	RetryBackoffSeconds int
	LLMEnabled          bool
	Rules               RuleConfig
}

// QualityService 质检应用服务：扫描 → 规则 → 打分 → 落库。
type QualityService struct {
	repo   Repository
	scorer ScoreProvider // 可 nil：rules-only
	cfg    ServiceConfig
	logger *logrus.Logger
	engine *RuleEngine
	nowFn  func() time.Time
}

func NewQualityService(repo Repository, scorer ScoreProvider, cfg ServiceConfig, logger *logrus.Logger) *QualityService {
	if logger == nil {
		logger = logrus.New()
		logger.SetLevel(logrus.PanicLevel)
	}
	return &QualityService{
		repo:   repo,
		scorer: scorer,
		cfg:    cfg,
		logger: logger,
		engine: NewRuleEngine(cfg.Rules, func(rule string, r any) {
			logger.WithField("rule", rule).Errorf("quality rule panic: %v", r)
		}),
		nowFn: time.Now,
	}
}

// SetNowFunc 供测试注入时钟。
func (s *QualityService) SetNowFunc(fn func() time.Time) { s.nowFn = fn }

// ScorerEnabled 返回 LLM 打分是否生效（打分开关且 provider 就绪）。
func (s *QualityService) ScorerEnabled() bool { return s.cfg.LLMEnabled && s.scorer != nil }

// RunScan 执行一轮扫描，返回本轮推进的会话数（含 skipped / 失败重试）。
func (s *QualityService) RunScan(ctx context.Context) (int, error) {
	now := s.nowFn()
	processed := 0

	candidates, err := s.repo.ListReviewCandidates(ctx, now.AddDate(0, 0, -s.cfg.LookbackDays), s.cfg.BatchSize)
	if err != nil {
		return processed, fmt.Errorf("list review candidates: %w", err)
	}
	for i := range candidates {
		if err := s.processCandidate(ctx, &candidates[i]); err != nil {
			s.logger.WithError(err).WithField("session_id", candidates[i].ID).
				Warn("quality: review candidate failed")
			continue
		}
		processed++
	}

	retries, err := s.repo.ListReviewsForRetry(ctx, s.cfg.MaxAttempts, now, s.cfg.BatchSize)
	if err != nil {
		return processed, fmt.Errorf("list reviews for retry: %w", err)
	}
	for i := range retries {
		if err := s.rescore(ctx, &retries[i]); err != nil {
			s.logger.WithError(err).WithField("session_id", retries[i].SessionID).
				Warn("quality: rescore failed")
			continue
		}
		processed++
	}
	return processed, nil
}

// ListReviews 管理面分页查询质检记录。
func (s *QualityService) ListReviews(ctx context.Context, query ReviewListQuery) ([]models.QualityReview, int64, error) {
	return s.repo.ListReviews(ctx, query)
}

// GetReviewBySession 按会话取单条质检记录。
func (s *QualityService) GetReviewBySession(ctx context.Context, sessionID string) (*models.QualityReview, error) {
	return s.repo.GetReviewBySession(ctx, sessionID)
}

// ConfirmReview 人工确认：scored → confirmed，写入人工字段；绝不覆盖已有确认。
func (s *QualityService) ConfirmReview(ctx context.Context, sessionID string, cmd ConfirmCommand) error {
	if _, err := s.repo.GetReviewBySession(ctx, sessionID); err != nil {
		return err
	}
	ok, err := s.repo.ConfirmReview(ctx, sessionID, cmd)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotScoreable
	}
	return nil
}

// RescoreReview 重新打分：记录重置为 pending（attempt 清零），下一轮扫描重新处理。
// confirmed 记录需显式 force。
func (s *QualityService) RescoreReview(ctx context.Context, sessionID string, force bool) error {
	review, err := s.repo.GetReviewBySession(ctx, sessionID)
	if err != nil {
		return err
	}
	if review.Status == StatusConfirmed && !force {
		return ErrConfirmedNeedsForce
	}
	ok, err := s.repo.RescheduleReview(ctx, sessionID, force)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// processCandidate 处理一个尚无质检记录的已结束会话：抽样 → 规则 → （可选）打分。
func (s *QualityService) processCandidate(ctx context.Context, session *models.Session) error {
	now := s.nowFn()
	if !selectedBySample(session.ID, s.cfg.SampleRate) {
		// 未入选也要落一行：下轮 NOT EXISTS 候选集自然排除（确定性抽样保证重扫结论一致）
		return s.insertSkipped(ctx, session, now)
	}

	messages, err := s.repo.ListMessages(ctx, session.ID)
	if err != nil {
		return fmt.Errorf("list messages: %w", err)
	}
	if len(messages) < s.cfg.MinMessages {
		return s.insertSkipped(ctx, session, now)
	}

	violations := s.engine.Run(ctx, RuleInput{Session: session, Messages: messages, Config: s.cfg.Rules})
	review := newReviewRow(session, messages, violations, now)
	inserted, err := s.repo.InsertReviewIfAbsent(ctx, review)
	if err != nil {
		return fmt.Errorf("insert review: %w", err)
	}
	if !inserted {
		return nil // 并发竞争落败，另一 worker 已处理
	}
	if !s.ScorerEnabled() {
		// rules-only：直接置 scored（LLM 字段留空），后续可人工 rescore 补打分
		_, err := s.repo.MarkReviewScored(ctx, session.ID, []string{"pending", "failed"}, ScoredFields{ScoredAt: now})
		return err
	}
	return s.scoreAndPersist(ctx, session.ID, messages, review.AttemptCount, now)
}

// rescore 复活一条 failed 记录：重读消息重新打分（规则违规沿用已落库值）。
func (s *QualityService) rescore(ctx context.Context, review *models.QualityReview) error {
	now := s.nowFn()
	messages, err := s.repo.ListMessages(ctx, review.SessionID)
	if err != nil {
		return fmt.Errorf("list messages: %w", err)
	}
	if !s.ScorerEnabled() {
		_, err := s.repo.MarkReviewScored(ctx, review.SessionID, []string{"pending", "failed"}, ScoredFields{ScoredAt: now})
		return err
	}
	return s.scoreAndPersist(ctx, review.SessionID, messages, review.AttemptCount, now)
}

// scoreAndPersist 调用 LLM 打分并 CAS 落库；失败走重试记账。
func (s *QualityService) scoreAndPersist(ctx context.Context, sessionID string, messages []models.Message, prevAttempts int, now time.Time) error {
	turns := make([]TranscriptTurn, 0, len(messages))
	for i := range messages {
		turns = append(turns, TranscriptTurn{Role: messages[i].Sender, At: messages[i].CreatedAt, Content: messages[i].Content})
	}
	result, err := s.scorer.ScoreSession(ctx, ScoreRequest{SessionID: sessionID, Turns: turns, Dimensions: ScoringDimensions})
	if err != nil {
		attempts := prevAttempts + 1
		nextRetry := now.Add(time.Duration(s.cfg.RetryBackoffSeconds) * time.Second)
		if _, markErr := s.repo.MarkReviewFailed(ctx, sessionID, []string{"pending", "failed"}, attempts, nextRetry, err.Error()); markErr != nil {
			return markErr
		}
		return err
	}
	dimensionsJSON, err := json.Marshal(result.Dimensions)
	if err != nil {
		return fmt.Errorf("marshal dimensions: %w", err)
	}
	total := result.TotalScore
	_, err = s.repo.MarkReviewScored(ctx, sessionID, []string{"pending", "failed"}, ScoredFields{
		DimensionsJSON: string(dimensionsJSON),
		TotalScore:     &total,
		Summary:        result.Summary,
		Provider:       result.Provider,
		Model:          result.Model,
		ScoredAt:       now,
	})
	return err
}

func (s *QualityService) insertSkipped(ctx context.Context, session *models.Session, now time.Time) error {
	customerID := session.UserID
	review := &models.QualityReview{
		TenantID:        session.TenantID,
		WorkspaceID:     session.WorkspaceID,
		SessionID:       session.ID,
		CustomerID:      &customerID,
		AgentID:         session.AgentID,
		Status:          "skipped",
		Trigger:         "worker",
		MessageCount:    0,
		DurationSeconds: durationSeconds(session),
	}
	_, err := s.repo.InsertReviewIfAbsent(ctx, review)
	return err
}

// newReviewRow 组装 pending 记录（违规 JSON 化）。
func newReviewRow(session *models.Session, messages []models.Message, violations []Violation, now time.Time) *models.QualityReview {
	violationsJSON, _ := json.Marshal(violations) // []Violation 无不可序列化字段
	customerID := session.UserID
	return &models.QualityReview{
		TenantID:        session.TenantID,
		WorkspaceID:     session.WorkspaceID,
		SessionID:       session.ID,
		CustomerID:      &customerID,
		AgentID:         session.AgentID,
		Status:          "pending",
		Trigger:         "worker",
		MessageCount:    len(messages),
		DurationSeconds: durationSeconds(session),
		ViolationsJSON:  string(violationsJSON),
		ViolationCount:  len(violations),
		MaxSeverity:     maxSeverity(violations),
	}
}

func durationSeconds(session *models.Session) int {
	if session == nil || session.EndedAt == nil {
		return 0
	}
	secs := int(session.EndedAt.Sub(session.StartedAt).Seconds())
	if secs < 0 {
		return 0
	}
	return secs
}

func maxSeverity(violations []Violation) string {
	order := map[string]int{SeverityLow: 1, SeverityMedium: 2, SeverityHigh: 3}
	best := 0
	severity := ""
	for i := range violations {
		if order[violations[i].Severity] > best {
			best = order[violations[i].Severity]
			severity = violations[i].Severity
		}
	}
	return severity
}

// selectedBySample 确定性抽样：fnv32a(session_id)%10000 < sample_rate*100。
// 同一会话每轮结论一致，避免「上轮抽中下轮落榜」。
func selectedBySample(sessionID string, sampleRate float64) bool {
	if sampleRate >= 100 {
		return true
	}
	if sampleRate <= 0 {
		return false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(sessionID))
	return int(h.Sum32()%10000) < int(sampleRate*100)
}
