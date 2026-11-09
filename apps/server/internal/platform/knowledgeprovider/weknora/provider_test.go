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
	if err := provider.DeleteDocument(context.Background(), knowledgeprovider.KnowledgeDocument{ExternalID: "doc-1"}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// TestProviderDeleteDocumentScopedToDocumentNamespace 删除作用域贯通：文档
// 挂在哪个知识库（doc.KnowledgeID）就从哪个库删，默认库配置不掺和跨库目标；
// 无 doc.KnowledgeID 时回落 provider 默认库。
func TestProviderDeleteDocumentScopedToDocumentNamespace(t *testing.T) {
	client := &recordingClient{}
	provider := NewProvider(client, "kb-default")

	// 挂在 kb-a 的文档：删除目标是 kb-a，不是默认库。
	if err := provider.DeleteDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		TenantID:    "tenant-a",
		KnowledgeID: "kb-a",
		ExternalID:  "doc-a",
	}); err != nil {
		t.Fatalf("delete kb-a document: %v", err)
	}
	if len(client.calls) != 1 || client.calls[0] != "delete:kb-a/doc-a" {
		t.Fatalf("calls = %v, want delete scoped to the document namespace", client.calls)
	}

	// 挂在 kb-b 的文档：只删 kb-b，kb-a/默认库不受影响。
	if err := provider.DeleteDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		TenantID:    "tenant-a",
		KnowledgeID: "kb-b",
		ExternalID:  "doc-b",
	}); err != nil {
		t.Fatalf("delete kb-b document: %v", err)
	}
	if len(client.calls) != 2 || client.calls[1] != "delete:kb-b/doc-b" {
		t.Fatalf("calls = %v, want second delete scoped to kb-b", client.calls)
	}

	// 无 doc namespace：回落 provider 默认库（与 UpsertDocument 同映射规则）。
	if err := provider.DeleteDocument(context.Background(), knowledgeprovider.KnowledgeDocument{ExternalID: " doc-c "}); err != nil {
		t.Fatalf("delete with default namespace: %v", err)
	}
	if len(client.calls) != 3 || client.calls[2] != "delete:kb-default/doc-c" {
		t.Fatalf("calls = %v, want fallback to provider default kb with trimmed id", client.calls)
	}
}

func TestProviderDeleteDocumentErrorBranches(t *testing.T) {
	ctx := context.Background()

	nilProvider := NewProvider(nil, "kb-1")
	if err := nilProvider.DeleteDocument(ctx, knowledgeprovider.KnowledgeDocument{ExternalID: "doc-1"}); err == nil {
		t.Fatal("nil client delete should fail")
	}

	noDoc := NewProvider(&mockClient{}, "kb-1")
	if err := noDoc.DeleteDocument(ctx, knowledgeprovider.KnowledgeDocument{ExternalID: " "}); err == nil {
		t.Fatal("missing document id delete should fail")
	}

	noKB := NewProvider(&mockClient{}, "")
	if err := noKB.DeleteDocument(ctx, knowledgeprovider.KnowledgeDocument{ExternalID: "doc-1"}); err == nil {
		t.Fatal("unresolvable knowledge base delete should fail")
	}

	errProvider := NewProvider(&mockClient{deleteErr: errors.New("boom")}, "kb-1")
	if err := errProvider.DeleteDocument(ctx, knowledgeprovider.KnowledgeDocument{ExternalID: "doc-1"}); err == nil || !strings.Contains(err.Error(), "boom") {
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

// TestProviderNotRebuildable WeKnora 远端无重建端点（WeKnoraInterface 无对应
// 方法），Provider 不得实现 RebuildableProvider 伪造成功记录——重建能力的
// 缺失由类型断言显式表达（与 ragflow/dify/local/pgvector 同口径）。
func TestProviderNotRebuildable(t *testing.T) {
	var prov knowledgeprovider.KnowledgeProvider = NewProvider(&mockClient{}, "kb-1")
	if _, ok := prov.(knowledgeprovider.RebuildableProvider); ok {
		t.Fatal("weknora remote has no rebuild endpoint; provider must not fake rebuild success")
	}
}

func TestProviderHealthCheckNilClient(t *testing.T) {
	if err := NewProvider(nil, "kb-1").HealthCheck(context.Background()); err == nil {
		t.Fatal("nil client health check should fail")
	}
}

// recordingClient 记录 UpsertDocument 的删旧/上传调用顺序与参数；deleteFn
// 可按文档 id 定制删除结果（nil 时回落 deleteErr）。
type recordingClient struct {
	mockClient
	calls    []string
	deleteFn func(docID string) error
}

func (m *recordingClient) UploadDocument(ctx context.Context, kbID string, doc *base.Document) (*base.DocumentInfo, error) {
	m.calls = append(m.calls, "upload:"+kbID)
	return &base.DocumentInfo{ID: "doc-new", Title: doc.Title, ProcessedAt: time.Now()}, m.uploadErr
}

func (m *recordingClient) DeleteDocument(ctx context.Context, kbID, docID string) error {
	m.calls = append(m.calls, "delete:"+kbID+"/"+docID)
	if m.deleteFn != nil {
		return m.deleteFn(docID)
	}
	return m.deleteErr
}

// TestProviderUpsertDocumentUploadsBeforeStaleDelete 文档更新容错：先上传新
// 版本再删旧外部文档（先新建后切换）——上传失败时旧内容仍在远端可用，不因
// 先删后传失败造成内容不可用；删除目标 kb 与上传一致、id 去空格。
func TestProviderUpsertDocumentUploadsBeforeStaleDelete(t *testing.T) {
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
	want := []string{"upload:kb-doc", "delete:kb-doc/old-1"}
	if len(client.calls) != 2 || client.calls[0] != want[0] || client.calls[1] != want[1] {
		t.Fatalf("calls = %v, want %v (upload before delete)", client.calls, want)
	}
}

// TestProviderUpsertDocumentUploadFailureKeepsOldDocument 上传失败即中断，
// 旧文档未被动过（删除在上传成功之后才发生），重试可收敛。
func TestProviderUpsertDocumentUploadFailureKeepsOldDocument(t *testing.T) {
	client := &recordingClient{mockClient: mockClient{uploadErr: errors.New("upload boom")}}
	provider := NewProvider(client, "kb-1")

	_, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ExternalID: "old-1",
		Title:      "Billing",
		Content:    "Billing content",
	})
	if err == nil || !strings.Contains(err.Error(), "upload boom") {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	want := []string{"upload:kb-1"}
	if len(client.calls) != 1 || client.calls[0] != want[0] {
		t.Fatalf("calls = %v, want %v (stale delete must not run after upload failure)", client.calls, want)
	}
}

// TestProviderUpsertDocumentRollsBackNewWhenStaleDeleteFails 删旧失败时回滚
// 刚上传的新文档并返回错误（本地映射未动，旧内容仍可用）：远端收敛为「旧
// 文档原样、无孤儿」，重试重新走完整先传后删路径。
func TestProviderUpsertDocumentRollsBackNewWhenStaleDeleteFails(t *testing.T) {
	client := &recordingClient{}
	client.deleteFn = func(docID string) error {
		if docID == "old-1" {
			return errors.New("delete boom")
		}
		return nil // 回滚删除新文档成功
	}
	provider := NewProvider(client, "kb-1")

	_, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ExternalID: "old-1",
		Title:      "Billing",
		Content:    "Billing content",
	})
	if err == nil || !strings.Contains(err.Error(), "delete boom") {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	want := []string{"upload:kb-1", "delete:kb-1/old-1", "delete:kb-1/doc-new"}
	if len(client.calls) != 3 || client.calls[0] != want[0] || client.calls[1] != want[1] || client.calls[2] != want[2] {
		t.Fatalf("calls = %v, want %v (rollback the just-uploaded document)", client.calls, want)
	}
}

// TestProviderUpsertDocumentTracesOrphanWhenRollbackAlsoFails 删旧与回滚双失败：
// 新文档 id 必须随错误透出可追踪（旧/新版本可追踪），错误如实上抛。
func TestProviderUpsertDocumentTracesOrphanWhenRollbackAlsoFails(t *testing.T) {
	client := &recordingClient{mockClient: mockClient{deleteErr: errors.New("delete boom")}}
	provider := NewProvider(client, "kb-1")

	_, err := provider.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ExternalID: "old-1",
		Title:      "Billing",
		Content:    "Billing content",
	})
	if err == nil {
		t.Fatal("expected double failure error")
	}
	if !strings.Contains(err.Error(), "old-1") || !strings.Contains(err.Error(), "doc-new") {
		t.Fatalf("error must trace stale and rolled-back document ids, got %v", err)
	}
	want := []string{"upload:kb-1", "delete:kb-1/old-1", "delete:kb-1/doc-new"}
	if len(client.calls) != 3 || client.calls[2] != want[2] {
		t.Fatalf("calls = %v, want %v (rollback attempted)", client.calls, want)
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
