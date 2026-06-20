# 10 Service To Module Migration

范围：

- 旧 `services` / `handlers` 收口
- `modules/*` 应用入口固化
- legacy service 退役与删除
- 迁移路线与完成度治理

## M1 migration-inventory

- [x] 盘点 `handlers -> services -> repo` 旧链路
- [x] 盘点 `delivery -> application -> domain -> infra` 新链路覆盖情况
- [x] 为每个领域模块绘制当前入口与目标入口映射
- [x] 标出高风险交叉依赖、共享模型、循环引用点

验收：

- 每个核心能力都能回答“当前走哪条链路，目标走哪条链路”

产出：

- [10-migration-inventory.md](./10-migration-inventory.md)

## M2 module-entry-contracts

- [x] 为 `conversation`、`routing`、`ticket`、`analytics`、`ai` 定义唯一应用入口
- [x] 明确 handler 只能依赖 adapter / application contract，不能直接拼业务
- [x] 为旧 service 定义允许保留的兼容职责
- [x] 为 repo / gorm 依赖建立单向约束

验收：

- 新增需求不会再同时落入旧 service 和新 module 两套实现

当前已落地：

- `ticket`
  - handler-facing contract 已收口到 `modules/ticket/delivery.HandlerService`
  - runtime 已改为 `modules/ticket/delivery.NewHandlerServiceWithDependencies(...)`
  - 旧 `services/TicketService` 已删除
- `agent`
  - handler-facing contract 已收口到 `modules/agent/delivery.HandlerService`
  - runtime 仍复用 `services.AgentService` 作为 contract 实现
  - `services.AgentService` 明确保留为兼容 facade + runtime state holder
- `analytics`
  - handler-facing contract 已收口到 `modules/analytics/delivery.HandlerService`
  - runtime 仍复用 `services.StatisticsService` 作为 contract 实现
  - `services.StatisticsService` 明确保留为兼容 facade + event bus glue
- `routing / session transfer`
  - handler-facing contract 已收口到 `modules/routing/delivery.HandlerService`
  - runtime 已改为 `modules/routing/delivery.HandlerServiceAdapter`
  - 旧 `services.SessionTransferService` 已删除
- `conversation / websocket runtime`
  - websocket 持久化入口已显式收口到 `modules/conversation/delivery.WebSocketMessageWriter`
  - 主 runtime 与 lightweight realtime runtime 都通过 `WebSocketMessageAdapter` 注入 conversation 模块
  - `WebSocketHub` 已删除对 `sessions/messages` 的 gorm 直写 fallback，只保留 conversation delivery 路径
- `ai`
  - handler-facing contract 已收口到 `modules/ai/delivery.HandlerService`
  - `AIAssembly` 已拆分为 handler-facing service 与 runtime-facing service
  - 默认 handler 主路径已提升到 orchestrated enhanced AI，而不是直接暴露旧 `AIServiceInterface`
  - server/runtime/router/websocket 主链路已改为依赖 `modules/ai/delivery.RuntimeService` 或局部 runtime contract，不再透传 `services.AIServiceInterface`
  - 旧 `services/AIServiceInterface` / `EnhancedAIServiceInterface` 已删除
- `suggestion`
  - handler-facing contract 已收口到 `modules/suggestion/delivery.HandlerService`
  - runtime 已改为 `modules/suggestion/delivery.NewHandlerService(db)`
  - 旧 `services.SuggestionService` 已删除，DTO alias 与 helper 已下沉到 module application / contract
- `gamification`
  - handler-facing contract 已收口到 `modules/gamification/delivery.HandlerService`
  - runtime 已改为 `modules/gamification/delivery.NewHandlerService(db)`
  - 旧 `services.GamificationService` 已删除，评分 / 徽章逻辑与 DTO alias 已下沉到 module application / contract
- 边界守护
  - CI 已增加 `scripts/check-module-boundaries.sh`
  - 当前自动校验 `ticket` / `agent` / `analytics` / `customer` / `automation` / `knowledge` / `routing` / `ai` / `suggestion` / `gamification` 的 handler constructor、router dependency、runtime wiring，以及 `conversation` 的 websocket persistence wiring，都必须停留在 module delivery contract

## M3 legacy-service-retirement

- [x] 为仍需过渡的运行态入口补窄接口或 adapter，而不是继续保留 handler 直连旧 service
- [x] 将旧 service 中可迁移的业务规则搬到 module application / delivery 层
- [x] 已收口能力直接删除 legacy service，而不是保留 facade
- [x] 为适配层增加测试，确保迁移前后行为一致

验收：

- 已完成收口的能力不再保留双入口；仍未完成的能力也有明确的冻结边界

当前已落地：

- `routing`
  - 已为 `modules/routing/delivery.SessionTransferAdapter` 补 waiting lifecycle、assignment/history、transaction-scoped repository 行为测试
  - handler/runtime 主路径已切到 `modules/routing/delivery.HandlerServiceAdapter`
  - 旧 `services/SessionTransferService` 已删除，不再保留兼容 facade
- `ticket`
  - handler/runtime 主路径已切到 `modules/ticket/delivery.NewHandlerServiceWithDependencies(...)`
  - 旧 `services/TicketService` 已删除，不再保留兼容 facade
- `ai`
  - handler/runtime 主路径已切到 `modules/ai/delivery.HandlerService` 与 `AIAssembly.RuntimeService`
  - 旧 `services/AIServiceInterface` / `services.OrchestratedAIService` 已删除，runtime 只保留局部窄接口
- `conversation`
  - `WebSocketHub` 已删除对 `sessions/messages` 的 gorm 直写 fallback
  - websocket 持久化只允许经过 `modules/conversation/delivery.WebSocketMessageWriter`

## M4 core-module-migrations

- [x] 优先迁移 `conversation`
- [x] 优先迁移 `routing`
- [x] 优先迁移 `ticket`
- [x] 优先迁移 `ai`
- [x] 为每个模块拆分 1 到 3 个可独立提交的小任务包

验收：

- 核心闭环模块率先完成收口，后续模块可复用迁移模式

建议任务包：

- `conversation`
  - C1: 将 websocket message persistence 之外的会话写路径继续收口到 `modules/conversation/application`
  - C2: 继续压缩 `WebSocketHub` 内部的会话/协议编排，避免重新长出 conversation 私有写路径
- `routing`
  - R1: 将 waiting queue / assignment / cancel 的运行态编排继续下沉到 `modules/routing/application`
  - R2: 为 routing runtime adapter 补迁移前后行为一致性测试
- `ticket`
  - T1: 继续为 module delivery orchestration 补跨模块行为测试
  - T2: 审查是否还有遗留注释、文档或调用点引用已删除的 `TicketService`
- `ai`
  - A1: 将 websocket / router 仍共享的 runtime surface 继续切薄为显式 contract
  - A2: 为 AI handler/runtime 双入口补回归测试，锁定 assembly 边界

## M5 boundary-enforcement-and-scorecard

- [x] 增加迁移完成度表
- [x] 为目录职责与依赖方向补文档
- [x] 在 code review / CI 中增加边界约束检查
- [x] 明确 `services/*` 的冻结策略与退役条件

验收：

- 迁移工作可追踪、可量化，不会长期停留在“正在重构”

产出：

- [10-migration-scorecard.md](./10-migration-scorecard.md)
- [10-module-boundaries.md](./10-module-boundaries.md)

## M6 app-server-assembly-hardening

- [x] 将 `app/server/router.go` 按 auth / management / public / realtime 拆分
- [x] 将 `app/server/runtime.go` 拆为可组合的分阶段装配函数
- [x] 保持现有行为不变，并通过 `app/server` 与 `app/bootstrap` 测试验证

验收：

- `app/server` 不再依赖单个超大装配文件承载全部启动复杂度
- 新增模块或新接入面时，有明确的装配落点，而不是继续堆进总入口
