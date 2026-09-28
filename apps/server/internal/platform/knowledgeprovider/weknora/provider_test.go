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

// recordingClient 记录 UpsertDocument 的删旧/上传调用顺序与参数。
type recordingClient struct {
	mockClient
	calls []string
}

func (m *recordingClient) UploadDocument(ctx context.Context, kbID string, doc *base.Document) (*base.DocumentInfo, error) {
	m.calls = append(m.calls, "upload:"+kbID)
	return &base.DocumentInfo{ID: "doc-new", Title: doc.Title, ProcessedAt: time.Now()}, m.uploadErr
}

func (m *recordingClient) DeleteDocument(ctx context.Context, kbID, docID string) error {
	m.calls = append(m.calls, "delete:"+kbID+"/"+docID)
	return m.deleteErr
}

// TestProviderUpsertDocumentDeletesStaleExternalFirst P1-1 一致性收口：更新
// 场景（ExternalID 非空）必须先按本地映射删旧外部文档再上传，防止 WeKnora
// 纯 Upload 语义残留旧版本被检索命中；删除目标 kb 与上传一致、id 去空格。
func TestProviderUpsertDocumentDeletesStaleExternalFirst(t *testing.T) {
	client := &recordingClient{}
	provider := NewProvider(client, "kb-default")

	id, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		TenantID:    "tenant-a",
		KnowledgeID: "kb-doc",
		ExternalID:  " old-1 ",
		Title:       "Billing",
		Content:     "Billing content",
	})
	if err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if id != "doc-new" {
		t.Fatalf("external id = %q", id)
	}
	want := []string{"delete:kb-doc/old-1", "upload:kb-doc"}
	if len(client.calls) != 2 || client.calls[0] != want[0] || client.calls[1] != want[1] {
		t.Fatalf("calls = %v, want %v (delete before upload)", client.calls, want)
	}
}

// TestProviderUpsertDocumentAbortsWhenStaleDeleteFails 删旧失败即中断且不上传
// （与 ragflow/dify 同语义：管理端可重试收敛，不留下新旧并存）。
func TestProviderUpsertDocumentAbortsWhenStaleDeleteFails(t *testing.T) {
	client := &recordingClient{mockClient: mockClient{deleteErr: errors.New("delete boom")}}
	provider := NewProvider(client, "kb-1")

	_, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ExternalID: "old-1",
		Title:      "Billing",
		Content:    "Billing content",
	})
	if err == nil || err.Error() != "delete stale weknora document old-1: delete boom" {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if len(client.calls) != 1 || client.calls[0] != "delete:kb-1/old-1" {
		t.Fatalf("calls = %v, want delete only (no upload after failure)", client.calls)
	}
}

// TestProviderUpsertDocumentSkipsDeleteWithoutExternalID 首次建档（无旧映射）
// 不触发删旧，直接上传。
func TestProviderUpsertDocumentSkipsDeleteWithoutExternalID(t *testing.T) {
	client := &recordingClient{}
	provider := NewProvider(client, "kb-1")

	if _, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		Title:   "Billing",
		Content: "Billing content",
	}); err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if len(client.calls) != 1 || client.calls[0] != "upload:kb-1" {
		t.Fatalf("calls = %v, want upload only", client.calls)
	}
}

func TestProviderName(t *testing.T) {
	var named knowledgeprovider.NamedProvider = NewProvider(nil, "kb-1")
	if named.ProviderName() != "weknora" {
		t.Fatalf("ProviderName() = %q", named.ProviderName())
	}
}
