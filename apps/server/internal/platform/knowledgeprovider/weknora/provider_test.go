package weknora

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/platform/knowledgeprovider"
	base "servify/apps/server/pkg/weknora"
)

type mockClient struct {
	searchResp *base.SearchResponse
	searchErr  error
	uploadErr  error
	deleteErr  error
	healthyErr error
}

func (m *mockClient) CreateKnowledgeBase(ctx context.Context, req *base.CreateKBRequest) (*base.KnowledgeBase, error) {
	return nil, nil
}
func (m *mockClient) GetKnowledgeBase(ctx context.Context, kbID string) (*base.KnowledgeBase, error) {
	return nil, nil
}
func (m *mockClient) UploadDocument(ctx context.Context, kbID string, doc *base.Document) (*base.DocumentInfo, error) {
	return &base.DocumentInfo{ID: "doc-1", Title: doc.Title, ProcessedAt: time.Now()}, m.uploadErr
}
func (m *mockClient) DeleteDocument(ctx context.Context, kbID, docID string) error {
	return m.deleteErr
}
func (m *mockClient) SearchKnowledge(ctx context.Context, req *base.SearchRequest) (*base.SearchResponse, error) {
	return m.searchResp, m.searchErr
}
func (m *mockClient) CreateSession(ctx context.Context, req *base.SessionRequest) (*base.Session, error) {
	return nil, nil
}
func (m *mockClient) Chat(ctx context.Context, sessionID string, req *base.ChatRequest) (*base.ChatResponse, error) {
	return nil, nil
}
func (m *mockClient) HealthCheck(ctx context.Context) error { return m.healthyErr }

func TestProviderSearch(t *testing.T) {
	provider := NewProvider(&mockClient{
		searchResp: &base.SearchResponse{
			Success: true,
			Data: base.SearchData{
				Results: []base.SearchResult{
					{DocumentID: "doc-1", Title: "Billing", Content: "Billing content", Score: 0.95, Source: "weknora"},
				},
			},
		},
	}, "kb-1")

	hits, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{
		Query: "billing",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].DocumentID != "doc-1" {
		t.Fatalf("unexpected document id: %s", hits[0].DocumentID)
	}
}

func TestProviderHealthCheck(t *testing.T) {
	provider := NewProvider(&mockClient{}, "kb-1")
	if err := provider.HealthCheck(context.Background()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestProviderUpsertDocumentReturnsExternalID(t *testing.T) {
	provider := NewProvider(&mockClient{}, "kb-1")
	id, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID:      "doc-1",
		Title:   "Billing",
		Content: "Billing content",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != "doc-1" {
		t.Fatalf("expected returned external id, got %q", id)
	}
}

func TestProviderDeleteDocument(t *testing.T) {
	provider := NewProvider(&mockClient{}, "kb-1")
	if err := provider.DeleteDocument(context.Background(), "doc-1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestProviderDeleteDocumentErrorBranches(t *testing.T) {
	ctx := context.Background()

	nilProvider := NewProvider(nil, "kb-1")
	if err := nilProvider.DeleteDocument(ctx, "doc-1"); err == nil {
		t.Fatal("nil client delete should fail")
	}

	noDoc := NewProvider(&mockClient{}, "kb-1")
	if err := noDoc.DeleteDocument(ctx, ""); err == nil {
		t.Fatal("missing document id delete should fail")
	}

	noKB := NewProvider(&mockClient{}, "")
	if err := noKB.DeleteDocument(ctx, "doc-1"); err == nil {
		t.Fatal("missing knowledge id delete should fail")
	}

	errProvider := NewProvider(&mockClient{deleteErr: errors.New("boom")}, "kb-1")
	if err := errProvider.DeleteDocument(ctx, "doc-1"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("delete error should propagate, got %v", err)
	}
}

func TestProviderSearchErrorBranches(t *testing.T) {
	ctx := context.Background()

	nilProvider := NewProvider(nil, "kb-1")
	if _, err := nilProvider.Search(ctx, knowledgeprovider.SearchRequest{}); err == nil {
		t.Fatal("nil client search should fail")
	}

	errProvider := NewProvider(&mockClient{searchErr: errors.New("boom")}, "kb-1")
	if _, err := errProvider.Search(ctx, knowledgeprovider.SearchRequest{}); err == nil {
		t.Fatal("search error should propagate")
	}

	failProvider := NewProvider(&mockClient{searchResp: &base.SearchResponse{Success: false, Message: "not found"}}, "kb-1")
	_, err := failProvider.Search(ctx, knowledgeprovider.SearchRequest{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unsuccessful search = %v", err)
	}
}

func TestProviderUpsertErrorBranches(t *testing.T) {
	ctx := context.Background()

	nilProvider := NewProvider(nil, "kb-1")
	if _, err := nilProvider.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{}); err == nil {
		t.Fatal("nil client upsert should fail")
	}

	noKB := NewProvider(&mockClient{}, "")
	if _, err := noKB.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{}); err == nil {
		t.Fatal("missing knowledge id upsert should fail")
	}

	uploadErr := NewProvider(&mockClient{uploadErr: errors.New("upload failed")}, "kb-1")
	if _, err := uploadErr.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{}); err == nil {
		t.Fatal("upload error should propagate")
	}
}

func TestProviderRebuildIndex(t *testing.T) {
	ctx := context.Background()
	if err := NewProvider(nil, "kb-1").RebuildIndex(ctx, knowledgeprovider.RebuildRequest{}); err == nil {
		t.Fatal("nil client rebuild should fail")
	}
	if err := NewProvider(&mockClient{}, "kb-1").RebuildIndex(ctx, knowledgeprovider.RebuildRequest{}); err != nil {
		t.Fatalf("rebuild should be a no-op, got %v", err)
	}
}

func TestProviderHealthCheckNilClient(t *testing.T) {
	if err := NewProvider(nil, "kb-1").HealthCheck(context.Background()); err == nil {
		t.Fatal("nil client health check should fail")
	}
}
