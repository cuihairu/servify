package ragflow

import (
	"context"
	"fmt"
	"strings"

	"servify/apps/server/internal/platform/knowledgeprovider"
	base "servify/apps/server/pkg/ragflow"
)

type Provider struct {
	client    base.ClientInterface
	datasetID string
	search    SearchConfig
}

type SearchConfig struct {
	TopK           int
	ScoreThreshold float64
}

func NewProvider(client base.ClientInterface, datasetID string, search SearchConfig) *Provider {
	return &Provider{client: client, datasetID: datasetID, search: search}
}

func (p *Provider) Search(ctx context.Context, req knowledgeprovider.SearchRequest) ([]knowledgeprovider.KnowledgeHit, error) {
	if p.client == nil {
		return nil, fmt.Errorf("ragflow client is not configured")
	}
	datasetID := req.KnowledgeID
	if datasetID == "" {
		datasetID = p.datasetID
	}
	if datasetID == "" {
		return nil, fmt.Errorf("ragflow dataset id is not configured")
	}

	topK := req.TopK
	if topK <= 0 {
		topK = p.search.TopK
	}
	if topK <= 0 {
		topK = 10
	}
	threshold := req.Threshold
	if threshold <= 0 {
		threshold = p.search.ScoreThreshold
	}
	if threshold <= 0 {
		threshold = 0.2
	}

	resp, err := p.client.Retrieve(ctx, &base.RetrieveRequest{
		Question:            req.Query,
		DatasetIDs:          []string{datasetID},
		PageSize:            topK,
		SimilarityThreshold: threshold,
		TopK:                topK,
	})
	if err != nil {
		return nil, err
	}

	hits := make([]knowledgeprovider.KnowledgeHit, 0, len(resp.Chunks))
	for _, chunk := range resp.Chunks {
		hits = append(hits, knowledgeprovider.KnowledgeHit{
			DocumentID: chunk.DocumentID,
			Title:      chunk.DocumentKeyword,
			Content:    chunk.Content,
			Score:      chunk.Similarity,
			Source:     "ragflow",
			Metadata:   map[string]interface{}{"dataset_id": chunk.DatasetID},
		})
	}
	return hits, nil
}

// UpsertDocument 实现 RAGFlow 无幂等 upsert 的删旧建新：先按名查重删除
// 旧文档，再上传并触发异步解析（解析为 RAGFlow 侧管线，失败按重试收敛）。
func (p *Provider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("ragflow client is not configured")
	}
	datasetID := doc.KnowledgeID
	if datasetID == "" {
		datasetID = p.datasetID
	}
	if datasetID == "" {
		return "", fmt.Errorf("ragflow dataset id is not configured")
	}
	title := strings.TrimSpace(doc.Title)
	if title == "" {
		return "", fmt.Errorf("ragflow document title is not configured")
	}
	// 上传文件名为 title.txt，查重必须按同一文件名（而非裸标题）过滤，
	// 否则旧文档永远查不中、去重失效。
	fileName := title + ".txt"

	existing, err := p.client.ListDocuments(ctx, datasetID, fileName)
	if err != nil {
		return "", err
	}
	if len(existing) > 0 {
		oldIDs := make([]string, 0, len(existing))
		for _, item := range existing {
			oldIDs = append(oldIDs, item.ID)
		}
		if err := p.client.DeleteDocuments(ctx, datasetID, oldIDs); err != nil {
			return "", err
		}
	}

	created, err := p.client.UploadDocument(ctx, datasetID, title, doc.Content)
	if err != nil {
		return "", err
	}
	if err := p.client.ParseDocuments(ctx, datasetID, []string{created.ID}); err != nil {
		return "", err
	}
	return created.ID, nil
}
func (p *Provider) DeleteDocument(ctx context.Context, id string) error {
	if p.client == nil {
		return fmt.Errorf("ragflow client is not configured")
	}
	if p.datasetID == "" {
		return fmt.Errorf("ragflow dataset id is not configured")
	}
	if id = strings.TrimSpace(id); id == "" {
		return fmt.Errorf("ragflow document id is not configured")
	}
	return p.client.DeleteDocuments(ctx, p.datasetID, []string{id})
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("ragflow client is not configured")
	}
	if p.datasetID == "" {
		return fmt.Errorf("ragflow dataset id is not configured")
	}
	return p.client.HealthCheck(ctx, p.datasetID)
}
