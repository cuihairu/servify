# 10 Migration Inventory

> 状态（2026-10-04 文档对账）：迁移已完成——`internal/services` 目录已整体移除，业务能力全部收口 `internal/modules/*`（27 个一级模块封顶）。本文是 M1 盘点阶段的迁移史档案，各能力「已完成收口」的结论已兑现。

本文件记录 `handlers -> services -> modules` 的迁移现状（M1 盘点快照），用于支持 `10-service-to-module-migration` 的 M1 盘点阶段。

## 当前观察结论

- `ticket`
  - 已具备较强的模块化基础
  - `handlers/ticket_handler.go` 通过 `ticketHandlerService` 接口工作
  - 已存在模块交付适配层 `modules/ticket/delivery/handler_adapter.go`
  - runtime 已直接改用 `modules/ticket/delivery.NewHandlerServiceWithDependencies(...)`
  - 旧 `services/TicketService` 已删除
  - 当前规则：HTTP handler 应依赖 `modules/ticket/delivery.HandlerService`
  - 结论：`ticket` 已完成主路径收口，并采用删除 legacy service 的策略收尾

- `agent`
  - `services/AgentService` 已明显成为兼容层
  - 核心读写已走 `modules/agent/application.Service`
  - handler-facing DTO 与 transfer runtime contract 已回收到 `modules/agent/delivery`
  - `agentQueues` 已移除；剩余在线客服兼容缓存回填、后台清理调度与 service 装配已拆成独立 runtime helper/adapter/assembly，且后台任务只在 server runtime 装配点显式启动
  - `workspace` / `session transfer` 等下游 service 已改为依赖窄接口，而不再直接绑定 `*AgentService`
  - `app/server` runtime/router 也不再对外暴露 `*services.AgentService` 字段，只保留 handler-facing 与业务专用依赖
  - `workspace handler` 与 `websocket hub` 进一步切到工作台/转接接口，`app/server` 对 `WorkspaceService` / `SessionTransferService` 的 concrete 暴露继续收缩
  - 当前规则：HTTP handler 应依赖 `modules/agent/delivery.HandlerService`
  - 结论：属于“module 已落地，但 runtime 兼容状态尚未收口”的类型

- `analytics`
  - `services/StatisticsService` 基本是 module facade
  - 核心统计能力已走 `modules/analytics/application.Service`
  - 还保留事件总线订阅和 DTO 映射的兼容包装
  - `app/server` runtime 已不再对外暴露 `*services.StatisticsService` 字段，仅保留 handler-facing contract 与局部 wiring
  - 结论：迁移风险低，适合作为后续收口样板

- `routing / session transfer`
  - 主运行时已不再装配 `services/SessionTransferService`
  - handler/runtime 已直接改用 `modules/routing/delivery.HandlerServiceAdapter`
  - session assignment 同步已改走 `modules/conversation/delivery` runtime adapter，不再直接更新 `Session`
  - transfer / waiting 的系统消息写入已改走 `modules/conversation/delivery` runtime adapter，不再直接写 `Message`
  - waiting queue 的读取、新增、取消、查询、转态同步，以及 transfer record 写入已收口到 routing module 状态机
  - waiting -> transferred 与 transfer record 写入已纳入同一事务，不再依赖事务后的补写同步
  - 取消 waiting queue 时的系统消息写入也已切到 `modules/conversation/delivery` runtime adapter，减少 legacy `Message` 直写分支
  - 会话转接触发的 ticket assignment 同步已改走 `modules/ticket/delivery` runtime adapter，不再直接写 `Ticket` / `TicketStatus`
  - agent load 调整已改走 `modules/agent/delivery` runtime adapter，原先由 handler contract 反向依赖 `services` 引起的 import cycle 已被移除
  - websocket 通知依赖已收窄为 `sessionTransferNotifier`，不再把 `WebSocketHub` 作为 service 内部 concrete 字段保存
  - 会话读取入口已切到 `modules/conversation/delivery.RuntimeService.LoadTransferSession(...)`
  - 默认装配下 `LoadTransferSession(...)` 的 adapter 错误会直接上抛，不再静默回退到 legacy DB 查询，避免掩盖 conversation runtime 故障
  - 加入等待队列时的会话 active/unassigned 同步也已切到 `modules/conversation/delivery.RuntimeService.SyncWaitingAssignment(...)`
  - websocket 通知已收窄为 notifier 接口，不再把 `WebSocketHub` 作为内部 concrete 依赖保存
  - 旧 `services/SessionTransferService` 已删除；残留的 `services/session_transfer_legacy_store.go`（DB 兼容读写孤儿，无生产调用方）也已删除
  - 结论：`routing / session transfer` 已完成主路径收口，并采用删除 legacy service 的策略收尾

- `ai`
  - 旧 `services/AIService` 与增强 AI 实现仍存在，但已不再通过 `AIServiceInterface` 暴露给 handler/runtime 顶层
  - handler 已开始收口到 `modules/ai/delivery.HandlerService`
  - `AIAssembly` 已去掉未消费的 `LegacyService *services.AIService` 暴露，仅保留 handler-facing contract、runtime-facing `RuntimeService` 与 knowledge provider state
  - 旧 `services/AIServiceInterface` / `services.OrchestratedAIService` 已删除
  - `services/router.go` 与 `services/websocket.go` 只依赖局部最小 AI runtime 接口
  - 结论：`ai` 已完成 handler/runtime 主路径收口，后续重点是继续收缩 runtime 局部接口

- `legacy handler-only capabilities`
  - `satisfaction`、`csat public`、`macro`、`app integration`、`custom field`、`shift` 等能力尚未模块化
  - 这批 handler 现在已改为依赖 `handlers` 包内定义的最小接口，而不再把 `*services.*` concrete type 暴露到 `app/server` runtime/router surface
  - 结论：虽然还不属于 `services -> modules` 迁移完成项，但已完成一轮“装配面收口”，可减少 concrete legacy service 在顶层 runtime 的扩散

- `message router`
  - 轻量 runtime 与主 runtime 都还需要 `Start/Stop` 生命周期，但 HTTP 层只关心平台统计读取
  - 现已抽出 `services.MessageRouterRuntime`，`app/server` 与 `handlers.MessageHandler` 不再暴露 concrete `*services.MessageRouter`
  - `MetricsHandler` 的无用 message router 注入已移除

- `realtime runtime internals`
  - `WebSocketHub` / `WebRTCService` 属于运行态装配细节，不应继续作为 `Runtime` / `RealtimeRuntime` 的公开 concrete 字段暴露
  - 现已收口为内部 `Run()` 启动依赖与 gateway adapter 装配细节，外部只保留 `RealtimeGateway` / `RTCGateway` / `MessageRouterRuntime`

- `sla`
  - `SLAHandler` 早已在内部按最小方法集工作，但此前 `app/server` 顶层装配仍暴露 `*services.SLAService`
  - 现已把 handler contract 显式提升为 `handlers.SLAService` / `handlers.SLATicketReader`
  - `ticketdelivery.ReaderServiceAdapter` 直接复用为工单读取依赖，顶层 runtime/router 不再暴露 concrete `*services.SLAService`

- `customer`
  - `services/CustomerService` 已经是 `modules/customer/application.Service` 的轻量 facade
  - HTTP handler 只消费请求/响应 DTO 与兼容方法，核心业务已经下沉到 module application + infra repository
  - `app/server` runtime 已不再对外暴露 `*services.CustomerService` 字段，且 customer handler 装配已直接切到 `modules/customer/delivery.NewHandlerService(db)`，不再经由 legacy facade 中转
  - 结论：handler 主路径已直接贴近 module；旧 facade 主要只为历史调用者保留 DTO 兼容

- `automation`
  - `services/AutomationService` 已经把核心触发器与执行查询下沉到 `modules/automation/application.Service`
  - legacy service 主要保留 event bus subscriber 注册、测试兼容 helper 与 module 装配
  - `app/server` runtime/router 已不再对外暴露 `*services.AutomationService` 字段，且 automation handler 装配已直接切到 `modules/automation/delivery.NewHandlerService(db)`
  - 结论：handler 主路径已直接贴近 module；旧 service 主要只保留 event bus subscriber 与测试兼容 glue

- `knowledge`
  - 已存在 `modules/knowledge/application`，但此前缺少 Gorm repository 与 handler-facing delivery contract
  - 旧 `services/KnowledgeDocService` 主要承接 CRUD 与 tag/category DTO，适合改成 module facade
  - `app/server` runtime 已不再对外暴露 `*services.KnowledgeDocService` 字段，knowledge handler 装配已直接切到 `modules/knowledge/delivery.NewHandlerServiceWithProvider(db, ...)`，不再经由 legacy facade 中转
  - `modules/knowledge/infra` 已补齐 `KnowledgeIndexJob` 的 Gorm 持久化仓储，module delivery 也已支持按 runtime 注入 provider
  - 结论：knowledge 的 handler/runtime 主路径都已贴近 module；legacy facade 仅保留历史调用兼容

- `suggestion`
  - `modules/suggestion/application`、`contract`、`infra`、`delivery` 已落地
  - handler/runtime 已直接消费 `modules/suggestion/delivery.HandlerService`
  - 旧 `services/SuggestionService` 已删除，DTO alias 与 helper 已全部下沉到 module application / contract
  - 结论：`suggestion` 已完成主路径收口，并采用删除 legacy service 的策略收尾

- `gamification`
  - `modules/gamification/application`、`contract`、`infra`、`delivery` 已落地
  - handler/runtime 已直接消费 `modules/gamification/delivery.HandlerService`
  - 旧 `services/GamificationService` 已删除，评分 / 徽章逻辑与 DTO alias 已全部下沉到 module application / contract
  - 结论：`gamification` 已完成主路径收口，并采用删除 legacy service 的策略收尾

## 按迁移成熟度分组

### A. 已有明显 module facade

- `ticket`
- `agent`
- `analytics`
- `customer`
- `automation`
- `knowledge`
- `suggestion`
- `gamification`

这些模块的共同特点：

- 已有 `modules/*/application` 作为核心业务入口
- 旧 `services/*` 更多承担兼容包装、DTO 映射、runtime glue
- 适合优先定义“唯一入口”并冻结旧 service 新增逻辑

### B. 已接入 module adapter，但仍需运行态硬化

- `conversation` 相关 websocket/runtime 路径

这些模块的共同特点：

- module 已存在
- 关键持久化入口已切到 module delivery adapter
- connection hub、通知、转接协作等运行态对象仍需要持续保持窄接口
- 后续重点不是回退重迁移，而是避免在 realtime runtime 内重新引入业务直写路径

### C. 多实现并存，需要先确定默认主路径

- `voice` 等非典型 `services -> modules` 能力

特点：

- 并不完全遵循本轮 `services -> modules` 迁移模板
- 下一步更像边界收紧与 runtime assembly 整理，而不是简单替换调用点

## 推荐迁移顺序

1. `ticket`
2. `agent`
3. `analytics`
4. `customer`
5. `automation`
6. `knowledge`
7. `suggestion`
8. `gamification`
9. 持续硬化 `conversation / websocket runtime`
10. 扩大 scorecard 覆盖范围

## 当前高风险点

- 同一个业务能力同时存在 handler interface、legacy service、module service、delivery adapter 多层入口
- 旧 service 中仍混有 runtime 状态与 side effects，导致无法简单替换
- `ai` 路径下仍有多种 provider / enhanced 实现，runtime contract 如果不继续收窄仍可能重新分叉
- `conversation` 的持久化主入口已收口，但 realtime connection hub 仍是运行态核心对象，需要继续防止业务写路径回流
- 若不继续守护，未模块化但较薄的 legacy handler service 仍可能重新把 concrete type 扩散回 `app/server` runtime/router

## 下一步建议

- 持续守住 `conversation` 的运行态写路径，避免绕过 module contract
- 明确剩余 `services/*` 中哪些类型属于 runtime state holder / event bus glue / 历史兼容入口
- 为仍处于 `legacy` 的能力拆最小任务包，并同步写入 scorecard

## 已确认的入口规则

- `ticket`
  - HTTP handler 入口：`modules/ticket/delivery.HandlerService`
  - 旧 `services/TicketService` 状态：已删除
- `agent`
  - HTTP handler 入口：`modules/agent/delivery.HandlerService`
  - 旧 `services/AgentService` 定位：兼容 facade + runtime state holder
- `analytics`
  - HTTP handler 入口：`modules/analytics/delivery.HandlerService`
  - 旧 `services/StatisticsService` 定位：兼容 facade + event bus subscriber / DTO mapping glue
- `customer`
  - HTTP handler 入口：`modules/customer/delivery.HandlerService`
  - 旧 `services/CustomerService` 定位：兼容 facade + DTO mapping + module application 装配
- `automation`
  - HTTP handler 入口：`modules/automation/delivery.HandlerService`
  - 旧 `services/AutomationService` 定位：兼容 facade + event bus subscriber + module application 装配
- `knowledge`
  - HTTP handler 入口：`modules/knowledge/delivery.HandlerService`
  - runtime 装配入口：`modules/knowledge/delivery.NewHandlerServiceWithProvider(db, ...)`
  - 旧 `services/KnowledgeDocService` 定位：兼容 facade + module application / repository 装配
- `suggestion`
  - HTTP handler 入口：`modules/suggestion/delivery.HandlerService`
  - runtime 装配入口：`modules/suggestion/delivery.NewHandlerService(db)`
  - 旧 `services/SuggestionService` 状态：已删除
- `gamification`
  - HTTP handler 入口：`modules/gamification/delivery.HandlerService`
  - runtime 装配入口：`modules/gamification/delivery.NewHandlerService(db)`
  - 旧 `services/GamificationService` 状态：已删除
- `routing / session transfer`
  - HTTP handler 入口：`modules/routing/delivery.HandlerService`
  - 旧 `services/SessionTransferService` 状态：已删除
- `conversation / websocket runtime`
  - websocket 持久化入口：`modules/conversation/delivery.WebSocketMessageWriter`
  - `services/WebSocketHub` 定位：runtime connection hub，不再自定义 conversation 私有持久化接口
- `ai`
  - HTTP handler 入口：`modules/ai/delivery.HandlerService`
  - `AIAssembly` 定位：显式区分 handler-facing AI contract 与 runtime-facing `RuntimeService`

## 已确认的兼容职责边界

- `services/AgentService`
  - 允许保留：旧调用方兼容入口、少量兼容方法
  - 不应新增：新的 HTTP handler 直接依赖、脱离 `modules/agent/application.Service` 的核心业务写路径
- `services/StatisticsService`
  - 允许保留：旧调用方兼容入口、event bus 订阅注册、DTO 映射、统计后台任务调度
  - 不应新增：新的 HTTP handler 直接依赖、绕过 `modules/analytics/application.Service` 的主统计读写路径
- `services/CustomerService`
  - 允许保留：旧调用方兼容入口、DTO 映射、`modules/customer/application.Service` 与 repository 的装配
  - 不应新增：新的 HTTP handler 直接依赖、绕过 `modules/customer/application.Service` 的核心客户读写路径
- `services/AutomationService`
  - 允许保留：旧调用方兼容入口、event bus subscriber 注册、测试辅助方法、`modules/automation/application.Service` 的装配
  - 不应新增：新的 HTTP handler 直接依赖、绕过 `modules/automation/application.Service` 的触发器和执行记录主入口
- `services/KnowledgeDocService`
  - 允许保留：旧调用方兼容入口、DTO 映射、`modules/knowledge/application.Service` 与 repository 的兼容装配
  - 不应新增：新的 HTTP handler 直接依赖、绕过 `modules/knowledge/application.Service` 的文档 CRUD 主入口
- `services/WebSocketHub`
  - 允许保留：连接管理、广播、协议消息分发、对 AI/transfer 的 runtime glue
  - 不应新增：新的 conversation 私有持久化接口、绕过 `modules/conversation/delivery.WebSocketMessageWriter` 的消息落库路径
- `services` 下 legacy AI types
  - 允许保留：provider 实现、增强链路、局部 runtime 协调
  - 不应新增：新的 HTTP handler 直接依赖、重新暴露全局 `AIServiceInterface` 一类宽接口

## 当前自动化守护

- `scripts/check-module-boundaries.sh`
  - 校验 `ticket` / `agent` / `analytics` / `customer` / `automation` / `knowledge` / `routing` / `ai` 的 handler constructor 必须依赖 `modules/*/delivery.HandlerService`
  - 校验 router/runtime 对这八个模块的注入类型必须停留在 handler-facing contract，并校验 `knowledge` runtime 必须直接通过 module delivery 装配，以及 `conversation` 的 websocket persistence 入口必须走 module delivery adapter
  - 校验 `satisfaction` / `macro` / `app integration` / `custom field` / `shift` 等薄 handler 依赖必须停留在 handler-local contract，避免 `app/server` 顶层装配回退暴露 concrete legacy service
  - 校验 `suggestion` / `gamification` 的 handler/runtime 入口必须停留在 module delivery contract
  - 校验 `workspace` / `websocket transfer` 等新增收窄点必须依赖 `WorkspaceOverviewReader`、`SessionTransferRuntime` 等接口，并禁止 `app/server` runtime/router 回退暴露若干 concrete legacy service
  - 目的：先锁住已完成迁移的入口，避免回退到 handler 直连具体旧 service
