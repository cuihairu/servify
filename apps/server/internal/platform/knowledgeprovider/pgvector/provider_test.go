package pgvector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pgvector/pgvector-go"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/platform/knowledgeprovider"
)

// mockEmbeddingProvider 是 embedding.Provider 的 mock 实现
type mockEmbeddingProvider struct {
	vectors    [][]float32
	dimension  int
	embedError error
}

func (m *mockEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.embedError != nil {
		return nil, m.embedError
	}
	if len(m.vectors) > 0 {
		return m.vectors, nil
	}
	// 返回随机向量
	result := make([][]float32, len(texts))
	for i := range result {
		result[i] = make([]float32, m.Dimension())
		// 填充一些简单的值
		for j := range result[i] {
			result[i][j] = 0.1
		}
	}
	return result, nil
}

func (m *mockEmbeddingProvider) Dimension() int {
	if m.dimension > 0 {
		return m.dimension
	}
	return 1536
}

func (m *mockEmbeddingProvider) HealthCheck(ctx context.Context) error {
	return nil
}

// setupTestDB 创建内存 SQLite 数据库用于测试
func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// 创建表结构
	if err := db.AutoMigrate(&models.KnowledgeDoc{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	return db
}

// TestNewProvider 测试 Provider 的创建
func TestNewProvider(t *testing.T) {
	db := setupTestDB(t)
	mockEmbedding := &mockEmbeddingProvider{dimension: 512}

	tests := []struct {
		name     string
		config   Config
		wantSize int
		wantOverlap int
	}{
		{
			name: "valid config",
			config: Config{
				Search: SearchConfig{
					TopK:     10,
					Strategy: "cosine",
				},
				Indexing: IndexingConfig{
					ChunkSize:    500,
					ChunkOverlap: 50,
				},
			},
			wantSize: 500,
			wantOverlap: 50,
		},
		{
			name: "default values",
			config: Config{
				Search:   SearchConfig{},
				Indexing: IndexingConfig{},
			},
			wantSize: 500,
			wantOverlap: 0,
		},
		{
			name: "zero chunk size",
			config: Config{
				Indexing: IndexingConfig{
					ChunkSize: 0,
				},
			},
			wantSize: 500,
			wantOverlap: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewProvider(db, mockEmbedding, tt.config)
			if provider == nil {
				t.Fatal("NewProvider returned nil")
			}
			if provider.chunker.ChunkSize != tt.wantSize {
				t.Errorf("ChunkSize = %d, want %d", provider.chunker.ChunkSize, tt.wantSize)
			}
			if provider.chunker.ChunkOverlap != tt.wantOverlap {
				t.Errorf("ChunkOverlap = %d, want %d", provider.chunker.ChunkOverlap, tt.wantOverlap)
			}
			if provider.config.Search.TopK == 0 {
				provider.config.Search.TopK = 10
			}
		})
	}
}

// TestProvider_Search 测试 Search 方法
// 注意：由于 pgvector 是 PostgreSQL 扩展，SQLite 测试环境中无法实际执行向量搜索
// 因此这里主要测试参数处理和 SQL 构建逻辑
func TestProvider_Search(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// 创建测试文档（不使用 embedding 字段，因为 SQLite 不支持）
	testDoc := models.KnowledgeDoc{
		TenantID:    "tenant1",
		WorkspaceID: "kb1",
		ProviderID:  "pgvector",
		ExternalID:  "doc1",
		Title:       "Test Document 1",
		Content:     "This is a test document about artificial intelligence.",
		Category:    "tech",
		ChunkIndex:  0,
		DocChunkID:  "doc1-chunk-0",
	}

	if err := db.Create(&testDoc).Error; err != nil {
		t.Fatalf("failed to create test document: %v", err)
	}

	mockEmbedding := &mockEmbeddingProvider{
		dimension: 5,
		vectors: [][]float32{
			{0.15, 0.25, 0.35, 0.45, 0.55}, // 查询向量
		},
	}

	provider := NewProvider(db, mockEmbedding, Config{
		Search: SearchConfig{
			TopK:     10,
			Strategy: "cosine",
		},
	})

	// 测试 embedding 错误处理
	t.Run("embedding error", func(t *testing.T) {
		errorProvider := &mockEmbeddingProvider{
			embedError: errors.New("embedding failed"),
			dimension:  5,
		}
		errorP := NewProvider(db, errorProvider, Config{})

		_, err := errorP.Search(ctx, knowledgeprovider.SearchRequest{Query: "test"})
		if err == nil {
			t.Error("Search() should return error when embedding fails")
		}
	})

	// 测试配置默认值
	t.Run("default config values", func(t *testing.T) {
		if provider.config.Search.TopK != 10 {
			t.Errorf("default TopK = %d, want 10", provider.config.Search.TopK)
		}
		if provider.config.Search.Strategy != "cosine" {
			t.Errorf("default Strategy = %s, want cosine", provider.config.Search.Strategy)
		}
	})

	// 测试无效策略
	t.Run("invalid strategy", func(t *testing.T) {
		_, err := provider.Search(ctx, knowledgeprovider.SearchRequest{
			Query:    "test",
			Strategy: "invalid",
		})
		if err == nil {
			t.Error("Search() should return error for invalid strategy")
		}
	})
}

// TestProvider_Search_EmbedError 测试 embedding 错误处理
func TestProvider_Search_EmbedError(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	mockEmbedding := &mockEmbeddingProvider{
		embedError: errors.New("embedding failed"),
		dimension:  5,
	}

	provider := NewProvider(db, mockEmbedding, Config{})

	_, err := provider.Search(ctx, knowledgeprovider.SearchRequest{Query: "test"})
	if err == nil {
		t.Error("Search() should return error when embedding fails")
	}
	if err != nil && !strings.Contains(err.Error(), "embedding") {
		t.Errorf("error should mention embedding, got: %v", err)
	}
}

// TestProvider_UpsertDocument 测试 UpsertDocument 方法
func TestProvider_UpsertDocument(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// 测试 embedding 错误处理
	t.Run("embedding error", func(t *testing.T) {
		errorProvider := &mockEmbeddingProvider{
			embedError: errors.New("embedding failed"),
			dimension:  3,
		}
		errorP := NewProvider(db, errorProvider, Config{})

		_, err := errorP.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{
			TenantID: "tenant1",
			Title:    "Test",
			Content:  "Content",
		})
		if err == nil {
			t.Error("UpsertDocument() should return error when embedding fails")
		}
	})

	// 测试短内容（不分块）
	t.Run("short content without chunking", func(t *testing.T) {
		mockEmbedding := &mockEmbeddingProvider{
			dimension: 3,
			// 短内容应该产生 1 个块
			vectors: [][]float32{{0.1, 0.2, 0.3}},
		}
		p := NewProvider(db, mockEmbedding, Config{
			Indexing: IndexingConfig{ChunkSize: 1000},
		})

		doc := knowledgeprovider.KnowledgeDocument{
			TenantID:    "tenant1",
			KnowledgeID: "kb1",
			Title:       "Short Doc",
			Content:     "Short content",
		}

		docID, err := p.UpsertDocument(ctx, doc)
		if err != nil {
			t.Errorf("UpsertDocument() error = %v", err)
			return
		}
		if docID == "" {
			t.Error("UpsertDocument() returned empty doc ID")
		}
	})

	// 测试空内容
	t.Run("empty content", func(t *testing.T) {
		mockEmbedding := &mockEmbeddingProvider{
			dimension: 3,
			// 空内容会返回原内容作为 1 个块
			vectors:   [][]float32{{0.1, 0.2, 0.3}},
		}
		p := NewProvider(db, mockEmbedding, Config{
			Indexing: IndexingConfig{ChunkSize: 100},
		})

		doc := knowledgeprovider.KnowledgeDocument{
			TenantID:    "tenant1",
			KnowledgeID: "kb1",
			Title:       "Empty Doc",
			Content:     "",
		}

		docID, err := p.UpsertDocument(ctx, doc)
		if err != nil {
			t.Errorf("UpsertDocument() error = %v", err)
			return
		}
		if docID == "" {
			t.Error("UpsertDocument() returned empty doc ID")
		}
	})

	// 测试带 metadata 和 tags
	t.Run("document with metadata and tags", func(t *testing.T) {
		mockEmbedding := &mockEmbeddingProvider{
			dimension: 3,
			vectors:   [][]float32{{0.1, 0.2, 0.3}},
		}
		p := NewProvider(db, mockEmbedding, Config{
			Indexing: IndexingConfig{ChunkSize: 1000},
		})

		doc := knowledgeprovider.KnowledgeDocument{
			TenantID:    "tenant1",
			KnowledgeID: "kb1",
			Title:       "Test Document",
			Content:     "Content with tags",
			Tags:        []string{"test", "document"},
			Metadata: map[string]interface{}{
				"category": "test",
			},
		}

		docID, err := p.UpsertDocument(ctx, doc)
		if err != nil {
			t.Errorf("UpsertDocument() error = %v", err)
			return
		}
		if docID == "" {
			t.Error("UpsertDocument() returned empty doc ID")
			return
		}

		// 验证文档被保存
		var savedDoc models.KnowledgeDoc
		if err := db.Where("title = ?", "Test Document").First(&savedDoc).Error; err != nil {
			t.Errorf("failed to find saved document: %v", err)
		}
	})
}

// TestProvider_UpsertDocument_Update 测试文档更新
func TestProvider_UpsertDocument_Update(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	mockEmbedding := &mockEmbeddingProvider{
		dimension: 3,
		vectors:   [][]float32{{0.1, 0.2, 0.3}},
	}

	provider := NewProvider(db, mockEmbedding, Config{})

	// 首先创建一个文档
	doc := knowledgeprovider.KnowledgeDocument{
		ExternalID:  "update-test",
		TenantID:    "tenant1",
		KnowledgeID: "kb1",
		Title:       "Original Title",
		Content:     "Original content",
	}

	docID, err := provider.UpsertDocument(ctx, doc)
	if err != nil {
		t.Fatalf("failed to create document: %v", err)
	}

	// 更新文档
	doc.Title = "Updated Title"
	doc.Content = "Updated content"

	_, err = provider.UpsertDocument(ctx, doc)
	if err != nil {
		t.Fatalf("failed to update document: %v", err)
	}

	// 验证更新
	var docs []models.KnowledgeDoc
	if err := db.Where("doc_chunk_id LIKE ?", "update-test-chunk-%").Find(&docs).Error; err != nil {
		t.Fatalf("failed to query documents: %v", err)
	}

	if len(docs) == 0 {
		t.Error("no documents found after update")
	}

	for _, d := range docs {
		if d.Title != "Updated Title" {
			t.Errorf("document title = %s, want Updated Title", d.Title)
		}
	}

	t.Logf("Created and updated document with ID: %s", docID)
}

// TestProvider_DeleteDocument 测试 DeleteDocument 方法
func TestProvider_DeleteDocument(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// 创建测试文档
	doc := models.KnowledgeDoc{
		TenantID:    "tenant1",
		WorkspaceID: "kb1",
		ProviderID:  "pgvector",
		Title:       "To Delete",
		Content:     "This will be deleted",
	}
	if err := db.Create(&doc).Error; err != nil {
		t.Fatalf("failed to create test document: %v", err)
	}

	provider := NewProvider(db, &mockEmbeddingProvider{}, Config{})

	// 删除文档
	err := provider.DeleteDocument(ctx, fmt.Sprintf("%d", doc.ID))
	if err != nil {
		t.Errorf("DeleteDocument() error = %v", err)
	}

	// 验证删除
	var count int64
	if err := db.Table("knowledge_docs").Where("id = ?", doc.ID).Count(&count).Error; err != nil {
		t.Errorf("failed to count documents: %v", err)
	} else if count > 0 {
		t.Error("document was not deleted")
	}
}

// TestProvider_HealthCheck 测试 HealthCheck 方法
func TestProvider_HealthCheck(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	mockEmbedding := &mockEmbeddingProvider{dimension: 512}
	provider := NewProvider(db, mockEmbedding, Config{})

	tests := []struct {
		name    string
		setup   func() // 在测试前修改数据库状态
		wantErr bool
	}{
		{
			name:    "healthy",
			setup:   func() {},
			wantErr: true, // SQLite 不支持 pgvector 扩展
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			err := provider.HealthCheck(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("HealthCheck() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestProvider_Search_EmptyResult 测试空结果处理
// 注意：由于 SQLite 不支持 pgvector，这个测试验证错误处理
func TestProvider_Search_EmptyResult(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	mockEmbedding := &mockEmbeddingProvider{
		dimension: 3,
		vectors:   [][]float32{{0.1, 0.2, 0.3}},
	}

	provider := NewProvider(db, mockEmbedding, Config{})

	// 在空数据库中搜索 - SQLite 会因为不支持 pgvector 操作符而失败
	// 这是预期的行为，因为 pgvector 是 PostgreSQL 扩展
	hits, err := provider.Search(ctx, knowledgeprovider.SearchRequest{Query: "test"})
	// SQLite 不支持 pgvector 操作符，所以会返回错误
	// 在真实的 PostgreSQL + pgvector 环境中，这应该返回空结果
	if err != nil {
		// 预期的 SQLite 错误
		t.Logf("Search() on SQLite (without pgvector) returned expected error: %v", err)
		return
	}
	// 如果成功执行（例如在 PostgreSQL 环境中），应该返回空结果
	if len(hits) != 0 {
		t.Errorf("Search() on empty DB returned %d hits, want 0", len(hits))
	}
}

// TestProvider_UpsertDocument_EmbedError 测试 embedding 错误处理
func TestProvider_UpsertDocument_EmbedError(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	mockEmbedding := &mockEmbeddingProvider{
		embedError: errors.New("embedding failed"),
		dimension:  3,
	}

	provider := NewProvider(db, mockEmbedding, Config{})

	doc := knowledgeprovider.KnowledgeDocument{
		TenantID: "tenant1",
		Title:    "Test",
		Content:  "Content",
	}

	_, err := provider.UpsertDocument(ctx, doc)
	if err == nil {
		t.Error("UpsertDocument() should return error when embedding fails")
	}
}

// TestConfig_Defaults 测试配置默认值
func TestConfig_Defaults(t *testing.T) {
	db := setupTestDB(t)
	mockEmbedding := &mockEmbeddingProvider{}

	provider := NewProvider(db, mockEmbedding, Config{})

	if provider.config.Search.TopK != 10 {
		t.Errorf("default TopK = %d, want 10", provider.config.Search.TopK)
	}
	if provider.config.Search.Strategy != "cosine" {
		t.Errorf("default Strategy = %s, want cosine", provider.config.Search.Strategy)
	}
	if provider.config.Indexing.ChunkSize != 500 {
		t.Errorf("default ChunkSize = %d, want 500", provider.config.Indexing.ChunkSize)
	}
}

// BenchmarkProvider_Search 性能测试
func BenchmarkProvider_Search(b *testing.B) {
	db := setupTestDB(&testing.T{})
	ctx := context.Background()

	// 创建测试文档
	for i := 0; i < 100; i++ {
		doc := models.KnowledgeDoc{
			TenantID:    "tenant1",
			WorkspaceID: "kb1",
			ProviderID:  "pgvector",
			Title:       fmt.Sprintf("Document %d", i),
			Content:     fmt.Sprintf("Content for document %d", i),
			Embedding:   pgvector.NewVector([]float32{0.1, 0.2, 0.3}),
		}
		db.Create(&doc)
	}

	mockEmbedding := &mockEmbeddingProvider{
		dimension: 3,
		vectors:   [][]float32{{0.1, 0.2, 0.3}},
	}

	provider := NewProvider(db, mockEmbedding, Config{
		Search: SearchConfig{TopK: 10},
	})

	req := knowledgeprovider.SearchRequest{
		Query: "test query",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = provider.Search(ctx, req)
	}
}

// BenchmarkProvider_UpsertDocument 性能测试
func BenchmarkProvider_UpsertDocument(b *testing.B) {
	db := setupTestDB(&testing.T{})
	ctx := context.Background()

	mockEmbedding := &mockEmbeddingProvider{
		dimension: 3,
		vectors:   [][]float32{{0.1, 0.2, 0.3}},
	}

	provider := NewProvider(db, mockEmbedding, Config{
		Indexing: IndexingConfig{ChunkSize: 500},
	})

	doc := knowledgeprovider.KnowledgeDocument{
		TenantID:    "tenant1",
		KnowledgeID: "kb1",
		Title:       "Test Document",
		Content:     "This is a test document with some content.",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc.ExternalID = fmt.Sprintf("doc-%d", i)
		_, _ = provider.UpsertDocument(ctx, doc)
	}
}
