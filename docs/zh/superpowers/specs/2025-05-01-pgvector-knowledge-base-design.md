# 基于 pgvector 的自建知识库设计文档

**创建日期：** 2025-05-01
**状态：** 已被取代（2026-10-10 注）——pgvector 自建 provider 与 openai/tei embedding 验收路径已由 local 自含知识引擎（`scripts/test-local-knowledge-acceptance.sh`，`make local-knowledge-acceptance`）及各 provider 验收脚本（ragflow/dify/weknora/pgvector）取代；`test-knowledge-acceptance.sh` 已删除，保留本文档作历史记录。
**作者：** AI Assistant

## 1. 概述

### 1.1 背景

Servify 当前支持 Dify 和 WeKnora 作为外部知识库 provider。为了支持企业内网私有部署场景，需要实现一个基于 pgvector 的自建知识库方案，作为默认推荐选项。

### 1.2 目标

- 实现 Embedding 服务抽象层，支持多种 embedding provider
- 实现 PgvectorProvider 作为默认知识库 provider
- 保留 Dify/WeKnora 作为可选 provider
- 支持云部署（OpenAI）和内网部署（TEI/Xinference）两种模式

### 1.3 非目标

- 实现复杂的文档解析（PDF、Word 等）- 暂支持纯文本
- 实现本地模型推理 - 通过外部服务（TEI/Xinference）实现
- 替换现有的 Dify/WeKnora provider - 保留作为可选项

## 2. 架构设计

### 2.1 整体架构

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

### 2.2 核心接口

#### EmbeddingProvider 接口

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

#### Provider 实现

| Provider | 文件位置 | 向量维度 | 用途 |
|----------|----------|----------|------|
| `OpenAIProvider` | `embedding/openai/provider.go` | 1536 | 云部署默认 |
| `TEIProvider` | `embedding/tei/provider.go` | 512/768 | 内网部署推荐 |
| `XinferenceProvider` | `embedding/xinference/provider.go` | 可变 | 内网备选 |

## 3. 数据库设计

### 3.1 表结构变更

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

### 3.2 模型扩展

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

## 4. 组件设计

### 4.1 EmbeddingProvider 实现

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

### 4.2 PgvectorProvider 实现

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

## 5. 配置设计

### 5.1 配置结构

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

### 5.2 Config 结构扩展

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

## 6. 文件结构

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

## 7. 实施步骤

### Phase 1: Embedding 抽象层
- [ ] 定义 `EmbeddingProvider` 接口
- [ ] 实现 `OpenAIProvider`
- [ ] 实现 `TEIProvider`
- [ ] 实现 `XinferenceProvider` (可选)
- [ ] 添加配置结构
- [ ] 实现 Factory 模式创建 provider
- [ ] 编写单元测试

### Phase 2: PgvectorProvider
- [ ] 扩展数据库模型（添加 embedding 列）
- [ ] 编写数据库迁移脚本
- [ ] 实现文档分块逻辑
- [ ] 实现 `UpsertDocument` 方法
- [ ] 实现 `Search` 方法
- [ ] 实现 `DeleteDocument` 方法
- [ ] 实现 `HealthCheck` 方法
- [ ] 编写集成测试

### Phase 3: 集成与测试
- [ ] 更新知识库 service 以使用新 provider
- [ ] 更新 runtime wiring
- [ ] 编写端到端测试
- [ ] 编写验收脚本
- [ ] 更新文档

## 8. 测试策略

### 8.1 单元测试
- EmbeddingProvider 各实现的测试
- 文档分块逻辑测试
- 向量相似度计算测试

### 8.2 集成测试
- PgvectorProvider 完整流程测试
- 与现有知识库 service 集成测试

### 8.3 验收脚本
```bash
# 使用真实 OpenAI 测试
EMBEDDING_PROVIDER=openai ./scripts/test-knowledge-acceptance.sh

# 使用本地 TEI 测试
EMBEDDING_PROVIDER=tei ./scripts/test-knowledge-acceptance.sh
```

## 9. 依赖项

- `github.com/pgvector/pgvector-go` - pgvector Go 绑定
- `github.com/lib/pq` - PostgreSQL 驱动（已有）

## 10. 风险与缓解

| 风险 | 缓解措施 |
|------|----------|
| pgvector 性能问题 | 使用 ivfflat 索引，设置合适的 lists 参数 |
| Embedding API 限流 | 实现请求批处理和缓存 |
| 向量维度不一致 | 在配置中明确指定维度，自动检测 |
| 文档分块策略不优 | 先实现简单分块，后续可扩展 |
