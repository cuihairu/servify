package local

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	localllm "servify/apps/server/internal/platform/embedding/local"
	"servify/apps/server/internal/platform/knowledgeprovider"
)

// closedDB 打开一个已关闭的 sqlite 句柄（触发查询/写入/健康检查的失败分支）。
func closedDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
	return db
}

// TestSearchCandidateLoadError 候选分块加载失败显式报错（closed DB）。
func TestSearchCandidateLoadError(t *testing.T) {
	p := NewProvider(closedDB(t), localllm.NewProvider(localllm.Config{}), Config{})
	_, err := p.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q"})
	if err == nil || !strings.Contains(err.Error(), "failed to load candidate chunks") {
		t.Fatalf("search on closed db = %v, want candidate load error", err)
	}
}

// TestUpsertEmptyContent 空内容落到单块原文兜底（Chunker 空结果时）。
func TestUpsertEmptyContent(t *testing.T) {
	db := openDB(t)
	p := NewProvider(db, localllm.NewProvider(localllm.Config{}), Config{})
	externalID, err := p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID: "doc-empty", TenantID: "t",
	})
	if err != nil || externalID != "doc-empty" {
		t.Fatalf("upsert empty content: %v, %v", externalID, err)
	}
	var count int64
	if err := db.Model(&models.KnowledgeDoc{}).Where("external_id = ?", "doc-empty").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("chunks = %d, want 1", count)
	}
}

// TestUpsertNoID 无外部 id 且无内部 id 的文档：分块命名退化为 chunk-N、
// 返回首个分块主键、Metadata category 落库；替换删除按空 externalID 短路，
// 不误清其他无号文档。
func TestUpsertNoID(t *testing.T) {
	db := openDB(t)
	p := NewProvider(db, localllm.NewProvider(localllm.Config{}), Config{})
	first, err := p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		Content:  "没有编号的文档内容",
		Metadata: map[string]interface{}{"category": "ops"},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if first == "" {
		t.Fatalf("first chunk id empty")
	}
	var row models.KnowledgeDoc
	if err := db.First(&row, "doc_chunk_id = ?", "chunk-0").Error; err != nil {
		t.Fatalf("chunk-0 naming lookup: %v", err)
	}
	if row.Category != "ops" {
		t.Fatalf("metadata category = %q, want ops", row.Category)
	}
	if row.ExternalID != "" {
		t.Fatalf("no-id doc external_id = %q, want empty", row.ExternalID)
	}
	// 再写一条无号文档：此前那篇 external_id 为空，不能被误清。
	if _, err := p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		Content: "第二条无编号文档",
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	var count int64
	if err := db.Model(&models.KnowledgeDoc{}).Count(&count).Error; err != nil {
		t.Fatalf("count all: %v", err)
	}
	if count != 2 {
		t.Fatalf("rows = %d, want 2 (no cross-delete)", count)
	}
	// 删除空 id → 无操作。
	if err := p.DeleteDocument(context.Background(), "  "); err != nil {
		t.Fatalf("delete blank id = %v, want nil", err)
	}
}

// TestUpsertDeleteErrors upsert 的替换删除失败显式报错（closed DB）。
func TestUpsertDeleteErrors(t *testing.T) {
	p := NewProvider(closedDB(t), &failingEmbedding{}, Config{})
	if _, err := p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID: "doc-e", Content: "内容",
	}); err == nil || !strings.Contains(err.Error(), "failed to delete existing document") {
		t.Fatalf("upsert on closed db = %v, want delete error", err)
	}
	if err := p.DeleteDocument(context.Background(), "doc-e"); err == nil || !strings.Contains(err.Error(), "failed to delete existing document") {
		t.Fatalf("delete on closed db = %v, want delete error", err)
	}
}

// TestUpsertCreateError 分块写入失败显式报错：BEFORE INSERT 触发器让
// 替换删除成功而新块写入失败，覆盖 create 分支。
func TestUpsertCreateError(t *testing.T) {
	db := openDB(t)
	if err := db.Exec(`CREATE TRIGGER block_insert BEFORE INSERT ON knowledge_docs
		BEGIN SELECT RAISE(ABORT, 'insert blocked'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	p := NewProvider(db, localllm.NewProvider(localllm.Config{}), Config{})
	_, err := p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID: "doc-c", Content: "触发器拦截的写入",
	})
	if err == nil || !strings.Contains(err.Error(), "failed to create chunk 0") {
		t.Fatalf("upsert with blocked insert = %v, want create chunk error", err)
	}
}

// fakeConnPool 是不实现 GetDBConnector 的最小连接池（pgvector 测试同款），
// 让 gorm 的 DB() 返回 ErrInvalidDB。
type fakeConnPool struct{}

func (fakeConnPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("not implemented")
}

func (fakeConnPool) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	return nil, errors.New("not implemented")
}

func (fakeConnPool) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, errors.New("not implemented")
}

func (fakeConnPool) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	return nil
}

// TestHealthCheckConnPoolNotSQLDB 覆盖 db.DB() 失败分支：ConnPool 不是
// *sql.DB 时 gorm 返回 ErrInvalidDB。
func TestHealthCheckConnPoolNotSQLDB(t *testing.T) {
	p := &Provider{
		db:        &gorm.DB{Config: &gorm.Config{ConnPool: fakeConnPool{}}},
		embedding: localllm.NewProvider(localllm.Config{}),
	}
	err := p.HealthCheck(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed to get database connection") {
		t.Fatalf("health = %v, want connection failure", err)
	}
}
