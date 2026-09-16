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

type Repository interface {
	FindTicketCandidates(ctx context.Context, tokens []string, candidateMax int) ([]TicketCandidate, error)
	FindKnowledgeDocCandidates(ctx context.Context, tokens []string) ([]KnowledgeDocCandidate, error)
	// FindPublicKnowledgeDocs 客户侧首屏推荐（P2-0 RQ-1）：仅 is_public
	// 文档，updated_at 新近优先。
	FindPublicKnowledgeDocs(ctx context.Context, limit int) ([]KnowledgeDocCandidate, error)
	// FindPublicKnowledgeDocCandidates 客户侧上下文联想（P2-0 RQ-2）：
	// 仅 is_public 文档的 token 候选集（客户侧绝不过非公开数据）。
	FindPublicKnowledgeDocCandidates(ctx context.Context, tokens []string) ([]KnowledgeDocCandidate, error)
}
