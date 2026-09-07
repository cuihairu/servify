# 架构评估：AI / Agent 方向（2026-07）

> 状态：评估文档（未改动任何产品代码）
> 范围：结合 2025–2026 新出现的框架，重新审视 Servify 架构，聚焦 **AI/Agent 框架方向**，给出架构师视角的客观判断。
> 配套阅读：[当前架构分析](./current-architecture.md)、[架构重设计计划](./architecture-redesign-plan.md)、[V1 产品范围](./v1-product-scope.md)

---

## TL;DR（结论先行）

1. **后端 Go modular monolith 是健康的**，业务底子（多租户、路由、会话、语音、工单、知识库）扎实，**不建议重写**。
2. **AI 层真实状态：单轮 RAG**。tool-calling / agent-loop 只有脚手架、**尚未接线**；streaming 接口已定义却没在主链路用。这是当前最大的"看起来有、其实没"的能力缺口。
3. **真正的产品差距**：要让"智能客服"真正能多步行动（查工单、查客户、转人工、追问），需要一个**真正的 agent loop**——而不是再加 RAG。
4. **语言选型**：TS（Vercel AI SDK / Mastra / LangGraph.js）迭代最快、生态最广；但 Go 侧已有 **Eino**（字节，"Go 版 LangGraph"，生产验证）与 LangChainGo——**Go 内即可获得成熟 agent 能力，不必非剥 TS 服务**。这是本次评估相对初版的重要修正。
5. **推荐渐进路线**（按架构冲击从小到大）：
   - **Step 0（可选）**：引入 **LiteLLM** 模型网关，统一多模型 / failover / 成本 / 限流，几乎不动现有 Go 架构。
   - **P0 短期**：Go 内接通 agent loop（自研 minimal loop 或引入 Eino；脚手架已就位，2–3 周）。详见 [P0 实施计划](./implementation/13-ai-agent-loop.md)。
   - **P1 中期**：仅当 Go 侧能力跟不上时，才把 AI 编排剥成 TS 服务（首选 Vercel AI SDK；Mastra 因供应链风险暂缓）。
   - **P2 边缘**：官网已上 Cloudflare，可顺带承载轻量边缘网关；**不建议**全栈迁 Workers。
6. **不建议**：全盘重写、全栈迁 Cloudflare（WebRTC / 语音 / 多租户 PG 代价过大）、自研全套 agent 框架长期追新。

---

## 1. 当前架构（一页纸）

| 层 | 现状 |
|---|---|
| 后端 | Go 1.25，Gin + GORM + Postgres(pgvector) + Redis + gorilla/WS + pion/WebRTC；**modular monolith**，正从 `handlers→services→models` 收口到 `modules/*/{domain,application,infra,delivery}` |
| 平台抽象 | 16 个 platform 包：llm / embedding / knowledgeprovider / aiprovider / realtime / sip …（Ports & Adapters，做得相当干净） |
| AI 编排 | `modules/ai/application`：QueryOrchestrator + Retriever + PromptBuilder + Guardrails + ToolRegistry/Executor + policy/guardrails/metrics |
| 前端管理台 | UmiJS Max + Ant Design Pro（pnpm） |
| SDK | TS workspace（core + react/vue/vanilla），Vite，WebSocket-first |
| 官网 | 静态站，已部署 Cloudflare Worker（`servify.cuihairu.site`） |
| 文档 | VitePress → GitHub Pages |
| 可观测 | OpenTelemetry(tracing) + Prometheus(metrics) + logrus |

底子好。问题不在"架构烂"，而在 **AI 能力停在 RAG、没进 agent 阶段**。

---

## 2. AI 层真实状态（核心技术发现）

读 `apps/server/internal/modules/ai/application/query_orchestrator.go::Handle` 的真实链路：

```
policy hooks → guardrails.ValidateInput
  → retriever.Retrieve (单次向量检索 RAG)
  → promptBuilder.Build (system + 历史 + knowledge 拼接)
  → llmProvider.Chat (单次、非流式)
  → guardrails.SanitizeOutput
  → return AIResponse
```

**关键事实**：

- **没有 agent loop**。整条链路里没有 `for { ... tool calls ... }`。`ToolRegistry` / `ToolExecutor` / `tool_customer_lookup` / `tool_ticket_lookup` / `tool_handoff` 都已实现，但 **`Handle` 根本不调用它们**。
- `LLMProvider` 接口已支持 `ChatStream`、`ToolDefinition`、返回 `ToolCalls`（OpenAI / Anthropic / Mock 三种 adapter 都在）——**能力都留好了，就是没通电**。
- `PromptBuilder` 很朴素：拼接 system / 历史 / knowledge 三段，**没有 token 预算 / 上下文窗口管理 / re-ranking / query rewrite**。
- Embedding（OpenAI / TEI / Xinference）与 Knowledge（pgvector / Dify / WeKnora）的多 provider 抽象**做得很好**，是整个 AI 层最成熟的部分。

**一句话**：这是"结构良好的单轮 RAG + 一套未接线的 agent 脚手架"。距离真正的智能客服，差的是一个 agent loop，不是更多检索。

---

## 3. 2025–2026 框架现状速览

| 框架 | 定位 | 与 Servify 契合度 |
|---|---|---|
| **Vercel AI SDK v5/v6** | TS 生态最成熟 agent SDK：`stopWhen` agent loop、`ToolLoopAgent`、typed SSE 流式、`toolApproval`(HITL)、`prepareStep`、动态工具、v6 统一 generateText/Object | ⭐⭐⭐⭐⭐ 若走 TS 编排层，首选 |
| **Cloudflare Agents SDK + Durable Objects + Workers AI** | 边缘有状态 agent：每个 agent 独立 state / SQLite、内置 WS / 调度 / MCP、Workers AI 50+ 模型边缘推理、Agents Week 2026 发布 18 个产品 | ⭐⭐⭐ 适合新项目 / 边缘优先，迁现有 Go 后端代价大 |
| **Mastra** | TS 原生：workflow + agent + RAG + eval + dev studio，DX 好 | ⭐⭐⭐ 功能完整；但 2026-06 `@mastra` npm 组织遭供应链投毒（140+ 包），安全敏感场景需配套依赖锁定 / provenance |
| **LangGraph (Py/TS)** | 图状态机式 agent，1.0（2025-10）招牌是 durable execution（断点恢复），企业采纳高 | ⭐⭐⭐ 复杂长流程工单有价值，但引入 Python 栈 |
| **Go: Eino（字节）/ LangChainGo** | Eino = "Go 版 LangGraph"，编排 + ADK + workflow 可视化，字节生产验证；LangChainGo 更轻 | ⭐⭐⭐⭐ **同进程最契合**，Go 内即可获得 LangGraph 级能力 |
| **模型网关 LiteLLM** | 语言无关、自托管、统一多模型 + failover + 成本 / 限流 | ⭐⭐⭐⭐ **ROI 最确定、冲击最小**，可作独立第一步 |

**参考来源**：

- [Vercel AI SDK 5](https://vercel.com/blog/ai-sdk-5) · [AI SDK 6](https://vercel.com/blog/ai-sdk-6) · [Tool Calling](https://ai-sdk.dev/docs/ai-sdk-core/tools-and-tool-calling) · [Loop Control](https://ai-sdk.dev/docs/agents/loop-control)
- [Cloudflare Workers AI](https://www.cloudflare.com/products/workers-ai/) · [Durable Objects](https://developers.cloudflare.com/durable-objects/) · [Agents SDK](https://github.com/cloudflare/agents)

---

## 4. 核心矛盾：Go 后端 vs TS agent 生态

- 业务 / 并发 / 性能：**Go 强项**，modular monolith 没必要动。
- Agent 框架迭代速度 / 生态广度：**TS 完胜**。Go 每跟一个新能力（流式协议、HITL、eval、新模型特性）都要自研，长期是负担。
- 项目已有 TS SDK 团队栈；AI 编排层用 TS，能与 SDK / 前端**统一语言与类型契约**。

矛盾点是真实的，但不是"二选一"——可以把 **AI 编排**单独剥出来用 TS，业务与数据留在 Go。

---

## 5. 四条演进路径 + 推荐

### 路径 A：Go 内接通 agent loop ✅ 推荐（短期）

把已有 `ToolExecutor` 接进 `Handle`，实现 `while(toolCalls){ 执行→回灌→再 Chat }` 的等价 `stopWhen`，并把 `ChatStream` 接到 WebSocket。

- **优点**：不换语言、2–3 周量级、立即让客服"能动起来"、复用全部脚手架。
- **缺点**：长期仍要自己跟 streaming / HITL / eval。

### 路径 B：AI 编排独立成 TS 服务 ✅ 推荐（中期）

把 `modules/ai` 抽成一个 TS 服务（Vercel AI SDK 或 Mastra），通过内部 HTTP / gRPC 给 Go 后端调用；Go 保留业务 / 数据 / 路由 / 语音。

- **优点**：拿到最成熟 agent 能力、与 SDK 统一、迭代快、可独立部署 / 扩缩。
- **缺点**：双运行时、跨服务调用、团队需会 TS（已有 SDK 栈，门槛低）。

### 路径 C：全栈迁 Cloudflare（Workers + DO + Agents SDK）⚠️ 不推荐

- 代价：WebRTC 远程协助（pion）、PSTN / SIP 语音、多租户 Postgres+pgvector、Redis 会话——这些在 Workers 边缘模型里要么跑不了要么要重做。
- 适合"新项目"，不适合"迁移现有成熟后端"。
- **可取的小步**：官网已在 Cloudflare，可再加一个轻量边缘网关（鉴权 / 限流 / 边缘 AI 兜底）。

### 路径 D：模型路由网关（LiteLLM / Portkey）➕ 补充，非核心

只解决多 provider / 成本 / 可观测 / 降级，**不解决 agent 能力**。可作为 A / B 的配套，不单独成立。

---

## 6. 造 vs 买（必须直面）

如果目标只是"有一个能用的智能客服"，**Chatwoot + AI 扩展**（开源）或商业客服 SaaS 能省下极大人力。代价是**放弃产品差异化与数据控制权**。Servify 已经走到自研模块化单体 + 多 provider 抽象这一步，沉没成本和差异化诉求都指向"继续自研"，但这是要由决策者拍板的战略选择，不是技术选择。

---

## 7. 推荐渐进路线（三阶段）

| 阶段 | 动作 | 产出 | 量级 |
|---|---|---|---|
| **Step 0（可选）** | 引入 LiteLLM 模型网关，统一多模型 / failover / 成本 / 限流 | 多模型可替换、降本、可观测；几乎不动 Go 架构 | 数天 |
| **P0 短期** | Go 内接通 agent loop：`Handle` 里加 tool 执行循环 + `ChatStream` 接 WS；接 customer / ticket / handoff 三个工具 | 客服能多步行动、流式回答 | 2–3 周 |
| **P1 中期** | AI 编排剥成 TS 服务（Vercel AI SDK / Mastra），Go 通过内部 API 调用；加 eval + HITL（转人工审批） | 迭代速度 ×N、统一类型契约 | 1–2 月 |
| **P2 边缘** | 官网旁加 Cloudflare 轻量网关；评估 Workers AI 做高频兜底推理降本 | 边缘低延迟、降服务器负担 | 按需 |

---

## 8. 不建议动的部分

- ❌ Go 后端整体重写 / 换语言
- ❌ 全栈迁 Cloudflare Workers（语音 / WebRTC / 多租户 PG 代价过大）
- ❌ 自研全套 agent 框架长期追新
- ❌ 现在就上 LangGraph 这类重框架（过度工程）

---

## 附：推进时的后续可选动作

1. 针对 **P0（Go 内接 agent loop）** 出具体实施计划（改哪些文件、怎么接 tool 循环与 streaming）。
2. 针对 **P1（TS 编排服务）** 出边界划分 + 技术选型（Vercel AI SDK vs Mastra）的对比实施计划。
3. 维持现状，仅保留本评估。

本文档仅为评估结论，不包含任何代码改动。
