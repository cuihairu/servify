package memory

import (
	"context"
	"strings"
	"sync"

	"servify/apps/server/internal/platform/knowledgeprovider"
)

// Provider is an in-memory implementation used for provider-switch testing and non-network fallback.
type Provider struct {
	mu              sync.RWMutex
	defaultTenantID string
	defaultKBID     string
	documents       map[string]knowledgeprovider.KnowledgeDocument
}

func NewProvider(defaultTenantID, defaultKnowledgeID string) *Provider {
	return &Provider{
		defaultTenantID: defaultTenantID,
		defaultKBID:     defaultKnowledgeID,
		documents:       map[string]knowledgeprovider.KnowledgeDocument{},
	}
}

// ProviderName 实现 NamedProvider：knowledge_docs.provider_id 落库标识。
func (p *Provider) ProviderName() string { return "memory" }

func (p *Provider) Search(ctx context.Context, req knowledgeprovider.SearchRequest) ([]knowledgeprovider.KnowledgeHit, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	ns := knowledgeprovider.ResolveNamespace(p.defaultTenantID, p.defaultKBID, req.TenantID, req.KnowledgeID)
	query := strings.ToLower(strings.TrimSpace(req.Query))
	hits := make([]knowledgeprovider.KnowledgeHit, 0, len(p.documents))
	for _, doc := range p.documents {
		docNS := knowledgeprovider.ResolveNamespace(p.defaultTenantID, p.defaultKBID, doc.TenantID, doc.KnowledgeID)
		if ns.KnowledgeID != "" && docNS.KnowledgeID != ns.KnowledgeID {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(doc.Title+" "+doc.Content), query) {
			continue
		}
		hits = append(hits, knowledgeprovider.KnowledgeHit{
			DocumentID: doc.ID,
			Title:      doc.Title,
			Content:    doc.Content,
			Score:      1,
			Source:     "memory",
			Metadata: map[string]interface{}{
				"tenant_id":    docNS.TenantID,
				"knowledge_id": docNS.KnowledgeID,
				"consistency":  knowledgeprovider.ConsistencyStrong,
			},
		})
	}
	if req.TopK > 0 && len(hits) > req.TopK {
		hits = hits[:req.TopK]
	}
	return hits, nil
}

func (p *Provider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ns := knowledgeprovider.ResolveNamespace(p.defaultTenantID, p.defaultKBID, doc.TenantID, doc.KnowledgeID)
	doc.TenantID = ns.TenantID
	doc.KnowledgeID = ns.KnowledgeID
	if strings.TrimSpace(doc.ID) == "" {
		doc.ID = strings.TrimSpace(doc.Title)
	}
	if strings.TrimSpace(doc.ExternalID) == "" {
		doc.ExternalID = doc.ID
	}
	p.documents[doc.ID] = doc
	return doc.ExternalID, nil
}

func (p *Provider) DeleteDocument(ctx context.Context, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.documents, id)
	return nil
}

func (p *Provider) RebuildIndex(ctx context.Context, req knowledgeprovider.RebuildRequest) error {
	return nil
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	return nil
}

var _ knowledgeprovider.KnowledgeProvider = (*Provider)(nil)
var _ knowledgeprovider.RebuildableProvider = (*Provider)(nil)
