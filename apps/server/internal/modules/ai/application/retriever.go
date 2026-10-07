package application

import (
	"context"
	"strings"

	"servify/apps/server/internal/platform/knowledgeprovider"
)

// Retriever isolates knowledge retrieval policy from the orchestrator.
type Retriever struct {
	provider knowledgeprovider.KnowledgeProvider
}

func NewRetriever(provider knowledgeprovider.KnowledgeProvider) *Retriever {
	return &Retriever{provider: provider}
}

func (r *Retriever) Retrieve(ctx context.Context, req AIRequest) ([]knowledgeprovider.KnowledgeHit, error) {
	if r == nil || r.provider == nil {
		return nil, nil
	}
	if !req.RetrievalPolicy.Enabled {
		return nil, nil
	}
	if strings.TrimSpace(req.Query) == "" {
		return nil, nil
	}

	// KnowledgeID 留空：会话 id 不是知识库 id。此前把 ConversationID 透传成
	// SearchRequest.KnowledgeID，各 provider 据此按会话 id 过滤知识命名空间，
	// 而摄入侧的 workspace 永不等于会话 id ⇒ 检索恒空（acceptance-checklist
	// 已登记的基线缺口，验收脚本只能以无 session_id 查询规避）。留空让
	// provider 落回各自默认命名空间（WeKnora→配置的默认知识库；local/pgvector
	// →不做 workspace 过滤，租户隔离仍由 TenantID 承担）。
	searchReq := knowledgeprovider.SearchRequest{
		Query:           req.Query,
		TenantID:        req.TenantID,
		TopK:            req.RetrievalPolicy.TopK,
		Threshold:       req.RetrievalPolicy.Threshold,
		Strategy:        req.RetrievalPolicy.Strategy,
		ConsistencyMode: knowledgeprovider.ConsistencyEventual,
	}
	return r.provider.Search(ctx, searchReq)
}
