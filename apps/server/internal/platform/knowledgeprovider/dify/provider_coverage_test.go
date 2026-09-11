package dify

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/platform/knowledgeprovider"
	base "servify/apps/server/pkg/dify"
)

func TestProviderNilClient(t *testing.T) {
	provider := NewProvider(nil, "", SearchConfig{})

	if _, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q"}); err == nil || err.Error() != "dify client is not configured" {
		t.Fatalf("Search() nil client error = %v", err)
	}
	if _, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{}); err == nil || err.Error() != "dify client is not configured" {
		t.Fatalf("UpsertDocument() nil client error = %v", err)
	}
	if err := provider.DeleteDocument(context.Background(), "doc"); err == nil || err.Error() != "dify client is not configured" {
		t.Fatalf("DeleteDocument() nil client error = %v", err)
	}
	if err := provider.HealthCheck(context.Background()); err == nil || err.Error() != "dify client is not configured" {
		t.Fatalf("HealthCheck() nil client error = %v", err)
	}
}

func TestProviderMissingDatasetID(t *testing.T) {
	provider := NewProvider(&mockClient{}, "", SearchConfig{})

	if _, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q"}); err == nil || err.Error() != "dify dataset id is not configured" {
		t.Fatalf("Search() missing dataset error = %v", err)
	}
	if _, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{Title: "t", Content: "c"}); err == nil || err.Error() != "dify dataset id is not configured" {
		t.Fatalf("UpsertDocument() missing dataset error = %v", err)
	}
	if err := provider.DeleteDocument(context.Background(), "doc-1"); err == nil || err.Error() != "dify dataset id is not configured" {
		t.Fatalf("DeleteDocument() missing dataset error = %v", err)
	}
	if err := provider.HealthCheck(context.Background()); err == nil || err.Error() != "dify dataset id is not configured" {
		t.Fatalf("HealthCheck() missing dataset error = %v", err)
	}
}

func TestProviderDeleteDocumentValidationAndErrors(t *testing.T) {
	provider := NewProvider(&mockClient{deleteErr: errors.New("delete boom")}, "dataset-1", SearchConfig{})

	if err := provider.DeleteDocument(context.Background(), "   "); err == nil || err.Error() != "dify document id is not configured" {
		t.Fatalf("DeleteDocument() blank id error = %v", err)
	}
	if err := provider.DeleteDocument(context.Background(), " doc-1 "); err == nil || err.Error() != "delete boom" {
		t.Fatalf("DeleteDocument() propagated error = %v", err)
	}
}

func TestProviderDeleteDocumentTrimsID(t *testing.T) {
	client := &deletingMockClient{}
	provider := NewProvider(client, "dataset-1", SearchConfig{})

	if err := provider.DeleteDocument(context.Background(), "  doc-9  "); err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}
	if client.gotDocumentID != "doc-9" {
		t.Fatalf("document id = %q, want trimmed doc-9", client.gotDocumentID)
	}
	if client.gotDatasetID != "dataset-1" {
		t.Fatalf("dataset id = %q", client.gotDatasetID)
	}
}

type deletingMockClient struct {
	mockClient
	gotDatasetID  string
	gotDocumentID string
}

func (m *deletingMockClient) DeleteDocument(ctx context.Context, datasetID, documentID string) error {
	m.gotDatasetID = datasetID
	m.gotDocumentID = documentID
	return nil
}

func TestProviderSearchUsesRequestKnowledgeIDAndDefaults(t *testing.T) {
	client := &capturingRetrieveClient{
		resp: &base.RetrieveResponse{Records: []base.RetrieveRecord{{DocumentID: "d1", Content: "c"}}},
	}
	provider := NewProvider(client, "dataset-default", SearchConfig{
		TopK:            3,
		ScoreThreshold:  0.6,
		SearchMethod:    "",
		RerankingEnable: true,
	})

	hits, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{
		Query:       "refund",
		KnowledgeID: "dataset-override",
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 1 || hits[0].DocumentID != "d1" {
		t.Fatalf("hits = %+v", hits)
	}
	if client.gotDatasetID != "dataset-override" {
		t.Fatalf("dataset id = %q, want dataset-override", client.gotDatasetID)
	}
	if client.gotRequest.RetrievalModel.TopK != 3 {
		t.Fatalf("topK = %d, want 3", client.gotRequest.RetrievalModel.TopK)
	}
	if client.gotRequest.RetrievalModel.ScoreThreshold != 0.6 {
		t.Fatalf("threshold = %v, want 0.6", client.gotRequest.RetrievalModel.ScoreThreshold)
	}
	if client.gotRequest.RetrievalModel.SearchMethod != "semantic_search" {
		t.Fatalf("search method = %q, want semantic_search default", client.gotRequest.RetrievalModel.SearchMethod)
	}
	if !client.gotRequest.RetrievalModel.RerankingEnable {
		t.Fatal("reranking should be inherited from provider config")
	}
}

type capturingRetrieveClient struct {
	mockClient
	gotDatasetID string
	gotRequest   *base.RetrieveRequest
	resp         *base.RetrieveResponse
}

func (m *capturingRetrieveClient) Retrieve(ctx context.Context, datasetID string, req *base.RetrieveRequest) (*base.RetrieveResponse, error) {
	m.gotDatasetID = datasetID
	cp := *req
	m.gotRequest = &cp
	return m.resp, nil
}

func TestProviderSearchPropagatesError(t *testing.T) {
	provider := NewProvider(&mockClient{retrieveErr: errors.New("retrieve boom")}, "dataset-1", SearchConfig{})
	if _, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q"}); err == nil || err.Error() != "retrieve boom" {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestProviderUpsertDocumentUsesDocumentKnowledgeIDAndPropagatesError(t *testing.T) {
	provider := NewProvider(&mockClient{createErr: errors.New("create boom")}, "dataset-default", SearchConfig{})
	if _, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		KnowledgeID: "dataset-doc",
		Title:       "t",
		Content:     "c",
	}); err == nil || err.Error() != "create boom" {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
}

func TestProviderHealthCheckPropagatesError(t *testing.T) {
	provider := NewProvider(&mockClient{healthErr: errors.New("unhealthy")}, "dataset-1", SearchConfig{})
	if err := provider.HealthCheck(context.Background()); err == nil || err.Error() != "unhealthy" {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}
