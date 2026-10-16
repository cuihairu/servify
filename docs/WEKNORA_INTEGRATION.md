# External Knowledge Base Integration Guide: Knowledge Provider Path (Dify dataset compatible + WeKnora)

## Project Overview

This guide explains how Servify connects an external knowledge base into the AI orchestration chain. The current code selection chain uses `RagFlow` as the primary provider, `Dify` as the secondary compatibility path, and `WeKnora` for protocol regression and fallback validation; when no external source is enabled, the system falls back to running without a knowledge source (`knowledge.provider` defaults to empty, and the selection chain returning "no source" means it starts in the no-knowledge-source form). **pgvector / local are not automatic fallback paths**: both require explicit `provider: "pgvector"` / `provider: "local"` declarations in the config file to be enabled (`apps/server/internal/config/config.go:1288-1289`). The selection chain implementation is in `apps/server/internal/app/server/knowledge_source.go`; see [Knowledge Base Landscape](/KNOWLEDGE_BASE_LANDSCAPE) for the full selection overview.

> **Positioning clarification (2026-09)**: Dify is strictly speaking an LLM application orchestration platform; its dataset knowledge base is only an ancillary capability and is not suitable as the long-term flagship direction for knowledge bases. The mainstream knowledge base selection and adaptation plan (RAGFlow chain head, pgvector direct config as the default path, Dify positioning demoted) is in [Knowledge Base Landscape](/KNOWLEDGE_BASE_LANDSCAPE).
The document name is kept as `WEKNORA_INTEGRATION` mainly for compatibility with historical links; the content semantics center on the general knowledge provider.

## Integration Plan Completion Status

### Completed Tasks

1. **Project roadmap update**
   - Updated the phase 2 plan in README.md
   - Incorporated external knowledge provider integration into the main AI evolution path
   - Updated the technical architecture diagram and tech stack description

2. **Technical implementation plan design**
   - `KnowledgeProvider` abstraction and `QueryOrchestrator`
   - `Dify` provider adapter (`apps/server/internal/platform/knowledgeprovider/dify/`)
   - `WeKnora` provider adapter (`apps/server/internal/platform/knowledgeprovider/weknora/`)
   - Degradation strategy and circuit breaker mechanism

3. **Development environment configuration**
   - Dify / WeKnora provider config templates
   - WeKnora mock acceptance config (`infra/compose/docker-compose.weknora.yml`)
   - Database initialization script (`scripts/init-db.sql`)
   - Environment variable config template (`.env.weknora.example`)
   - Config file template (`config.weknora.yml`)

4. **Compatibility path deployment and management scripts**
   - One-click startup script (`scripts/start-weknora.sh`)
   - Knowledge base initialization script (`scripts/init-knowledge-base.sh`; on completion it generates the `scripts/manage-knowledge-base.sh` helper script for search/list/stats)

## Technical Architecture

### Integration Architecture
```
Servify 智能客服
       ↓
 Query Orchestrator
       ↓
 KnowledgeProvider（外部选择链：健康逐级降级，终点为无知识源运行）
   ├─ RagFlow (primary)
   ├─ Dify (secondary / compatibility)
   └─ WeKnora (compatibility)
（provider=pgvector / local 时配置直通，优先于外部选择链）
```

### Service Deployment Diagram
```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Servify   │───▶│   RagFlow   │
│  (Port 8080)│    │  (primary)  │
└─────────────┘    └─────────────┘
         │
         ├────────────▶ Dify (secondary / compatibility)
         │
         └────────────▶ WeKnora (compatibility)
```

## Quick Start

> The current project default recommendation is to integrate `RagFlow` (dedicated external knowledge base) or `pgvector` (zero external dependency, in-process direct config); `Dify` is kept as a compatibility path. The repo's `infra/compose/docker-compose.weknora.yml` is still retained, for WeKnora protocol regression and fallback validation; it does not mean WeKnora is the main path.

### 1. Environment Preparation
```bash
# 检查环境
docker --version
docker-compose --version

# 克隆项目（如果还没有）
git clone <your-servify-repo>
cd servify
```

### 2. Configure Environment Variables
```bash
# 复制环境变量模板
cp .env.weknora.example .env

# 编辑环境变量，至少需要配置：
# - OPENAI_API_KEY: OpenAI API 密钥
# - RagFlow（链首）走 config.yml 的 ragflow 段（enabled/base_url/api_key/dataset_id）；
#   或 knowledge.provider=pgvector 直配（零外部依赖）
# - DIFY_API_KEY / DIFY_DATASET_ID: Dify 兼容路径（次选）
# - WEKNORA_API_KEY: 仅在需要兼容或 fallback 验收时配置
nano .env
```

### 3. Start Services
```bash
# 一键启动开发环境
./scripts/start-weknora.sh dev

# 等待服务启动完成...
```

### 4. Initialize Compatibility Knowledge Base
```bash
# 创建 WeKnora compatibility 知识库并上传示例文档
./scripts/init-knowledge-base.sh
```

### 5. Verify Compatibility Integration
```bash
# 检查服务健康状态
curl http://localhost:8080/health
curl http://localhost:9000/api/v1/health

# 测试知识库搜索（manage-knowledge-base.sh 由 init-knowledge-base.sh 初始化时生成）
./scripts/manage-knowledge-base.sh search "远程协助"
```

### 6. Run Provider Acceptance Scripts
```bash
# Dify 兼容路径 mock 回归
DIFY_ACCEPTANCE_MODE=mock \
EVIDENCE_DIR=./scripts/test-results/dify-acceptance/mock \
./scripts/test-dify-integration.sh

# Dify 兼容路径真实环境验收
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

The scripts emit minimal acceptance evidence:

- Dify compatibility path:
  - `summary.txt`
  - `manifest.json`
  - `servify-health.json`
  - `dify-dataset.json` (when available)
  - `ai-status.json`
  - `ai-query.json`
  - `knowledge-upload.json`
  - `knowledge-sync.json`
  - `ai-metrics.json`
- WeKnora compatibility path:
- `summary.txt`
- `manifest.json`
- `servify-health.json`
- `weknora-health.json` (when available)
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

Among these, `Dify real` mode strictly rejects `localhost`, `127.0.0.1`, `0.0.0.0`, private-network addresses, and `.local/.internal` hostnames, to avoid recording local mock or internal temporary addresses as real-environment evidence; it also requires:

- `dify-dataset` probe succeeds
- `knowledge provider` is currently active as `dify`
- `knowledge_provider_enabled=true` and `knowledge_provider_healthy=true`
- `ai-query` must succeed, and must not return only `strategy=fallback`
- `knowledge upload` and `knowledge sync` must both succeed

Among these, `WeKnora real` mode is for the compatibility path and uses strict criteria for acceptance:

- `WEKNORA_URL` cannot be `localhost`, `127.0.0.1`, `0.0.0.0`, a private-network address, or a `.local/.internal` hostname
- If the health check response identifies `service=weknora-mock`, the script directly rejects it as real evidence
- The WeKnora health check must pass
- Servify must be running in `enhanced` mode
- `knowledge provider` must currently be `weknora`
- `knowledge_provider_enabled=true` and `knowledge_provider_healthy=true`
- The fallback query after `disable` must succeed, proving that the degradation path is controllable when the external provider is unavailable
- `knowledge upload` and `knowledge sync` must both succeed

The scripts can be run through the unified entry points `make dify-acceptance` and `make weknora-acceptance`; `knowledge-provider-acceptance` currently is equivalent to `make weknora-acceptance`, used for compatibility path regression.

Only when the above conditions are met can the results be backfilled into `docs/acceptance-checklist.md`, as real runtime evidence for the Dify compatibility path and the WeKnora compatibility path respectively.

`manifest.json` is the machine-readable acceptance index, containing provider, mode, key statuses, core check results, and the list of evidence files actually generated. When backfilling a real environment, prefer `manifest.json` to judge whether the acceptance threshold is met, then cite specific response files as human-review evidence.

Recommended additions for a real environment:

```bash
# 先运行对应 acceptance 脚本，确保 real 目录下已生成 manifest.json
make validate-acceptance-manifest MANIFEST=./scripts/test-results/dify-acceptance/real/manifest.json
make validate-acceptance-manifest MANIFEST=./scripts/test-results/weknora-acceptance/real/manifest.json

# 如果需要校验仓库里已纳入 git 的全部证据目录
make check-acceptance-evidence
```

Only when the validator returns success should the directory be treated as an evidence set eligible for backfilling into the acceptance checklist; the batch scan also only checks manifests already in git, to avoid counting local temporary artifacts as official evidence.

## Development Guide

### Project Structure
```
servify/
├── docs/
│   └── WEKNORA_INTEGRATION.md     # 本文档
├── scripts/
│   ├── start-weknora.sh           # 启动脚本
│   ├── init-knowledge-base.sh     # 知识库初始化
│   ├── test-weknora-integration.sh # 集成验收（mock/real 两模式）
│   └── init-db.sql                # 数据库初始化
├── apps/server/
│   ├── pkg/weknora/               # WeKnora HTTP 客户端（client.go / types.go）
│   └── internal/platform/knowledgeprovider/
│       ├── weknora/               # WeKnora provider（知识库 provider 接口实现）
│       ├── dify/ ragflow/ local/ memory/ mock/ pgvector/   # 其他 provider
│       └── provider.go types.go   # provider 契约
├── infra/compose/
│   ├── docker-compose.weknora.yml # WeKnora 叠加编排（含 weknora-mock 服务）
│   └── weknora-mock/              # mock 实现（Dockerfile / main.go / main_test.go）
├── config.weknora.yml             # 配置文件模板
└── .env.weknora.example           # 环境变量模板
```

### Core Code Examples

#### Dify provider usage
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

#### WeKnora compatibility client usage
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

#### AI service integration
```go
// 处理用户查询（知识源按 provider 直通 + ragflow→dify→weknora 选择链逐级降级，终点为无知识源运行）
response, err := aiService.ProcessQuery(ctx, userQuery, sessionID)
```

## Monitoring and Maintenance

### Health Check Endpoints
- Servify API: `GET http://localhost:8080/health`
- Dify API: uses `GET /datasets/:id` as the dataset-level health probe
- WeKnora API: `GET http://localhost:9000/api/v1/health` (compatibility path)
- PostgreSQL: `docker-compose -f infra/compose/docker-compose.yml exec postgres pg_isready`
- Redis: `docker-compose -f infra/compose/docker-compose.yml exec redis redis-cli ping`

### Log Viewing
```bash
# 查看所有服务日志
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml logs -f

# 查看特定服务日志
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml logs -f servify
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml logs -f weknora

# 查看应用日志文件
tail -f logs/servify.log
```

### Performance Monitoring
```bash
# 查看服务状态
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml ps

# 查看资源使用情况
docker stats

# 查看知识库统计（脚本由 init-knowledge-base.sh 初始化时生成）
./scripts/manage-knowledge-base.sh stats
```

## Security Configuration

### Production Environment Configuration
1. **Change default secrets**:
   - Change JWT_SECRET
   - Change WEKNORA_API_KEY
   - Change database password

2. **Network security**:
   - Restrict the CORS allowed origins
   - Configure firewall rules
   - Use HTTPS

3. **Data security**:
   - Back up the database regularly
   - Encrypt sensitive data
   - Implement access control

### Configuration Example
```yaml
# 生产环境配置
security:
  cors:
    allowed_origins: ["https://yourdomain.com"]
  rate_limiting:
    enabled: true
    requests_per_minute: 60
```

## Performance Optimization

### Recommended Configuration
1. **Database optimization**:
   - Configure the connection pool
   - Create appropriate indexes
   - VACUUM regularly

2. **Cache strategy**:
   - Enable Redis caching
   - Configure TTL
   - Implement cache warming

3. **Provider optimization**:
   - Tune chunk_size
   - Optimize the embedding model
   - Configure the retrieval strategy

## Troubleshooting

### Common Problems

#### 1. WeKnora compatibility service fails to start
```bash
# 检查端口占用
lsof -i :9000

# 检查配置文件
docker-compose config

# 查看详细错误日志
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml logs weknora
```

#### 2. Database connection failure
```bash
# 检查数据库状态
docker-compose -f infra/compose/docker-compose.yml exec postgres pg_isready -U postgres

# 检查网络连接
docker network ls
docker network inspect servify-network
```

#### 3. Knowledge base search returns no results
```bash
# 检查文档是否上传成功（脚本由 init-knowledge-base.sh 初始化时生成）
./scripts/manage-knowledge-base.sh list

# 检查索引状态
curl -H "X-API-Key: default-api-key" \
     http://localhost:9000/api/v1/knowledge/default-kb
```

## Updates and Maintenance

### Version Updates
```bash
# 停止服务
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml down

# 更新镜像
docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml pull

# 重新启动
./scripts/start-weknora.sh
```

### Data Backup
```bash
# 备份 PostgreSQL
docker-compose -f infra/compose/docker-compose.yml exec postgres pg_dump -U postgres servify > backup.sql

# 备份 WeKnora compatibility 数据
docker cp servify_weknora:/app/data ./backup/weknora_data
```

## Related Resources

### Official Documentation
- [WeKnora GitHub](https://github.com/Tencent/WeKnora)
- [WeKnora API Documentation](https://github.com/Tencent/WeKnora/blob/main/docs/API.md)
- [pgvector Documentation](https://github.com/pgvector/pgvector)

### Community Resources
- [WeKnora Issues](https://github.com/Tencent/WeKnora/issues)
- [PostgreSQL Chinese Community](https://www.postgresql.org/community/)

## Current Status and Future Direction

The external knowledge base integration in the current repo has completed a role adjustment:

- The external knowledge selection chain uses `RagFlow` as the primary, `Dify` as the secondary compatibility path (when provider=pgvector/local, config passes through directly, taking priority over the external chain)
- `WeKnora` no longer exists as a system core capability, but as one compatibility implementation of `KnowledgeProvider`
- The AI main flow has migrated to `QueryOrchestrator`, depending only on the unified retrieval abstraction
- Both standard mode and enhanced mode can switch to the orchestration implementation without changing the handler protocol
- If switching to another knowledge base later, only a new provider adapter needs to be added; the AI main flow does not need to be rewritten

More detailed implementation tasks have migrated to `docs/implementation/02-ai-and-knowledge.md` and `docs/implementation/04-sdk-and-channel-adapters.md`; both backlogs are now cleared.

Future incremental work no longer hangs off the WeKnora document alone, but is grouped into the following long-term directions:

1. Add more `KnowledgeProvider` implementations, e.g. Milvus, Elasticsearch, or a self-developed retrieval service (pgvector is already implemented: `internal/platform/knowledgeprovider/pgvector/`, via `knowledge.provider=pgvector` direct config)
2. Complete the unified interface for document upload, batch indexing, index rebuild, and other management capabilities
3. Sink monitoring, caching, fault recovery, and security policies to the platform layer, rather than binding them to a single knowledge base implementation
4. Let the Web/API/App SDK consume a stable AI/knowledge contract uniformly, rather than being aware of the specific provider

## Support and Feedback

If you have questions or suggestions:
1. Check the troubleshooting section of this document
2. Check the corresponding provider docs for `Dify` or WeKnora compatibility
3. Submit issues in the project Issues
4. Contact the development team

---

Current conclusion: the external knowledge base defaults to recommending `RagFlow` (dedicated knowledge base, selection chain head) or `pgvector` (zero external dependency direct config); `Dify` is kept as the secondary compatibility path, and `WeKnora` can continue to be kept as a compatibility or fallback adapter, but the system architecture no longer depends on a single provider.
