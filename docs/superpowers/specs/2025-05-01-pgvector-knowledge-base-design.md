# Self-Hosted pgvector Knowledge Base Design Document

**Created:** 2025-05-01
**Status:** Superseded (2026-10-10 note) — the pgvector self-hosted provider and the openai/tei embedding acceptance paths have been superseded by the local self-contained knowledge engine (`scripts/test-local-knowledge-acceptance.sh`, `make local-knowledge-acceptance`) and the per-provider acceptance scripts (ragflow/dify/weknora/pgvector); `test-knowledge-acceptance.sh` has been deleted, and this document is kept as a historical record.
**Author:** AI Assistant

## 1. Overview

### 1.1 Background

Servify currently supports Dify and WeKnora as external knowledge base providers. To support enterprise intranet private deployment scenarios, a self-hosted knowledge base solution based on pgvector needs to be implemented as the default recommended option.

### 1.2 Goals

- Implement an Embedding service abstraction layer supporting multiple embedding providers
- Implement PgvectorProvider as the default knowledge base provider
- Keep Dify/WeKnora as optional providers
- Support both cloud deployment (OpenAI) and intranet deployment (TEI/Xinference) modes

### 1.3 Non-Goals

- Implement complex document parsing (PDF, Word, etc.) - plain text only for now
- Implement local model inference - implemented via external services (TEI/Xinference)
- Replace the existing Dify/WeKnora providers - keep them as options

## 2. Architecture Design

### 2.1 Overall Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Knowledge Service                           │
│  (CreateDocument, UpdateDocument, Search, RunIndexJob)             │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                    ┌───────────────┼───────────────┐
                    ▼               ▼               ▼
        ┌──────────────────┐ ┌──────────────┐ ┌──────────────┐
        │ PgvectorProvider │ │ DifyProvider │ │WeKnoraProvider│
        │   (默认)          │ │  (可选)       │ │   (兼容)      │
        └──────────────────┘ └──────────────┘ └──────────────┘
                    │
                    ▼
        ┌──────────────────────────────────┐
        │    EmbeddingService              │
        │   (抽象层，支持多 provider)      │
        └──────────────────────────────────┘
                    │
        ┌───────────┼───────────┬───────────┐
        ▼           ▼           ▼           ▼
   ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐
   │ OpenAI │ │   TEI  │ │Xinference│ │ Local │
   │  Cloud │ │ 内网   │ │  内网   │ │ 模型  │
   └────────┘ └────────┘ └────────┘ └────────┘
```

### 2.2 Core Interfaces

#### EmbeddingProvider Interface

```go
// apps/server/internal/platform/embedding/provider.go
package embedding

import "context"

// EmbeddingProvider 定义文本嵌入服务接口
type EmbeddingProvider interface {
    // Embed 返回文本的向量表示
    Embed(ctx context.Context, texts []string) ([][]float32, error)

    // Dimension 返回向量维度
    Dimension() int

    // HealthCheck 健康检查
    HealthCheck(ctx context.Context) error
}
```

#### Provider Implementations

| Provider | File Location | Vector Dimension | Use |
|----------|----------|----------|------|
| `OpenAIProvider` | `embedding/openai/provider.go` | 1536 | Default for cloud deployment |
| `TEIProvider` | `embedding/tei/provider.go` | 512/768 | Recommended for intranet deployment |
| `XinferenceProvider` | `embedding/xinference/provider.go` | Variable | Intranet alternative |

## 3. Database Design

### 3.1 Table Structure Changes

```sql
-- 扩展 knowledge_docs 表
ALTER TABLE knowledge_docs
ADD COLUMN embedding vector(1536),
ADD COLUMN chunk_index int DEFAULT 0,
ADD COLUMN doc_chunk_id varchar(255);

-- 创建向量索引
CREATE INDEX idx_knowledge_docs_embedding
ON knowledge_docs
USING ivfflat (embedding vector_cosine_ops)
WITH (lists = 100);
```

### 3.2 Model Extension

```go
// apps/server/internal/models/models.go
type KnowledgeDoc struct {
    ID          uint      `gorm:"primaryKey"`
    TenantID    string    `gorm:"index"`
    WorkspaceID string    `gorm:"index"`
    ProviderID  string    `gorm:"index"`
    ExternalID  string    `gorm:"index"`
    Title       string
    Content     string    `gorm:"type:text"`
    Category    string
    Tags        string
    IsPublic    bool      `gorm:"default:false;index"`
    // 新增字段
    Embedding   pgvector.Vector `gorm:"type:vector(1536)"`
    ChunkIndex  int             `gorm:"default:0"`
    DocChunkID  string          `gorm:"index"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

## 4. Component Design

### 4.1 EmbeddingProvider Implementations

#### OpenAIProvider

```go
// apps/server/internal/platform/embedding/openai/provider.go
package openai

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"

    "servify/apps/server/internal/platform/embedding"
)

type Config struct {
    APIKey  string
    BaseURL string
    Model   string
}

type Provider struct {
    config Config
    client *http.Client
}

func NewProvider(cfg Config) *Provider {
    if cfg.BaseURL == "" {
        cfg.BaseURL = "https://api.openai.com/v1"
    }
    if cfg.Model == "" {
        cfg.Model = "text-embedding-3-small"
    }
    return &Provider{
        config: cfg,
        client: &http.Client{Timeout: 30 * time.Second},
    }
}

func (p *Provider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
    // 调用 OpenAI Embeddings API
    // POST /v1/embeddings
}

func (p *Provider) Dimension() int {
    return 1536
}

func (p *Provider) HealthCheck(ctx context.Context) error {
    // GET /v1/models
}
```

#### TEIProvider

```go
// apps/server/internal/platform/embedding/tei/provider.go
package tei

import (
    "context"
    "encoding/json"
    "net/http"
)

type Config struct {
    BaseURL string
    Model   string
}

type Provider struct {
    config Config
    client *http.Client
}

func (p *Provider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
    // 调用 TEI API
    // POST /embed
}

func (p *Provider) Dimension() int {
    // 根据模型返回维度
    // bge-small-zh-v1.5: 512
    // bge-base-zh-v1.5: 768
}
```

### 4.2 PgvectorProvider Implementation

```go
// apps/server/internal/platform/knowledgeprovider/pgvector/provider.go
package pgvector

import (
    "context"
    "fmt"

    "servify/apps/server/internal/platform/embedding"
    "servify/apps/server/internal/platform/knowledgeprovider"
    "gorm.io/gorm"
    "github.com/pgvector/pgvector-go"
)

type Config struct {
    Search          SearchConfig
    Indexing        IndexingConfig
}

type SearchConfig struct {
    TopK       int
    Threshold  float64
    Strategy   string  // cosine, euclidean
}

type IndexingConfig struct {
    ChunkSize     int
    ChunkOverlap  int
}

type Provider struct {
    db        *gorm.DB
    embedding embedding.EmbeddingProvider
    config    Config
}

func NewProvider(db *gorm.DB, emb embedding.EmbeddingProvider, cfg Config) *Provider {
    return &Provider{
        db:        db,
        embedding: emb,
        config:    cfg,
    }
}

func (p *Provider) Search(ctx context.Context, req knowledgeprovider.SearchRequest) ([]knowledgeprovider.KnowledgeHit, error) {
    // 1. 将 query 转为向量
    vectors, err := p.embedding.Embed(ctx, []string{req.Query})
    if err != nil {
        return nil, err
    }
    queryVec := vectors[0]

    // 2. 执行向量相似度搜索
    var docs []models.KnowledgeDoc
    query := p.db.WithContext(ctx).
        Table("knowledge_docs").
        Where("embedding IS NOT NULL")

    if req.TenantID != "" {
        query = query.Where("tenant_id = ?", req.TenantID)
    }
    if req.IsPublicOnly {
        query = query.Where("is_public = ?", true)
    }

    // 余弦相似度搜索
    query = query.Order(fmt.Sprintf("embedding <=> '%s'::vector",
        pgvector.ToString(queryVec)))

    if p.config.Search.TopK > 0 {
        query = query.Limit(p.config.Search.TopK)
    }

    if err := query.Find(&docs).Error; err != nil {
        return nil, err
    }

    // 3. 构造返回结果
    hits := make([]knowledgeprovider.KnowledgeHit, len(docs))
    for i, doc := range docs {
        similarity := 1 - pgvector.CosineDistance(doc.Embedding, queryVec)
        if similarity < p.config.Search.Threshold {
            continue
        }
        hits[i] = knowledgeprovider.KnowledgeHit{
            DocumentID: fmt.Sprint(doc.ID),
            Title:      doc.Title,
            Content:    doc.Content,
            Score:      similarity,
            Source:     "pgvector",
        }
    }
    return hits, nil
}

func (p *Provider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
    // 1. 文档分块
    chunks := p.chunkDocument(doc.Content)

    // 2. 为每个 chunk 生成向量
    vectors, err := p.embedding.Embed(ctx, chunks)
    if err != nil {
        return "", err
    }

    // 3. 存储每个 chunk
    for i, chunk := range chunks {
        doc := &models.KnowledgeDoc{
            Title:      doc.Title,
            Content:    chunk,
            Embedding:  pgvector.NewVector(vectors[i]),
            ChunkIndex: i,
            DocChunkID: fmt.Sprintf("%s-chunk-%d", doc.ID, i),
        }
        if err := p.db.WithContext(ctx).Save(doc).Error; err != nil {
            return "", err
        }
    }

    return fmt.Sprintf("%s-chunks-%d", doc.ID, len(chunks)), nil
}

func (p *Provider) chunkDocument(content string) []string {
    // 简单分块策略
    // TODO: 支持更智能的分块（按段落、句子等）
    chunkSize := p.config.Indexing.ChunkSize
    overlap := p.config.Indexing.ChunkOverlap

    var chunks []string
    for i := 0; i < len(content); i += (chunkSize - overlap) {
        end := i + chunkSize
        if end > len(content) {
            end = len(content)
        }
        chunks = append(chunks, content[i:end])
    }
    return chunks
}
```

## 5. Configuration Design

### 5.1 Configuration Structure

```yaml
# config.yml

# 新增：Embedding 配置
embedding:
  provider: "openai"  # openai, tei, xinference

  openai:
    api_key: "${OPENAI_API_KEY}"
    base_url: "https://api.openai.com/v1"
    model: "text-embedding-3-small"  # 1536 维

  tei:
    base_url: "http://localhost:8080"
    model: "BAAI/bge-small-zh-v1.5"  # 512 维

  xinference:
    base_url: "http://localhost:9997"
    model_uid: "bge-small-zh"

# 新增：知识库配置
knowledge:
  provider: "pgvector"  # pgvector (默认), dify, weknora

  pgvector:
    search:
      top_k: 5
      threshold: 0.7
      strategy: "cosine"  # cosine, euclidean
    indexing:
      chunk_size: 500
      chunk_overlap: 50

# 保留原有配置
dify:
  enabled: ${DIFY_ENABLED}
  base_url: "${DIFY_BASE_URL}"
  ...

weknora:
  enabled: ${WEKNORA_ENABLED}
  ...
```

### 5.2 Config Structure Extension

```go
// apps/server/internal/config/config.go

type Config struct {
    Server     ServerConfig      `yaml:"server"`
    EventBus   EventBusConfig    `yaml:"event_bus"`
    Database   DatabaseConfig    `yaml:"database"`
    Redis      RedisConfig       `yaml:"redis"`
    // ... 其他配置

    // 新增
    Embedding  EmbeddingConfig   `yaml:"embedding"`
    Knowledge  KnowledgeConfig   `yaml:"knowledge"`

    // 保留原有
    AI         AIConfig          `yaml:"ai"`
    Dify       DifyConfig        `yaml:"dify"`
    WeKnora    WeKnoraConfig     `yaml:"weknora"`
    // ...
}

type EmbeddingConfig struct {
    Provider  string             `yaml:"provider"`
    OpenAI    OpenAIEmbedConfig  `yaml:"openai"`
    TEI       TEIEmbedConfig     `yaml:"tei"`
    Xinference XinferenceEmbedConfig `yaml:"xinference"`
}

type OpenAIEmbedConfig struct {
    APIKey  string `yaml:"api_key"`
    BaseURL string `yaml:"base_url"`
    Model   string `yaml:"model"`
}

type TEIEmbedConfig struct {
    BaseURL string `yaml:"base_url"`
    Model   string `yaml:"model"`
}

type XinferenceEmbedConfig struct {
    BaseURL  string `yaml:"base_url"`
    ModelUID string `yaml:"model_uid"`
}

type KnowledgeConfig struct {
    Provider  string              `yaml:"provider"`
    Pgvector  PgvectorConfig      `yaml:"pgvector"`
}

type PgvectorConfig struct {
    Search   SearchConfig   `yaml:"search"`
    Indexing IndexingConfig `yaml:"indexing"`
}

type SearchConfig struct {
    TopK      int     `yaml:"top_k"`
    Threshold float64 `yaml:"threshold"`
    Strategy  string  `yaml:"strategy"`
}

type IndexingConfig struct {
    ChunkSize    int `yaml:"chunk_size"`
    ChunkOverlap int `yaml:"chunk_overlap"`
}
```

## 6. File Structure

```
apps/server/internal/
├── platform/
│   ├── embedding/                          # 新增
│   │   ├── provider.go                     # 接口定义
│   │   ├── openai/
│   │   │   ├── provider.go                 # OpenAI 实现
│   │   │   └── provider_test.go
│   │   ├── tei/
│   │   │   ├── provider.go                 # TEI 实现
│   │   │   └── provider_test.go
│   │   ├── xinference/
│   │   │   ├── provider.go                 # Xinference 实现
│   │   │   └── provider_test.go
│   │   └── factory.go                      # 根据 config 创建 provider
│   └── knowledgeprovider/
│       ├── pgvector/                        # 新增
│       │   ├── provider.go                  # 主实现
│       │   ├── provider_test.go
│       │   ├── chunking.go                  # 文档分块
│       │   └── search.go                    # 向量检索
│       ├── dify/                            # 已有
│       └── weknora/                         # 已有
├── models/
│   └── models.go                            # 扩展 KnowledgeDoc
├── config/
│   └── config.go                            # 添加 EmbeddingConfig, KnowledgeConfig
└── modules/knowledge/
    └── application/
        └── service.go                       # 更新以支持新 provider
```

## 7. Implementation Steps

### Phase 1: Embedding Abstraction Layer
- [ ] Define the `EmbeddingProvider` interface
- [ ] Implement `OpenAIProvider`
- [ ] Implement `TEIProvider`
- [ ] Implement `XinferenceProvider` (optional)
- [ ] Add configuration structures
- [ ] Implement the Factory pattern for creating providers
- [ ] Write unit tests

### Phase 2: PgvectorProvider
- [ ] Extend the database model (add the embedding column)
- [ ] Write database migration scripts
- [ ] Implement document chunking logic
- [ ] Implement the `UpsertDocument` method
- [ ] Implement the `Search` method
- [ ] Implement the `DeleteDocument` method
- [ ] Implement the `HealthCheck` method
- [ ] Write integration tests

### Phase 3: Integration and Testing
- [ ] Update the knowledge base service to use the new provider
- [ ] Update runtime wiring
- [ ] Write end-to-end tests
- [ ] Write acceptance scripts
- [ ] Update docs

## 8. Testing Strategy

### 8.1 Unit Tests
- Tests for each EmbeddingProvider implementation
- Document chunking logic tests
- Vector similarity computation tests

### 8.2 Integration Tests
- PgvectorProvider full-flow tests
- Integration tests with the existing knowledge base service

### 8.3 Acceptance Scripts

```bash
# 使用真实 OpenAI 测试
EMBEDDING_PROVIDER=openai ./scripts/test-knowledge-acceptance.sh

# 使用本地 TEI 测试
EMBEDDING_PROVIDER=tei ./scripts/test-knowledge-acceptance.sh
```

## 9. Dependencies

- `github.com/pgvector/pgvector-go` - pgvector Go bindings
- `github.com/lib/pq` - PostgreSQL driver (already present)

## 10. Risks and Mitigation

| Risk | Mitigation |
|------|----------|
| pgvector performance issues | Use an ivfflat index with an appropriate lists parameter |
| Embedding API rate limiting | Implement request batching and caching |
| Inconsistent vector dimensions | Specify dimensions explicitly in config, with auto-detection |
| Suboptimal document chunking strategy | Implement simple chunking first, extensible later |
