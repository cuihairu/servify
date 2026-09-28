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

// recordingUpsertClient 记录 UpsertDocument 的删旧/建新调用顺序与参数。
type recordingUpsertClient struct {
	mockClient
	calls []string
}

func (m *recordingUpsertClient) CreateDocumentFromText(ctx context.Context, datasetID string, req *base.CreateDocumentRequest) (*base.Document, error) {
	m.calls = append(m.calls, "create:"+datasetID)
	return &base.Document{ID: "doc-new", Name: req.Name}, m.createErr
}

func (m *recordingUpsertClient) DeleteDocument(ctx context.Context, datasetID, documentID string) error {
	m.calls = append(m.calls, "delete:"+datasetID+"/"+documentID)
	return m.deleteErr
}

// TestProviderUpsertDocumentDeletesStaleExternalFirst P1-1 一致性收口：更新
// 场景（ExternalID 非空）必须先按本地映射删旧外部文档再建新，防止 Dify 纯
// Create 语义残留旧版本被检索命中；删除目标 dataset 与建新一致、id 去空格。
func TestProviderUpsertDocumentDeletesStaleExternalFirst(t *testing.T) {
	client := &recordingUpsertClient{}
	provider := NewProvider(client, "dataset-default", SearchConfig{})

	id, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		KnowledgeID: "dataset-doc",
		ExternalID:  " old-1 ",
		Title:       "t",
		Content:     "c",
	})
	if err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if id != "doc-new" {
		t.Fatalf("external id = %q", id)
	}
	want := []string{"delete:dataset-doc/old-1", "create:dataset-doc"}
	if len(client.calls) != 2 || client.calls[0] != want[0] || client.calls[1] != want[1] {
		t.Fatalf("calls = %v, want %v (delete before create)", client.calls, want)
	}
}

// TestProviderUpsertDocumentAbortsWhenStaleDeleteFails 删旧失败即中断且不建新
// （与 ragflow 同语义：管理端可重试收敛，不留下新旧并存）。
func TestProviderUpsertDocumentAbortsWhenStaleDeleteFails(t *testing.T) {
	client := &recordingUpsertClient{mockClient: mockClient{deleteErr: errors.New("delete boom")}}
	provider := NewProvider(client, "dataset-1", SearchConfig{})

	_, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ExternalID: "old-1",
		Title:      "t",
		Content:    "c",
	})
	if err == nil || err.Error() != "delete stale dify document old-1: delete boom" {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if len(client.calls) != 1 || client.calls[0] != "delete:dataset-1/old-1" {
		t.Fatalf("calls = %v, want delete only (no create after failure)", client.calls)
	}
}

// TestProviderUpsertDocumentSkipsDeleteWithoutExternalID 首次建档（无旧映射）
// 不触发删旧，直接建新。
func TestProviderUpsertDocumentSkipsDeleteWithoutExternalID(t *testing.T) {
	client := &recordingUpsertClient{}
	provider := NewProvider(client, "dataset-1", SearchConfig{})

	if _, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		Title:   "t",
		Content: "c",
	}); err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if len(client.calls) != 1 || client.calls[0] != "create:dataset-1" {
		t.Fatalf("calls = %v, want create only", client.calls)
	}
}

func TestProviderName(t *testing.T) {
	var named knowledgeprovider.NamedProvider = NewProvider(nil, "", SearchConfig{})
	if named.ProviderName() != "dify" {
		t.Fatalf("ProviderName() = %q", named.ProviderName())
	}
}
