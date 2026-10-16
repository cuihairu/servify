# 10 Module Boundaries

> 状态（2026-10-04 文档对账）：迁移完成后 `services` 层已不存在，本文的三层
> 职责表述按迁移期档案理解；当前依赖方向以
> [current-architecture.md](../current-architecture.md) 为准，可执行规则仍是
> `scripts/module-boundaries.rules`（持续生效）。

本文件定义 `handlers`、`services`、`modules` 三层在迁移阶段的职责与依赖方向，用于支持 M5 的边界治理。

可执行规则来源：

- [scripts/module-boundaries.rules](../../scripts/module-boundaries.rules)
  - 这是当前边界检查的规则清单
- [scripts/check-module-boundaries.sh](../../scripts/check-module-boundaries.sh)
  - 这是规则执行器

维护约定：

- 新增一个已收口模块时，优先更新 `scripts/module-boundaries.rules`
- 文档中的“当前已锁定的边界”应与规则清单保持一致
- 如果文档与规则冲突，以规则清单和 CI 结果为准，再回补文档

## 目标规则

- `handlers/*`
  - 只依赖 `modules/*/delivery` contract、少量平台接口、请求响应 DTO
  - 不直接依赖 concrete legacy service 作为默认业务入口
- `app/server/*`
  - 负责 runtime assembly
  - 可以持有 legacy concrete service，但对 HTTP 注入时应优先暴露 `modules/*/delivery` contract
  - 路由注册与 runtime 装配应按领域或接入面拆分文件，避免继续集中回单个超大入口文件
- `services/*`
  - 只在确有未迁出 runtime glue、状态同步、side effect 组装时临时保留
  - 已收口且不再承担必要职责时应直接删除，而不是继续增长为默认业务中心
- `modules/*/application`
  - 承担新的核心业务逻辑
- `modules/*/delivery`
  - 承担 handler-facing / runtime-facing adapter 与 contract

## 允许的依赖方向

- `handlers -> modules/*/delivery`
- `app/server -> modules/*/delivery`
- `app/server -> services/*`
- `services/* -> modules/*/application`
- `services/* -> modules/*/delivery`
- `modules/*/delivery -> modules/*/application`
- `modules/*/application -> modules/*/domain|infra`

## 禁止继续扩散的模式

- `handlers -> *services.ConcreteType`
- `handlers -> gorm.DB`
- `handlers -> modules/*/application` 直接拼业务
- `services/*` 为了新需求再次变成 handler 默认入口
- runtime 新增一条与既有 module contract 平行的 legacy 入口
- 在 `app/server` 重新堆积跨域装配逻辑到单个 `router.go` 或 `runtime.go`

## 当前已锁定的边界

- `ticket`
  - HTTP 入口必须走 `modules/ticket/delivery.HandlerService`
- `agent`
  - HTTP 入口必须走 `modules/agent/delivery.HandlerService`
- `analytics`
  - HTTP 入口必须走 `modules/analytics/delivery.HandlerService`
- `routing / session transfer`
  - HTTP 入口必须走 `modules/routing/delivery.HandlerService`
- `ai`
  - HTTP / health / metrics 入口必须走 `modules/ai/delivery.HandlerService`
- `customer`
  - HTTP 入口必须走 `modules/customer/delivery.HandlerService`
- `automation`
  - HTTP 入口必须走 `modules/automation/delivery.HandlerService`
- `knowledge`
  - HTTP 入口必须走 `modules/knowledge/delivery.HandlerService`
- `suggestion`
  - HTTP 入口必须走 `modules/suggestion/delivery.HandlerService`
- `gamification`
  - HTTP 入口必须走 `modules/gamification/delivery.HandlerService`
- `conversation`
  - websocket persistence 入口必须走 `modules/conversation/delivery.WebSocketMessageWriter`
- handler-local contracts
  - `satisfaction`、`macro`、`app integration`、`custom field`、`shift`、`sla` 等尚未完全模块化的薄能力，HTTP handler 与 router/runtime surface 必须停留在 handler-local contract，避免回退到 concrete legacy service

对应规则：

- `handler` / `runtime` 规则记录在 `scripts/module-boundaries.rules`
- `conversation` 的 websocket persistence 与 `AIAssembly` 的双入口规则也记录在同一规则文件
- `scripts/check-module-boundaries.sh` 额外对非测试 handler 做 import 扫描，禁止直连 `modules/*/application`、`modules/*/infra`、`gorm`

## Legacy Service Freeze Policy

- 已进入 `stabilized` 的能力：
  - legacy `services/*` 只允许必要的 runtime glue、状态同步、side effect 组装类修改
  - 新业务逻辑优先进入 `modules/*/application`
  - 新的 handler/router wiring 不得回退到 concrete service
  - 若某个 legacy service 已无必要职责，应优先删除，不保留空壳 facade
- 仍处于 `legacy` 的能力：
  - 允许临时修改，但每次新增业务逻辑都应同步标注目标 module 归属

## Retirement 条件

某个 legacy service 可以进入“退役候选”状态，至少应满足：

1. HTTP handler 已全部改为依赖 `modules/*/delivery`
2. runtime 注入已不再需要 concrete legacy type 暴露给 router
3. 关键兼容职责已被 adapter 或 platform layer 吸收
4. 有测试覆盖迁移前后的关键行为
5. scorecard 状态达到 `stabilized` 并持续一段时间无回退
