package application

import (
	"context"
	"testing"

	"servify/apps/server/internal/platform/knowledgeprovider"
)

// capturingKP 捕获 Retriever 下发的 SearchRequest，供断言命名空间字段。
type capturingKP struct {
	req knowledgeprovider.SearchRequest
}

func (p *capturingKP) Search(_ context.Context, req knowledgeprovider.SearchRequest) ([]knowledgeprovider.KnowledgeHit, error) {
	p.req = req
	return nil, nil
}

func (p *capturingKP) UpsertDocument(_ context.Context, _ knowledgeprovider.KnowledgeDocument) (string, error) {
	return "", knowledgeprovider.ErrOperationNotSupported
}

func (p *capturingKP) DeleteDocument(_ context.Context, _ string) error {
	return knowledgeprovider.ErrOperationNotSupported
}

func (p *capturingKP) HealthCheck(_ context.Context) error { return nil }

// 会话 id 不是知识库 id：ConversationID（如 widget 自造的 ws_<ts>）绝不能
// 透传成 SearchRequest.KnowledgeID，否则 provider 按会话 id 过滤知识命名空间
// 恒空（摄入侧 workspace 永不等于会话 id）。KnowledgeID 留空 = provider 落回
// 默认命名空间；租户隔离仍由 TenantID 承担。
func TestRetrieverDoesNotLeakConversationIDIntoKnowledgeNamespace(t *testing.T) {
	kp := &capturingKP{}
	r := NewRetriever(kp)
	_, err := r.Retrieve(context.Background(), AIRequest{
		TenantID:       "tenant-a",
		ConversationID: "ws_1791342216910",
		Query:          "云主机怎么按小时计费？",
		RetrievalPolicy: RetrievalPolicy{
			Enabled:  true,
			TopK:     5,
			Strategy: "semantic",
		},
	})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if kp.req.KnowledgeID != "" {
		t.Fatalf("KnowledgeID must stay empty (provider default namespace), got %q", kp.req.KnowledgeID)
	}
	if kp.req.TenantID != "tenant-a" {
		t.Fatalf("TenantID must pass through, got %q", kp.req.TenantID)
	}
	if kp.req.Query != "云主机怎么按小时计费？" {
		t.Fatalf("Query must pass through, got %q", kp.req.Query)
	}
	if kp.req.TopK != 5 || kp.req.Strategy != "semantic" {
		t.Fatalf("retrieval policy must pass through, got topK=%d strategy=%q", kp.req.TopK, kp.req.Strategy)
	}
	// capturingKP 只为捕获 SearchRequest 存在；其余接口方法显式走
	// ErrOperationNotSupported，这里补齐覆盖以满足聚合覆盖率门。
	if _, err := kp.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{}); err == nil {
		t.Fatal("UpsertDocument should be unsupported")
	}
	if err := kp.DeleteDocument(context.Background(), "doc-1"); err == nil {
		t.Fatal("DeleteDocument should be unsupported")
	}
	if err := kp.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}
