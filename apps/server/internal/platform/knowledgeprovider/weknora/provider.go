package weknora

import (
	"context"
	"fmt"

	"servify/apps/server/internal/platform/knowledgeprovider"
	base "servify/apps/server/pkg/weknora"
)

// Provider adapts the existing WeKnora client to the KnowledgeProvider contract.
type Provider struct {
	client      base.WeKnoraInterface
	knowledgeID string
}

func NewProvider(client base.WeKnoraInterface, knowledgeID string) *Provider {
	return &Provider{
		client:      client,
		knowledgeID: knowledgeID,
	}
}

func (p *Provider) Search(ctx context.Context, req knowledgeprovider.SearchRequest) ([]knowledgeprovider.KnowledgeHit, error) {
	if p.client == nil {
		return nil, fmt.Errorf("weknora client is not configured")
	}
	namespace := knowledgeprovider.ResolveNamespace("", p.knowledgeID, req.TenantID, req.KnowledgeID)
	resp, err := p.client.SearchKnowledge(ctx, &base.SearchRequest{
		Query:           req.Query,
		KnowledgeBaseID: namespace.KnowledgeID,
		Limit:           req.TopK,
		Threshold:       req.Threshold,
		Strategy:        req.Strategy,
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("weknora search failed: %s", resp.Message)
	}

	hits := make([]knowledgeprovider.KnowledgeHit, 0, len(resp.Data.Results))
	for _, result := range resp.Data.Results {
		hits = append(hits, knowledgeprovider.KnowledgeHit{
			DocumentID: result.DocumentID,
			Title:      result.Title,
			Content:    result.Content,
			Score:      result.Score,
			Source:     result.Source,
			Metadata:   result.Metadata,
		})
	}
	return hits, nil
}

func (p *Provider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("weknora client is not configured")
	}
	namespace := knowledgeprovider.ResolveNamespace("", p.knowledgeID, doc.TenantID, doc.KnowledgeID)
	if namespace.KnowledgeID == "" {
		return "", fmt.Errorf("knowledge base id is not configured")
	}
	info, err := p.client.UploadDocument(ctx, namespace.KnowledgeID, &base.Document{
		Type:     "text",
		Title:    doc.Title,
		Content:  doc.Content,
		Tags:     doc.Tags,
		Metadata: doc.Metadata,
	})
	if err != nil {
		return "", err
	}
	return info.ID, nil
}

func (p *Provider) DeleteDocument(ctx context.Context, id string) error {
	if p.client == nil {
		return fmt.Errorf("weknora client is not configured")
	}
	if id == "" {
		return fmt.Errorf("external document id is required")
	}
	if p.knowledgeID == "" {
		return fmt.Errorf("knowledge base id is not configured")
	}
	return p.client.DeleteDocument(ctx, p.knowledgeID, id)
}

func (p *Provider) RebuildIndex(ctx context.Context, req knowledgeprovider.RebuildRequest) error {
	if p.client == nil {
		return fmt.Errorf("weknora client is not configured")
	}
	return nil
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("weknora client is not configured")
	}
	return p.client.HealthCheck(ctx)
}
