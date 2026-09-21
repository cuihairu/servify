package ragflow

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"servify/apps/server/internal/platform/knowledgeprovider"
	base "servify/apps/server/pkg/ragflow"
)

type capturingClient struct {
	retrieveErr error
	listDocs    []base.Document
	listErr     error
	uploadDoc   *base.Document
	uploadErr   error
	deleteErr   error
	parseErr    error
	healthErr   error

	gotRetrieveDatasetIDs []string
	gotRetrieve           *base.RetrieveRequest
	gotListDatasetID      string
	gotListName           string
	gotDeleteDatasetID    string
	gotDeleteIDs          []string
	gotUploadDatasetID    string
	gotUploadName         string
	gotUploadContent      string
	gotParseDatasetID     string
	gotParseIDs           []string
	gotHealthDatasetID    string
}

func (m *capturingClient) GetDataset(ctx context.Context, datasetID string) (*base.Dataset, error) {
	return &base.Dataset{ID: datasetID, Name: "Test"}, m.healthErr
}

func (m *capturingClient) Retrieve(ctx context.Context, req *base.RetrieveRequest) (*base.RetrieveResponse, error) {
	cp := *req
	m.gotRetrieve = &cp
	m.gotRetrieveDatasetIDs = req.DatasetIDs
	return &base.RetrieveResponse{}, m.retrieveErr
}

func (m *capturingClient) ListDocuments(ctx context.Context, datasetID, name string) ([]base.Document, error) {
	m.gotListDatasetID = datasetID
	m.gotListName = name
	return m.listDocs, m.listErr
}

func (m *capturingClient) UploadDocument(ctx context.Context, datasetID, name, content string) (*base.Document, error) {
	m.gotUploadDatasetID = datasetID
	m.gotUploadName = name
	m.gotUploadContent = content
	return m.uploadDoc, m.uploadErr
}

func (m *capturingClient) ParseDocuments(ctx context.Context, datasetID string, documentIDs []string) error {
	m.gotParseDatasetID = datasetID
	m.gotParseIDs = documentIDs
	return m.parseErr
}

func (m *capturingClient) DeleteDocuments(ctx context.Context, datasetID string, documentIDs []string) error {
	m.gotDeleteDatasetID = datasetID
	m.gotDeleteIDs = documentIDs
	return m.deleteErr
}

func (m *capturingClient) HealthCheck(ctx context.Context, datasetID string) error {
	m.gotHealthDatasetID = datasetID
	return m.healthErr
}

func TestProviderNilClient(t *testing.T) {
	provider := NewProvider(nil, "", SearchConfig{})

	if _, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q"}); err == nil || err.Error() != "ragflow client is not configured" {
		t.Fatalf("Search() nil client error = %v", err)
	}
	if _, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{}); err == nil || err.Error() != "ragflow client is not configured" {
		t.Fatalf("UpsertDocument() nil client error = %v", err)
	}
	if err := provider.DeleteDocument(context.Background(), "doc"); err == nil || err.Error() != "ragflow client is not configured" {
		t.Fatalf("DeleteDocument() nil client error = %v", err)
	}
	if err := provider.HealthCheck(context.Background()); err == nil || err.Error() != "ragflow client is not configured" {
		t.Fatalf("HealthCheck() nil client error = %v", err)
	}
}

func TestProviderMissingDatasetID(t *testing.T) {
	provider := NewProvider(&mockClient{}, "", SearchConfig{})

	if _, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q"}); err == nil || err.Error() != "ragflow dataset id is not configured" {
		t.Fatalf("Search() missing dataset error = %v", err)
	}
	if _, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{Title: "t", Content: "c"}); err == nil || err.Error() != "ragflow dataset id is not configured" {
		t.Fatalf("UpsertDocument() missing dataset error = %v", err)
	}
	if err := provider.DeleteDocument(context.Background(), "doc-1"); err == nil || err.Error() != "ragflow dataset id is not configured" {
		t.Fatalf("DeleteDocument() missing dataset error = %v", err)
	}
	if err := provider.HealthCheck(context.Background()); err == nil || err.Error() != "ragflow dataset id is not configured" {
		t.Fatalf("HealthCheck() missing dataset error = %v", err)
	}
}

func TestProviderSearchTuningDefaultsAndOverrides(t *testing.T) {
	client := &capturingClient{}
	provider := NewProvider(client, "ds-default", SearchConfig{})

	// 零值构造：TopK/阈值取硬编码兜底。
	if _, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q"}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if client.gotRetrieveDatasetIDs[0] != "ds-default" {
		t.Fatalf("dataset ids = %v", client.gotRetrieveDatasetIDs)
	}
	if client.gotRetrieve.TopK != 10 || client.gotRetrieve.PageSize != 10 {
		t.Fatalf("topK defaults = %d/%d", client.gotRetrieve.TopK, client.gotRetrieve.PageSize)
	}
	if client.gotRetrieve.SimilarityThreshold != 0.2 {
		t.Fatalf("threshold default = %v", client.gotRetrieve.SimilarityThreshold)
	}

	// provider 配置作为中间层默认。
	client = &capturingClient{}
	provider = NewProvider(client, "ds-default", SearchConfig{TopK: 3, ScoreThreshold: 0.6})
	if _, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q"}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if client.gotRetrieve.TopK != 3 || client.gotRetrieve.SimilarityThreshold != 0.6 {
		t.Fatalf("search config tuning = %+v", client.gotRetrieve)
	}

	// 请求级 TopK/Threshold 与 KnowledgeID 覆盖一切。
	client = &capturingClient{}
	provider = NewProvider(client, "ds-default", SearchConfig{TopK: 3, ScoreThreshold: 0.6})
	if _, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{
		Query:       "q",
		KnowledgeID: "ds-override",
		TopK:        7,
		Threshold:   0.4,
	}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if client.gotRetrieveDatasetIDs[0] != "ds-override" {
		t.Fatalf("dataset ids = %v", client.gotRetrieveDatasetIDs)
	}
	if client.gotRetrieve.TopK != 7 || client.gotRetrieve.PageSize != 7 || client.gotRetrieve.SimilarityThreshold != 0.4 {
		t.Fatalf("request tuning = %+v", client.gotRetrieve)
	}
}

func TestProviderSearchPropagatesError(t *testing.T) {
	provider := NewProvider(&mockClient{retrieveErr: errors.New("retrieve boom")}, "ds-1", SearchConfig{})
	if _, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q"}); err == nil || err.Error() != "retrieve boom" {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestProviderUpsertMissingTitle(t *testing.T) {
	provider := NewProvider(&mockClient{}, "ds-1", SearchConfig{})
	if _, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{Title: "   ", Content: "c"}); err == nil || err.Error() != "ragflow document title is not configured" {
		t.Fatalf("UpsertDocument() blank title error = %v", err)
	}
}

func TestProviderUpsertUsesDocumentKnowledgeID(t *testing.T) {
	client := &capturingClient{uploadDoc: &base.Document{ID: "doc-9"}}
	provider := NewProvider(client, "ds-default", SearchConfig{})
	if _, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		KnowledgeID: "ds-doc",
		Title:       " Refund ",
		Content:     "7 day refund",
	}); err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if client.gotListDatasetID != "ds-doc" || client.gotListName != "Refund.txt" {
		t.Fatalf("list = %q/%q", client.gotListDatasetID, client.gotListName)
	}
	if client.gotUploadDatasetID != "ds-doc" || client.gotUploadName != "Refund" || client.gotUploadContent != "7 day refund" {
		t.Fatalf("upload = %q/%q/%q", client.gotUploadDatasetID, client.gotUploadName, client.gotUploadContent)
	}
}

func TestProviderUpsertDeduplicatesOldDocuments(t *testing.T) {
	client := &capturingClient{
		listDocs:  []base.Document{{ID: "doc-old", Name: "Refund", Run: "DONE"}},
		uploadDoc: &base.Document{ID: "doc-9", Name: "Refund", Run: "UNSTART"},
	}
	provider := NewProvider(client, "ds-1", SearchConfig{})
	id, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		Title:   "Refund",
		Content: "updated body",
	})
	if err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if id != "doc-9" {
		t.Fatalf("external id = %q", id)
	}
	if client.gotDeleteDatasetID != "ds-1" || !reflect.DeepEqual(client.gotDeleteIDs, []string{"doc-old"}) {
		t.Fatalf("delete = %q/%v", client.gotDeleteDatasetID, client.gotDeleteIDs)
	}
	if !reflect.DeepEqual(client.gotParseIDs, []string{"doc-9"}) {
		t.Fatalf("parse ids = %v", client.gotParseIDs)
	}
}

func TestProviderUpsertSkipsDeleteWhenNoExisting(t *testing.T) {
	client := &capturingClient{uploadDoc: &base.Document{ID: "doc-1"}}
	provider := NewProvider(client, "ds-1", SearchConfig{})
	if _, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{Title: "Refund", Content: "c"}); err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if client.gotDeleteIDs != nil {
		t.Fatalf("unexpected delete = %v", client.gotDeleteIDs)
	}
}

func TestProviderUpsertPropagatesErrors(t *testing.T) {
	cases := []struct {
		name   string
		client *capturingClient
		want   string
	}{
		{"list", &capturingClient{listErr: errors.New("list boom")}, "list boom"},
		{"delete", &capturingClient{listDocs: []base.Document{{ID: "doc-old"}}, deleteErr: errors.New("delete boom")}, "delete boom"},
		{"upload", &capturingClient{uploadErr: errors.New("upload boom")}, "upload boom"},
		{"parse", &capturingClient{uploadDoc: &base.Document{ID: "doc-9"}, parseErr: errors.New("parse boom")}, "parse boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := NewProvider(tc.client, "ds-1", SearchConfig{})
			_, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{Title: "Refund", Content: "c"})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("UpsertDocument() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestProviderDeleteDocumentValidationAndErrors(t *testing.T) {
	client := &capturingClient{}
	provider := NewProvider(client, "ds-1", SearchConfig{})

	if err := provider.DeleteDocument(context.Background(), "   "); err == nil || err.Error() != "ragflow document id is not configured" {
		t.Fatalf("DeleteDocument() blank id error = %v", err)
	}
	if err := provider.DeleteDocument(context.Background(), "  doc-9  "); err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}
	if client.gotDeleteDatasetID != "ds-1" || !reflect.DeepEqual(client.gotDeleteIDs, []string{"doc-9"}) {
		t.Fatalf("delete = %q/%v", client.gotDeleteDatasetID, client.gotDeleteIDs)
	}

	failing := NewProvider(&mockClient{deleteErr: errors.New("delete boom")}, "ds-1", SearchConfig{})
	if err := failing.DeleteDocument(context.Background(), "doc-1"); err == nil || err.Error() != "delete boom" {
		t.Fatalf("DeleteDocument() propagated error = %v", err)
	}
}

func TestProviderHealthCheckPropagatesError(t *testing.T) {
	client := &capturingClient{healthErr: errors.New("unhealthy")}
	provider := NewProvider(client, "ds-1", SearchConfig{})
	if err := provider.HealthCheck(context.Background()); err == nil || err.Error() != "unhealthy" {
		t.Fatalf("HealthCheck() error = %v", err)
	}
	if client.gotHealthDatasetID != "ds-1" {
		t.Fatalf("health dataset id = %q", client.gotHealthDatasetID)
	}
}
