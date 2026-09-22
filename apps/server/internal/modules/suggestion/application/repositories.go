package application

import (
	"context"
	"time"
)

type TicketCandidate struct {
	ID          uint
	Title       string
	Description string
	Status      string
	Category    string
	Priority    string
	CreatedAt   time.Time
}

type KnowledgeDocCandidate struct {
	ID       uint
	Title    string
	Content  string
	Category string
	Tags     string
}

// ExposureRecord 一次客户侧推荐曝光（P2-0 RQ-5）：initial/next 接口成功
// 返回时由服务端落一行，Questions 为本次曝光的问题文案列表。
type ExposureRecord struct {
	SessionID string
	Kind      string // initial|next
	Strategy  string
	Questions []string
}

// OpenExposure 待转化曝光（该 session 最近一次未转化的曝光行）。
type OpenExposure struct {
	ID        uint
	Questions []string
}

// ExposureSummary 曝光/转化聚合（最小口径，不做看板）。
type ExposureSummary struct {
	TotalExposures     int64
	ConvertedExposures int64
	ByKind             []ExposureKindSummary
}

// ExposureKindSummary 按 kind（initial|next）分组的曝光/转化计数。
type ExposureKindSummary struct {
	Kind               string
	TotalExposures     int64
	ConvertedExposures int64
}

type Repository interface {
	FindTicketCandidates(ctx context.Context, tokens []string, candidateMax int) ([]TicketCandidate, error)
	FindKnowledgeDocCandidates(ctx context.Context, tokens []string) ([]KnowledgeDocCandidate, error)
	// FindPublicKnowledgeDocs 客户侧首屏推荐（P2-0 RQ-1）：仅 is_public
	// 文档，updated_at 新近优先。
	FindPublicKnowledgeDocs(ctx context.Context, limit int) ([]KnowledgeDocCandidate, error)
	// FindPublicKnowledgeDocCandidates 客户侧上下文联想（P2-0 RQ-2）：
	// 仅 is_public 文档的 token 候选集（客户侧绝不过非公开数据）。
	FindPublicKnowledgeDocCandidates(ctx context.Context, tokens []string) ([]KnowledgeDocCandidate, error)
	// RecordExposure 记录一次客户侧推荐曝光（P2-0 RQ-5，服务端归因口径）。
	RecordExposure(ctx context.Context, rec ExposureRecord) error
	// FindLatestOpenExposure 取该 session 最近一次未转化曝光；无则返回
	// nil, nil（公开接口匿名访问，曝光日志不区分租户，不做 scope 过滤）。
	FindLatestOpenExposure(ctx context.Context, sessionID string) (*OpenExposure, error)
	// MarkExposureConverted 标记转化命中（converted_question 非空即转化）。
	MarkExposureConverted(ctx context.Context, exposureID uint, question string, at time.Time) error
	// ExposureSummary 曝光/转化聚合（管理面最小口径查询）。
	ExposureSummary(ctx context.Context) (*ExposureSummary, error)
}
