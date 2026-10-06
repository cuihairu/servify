# Servify V1.0 产品与架构收敛改造计划书

> 依据：ChatGPT 审核（2026-10-04，`docs/review/chatgpt-review-2026-10-04.md`）+ 本轮复查（`docs/review/chatgpt-review-2026-10-04-verification.md`）。
> 复查结论：产品定位收敛判断【属实】；架构过度平台化、应以客服核心产品模型和 Agent Workspace 为打磨重点【属实】。
> 本文是把审核建议落成可执行任务的计划书。**本文档只规划，执行需另行拍板。**

---

## 0. 目标与原则

**一句话目标：** 把 Servify 从“模块骨架铺得很宽”的阶段，收敛到“一个独立部署的 Web 智能客服，主链路顺滑、坐席工作台高效、知识链路可用”的 V1.0 状态。

**格局原则：**

1. **不新增一级模块。** `apps/server/internal/modules/` 27 个模块封顶，只允许合并/降级，不允许新开。
2. **Conversation 是服务过程的中心，Ticket 是工作对象，Customer 是客户中心** —— 在领域文档写死（`ARCHITECTURE.md` 6.4/6.5/6.6 与 `docs/current-architecture.md`）。
3. **AI 服务于客服，不做 AI Agent 平台。** 工具保持 3-4 个；不引入 Planner/MCP/Multi-agent/Vector DB 主叙事。
4. **Analytics 是观察者**（事件投影 → read model），不得反向驱动业务决策链路。
5. **Event Bus 维持 in-process（+Redis 多实例可选）**，不做 Kafka、不做 full event sourcing（维持 `ARCHITECTURE.md` non-goal）。
6. **Tenant/Workspace 只做治理与隔离**，概念定死，不进入产品主叙事。
7. 每阶段交付都有**验收标准**，过闸才进入下一阶段。

---

## 1. 保留 / 降级 / 删除清单

### 1.1 保留并增强（第一层核心模块，7 个）

| 模块 | 增强内容 | 对应计划书章节 |
| --- | --- | --- |
| `conversation` | 身份重定位：系统中心；补 `ConversationEvent`（时间线投影源）、与会话状态机联动 | §5.2、§5.4 |
| `agent` | 补充在线状态/负载快照与工作台侧数据契约（Inbox/Queue 查询口） | §4 |
| `customer` | 补 Profile 读模型（标签、历史、Ticket 汇、Timeline 汇） | §5.2、§6 |
| `routing` | 打分路由引擎（skill/language/availability/workload/priority/tier/channel/SLA） | §6 |
| `ticket` | 与 Conversation 的关系显式化（`conversation_id`）、状态机不动、SLA 挂靠 | §7 |
| `ai` | 首答/助手链路的产品化补齐（结构化输出、反馈回传、摘要沉淀） | §5.3 |
| `knowledge` | 产品化（source/document/chunk/index/version/retrieval analytics、citation 展示） | §5.4 |

### 1.2 降级为子能力（不再是一级产品概念，保留代码但收紧架构存在感）

按审核 §22 与复查 §11，以下模块**不删除代码**（`.claude`/测试/迁移风险高），但：

- 从「一级模块」叙事降为「核心模块的子能力」，README/文档站不再单列产品章节；
- 收敛规则：**代码位置不动**，文档与路由表集权；`sla`/`satisfaction`/`shift`/`macro`/`custom_field` 挂靠 `ticket`/`routing`/`agent` 组合；`gamification`/`suggestion` 挂靠 `conversation`+`analytics`；`quality` 挂靠 `conversation` 分析面。

| 模块 | 降级归宿 | 备注 |
| --- | --- | --- |
| `sla` | → `ticket` 子能力（SLA 评估/违背事件） | 无 domain，薄壳；SLA 逻辑本属 ticket 生命周期 |
| `satisfaction` | → `ticket` 子能力（关单后评价） | 无 domain |
| `shift` | → `agent` 子能力（排班是 availability 数据源） | 无 domain |
| `macro` | → `agent` 子能力（快捷回复是坐席工具） | 无 domain |
| `custom_field` | → `ticket` 子能力（自定义字段作用于工单） | 无 domain |
| `gamification` | → `analytics` 子能力（表现评分是 read model 派生） | 无 domain |
| `suggestion` | → `conversation` 子能力（推荐/意图辅助） | 无 domain |
| `quality` | → `conversation` 分析与质检视角 | 保持 domain，暂不动 |
| `statistics`（仍散在 `internal/handlers/statistics_*.go`） | → 收口进 `analytics` 模块（含导出） | 迁移式收口，非降级 |

### 1.3 删除 / 冻结清单

| 项 | 动作 | 理由 |
| --- | --- | --- |
| `api_key`、`email`、`push`、`webhook`、`translation`、`app_integration`、`auth`（模块内薄壳） | **冻结**：不再新增能力面，不进入任何产品文案 | 多渠道/开放平台是 P2+ 扩展；auth 逻辑实际在 `platform/auth` + `handlers/auth_*`，薄壳模块纯占位 |
| `voice` 及其扩展（SIP/PSTN/transcription） | **冻结为扩展边界**：维持现状（有 domain + provider），不投 V1 资源 | 审核 §14、v1-product-scope P2 |
| 新增任何一级模块 | **禁止**（架构门禁：新能力先找既有模块子能力归属） | 审核 §11 |
| 新渠道（Telegram/WeCom/WhatsApp/Mobile UI/SIP 深度接入） | 冻结 | v1-product-scope `V1 不做的功能` |

---

## 2. Domain 边界（写死版）

### 2.1 一级关系（治理规范，写入 `ARCHITECTURE.md` 6.4-6.6）

```text
Customer（客户中心）
   └── Conversation（服务过程中心，唯一核心聚合）
         ├── Message / Participant / ChannelBinding / ConversationEvent
         ├── AI interaction         （ai 模块，经 QueryOrchestrator）
         ├── Agent interaction      （agent 模块：接管/协作/转接）
         ├── Routing                （routing 模块：排队/评分/分配/转接）
         └── Ticket                 （ticket 模块：后续工作项，持有 conversation_id）
```

规则（架构门禁）：

1. **Ticket 必须经由 Conversation 派生**（`ticket.conversation_id NOT NULL` 从 V1 开始执行，历史数据回填）；任何模块不得反向把 Conversation 挂在 Ticket 下。
2. `routing`、`ai`、`agent` 对 Conversation 只读引用与会话期间更新状态；写入口收敛到 `conversation` 模块 application。
3. 模块依赖方向只允许：`delivery → application → domain`，跨模块经模块内 contract 或事件，禁止 `models.go` 穿透（见 §5.1）。
4. 每个第一层核心模块补 `domain/doc.go` 一段「Owns / 中心地位」注释（Conversation 中心地位写死）。

### 2.2 横向能力（不参与业务分层）

`Identity/Auth`（`platform/auth` + `modules/auth`）、`Tenant/Workspace`（治理）、`Realtime`（`platform/realtime`）、`Event Bus`（`platform/eventbus`，in-process）、`Observability`、`Storage`、`SDK`（`sdk/`，Web 优先）。

### 2.3 扩展边界（不进入 V1 模块图）

Remote Assist（对话内协助入口维持）、Voice/SIP、多渠道、Mobile、深度业务集成。

---

## 3. 数据模型

### 3.1 核心表收敛（持续保留/新增/迁移）

现状：共享模型集中在 `apps/server/internal/models/models.go`（30+ 表，跨模块共享）。收敛目标：**每个核心模块持有自己的模型与仓储**，`models.go` 只保留跨模块共享基类与迁移产线。

| 域 | 表（现状→目标） | 说明 |
| --- | --- | --- |
| conversation | `sessions`、`messages`（现状 `models.go:235,260`）→ + `conversation_participants`（独立表，现为 Session 内嵌或外键）、`channel_bindings`、**`conversation_events`（新增）** | `conversation_events` 是 Service Timeline 的落库源：`{conversation_id, event_type, actor, payload, occurred_at, tenant_id, workspace_id}` |
| ticket | `tickets`、`ticket_comments`、`ticket_files`、`ticket_statuses`、`ticket_custom_field_values`（`models.go:129-221`）+ **`tickets.conversation_id`** | 关单评价（satisfaction）以 ticket 为锚 |
| customer | `customers`（`models.go:86`）+ Profile 读模型视图（标签、历史、Ticket 汇） | — |
| agent | `agents`（`models.go:105`）+ `agent_status`/负载快照表（新增或并入） | 供 routing 评分与工作台查询 |
| routing | `transfer_records`、`waiting_records`（`models.go:276,291`）+ **`routing_assignments`（持久化）** + `queue_entries`（现内存态→补初始持久化可选） | 打分因子落表（skills/language/priority/tier 来自对侧表） |
| ai / knowledge | 现有检索/文档表 + **`ai_answers`（答案/来源/置信持久化）**、**`answer_feedback`（"是否有帮助"评价）** | Feedback 回传用 |
| analytics | 现有 `daily_stats`（`models.go:432`）等，维持 read model 地位 | 统计由事件投影，不在业务写路径 |
| sla | `sla_configs`、`sla_violations`（`models.go:320,339`）保留 | 降级为 ticket 子能力，表不动 |
| security | `audit_logs`（`models.go:511`）+ **PII 策略表（retention 规则、删除/导出任务）** | 落地审核 §18-② |
| tenant/workspace | `tenant_configs`、`workspace_configs`（`models.go:482,496`）+ **核心业务表补 `tenant_id`/`workspace_id` 列** | 先 ticket（已完成），V1 内扩到 conversation/customer/ticket 附件/routing/ticket 评论 |

### 3.2 tenant/workspace 落库规则（V1 一步到位）

- 新写 SQL 迁移：为 `conversation`、`ticket`、`customer`、`knowledge`（文档表）、`routing` 记录补 `tenant_id`/`workspace_id`（default workspace 语义一次迁移回填）。
- 仓储层查询强制按 scope 过滤（现有 ticket 仓储模式为范本）。
- 概念定死（写进 `docs/tenant-workspace-boundaries.md`，对齐审核 §17）：

```text
Tenant = 企业/组织；Workspace = 一个运营空间；Agent = 坐席；Customer = 客户；
Conversation = 一次服务过程；Ticket = 后续工作项
```

---

## 4. Agent Workspace（V1 第一优先，最高 ROI）

位置：`apps/admin/src/pages/Conversation` 改造 + 新增 `apps/admin/src/pages/Workspace`，替代分散 CRUD 的后台接待体验。

### 4.1 页面结构（审核 §7 三栏）

```text
┌──────────────┬──────────────────────────┬──────────────┐
│ Inbox/Queue  │ Conversation 会话区      │ Customer     │
│              │                          │ Profile      │
│ 🔴 12 排队中  │ AI summary（可折叠）     │ 标签          │
│ 🟡 8 AI 接待中│ Timeline（新）           │ 历史会话      │
│ 🟢 23 进行中  │ Messages（流式/富文本）   │ Tickets 汇   │
│              │ Reply（AI 建议/改写）     │ Knowledge    │
│ My tickets   │ 动作：接管/转接/创建工单  │ 建议（浮动）  │
└──────────────┴──────────────────────────┴──────────────┘
```

### 4.2 功能与验收（一个页面完成全流程）

| # | 能力 | 验收标准 |
| --- | --- | --- |
| W1 | Inbox 聚合（排队/进行中/我的工单，计数徽章） | 坐席登录后第一屏即见队列与负载，无需跳页 |
| W2 | 单会话处理：接管、回复、转接、关闭 | 全链路在会话页内完成（现 `Conversation/index.tsx` 能力保留并扩） |
| W3 | AI summary + 建议回复/改写（copilot 接入） | 复用 `POST /api/v1/ai/copilot` 三类 action；会话页一键触发 |
| W4 | Knowledge 建议侧栏 | 基于当前会话调检索，附 citation（relevance 展示） |
| W5 | Customer 面板（档案/标签/历史/Ticket 汇/Timeline） | 无需离开会话页查看客户 360 |
| W6 | Timeline 组件（审核 §19） | 会话页可见 `conversation_events` 投影流水（10:32 created → 10:35 handoff → …） |
| W7 | 工单联动 | 会话页一键建单/改单，Ticket 页保留但为“工作对象”视图 |
| W8 | 性能基线 | 会话页首屏渲染 ≤ 1s（本地 dev）、WS 消息延迟不劣于现状 |

### 4.3 不做

- 不做远程协助独立工作台界面的 V1 全量（维持 `AssistReviewPanel` 现状）；
- 不做移动端坐席端。

---

## 5. AI（服务客服，不做平台）

### 5.1 现有不动

`QueryOrchestrator` / `PromptBuilder` / `Retriever` / `ToolRegistry` / `Guardrails` / `handoff_policy` / streaming（delta 帧）/ SessionHistoryLoader 全部保留（复查 §8 已确认合理）。

### 5.2 结构化输出收口（对齐 v1-product-scope）

- 首答输出保持 `answer/citations/confidence/next_action/handoff_reason/ticket_summary` 结构；`next_action ∈ {answer, clarify, handoff, ticket}` 为稳定状态集；评审任何新增状态前必须过「不破坏 V1 主链路」检查。

### 5.3 反馈回传（新增，P1 内）

- 端点：`POST /api/v1/ai/feedback`（对一次 ai-answer 评价 `helpful/not_helpful + 可选意见`），落 `answer_feedback`；
- 会话页与访客侧 widget 都有入口（widget 侧"Was this helpful?"，审核 §21/P4）；
- 管理侧 Knowledge 页展示 top unanswered / low-confidence 问题（复用 analytics read model 投影）。

### 5.4 明确禁区（架构门禁）

不引入：Agent Framework/MCP/Planner/Workflow 编排/多 Agent 自治/模型路由市场。工具集维持 3-4 个（检索知识、会话/客户上下文、建单、人工接管）。任何超出项须先写回本文档并获得同意。

---

## 6. Routing（从"分配"到"打分引擎"，渐进不重构）

### 6.1 现状（保留）

`queue/assignment/transfer/skill match/availability/load balance` 已存在（`routing/domain/entities.go:5-57`），escalation 走 SLA+automation（`models.go:331,345`；`automation/application/service.go:427`）——V1 不动。

### 6.2 演进（P2 内，审核 §6 打分模型）

1. **评分器接入点**：`routing/application` 内新增 `Scorer` 接口（多因子加权），默认聚合：
   - `skill`（SkillMatchPolicy 现有）、`language`（会话首语/Agent 语种）、`availability`（在线状态+shift 数据源）、`workload`（在办会话数）、`priority`（会话优先级+客户 tier）、`tier`（客户分级）、`channel`、`SLA`（剩余时限，违背阈值加权）。
2. 分配流程保持：`Conversation → Routing Engine（Scorer）→ Candidate Agents 排序 → Assignment`；分数与因子落 `routing_assignments` 便于审计与 analytics 投影。
3. 手动转接、优先级策略维持人工优先于自动：评分只影响「自动推荐候选序列」，不自动执行转接（与现 handoff 语义一致，人工/关键词触发不变）。

### 6.3 验收

- 有两组不同 skill/language 的坐席时，分配结果可复现符合预期权重；
- 负载因子：空闲坐席优先于满载坐席（单测 + 1 条集成用例）；
- 分数与因子可见（管理端 Routing 页展示本次分配理由，读写口来自 `routing_assignments`）。

---

## 7. Ticket（工作对象，不与 Conversation 平级）

### 7.1 关系写死

- `tickets.conversation_id` 显式化；迁移回填历史数据（`conversation_id` 可空=遗留，新数据必填）。
- 生命周期锚定在会话：Conversation 关闭时未完结工作必须要求先建单或明确降级（关闭前拦截规则，P1 内）。
- `sla`、`satisfaction`、`custom_field` 语义归属 Ticket 子能力（§1.2），代码位置不动但 README/文档不再单独叙事。

### 7.2 状态机与事件（不动核心，只补接缝）

- 维持现有状态流转与 `ticket.*` 事件（`ticket/application/events.go`）；
- 新增投影：`ticket.*` 事件同时进入 `conversation_events`（Timeline 完整覆盖客户视角）。

---

## 8. Knowledge（产品化，P3）

按审核 §10 增加（渐进，不推倒 provider 抽象）：

1. **来源模型**：`knowledge_sources`（markdown/website/PDF/FAQ/API 的元数据登记），文档挂 source；
2. **版本与索引**：document 版本号 + `index_jobs` 关联版本；失败重试与可见状态（现状已有索引任务雏形，补版本与状态枚举收口）；
3. **检索分析**：读 `retrieval analytics`（top 问答、无命中率、低置信率）→ Knowledge 管理页展示；
4. **Citation 可视化**：访客侧与坐席侧都渲染 Sources（`📄 refund-policy.md relevance 0.91`），问答链路依赖 §5.3 feedback。

---

## 9. 横向（Analytics / Events / Security / Tenant）

### 9.1 Analytics（观察者，不动基座）

- 保留 `analytics/delivery/eventbus_subscriber.go` 计数投影；把仍散在旧 handler 的 `statistics_*` 收口进 analytics 模块（含导出）；
- V1 里程碑指标（审核 §21/P6 口径）：First Response Time、Resolution Time、AI Resolution Rate、AI Handoff Rate、Agent Load、Queue Time、Ticket Resolution Rate、CSAT（满意度接入后）。

### 9.2 Event Bus

- 维持 `inmemory` 默认 + `redis` 多实例；事件契约清单随模块收口同步更新 `ARCHITECTURE.md` §11；
- 新事件（`conversation.events`、`ai.answer_generated`、`ai.feedback_received`）只做投影消费，不进入业务写路径。

### 9.3 Security（补齐两件事，其余维护现状）

1. **文档修正**：`README.md:220` 安全表改为已实现口径（refresh/sessions/logout-current/2FA 已落地，本次复查证据 `router_auth.go:55,65-72`）；
2. **Customer Data Boundary（P2 内）**：PII 清单（姓名/邮箱/电话/聊天记录/附件/订单字段索引）、`audit_logs` 扩展数据保留策略字段、新增「数据导出」与「数据删除（含关联擦除）」管理面、保留期配置化。

### 9.4 Tenant/Workspace

按 §3.2 落库 + 概念写死；进入 V1 的页面（Agent Workspace）从第一天按 scope 过滤。

---

## 10. 实施优先级（阶段闸门，每闸有验收）

| 阶段 | 内容 | 产出 | 验收闸门（Gate） |
| --- | --- | --- | --- |
| **P0（本轮评审后立即）** | ① 复查文档入库（已完成：verification 报告）② README/文档漂移修正（§9.3-1、current-architecture 快照更新）③ `ARCHITECTURE.md` 6.4-6.6 写死 Conversation 中心与 Ticket 从属关系 + `domain/doc.go` Owns 注释 | 文档 + 架构声明 | `make local-check` 绿；文档站无旧口径残留 |
| **P1（产品链路增强）** | Agent Workspace 三栏工作台（§4 全量）；Conversation 补 `conversation_events` 落库与 Timeline 组件；satisfaction/macro/shift 归位叙事降级 | 工作台可完整处理一个会话全流程 | W1-W8 验收表全过；e2e：访客进线→AI 首答→handoff→坐席回复→建单→关单全链路自动化用例通过；`make release-check` 绿 |
| **P2（Routing + Security）** | Scorer 打分引擎（§6.2）；PII 边界（§9.3-2）；statistics 收口进 analytics；核心业务表 tenant/workspace 回填 | 打分路由 + 数据合规面 | §6.3 验收三例全过；PII 用例（导出/删除/retention）全过；迁移在空库+既有库两态可逆 |
| **P3（Knowledge 产品化）** | 来源/版本/检索分析/citation 展示（§8）；feedback 回传（§5.3） | 知识库管理全流程 | Knowledge 管理页可完成 source→文档→版本→检索分析→反馈回看一条链；`README_KNOWLEDGE.md` 更新 |
| **P4（收口与发布）** | 薄壳模块叙事收口（§1.2 文档面全部完成）；`TASKS.md`/验收矩阵与 V1.0 口径统一；发布 `v1.0.0` | V1.0 发布 | 全闸累计验收矩阵全绿；`make release-check`+`security-check`+`observability-check` 绿；发布说明按 V1 收敛口径书写 |

**并行不冲突**：P1 工作台与 P2 Routing 的 scoring 依赖 conversation/routing 现状边界，可两路并进；P3 依赖 P2 的 analytics 收口（read model 基座）；P4 为累计收尾。

---

## 11. V1.0 验收标准（总闸）

沿用 `docs/v1-product-scope.md` 成功标准并落为可执行清单：

1. 独立站经 Web widget 发起真实会话并在会话页完成 AI 首答→人工接管→转接→建单→关单（自动化 e2e 用例为准）；
2. Agent Workspace 为唯一接待入口（后台 CRUD 页只服务配置，不出现在接待路径上）；
3. AI 无依据时明确降级（guardrail），回答带 citation；feedback 可查；
4. 路由分配可解释（因子可见）+ 负载/技能优先可复现；
5. 核心表 tenant/workspace 落库，管理面全链路 scope 过滤；
6. 生产配置不依赖 mock/in-memory 伪装交付；PII 导出/删除可用；
7. 文档站、README、官网、验收清单同口径表达 V1 主链路（复用审核 §24 定位语）；
8. 27 个模块不再增加；薄壳模块文档面完成降级叙事。

## 12. 执行方式（交付给 Code Agent 时）

每阶段拆为独立任务条目（如 `docs/implementation/13-v1-convergence/`），每条含：目标文件路径、依赖、验收命令（`make local-check` / 指定单测 / e2e 脚本）、禁止事项（如“不得新增一级模块”）。执行顺序严格按 §10 闸门；任一闸门未过不进入下一阶段。

---

## 13. 非目标（V1.0 明确不做）

多渠道、语音/SIP/录音转写生产链路、完整远程协助工作台、多 Agent/Workflow/MCP/模型路由、CRM/订单/支付深集成、Provider marketplace、多租户 SaaS/计费、移动端坐席、Kafka/Event Sourcing、微服务拆分、新增一级模块。