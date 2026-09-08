package memory

import (
	"context"
	"testing"

	"servify/apps/server/internal/platform/knowledgeprovider"
)

func TestProviderSearchAndNamespaceMapping(t *testing.T) {
	provider := NewProvider("tenant-default", "kb-default")
	externalID, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID:       "doc-1",
		TenantID: "tenant-a",
		Title:    "Billing",
		Content:  "Billing details",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if externalID != "doc-1" {
		t.Fatalf("unexpected external id: %q", externalID)
	}

	hits, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{
		Query:    "billing",
		TenantID: "tenant-a",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].Source != "memory" {
		t.Fatalf("unexpected source: %+v", hits[0])
	}
}

func TestProviderSearchFiltersAndTopK(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider("tenant-default", "kb-default")

	for _, doc := range []knowledgeprovider.KnowledgeDocument{
		{ID: "doc-a", TenantID: "tenant-a", Title: "Alpha Guide", Content: "covers billing"},
		{ID: "doc-b", TenantID: "tenant-a", KnowledgeID: "kb-2", Title: "Beta Guide", Content: "covers routing"},
		{ID: "doc-c", TenantID: "tenant-b", Title: "Gamma Guide", Content: "covers billing"},
	} {
		if _, err := provider.UpsertDocument(ctx, doc); err != nil {
			t.Fatalf("upsert %s: %v", doc.ID, err)
		}
	}

	// knowledge-id filter excludes doc-b (kb-2)
	hits, err := provider.Search(ctx, knowledgeprovider.SearchRequest{KnowledgeID: "kb-default"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("kb filter hits = %+v", hits)
	}

	// query filter: no match
	hits, err = provider.Search(ctx, knowledgeprovider.SearchRequest{Query: "  nothing  "})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected no hits, got %+v", hits)
	}

	// topk truncation (doc-b kb-2 excluded by default kb namespace)
	hits, err = provider.Search(ctx, knowledgeprovider.SearchRequest{Query: "guide"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	hits, err = provider.Search(ctx, knowledgeprovider.SearchRequest{Query: "guide", TopK: 1})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected topk=1 hit, got %d", len(hits))
	}
}

func TestProviderUpsertGeneratesIDFromTitle(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider("", "")
	externalID, err := provider.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{Title: "Generated Title"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if externalID != "Generated Title" {
		t.Fatalf("expected generated external id, got %q", externalID)
	}
}

func TestProviderDeleteRebuildHealth(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider("", "")
	if _, err := provider.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{ID: "doc-x", Title: "X"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := provider.DeleteDocument(ctx, "doc-x"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := provider.DeleteDocument(ctx, "missing"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
	hits, err := provider.Search(ctx, knowledgeprovider.SearchRequest{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected empty search after delete, got %+v", hits)
	}
	if err := provider.RebuildIndex(ctx, knowledgeprovider.RebuildRequest{}); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if err := provider.HealthCheck(ctx); err != nil {
		t.Fatalf("health: %v", err)
	}
}
