package dify

import (
	"context"
	"testing"

	"servify/apps/server/internal/platform/knowledgeprovider"
	base "servify/apps/server/pkg/dify"
)

type mockClient struct {
	retrieveResp *base.RetrieveResponse
	retrieveErr  error
	createErr    error
	deleteErr    error
	healthErr    error
}

func (m *mockClient) GetDataset(ctx context.Context, datasetID string) (*base.Dataset, error) {
	return &base.Dataset{ID: datasetID, Name: "Test"}, m.healthErr
}

func (m *mockClient) Retrieve(ctx context.Context, datasetID string, req *base.RetrieveRequest) (*base.RetrieveResponse, error) {
	return m.retrieveResp, m.retrieveErr
}

func (m *mockClient) CreateDocumentFromText(ctx context.Context, datasetID string, req *base.CreateDocumentRequest) (*base.Document, error) {
	return &base.Document{ID: "doc-1", Name: req.Name}, m.createErr
}

func (m *mockClient) DeleteDocument(ctx context.Context, datasetID, documentID string) error {
	return m.deleteErr
}

func (m *mockClient) HealthCheck(ctx context.Context, datasetID string) error {
	return m.healthErr
}

func TestProviderSearch(t *testing.T) {
	provider := NewProvider(&mockClient{
		retrieveResp: &base.RetrieveResponse{
			Records: []base.RetrieveRecord{
				{DocumentID: "doc-1", Title: "Refund", Content: "7 day refund", Score: 0.92},
			},
		},
	}, "dataset-1", SearchConfig{TopK: 5, ScoreThreshold: 0.7, SearchMethod: "semantic_search"})

	hits, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "refund"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d", len(hits))
	}
	if hits[0].Source != "dify" {
		t.Fatalf("source = %q", hits[0].Source)
	}
}

func TestProviderUpsertDocumentReturnsExternalID(t *testing.T) {
	provider := NewProvider(&mockClient{}, "dataset-1", SearchConfig{})
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

func TestProviderHealthCheck(t *testing.T) {
	provider := NewProvider(&mockClient{}, "dataset-1", SearchConfig{})
	if err := provider.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}

func TestProviderDeleteDocument(t *testing.T) {
	provider := NewProvider(&mockClient{}, "dataset-1", SearchConfig{})
	err := provider.DeleteDocument(context.Background(), "doc-1")
	if err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}
}
