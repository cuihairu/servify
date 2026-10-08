# 10 Migration Scorecard

> ⚠️ **快照注记（2026-10-04，V1.0 收敛 B0 批次）**：本记分卡为 2026-09-25 迁移期快照。此后 `services` 目录已整体移除（P3-2），自动机与统计的事件订阅已由 module delivery 装配（`internal/app/server/runtime_assembly.go:401,429`），agent 运行态已入驻 `modules/agent/infra/redis_registry.go`。当前权威快照= `docs/current-architecture.md`（2026-10-04 核验）；本表个别行（agent/automation）的 legacy service 表述仅作迁移史，不再代表现状。

本文件记录 `10-service-to-module-migration` 的当前完成度，用于支持 M5 的持续追踪。

状态定义：

- `legacy`
  - handler/runtime 仍主要依赖旧 `services/*`
- `mixed`
  - 已部分接入 `modules/*`，但 HTTP 或 runtime 主入口尚未收口
- `contracted`
  - handler-facing 或 runtime-facing 主入口已收口到 `modules/*/delivery`
- `stabilized`
  - 已收口，且有 CI / 文档 / 兼容职责规则持续守护

判定补充：

- 若某能力已经收口，但尚未进入 `scripts/module-boundaries.rules`，不能标记为 `stabilized`
- 若文档已声明收口，但 CI 规则尚未覆盖，最多只能标记为 `contracted`

## Scorecard

| Capability | Handler Entry | Runtime Entry | Legacy Service Role | Status | Notes |
| --- | --- | --- | --- | --- | --- |
| `ticket` | `modules/ticket/delivery.HandlerService` | `ticketdelivery.NewHandlerServiceWithDependencies(...)` | legacy service removed | `stabilized` | handler/router/runtime 已收口，旧 `services.TicketService` 已删除，工单 orchestration 由 module delivery 直接装配 |
| `agent` | `modules/agent/delivery.HandlerService` | `services.AgentService` as contract impl | compatibility facade + runtime state holder | `stabilized` | handler DTO 与 transfer runtime contract 已回收到 module delivery；运行态状态已入驻 `modules/agent/infra/redis_registry.go`（2026-10-04 快照核验，不再在 legacy service）；runtime surface 收窄已纳入边界脚本守护 |
| `analytics` | `modules/analytics/delivery.HandlerService` | `services.StatisticsService` as contract impl | compatibility facade + event bus glue | `stabilized` | DTO 已抽到 module contract，handler 不再直连 concrete service |
| `routing / session transfer` | `modules/routing/delivery.HandlerService` | `modules/routing/delivery.NewHandlerService(...)` | legacy service removed | `stabilized` | session 读取/assignment/system message 已走 conversation runtime adapter，waiting queue/transfer record 已走 routing module，ticket assignment 已走 ticket module runtime adapter，agent load 调整已走 agent module runtime adapter；websocket 通知依赖已收窄为 notifier 接口，主运行时已不再经过 `services.SessionTransferService` |
| `conversation / websocket runtime` | n/a | `modules/conversation/delivery.WebSocketMessageWriter` | runtime connection hub | `stabilized` | 主 runtime 与 lightweight realtime runtime 都已走 adapter 注入；`WebSocketHub` 的 DB 直写 fallback 已删除 |
| `ai` | `modules/ai/delivery.HandlerService` | `AIAssembly.RuntimeService` | narrow runtime contract for websocket/router | `stabilized` | handler 主路径已切到 orchestrated enhanced AI；assembly 不再暴露 legacy concrete AI service，旧 `AIServiceInterface` 已删除 |
| `customer` | `modules/customer/delivery.HandlerService` | `modules/customer/delivery.NewHandlerService(db)` | compatibility facade + DTO mapping for old callers | `stabilized` | handler/router/runtime 主路径已直接走 module delivery，集成测试与边界脚本已守护 |
| `automation` | `modules/automation/delivery.HandlerService` | `modules/automation/delivery.NewHandlerService(db)` | compatibility facade + event bus glue | `stabilized` | handler/router/runtime 主路径已直接走 module delivery；subscriber 已由 module delivery 装配（`runtime_assembly.go:389`，2026-10-04 快照核验） |
| `knowledge` | `modules/knowledge/delivery.HandlerService` | `modules/knowledge/delivery.NewHandlerServiceWithProvider(db, ...)` | compatibility facade retained for old callers | `stabilized` | handler/router/runtime 的主路径已走 module delivery；index job 仓储与 knowledge provider 装配已纳入 runtime 主路径，并进入边界脚本守护，运行时按 Dify 优先、WeKnora 兼容装配 |
| `suggestion` | `modules/suggestion/delivery.HandlerService` | `modules/suggestion/delivery.NewHandlerService(db)` | legacy service removed | `stabilized` | handler/router/runtime 主路径已切到 module delivery；旧 `services.SuggestionService` 已删除，helper 单元测试与 DB 集成测试已下沉到 module application / delivery |
| `gamification` | `modules/gamification/delivery.HandlerService` | `modules/gamification/delivery.NewHandlerService(db)` | legacy service removed | `stabilized` | handler/router/runtime 主路径已切到 module delivery；旧 `services.GamificationService` 已删除，评分 / 徽章单元测试与 DB 集成测试已下沉到 module application / delivery |
| `voice` | already module coordinator | module coordinator | not primarily legacy | `mixed` | 已较模块化，但不属于本轮 `services -> modules` 的典型迁移样式 |

## 当前结论

- `ticket`、`agent`、`analytics`、`routing`、`conversation runtime`、`ai`、`customer`、`automation`、`knowledge` 已具备持续守护条件
- `customer` 的旧 facade 仅保留历史调用兼容与 DTO 映射职责
- `automation` 的旧 facade 仅保留 event bus subscriber 与少量兼容 glue
- `knowledge` 已完成 handler/runtime 收口，索引任务仓储与 provider 集成已纳入 module delivery 主路径
- 一批尚未完全模块化的薄 handler 能力已完成 `app/server` 装配面收口：`satisfaction`、`macro`、`app integration`、`custom field`、`shift`
- `suggestion` 已完成 handler/runtime 收口，旧 `services.SuggestionService` 已删除，helper 与集成测试已下沉到 module 层
- `gamification` 已完成 handler/runtime 收口，旧 `services.GamificationService` 已删除，评分 / 徽章逻辑与测试已下沉到 module 层
- `message router` 已完成 runtime/router/handler 装配面收口，保留 concrete 生命周期实现于 `services.MessageRouter`
- `sla` 已完成 handler/runtime/router 的装配面收口，但 service 本体仍保留 automation glue 与 runtime 组装职责
- `realtime runtime` 已去掉 `WebSocketHub` / `WebRTCService` 的顶层 concrete 暴露，仅保留 gateway contract 与内部启动细节
- 上述 `stabilized` 条目都已进入 `scripts/module-boundaries.rules`，其中 `customer` / `automation` / `knowledge` 的 handler 集成测试也已切到 module delivery 主路径
- 对已收口能力，旧 `services/*` 若无保留价值应直接删除，而不是长期保留 facade
- 下一阶段重点不再是“再找一个模块收口”，而是：
  - 扩大 scorecard 覆盖范围
  - 持续压缩剩余 `legacy` 状态条目，并补齐 `knowledge` 的 indexing/runtime 策略
  - 为 `legacy` 条目明确冻结策略与退役条件
