package pgvector

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/pgvector/pgvector-go"
	"servify/apps/server/internal/models"
	"servify/apps/server/internal/platform/embedding"
	"servify/apps/server/internal/platform/knowledgeprovider"
)

// Config 包含 PgvectorProvider 的配置
type Config struct {
	Search   SearchConfig
	Indexing IndexingConfig
}

// SearchConfig 是搜索配置
type SearchConfig struct {
	TopK      int
	Threshold float64
	Strategy  string // "cosine" or "euclidean"
}

// IndexingConfig 是索引配置
type IndexingConfig struct {
	ChunkSize    int
	ChunkOverlap int
}

// Provider 实现 knowledgeprovider.Provider 接口
type Provider struct {
	db        *gorm.DB
	embedding embedding.Provider
	config    Config
	chunker   *Chunker
}

// NewProvider 创建新的 PgvectorProvider
func NewProvider(db *gorm.DB, emb embedding.Provider, cfg Config) *Provider {
	if cfg.Search.TopK <= 0 {
		cfg.Search.TopK = 10
	}
	if cfg.Search.Strategy == "" {
		cfg.Search.Strategy = "cosine"
	}
	if cfg.Indexing.ChunkSize <= 0 {
		cfg.Indexing.ChunkSize = 500
	}

	return &Provider{
		db:        db,
		embedding: emb,
		config:    cfg,
		chunker:   NewChunker(cfg.Indexing.ChunkSize, cfg.Indexing.ChunkOverlap),
	}
}

// Search 执行向量相似度搜索
func (p *Provider) Search(ctx context.Context, req knowledgeprovider.SearchRequest) ([]knowledgeprovider.KnowledgeHit, error) {
	// 1. 将 query 转为向量
	vectors, err := p.embedding.Embed(ctx, []string{req.Query})
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("no vectors returned from embedding provider")
	}
	queryVector := vectors[0]

	// 2. 确定 topK 和 threshold
	topK := req.TopK
	if topK <= 0 {
		topK = p.config.Search.TopK
	}

	threshold := req.Threshold
	if threshold == 0 {
		threshold = p.config.Search.Threshold
	}

	strategy := req.Strategy
	if strategy == "" {
		strategy = p.config.Search.Strategy
	}

	// 3. 构建查询
	query := p.db.WithContext(ctx).Table("knowledge_docs")

	// 添加过滤条件
	if req.TenantID != "" {
		query = query.Where("tenant_id = ?", req.TenantID)
	}
	if req.KnowledgeID != "" {
		query = query.Where("workspace_id = ?", req.KnowledgeID)
	}

	// 根据 strategy 选择不同的搜索方式
	switch strategy {
	case "cosine":
		// 余弦相似度搜索：使用 <=> 操作符
		// pgvector 的 <=> 返回余弦距离，距离越小越相似
		// 余弦相似度 = 1 - 余弦距离
		pgvectorVec := pgvector.NewVector(queryVector)
		query = query.Order(fmt.Sprintf("embedding <=> '%s'::vector", pgvectorVec.String()))
	case "euclidean":
		// 欧几里得距离搜索：使用 <-> 操作符
		pgvectorVec := pgvector.NewVector(queryVector)
		query = query.Order(fmt.Sprintf("embedding <-> '%s'::vector", pgvectorVec.String()))
	default:
		return nil, fmt.Errorf("unsupported search strategy: %s", strategy)
	}

	query = query.Limit(topK)

	// 4. 执行查询
	var docs []models.KnowledgeDoc
	if err := query.Find(&docs).Error; err != nil {
		return nil, fmt.Errorf("failed to execute search query: %w", err)
	}

	// 5. 构造返回结果并应用阈值过滤
	hits := make([]knowledgeprovider.KnowledgeHit, 0, len(docs))
	for _, doc := range docs {
		// 计算相似度分数
		var score float64
		var distance float32

		docEmbedding := doc.Embedding.Slice()
		if len(docEmbedding) != len(queryVector) {
			continue // 跳过维度不匹配的文档
		}

		switch strategy {
		case "cosine":
			distance = CosineDistance(queryVector, docEmbedding)
			score = float64(1 - distance) // 余弦相似度
		case "euclidean":
			distance = EuclideanDistance(queryVector, docEmbedding)
			// 将欧几里得距离转换为相似度分数
			// 使用指数衰减：score = exp(-distance)
			score = float64(exp32(-distance))
		}

		// 应用阈值过滤
		if threshold > 0 && score < threshold {
			continue
		}

		hits = append(hits, knowledgeprovider.KnowledgeHit{
			DocumentID: fmt.Sprintf("%d", doc.ID),
			Title:      doc.Title,
			Content:    doc.Content,
			Score:      score,
			Source:     doc.ProviderID,
			Metadata: map[string]interface{}{
				"tenant_id":    doc.TenantID,
				"workspace_id": doc.WorkspaceID,
				"external_id":  doc.ExternalID,
				"chunk_index":  doc.ChunkIndex,
				"doc_chunk_id": doc.DocChunkID,
				"category":     doc.Category,
			},
		})
	}

	return hits, nil
}

// UpsertDocument 创建或更新文档
func (p *Provider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
	// 1. 文档分块
	chunks := p.chunker.Chunk(doc.Content)
	if len(chunks) == 0 {
		chunks = []string{doc.Content} // 如果内容为空或太短，保留原内容
	}

	// 2. 为每个 chunk 生成向量
	vectors, err := p.embedding.Embed(ctx, chunks)
	if err != nil {
		return "", fmt.Errorf("failed to embed chunks: %w", err)
	}
	if len(vectors) != len(chunks) {
		return "", fmt.Errorf("embedding count mismatch: got %d, want %d", len(vectors), len(chunks))
	}

	// 3. 首先删除该文档的所有现有 chunks（如果是更新）
	if doc.ID != "" || doc.ExternalID != "" {
		query := p.db.WithContext(ctx).Table("knowledge_docs")
		if doc.ID != "" {
			query = query.Where("id = ?", doc.ID)
		} else if doc.ExternalID != "" {
			query = query.Where("external_id = ? AND provider_id = ?", doc.ExternalID, "pgvector")
		}
		if doc.TenantID != "" {
			query = query.Where("tenant_id = ?", doc.TenantID)
		}
		if err := query.Delete(&models.KnowledgeDoc{}).Error; err != nil {
			return "", fmt.Errorf("failed to delete existing document: %w", err)
		}
	}

	// 4. 存储每个 chunk
	var firstDocID string
	for i, chunk := range chunks {
		docModel := &models.KnowledgeDoc{
			TenantID:    doc.TenantID,
			WorkspaceID: doc.KnowledgeID,
			ProviderID:  "pgvector",
			ExternalID:  doc.ExternalID,
			Title:       doc.Title,
			Content:     chunk,
			Category:    "knowledge",
		}

		// 从 metadata 中获取额外的信息
		if category, ok := doc.Metadata["category"].(string); ok {
			docModel.Category = category
		}

		// 处理 tags
		if len(doc.Tags) > 0 {
			// 将 tags 数组转换为字符串存储
			tagsStr := ""
			for j, tag := range doc.Tags {
				if j > 0 {
					tagsStr += ","
				}
				tagsStr += tag
			}
			docModel.Tags = tagsStr
		}

		// 设置 chunk 相关字段
		docModel.ChunkIndex = i
		if doc.ExternalID != "" {
			docModel.DocChunkID = fmt.Sprintf("%s-chunk-%d", doc.ExternalID, i)
		} else {
			docModel.DocChunkID = fmt.Sprintf("chunk-%d", i)
		}

		// 设置向量
		docModel.Embedding = pgvector.NewVector(vectors[i])

		// 保存到数据库
		if err := p.db.WithContext(ctx).Create(docModel).Error; err != nil {
			return "", fmt.Errorf("failed to create chunk %d: %w", i, err)
		}

		if i == 0 {
			firstDocID = fmt.Sprintf("%d", docModel.ID)
		}
	}

	return firstDocID, nil
}

// DeleteDocument 删除文档及其所有 chunks
func (p *Provider) DeleteDocument(ctx context.Context, id string) error {
	query := p.db.WithContext(ctx).Table("knowledge_docs").Where("id = ?", id)
	result := query.Delete(&models.KnowledgeDoc{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete document: %w", result.Error)
	}
	return nil
}

// HealthCheck 检查数据库连接和 pgvector 扩展
func (p *Provider) HealthCheck(ctx context.Context) error {
	// 1. 检查数据库连接
	sqlDB, err := p.db.DB()
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	// 2. 检查 embedding provider
	if err := p.embedding.HealthCheck(ctx); err != nil {
		return fmt.Errorf("embedding provider health check failed: %w", err)
	}

	// 3. 检查 pgvector 扩展是否可用
	var extVersion string
	err = p.db.WithContext(ctx).Raw("SELECT extversion FROM pg_extension WHERE extname = 'vector'").Scan(&extVersion).Error
	if err != nil {
		return fmt.Errorf("pgvector extension not available: %w", err)
	}

	return nil
}

// exp32 计算 e^x，使用 float32
func exp32(x float32) float32 {
	// 使用 math.Exp 然后转换回 float32
	return float32(exp64(float64(x)))
}

// exp64 是 math.Exp 的别名，避免循环依赖
func exp64(x float64) float64 {
	// 这里使用一个简单的泰勒级数展开来计算 e^x
	// e^x = 1 + x + x^2/2! + x^3/3! + ...
	// 对于小的 x，这个近似足够好
	// 对于性能关键的代码，可以使用 math.Exp
	if x < -10 {
		return 0
	}
	if x > 10 {
		return 22026.465794806718 // e^10
	}

	result := 1.0
	term := 1.0
	for i := 1; i <= 20; i++ {
		term *= x / float64(i)
		result += term
		if term < 1e-15 {
			break
		}
	}
	return result
}
