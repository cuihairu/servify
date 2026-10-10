# Servify 知识库使用指南

本指南介绍如何使用 Servify 的自建知识库功能。知识库功能基于 pgvector 实现，支持将文档内容转换为向量并进行语义搜索。

## 快速开始

### 1. 前置条件

- PostgreSQL 数据库已安装并启用 pgvector 扩展
- 已配置 Embedding Provider（见下方）
- 已运行数据库迁移脚本

### 2. 启用知识库

在配置文件 `config.yml`（仓根）中添加以下配置：

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
      strategy: cosine    # 搜索策略: cosine（默认）, euclidean
    indexing:
      chunk_size: 1000       # 文档分块大小（字符数）
      chunk_overlap: 200     # 分块重叠大小
```

### 3. 运行数据库迁移

```bash
make migrate
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

- `cosine`: 余弦相似度，适合大多数文本搜索场景（默认值）
- `euclidean`: 欧几里得距离，适合需要精确距离计算的场景

> 仅以上两个合法值：pgvector 检索按 strategy 分派 SQL 排序，其余取值
> （如历史文档出现过的 `hybrid`）会在查询期报
> `unsupported search strategy`。

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

## 来源登记与版本（V1.0 收敛 B3-1a）

文档可挂接「知识来源」（`knowledge_sources`：markdown / website / pdf / faq / api 元数据登记），并带版本号（标题或内容变更自增，纯元数据不动）。

### 登记来源 / 挂接到文档

```bash
# 登记来源
curl -X POST http://localhost:8080/api/knowledge-docs/sources \
  -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN" \
  -d '{"name": "官方帮助中心", "type": "website", "description": "帮助中心爬取"}'

# 创建/更新文档时挂接（source_id=0 表示未归属来源）
curl -X POST http://localhost:8080/api/knowledge-docs \
  -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN" \
  -d '{"title": "退款政策", "content": "...", "source_id": 1}'
```

删除来源时若仍被文档引用会被拒绝（引用守卫）。

### 索引任务

索引任务落 `knowledge_index_jobs` 并关联执行时文档版本，状态与错误可见、失败可重试：

```bash
# 重建索引（排队并同步执行一次）
curl -X POST http://localhost:8080/api/knowledge-docs/42/index-jobs \
  -H "Authorization: Bearer $TOKEN"

# 查看文档的索引任务（状态/版本/错误）
curl http://localhost:8080/api/knowledge-docs/42/index-jobs?limit=20 \
  -H "Authorization: Bearer $TOKEN"

# 重试失败任务（done 任务重跑 = 按当前版本重建）
curl -X POST http://localhost:8080/api/knowledge-docs/index-jobs/<job_id>/retry \
  -H "Authorization: Bearer $TOKEN"
```

## 检索分析与反馈回传（V1.0 收敛 B3-1b）

AI 首答旁路记录到 `ai_answers`（query/answer/confidence/strategy/来源快照），反馈落 `answer_feedbacks`；记录失败静默不阻塞作答。

### 反馈端点

```bash
curl -X POST http://localhost:8080/api/v1/ai/feedback \
  -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN" \
  -d '{"answer_id": 123, "helpful": true, "comment": "很有用"}'
```

- 认证即可调用（坐席与访客两面共用）；访客（guest token）强制会话绑定——token 的 `sid` 必须与答案的会话一致；
- 来源快照只落 document_id/title/score，不落内容全文。

### 检索分析读口

```bash
curl "http://localhost:8080/api/v1/ai/retrieval-analytics?days=7&limit=10" \
  -H "Authorization: Bearer $TOKEN"
```

返回窗口内（默认 7 天，上限 90 天）的 top 问答、零命中问题（知识缺口信号）、低置信问题（置信 < 0.65）与反馈计数。

## 管理面板

管理端（apps/admin）知识库菜单提供完整管理面：

- **文档管理**：文档 CRUD、来源与版本列、挂源、重建索引、任务抽屉（失败重试）；
- **来源与索引**：`knowledge_sources` 登记（类型枚举校验，删除引用守卫）；
- **检索分析**：窗口统计（引用命中率/低置信/平均置信/反馈计数）+ 三个榜单。

访客侧 widget 在 AI 终帧带 `answer_id` 与 `sources` 时渲染引用行（`📄 标题 · relevance 0.91`）与"Was this helpful?"反馈条（需嵌入方注入 guest token）。

## 验收测试

本地自含知识引擎（`knowledge.provider=local` + `embedding.provider=local` +
`ai.provider=local` 三 local，零外部依赖、零网络出站）的验收走
`test-local-knowledge-acceptance.sh`：

```bash
make local-knowledge-acceptance
```

全链路真实留证：build → 起服 ready → 401 负例 → 首用户自动 admin → `ai/status`
报 local → enable→healthy → 上传三篇主题文档（真实分块+确定性哈希嵌入落库）→
sync 留档 → sqlite3 数据证据（行数/chunking/维度 256）→ 语义检索正例命中与
无关 query 过滤 → 回答逐字来自知识原文（extractive 断言）。

清单行见 [acceptance-checklist.md](acceptance-checklist.md) §3「AI 与外部知识库」
（本地自含知识引擎行，状态：通过）；manifest 与证据在
`scripts/test-results/local-knowledge/`。

历史脚本 `test-knowledge-acceptance.sh`（openai/tei embedding provider、
localhost:8080 外部服务）已删除，由本脚本与 ragflow/dify/weknora/pgvector 各
provider 验收脚本取代。

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

知识库功能由以下组件构成（V1.0 收敛后为模块化结构，`apps/server/` 下）：

- **knowledge 模块**（domain → application → infra/delivery 分层）：
  - `internal/modules/knowledge/domain`: 实体（Document / Source / IndexJob）
  - `internal/modules/knowledge/application`: 业务规则（版本语义、来源枚举校验、引用守卫、索引任务）
  - `internal/modules/knowledge/infra`: GORM 仓储
  - `internal/modules/knowledge/delivery`: HTTP 契约适配
- **ai 模块**（反馈回传与检索分析）：
  - `internal/modules/ai/application/feedback.go`: 反馈校验（访客会话绑定）与检索分析聚合
  - `internal/modules/ai/infra/answer_repository.go`: ai_answers / answer_feedbacks 仓储
  - `internal/modules/ai/delivery/`: 记录路径（REST/WS 旁路观测）与反馈/分析端点
- **EmbeddingProvider**: 将文本转换为向量
  - `internal/platform/embedding/`: 接口与 openai / tei / xinference 实现
- **KnowledgeProvider**: 向量存储和检索
  - `internal/platform/knowledgeprovider/pgvector`: pgvector 实现

## 相关文档

- [配置文档](./docs/configuration-scopes.md)
- [API 文档](./docs/generated/api/)
- [V1.0 收敛改造计划书](./docs/v1-convergence-plan.md)（§5.3 反馈回传 / §8 Knowledge 产品化）
