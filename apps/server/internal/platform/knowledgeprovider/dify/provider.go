package dify

import (
	"context"
	"fmt"
	"strings"

	"servify/apps/server/internal/platform/knowledgeprovider"
	base "servify/apps/server/pkg/dify"
)

type Provider struct {
	client    base.ClientInterface
	datasetID string
	search    SearchConfig
}

type SearchConfig struct {
	TopK            int
	ScoreThreshold  float64
	SearchMethod    string
	RerankingEnable bool
}

func NewProvider(client base.ClientInterface, datasetID string, search SearchConfig) *Provider {
	return &Provider{client: client, datasetID: datasetID, search: search}
}

func (p *Provider) Search(ctx context.Context, req knowledgeprovider.SearchRequest) ([]knowledgeprovider.KnowledgeHit, error) {
	if p.client == nil {
		return nil, fmt.Errorf("dify client is not configured")
	}
	datasetID := req.KnowledgeID
	if datasetID == "" {
		datasetID = p.datasetID
	}
	if datasetID == "" {
		return nil, fmt.Errorf("dify dataset id is not configured")
	}

	topK := req.TopK
	if topK <= 0 {
		topK = p.search.TopK
	}
	threshold := req.Threshold
	if threshold <= 0 {
		threshold = p.search.ScoreThreshold
	}
	searchMethod := req.Strategy
	if searchMethod == "" {
		searchMethod = p.search.SearchMethod
	}
	if searchMethod == "" {
		searchMethod = "semantic_search"
	}

	resp, err := p.client.Retrieve(ctx, datasetID, &base.RetrieveRequest{
		Query: req.Query,
		RetrievalModel: base.RetrievalModel{
			SearchMethod:    searchMethod,
			RerankingEnable: p.search.RerankingEnable,
			TopK:            topK,
			ScoreThreshold:  threshold,
		},
	})
	if err != nil {
		return nil, err
	}

	hits := make([]knowledgeprovider.KnowledgeHit, 0, len(resp.Records))
	for _, record := range resp.Records {
		hits = append(hits, knowledgeprovider.KnowledgeHit{
			DocumentID: record.DocumentID,
			Title:      record.Title,
			Content:    record.Content,
			Score:      record.Score,
			Source:     "dify",
			Metadata:   record.Metadata,
		})
	}
	return hits, nil
}

// ProviderName 实现 NamedProvider：knowledge_docs.provider_id 落库标识。
func (p *Provider) ProviderName() string { return "dify" }

func (p *Provider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("dify client is not configured")
	}
	datasetID := doc.KnowledgeID
	if datasetID == "" {
		datasetID = p.datasetID
	}
	if datasetID == "" {
		return "", fmt.Errorf("dify dataset id is not configured")
	}

	// 删旧建新：Dify 无幂等 upsert，纯 CreateDocumentFromText 会在更新场景
	// 残留旧外部文档（检索可命中过期内容）。与 ragflow 同语义——本地映射的
	// ExternalID 非空时先删旧再建新，删旧失败即中断（管理端可重试收敛）。
	if oldID := docID(doc.ExternalID); oldID != "" {
		if err := p.client.DeleteDocument(ctx, datasetID, oldID); err != nil {
			return "", fmt.Errorf("delete stale dify document %s: %w", oldID, err)
		}
	}

	created, err := p.client.CreateDocumentFromText(ctx, datasetID, &base.CreateDocumentRequest{
		Name:              doc.Title,
		Text:              doc.Content,
		IndexingTechnique: "high_quality",
		ProcessRule:       base.ProcessRule{Mode: "automatic"},
		RetrievalModel: base.RetrievalModel{
			SearchMethod:    p.search.SearchMethod,
			RerankingEnable: p.search.RerankingEnable,
			TopK:            p.search.TopK,
			ScoreThreshold:  p.search.ScoreThreshold,
		},
	})
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

func (p *Provider) DeleteDocument(ctx context.Context, id string) error {
	if p.client == nil {
		return fmt.Errorf("dify client is not configured")
	}
	if p.datasetID == "" {
		return fmt.Errorf("dify dataset id is not configured")
	}
	if id = docID(id); id == "" {
		return fmt.Errorf("dify document id is not configured")
	}
	return p.client.DeleteDocument(ctx, p.datasetID, id)
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("dify client is not configured")
	}
	if p.datasetID == "" {
		return fmt.Errorf("dify dataset id is not configured")
	}
	return p.client.HealthCheck(ctx, p.datasetID)
}

func docID(id string) string {
	return strings.TrimSpace(id)
}
