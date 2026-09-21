# 外部知识库集成指南：RAGFlow 知识库 provider（ragflowkp）

## 🎯 项目概述

RAGFlow（InfiniFlow 开源 RAG 引擎）是 servify 知识库主推方向之一，定位与 WeKnora 并列为「正统知识库」双路径：**深度文档解析场景走 RAGFlow**（DeepDoc 对复杂 PDF/表格/版式的解析是其强项），**轻量中文生态走 WeKnora**（见 [WEKNORA_INTEGRATION.md](WEKNORA_INTEGRATION.md)）。选型背景与产品全景见 [KNOWLEDGE_BASE_LANDSCAPE.md](KNOWLEDGE_BASE_LANDSCAPE.md)。

driver 已落地并接入选择链最高优先：

```
ragflow → dify → weknora → pgvector 直配
```

- ragflow 启用且健康检查通过时，优先于 dify / weknora 被选中
- 健康检查失败时按链降级（warn 日志），仅当 dify、weknora 均未启用且启动期要求健康 provider 时才立即失败
- dify / weknora 相对顺序不变；行为变更点仅是 ragflow 插入链首

版本锚定：**RAGFlow v0.27.x**（API 面按此锚定，升级前先跑验收脚本回归）。

## 🏗️ 技术架构

### 代码分层

| 层 | 位置 | 职责 |
| --- | --- | --- |
| HTTP client | `apps/server/pkg/ragflow/` | REST 封装、envelope 双层判定、multipart 上传；不 import internal |
| driver | `apps/server/internal/platform/knowledgeprovider/ragflow/` | 契约四操作实现、删旧建新 upsert、检索调参回退 |
| 选择链 | `apps/server/internal/app/server/knowledge_source.go` | `selectKnowledgeSource` 门面，启动级 + 请求级共用 |
| 租户作用域 | `internal/platform/configscope` + `internal/models` + 迁移 000009 | tenant/workspace 覆盖链 |

### RAGFlow API 事实（v0.27.x）

- 认证：`Authorization: Bearer ragflow-xxx`；base `{base_url}/api/v1`
- 统一包裹 `{"code":<int>,"data":...,"message":...}`，成功 `code==0`；HTTP 状态码与业务码**双层判定**
- 检索 `POST /retrieval`：`{question, dataset_ids, page_size, similarity_threshold, knn_top_k}`；响应 `data.chunks[]{content, similarity, document_id, document_keyword, dataset_id}`
- 上传 `POST /datasets/{id}/documents`：multipart `file` 字段，driver 以 `<标题>.txt` 内存构造上传；响应 data 为数组取 `[0]`；**上传后 `run:"UNSTART"` 不自动解析**
- 触发解析 `POST /datasets/{id}/chunks`：`{"document_ids":[...]}`，异步管线，driver 不等待
- 查重 `GET /datasets/{id}/documents?name=<文件名>`：RAGFlow 无幂等 upsert，driver 自己删旧建新
- 删除 `DELETE /datasets/{id}/documents`：**带 JSON body** `{"ids":[...]}`
- 健康检查 `GET /datasets?id={DatasetID}&page=1&page_size=1`：带鉴权验证配置 dataset 存在（同时验证 BaseURL、凭证、dataset 三者）

### 删旧建新 upsert 语义

RAGFlow 上传不覆盖同名文档，driver 的 `UpsertDocument` 依次执行：

1. `ListDocuments(datasetID, "<标题>.txt")` 按文件名查重
2. 命中则 `DeleteDocuments` 删除旧文档
3. `UploadDocument` 上传新内容
4. `ParseDocuments` 触发异步解析后立即返回（不等解析完成；解析失败按上抛错误重试收敛）

### 检索调参回退链

| 参数 | 优先级 | 兜底 |
| --- | --- | --- |
| TopK | 请求级 > `ragflow.search.top_k` | 10 |
| 相似度阈值 | 请求级 > `ragflow.search.score_threshold` | 0.2 |

## 🚀 快速开始

### 1. 部署 RAGFlow

按 [RAGFlow 官方文档](https://ragflow.io)以 Docker Compose 自托管（默认服务端口 9380）。部署后：

1. 登录 Web UI 创建知识库（dataset），记录 dataset ID
2. 在 API 页面生成 API Key（`ragflow-` 前缀）

### 2. 配置 servify

`config.yml` 的 ragflow 块（env 占位符形态，staging/production 模板默认 `enabled: false`）：

```yaml
ragflow:
  enabled: ${RAGFLOW_ENABLED}
  base_url: "${RAGFLOW_BASE_URL}"      # 如 http://ragflow-host:9380
  api_key: "${RAGFLOW_API_KEY}"
  dataset_id: "${RAGFLOW_DATASET_ID}"
  timeout: 30s
  search:
    top_k: 10
    score_threshold: 0.2
```

安全门禁：`ragflow.enabled=true` 且 `api_key` 或 `dataset_id` 为空时**启动即拒绝**（对齐 dify/weknora 的零容忍 gate）。布尔占位符未设 env 时安全落空（`expandEnvVarsInConfig` 机制，与 dify/weknora 同形态）。

### 3. 验证集成

```bash
# 状态确认：knowledge_provider 应为 ragflow
curl -H "Authorization: Bearer <token>" http://127.0.0.1:8080/api/v1/ai/status
```

## 🧪 验收

### mock 模式（全自含，CI 可跑）

```bash
make ragflow-acceptance
```

脚本全自含：构建真实 servify 二进制 → 内嵌 python3 RAGFlow mock（有状态、请求留痕）→ sqlite 自起服务（ragflow 指向 mock）→ 未认证 401 负例 → `/api/v1/ai/status` 断言 `provider=ragflow` → 检索命中 → 上传×2（第二跳验证删旧建新）→ sync → mock 留痕断言。证据落 `scripts/test-results/ragflow-acceptance/`（manifest 入库）。

### real 模式（需真实 RAGFlow 环境）

```bash
RAGFLOW_ACCEPTANCE_MODE=real \
SERVIFY_URL=http://<servify-host>:<port> \
RAGFLOW_URL=http://<ragflow-host>:9380 \
RAGFLOW_API_KEY=ragflow-xxx \
RAGFLOW_DATASET_ID=<dataset-id> \
./scripts/test-ragflow-acceptance.sh
```

real 模式拒绝 localhost/私网 RAGFlow 地址（防把本地 mock 误留成真实运行证据）；通过条件与 weknora real 口径对齐：provider 健康 + 检索命中 + upload/sync 成功。跑完后 `bash scripts/validate-acceptance-manifest.sh <evidence>/manifest.json` 校验证据集。

### 租户 / 工作区作用域

管理端 scoped config 支持 ragflow 段（对齐 dify/weknora 语义）：

- 存储：`tenant_configs.rag_flow_json` / `workspace_configs.rag_flow_json`（迁移 `000009_ragflow_scoped_config`）
- 覆盖顺序：system → tenant → workspace → runtime，字段级合并（`enabled` sticky-true、字符串非空覆盖、数值非零覆盖）
- 请求级构造的 AI 服务会按当前租户/工作区解析后的 ragflow 配置重建选择链

## 🔒 安全配置

- staging / production 模板 `ragflow.enabled: false`，启用需显式改配置并配齐凭证
- `api_key` 属敏感键：模板中只能为 `""` 或 `${RAGFLOW_API_KEY}` 占位（secure-config 模板门禁扫描）
- 生产建议：RAGFlow 与 servify 同私网段，BaseURL 走内网地址；API Key 经环境变量注入，不落配置文件

## 🐛 故障排除

| 现象 | 排查 |
| --- | --- |
| `/ai/status` 报 `knowledge_provider` 非 ragflow | 查 `ragflow.enabled` 是否为 true；查日志 `RagFlow health check failed`——健康检查失败会沿选择链降级 |
| 启动报 `ragflow is enabled but ragflow.api_key is empty` | 安全 gate 生效，补齐 env 或关掉 enabled |
| 上传成功但检索无结果 | RAGFlow 侧解析是异步管线，`POST chunks` 触发后需等 RAGFlow 解析完成；到 RAGFlow Web UI 查文档解析状态 |
| 查重未删除旧文档 | 确认旧文档在 RAGFlow 中的文件名是 `<标题>.txt`（driver 按上传文件名查重） |
| `ragflow api error code=xxx` | 业务码错误，看 message；`ragflow http 4xx` 则是 HTTP 层（多为鉴权/路径问题） |

## 🎯 当前状态与后续方向

- ✅ driver 四操作 + HealthCheck 全部落地（100% 测试覆盖）
- ✅ mock 模式验收全自含入 CI，manifest 入库
- ✅ 租户/工作区作用域与 dify/weknora 全量对齐
- ⬜ real 模式真实 RAGFlow 环境端到端运行证据（等外部环境与凭证，阻塞项同 P1-1）
- ⬜ RAGFlow 版本跟进（v0.27.x 锚定，升级前先 `make ragflow-acceptance` 回归）
- 不做：dataset 名→id 解析（配置直接给 DatasetID，对齐 dify）、`RebuildIndex`（RAGFlow 侧自管解析管线）

## 📚 相关资源

- [RAGFlow 官方文档](https://ragflow.io)
- [KNOWLEDGE_BASE_LANDSCAPE.md](KNOWLEDGE_BASE_LANDSCAPE.md)——选型全景
- [WEKNORA_INTEGRATION.md](WEKNORA_INTEGRATION.md)——双路径另一侧
- [configuration-scopes.md](configuration-scopes.md)——租户/工作区配置覆盖机制
