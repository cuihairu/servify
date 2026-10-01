// Package local 是零外部依赖的自建知识引擎：真实分块 → 真实嵌入 →
// 持久化到 knowledge_docs 表（sqlite/postgres 双轨）→ 进程内余弦检索。
//
// 与 pgvector driver 的差异只在向量运算位置：pgvector 依赖 pg 扩展的
// <=>/<-> 算子，本包在 Go 内对候选分块做余弦相似度（候选集按租户/知识库
// 过滤后有上限截断），因此不依赖 pgvector 扩展，sqlite 部署形态也可用。
// 嵌入侧建议配 embedding.provider=local（零依赖确定性嵌入器）；检索分值
// 经幂次校准（cos^(1/3)）后回传，使强词法命中可越过编排层默认 0.7 的
// 相关门、弱命中仍被滤除（校准只单调保序，不改变排序）。
package local

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"
	"servify/apps/server/internal/platform/embedding"
	"servify/apps/server/internal/platform/knowledgeprovider"
	pgvectorkp "servify/apps/server/internal/platform/knowledgeprovider/pgvector"
)

// ProviderID 是本驱动在 knowledge_docs.provider_id 里的落库标识。
const ProviderID = "local"

// ProviderName 实现 NamedProvider：knowledge_docs.provider_id 落库标识。
func (p *Provider) ProviderName() string { return ProviderID }

// candidateScanLimit 是单次检索的最大候选分块数（进程内余弦是 O(n·d)，
// 上限防全表扫描；命中面按租户/知识库过滤后仍超限时取最近写入的分块）。
const candidateScanLimit = 5000

// Config 是本地知识引擎配置（结构与 pgvector driver 同形，便于配置迁移）。
type Config struct {
	Search   SearchConfig
	Indexing IndexingConfig
}

// SearchConfig 是检索配置。
type SearchConfig struct {
	TopK      int
	Threshold float64
	Strategy  string // semantic/hybrid → cosine；其余显式报错
}

// IndexingConfig 是索引配置。
type IndexingConfig struct {
	ChunkSize    int
	ChunkOverlap int
}

// Provider 实现 knowledgeprovider.KnowledgeProvider（本地自含面）。
type Provider struct {
	db        *gorm.DB
	embedding embedding.Provider
	config    Config
	chunker   *pgvectorkp.Chunker
}

// NewProvider 构造本地知识引擎。
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
		chunker:   pgvectorkp.NewChunker(cfg.Indexing.ChunkSize, cfg.Indexing.ChunkOverlap),
	}
}

// Search 语义检索：嵌入查询 → 载入候选分块 → 进程内余弦 → 校准 → 阈值/topK。
func (p *Provider) Search(ctx context.Context, req knowledgeprovider.SearchRequest) ([]knowledgeprovider.KnowledgeHit, error) {
	vectors, err := p.embedding.Embed(ctx, []string{req.Query})
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("no vectors returned from embedding provider")
	}
	queryVector := vectors[0]

	strategy := req.Strategy
	if strategy == "" {
		strategy = p.config.Search.Strategy
	}
	if normalizeSearchStrategy(strategy) != "cosine" {
		return nil, fmt.Errorf("unsupported search strategy: %s", strategy)
	}

	topK := req.TopK
	if topK <= 0 {
		topK = p.config.Search.TopK
	}
	threshold := req.Threshold
	if threshold == 0 {
		threshold = p.config.Search.Threshold
	}

	query := p.db.WithContext(ctx).Model(&models.KnowledgeDoc{}).
		Where("provider_id = ?", ProviderID)
	if req.TenantID != "" {
		query = query.Where("tenant_id = ?", req.TenantID)
	}
	if req.KnowledgeID != "" {
		query = query.Where("workspace_id = ?", req.KnowledgeID)
	}
	var docs []models.KnowledgeDoc
	if err := query.Order("id DESC").Limit(candidateScanLimit).Find(&docs).Error; err != nil {
		return nil, fmt.Errorf("failed to load candidate chunks: %w", err)
	}

	type scored struct {
		doc   models.KnowledgeDoc
		score float64
	}
	hits := make([]scored, 0, len(docs))
	for _, doc := range docs {
		docVector := doc.Embedding.Slice()
		if len(docVector) != len(queryVector) {
			continue // 维度不匹配的分块（历史遗留/异源写入）不参与排序
		}
		score := calibratedScore(cosineSimilarity(queryVector, docVector))
		if threshold > 0 && score < threshold {
			continue
		}
		hits = append(hits, scored{doc: doc, score: score})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].doc.ID > hits[j].doc.ID
	})
	if len(hits) > topK {
		hits = hits[:topK]
	}

	out := make([]knowledgeprovider.KnowledgeHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, knowledgeprovider.KnowledgeHit{
			DocumentID: fmt.Sprintf("%d", h.doc.ID),
			Title:      h.doc.Title,
			Content:    h.doc.Content,
			Score:      h.score,
			Source:     ProviderID,
			Metadata: map[string]interface{}{
				"tenant_id":    h.doc.TenantID,
				"workspace_id": h.doc.WorkspaceID,
				"external_id":  h.doc.ExternalID,
				"chunk_index":  h.doc.ChunkIndex,
				"doc_chunk_id": h.doc.DocChunkID,
				"category":     h.doc.Category,
			},
		})
	}
	return out, nil
}

// UpsertDocument 分块 → 嵌入 → 替换式落库（先删该文档既有分块再写入）。
func (p *Provider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
	externalID := strings.TrimSpace(doc.ExternalID)
	if externalID == "" {
		externalID = strings.TrimSpace(doc.ID)
	}
	chunks := p.chunker.Chunk(doc.Content)
	if len(chunks) == 0 {
		chunks = []string{doc.Content}
	}
	vectors, err := p.embedding.Embed(ctx, chunks)
	if err != nil {
		return "", fmt.Errorf("failed to embed chunks: %w", err)
	}
	if len(vectors) != len(chunks) {
		return "", fmt.Errorf("embedding count mismatch: got %d, want %d", len(vectors), len(chunks))
	}

	if err := p.deleteByDocument(ctx, externalID); err != nil {
		return "", err
	}

	var firstDocID string
	for i, chunk := range chunks {
		docModel := &models.KnowledgeDoc{
			TenantID:    doc.TenantID,
			WorkspaceID: doc.KnowledgeID,
			ProviderID:  ProviderID,
			ExternalID:  externalID,
			Title:       doc.Title,
			Content:     chunk,
			Category:    "knowledge",
		}
		if category, ok := doc.Metadata["category"].(string); ok {
			docModel.Category = category
		}
		if len(doc.Tags) > 0 {
			docModel.Tags = strings.Join(doc.Tags, ",")
		}
		docModel.ChunkIndex = i
		if externalID != "" {
			docModel.DocChunkID = fmt.Sprintf("%s-chunk-%d", externalID, i)
		} else {
			docModel.DocChunkID = fmt.Sprintf("chunk-%d", i)
		}
		docModel.Embedding = knowledgedomain.NewEmbedding(vectors[i])
		if err := p.db.WithContext(ctx).Create(docModel).Error; err != nil {
			return "", fmt.Errorf("failed to create chunk %d: %w", i, err)
		}
		if i == 0 {
			firstDocID = fmt.Sprintf("%d", docModel.ID)
		}
	}
	if externalID != "" {
		return externalID, nil
	}
	return firstDocID, nil
}

// DeleteDocument 删除文档及其全部分块（空 id 为无操作，与 pgvector 同契约）。
func (p *Provider) DeleteDocument(ctx context.Context, id string) error {
	return p.deleteByDocument(ctx, strings.TrimSpace(id))
}

// deleteByDocument 按 external_id+provider 删除既有分块；externalID 为空
// 返回 nil（Upsert 双参数来源保证至少一个非空才走到这里）。
func (p *Provider) deleteByDocument(ctx context.Context, externalID string) error {
	if externalID == "" {
		return nil
	}
	query := p.db.WithContext(ctx).Where("provider_id = ?", ProviderID).
		Where("external_id = ?", externalID)
	if err := query.Delete(&models.KnowledgeDoc{}).Error; err != nil {
		return fmt.Errorf("failed to delete existing document: %w", err)
	}
	return nil
}

// HealthCheck 数据库连通 + 嵌入器健康（无 pg 扩展依赖）。
func (p *Provider) HealthCheck(ctx context.Context) error {
	sqlDB, err := p.db.DB()
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}
	if err := p.embedding.HealthCheck(ctx); err != nil {
		return fmt.Errorf("embedding provider health check failed: %w", err)
	}
	return nil
}

// cosineSimilarity 计算两条等长向量的余弦相似度；零向量返回 0。
func cosineSimilarity(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	sim := dot / (math.Sqrt(na) * math.Sqrt(nb))
	if sim < 0 {
		return 0
	}
	return sim
}

// calibratedScore 把 [0,1] 余弦单调映射到检索分：幂次 1/3 校准使「查询
// 词面完整落在分块内」的强命中（cos≈0.3-0.6）越过编排层默认 0.7 相关门，
// 弱相关（cos<0.15）仍被滤除。只保序不重排。
func calibratedScore(cosine float64) float64 {
	if cosine <= 0 {
		return 0
	}
	return math.Pow(cosine, 1.0/3.0)
}

func normalizeSearchStrategy(strategy string) string {
	switch strings.TrimSpace(strings.ToLower(strategy)) {
	case "", "semantic", "hybrid", "cosine":
		return "cosine"
	default:
		return strings.TrimSpace(strings.ToLower(strategy))
	}
}
