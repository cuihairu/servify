# 当前架构分析

本文记录当前仓库的真实架构状态，用于衔接根目录 `ARCHITECTURE.md` 中的目标设计和 `docs/implementation/` 下的实施 backlog。

> 快照核验：2026-10-04（V1.0 收敛改造 B0 批次建档；`v1.0.0` 发布后复核更新，statistics 收口状态、依赖图、customer 模块分层与当日代码对齐）。上一版本称「services 目录仍存在」已过时：旧 `internal/services` 已于 P3-2 整体移除并清零引用，业务逻辑或随迁移下沉到 `modules/*/application`、或以薄壳模块（无 `domain` 层）形式存在。

## 结论

当前 Servify 是一个**已基本完成 services→modules 迁移的模块化单体**：

- 后端仍是单进程优先的 Go modular monolith，不是微服务架构。
- 主入口已经从 `cmd/server` 下沉到 `internal/app/bootstrap` 与 `internal/app/server`。
- 业务能力已全部收口到 `modules/*`，其中 14 个模块具备完整 `domain/application/infra/delivery`（agent、ai、analytics、assist、automation、conversation、customer、knowledge、quality、routing、ticket、translation、voice、webhook），另有 13 个薄壳模块（无 `domain` 层：api_key、app_integration、auth、custom_field、email、gamification、macro、push、satisfaction、shift、sla、suggestion、workspace），其业务逻辑仍以 legacy handler/service 承载。
- `handlers` 大多已经依赖 module delivery contract 或 handler-local contract，而不是直接依赖 concrete legacy service。
- `statistics` 已收口进 `analytics` 模块（V1.0 B2-3，`modules/analytics/delivery/statistics_*.go`），顶层 `internal/handlers` 不再保留 statistics 业务面（仅剩迁移来源的单元测试文件）。

换句话说，当前系统不是纯目标态，也不是旧架构；它是有边界守护的迁移后架构。V1.0 收敛改造（B0–B4）已于 2026-10-04 完成并发布 `v1.0.0`：27 个模块不再增加，核心 7 模块（conversation/ticket/routing/ai/knowledge/customer/agent）围绕 Conversation 中心模型打磨，13 个薄壳模块叙事降级为子能力。

## 仓库形态

主要目录职责如下：

| 路径 | 当前职责 |
| --- | --- |
| `apps/server` | Go 后端，当前架构重设计的主战场 |
| `apps/admin` | 管理端，UmiJS + Ant Design Pro |
| `apps/admin-legacy` | 旧静态管理端，保留兼容和演示用途 |
| `apps/website` | 官网静态站点 |
| `sdk` | TypeScript SDK workspace，已拆 core、transport、framework binding |
| `docs` | 文档站、架构说明、验收、实施 backlog、运行手册 |
| `infra` / `deploy` | 本地 compose、观测性、部署辅助资产 |
| `scripts` | 本地与 CI 检查、验收脚本、生成物治理 |

## 后端运行时

当前 HTTP 后端启动链路可以按四层理解：

1. `apps/server/cmd/server`
   - 保留为薄入口。
   - 负责启动参数、错误退出、启动顺序和 graceful shutdown。

2. `internal/app/bootstrap`
   - 构建 config、logger、database、Redis、event bus、embedding provider。
   - 管理 worker、HTTP runtime、server 与 shutdown hooks。
   - 是进程级依赖的 bootstrap root。

3. `internal/app/server`
   - 构建 HTTP runtime service graph。
   - 负责 AI、realtime、conversation、routing、voice、业务 handler service、metrics 的装配。
   - router 已按 auth / management / public / realtime / static 拆分注册。

4. `internal/handlers` 与 `internal/modules/*/delivery`
   - handler 负责 HTTP DTO、请求解析、状态码和 use case 调用。
   - delivery contract 是 handler 面向业务模块的主要边界。

## 模块分层

`apps/server/internal/modules` 当前共 **27 个模块**（2026-10-04 核验；V1.0 收敛起不再新增）：

完整分层（14 个，具 `domain`）：

| Module | Layers |
| --- | --- |
| `agent` | `domain` / `application` / `infra` / `delivery` |
| `ai` | `domain` / `application` / `infra` / `delivery` |
| `analytics` | `domain` / `application` / `infra` / `delivery` / `contract` |
| `assist` | `domain` / `application` / `infra` / `delivery` |
| `automation` | `domain` / `application` / `infra` / `delivery` |
| `conversation` | `domain` / `application` / `infra` / `delivery` |
| `customer` | `domain` / `application` / `infra` / `delivery` / `api` |
| `knowledge` | `domain` / `application` / `infra` / `delivery` |
| `quality` | `domain` / `application` / `infra` / `delivery` |
| `routing` | `domain` / `application` / `infra` / `delivery` / `contract` |
| `ticket` | `domain` / `application` / `infra` / `delivery` / `contract` / `orchestration` |
| `translation` | `domain` / `application` / `infra` / `delivery` |
| `voice` | `domain` / `application` / `infra` / `delivery` / `provider` |
| `webhook` | `domain` / `application` / `infra` / `delivery` |

薄壳模块（13 个，无 `domain` 层；业务逻辑仍以 legacy handler/service 承载，V1.0 收敛后叙事降级为子能力，代码保留）：

| Module | Layers |
| --- | --- |
| `api_key` | `application` / `delivery` / `infra` |
| `app_integration` | `application` / `delivery` / `infra` |
| `auth` | `application` / `delivery`（真实逻辑在 `platform/auth` + `handlers/auth_*`） |
| `custom_field` | `application` / `delivery` / `infra` |
| `email` | `application` / `delivery` / `infra` |
| `gamification` | `application` / `delivery` / `infra` / `contract` |
| `macro` | `application` / `delivery` / `infra` |
| `push` | `application` / `delivery` / `infra` |
| `satisfaction` | `application` / `delivery` / `infra` |
| `shift` | `application` / `delivery` / `infra` |
| `sla` | `application` / `delivery` / `infra` |
| `suggestion` | `application` / `delivery` / `infra` / `contract` |
| `workspace` | `application` / `delivery` / `infra` |

目标依赖方向（`internal/services` 已于 P3-2 整体移除，图中不再有 services 层）：

```text
handlers -> modules/*/delivery
app/server -> modules/*/delivery
modules/*/delivery -> modules/*/application
modules/*/application -> modules/*/domain|infra
```

当前已有 `scripts/check-module-boundaries.sh` 和 `scripts/module-boundaries.rules` 守护关键依赖，不应把已收口模块重新接回 concrete legacy service。

## 当前已收口的主路径

以下能力已经具备较稳定的 module delivery 主路径或受控 runtime contract：

- `ticket`
- `agent`
- `analytics`
- `routing / session transfer`
- `conversation / websocket runtime`
- `ai`
- `customer`
- `automation`
- `knowledge`
- `suggestion`
- `gamification`

这不代表所有 legacy 代码都已删除，而是代表 handler/router/runtime 的主入口已经有明确 contract 和边界检查。

## 仍在过渡的区域

当前需要继续审慎处理的区域：

| 区域 | 现状 | 风险 |
| --- | --- | --- |
| `statistics` 旧 handler | 已收口：`modules/analytics/delivery/statistics_handler.go` / `statistics_export_handler.go`（V1.0 B2-3） | 无遗留风险；顶层 handlers 仅剩迁移来源测试 |
| `voice` | 模块化程度较高（完整分层），但不是典型 `services -> modules` 迁移形态 | provider、media、protocol、业务状态容易混在一起；V1.0 冻结为扩展边界 |
| `realtime` | WebSocket hub 仍是运行态核心对象 | connection runtime 与业务持久化边界必须继续守住 |
| `AI / Knowledge` | provider 抽象与 mock/容器化验收已闭环（P1-1）；仅 real 模式验收受外部凭证阻塞 | 接口成功不等于真实 provider 主路径命中 |
| `storage / uploads` | 当前有 local provider，代码已标注多节点限制 | 多实例部署需要对象存储边界 |
| `DailyStats` 全局聚合 | `DailyStats` 等系统级汇总表尚无 tenant/workspace 维度拆分口径 | 多租户语义下汇总口径歧义（见 `tenant-workspace-boundaries.md`） |

## 文档状态源

后续判断架构时按下面顺序读取：

1. `docs/current-architecture.md`
   - 当前真实架构快照。
2. `ARCHITECTURE.md`
   - 目标架构和长期设计原则。
3. `docs/architecture-redesign-plan.md`
   - 架构重设计计划（services→modules 迁移期产物，仅存档）。
4. `docs/implementation/10-service-to-module-migration.md`
   - services 到 modules 的迁移计划。
5. `docs/implementation/10-migration-scorecard.md`
   - 当前模块迁移完成度。
6. `docs/implementation/10-module-boundaries.md`
   - 迁移期依赖规则。
7. `todo.md` 和 `docs/delivery-priorities.md`
   - 当前交付优先级和恢复点。

