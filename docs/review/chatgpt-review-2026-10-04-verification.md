# ChatGPT 审核复查报告

> 复查基线：`cuihairu/servify`（GitHub 公开仓）今日（2026-10-04）HEAD `8e2034d`（docs: ChatGPT 审核文档入库）
> 复查方式：逐条对照当前代码与文档（`README.md` / `ARCHITECTURE.md` / `docs/v1-product-scope.md` / `apps/server/internal/*`）给出证据。
> 结论统计见文末；每条标注【属实 / 部分属实 / 已过时 / 不成立 / 建议未落地】。
> **快照注记（2026-10-08 文档对账）**：本报告为 `8e2034d` 时点快照。同日 V1.0 收敛 B0-B4 过闸后，§5 / §10 / §18-② / §19 / §22 的「未落地」判定已不反映当前状态（Service Timeline、PII 边界 retention 门、knowledge sources/versions/index-jobs/检索分析均已落地），现状见 [acceptance-checklist.md](../acceptance-checklist.md) §14。

---

## 核心判断核查（用户指定重点）

### 判断一：「已收敛为可私有部署的 AI Customer Service Platform」——【属实】

| 证据 | 位置 |
| --- | --- |
| README 主标语即“开源智能客服系统 — Web 优先，AI 首答，人工接管，工单闭环” | `README.md:9` |
| “第一版产品目标先收敛在企业官网、品牌独立站、SaaS 官网和文档站的 Web 智能客服…主链路 `Web 接入 -> AI 首答 -> 人工接管 -> 转接协作 -> 工单闭环`” | `README.md:22-26` |
| “其产品重心不是‘平台化多租户’，而是先把客服主链路做完整” | `README.md:26` |
| “`tenant/workspace` 更接近治理和隔离能力，而不是产品主叙事” | `README.md:59` |
| V1 产品边界文档白纸黑字：非平台化客服中台、非全渠道联络中心，而是“可独立部署、Web 优先、AI 协同的智能客服产品” | `docs/v1-product-scope.md:7-11` |
| V1 定位句与审核 §24 建议完全一致 | `docs/v1-product-scope.md:7-9` |

**结论：** 审核该判断与当前文档叙事高度吻合。README 与 v1-product-scope 早已把“独立部署 Web 智能客服”设为唯一主叙事，远程协助、语音、多渠道均被明确降级为扩展/后续方向（`README.md:28-30, 155`）。

### 判断二：「架构过度平台化，应打磨客服核心产品模型和 Agent Workspace」——【属实】

证据一：一级模块数量超标（合并 Evidence）：

| 事实 | 证据 |
| --- | --- |
| `apps/server/internal/modules/` 下共 **27 个一级模块**；其中 **13 个没有 `domain` 层**（薄壳模块）：`api_key`、`app_integration`、`auth`、`custom_field`、`email`、`gamification`、`macro`、`push`、`satisfaction`、`shift`、`sla`、`suggestion`、`workspace` | `ls apps/server/internal/modules/` |
| README 自述仍有多项业务以旧链路承载：statistics、SLA、satisfaction、shift、workspace、macro、custom_field、app_integration | `README.md:207-210` |
| `statistics` 业务仍走旧 handler（无模块归属） | `apps/server/internal/handlers/statistics_handler.go`、`statistics_export_handler.go` |
| 共享数据模型仍集中在 `internal/models/models.go`（30+ 表） | `models.go:14-589` |

证据二：客服核心产品模型单薄：

| 事实 | 证据 |
| --- | --- |
| 系统主对象 Conversation 的领域模型仅 4 个实体（ChannelBinding/Participant/Conversation/ConversationMessage），且无 Event/Timeline 概念 | `apps/server/internal/modules/conversation/domain/entities.go:30-58` |
| 审核建议的 “Conversation 中心服务过程模型”（AI/Agent/Routing/Ticket 全部挂到 Conversation 下）尚未在领域模型写死 | `ARCHITECTURE.md:6.4` 仅描述 Ownership，无聚合关系声明 |
| “Service Timeline”（Customer/Conversation/Ticket 统一事件流）完全不存在 | 全库无 timeline 相关模型（grep 验证） |

证据三：Agent Workspace 是当前最弱环节：

| 事实 | 证据 |
| --- | --- |
| 管理端共 **24 个独立后台页面**（SLA/Automation/Gamification/Macro/Voice/AppMarket…），无聚合三栏 Inbox/Queue 工作台 | `find apps/admin/src/pages -maxdepth 1 -type d` |
| 仅 `Conversation` 单页（1394 行）具备会话处理主体，其余均为 CRUD 页 | `apps/admin/src/pages/Conversation/index.tsx` |
| README 自述“工作台密度、接待流程收敛”仍为进行中（🔄） | `README.md:43` |

**结论：** 平台化的“积木”已铺开（27 模块、13 薄壳、provider 抽象、SDK 全家桶、voice/SIP/自动化/游戏化），但客服产品体验（聚合工作台、服务时间线、知识反馈闭环）尚未同步。审核的核心判断成立。

---

## 逐条复查（对应审核正文章节）

| # | 审核结论 | 判定 | 证据 |
| --- | --- | --- | --- |
| §2.1 | 模块化单体（Modular Monolith）非常正确、不要拆微服务 | **属实** | `ARCHITECTURE.md:9,37-39`（“remain a single deployable backend until scale or organizational needs clearly justify decomposition”）；Non-goal 明确 microservice decomposition / full event sourcing（`ARCHITECTURE.md:29-33`）；代码仍为单进程 `cmd/server` + worker |
| §3 | Domain-first 模块边界（domain/application/infra/delivery）对 | **属实** | `conversation`、`routing`、`ticket`、`ai`、`knowledge`、`agent`、`customer` 等均含四层结构（目录验证）；但 13 个薄壳模块无 `domain`——分层只对已收口模块成立（见核心判断二） |
| §4 | Conversation 应继续做系统“核心对象” | **属实** | `ARCHITECTURE.md:267-277`（Conversation owns unified record / participants / messages / channel bindings / conversation status，并注明 “This module is the center of multi-channel interaction”）；实体齐备（`conversation/domain/entities.go:30-58`） |
| §5 | Ticket 不应与 Conversation 平级竞争核心 | **属实（建议未落地）** | 代码层面 ticket/conversation 确为并列一级模块（`modules/ticket+domain`、`modules/conversation+domain`）；审核“Ticket 是工作对象、不取代 Conversation”的建议在领域文档中未写死，需在收敛计划中落实 |
| §6 | Routing 已有 queue/assignment/transfer/skill-based routing/escalation，方向正确 | **属实（escalation 部分）** | queue/assignment/transfer/skill：`routing/domain/entities.go:5-57`（QueueStatus/QueueEntry、Assignment、TransferRecord、SkillMatchPolicy、AgentAvailabilityPolicy、LoadBalancePolicy）；escalation：主实现不在 routing 而在 SLA（`models.go:331,345` 的 EscalationTime/ViolationType=escalation）与 automation（`automation/application/service.go:427` `escalate_priority`），routing 模块内仅测试引用 |
| §7 | Agent Workspace 是当前产品层面最大机会，应升级为第一优先级 | **属实** | 见核心判断二证据三（24 个独立页、无聚合工作台、Conversation 单页为唯一接待主体）；审核给出的三栏工作台设计尚未出现 |
| §8 | AI 设计合理（Orchestrator+Buffers+Provider），但不要做成 AI Agent 平台 | **属实** | `ai/application/`：`query_orchestrator.go`、`prompt_builder.go`、`retriever.go`、`tools.go`（ToolRegistry/Register/Get/List/ToolExecutor）、`handoff_policy.go`、`guardrails.go`；LLM provider：`platform/llm/{openai,anthropic,local,mock,factory}`；工具保持少量（customer_lookup/handoff/ticket_lookup）与 v1-product-scope“少量明确工具+结构化输出”一致（`docs/v1-product-scope.md:37-57`） |
| §8 | 已做 streaming / 会话历史 / 知识检索 / 来源引用 / confidence / handoff 建议 / AgentCopilot | **属实** | `ARCHITECTURE.md:596-602`（SessionHistoryLoader 窄接口、sources/strategy 透出、confidence 门控 next_action=handoff、WS `ai-response-delta` 增量帧）；copilot：`ai/delivery/agent_copilot.go` + `internal/handlers/ai_copilot_handler.go`（suggest_reply/rewrite/session_summary 三类 action） |
| §9 | Knowledge Provider 抽象（pgvector/Dify/WeKnora/future）正确，应保留 | **属实** | `platform/knowledgeprovider/`：`dify/`、`local/`、`memory/`、`mock/`、`pgvector/`、`ragflow/`、`weknora/`、`provider.go` 接口；README 默认 pgvector 自建、Dify 推荐、WeKnora 兼容（`README.md:245-268`） |
| §10 | Knowledge 应进一步产品化（sources/docs/chunks/index jobs/versions/permissions/retrieval analytics） | **建议未落地（建议成立）** | knowledge 模块现有 document 管理/索引/检索 provider 抽象（`modules/knowledge/`、`gorm_repository.go`），但检索分析、版本管理、权限未落地；“这条回答为什么这么答”的回溯体验（sources+relevance 展示）已有 `sources`/`strategy` 透出（`ARCHITECTURE.md:598`），缺 UI 呈现层 |
| §11 | Bounded Context 有一点过度：不要继续增加一级模块，suggestion/gamification/analytics/automation/SLA/satisfaction/shift/macro 更适合做子能力 | **属实** | 27 个一级模块、13 个无 domain 薄壳（见核心判断二证据一）；README 自身已把部分业务列为“收口”目标（`README.md:207-210`）；审核建议“降级为子能力”尚未执行，正是计划书 Part 1 内容 |
| §12 | Analytics 应做 Read Model（观察者），不要变成业务中心 | **属实（基本对齐）** | 现有 `analytics/delivery/eventbus_subscriber.go`：消费 `conversation.created`/`conversation.message_received` 等事件做 `IncrementSessions/IncrementMessages` 计数（观察者模式）；`ARCHITECTURE.md:6.10`（operational read models）；残余：统计导出仍在旧 handler（`statistics_export_handler.go`） |
| §13 | Event Bus 不升级 Kafka、不做 Event Sourcing，保持 In-process | **属实** | 现实现为 `platform/eventbus/inmemory_bus.go` + `redis_bus.go`（多实例可选），无 Kafka；`ARCHITECTURE.md:32` 明确 full event sourcing 为 non-goal；已发布事件与审核列举一致（`automation/application/service.go:574`、`analytics/delivery/eventbus_subscriber.go:24-25`） |
| §14 | Voice/SIP 继续降级成 Extension | **属实** | `README.md:30`（“语音能力属于后续扩展边界”）；`docs/v1-product-scope.md:147-154`（P2 不纳入 V1：语音）；voice 模块保持模块化但产品叙事不进入 V1 中心 |
| §15 | Remote Assist 保持“高级能力”而非主线（L0-L4 分层） | **属实** | `README.md:63-69`（“解释”升级为“带着完成”、不承诺完整 co-browsing 产品）；管理端仅 `Conversation/components/AssistReviewPanel.tsx`（453 行）为最小协助入口 |
| §16 | SDK 不要提前实现所有（当前只需 @servify/web） | **部分已过时** | 现 SDK 已超 Web：`sdk/packages/` 含 core/transport-http/transport-websocket/vanilla/react/vue/api-client/app-core/react-native；且 Android/iOS 原生 SDK 已随移动端里程碑 M1-M3 落地（`README.md:284,312`、`sdk/android`、`sdk/ios`）。仍符合的部分：`api-client`/`app-core` 只保留骨架（`ARCHITECTURE.md:8.5` “Reserved but not implemented yet”） |
| §17 | Tenant/Workspace：存在但不抢戏，概念需写死 | **属实（概念已基本定死）** | `README.md:59`；`docs/tenant-workspace-boundaries.md` 已定义 tenant=部署业务租户/workspace=运营空间，ticket 已补显式 `tenant_id`/`workspace_id`；残余：多数业务表尚未显式落库 tenant_id（该文档自述） |
| §18-① | 需补 Refresh Token Session（logout current / logout all / revoke） | **已过时** | 已实现：`POST /api/v1/auth/refresh`（`router_auth.go:55`）、`GET /auth/sessions`、`POST /auth/sessions/logout-current`、`POST /auth/sessions/logout-others`（`router_auth.go:65-67`），外加 TOTP 2FA（setup/enable/disable/recovery-codes，`router_auth.go:68-72`）、OIDC、session risk（refresh 重放处置/登录风险档位，`router_auth.go:41-51`）。注意：`README.md:220` 安全表仍写“仍缺 refresh token、revoke list、批量失效”——**该行相对代码已过时，属文档漂移，建议随收敛一起修正** |
| §18-② | 需补 Customer Data Boundary（PII/Retention/Deletion/Export） | **建议未落地（建议成立）** | 全库未见 PII 保留策略/数据导出删除面（Audit 已有：`audit.go`、`models.go:511 AuditLog`）；无 retention/export/erase 用例 |
| §19 | 建议增加 Service Timeline（Customer 360 核心视图） | **建议未落地（建议成立）** | 全库无 timeline 实体/事件视图；现有 Session/Message/Ticket 各自独立，无法直接呈现“10:32 created→10:35 handoff→11:30 resolved”流水。接入点现成：in-process event bus（对事件做投影） |
| §20 | AI↔客服闭环最终模型（Customer→Conversation→AI/Agent→Routing→Ticket→Resolution） | **方向属实** | 产品叙事已按此组织（`README.md:22-26`、`docs/v1-product-scope.md:178`），但领域模型尚未把关系显式化（同 §5） |
| §21 | 演进顺序 P0 核心闭环 → P1 Agent Workspace → P2 Conversation Domain → P3 Routing → P4 Knowledge → P5 Security → P6 Analytics → P7 Extension | **基本采纳（计划书据此排期）** | 与现有 V1 分层一致（v1-product-scope P0/P1/P2）；本计划书将其细化为可执行任务 |
| §22 | 删/压缩：gamification、suggestion、voice、automation、analytics 降为 capabilities | **建议未落地（建议成立）** | 上述仍为一级模块（gamification/suggestion 无 domain，voice/automation/analytics 有 domain）；未降级。计划书 Part 1 “降级清单”据此制定 |
| §23 | 收敛后目标架构（Customer/Agent/Conversation 主轴 + Knowledge/Workflow 横向 + 横截面能力） | **建议性（未落地）** | 计划书 Part 2 Domain 边界据此制定 |
| §24 | 定位语：“Servify — Open-source AI customer service platform for self-hosted teams” | **属实（已一致）** | README 主标语与 V1 文档均已同义表达；未逐字采用英文标语，无实质差异 |
| §25 | 判断表：模块化/AI/Knowledge/SDK/安全基础高评；Agent 工作台与客服核心体验 ⭐⭐⭐☆☆；最大风险=架构膨胀而产品体验未同步 | **属实** | 与核心判断二证据一致（27 模块 vs 无聚合工作台）。实时能力、安全基础（token 策略/审计/2FA/OIDC）均有代码支撑 |
| §26 | “759 个 commit”“不要再来一次大架构重构，进入产品收拢阶段”“写给 Code Agent 的计划书” | **属实** | GitHub API：`repos/cuihairu/servify/commits` 当前约 759-760 commits（今日新增 docs 提交后为 760）；本地镜像仅 102 commits（历史被裁剪，不影响复查）；“不做大重构、写计划书”即本复查产出的 V1.0 收敛计划书 |

---

## 附带发现（复查过程中定位的文档漂移，供一并修正）

1. `README.md:220` 安全表“仍缺 refresh token / revoke list / 批量失效” —— 代码已实现（见 §18-①），表述过时。
2. `docs/current-architecture.md` 仍称“`services` 目录仍存在、statistics/SLA 等仍在 services 承载” —— `apps/server/internal/services/` 已不存在（目录已删除），该快照文档过时（新一轮架构快照应更新它为当前状态）。
3. `README.md:197`（`automation` 行）“event bus subscriber 仍在 legacy” —— 模块内已有 `modules/automation/delivery/eventbus_subscriber.go`，同款表述对 `analytics` 也已不准确（`analytics/delivery/eventbus_subscriber.go` 存在）。

---

## 复查结论统计

| 判定 | 条数 | 说明 |
| --- | --- | --- |
| 属实 | 15 | 含两项核心判断（产品定位收敛 ✓、过度平台化 ✓） |
| 部分属实 / 建议未落地 | 8 | 方向正确但尚未实施（Ticket 降级、Knowledge 产品化、PII 边界、Timeline、删减清单、SDK 部分） |
| 已过时 | 2 | §16 SDK“不要提前实现”（移动端已落地）、§18-① Refresh Token Session（已实现且超纲） |
| 不成立 | 0 | — |

**复核结论：** 审核与当前代码/文档整体吻合度高，无“不成立”结论；两类“已过时”集中在「审核时尚未实现、当前已实现」的项上，不影响其对未来方向的判断。核心建议（围绕 Conversation + Agent Workspace + AI Handoff + Routing + Ticket 做产品级收敛，不再增加一级模块）成立，已转化为随附《Servify V1.0 产品与架构收敛改造计划书》。