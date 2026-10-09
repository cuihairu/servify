[English](README.md) | [中文](README.zh.md)

<p align="center">
  <img src="./docs/.vitepress/public/icon.png" width="80" alt="Servify Logo">
</p>

<div align="center">

# Servify

**开源智能客服系统** — Web 优先，AI 首答，人工接管，工单全流程

[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![CI](https://img.shields.io/github/actions/workflow/status/cuihairu/servify/ci.yml?branch=main&label=CI)](https://github.com/cuihairu/servify/actions)
[![codecov](https://codecov.io/gh/cuihairu/servify/graph/badge.svg)](https://codecov.io/gh/cuihairu/servify)
[![GitHub Stars](https://img.shields.io/github/stars/cuihairu/servify?style=social)](https://github.com/cuihairu/servify)
[![Website](https://img.shields.io/badge/website-servify.cuihairu.site-6366f1?logo=cloudflare)](https://servify.cuihairu.site/)

</div>

---

Servify 是一个面向企业独立部署的开源智能客服系统。

第一版产品目标先收敛在企业官网、品牌独立站、SaaS 官网和文档站的 Web 智能客服：站点嵌入客服入口，AI 基于知识库首答，复杂问题转人工并沉淀为工单。

它当前先把客服主链路做完整，暂不往平台化多租户走：`Web 接入 -> AI 首答 -> 人工接管 -> 转接协作 -> 工单全流程`。

当问题需要更强的引导和排查时，远程协助仍然是保留中的增强方向；但它不再作为第一版验收的中心能力。

当前仓库已经收敛到 `模块化单体` 架构，重点围绕会话、路由、工单、AI、知识库和后台运营持续演化；多端 SDK、更多渠道和语音能力属于后续扩展边界，而不是当前产品中心。

---

## 当前状态

`v1.0.0` 已于 2026-10-04 发布（见
[发布说明](./docs/release-notes-v1.0.0.md)），V1.0 收敛改造 B0–B4 批次
全部过闸：

- 客服主链路打通：`conversation`（中心聚合）、`routing`、`ticket`
- AI 与知识库产品化：`ai`（首答记录/反馈回传/检索分析）、`knowledge`
  （来源登记/文档版本/索引任务/citation）
- 管理面安全基线：认证、审计、token state revoke、session security
  surface、PII 导出/擦除与保留策略
- 27 个一级模块不增（架构门禁），薄壳模块按「核心 7 模块的子能力」
  叙事收口
- Web 优先、多端预留：Web SDK 已实现，Android/iOS 原生 SDK 已随
  M1–M3 落地，语音与多渠道冻结为扩展边界
- 后续演进：Routing 打分引擎、Ticket 关闭前拦截等（见发布说明
  「演进方向」）

---

## 产品定位

Servify 当前更适合这样理解：

- 一个企业部署一套 Servify
- 访客从 Web 页面发起咨询
- AI 先做首答、澄清和知识召回
- 需要时可进一步升级到协助型处理，但主链路先保证 AI 首答、人工接管和工单全流程
- 坐席随时接管、协作、转接
- 无法即时解决的问题进入工单继续跟进
- 管理员在后台管理坐席、知识库、权限和基础配置

这意味着 `tenant/workspace` 更接近治理和隔离能力，而不是产品主叙事。

## 远程协助当前指什么

在 Servify 当前阶段，远程协助应该被理解为：

- 客户在 Web 会话中遇到需要一步步引导的问题时，客服可以从“解释”升级到“带着完成”
- AI、人工接管、实时交互和工单在同一条连续服务链路上
- 远程协助结束后，客服仍可以继续转接、协作或沉淀工单，而不是把上下文丢到外部系统

当前仓库已经具备这条能力链路的实时基础，包括会话、消息、WebSocket、WebRTC stats / connections、人工接管和后续工单衔接能力；管理端会话页也已经有最小协助入口，但它现在还不是一个“已经交付完整 co-browsing 产品”的承诺。

---

## 仓库结构

```text
.
|-- apps/
|   |-- server/              # Go 服务端
|   |-- admin/               # Admin 管理面板（UmiJS + Ant Design Pro）
|   |-- demo/                # 产品演示站点
|   |-- demo-sdk/            # SDK 预构建产物与示例
|   `-- website/             # 官网静态站点
|-- docs/
|   `-- implementation/      # 分主题实施 backlog
|-- infra/                   # compose、部署辅助
|-- internal/                # 共用内部包
`-- sdk/                     # SDK 工作区（源码）
```

## 常用校验入口

- `make local-check`
- `make security-check CONFIG=./config.yml`
- `make observability-check CONFIG=./config.yml`
- `make release-check CONFIG=./config.yml`

## 根目录职责

### 应包含的内容

| 目录/文件 | 说明 |
|-----------|------|
| `apps/` | 应用入口与可运行表面，包括服务端、管理端、演示站点等 |
| `apps/server/` | Go 服务端（模块化单体架构） |
| `apps/admin/` | Admin 管理面板（UmiJS + Ant Design Pro） |
| `apps/demo/` | 产品演示站点与示例 |
| `apps/demo-sdk/` | SDK 预构建产物（UMD/ESM）与集成示例 |
| `apps/website/` | 官网静态站点 |
| `docs/` | 说明文档、实施 backlog、发布与协作规则 |
| `infra/` | 本地或部署环境相关的 compose、可观测性与辅助配置 |
| `scripts/` | CI、本地开发、生成物与检查脚本 |
| `sdk/` | SDK workspace 源码（TypeScript） |
| `config.yml`、`config.weknora.yml` | 本地运行配置样例；`config.yml` 默认使用 pgvector 自建知识库，`config.weknora.yml` 用于 WeKnora 兼容部署 |
| `config.production.secure.example.yml` | 生产环境安全配置模板 |
| `generated-assets.manifest` | 必须提交的生成物清单 |
| `Makefile`、`build.sh` | 常用构建与开发入口 |

### 不应长期出现的内容

- 本地构建二进制，例如 `server`、`server.exe`
- 运行时输出目录，例如 `uploads/`、`.runtime/`
- 临时调试文件、测试残留、缓存文件

---

## 架构原则

- **业务模块化**：每个模块具备 `domain`、`application`、`infra`、`delivery`
- **平台能力抽象**：认证、事件总线、AI/Knowledge provider、realtime/SIP 独立
- **多端 SDK 预留**：Web 先落地，API/App 预留 contract，不做伪实现
- **语音能力隔离**：通过 `voice` 模块和 SIP adapter 接入，不耦合聊天链路
- **Provider 可替换**：默认 pgvector 自建知识库，Dify 为推荐的外部知识源，WeKnora 为兼容实现之一

---

## 当前重点

当前阶段，Servify 优先做好这些事情：

- 把 Web 接入做成正式产品入口
- 把 AI 协同和人工接管打通
- 把转接、协作和工单全流程收完整
- 把后台运营和安全基线稳定下来
- 把远程协助保留为后续增强方向，而不是拉高 V1 复杂度

推荐先读：

- [v1.0.0 Release Notes](./docs/release-notes-v1.0.0.md)（当前版本）
- [V1 产品收敛](./docs/v1-product-scope.md)
- [Web 嵌入集成指南](./docs/embedding-guide.md)（把客服组件嵌进自有站点）
- [文档站首页](./docs/index.md)（按任务分组：上手/接入/部署运维/安全/AI 知识库）

当前不会把“平台化租户能力”作为产品中心持续扩张，而是先把独立部署客服产品做扎实。

---

## 总体架构

```mermaid
flowchart LR
    WEB[Web SDK] --> GIN[HTTP or WS Entry]
    CHANNEL[Future Channel Adapters] --> GIN
    SIP[SIP Adapter] --> VOICE[voice module]
    GIN --> CONV[conversation module]
    GIN --> ROUTING[routing module]
    GIN --> TICKET[ticket module]
    GIN --> AGENT[agent module]
    GIN --> CUSTOMER[customer module]
    CONV --> AI[ai module]
    AI --> LLM[LLMProvider]
    AI --> KNOW[KnowledgeProvider]
    ROUTING --> AGENT
    VOICE --> ROUTING
    TICKET --> BUS[event bus]
    CONV --> BUS
    ROUTING --> BUS
    BUS --> AUTO[automation module]
    BUS --> ANALYTICS[analytics module]
```

## 业务模块边界

V1.0 产品中心是 **7 个核心模块**（conversation 为唯一中心聚合，详见
[ARCHITECTURE.md §6](./ARCHITECTURE.md) 与
[V1.0 收敛计划](./docs/v1-convergence-plan.md)）。其余业务代码一律按
「核心模块的子能力」定位，不再单列产品叙事。

### 核心 7 模块（V1.0 产品中心）

| 模块 | 产品定位 |
| --- | --- |
| `conversation` | **系统中心聚合**：会话、消息、参与者、服务过程时间线（`conversation_events` 投影） |
| `routing` | 人工接管、排队、分配、转接（打分路由引擎为 V1.0 增强） |
| `ticket` | 工单全流程：从会话一键建单、状态机、SLA/评价挂靠 |
| `ai` | AI 首答、坐席辅助（建议回复/改写/摘要）、知识检索答案 |
| `knowledge` | 知识库文档、检索与引用（citation） |
| `agent` | 坐席档案、在线状态/负载、三栏接待工作台数据源 |
| `customer` | 客户档案、标签、历史与工单汇（客户 360） |

### 子能力模块（薄壳，挂靠核心，不再是一级产品概念）

| 模块 | 归宿 | 说明 |
| --- | --- | --- |
| `sla` | → `ticket` | SLA 评估/违背事件，本属工单生命周期 |
| `satisfaction` | → `ticket` | 关单后评价 |
| `custom_field` | → `ticket` | 自定义字段作用于工单 |
| `shift` | → `agent` | 排班是坐席 availability 数据源 |
| `macro` | → `agent` | 快捷回复是坐席工具 |
| `gamification` | → `analytics` | 表现评分是 read model 派生 |
| `suggestion` | → `conversation` | 推荐/意图辅助 |
| `quality` | → `conversation` | 会话分析与质检视角 |

冻结（不新增能力面、不进入产品文案）：`api_key`、`email`、`push`、
`webhook`、`translation`、`app_integration`、`auth`（能力面冻结；
登录/刷新/2FA/OIDC 逻辑在 `modules/auth/application` + `platform/auth`）；
`voice` 及其扩展（SIP/PSTN/转写）冻结为扩展边界。**架构门禁：不新增一级模块**，
新能力先找既有模块的子能力归属。

### 工程视角：模块迁移成熟度

各模块的迁移成熟度与 legacy service 角色（含 `automation`/`analytics`
facade 收敛、`statistics` 旧 handler 收口计划），以
[迁移记分卡](./docs/implementation/10-migration-scorecard.md) 与
[当前架构快照](./docs/current-architecture.md) 为准。

### 安全与管理面现状

| 能力 | 状态 | 说明 |
| --- | --- | --- |
| tenant / workspace scope | 已接入管理面 | 管理类路由统一经过 `AuthMiddleware`、`EnforceRequestScope`、`RequirePrincipalKinds`、`RequireResourcePermission` |
| 审计日志 | 已接入管理面 | 关键管理操作统一经过 `AuditMiddleware`，敏感字段做脱敏 |
| 用户 token 状态失效 | 已完成首轮能力 | 支持 `token_valid_after` + `token_version`，并在 router auth middleware 中强制校验 |
| 通用 user security surface | 已完成首轮能力 | 提供 `/api/security/users/:id` 与 `/api/security/users/:id/revoke-tokens` |
| 会话 / refresh token 管理 | 已完成首轮能力 | `POST /api/v1/auth/refresh`、`GET /auth/sessions`、`logout-current` / `logout-others`、TOTP 2FA、OIDC 与 session risk 档位（refresh 重放处置/登录风险）均已落地；revoke list 由平台 `RevokedTokenPolicy` 承载；剩余为审批回滚等管理面增强（V1.0 收口外） |

---

## AI 与知识库设计

Servify 的 AI 能力已经按照“编排层 + Provider”拆开。

```mermaid
flowchart LR
    API[AI Request] --> ORCH[Query Orchestrator]
    ORCH --> PROMPT[Prompt Builder]
    ORCH --> RET[Retriever]
    ORCH --> TOOLS[Tool Registry]
    RET --> KP[KnowledgeProvider]
    ORCH --> LLM[LLMProvider]
    KP --> DIFY[Dify Adapter]
    KP --> WEK[WeKnora Compatibility Adapter]
    KP --> ALT[Future Provider]
    LLM --> OPENAI[OpenAI Adapter]
    LLM --> MOCK[Mock Adapter]
```

**结论：**

- 默认知识源是 pgvector 自建知识库（见下节）；Dify 是推荐的外部知识源，WeKnora 是 compatibility 适配器之一
- 后续如果切 Milvus、Elasticsearch 或自行开发知识库，只需要新增 provider adapter（pgvector 已是内置 provider）
- AI 主流程不应该感知具体知识库实现，只依赖统一检索 contract

### 自建知识库 (pgvector)

Servify 现在支持基于 pgvector 的自建知识库，这是企业私有部署的推荐方案。

**配置:**

```yaml
embedding:
  provider: "openai"  # 或 tei, xinference
  openai:
    api_key: "${OPENAI_API_KEY}"
    model: "text-embedding-3-small"

knowledge:
  provider: "pgvector"
  pgvector:
    search:
      top_k: 5
      threshold: 0.7
```

**内网部署:**

使用 TEI (Text Embeddings Inference) 进行本地 embedding：

```bash
docker run -p 8080:8080 \
  ghcr.io/huggingface/text-embeddings-inference:cpu-1.5 \
  --model-id BAAI/bge-small-zh-v1.5
```

---

## SDK 与渠道预留

当前只实现 Web 方向，但架构已经预留多端 SDK 和多渠道接入。（更新：Android/iOS 原生 SDK 已随移动端里程碑 M1–M3 落地（`sdk/android`、`sdk/ios`，SwiftPM/XCFramework 分发），设计与验收见 `docs/mobile-sdk-design.md`；多渠道接入仍是扩展边界。）

```mermaid
flowchart TB
    CORE[sdk core contracts]
    TWS[transport websocket]
    THTTP[transport http]
    WEBV[web vanilla]
    WEBR[web react]
    WEBVU[web vue]
    API[future api client]
    APP[future app core]
    CH[channel adapters]

    CORE --> TWS
    CORE --> THTTP
    CORE --> WEBV
    CORE --> WEBR
    CORE --> WEBVU
    CORE --> API
    CORE --> APP
    CH --> CONV[conversation module]
    CH --> ROUTING[routing module]
```

设计约束：

- `sdk/packages/core` 只放跨端 contract，不放浏览器 UI 逻辑
- Web SDK 已实现；App SDK（Android/iOS）已按 `docs/mobile-sdk-design.md` 落地原生实现（协议契约与 Web 对齐，运行时结构按各平台惯例）；API SDK 仍只保留目录和协议设计
- 渠道接入统一映射到 `conversation` 和 `routing`，不允许直接穿透到旧 service

---

## SIP 与语音扩展

当前还没有完整实现全套语音协议栈，但架构上已经明确预留 `signaling + media` 两层扩展。

```mermaid
flowchart LR
    SIP[SIP Invite or Hangup] --> SADAPTER[SIP Adapter]
    SADAPTER --> IE[Inbound Event]
    IE --> VOICE[voice module]
    VOICE --> ROUTING[routing module]
    VOICE --> AGENT[agent module]
    VOICE --> CONV[conversation module]
```

这意味着：

- SIP 不会变成聊天模块里的特殊分支
- SIP-WS、PSTN provider webhook 等未来也应走同一 signaling adapter 边界
- runtime 已内置 `voiceprotocol.Registry`，统一注册 SIP、SIP-WS、PSTN provider 和 WebRTC media adapter
- HTTP 已暴露统一协议入口：`/api/voice/protocols/:protocol/call-events/:event` 与 `/api/voice/protocols/:protocol/media-events/:event`
- 语音会先进入 `voice` 业务模型
- WebRTC、RTP/SRTP、录音、转写等媒体能力会走独立 media adapter
- 录音和转写也应走 provider 抽象，而不是把第三方云厂商 SDK 直接写进 `voice` service
- 当前录音/转写已形成独立应用层用例，后续只需要替换 provider
- 当前 runtime 已通过 `VoiceCoordinator` 将 WebRTC 生命周期挂入 `voice` 模块
- `voice` 再与 `routing`、`agent`、`conversation` 协同
- 后续支持呼叫转接、录音、转写、语音质检时，边界仍然清晰

---

## 快速开始

### 环境要求

- **Go** 1.25.0（toolchain go1.25.7）- 项目使用 Go workspace 模式
- **PostgreSQL** 12+ 或 SQLite（开发/测试）
- **Node.js** 24+（仅 Web/管理端/文档相关任务需要）
  - 管理端使用 **pnpm** 10+ 作为包管理器
- 可选：**Docker** / **Docker Compose**

### 常用命令

```bash
# 构建与运行
make build
make run                               # 启动服务端
make migrate                           # 数据库迁移

# 开发验证
make local-check                       # 本地环境检查
make security-check CONFIG=./config.yml
make observability-check CONFIG=./config.yml
make release-check CONFIG=./config.yml

# 仓库卫生
make repo-hygiene                      # 验证生成物未被跟踪
make generated-assets                  # 重新生成并验证提交的生成物

# 其他
make clean-runtime                     # 清理运行时输出
make release-changelog FROM=<tag> TO=HEAD
```

### 常用入口

- 健康检查：`GET /health`
- 就绪检查：`GET /ready`
- 指标端点：`GET /metrics`
- WebSocket：`GET /api/v1/ws?session_id=...`
- AI 查询：`POST /api/v1/ai/query`
- 语音协议列表：`GET /api/voice/protocols`
- 管理后台：`/admin/`
- 公开知识库：`/public/kb/docs`

**数据库配置：**
- 默认使用 PostgreSQL，可通过 `DB_DRIVER=sqlite` 切换到 SQLite（开发/测试）
- 发布检查脚本在默认配置下使用临时 SQLite 库完成自检

### 可观测性

```yaml
monitoring:
  tracing:
    enabled: true
    endpoint: http://localhost:4317
    insecure: true
    sample_ratio: 0.1
    service_name: servify
```

本地追踪链路：

```bash
make docker-up-observ
```

Jaeger 默认地址：`http://localhost:16686`

---

## 文档索引

### 核心文档

- [ARCHITECTURE.md](./ARCHITECTURE.md)
- [docs/current-architecture.md](./docs/current-architecture.md) - 当前真实架构快照
- [docs/embedding-guide.md](./docs/embedding-guide.md) - Web 嵌入集成指南（嵌入方式、会话互认、工单上下文、事件回调、主题全表）
- [docs/architecture-redesign-plan.md](./docs/architecture-redesign-plan.md) - 架构重设计计划（services→modules 迁移期产物，仅存档）
- [docs/index.md](./docs/index.md)
- [docs/WEKNORA_INTEGRATION.md](./docs/WEKNORA_INTEGRATION.md)
- [docs/CI_SELF_HOSTED.md](./docs/CI_SELF_HOSTED.md) - GitHub Hosted CI 说明
- [docs/repo-hygiene.md](./docs/repo-hygiene.md) - 运行时产物、生成物与 ignore 边界
- [docs/generated-assets.md](./docs/generated-assets.md) - 受控生成物、重建入口与校验规则
- [docs/local-development.md](./docs/local-development.md) - Windows / WSL / Linux 本地开发约定
- [docs/contributing.md](./docs/contributing.md) - 提交前自检与协作约定
- [docs/v1-convergence-plan.md](./docs/v1-convergence-plan.md) - **V1.0 产品与架构收敛改造计划书（当前口径）**
- [docs/release-notes-v1.0.0.md](./docs/release-notes-v1.0.0.md) - **v1.0.0 发布说明（当前版本）**
- [docs/release-notes-v0.1.0.md](./docs/release-notes-v0.1.0.md) - v0.1.0 历史发布说明（V1.0 已收敛，仅存档）

### 实施 backlog

- [docs/implementation/README.md](./docs/implementation/README.md)
- [docs/implementation/01-platform-and-runtime.md](./docs/implementation/01-platform-and-runtime.md)
- [docs/implementation/02-ai-and-knowledge.md](./docs/implementation/02-ai-and-knowledge.md)
- [docs/implementation/03-business-modules.md](./docs/implementation/03-business-modules.md)
- [docs/implementation/04-sdk-and-channel-adapters.md](./docs/implementation/04-sdk-and-channel-adapters.md)
- [docs/implementation/05-engineering-hardening.md](./docs/implementation/05-engineering-hardening.md)
- [docs/implementation/06-voice-and-protocol-expansion.md](./docs/implementation/06-voice-and-protocol-expansion.md)
- [docs/implementation/07-sdk-multi-surface.md](./docs/implementation/07-sdk-multi-surface.md)
- [docs/implementation/08-ai-provider-expansion.md](./docs/implementation/08-ai-provider-expansion.md)
- [docs/implementation/09-runtime-and-repo-hygiene.md](./docs/implementation/09-runtime-and-repo-hygiene.md)
- [docs/implementation/10-service-to-module-migration.md](./docs/implementation/10-service-to-module-migration.md)
- [docs/implementation/10-migration-inventory.md](./docs/implementation/10-migration-inventory.md)
- [docs/implementation/10-migration-scorecard.md](./docs/implementation/10-migration-scorecard.md)
- [docs/implementation/10-module-boundaries.md](./docs/implementation/10-module-boundaries.md)
- [docs/implementation/11-tenant-auth-and-audit.md](./docs/implementation/11-tenant-auth-and-audit.md)
- [docs/implementation/12-operator-observability.md](./docs/implementation/12-operator-observability.md)

---

## 当前实施进度（V1.0 收敛口径）

V1.0 收敛改造按 [V1.0 收敛改造计划书](./docs/v1-convergence-plan.md) 实施，
批次状态、提交号与过闸证据见 [todo.md](./todo.md)：

- **B1（Domain 边界/迁移双路径）✅**：核心 7 模块边界与依赖方向治理、
  模块依赖只允许 `delivery → application → domain`、迁移双路径
  （postgres versioned SQL + sqlite AutoMigrate）
- **B2（Agent Workspace/Routing 事件统一/PII 保留）✅**：直派与改派事件
  统一路径（`routing.agent_assigned` / `routing.transfer_completed`）、
  凭证导出/擦除 PII 边界、保留策略过期擦除
- **B3（Knowledge 产品化/AI 反馈回传）✅**：知识来源登记与文档版本、
  索引任务 HTTP 面、AI 首答持久化（REST+WS 旁路记录）、反馈回传
  （`POST /api/v1/ai/feedback`，访客会话绑定）、检索分析读口、访客侧
  citation 引用行与反馈条、admin Knowledge 管理页（来源/版本/任务/分析）
- **B4（收口与发布）✅**：TASKS.md/验收矩阵/文档站口径统一、四道发布门禁
  全绿（2026-10-04），`v1.0.0` 已发布（见
  [发布说明](./docs/release-notes-v1.0.0.md)）

当前代码状态说明：

- 服务端已收敛到模块化单体：`delivery -> application -> domain -> infra`
  分层；跨模块经 module 内 contract 或事件。共享模型层（`internal/models`）
  收敛进行中：过渡别名引用已清零并有 parser 级门禁钉住，残余跨模块直查
  若干处（现状见 [modules-dependency-map](./docs/modules-dependency-map.md)）
- AI 统一到 `QueryOrchestrator + LLMProvider + KnowledgeProvider`；
  `ai` 模块承载首答记录、反馈回传与检索分析（失败静默的旁路观测路径）
- 知识检索两级结构：`knowledge.provider = pgvector | local` 时配置直通
  （优先于外部源）；未配置时走外部源选择链 ragflow → dify → weknora，
  逐级健康降级，终点为无知识源运行
  （外部 provider real 模式证据依赖环境凭证，mock/兼容模式全链留痕）
- 管理面安全基线：scope、RBAC、audit、token policy 已接入管理面
- 数据库层支持 PostgreSQL 与 SQLite（开发/测试回退），迁移走双路径

---

## 官网部署

**[servify.cuihairu.site](https://servify.cuihairu.site/)** — 托管在 Cloudflare Pages，由 Cloudflare 侧 Connect to Git 集成在推送后自动部署（不经过本仓库的 GitHub Actions）。（原 servify.cloud 域名未续费已过期）

### 首次设置

1. **创建 Cloudflare Pages 项目**
   - 登录 [Cloudflare Dashboard](https://dash.cloudflare.com)
   - 进入 **Workers & Pages** → **Create application** → **Pages** → **Connect to Git**
   - 选择 GitHub 仓库 `cuihairu/servify`
   - 项目名称设为：`servify-website`
   - 构建设置（静态站点无需构建）：
     - 生产分支：`main`
     - 构建命令：留空
     - 构建输出目录：`apps/website`

2. **配置自定义域名**
   - 在 Pages 项目设置中添加 `servify.cuihairu.site`
   - Cloudflare 会自动配置 DNS 和 SSL 证书

### 自动部署

当 `apps/website/` 目录下的文件有变更并推送到 `main` 时，Cloudflare Pages 的 Connect to Git 集成会自动重新部署（部署由 Cloudflare 侧触发，本仓库 `.github/workflows/` 不含网站部署 workflow；手动部署可用 `make website-deploy` / `make website-pages-deploy`）。

---

## CI 与文档发布

- GitHub Actions 工作流：`.github/workflows/ci.yml`
- 文档目录按 VitePress 使用方式组织：`docs/`
- CI 运行环境与检查项见 [docs/CI_SELF_HOSTED.md](./docs/CI_SELF_HOSTED.md)

---

## 现阶段结论

V1.0 收敛改造（B0 文档与架构声明 → B1 Domain 边界/迁移双路径 → B2
Routing 事件统一/PII 保留 → B3 Knowledge 产品化/AI 反馈回传 → B4 收口
发布）已全部过闸，`v1.0.0` 已发布。此前的运行时收口、services→modules
迁移、租户/审计/安全基线、可观测性各阶段 backlog 均已清零（历史记录见
[todo.md](./todo.md)）。

后续演进按 [v1.0.0 发布说明](./docs/release-notes-v1.0.0.md)「演进方向」：
Routing 打分引擎（多因子）、Ticket 关闭前拦截规则、薄壳模块文档面收口
剩余项。

---

<div align="center">

**[⬆ 返回顶部](#-servify)**

Made with ❤️ by [cuihairu](https://github.com/cuihairu)

[![Apache License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![GitHub](https://img.shields.io/badge/GitHub-cuihairu%2Fservify-181717?logo=github)](https://github.com/cuihairu/servify)

</div>
