package mock

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/platform/knowledgeprovider"
)

func TestProviderSearchFilteredByQuery(t *testing.T) {
	p := &Provider{
		Hits: []knowledgeprovider.KnowledgeHit{
			{DocumentID: "d1", Title: "Refund Policy", Content: "How to refund orders"},
			{DocumentID: "d2", Title: "Shipping", Content: "Delivery times"},
		},
	}

	// 命中标题
	hits, err := p.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "refund"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 1 || hits[0].DocumentID != "d1" {
		t.Fatalf("expected only d1, got %+v", hits)
	}

	// 命中正文（大小写不敏感）
	hits, err = p.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "DELIVERY"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 1 || hits[0].DocumentID != "d2" {
		t.Fatalf("expected only d2, got %+v", hits)
	}

	// 无命中时返回全部（mock 的宽松语义）
	hits, err = p.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "nothing-matches"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected all hits as fallback, got %+v", hits)
	}
}

func TestProviderSearchEmptyQueryAndEmptyHits(t *testing.T) {
	// 空 Query 直接返回预设 Hits
	all := []knowledgeprovider.KnowledgeHit{{DocumentID: "d1"}}
	p := &Provider{Hits: all}
	hits, err := p.Search(context.Background(), knowledgeprovider.SearchRequest{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 1 || hits[0].DocumentID != "d1" {
		t.Fatalf("expected preset hits, got %+v", hits)
	}

	// 无预设 Hits 且带查询词：返回空
	empty := &Provider{}
	hits, err = empty.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "anything"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if hits != nil {
		t.Fatalf("expected nil hits, got %+v", hits)
	}
}

func TestProviderSearchError(t *testing.T) {
	wantErr := errors.New("search down")
	p := &Provider{SearchError: wantErr}
	if _, err := p.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q"}); !errors.Is(err, wantErr) {
		t.Fatalf("Search() error = %v, want %v", err, wantErr)
	}
}

func TestProviderUpsertDocument(t *testing.T) {
	p := &Provider{}

	// 以 ID 为 key，且空 ExternalID 回填为 key
	id, err := p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID: "doc-1", Title: "T", Content: "C",
	})
	if err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if id != "doc-1" {
		t.Fatalf("external id = %q, want doc-1", id)
	}
	if doc, ok := p.Documents["doc-1"]; !ok || doc.ExternalID != "doc-1" {
		t.Fatalf("document not stored correctly: %+v", doc)
	}

	// 无 ID 时以 Title 为 key
	id, err = p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{Title: "title-key"})
	if err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if id != "title-key" {
		t.Fatalf("external id = %q, want title-key", id)
	}
	if _, ok := p.Documents["title-key"]; !ok {
		t.Fatal("document keyed by title not stored")
	}

	// 已有 ExternalID 保持不变
	id, err = p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{ID: "doc-2", ExternalID: "ext-2"})
	if err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if id != "ext-2" {
		t.Fatalf("external id = %q, want ext-2", id)
	}
}

func TestProviderDeleteDocument(t *testing.T) {
	p := &Provider{Documents: map[string]knowledgeprovider.KnowledgeDocument{
		"doc-1": {ID: "doc-1"},
	}}
	if err := p.DeleteDocument(context.Background(), "doc-1"); err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}
	if _, ok := p.Documents["doc-1"]; ok {
		t.Fatal("document should be deleted")
	}

	// Documents 为 nil 时删除也安全
	if err := (&Provider{}).DeleteDocument(context.Background(), "x"); err != nil {
		t.Fatalf("DeleteDocument() on nil map error = %v", err)
	}

	wantErr := errors.New("delete forbidden")
	p2 := &Provider{DeleteError: wantErr}
	if err := p2.DeleteDocument(context.Background(), "doc-1"); !errors.Is(err, wantErr) {
		t.Fatalf("DeleteDocument() error = %v, want %v", err, wantErr)
	}
}

func TestProviderHealthCheckAndRebuildIndex(t *testing.T) {
	healthy := &Provider{}
	if err := healthy.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
	if err := healthy.RebuildIndex(context.Background(), knowledgeprovider.RebuildRequest{Reason: "test"}); err != nil {
		t.Fatalf("RebuildIndex() error = %v", err)
	}

	wantErr := errors.New("unhealthy")
	sick := &Provider{HealthError: wantErr}
	if err := sick.HealthCheck(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("HealthCheck() error = %v, want %v", err, wantErr)
	}
}
