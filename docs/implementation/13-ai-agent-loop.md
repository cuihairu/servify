# 13. AI Agent Loop 接通（P0）

> 状态：**主循环已接通（2026-10-04 文档对账核实）**——`query_orchestrator.go` 的
> `Handle` 已走 `handleWithTools` agent 循环（`Chat → 执行工具 → 回灌 → 重复`，
> `maxSteps` 上限），流式 `streamInteraction` 每步走 `ChatStream` 并经 WS 推送；
> 生产装配（`modules/ai/delivery/enhanced_service.go`）已注册
> `customer_lookup` / `ticket_lookup` / `handoff` 三工具并注入 `ToolExecutor`，
> `AIRequest` 已带 `ToolPolicy{Enabled:true, MaxSteps:5}`。
> **遗留**：工具注册时 Port 与 PermissionChecker 传 `nil`（工具调用会以
> “port 未配置”降级），真实数据端口与权限校验接入仍是后续项；转人工关键词
> 短路仍保留为兜底。
> 关联：[架构评估 2026](../architecture-review-2026.md) · [02 AI 与知识](./02-ai-and-knowledge.md) · [10 模块边界](./10-module-boundaries.md)

## 背景与动机

评估结论（见 [architecture-review-2026](../architecture-review-2026.md)）：当前 AI 编排是**单轮 RAG**，`tool` 系统已实现但**未接入主循环**，"转人工"靠关键词启发式。本任务把 agent loop 接通，让智能客服能**多步行动**（查客户 / 查工单 / 转人工 / 追问）——这是产品从"问答机器人"升级到"智能客服"的关键一步。

## 目标 / 非目标

**目标**
- `Handle` 支持 tool-calling 循环（`stopWhen` 等价，带 `maxSteps` 上限防失控）
- 接入 3 个已实现工具：`customer_lookup` / `ticket_lookup` / `handoff`
- 流式（`ChatStream`）接到 WebSocket（SDK 已是 ws-first）

**非目标**
- 不引入新 agent 框架（本阶段自行开发 minimal loop；Eino 迁移留 P1 评估）
- 不动业务 / 数据 / 路由 / 语音模块

## 现状（接线点 · 2026-10-04 对账核实，原「计划（未实施）」快照已过时）

| 关注点 | 位置 | 现状 |
|---|---|---|
| 编排核心 | `internal/modules/ai/application/query_orchestrator.go::Handle` | **已接循环**：`handleWithTools`（Chat → 执行工具 → 回灌 → 重复，`maxSteps` 上限） |
| 工具脚手架 | 同目录 `tools.go`(`ToolRegistry`/`ToolExecutor`)、`tool_customer_lookup.go`、`tool_ticket_lookup.go`、`tool_handoff.go` | **已实现并注册进生产 registry**；Port/PermissionChecker 暂为 `nil`，调用降级报“port 未配置” |
| 装配 / 调用 | `internal/modules/ai/delivery/enhanced_service.go`（装配） | `AIRequest` 已带 `ToolPolicy{Enabled:true, MaxSteps:5}`，`SetToolExecutor` 已注入 |
| 转人工 | `internal/modules/ai/application/handoff_policy.go::ShouldTransferToHuman` | 关键词启发式（"人工/客服/投诉"），保留为兜底短路 |
| 流式 | `internal/modules/ai/application/query_orchestrator.go::streamInteraction` | **已接**：每步 `ChatStream`，chunk 经 `modules/ai/delivery/streaming.go` → WS 推送 |
| 触发入口 | `internal/platform/realtime/websocket_hub.go`(实时) · REST `/api/v1/ai/query` | 均走 `ProcessQuery` / `ProcessQueryStream` |

## 实施步骤

### 阶段 1：非流式 agent loop（先打通闭环）
1. **orchestrator 接收 `ToolExecutor`**：`query_orchestrator.go` struct 加字段；`NewQueryOrchestrator` 加参数（或 `SetToolExecutor`）；`Handle` 构造 `ChatRequest` 时注入 `ToolDefinition`（`registry.List()` → `llm.ToolDefinition`）。
2. **`Handle` 加循环**：`retrieve → buildPrompt → loop{ Chat(带 tools) → 若 `ToolCalls` 空 break → 逐个 `ToolExecutor.Execute` → 结果作为 `tool` 角色消息回灌 → 再 Chat }`，`maxSteps` 上限（默认 5）；每轮打 span + metrics。
3. **装配接线**（`orchestrated_ai_enhanced.go`）：构造 `ToolRegistry` 注册 3 工具（Port 接 `CustomerService` / `TicketService` / `HandoffPort.RequestHandoff`）；构造 `ToolExecutor`（带 `PermissionChecker`）；`AIRequest` 设 `ToolPolicy.Enabled=true, AllowedTools=[...]`；把开头 `ShouldTransferToHuman` 关键词短路**降级为兜底**（LLM 未调 handoff 工具时）。

### 阶段 2：流式
4. 新增 `HandleStream`（`ChatStream`，chunk 累积 `tool_call`，执行后回灌），chunk 经 WS 推客户端。
5. 开启 `capabilities` `ChatStream` `Enabled`。
6. `websocket.go` 入口改用 `HandleStream`；REST 保留非流式 `Handle`。

### 阶段 3：测试
7. orchestrator 单测：mock LLM 返回 `tool_call` → 验证执行 / 回灌 / 二轮收敛；`maxSteps` 上限；tool disabled 不走 loop。
8. 集成：mock provider 跑一次多步对话 + 流式逐字呈现。

## 选型说明（自行开发 vs Eino）

- **P0 自行开发 minimal loop**：最轻、复用现有 `ToolExecutor` / `LLMProvider` 抽象、2–3 周。
- **P1 可评估迁 [Eino](https://github.com/cloudwego/eino)**（"Go 版 LangGraph"）：获得 workflow 可视化 / durable / ADK，代价是多一层框架依赖 + 学习成本。
- **决策依据**：先自行开发验证闭环与产品价值，再决定是否值得换框架。

## 风险与回退

- tool Port 注入需接业务 service（`CustomerService` / `TicketService`）——确认接口已存在或补薄适配。
- 流式 + tool 状态机较复杂，**先非流式后流式**。
- 全程保留现有 fallback / circuit breaker / metrics。
- **回退**：`ToolPolicy.Enabled=false` 即退回单轮 RAG（零行为变化）。

## 验收标准

- 客服可经 tool 多步完成"查工单 → 回答"等场景。
- `maxSteps` 触发时安全终止并给降级回复。
- 流式回答在 WS 客户端逐字呈现。
- 现有 AI 单测 / 集成测试全绿；新增 loop 测试覆盖。

## 工作量

2–3 周（与评估一致）。
