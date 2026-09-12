package application

import (
	"context"
	"errors"
	"time"

	"servify/apps/server/internal/models"
)

// ErrNotFound 质检记录不存在。
var ErrNotFound = errors.New("quality review not found")

// ScoringDimensions 是 LLM 打分的默认维度（key/权重固定，prompt 文案内置）。
var ScoringDimensions = []ScoringDimension{
	{Key: "attitude", Prompt: "服务态度与礼貌程度", Weight: 0.3},
	{Key: "resolution", Prompt: "问题理解与解决有效性", Weight: 0.5},
	{Key: "timeliness", Prompt: "响应及时性与节奏把控", Weight: 0.2},
}

// Repository 质检仓储（postgres 与 sqlite 通用，均为 worker 系统级访问、无租户 scope 注入）。
type Repository interface {
	// ListReviewCandidates 返回 lookback 之后结束、且尚无质检记录的会话（按结束时间升序）。
	ListReviewCandidates(ctx context.Context, lookback time.Time, limit int) ([]models.Session, error)
	// ListReviewsForRetry 返回可重试的 failed 记录（attempt<max 且退避到期）。
	ListReviewsForRetry(ctx context.Context, maxAttempts int, now time.Time, limit int) ([]models.QualityReview, error)
	// GetReviewBySession 按会话取质检记录；不存在返回 ErrNotFound。
	GetReviewBySession(ctx context.Context, sessionID string) (*models.QualityReview, error)
	// ListMessages 按时间升序返回会话全部消息。
	ListMessages(ctx context.Context, sessionID string) ([]models.Message, error)
	// InsertReviewIfAbsent 插入质检记录（session_id 冲突时跳过），返回是否实际插入。
	InsertReviewIfAbsent(ctx context.Context, review *models.QualityReview) (bool, error)
	// MarkReviewScored CAS 更新为 scored：仅当当前 status 在 allowedFrom 内才写，
	// 返回是否命中（绝不覆盖 confirmed 与人工字段）。
	MarkReviewScored(ctx context.Context, sessionID string, allowedFrom []string, fields ScoredFields) (bool, error)
	// MarkReviewFailed 记录一次打分失败：attempt_count 置为 attempts、退避与错误落库。
	// 仅当当前 status 在 allowedFrom 内才写。
	MarkReviewFailed(ctx context.Context, sessionID string, allowedFrom []string, attempts int, nextRetry time.Time, lastErr string) (bool, error)
}

// ScoredFields 是打分完成后的落库字段。
type ScoredFields struct {
	DimensionsJSON string
	TotalScore     *float64
	Summary        string
	Provider       string
	Model          string
	ScoredAt       time.Time
}
