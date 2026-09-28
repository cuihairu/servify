package weknora

import (
	"context"
	"fmt"
	"strings"

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

// ProviderName 实现 NamedProvider：knowledge_docs.provider_id 落库标识。
func (p *Provider) ProviderName() string { return "weknora" }

func (p *Provider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("weknora client is not configured")
	}
	namespace := knowledgeprovider.ResolveNamespace("", p.knowledgeID, doc.TenantID, doc.KnowledgeID)
	if namespace.KnowledgeID == "" {
		return "", fmt.Errorf("knowledge base id is not configured")
	}
	// 删旧建新：WeKnora UploadDocument 无幂等 upsert，更新场景不删旧会残留
	// 旧外部文档（检索可命中过期内容）。与 ragflow/dify 同语义——本地映射的
	// ExternalID 非空时先删旧再上传，删旧失败即中断（管理端可重试收敛）。
	if oldID := strings.TrimSpace(doc.ExternalID); oldID != "" {
		if err := p.client.DeleteDocument(ctx, namespace.KnowledgeID, oldID); err != nil {
			return "", fmt.Errorf("delete stale weknora document %s: %w", oldID, err)
		}
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
