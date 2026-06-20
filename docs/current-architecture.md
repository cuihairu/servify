# 当前架构分析

本文记录当前仓库的真实架构状态，用于衔接根目录 `ARCHITECTURE.md` 中的目标设计和 `docs/implementation/` 下的实施 backlog。

## 结论

当前 Servify 已经进入模块化单体过渡态：

- 后端仍是单进程优先的 Go modular monolith，不是微服务架构。
- 主入口已经从 `cmd/server` 下沉到 `internal/app/bootstrap` 与 `internal/app/server`。
- 主要业务能力正在从旧的 `handlers -> services -> models` 收口到 `modules/*/{domain,application,infra,delivery}`。
- `handlers` 大多已经依赖 module delivery contract 或 handler-local contract，而不是直接依赖 concrete legacy service。
- `services` 目录仍存在。其中一部分已收缩为 compatibility facade、runtime state holder、event bus glue、worker glue 或历史调用兼容；另一批（如 statistics、SLA、satisfaction、shift、workspace、macro、custom_field、app_integration、AI enhanced、MessageRouter）仍是当前业务逻辑的主要承载者，尚未完成模块化迁移（详见下方“仍在过渡的区域”）。

换句话说，当前系统不是纯目标态，也不是旧架构；它是一个已经有边界守护的迁移中架构。

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

`apps/server/internal/modules` 当前包含：

| Module | Layers |
| --- | --- |
| `agent` | `domain` / `application` / `infra` / `delivery` |
| `ai` | `domain` / `application` / `infra` / `delivery` |
| `analytics` | `domain` / `application` / `infra` / `delivery` / `contract` |
| `automation` | `domain` / `application` / `infra` / `delivery` |
| `conversation` | `domain` / `application` / `infra` / `delivery` |
| `customer` | `domain` / `application` / `infra` / `delivery` |
| `gamification` | `application` / `infra` / `delivery` / `contract` |
| `knowledge` | `domain` / `application` / `infra` / `delivery` |
| `routing` | `domain` / `application` / `infra` / `delivery` / `contract` |
| `suggestion` | `application` / `infra` / `delivery` / `contract` |
| `ticket` | `domain` / `application` / `infra` / `delivery` / `contract` / `orchestration` |
| `voice` | `domain` / `application` / `infra` / `delivery` / `provider` |

目标依赖方向：

```text
handlers -> modules/*/delivery
app/server -> modules/*/delivery
app/server -> services/* only for runtime glue
services/* -> modules/*/application or modules/*/delivery
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
| `services` | 仍保留 runtime state、subscriber、worker、compatibility facade | 容易重新变成默认业务中心 |
| `voice` | 模块化程度较高，但不是典型 `services -> modules` 迁移形态 | provider、media、protocol、业务状态容易混在一起 |
| `realtime` | WebSocket hub 仍是运行态核心对象 | connection runtime 与业务持久化边界必须继续守住 |
| `AI / Knowledge` | provider 抽象已在，但真实验收仍是当前交付优先项 | 接口成功不等于真实 provider 主路径命中 |
| `storage / uploads` | 当前有 local provider，代码已标注多节点限制 | 多实例部署需要对象存储边界 |
| `acceptance evidence` | 部分主链路仍缺真实运行证据 | 文档状态可能快于交付事实 |

## 文档状态源

后续判断架构时按下面顺序读取：

1. `docs/current-architecture.md`
   - 当前真实架构快照。
2. `ARCHITECTURE.md`
   - 目标架构和长期设计原则。
3. `docs/architecture-redesign-plan.md`
   - 下一轮重设计计划。
4. `docs/implementation/10-service-to-module-migration.md`
   - services 到 modules 的迁移计划。
5. `docs/implementation/10-migration-scorecard.md`
   - 当前模块迁移完成度。
6. `docs/implementation/10-module-boundaries.md`
   - 迁移期依赖规则。
7. `todo.md` 和 `docs/delivery-priorities.md`
   - 当前交付优先级和恢复点。

