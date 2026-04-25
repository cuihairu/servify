# 外部知识库集成指南：Dify 优先，WeKnora 兼容

## 🎯 项目概述

本指南说明 Servify 如何把外部知识库接入到 AI 编排链路中。当前推荐路径是 `Dify` 作为默认和优先的知识库 provider，`WeKnora` 保留为兼容与后备 provider，用于已有部署、协议回归和 fallback 验证。
文档名保留 `WEKNORA_INTEGRATION` 主要是为了兼容历史链接；内容语义以通用 knowledge provider 为主。

## 📋 集成计划完成情况

### ✅ 已完成任务

1. **项目路线图更新** ✅
   - 更新了 README.md 中的第二阶段计划
   - 将外部知识库 provider 集成纳入 AI 演进主路径
   - 更新了技术架构图和技术栈说明

2. **技术实施方案设计** ✅
   - `KnowledgeProvider` 抽象与 `QueryOrchestrator`
   - `Dify` provider 适配 (`apps/server/internal/platform/knowledgeprovider/dify/`)
   - `WeKnora` provider 适配 (`apps/server/internal/platform/knowledgeprovider/weknora/`)
   - 降级策略和熔断器机制

3. **开发环境配置** ✅
   - Dify / WeKnora provider 配置模板
   - WeKnora mock 验收配置 (`infra/compose/docker-compose.weknora.yml`)
   - 数据库初始化脚本 (`scripts/init-db.sql`)
   - 环境变量配置模板 (`.env.weknora.example`)
   - 配置文件模板 (`config.weknora.yml`)

4. **兼容路径部署和管理脚本** ✅
   - 一键启动脚本 (`scripts/start-weknora.sh`)
   - 知识库初始化脚本 (`scripts/init-knowledge-base.sh`)
   - 知识库管理脚本 (`scripts/manage-knowledge-base.sh`)

## 🏗️ 技术架构

### 集成架构
```
Servify 智能客服
       ↓
 Query Orchestrator
       ↓
 KnowledgeProvider
   ├─ Dify (primary)
   └─ WeKnora (fallback/compatibility)
```

### 服务部署图
```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Servify   │───▶│    Dify     │
│  (Port 8080)│    │  (primary)  │
└─────────────┘    └─────────────┘
         │
         └────────────▶ WeKnora (fallback / compatibility)
```

## 🚀 快速开始

> 当前项目的默认推荐是优先接入 `Dify`。仓库里的 `infra/compose/docker-compose.weknora.yml` 仍然保留，用于 WeKnora 协议回归和 fallback 验证，不代表 WeKnora 是主路径。

### 1. 环境准备
```bash
# 检查环境
docker --version
docker-compose --version

# 克隆项目（如果还没有）
git clone <your-servify-repo>
cd servify
```

### 2. 配置环境变量
```bash
# 复制环境变量模板
cp .env.weknora.example .env

# 编辑环境变量，至少需要配置：
# - OPENAI_API_KEY: OpenAI API 密钥
# - DIFY_API_KEY / DIFY_DATASET_ID: 推荐主路径
# - WEKNORA_API_KEY: 仅在需要兼容或 fallback 验收时配置
nano .env
```

### 3. 启动服务
```bash
# 一键启动开发环境
./scripts/start-weknora.sh dev

# 等待服务启动完成...
```

### 4. 初始化兼容知识库
```bash
# 创建 WeKnora compatibility 知识库并上传示例文档
./scripts/init-knowledge-base.sh
```

### 5. 验证 compatibility 集成
```bash
# 检查服务健康状态
curl http://localhost:8080/health
curl http://localhost:9000/api/v1/health

# 测试知识库搜索
./scripts/manage-knowledge-base.sh search "远程协助"
```

### 6. 运行 provider 验收脚本
```bash
# Dify 主路径 mock 回归
DIFY_ACCEPTANCE_MODE=mock \
EVIDENCE_DIR=./scripts/test-results/dify-acceptance/mock \
./scripts/test-dify-integration.sh

# Dify 主路径真实环境验收
DIFY_ACCEPTANCE_MODE=real \
SERVIFY_URL=http://localhost:8080 \
DIFY_URL=https://<real-dify-host>/v1 \
DIFY_DATASET_ID=<real-dataset-id> \
EVIDENCE_DIR=./scripts/test-results/dify-acceptance/real \
./scripts/test-dify-integration.sh

# mock / 本地协议回归
WEKNORA_ACCEPTANCE_MODE=mock \
EVIDENCE_DIR=./scripts/test-results/weknora-acceptance/mock \
./scripts/test-weknora-integration.sh

# real / 真实 WeKnora 环境验收（仅用于兼容路径）
WEKNORA_ACCEPTANCE_MODE=real \
SERVIFY_URL=http://localhost:8080 \
WEKNORA_URL=https://<real-weknora-host> \
EVIDENCE_DIR=./scripts/test-results/weknora-acceptance/real \
./scripts/test-weknora-integration.sh
```

脚本会输出最小验收证据：

- Dify 主路径：
  - `summary.txt`
  - `manifest.json`
  - `servify-health.json`
  - `dify-dataset.json`（可用时）
  - `ai-status.json`
  - `ai-query.json`
  - `knowledge-upload.json`
  - `knowledge-sync.json`
  - `ai-metrics.json`
- WeKnora compatibility 路径：
- `summary.txt`
- `manifest.json`
- `servify-health.json`
- `weknora-health.json`（可用时）
- `ai-status.json`
- `ai-query.json`
- `knowledge-upload.json`
- `knowledge-sync.json`
- `knowledge-provider-disable.json`
- `ai-status-after-disable.json`
- `ai-query-after-disable.json`
- `ai-metrics-after-fallback.json`
- `knowledge-provider-enable.json`
- `ai-status-after-enable.json`
- `circuit-breaker-reset.json`

其中 `Dify real` 模式会严格拒绝 `localhost`、`127.0.0.1`、`0.0.0.0`、私网地址和 `.local/.internal` 主机名，避免把本地 mock 或内网临时地址误记为真实主路径证据；同时要求：

- `dify-dataset` 探针成功
- `knowledge provider` 当前激活为 `dify`
- `knowledge_provider_enabled=true` 且 `knowledge_provider_healthy=true`
- `ai-query` 必须成功，且不能只返回 `strategy=fallback`
- `knowledge upload` 和 `knowledge sync` 必须都成功

其中 `WeKnora real` 模式针对兼容路径，不是“尽量通过”，而是严格验收：

- `WEKNORA_URL` 不能是 `localhost`、`127.0.0.1`、`0.0.0.0`、私网地址或 `.local/.internal` 主机名
- 健康检查返回若标识 `service=weknora-mock`，脚本会直接拒绝作为真实证据
- WeKnora 健康检查必须通过
- Servify 必须运行在 `enhanced` 模式
- `knowledge provider` 当前必须为 `weknora`
- `knowledge_provider_enabled=true` 且 `knowledge_provider_healthy=true`
- `disable` 后的 fallback 查询必须成功，证明外部 provider 不可用时降级路径可控
- `knowledge upload` 和 `knowledge sync` 必须都成功

可以通过 `make dify-acceptance`、`make weknora-acceptance` 作为统一入口运行脚本；其中 `knowledge-provider-acceptance` 目前等价于 `make weknora-acceptance`，用于兼容路径回归。

只有满足以上条件，才能把结果回填到 `docs/acceptance-checklist.md` 里，分别作为 Dify 主路径和 WeKnora compatibility 路径的真实运行证据。

`manifest.json` 是机器可读验收索引，包含 provider、mode、关键状态、核心检查结果和实际生成的证据文件列表。真实环境回填时优先以 `manifest.json` 判断是否满足验收门槛，再引用具体响应文件作为人工审阅证据。

真实环境建议追加执行：

```bash
# 先运行对应 acceptance 脚本，确保 real 目录下已生成 manifest.json
make validate-acceptance-manifest MANIFEST=./scripts/test-results/dify-acceptance/real/manifest.json
make validate-acceptance-manifest MANIFEST=./scripts/test-results/weknora-acceptance/real/manifest.json

# 如果需要校验仓库里已纳入 git 的全部证据目录
make check-acceptance-evidence
```

只有 validator 返回成功，才建议把该目录作为“可回填验收清单”的证据集；批量扫描也只会检查已纳入 git 的 manifest，避免把本地临时产物误算成正式证据。

## 🔧 开发指南

### 项目结构
```
servify/
├── docs/
│   └── WEKNORA_INTEGRATION.md     # 本文档
├── scripts/
│   ├── start-weknora.sh           # 启动脚本
│   ├── init-knowledge-base.sh     # 知识库初始化
│   ├── manage-knowledge-base.sh   # 知识库管理
│   └── init-db.sql               # 数据库初始化
├── apps/
│   └── server/
│       ├── pkg/weknora/           # WeKnora 客户端
│       ├── client.go
│       └── types.go
│       └── internal/services/
│           └── ai_enhanced.go     # 增强的 AI 服务
├── infra/compose/docker-compose.weknora.yml     # WeKnora mock 验收配置
├── config.weknora.yml            # 配置文件模板
└── .env.weknora.example          # 环境变量模板
```

### 核心代码示例

#### Dify provider 使用
```go
provider := difyprovider.NewProvider(
    dify.NewClient(&dify.Config{
        BaseURL: "https://dify.example.com/v1",
        APIKey:  "dify-key",
    }),
    "dataset-id",
    difyprovider.SearchConfig{
        TopK:           5,
        ScoreThreshold: 0.7,
        SearchMethod:   "semantic_search",
    },
)
```

#### WeKnora compatibility 客户端使用
```go
// 创建客户端
client := weknora.NewClient(config, logger)

// 搜索知识库
response, err := client.SearchKnowledge(ctx, &weknora.SearchRequest{
    Query:           "用户问题",
    KnowledgeBaseID: "kb-id",
    Limit:           5,
    Strategy:        "hybrid",
})

// 上传文档
docInfo, err := client.UploadDocument(ctx, kbID, &weknora.Document{
    Type:    "text",
    Title:   "文档标题",
    Content: "文档内容",
    Tags:    []string{"tag1", "tag2"},
})
```

#### AI 服务集成
```go
// 处理用户查询（优先使用 Dify，必要时 fallback 到 WeKnora 或本地知识库）
response, err := aiService.ProcessQuery(ctx, userQuery, sessionID)
```

## 📊 监控和维护

### 健康检查端点
- Servify API: `GET http://localhost:8080/health`
- Dify API: 以 `GET /datasets/:id` 作为 dataset 级健康探针
- WeKnora API: `GET http://localhost:9000/api/v1/health`（兼容路径）
- PostgreSQL: `docker-compose -f infra/compose/docker-compose.yml exec postgres pg_isready`
- Redis: `docker-compose -f infra/compose/docker-compose.yml exec redis redis-cli ping`

### 日志查看
```bash
# 查看所有服务日志
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml logs -f

# 查看特定服务日志
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml logs -f servify
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml logs -f weknora

# 查看应用日志文件
tail -f logs/servify.log
```

### 性能监控
```bash
# 查看服务状态
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml ps

# 查看资源使用情况
docker stats

# 查看知识库统计
./scripts/manage-knowledge-base.sh stats
```

## 🔒 安全配置

### 生产环境配置
1. **修改默认密钥**：
   - 更改 JWT_SECRET
   - 更改 WEKNORA_API_KEY
   - 更改数据库密码

2. **网络安全**：
   - 限制 CORS 允许的域名
   - 配置防火墙规则
   - 使用 HTTPS

3. **数据安全**：
   - 定期备份数据库
   - 加密敏感数据
   - 实施访问控制

### 配置示例
```yaml
# 生产环境配置
security:
  cors:
    allowed_origins: ["https://yourdomain.com"]
  rate_limiting:
    enabled: true
    requests_per_minute: 60
```

## 📈 性能优化

### 推荐配置
1. **数据库优化**：
   - 配置连接池
   - 创建合适的索引
   - 定期 VACUUM

2. **缓存策略**：
   - 启用 Redis 缓存
   - 配置 TTL
   - 实施缓存预热

3. **provider 优化**：
   - 调整 chunk_size
   - 优化 embedding 模型
   - 配置检索策略

## 🐛 故障排除

### 常见问题

#### 1. WeKnora compatibility 服务无法启动
```bash
# 检查端口占用
lsof -i :9000

# 检查配置文件
docker-compose config

# 查看详细错误日志
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml logs weknora
```

#### 2. 数据库连接失败
```bash
# 检查数据库状态
docker-compose -f infra/compose/docker-compose.yml exec postgres pg_isready -U postgres

# 检查网络连接
docker network ls
docker network inspect servify_servify_network
```

#### 3. 知识库搜索无结果
```bash
# 检查文档是否上传成功
./scripts/manage-knowledge-base.sh list

# 检查索引状态
curl -H "X-API-Key: default-api-key" \
     http://localhost:9000/api/v1/knowledge/default-kb
```

## 🔄 更新和维护

### 版本更新
```bash
# 停止服务
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml down

# 更新镜像
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml pull

# 重新启动
./scripts/start-weknora.sh
```

### 数据备份
```bash
# 备份 PostgreSQL
docker-compose -f infra/compose/docker-compose.yml exec postgres pg_dump -U postgres servify > backup.sql

# 备份 WeKnora compatibility 数据
docker cp servify_weknora:/app/data ./backup/weknora_data
```

## 📚 相关资源

### 官方文档
- [WeKnora GitHub](https://github.com/Tencent/WeKnora)
- [WeKnora API 文档](https://github.com/Tencent/WeKnora/blob/main/docs/API.md)
- [pgvector 文档](https://github.com/pgvector/pgvector)

### 社区资源
- [WeKnora Issues](https://github.com/Tencent/WeKnora/issues)
- [PostgreSQL 中文社区](https://www.postgresql.org/community/)

## 🎯 当前状态与后续方向

当前仓库里的外部知识库集成已经完成了角色调整：

- `Dify` 是当前推荐和优先的 `KnowledgeProvider`
- `WeKnora` 不再作为系统内核能力存在，而是 `KnowledgeProvider` 的一个兼容实现
- AI 主流程已经迁移到 `QueryOrchestrator`，只依赖统一检索抽象
- 标准模式和增强模式都可以在不改 handler 协议的前提下切换到编排式实现
- 后续如果切换到其他知识库，只需要新增 provider adapter，不需要重写 AI 主流程

更细的实施任务已经迁移到 `docs/implementation/02-ai-and-knowledge.md` 与 `docs/implementation/04-sdk-and-channel-adapters.md`，当前两份 backlog 都已清零。

后续增量工作不再单独挂在 WeKnora 文档里，而是归到下面几个长期方向：

1. 新增更多 `KnowledgeProvider` 实现，例如 pgvector、Milvus、Elasticsearch 或自研检索服务
2. 补齐文档上传、批量索引、重建索引等管理能力的统一接口
3. 把监控、缓存、故障恢复、安全策略沉到平台层，而不是绑定到某一个知识库实现
4. 让 Web/API/App SDK 统一消费稳定的 AI/knowledge contract，而不是感知具体 provider

## 💬 支持和反馈

如有问题或建议，请：
1. 查看本文档的故障排除部分
2. 检查 `Dify` 或 WeKnora compatibility 对应 provider 文档
3. 在项目 Issues 中提交问题
4. 联系开发团队

---

当前结论：`Dify` 应作为默认知识库 provider 使用；`WeKnora` 可以继续作为兼容或回退适配器保留，但系统架构已经不再依赖单一 provider。
