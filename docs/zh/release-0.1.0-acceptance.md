# Servify 0.1.0 Release Acceptance

> 注：本文为 **v0.1.0 历史版本验收规范**（已被 v1.0.0 取代，现行口径见 [acceptance-checklist.md](./acceptance-checklist.md)）。文内 `internal/services/*` 证据路径为当时时点——该目录已随 P3-2 整体移除，测试迁移落点见 acceptance-checklist。

这份文档定义 `Servify v0.1.0` 的发布验收范围、准入门槛、必测功能、证据要求和已知边界。

版本定位：

- `v0.1.0` 是首个公开预览版
- 目标是：可安装、可部署、可演示、主链路可跑通
- 不是：企业生产稳定版，不等同于 `v1.0`

发布表达建议：

- 可以说：核心客服主链路、AI 基础能力、后台基础运营能力已具备预览版交付条件
- 不应说：已完成企业级生产稳定交付

## 1. Release Gate

`v0.1.0` 发布前，以下条件必须同时成立：

1. `make build` 通过
2. `make release-check CONFIG=./config.yml` 通过
3. 核心客服主链路验收全部达到 `通过`
4. Auth 自助 session 链路达到 `通过`
5. AI / Knowledge 至少 1 条真实 provider 主路径达到 `通过`
6. 本文档、`acceptance-checklist.md`、README、deployment 文档完成同步
7. Release Note 明确写出已知限制和非目标

一票否决项：

- 服务无法启动
- 数据库迁移失败
- 核心接口返回 500 或结构与契约不符
- 核心状态流转无数据证据
- 鉴权/权限中间件失效
- 外部 knowledge provider 不可用时没有 fallback 或错误不可控

## 2. 当前版本边界

`v0.1.0` 明确不承诺以下事项：

- 多实例高可用与跨实例一致性
- 持久化消息队列语义下的异步 durability
- 完整 co-browsing 远程协助产品
- 真实 voice provider 的生产级接入
- 客户侧推荐问题 / 上下文联想问题
- 企业级容量、压测、备份恢复基线

已知实现边界，必须在 release note 中明确：

- 默认 event bus 仍为进程内实现
- voice runtime 默认 wiring 仍存在 mock/provider 边界
- agent runtime 仍有内存态 / legacy compatibility 残留
- compatibility / mock 资产只用于 dev/demo/验收辅助，不等同于正式生产能力

## 3. 验收证据规则

每个功能项必须同时具备以下 4 类证据，否则不能标记为 `通过`：

1. 代码入口：路由、页面、CLI 或 SDK 调用存在
2. 自动化证据：单测、集成测试或脚本校验存在
3. 运行证据：真实启动、真实请求、真实返回
4. 数据证据：数据库、状态流转、文件或外部依赖的前后变化可见

建议记录格式：

| 字段 | 内容 |
| --- | --- |
| 功能项 | 例如“发送会话消息” |
| 页面/API | 明确入口 |
| 前置条件 | 数据、账号、依赖服务 |
| 操作步骤 | 请求或操作过程 |
| 预期结果 | 状态码、响应、状态变化 |
| 自动化证据 | 测试文件或命令 |
| 人工证据 | curl、截图、日志、DB 查询 |
| 是否为 0.1.0 blocker | 是 / 否 |
| 状态 | 未验 / 部分通过 / 通过 / 阻塞 |

## 4. 功能包验收范围

`v0.1.0` 按 8 个功能包验收。

### 4.1 基础运行

目标：

- 仓库可以构建
- 服务可以启动
- 健康、就绪、指标面可访问
- 发布前最小基线检查可执行

必须验收的功能：

1. 数据库迁移
2. 标准构建
3. 服务启动
4. 健康检查
5. 就绪检查
6. 指标暴露
7. 安全基线检查
8. 可观测性基线检查
9. Release readiness 检查

验收项：

| 功能项 | 页面/API/命令 | 是否 blocker |
| --- | --- | --- |
| 数据库迁移 | `make migrate` | 是 |
| 标准构建 | `make build` | 是 |
| 服务启动 | `go -C apps/server run ./cmd/server` | 是 |
| 健康检查 | `GET /health` | 是 |
| 就绪检查 | `GET /ready` | 是 |
| Prometheus 指标 | `GET <metricsPath>` | 是 |
| 安全基线检查 | `make security-check CONFIG=...` | 是 |
| 可观测性基线检查 | `make observability-check CONFIG=...` | 是 |
| 发布前检查 | `make release-check CONFIG=...` | 是 |

自动化证据最小集合：

- `apps/server/internal/handlers/health_enhanced_test.go`
- `apps/server/cmd/cli/check_security_baseline_test.go`
- `apps/server/cmd/cli/check_observability_baseline_test.go`
- `scripts/check-release-readiness.sh`

### 4.2 认证与登录

目标：

- 管理员能够登录系统
- token 轮转正确
- 自助 session 管理有效
- 未授权访问能被阻断

必须验收的功能：

1. 登录
2. refresh token 轮转
3. 当前会话列表
4. 退出当前会话
5. 退出其它会话
6. 未授权拦截

验收项：

| 功能项 | 页面/API | 是否 blocker |
| --- | --- | --- |
| 登录 | `POST /api/v1/auth/login` | 是 |
| Refresh 轮转 | `POST /api/v1/auth/refresh` | 是 |
| 当前会话列表 | `GET /api/v1/auth/sessions` | 是 |
| 退出当前会话 | `POST /api/v1/auth/sessions/logout-current` | 是 |
| 退出其它会话 | `POST /api/v1/auth/sessions/logout-others` | 是 |
| 未授权拦截 | 访问受保护 API | 是 |

自动化证据最小集合：

- `apps/server/internal/handlers/auth_handler_test.go`
- `apps/server/internal/services/auth_service_test.go`
- `apps/server/internal/middleware/auth_extended_test.go`

### 4.3 客户咨询主链路

目标：

- 从会话进入到消息处理、人工接管、转接、关闭全程贯通

必须验收的功能：

1. 工作台概览可见会话
2. 会话详情可读
3. 消息列表可读
4. 发送消息成功
5. 接管会话成功
6. 转接会话成功
7. 关闭会话成功

验收项：

| 功能项 | 页面/API | 是否 blocker |
| --- | --- | --- |
| 工作台概览 | `GET /api/omni/workspace` | 是 |
| 会话详情 | `GET /api/omni/sessions/:id` | 是 |
| 消息列表 | `GET /api/omni/sessions/:id/messages` | 是 |
| 发送消息 | `POST /api/omni/sessions/:id/messages` | 是 |
| 接管会话 | `POST /api/omni/sessions/:id/assign` | 是 |
| 转接会话 | `POST /api/omni/sessions/:id/transfer` | 是 |
| 关闭会话 | `POST /api/omni/sessions/:id/close` | 是 |

自动化证据最小集合：

- `apps/server/internal/handlers/conversation_workspace_handler_test.go`
- `apps/server/internal/services/websocket_test.go`

人工验收最低要求：

1. 真实登录管理端
2. 打开工作台
3. 查看一条真实会话
4. 发一条客服消息
5. 执行一次接管
6. 执行一次转接
7. 执行一次关闭

### 4.4 工单全流程

目标：

- 咨询无法即时解决时，能够进入工单全流程

必须验收的功能：

1. 创建工单
2. 工单列表
3. 工单详情
4. 更新工单
5. 指派工单
6. 添加评论
7. 关闭工单
8. 工单统计
9. 工单导出

验收项：

| 功能项 | 页面/API | 是否 blocker |
| --- | --- | --- |
| 创建工单 | `POST /api/tickets` | 是 |
| 工单列表 | `GET /api/tickets` | 是 |
| 工单详情 | `GET /api/tickets/:id` | 是 |
| 更新工单 | `PUT /api/tickets/:id` | 是 |
| 指派工单 | `POST /api/tickets/:id/assign` | 是 |
| 添加评论 | `POST /api/tickets/:id/comments` | 是 |
| 关闭工单 | `POST /api/tickets/:id/close` | 是 |
| 工单统计 | `GET /api/tickets/stats` | 是 |
| 导出工单 | `GET /api/tickets/export` | 是 |

自动化证据最小集合：

- `apps/server/internal/handlers/ticket_handler_test.go`
- `apps/server/internal/modules/ticket/application/command_service_test.go`
- `apps/server/internal/modules/ticket/application/query_service_test.go`

### 4.5 AI 与知识库

目标：

- AI 至少具备稳定问答能力
- 外部 knowledge provider 可控制、可上传、可同步
- provider 异常时 fallback 可工作

必须验收的功能：

1. AI 查询
2. AI 状态
3. AI 指标
4. 知识上传
5. 知识同步
6. 启用 provider
7. 禁用 provider
8. 熔断器重置
9. fallback 行为

验收项：

| 功能项 | 页面/API | 是否 blocker |
| --- | --- | --- |
| AI 查询 | `POST /api/v1/ai/query` | 是 |
| AI 状态 | `GET /api/v1/ai/status` | 是 |
| AI 指标 | `GET /api/v1/ai/metrics` | 否 |
| 知识上传 | `POST /api/v1/ai/knowledge/upload` | 是 |
| 知识同步 | `POST /api/v1/ai/knowledge/sync` | 是 |
| 启用 provider | `PUT /api/v1/ai/knowledge-provider/enable` | 是 |
| 禁用 provider | `PUT /api/v1/ai/knowledge-provider/disable` | 是 |
| 熔断器重置 | `POST /api/v1/ai/circuit-breaker/reset` | 否 |
| provider fallback | AI 主链路 | 是 |

自动化证据最小集合：

- `apps/server/internal/handlers/ai_handler_comprehensive_test.go`
- `apps/server/internal/services/orchestrated_ai_fallback_integration_test.go`

人工验收最低要求：

1. 至少一条真实 provider 查询成功
2. 至少一条真实文档上传成功
3. 至少一条真实同步成功
4. 至少一条 provider 异常时 fallback 成功

### 4.6 管理端基础运营

目标：

- 后台有最小运营链路，不是纯演示壳

必须验收的功能：

1. Dashboard 可读真实数据
2. Customer 核心 CRUD
3. Agent 核心运营操作

验收项：

| 功能项 | 页面/API | 是否 blocker |
| --- | --- | --- |
| Dashboard 统计 | `GET /api/statistics/dashboard` | 是 |
| 创建客户 | `POST /api/customers` | 否 |
| 客户列表 | `GET /api/customers` | 否 |
| 客户详情 | `GET /api/customers/:id` | 否 |
| 更新客户 | `PUT /api/customers/:id` | 否 |
| 客户备注 | `POST /api/customers/:id/notes` | 否 |
| 客户标签 | `PUT /api/customers/:id/tags` | 否 |
| 客服列表 | `GET /api/agents` | 否 |
| 客服上线 | `POST /api/agents/:id/online` | 否 |
| 客服下线 | `POST /api/agents/:id/offline` | 否 |
| 客服状态更新 | `PUT /api/agents/:id/status` | 否 |
| 在线客服列表 | `GET /api/agents/online` | 否 |

自动化证据最小集合：

- `apps/server/internal/handlers/customer_handler_test.go`
- `apps/server/internal/handlers/agent_handler_test.go`
- `apps/server/internal/handlers/agent_handler_extended_test.go`
- `apps/server/internal/services/statistics_service_test.go`

### 4.7 实时能力

目标：

- 证明 WebSocket / WebRTC 基础能力为真实运行能力，不是伪实现

必须验收的功能：

1. WebSocket 建连
2. WebSocket 收发消息
3. WebSocket 统计
4. WebRTC 基础协商链路
5. WebRTC 统计
6. WebRTC 连接列表

验收项：

| 功能项 | 页面/API | 是否 blocker |
| --- | --- | --- |
| WebSocket 建连 | `GET /api/v1/ws` | 是 |
| WebSocket 统计 | `GET /api/v1/ws/stats` | 否 |
| WebRTC 统计 | `GET /api/v1/webrtc/stats` | 否 |
| WebRTC 连接列表 | `GET /api/v1/webrtc/connections` | 否 |

自动化证据最小集合：

- `apps/server/internal/services/websocket_test.go`
- `apps/server/internal/services/websocket_processing_test.go`
- `apps/server/internal/services/webrtc_test.go`
- `apps/server/internal/services/webrtc_stats_test.go`

### 4.8 公开入口

目标：

- 如果 README 和官网对外描述了公开入口，则必须具备最小可验收能力

必须验收的功能：

1. Portal config 可匿名访问
2. Public knowledge 可匿名访问
3. Public CSAT 可匿名访问并提交

验收项：

| 功能项 | 页面/API | 是否 blocker |
| --- | --- | --- |
| Portal 配置 | `GET /public/portal/config` | 否 |
| Public knowledge 列表 | `GET /public/kb/docs` | 否 |
| Public knowledge 详情 | `GET /public/kb/docs/:id` | 否 |
| Public CSAT 查询 | `GET /public/csat/:token` | 否 |
| Public CSAT 提交 | `POST /public/csat/:token/respond` | 否 |

自动化证据最小集合：

- `apps/server/internal/handlers/csat_public_handler.go`
- 对应公开入口 handler 测试

## 5. 0.1.0 可以不阻塞发布的功能

以下功能可以继续存在于 backlog，但不应阻塞 `v0.1.0`：

1. 客户侧推荐问题 / 上下文联想问题
2. Satisfaction 后台全量运营面
3. 宏、自动化、激励、排班
4. 更完整的 session transfer 运营后台
5. 真实 voice provider 接入
6. 多实例 HA
7. 容量压测与性能基线
8. 企业级备份恢复演练

## 6. 文档验收范围

`v0.1.0` 发布前，以下文档必须同步：

| 文档 | 要求 |
| --- | --- |
| `README.md` | 明确版本定位、核心能力、已知限制 |
| `docs/local-development.md` | 本地运行步骤可执行 |
| `docs/deployment.md` | 最小部署、配置分层、依赖说明清晰 |
| `docs/acceptance-checklist.md` | 对应功能项状态已回填 |
| `docs/release-versioning.md` | 版本与 changelog 流程正确 |
| `docs/demo-and-mock-boundaries.md` | demo/mock/prod 边界清晰 |

## 7. Release Note 必须包含的内容

发布 `v0.1.0` 时，release note 至少应包含：

1. 版本定位：首个公开预览版
2. 已具备能力：
   - 核心客服主链路
   - AI 基础问答与知识链路
   - 后台基础运营面
   - WebSocket/WebRTC 基础能力
3. 已知限制：
   - event bus durability 边界
   - voice/provider 边界
   - 单实例优先
   - 非生产级完整远程协助产品
4. 升级/部署说明
5. 已验证环境与执行方式

## 8. 当前建议执行顺序

如果以 `v0.1.0` 为目标，建议按以下顺序补齐：

1. 基础运行缺口：`GET /ready`、`GET <metricsPath>`、`make build`
2. Auth 自助 session 链路打通
3. 会话工作台走通
4. 工单高频操作全流程
5. AI / Knowledge 真实 provider 验收收口
6. `acceptance-checklist.md` 回填
7. README / deployment / release note 同步

## 9. 最终发布判断标准

满足以下条件即可发布 `v0.1.0`：

1. 所有 blocker 功能项在本文件和 `acceptance-checklist.md` 中均为 `通过`
2. 所有 release gate 命令通过
3. 已知限制已公开说明
4. 未把 demo/mock 能力误描述为生产稳定能力

若 blocker 中仍有 `部分通过`、`未验` 或 `阻塞`，则不应发布 `v0.1.0`
