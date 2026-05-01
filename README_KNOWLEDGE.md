# Servify 知识库使用指南

本指南介绍如何使用 Servify 的自建知识库功能。知识库功能基于 pgvector 实现，支持将文档内容转换为向量并进行语义搜索。

## 快速开始

### 1. 前置条件

- PostgreSQL 数据库已安装并启用 pgvector 扩展
- 已配置 Embedding Provider（见下方）
- 已运行数据库迁移脚本

### 2. 启用知识库

在配置文件 `config/config.yaml` 中添加以下配置：

```yaml
embedding:
  provider: openai  # 可选: openai, tei, xinference
  openai:
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1
    model: text-embedding-3-small

knowledge:
  provider: pgvector
  pgvector:
    search:
      top_k: 5           # 返回结果数量
      threshold: 0.7     # 相似度阈值
      strategy: hybrid    # 搜索策略: cosine, euclidean, hybrid
    indexing:
      chunk_size: 1000       # 文档分块大小（字符数）
      chunk_overlap: 200     # 分块重叠大小
```

### 3. 运行数据库迁移

```bash
cd apps/server
./bin/migrate up
```

## Embedding Provider 选择

Servify 支持三种 Embedding Provider，可根据需求选择：

### OpenAI

**适用场景**: 生产环境，需要高质量的向量表示

```yaml
embedding:
  provider: openai
  openai:
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1  # 可选，用于兼容 API
    model: text-embedding-3-small        # 或 text-embedding-3-large
```

**模型说明**:
- `text-embedding-3-small`: 1536 维，性价比高
- `text-embedding-3-large`: 3072 维，精度更高

### TEI (Text Embeddings Inference)

**适用场景**: 本地部署，私有化环境，成本控制

```yaml
embedding:
  provider: tei
  tei:
    base_url: http://localhost:8080
    model: bge-m3  # 模型名称，取决于 TEI 部署的模型
```

**安装 TEI**:

```bash
docker run -p 8080:80 \
  -v $PWD/data:/data \
  ghcr.io/huggingface/text-embeddings-inference:cpu-1.5 \
  --model-id BAAI/bge-m3
```

### Xinference

**适用场景**: 需要统一管理多种推理模型

```yaml
embedding:
  provider: xinference
  xinference:
    base_url: http://localhost:9997
    model_uid: embedding-model  # 在 Xinference 中注册的模型 UID
```

## 配置说明

### 搜索策略 (Strategy)

- `cosine`: 余弦相似度，适合大多数文本搜索场景
- `euclidean`: 欧几里得距离，适合需要精确距离计算的场景
- `hybrid`: 混合策略，默认选项

### 分块配置 (Indexing)

- `chunk_size`: 单个文本块的最大字符数
  - 较小值: 搜索更精确，但上下文较少
  - 较大值: 上下文更丰富，但搜索可能不够精确
- `chunk_overlap`: 相邻块之间的重叠字符数，避免语义被截断

### 搜索参数 (Search)

- `top_k`: 返回的最大结果数量
- `threshold`: 相似度阈值（0-1），低于此值的结果将被过滤

## API 使用

### 创建/更新文档

```bash
curl -X POST http://localhost:8080/api/knowledge-docs \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "title": "产品使用指南",
    "content": "这里是文档内容...",
    "category": "product",
    "tags": ["guide", "product"],
    "is_public": true
  }'
```

### 搜索文档

```bash
curl -X POST http://localhost:8080/api/v1/ai/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "如何安装产品？",
    "session_id": "user-session-123"
  }'
```

### 列出文档

```bash
curl http://localhost:8080/api/knowledge-docs?page=1&page_size=10 \
  -H "Authorization: Bearer $TOKEN"
```

## 验收测试

### 运行验收脚本

```bash
# 使用 OpenAI Embedding Provider
SERVIFY_URL=http://localhost:8080 \
EMBEDDING_PROVIDER=openai \
TEST_TOKEN=your-test-token \
./scripts/test-knowledge-acceptance.sh

# 使用 TEI Embedding Provider
SERVIFY_URL=http://localhost:8080 \
EMBEDDING_PROVIDER=tei \
TEST_TOKEN=your-test-token \
./scripts/test-knowledge-acceptance.sh
```

### 验收标准

1. 健康检查通过 (`/health` 返回知识库状态为 healthy)
2. 文档创建成功
3. 文档列表查询正常
4. 语义搜索返回相关结果（相似度分数 > threshold）

### 检查验收证据

验收脚本会在 `./scripts/test-results/knowledge-acceptance/` 目录下生成以下文件：

- `health.json`: 健康检查结果
- `create-doc.json`: 文档创建结果
- `list-docs.json`: 文档列表
- `search-result.json`: 搜索结果
- `manifest.json`: 验收清单

## 故障排查

### pgvector 扩展不可用

```sql
-- 检查扩展是否已安装
SELECT extname, extversion FROM pg_extension WHERE extname = 'vector';

-- 如果未安装，运行
CREATE EXTENSION IF NOT EXISTS vector;
```

### Embedding Provider 连接失败

检查配置中的 `base_url` 和 API 密钥是否正确：

```bash
# OpenAI
curl https://api.openai.com/v1/embeddings \
  -H "Authorization: Bearer $OPENAI_API_KEY"

# TEI
curl http://localhost:8080/embed \
  -X POST -H "Content-Type: application/json" \
  -d '{"inputs":"test"}'

# Xinference
curl http://localhost:9997/v1/embeddings \
  -X POST -H "Content-Type: application/json" \
  -d '{"model":"embedding-model","input":"test"}'
```

### 搜索结果为空

1. 确认数据库中有文档数据
2. 检查 `threshold` 是否设置过高
3. 验证 Embedding Provider 是否正常工作

## 架构说明

知识库功能由以下组件构成：

- **EmbeddingProvider**: 将文本转换为向量
  - `internal/platform/embedding/provider.go`: 接口定义
  - `internal/platform/embedding/openai`: OpenAI 实现
  - `internal/platform/embedding/tei`: TEI 实现
  - `internal/platform/embedding/xinference`: Xinference 实现

- **KnowledgeProvider**: 向量存储和检索
  - `internal/platform/knowledgeprovider/pgvector`: pgvector 实现

- **Service**: 业务逻辑层
  - `internal/service/knowledge.go`: 知识库服务

- **API**: HTTP 接口
  - `internal/app/api/knowledge.go`: 知识库 API 端点

## 相关文档

- [配置文档](./config/README.md)
- [API 文档](./docs/api.md)
- [数据库模型](./apps/server/internal/models/knowledge.go)
