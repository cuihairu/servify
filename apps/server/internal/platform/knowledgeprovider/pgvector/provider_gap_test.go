package pgvector

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"

	"servify/apps/server/internal/platform/knowledgeprovider"
)

// TestNormalizeWhitespaceStripsNewlines 固化 normalizeWhitespace 的语义:
// 它把换行折叠成空格。ChunkByParagraph 必须先按换行分段、再在段内调用它,
// 否则多段落会被合并成单段(见 chunking.go ChunkByParagraph 注释)。
func TestNormalizeWhitespaceStripsNewlines(t *testing.T) {
	normalized := normalizeWhitespace("line one\n\nline two\n\tline three")
	if strings.Contains(normalized, "\n") {
		t.Fatalf("normalized text still contains newlines: %q", normalized)
	}
	if paragraphs := strings.Split(normalized, "\n"); len(paragraphs) != 1 {
		t.Fatalf("expected single paragraph after normalize, got %d", len(paragraphs))
	}
}

// TestChunkerOverlapLargerThanChunkSize 覆盖滑动窗口中
// nextStart < 0 与 nextStart <= start 两个防回退分支。
func TestChunkerOverlapLargerThanChunkSize(t *testing.T) {
	c := &Chunker{ChunkSize: 10, ChunkOverlap: 100}

	chunks := c.Chunk(strings.Repeat("a", 25))
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	joined := strings.Join(chunks, "")
	// 无标点文本的窗口按 chunkEnd 前进,无字符丢失
	if !strings.Contains(joined, strings.Repeat("a", 10)) {
		t.Fatalf("unexpected chunks: %v", chunks)
	}

	// chunkLongParagraph 走同样的回退分支
	para := c.chunkLongParagraph(strings.Repeat("b", 25))
	if len(para) == 0 {
		t.Fatal("expected chunks from long paragraph")
	}
}

func TestDistanceFunctionsEmptyVectors(t *testing.T) {
	if got := CosineDistance(nil, nil); got != 0 {
		t.Fatalf("CosineDistance(nil,nil) = %v, want 0", got)
	}
	if got := EuclideanDistance([]float32{}, []float32{}); got != 0 {
		t.Fatalf("EuclideanDistance(empty,empty) = %v, want 0", got)
	}
	if got := DotProduct(nil, nil); got != 0 {
		t.Fatalf("DotProduct(nil,nil) = %v, want 0", got)
	}
	if got := L2Norm(nil); got != 0 {
		t.Fatalf("L2Norm(nil) = %v, want 0", got)
	}
}

// TestProvider_UpsertDocumentWhitespaceID 覆盖 externalID 为空但
// doc.ID 非空(仅空白)时的按 id 删除分支。
func TestProvider_UpsertDocumentWhitespaceID(t *testing.T) {
	db := setupTestDB(t)
	p := NewProvider(db, &mockEmbeddingProvider{
		dimension: 3,
		vectors:   [][]float32{{0.1, 0.2, 0.3}},
	}, Config{})

	id, err := p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID:      "  ", // TrimSpace 后为空,externalID 为空,走 else-if doc.ID 分支
		Title:   "Whitespace ID Doc",
		Content: "content",
	})
	if err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty generated doc id")
	}

	var count int64
	if err := db.Table("knowledge_docs").Where("title = ?", "Whitespace ID Doc").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 stored chunk, got %d", count)
	}
}

// fakeConnPool 是不实现 GetDBConnector 的最小连接池,让 gorm 的 DB()
// 返回 ErrInvalidDB,从而覆盖 HealthCheck 的连接获取失败分支。
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

// TestProvider_HealthCheckConnPoolNotSQLDB 覆盖 db.DB() 失败分支:
// ConnPool 不是 *sql.DB 时 gorm 返回 ErrInvalidDB。
func TestProvider_HealthCheckConnPoolNotSQLDB(t *testing.T) {
	p := &Provider{
		db:        &gorm.DB{Config: &gorm.Config{ConnPool: fakeConnPool{}}},
		embedding: &mockEmbeddingProvider{dimension: 3},
	}
	err := p.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error when ConnPool is not a *sql.DB")
	}
	if !strings.Contains(err.Error(), "failed to get database connection") {
		t.Fatalf("error = %v, want connection failure", err)
	}
}
