package mock

import (
	"context"
	"strings"

	"servify/apps/server/internal/platform/knowledgeprovider"
)

// Provider is a controllable mock implementation of knowledgeprovider.KnowledgeProvider.
type Provider struct {
	Hits        []knowledgeprovider.KnowledgeHit
	SearchError error
	DeleteError error
	HealthError error
	Documents   map[string]knowledgeprovider.KnowledgeDocument
}

// ProviderName 实现 NamedProvider：knowledge_docs.provider_id 落库标识。
func (p *Provider) ProviderName() string { return "mock" }

func (p *Provider) Search(ctx context.Context, req knowledgeprovider.SearchRequest) ([]knowledgeprovider.KnowledgeHit, error) {
	if p.SearchError != nil {
		return nil, p.SearchError
	}
	if req.Query == "" {
		return p.Hits, nil
	}
	if len(p.Hits) == 0 {
		return nil, nil
	}

	query := strings.ToLower(req.Query)
	result := make([]knowledgeprovider.KnowledgeHit, 0, len(p.Hits))
	for _, hit := range p.Hits {
		if strings.Contains(strings.ToLower(hit.Title), query) || strings.Contains(strings.ToLower(hit.Content), query) {
			result = append(result, hit)
		}
	}
	if len(result) == 0 {
		return p.Hits, nil
	}
	return result, nil
}

func (p *Provider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
	if p.Documents == nil {
		p.Documents = make(map[string]knowledgeprovider.KnowledgeDocument)
	}
	key := doc.ID
	if key == "" {
		key = doc.Title
	}
	if strings.TrimSpace(doc.ExternalID) == "" {
		doc.ExternalID = key
	}
	p.Documents[key] = doc
	return doc.ExternalID, nil
}

func (p *Provider) DeleteDocument(ctx context.Context, id string) error {
	if p.DeleteError != nil {
		return p.DeleteError
	}
	if p.Documents != nil {
		delete(p.Documents, id)
	}
	return nil
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	return p.HealthError
}

func (p *Provider) RebuildIndex(ctx context.Context, req knowledgeprovider.RebuildRequest) error {
	return nil
}
