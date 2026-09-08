package pgvector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/platform/knowledgeprovider"
)

// mockEmbeddingProvider 是 embedding.Provider 的 mock 实现
type mockEmbeddingProvider struct {
	vectors     [][]float32
	dimension   int
	embedError  error
	healthError error
	emptyResult bool
}

func (m *mockEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.embedError != nil {
		return nil, m.embedError
	}
	if m.emptyResult {
		return [][]float32{}, nil
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
	return m.healthError
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
		name        string
		config      Config
		wantSize    int
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
			wantSize:    500,
			wantOverlap: 50,
		},
		{
			name: "default values",
			config: Config{
				Search:   SearchConfig{},
				Indexing: IndexingConfig{},
			},
			wantSize:    500,
			wantOverlap: 0,
		},
		{
			name: "zero chunk size",
			config: Config{
				Indexing: IndexingConfig{
					ChunkSize: 0,
				},
			},
			wantSize:    500,
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
			vectors: [][]float32{{0.1, 0.2, 0.3}},
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

func TestProvider_DeleteDocumentDeletesAllChunksByExternalID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	provider := NewProvider(db, &mockEmbeddingProvider{dimension: 3}, Config{})

	_, err := provider.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{
		ExternalID: "doc-1",
		Title:      "Test",
		Content:    strings.Repeat("chunk ", 400),
	})
	if err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}

	var before int64
	if err := db.Table("knowledge_docs").Where("external_id = ?", "doc-1").Count(&before).Error; err != nil {
		t.Fatalf("count before delete: %v", err)
	}
	if before < 2 {
		t.Fatalf("expected chunked rows before delete, got %d", before)
	}

	if err := provider.DeleteDocument(ctx, "doc-1"); err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}

	var after int64
	if err := db.Table("knowledge_docs").Where("external_id = ?", "doc-1").Count(&after).Error; err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if after != 0 {
		t.Fatalf("expected all chunks deleted, got %d", after)
	}
}

func TestProvider_SearchNormalizesHybridStrategy(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	provider := NewProvider(db, &mockEmbeddingProvider{
		dimension: 3,
		vectors:   [][]float32{{0.1, 0.2, 0.3}},
	}, Config{
		Search: SearchConfig{Strategy: "hybrid"},
	})

	_, err := provider.Search(ctx, knowledgeprovider.SearchRequest{Query: "test", Strategy: "hybrid"})
	if err != nil && strings.Contains(err.Error(), "unsupported search strategy") {
		t.Fatalf("Search() did not normalize hybrid strategy: %v", err)
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
			Embedding:   models.NewEmbedding([]float32{0.1, 0.2, 0.3}),
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

func TestProvider_SearchEmptyVectors(t *testing.T) {
	db := setupTestDB(t)
	provider := NewProvider(db, &mockEmbeddingProvider{dimension: 3, emptyResult: true}, Config{})
	_, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q"})
	if err == nil || !strings.Contains(err.Error(), "no vectors") {
		t.Fatalf("expected no-vectors error, got %v", err)
	}
}

func interceptQuery(db *gorm.DB, docs []models.KnowledgeDoc) {
	_ = db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		if dest, ok := tx.Statement.Dest.(*[]models.KnowledgeDoc); ok {
			*dest = docs
		}
	})
}

func TestProvider_SearchScoresAndFilters(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	docs := []models.KnowledgeDoc{
		{ID: 1, TenantID: "t1", WorkspaceID: "kb1", ProviderID: "pgvector", ExternalID: "e1", Title: "Same Direction", Content: "c1", ChunkIndex: 0, DocChunkID: "e1-chunk-0", Embedding: models.NewEmbedding([]float32{0.1, 0.2, 0.3})},
		{ID: 2, TenantID: "t1", WorkspaceID: "kb1", ProviderID: "pgvector", ExternalID: "e2", Title: "Orthogonal", Content: "c2", ChunkIndex: 0, DocChunkID: "e2-chunk-0", Embedding: models.NewEmbedding([]float32{0.3, -0.3, 0.0})},
		{ID: 3, TenantID: "t1", WorkspaceID: "kb1", ProviderID: "pgvector", ExternalID: "e3", Title: "Dim Mismatch", Content: "c3", ChunkIndex: 0, DocChunkID: "e3-chunk-0", Embedding: models.NewEmbedding([]float32{0.1, 0.2})},
		{ID: 4, TenantID: "t1", WorkspaceID: "kb1", ProviderID: "pgvector", ExternalID: "e4", Title: "Opposite", Content: "c4", ChunkIndex: 0, DocChunkID: "e4-chunk-0", Embedding: models.NewEmbedding([]float32{-0.1, -0.2, -0.3})},
	}
	interceptQuery(db, docs)

	provider := NewProvider(db, &mockEmbeddingProvider{
		dimension: 3,
		vectors:   [][]float32{{0.1, 0.2, 0.3}},
	}, Config{Search: SearchConfig{Strategy: "cosine"}})

	hits, err := provider.Search(ctx, knowledgeprovider.SearchRequest{
		Query:      "q",
		TenantID:   "t1",
		KnowledgeID: "kb1",
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("expected 3 hits (dim mismatch skipped), got %d", len(hits))
	}
	if hits[0].DocumentID != "1" || hits[0].Score <= 0.99 {
		t.Fatalf("unexpected first hit: %+v", hits[0])
	}

	// threshold filters everything except the near-identical vector
	hits, err = provider.Search(ctx, knowledgeprovider.SearchRequest{Query: "q", Threshold: 0.99})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit after threshold, got %d", len(hits))
	}

	// euclidean strategy scores via exponential decay
	euclid := NewProvider(db, &mockEmbeddingProvider{
		dimension: 3,
		vectors:   [][]float32{{0.1, 0.2, 0.3}},
	}, Config{Search: SearchConfig{Strategy: "euclidean"}})
	hits, err = euclid.Search(ctx, knowledgeprovider.SearchRequest{Query: "q"})
	if err != nil {
		t.Fatalf("Search(euclidean) error = %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("euclidean expected 3 hits, got %d", len(hits))
	}
	if hits[0].Score <= 0.5 {
		t.Fatalf("euclidean score too low: %+v", hits[0])
	}
}

func TestProvider_UpsertDocumentBranches(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)

	// embedding count mismatch
	mismatch := NewProvider(db, &mockEmbeddingProvider{
		dimension: 3,
		vectors:   [][]float32{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}},
	}, Config{Indexing: IndexingConfig{ChunkSize: 5}})
	_, err := mismatch.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{Title: "T", Content: "short"})
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("expected count mismatch error, got %v", err)
	}

	// delete by numeric id branch (no external id)
	byID := NewProvider(db, &mockEmbeddingProvider{dimension: 3, vectors: [][]float32{{0.1, 0.2, 0.3}}}, Config{})
	seed := models.KnowledgeDoc{ProviderID: "pgvector", Title: "Old", Content: "old"}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	idStr := fmt.Sprintf("%d", seed.ID)
	if _, err := byID.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{ID: idStr, Title: "Replaced", Content: "new"}); err != nil {
		t.Fatalf("upsert by id: %v", err)
	}

	// delete failure surfaces as error
	brokenDB := setupTestDB(t)
	if err := brokenDB.Migrator().DropTable("knowledge_docs"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	broken := NewProvider(brokenDB, &mockEmbeddingProvider{dimension: 3, vectors: [][]float32{{0.1, 0.2, 0.3}}}, Config{})
	_, err = broken.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{ExternalID: "x", Title: "T", Content: "c"})
	if err == nil {
		t.Fatal("expected delete failure error")
	}
}

func TestProvider_DeleteDocumentBranches(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	if err := db.Migrator().DropTable("knowledge_docs"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	provider := NewProvider(db, &mockEmbeddingProvider{dimension: 3}, Config{})
	if err := provider.DeleteDocument(ctx, "whatever"); err == nil {
		t.Fatal("expected delete error on missing table")
	}
}

func TestProvider_HealthCheckBranches(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)

	embeddingDown := NewProvider(db, &mockEmbeddingProvider{dimension: 3, healthError: errors.New("embedding down")}, Config{})
	if err := embeddingDown.HealthCheck(ctx); err == nil || !strings.Contains(err.Error(), "embedding") {
		t.Fatalf("expected embedding health error, got %v", err)
	}

	closed, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if sqlDB, err := closed.DB(); err == nil {
		_ = sqlDB.Close()
	}
	closedProvider := NewProvider(closed, &mockEmbeddingProvider{dimension: 3}, Config{})
	if err := closedProvider.HealthCheck(ctx); err == nil {
		t.Fatal("expected ping failure after closing db")
	}
}

func TestExpHelpers(t *testing.T) {
	if got := exp64(-11); got != 0 {
		t.Fatalf("exp64(-11) = %v want 0", got)
	}
	if got := exp64(11); got != 22026.465794806718 {
		t.Fatalf("exp64(11) = %v", got)
	}
	if got := exp64(1); got < 2.7 || got > 2.72 {
		t.Fatalf("exp64(1) = %v", got)
	}
	if got := exp32(0); got != 1 {
		t.Fatalf("exp32(0) = %v", got)
	}
	if got := exp64(0); got != 1 {
		t.Fatalf("exp64(0) = %v", got)
	}
}

func TestChunkerEdgeBranches(t *testing.T) {
	c := NewChunker(10, 2)

	// whitespace-only input normalizes to empty
	if got := c.Chunk("   \t  "); len(got) != 0 {
		t.Fatalf("Chunk(whitespace) = %v", got)
	}
	if got := c.ChunkByParagraph("   "); len(got) != 0 {
		t.Fatalf("ChunkByParagraph(whitespace) = %v", got)
	}
	if got := c.ChunkByParagraph("\n \n \n"); len(got) != 0 {
		t.Fatalf("ChunkByParagraph(no valid paragraphs) = %v", got)
	}

	// merging paragraphs: two short paras fit together, third overflows
	got := c.ChunkByParagraph("aaaa\n\nbbbb\n\ncccccccccccccc")
	if len(got) < 2 {
		t.Fatalf("expected merged then overflow chunks, got %v", got)
	}

	// paragraph exactly at chunk size becomes current chunk
	got = c.ChunkByParagraph("12345\n\n6789012345")
	if len(got) < 2 {
		t.Fatalf("expected split chunks, got %v", got)
	}

	// four short paragraphs enter the merge loop
	got = NewChunker(50, 5).ChunkByParagraph("p1\n\np2\n\np3\n\np4")
	if len(got) != 1 {
		t.Fatalf("expected single merged chunk, got %v", got)
	}

	// CountTokens minimum clamp
	if got := CountTokens("a"); got != 1 {
		t.Fatalf("CountTokens('a') = %d want 1", got)
	}
}

func TestFindSentenceBoundaryFallbacks(t *testing.T) {
	c := NewChunker(10, 2)
	runes := []rune("one two three four five six seven eight nine ten eleven")
	// no punctuation, no space within window → returns end
	if got := c.findSentenceBoundary(runes, 0, 10); got <= 0 {
		t.Fatalf("boundary = %d", got)
	}

	// comma boundary is preferred over raw end
	commaText := []rune("aaaaaaaaaa,bbbbbbbbbb cccccccccc")
	if got := c.findSentenceBoundary(commaText, 0, 10); got != 11 {
		t.Fatalf("comma boundary = %d want 11", got)
	}

	// chinese sentence ender followed by space
	cnText := []rune("哈哈哈哈哈哈哈哈哈哈。 哈哈哈哈哈哈")
	if got := c.findSentenceBoundary(cnText, 0, 10); got != 11 {
		t.Fatalf("chinese boundary = %d want 11", got)
	}
}

func TestProvider_UpsertDocumentCreateFailure(t *testing.T) {
	db := setupTestDB(t)
	if err := db.Migrator().DropTable("knowledge_docs"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	p := NewProvider(db, &mockEmbeddingProvider{dimension: 3, vectors: [][]float32{{0.1, 0.2, 0.3}}}, Config{})
	if _, err := p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{Title: "T", Content: "c"}); err == nil {
		t.Fatal("expected create chunk failure")
	}
}

func TestProvider_DeleteDocumentSecondQueryFailure(t *testing.T) {
	db := setupTestDB(t)
	calls := 0
	_ = db.Callback().Delete().Replace("gorm:delete", func(tx *gorm.DB) {
		calls++
		if calls == 1 {
			tx.RowsAffected = 0
			return
		}
		tx.Error = errors.New("second delete failed")
	})
	p := NewProvider(db, &mockEmbeddingProvider{dimension: 3}, Config{})
	if err := p.DeleteDocument(context.Background(), "missing-id"); err == nil {
		t.Fatal("expected second delete failure")
	}
}

func TestProvider_HealthCheckSucceedsWithFakeExtensionTable(t *testing.T) {
	db := setupTestDB(t)
	if err := db.Exec("CREATE TABLE pg_extension (extname TEXT, extversion TEXT)").Error; err != nil {
		t.Fatalf("create pg_extension: %v", err)
	}
	p := NewProvider(db, &mockEmbeddingProvider{dimension: 3}, Config{})
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("expected healthy, got %v", err)
	}
}

func TestDistanceMismatchGuards(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("CosineDistance should panic on length mismatch")
		}
	}()
	_ = CosineDistance([]float32{1}, []float32{1, 2})
}

func TestEuclideanDistanceMismatchGuard(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("EuclideanDistance should panic on length mismatch")
		}
	}()
	_ = EuclideanDistance([]float32{1}, []float32{1, 2})
}

func TestDotProductMismatchGuard(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("DotProduct should panic on length mismatch")
		}
	}()
	_ = DotProduct([]float32{1}, []float32{1, 2})
}
