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
	// 先新建后切换：WeKnora UploadDocument 无幂等 upsert，更新场景（本地映射
	// 的 ExternalID 非空）不删旧会残留旧外部文档被检索命中。先上传新文档再删
	// 旧：上传失败时旧内容仍在远端可用；删旧失败时回滚刚上传的新文档（远端
	// 收敛为旧文档原样、无孤儿），回滚也失败时错误透出新旧 id 以便追踪残留。
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
	if oldID := strings.TrimSpace(doc.ExternalID); oldID != "" {
		if err := p.client.DeleteDocument(ctx, namespace.KnowledgeID, oldID); err != nil {
			if rollbackErr := p.client.DeleteDocument(ctx, namespace.KnowledgeID, info.ID); rollbackErr != nil {
				return "", fmt.Errorf("delete stale weknora document %s: %v; rollback delete of new document %s also failed: %w", oldID, err, info.ID, rollbackErr)
			}
			return "", fmt.Errorf("delete stale weknora document %s: %w", oldID, err)
		}
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

func (p *Provider) HealthCheck(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("weknora client is not configured")
	}
	return p.client.HealthCheck(ctx)
}
