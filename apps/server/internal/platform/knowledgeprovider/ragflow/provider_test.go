package ragflow

import (
	"context"
	"testing"

	"servify/apps/server/internal/platform/knowledgeprovider"
	base "servify/apps/server/pkg/ragflow"
)

type mockClient struct {
	retrieveResp *base.RetrieveResponse
	retrieveErr  error
	listDocs     []base.Document
	listErr      error
	uploadErr    error
	deleteErr    error
	parseErr     error
	healthErr    error
}

func (m *mockClient) GetDataset(ctx context.Context, datasetID string) (*base.Dataset, error) {
	return &base.Dataset{ID: datasetID, Name: "Test"}, m.healthErr
}

func (m *mockClient) Retrieve(ctx context.Context, req *base.RetrieveRequest) (*base.RetrieveResponse, error) {
	return m.retrieveResp, m.retrieveErr
}

func (m *mockClient) ListDocuments(ctx context.Context, datasetID, name string) ([]base.Document, error) {
	return m.listDocs, m.listErr
}

func (m *mockClient) UploadDocument(ctx context.Context, datasetID, name, content string) (*base.Document, error) {
	return &base.Document{ID: "doc-1", Name: name, Run: "UNSTART"}, m.uploadErr
}

func (m *mockClient) ParseDocuments(ctx context.Context, datasetID string, documentIDs []string) error {
	return m.parseErr
}

func (m *mockClient) DeleteDocuments(ctx context.Context, datasetID string, documentIDs []string) error {
	return m.deleteErr
}

func (m *mockClient) HealthCheck(ctx context.Context, datasetID string) error {
	return m.healthErr
}

func TestProviderSearch(t *testing.T) {
	provider := NewProvider(&mockClient{
		retrieveResp: &base.RetrieveResponse{
			Total: 1,
			Chunks: []base.Chunk{
				{Content: "7 day refund", DocumentID: "doc-1", DocumentKeyword: "refund.txt", DatasetID: "ds-1", Similarity: 0.92},
			},
		},
	}, "ds-1", SearchConfig{TopK: 5, ScoreThreshold: 0.7})

	hits, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "refund"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d", len(hits))
	}
	hit := hits[0]
	if hit.Source != "ragflow" {
		t.Fatalf("source = %q", hit.Source)
	}
	if hit.DocumentID != "doc-1" || hit.Title != "refund.txt" || hit.Score != 0.92 {
		t.Fatalf("hit = %+v", hit)
	}
	if hit.Metadata["dataset_id"] != "ds-1" {
		t.Fatalf("metadata = %+v", hit.Metadata)
	}
}

func TestProviderUpsertDocumentReturnsExternalID(t *testing.T) {
	provider := NewProvider(&mockClient{}, "ds-1", SearchConfig{})
	id, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID:      "doc-1",
		Title:   "Refund",
		Content: "7 day refund",
	})
	if err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if id != "doc-1" {
		t.Fatalf("external id = %q", id)
	}
}

func TestProviderDeleteDocument(t *testing.T) {
	provider := NewProvider(&mockClient{}, "ds-1", SearchConfig{})
	if err := provider.DeleteDocument(context.Background(), "doc-1"); err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}
}

func TestProviderHealthCheck(t *testing.T) {
	provider := NewProvider(&mockClient{}, "ds-1", SearchConfig{})
	if err := provider.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}
