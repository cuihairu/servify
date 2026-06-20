# Servify 验收清单

这份清单的目标不是证明“看起来差不多做完了”，而是防止把“代码里有入口”误判成“功能已经可交付”。

验收时，每个功能项都必须同时具备以下 4 类证据，否则一律不算完成：

1. 代码入口：路由、命令或调用入口明确存在。
2. 自动化测试：至少有相关单测、集成测试或命令级验证。
3. 运行证据：服务真实启动，接口可访问，请求与响应符合预期。
4. 数据证据：涉及数据库、状态流转、文件或外部依赖时，能看到前后状态变化。

## 状态定义

- `未验`：只确认代码入口存在，没有实际跑通。
- `部分通过`：编译或单测通过，但关键业务链路、异常路径或数据落库未验。
- `通过`：具备代码、测试、运行、数据四类证据。
- `阻塞`：缺配置、依赖、权限、环境或数据，暂时无法验。

## 使用方式

先准备运行环境：

```bash
make migrate DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=password DB_NAME=servify
make run-cli CONFIG=./config.yml
make run-knowledge-provider CONFIG=./config.weknora.yml
make run-weknora CONFIG=./config.weknora.yml
# `run-knowledge-provider` 是通用 alias；两者都仅用于 WeKnora compatibility / mock 验收
# 默认知识库 provider 仍以 Dify 为主
```

常用基础验证命令：

```bash
make build
make build-knowledge-provider
make build-weknora
make test
go test -tags weknora ./apps/server/cmd ./apps/server/internal/handlers ./apps/server/pkg/weknora/...
```

每个条目都按下面模板记录：

| 字段 | 内容 |
| --- | --- |
| 功能项 | 例如“创建工单” |
| 入口 | 路由 / CLI / 页面 |
| 前置条件 | 数据库、配置、鉴权、依赖服务 |
| 操作步骤 | 明确到请求参数或操作动作 |
| 预期结果 | 状态码、返回体、状态变化 |
| 自动化证据 | 相关测试文件或命令 |
| 人工证据 | curl 结果、截图、日志、DB 查询结果 |
| 状态 | 未验 / 部分通过 / 通过 / 阻塞 |

## 当前已确认的最小事实

这些是当前已经实际验证过的，不代表“全量功能完成”：

| 项目 | 证据 | 状态 |
| --- | --- | --- |
| Knowledge provider compatibility CLI 可构建 | `make build-knowledge-provider` 或 `make build-weknora` 通过 | 通过 |
| WeKnora 相关 handler/pkg 基础测试 | `go test -tags weknora ./apps/server/cmd ./apps/server/internal/handlers ./apps/server/pkg/weknora/...` 通过 | 通过 |
| 本地最小环境检查 | `make local-check` 通过 | 通过 |
| 发布前最小自检 | `make release-check CONFIG=./config.yml` 通过；当前默认根配置路径已通过临时 SQLite 库完成迁移、启动、`/health`、`/ready`、`/metrics` 烟测，不再依赖本机预装 Postgres 才能复现这条最小发布自检 | 通过 |
| 客服主链路聚焦自动化验证 | `go test -tags integration ./apps/server/internal/handlers -run 'Test(WorkspaceHandler_GetOverview_.*|StatisticsHandler_Dashboard_And_TimeRange|TicketHandler_Create_Get_List_Assign)'`、`go test ./apps/server/internal/handlers -run 'Test(ConversationWorkspaceHandler_(ListMessages|SendMessage|AssignAgent|Transfer|CloseSession|GetSession)|AuthHandlerRefreshToken|AuthHandlerSelfServiceSessions)'`、`go test ./apps/server/internal/services -run 'Test(AuthServiceLoginAndRefreshRotateSession|AuthServiceSelfManageSessions|WorkspaceService_GetOverview_WithSessions|StatisticsService_GetDashboardStats_EmptyDB|StatisticsService_GetDashboardStats_WithData)'` 通过 | 通过 |
| 主链路人工运行验证当前阻塞 | 该条记录保留为历史人工验收事实：当时 `nc -z localhost 5432` 返回非零；`go -C apps/server run ./cmd/server --port 18080` 实际报错 `dial tcp 127.0.0.1:5432: connect: connection refused`；`redis-cli -h localhost -p 6379 ping` 返回 `Connection refused`；当前本机 Docker daemon 也不可用。补充说明：这不再阻塞默认 `config.yml` 下的 `make release-check`，因为发布前最小自检现已具备 SQLite 临时库回退路径 | 阻塞 |
| 主链路人工运行验证第二轮 | 本机已拉起 `redis-server` 与 `postgresql@15`；`make migrate` 完成；`go -C apps/server run ./cmd/server --port 18080` 启动成功；`curl http://localhost:18080/health` 返回 healthy；`scripts/seed-data.sh http://localhost:18080` 完成；随后已实际完成 `admin / admin123` 登录、Dashboard 查询、Workspace 查询、Ticket 列表/详情查询、补建 agent 实体后完成 Ticket assign，并通过 WebSocket 向 `g1-session-1` 写入消息再经 `/api/omni/sessions/g1-session-1/messages` 读回 | 部分通过 |
| 主链路人工运行验证第三轮 | 本机以 `DB_USER=cui DB_NAME=servify` 完成 `make migrate`；启动 `SERVIFY_JWT_SECRET=dev-secret DB_USER=cui DB_NAME=servify go -C apps/server run ./cmd/server --port 18080`；`curl http://localhost:18080/health` 返回 healthy；更新后的 `scripts/seed-data.sh http://localhost:18080` 会创建 agent 实体并自动置为 online；随后已实际完成 `admin / admin123` 登录、`/api/statistics/dashboard`、`/api/omni/workspace`、`/api/omni/sessions/g1-session-2`、`/api/tickets`、`POST /api/tickets/30/assign`，并确认工单详情 `agent_id` 已更新为 `4` | 通过 |
| AI 主链路人工运行验证第一轮 | 以 `admin / admin123` 登录后，实际完成 `GET /api/v1/ai/status`、`POST /api/v1/ai/query`、`GET /api/v1/ai/metrics`；其中 `status` 返回 `type=orchestrated_enhanced`、`fallback_enabled=true`，`query` 返回 `strategy=fallback`，证明在外部 knowledge provider 不可用时主链路仍可响应；当前脚本留档也已覆盖 `ai-query-after-disable.json` 与 `ai-metrics-after-fallback.json`，用于补充 fallback 响应与指标证据 | 通过 |
| AI 控制面人工运行验证第二轮 | 使用启用外部 knowledge provider 的临时配置启动服务，实际完成 `PUT /api/v1/ai/knowledge-provider/enable`、`PUT /api/v1/ai/knowledge-provider/disable`、`POST /api/v1/ai/circuit-breaker/reset`；同时确认 `GET /api/v1/ai/status` 返回 `knowledge_provider_enabled=true`、`knowledge_provider=<active-provider>`、`knowledge_provider_healthy=true|false`，且 `disable -> status -> enable -> status` 的状态切换能被证据文件留档，说明控制路由已暴露且运行时能清楚标识当前知识源健康度；本轮另已补 `scoped AI handler` 状态保持回归，覆盖 enable/disable 之后后续请求仍能看到同一 runtime 开关状态 | 部分通过 |
| AI 知识链路人工运行验证第三轮 | 本机启动临时知识库 provider 测试环境后，以启用外部 knowledge provider 的临时配置启动服务；随后实际完成 `POST /api/v1/ai/knowledge/upload`、`POST /api/v1/ai/knowledge/sync`，并确认 `GET /api/v1/ai/status` 返回 `knowledge_provider_enabled=true`、当前激活 `knowledge_provider` 以及 `knowledge_provider_healthy=true`，说明 Servify 到外部知识源的上传 / 同步协议路径已跑通；当前另已补自动化回归，锁定 `enable` 之后 `UploadKnowledgeDocument` 会真正进入 provider 路径而不是被错误地判成 `knowledge provider is not enabled`，且 `SyncKnowledgeBase()` 在 provider 未启用时不再返回假成功 | 部分通过 |
| 配置加载口径修复回归验证 | 新增 `TestLoadConfigFindsRepoRootConfigFromNestedDir`，确认从 `apps/server` 这类嵌套目录启动时，`LoadConfig()` 仍能读到仓库根 `config.yml` | 通过 |
| 运营高价值链路人工运行验证第一轮 | 以 `admin / admin123` 登录后，实际完成 `POST /api/customers`、`GET /api/customers/:id`、`PUT /api/customers/:id`、`POST /api/customers/:id/notes`、`PUT /api/customers/:id/tags`、`GET /api/customers/stats`、`GET /api/agents`、`POST /api/agents/:id/online`、`PUT /api/agents/:id/status`、`GET /api/agents/online`、`GET /api/agents/stats`、`GET /api/security/users/:id`、`POST /api/security/users/:id/revoke-tokens`、`GET /api/security/users/:id/sessions`、`GET /api/audit/logs?action=customers.create`、`GET /api/audit/logs/:id`、`GET /api/audit/logs/:id/diff`、`GET /api/audit/logs/export?action=customers.create&limit=5`；其中审计查询在默认限流下触发过一次 `429`，随后使用白名单 `X-API-Key: internal-test-key` 验证通过 | 部分通过 |
| system 级 `DailyStats` 作用域回归 | `go test -tags integration ./apps/server/internal/services -run 'TestStatisticsService_(UpdateDailyStats_NewRecord|UpdateDailyStats_IgnoresRequestScopeForSystemAggregate)$'` 与 `go test -tags integration ./apps/server/internal/modules/analytics/infra -run 'TestGormRepositoryScoped(DashboardIgnoresGlobalDailyStats|TimeRangeIgnoresGlobalDailyStats|AgentPerformanceStaysScoped)'` 通过；已覆盖 scoped 读取不消费全局 `DailyStats`，以及 scoped 重算不会把 system 级日汇总写成局部数据 | 通过 |
| 审计保留与轻量导出约束自动化回归 | `go test ./apps/server/internal/platform/audit -run 'TestGormRetentionServiceCleanup'`、`go test ./apps/server/internal/handlers -run 'TestAuditHandlerExportCSV(ClampsLimit|RejectsInvalidFilters)$'` 通过；已覆盖 `created_at < cutoff` 保留边界与导出 `limit` 最大值 `5000` 的服务端截断行为 | 通过 |
| scoped config rollback / verify 治理链路自动化回归 | `go test ./apps/server/internal/handlers -run 'TestScopedConfigHandler(RollbackRestoresSnapshot|RollbackRequiresConfirmation|RollbackRequiresChangeControl|RollbackRequiresApprovalRefForHighRiskChange)$'` 通过；已覆盖显式确认、`change_ref` / `reason`、高风险 `approval_ref` 与 rollback 审计快照恢复约束 | 通过 |
| session risk 环境级策略与 Geo/IP provider 管理面回归 | `go test -tags integration ./apps/server/internal/handlers -run 'TestUserSecurityHandler_ListUserSessionsUses(ScopedRiskPolicy|EnvironmentRiskProfile|InjectedIPIntelligence)$'` 通过；已覆盖 management `user-security` 会话列表对 tenant scoped risk、environment risk profile 和可注入 IP intelligence provider 的直接证据 | 通过 |
| HTTP Geo/IP provider 契约回归 | `go test ./apps/server/internal/handlers -run 'TestHTTPSessionIPIntelligenceDescribeIP(SupportsNestedDataPayload|UsesCustomAuthHeaderWithoutBearerPrefix|ReturnsEmptyOnFailure|$)'` 通过；已覆盖嵌套 `data` JSON、非 `Authorization` 自定义鉴权头，以及上游失败时回退空结果的 adapter 契约 | 通过 |
| WeKnora 验收脚本 guard 与证据输出回归 | `go test ./scripts -run 'TestWeKnoraIntegrationScript(RealModeRejectsLocalHost|MockModeWritesEvidence)$'` 通过；已锁定 `real` 模式对本地/私网 WeKnora 地址的拒绝策略，并覆盖 `mock` 模式的 `summary.txt`、`ai-status.json`、`knowledge-upload.json`、`knowledge-sync.json` 等关键证据文件输出 | 通过 |
| Dify 验收脚本 guard 与证据输出回归 | `go test ./scripts -run 'TestDifyIntegrationScript(RealModeRejectsLocalHost|MockModeWritesEvidence)$'` 通过；已锁定 `real` 模式对本地/私网 Dify 地址的拒绝策略，并覆盖 `mock` 模式的 `summary.txt`、`dify-dataset.json`、`ai-status.json`、`knowledge-upload.json`、`knowledge-sync.json`、`ai-metrics.json` 等关键证据文件输出 | 通过 |
| Knowledge provider 验收统一入口回归 | `make dify-acceptance`、`make weknora-acceptance`、`make knowledge-provider-acceptance` 已接入统一入口；CI `script-checks` 也已执行对应 `go test ./scripts` 门禁，确保脚本 guard 与证据输出能力不会回退 | 通过 |
| 远程协助产品叙事文档对齐 | 已统一更新 [README](https://github.com/timebeau/servify/blob/main/README.md)、[文档首页](index.md)、[remote-assistance.md](remote-assistance.md)；现在一致强调”远程协助 = 实时指导/联合排查/人工接管/工单闭环中的产品能力”，并明确区分当前已具备的实时基础与尚未承诺的完整 co-browsing 工作台 | 通过 |
| v0.1.0 blocker 修复验收 | 1) AI/Knowledge 路由始终注册（`router.go` 移除 `externalKnowledgeProviderEnabled` 条件）2) `make test` 正确失败（移除 `|| true`）3) 工单错误码映射（新增 `ticketErrorToStatusCode` 函数区分 404/400/409/500）4) release-check 扩展（新增 build 验证、二进制检查、路由验证）5) security baseline 调整（v0.1.0 允许 fallback 模式） | 通过 |
| 远程协助现状入口盘点 | 已新增 [remote-assistance-current-state.md](remote-assistance-current-state.md)，盘点了服务端现有会话、转接、assist、voice、WebSocket/WebRTC 入口，以及管理端 `/conversation`、`/routing`、`/voice`、`/ticket/detail`、`/security` 相关页面；同时明确当前缺口在“缺统一产品入口、缺状态机、缺最小演示链路”，而不是基础 runtime 缺失 | 通过 |
| 远程协助 MVP 链路与验收口径 | 已新增 [remote-assistance-mvp.md](remote-assistance-mvp.md)，把最小可交付链路定义为“Web 会话 -> 人工接管 -> 转派/实时协作基础 -> 工单闭环”，并明确了对应页面、API 和人工演示步骤；当前不再把缺少独立 co-browsing 工作台误判成“没有远程协助产品方向” | 通过 |
| staging 口径 release readiness 演练 | 已新增 [config.staging.example.yml](https://github.com/timebeau/servify/blob/main/config.staging.example.yml)，并实际通过 `make security-check CONFIG=config.staging.example.yml`、`make observability-check CONFIG=config.staging.example.yml`、`make release-check CONFIG=config.staging.example.yml`；同时已在 [operator-runbook.md](operator-runbook.md) 和 [deployment.md](deployment.md) 回写 staging 基线与执行步骤 | 通过 |
| 生产模板与告警阈值对齐 | 已补齐 [config.production.secure.example.yml](https://github.com/timebeau/servify/blob/main/config.production.secure.example.yml) 中显式的 `metrics_path` / tracing 基线，并把 `deploy/observability/alerts/rules.yaml` 中的 10 条告警阈值同步回写到 [operator-runbook.md](operator-runbook.md)、[deployment.md](deployment.md) 与 [observability operational runbook](https://github.com/timebeau/servify/blob/main/deploy/observability/runbook/operational-runbook.md) | 通过 |
| demo/mock/in-memory 资产边界清点 | 已新增 [demo-and-mock-boundaries.md](demo-and-mock-boundaries.md)，明确盘点 `apps/demo`、`apps/demo-sdk`、`scripts/demo-setup.sh`、`scripts/seed-data.sh`、`infra/compose/weknora-mock`、`scripts/test-weknora-integration.sh`、进程内 event bus、voice mock provider、Telegram/WeChat stub 的保留原因、prod-safe 判断与退出条件；现已能区分“可保留的演示/回归资产”和“不可误判为正式能力的占位实现” | 通过 |
| 运营高价值链路人工运行验证第二轮 | 本机以 `DB_USER=cui DB_NAME=servify go run ./apps/server/cmd/server --port 18081` 启动服务，并继续使用 `admin / admin123` 登录；随后实际完成 `POST /api/sla/configs` 创建 urgent 配置、`POST /api/sla/configs` 创建 high 配置、`GET /api/sla/configs/:id`、`GET /api/sla/configs?page=1&page_size=20`、`PUT /api/sla/configs/:id`、`DELETE /api/sla/configs/:id`、`GET /api/sla/configs/priority/urgent`、`POST /api/sla/check/ticket/22`、`GET /api/sla/violations?page=1&page_size=20`、`POST /api/sla/violations/1/resolve`、`GET /api/sla/stats`；其中 ticket `22` 基于新建 urgent 配置命中 `resolution` 违约，`resolve` 后统计中的 `unresolved_violations` 变为 `0`；同时补充回归测试，确保 `CheckTicketSLA` 首次创建和重复返回违约时都会带上真实 `ticket` / `sla_config` 关联数据 | 通过 |
| 公开入口与实时入口人工运行验证第一轮 | 本机继续以 `DB_USER=cui DB_NAME=servify go run ./apps/server/cmd/server --port 18081` 启动服务；未登录访问 `GET /public/portal/config` 返回 `brand_name=Servify` 等公开配置；以 `admin / admin123` 登录后创建并显式发布知识文档 `G1-4 Public KB`，随后 `GET /public/kb/docs?search=G1-4&page=1&page_size=10` 与 `GET /public/kb/docs/16` 可公开读取；补充说明：当前公开知识库已收紧为“仅暴露显式 `is_public=true` 的文档”，管理端新建文档默认保持内部可见；使用 Node `ws` 客户端连到 `ws://127.0.0.1:18081/api/v1/ws?session_id=g1-4-ws` 后成功收到同会话回显消息，且在连接存活期间 `GET /api/v1/ws/stats` 返回 `connected_clients=1`。补充说明：当前 `/api/v1/ws` 仍为公开 realtime 入口，但已要求显式传入 `session_id`，不会再为缺失参数的请求自动生成匿名会话；同时通过插入一条缺失的 `customers.user_id=6` 扩展记录，实际完成 `GET /public/csat/g1-4-public-csat-token` 与 `POST /public/csat/g1-4-public-csat-token/respond`，确认公开问卷可读取并可提交，提交后再次读取可见 `status=completed`；Voice 基础路径方面，已实际完成 `GET /api/voice/protocols`、`POST /api/voice/recordings/start`、`GET /api/voice/recordings/:recordingID`、`POST /api/voice/recordings/stop`、`POST /api/voice/transcripts`、`GET /api/voice/transcripts?call_id=g1-4-call`、`POST /api/voice/protocols/sip/call-events/invite|answer|hangup`、`POST /api/voice/protocols/webrtc/media-events/session_started`；WebRTC 连接链路方面，已补齐 `ws` 对 `webrtc-offer/answer/candidate` 的真实 runtime wiring，并修复 `WebSocketClient.readPump()` 的 `512B` 入站限制，否则正常 SDP offer 会直接触发断连；随后使用本地生成的真实 SDP offer 连到 `ws://127.0.0.1:18081/api/v1/ws?session_id=g1-4-webrtc-real`，成功收到 `webrtc-answer` 与多条 `webrtc-candidate`，且 `GET /api/v1/webrtc/stats?session_id=g1-4-webrtc-real` 返回 `connection_state=connecting`、`ice_connection_state=checking`，`GET /api/v1/webrtc/connections` 返回 `connection_count=1` | 通过 |

说明：

- 上述结果只能证明构建链和部分测试是通的。
- 不能据此推出所有 API、权限、迁移、前端、数据链路都已完成。
- `G1-1` 曾受本地 Postgres/Redis 环境阻塞，但该阻塞已在后续验证轮次解除。
- 当前 `G1-1` 已补到一轮完整真实运行证据，原先暴露出的 3 个缺口里：
  - `scripts/seed-data.sh` 固定 `customer_id` 导致用户关联错位，已修复为基于真实注册返回的用户 ID 建票。
  - `scripts/seed-data.sh` 未创建 agent 实体，已修复为自动创建 `/api/agents` 记录。
  - `/api/omni/sessions/:id` 曾在仅有消息落库时返回 `conversation not found`，已补最小会话读模型兜底。
- 当前 `G1-2` 已拿到 AI 主链路、fallback、控制接口以及基于本地 provider mock 的知识上传 / 同步协议证据。
- 当前 `G1-2` 仍未拿到“真实 Dify 主路径 + WeKnora 兼容路径”的完整双路径运行证据；现阶段已证明 Servify 对外部 knowledge provider API 的调用路径可用，并已分别补齐 Dify 主路径与 WeKnora 兼容路径的验收脚本入口，但这仍不等同于真实生产级外部环境已完成验收。
- 当前 `knowledge-docs` 管理接口已具备持久化 CRUD，且 `Create/Update` 已开始自动 upsert 当前外部 knowledge provider；但 `Delete` 仍受 provider 能力约束，现阶段仅能做到“支持删除则同步删，不支持则显式失败并保留本地文档”。因此知识文档管理与 AI 外部检索的一致性边界已比之前清晰，但仍不能视为完整闭环能力已完成验收。
- 当前已开始持久化内部知识文档与外部 provider `document_id` 的映射，`knowledge-docs` 删除链路也会优先使用外部 ID；其中 Dify 已恢复为可基于外部 `document_id` 执行精准删除，但 WeKnora 仍缺真实删除实现，因此目前只有 Dify 可以对外声明 `deletion` capability 已恢复，WeKnora 仍不可以。
- 当前已补 `scripts/test-weknora-integration.sh` 的证据输出能力：可通过 `WEKNORA_ACCEPTANCE_MODE=mock|real` 与 `EVIDENCE_DIR=...` 生成 `summary.txt`、`ai-status.json`、`ai-query.json`、`knowledge-provider-disable.json`、`ai-status-after-disable.json`、`ai-query-after-disable.json`、`ai-metrics-after-fallback.json`、`knowledge-provider-enable.json`、`ai-status-after-enable.json`、`circuit-breaker-reset.json`、`knowledge-upload.json`、`knowledge-sync.json` 等验收留档；其中 `real` 模式当前主要服务于 WeKnora 兼容路径，会把 “WeKnora 健康可用 + AI 服务具备外部 knowledge provider 能力 + 控制面切换成功 + fallback 查询成功 + upload/sync 成功” 作为硬性通过条件。
- 当前已新增 `scripts/test-dify-integration.sh`：可通过 `DIFY_ACCEPTANCE_MODE=mock|real` 与 `EVIDENCE_DIR=...` 生成 `summary.txt`、`dify-dataset.json`、`ai-status.json`、`ai-query.json`、`knowledge-upload.json`、`knowledge-sync.json`、`ai-metrics.json` 等主路径验收留档；其中 `real` 模式要求当前激活 provider 为 `dify`，且 Dify dataset 健康、主路径 upload/sync 成功。
- 当前 `real` 模式还会主动拒绝 `localhost` / 私网地址，以及健康检查中显式暴露 `service=weknora-mock` 的上游，避免把本地 mock 环境误填成真实 WeKnora 兼容路径运行证据。
- `LoadConfig()` 现已补默认搜索 `.`、`..`、`../..`，避免从 `apps/server` 启动时读不到仓库根配置，导致验收口径和实际部署口径漂移。
- 当前仍不能据此推出 `G1-2` 到 `G1-4` 已完成，它们需要各自独立验收与证据回填。

## 一票否决项

以下任何一项失败，都不能对外宣称“已完成”：

- 服务无法启动。
- 数据库迁移失败。
- 核心路由返回 500 或返回结构与契约不符。
- 创建后无法查询，或更新后状态不变化。
- 鉴权与权限中间件失效。
- 外部 knowledge provider 不可用时没有回退或错误不可控。
- 关键流程只有 happy path，没有异常路径验证。

## 全量验收矩阵

以下清单按代码实际暴露的功能面整理，主要依据：

- [README](https://github.com/timebeau/servify/blob/main/README.md)
- [router.go](/Users/cui/Workspaces/servify/apps/server/internal/app/server/router.go)

### 1. 基础运行与可观测性

代码入口：

- [health.go](/Users/cui/Workspaces/servify/apps/server/internal/app/server/health.go)
- [run.go](/Users/cui/Workspaces/servify/apps/server/cmd/cli/run.go)
- [run_enhanced.go](/Users/cui/Workspaces/servify/apps/server/cmd/cli/run_enhanced.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 健康检查 | `GET /health` | 服务启动后直接请求 | `200` 且返回健康信息 | [health_enhanced_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/health_enhanced_test.go) | 通过 |
| 就绪检查 | `GET /ready` | 在依赖就绪后请求 | `200`；依赖异常时不应误报健康 | [health_enhanced_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/health_enhanced_test.go) | 通过 |
| Prometheus 指标 | `GET <metricsPath>` | 启动后访问 metrics | 返回文本指标；包含 runtime/AI/DB 指标 | [ai_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ai_handler.go) | 通过 |
| 安全基线严格校验 | `make security-check CONFIG=...` | 使用部署配置执行校验 | 无 warning 时返回 `0`；有高风险配置时非零退出 | [check_security_baseline_test.go](/Users/cui/Workspaces/servify/apps/server/cmd/cli/check_security_baseline_test.go) | 通过 |
| 可观测性基线严格校验 | `make observability-check CONFIG=...` | 使用部署配置执行校验 | metrics/tracing 配置与 dashboard/alert/runbook/collector 资产齐备时返回 `0` | [check_observability_baseline_test.go](/Users/cui/Workspaces/servify/apps/server/cmd/cli/check_observability_baseline_test.go) | 通过 |
| 发布前最小自检 | `make release-check CONFIG=./config.yml` | 执行统一自检脚本 | local/security/observability/focused Go tests 全部通过 | [check-release-readiness.sh](/Users/cui/Workspaces/servify/scripts/check-release-readiness.sh) | 通过 |
| CLI 标准构建 | `make build` | 执行构建 | 生成二进制，无编译错误 | `make build` | 通过 |
| CLI knowledge provider compatibility 构建 | `make build-knowledge-provider` | 执行构建 | 生成二进制，无编译错误 | `make build-knowledge-provider` | 通过 |
| 本地最小环境检查 | `make local-check` | 执行环境校验脚本 | 所有关键依赖通过 | [Makefile](/Users/cui/Workspaces/servify/Makefile) | 通过 |

### 2. 实时能力

代码入口：

- [router.go](/Users/cui/Workspaces/servify/apps/server/internal/app/server/router.go)
- [handlers.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/handlers.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| WebSocket 建连 | `GET /api/v1/ws` | 带 `session_id` 建立连接 | 成功升级协议，可收发消息 | [websocket_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/websocket_test.go) | 通过 |
| WebSocket 统计 | `GET /api/v1/ws/stats` | 建连前后各请求一次 | client 数量正确变化 | [websocket_processing_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/websocket_processing_test.go) | 通过 |
| WebRTC 统计 | `GET /api/v1/webrtc/stats` | 建立会话后请求 | 返回连接统计 | [webrtc_stats_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/webrtc_stats_test.go) | 通过 |
| WebRTC 连接列表 | `GET /api/v1/webrtc/connections` | 建立多连接后请求 | 返回当前连接列表 | [webrtc_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/webrtc_test.go) | 通过 |
| 平台消息路由统计 | `GET /api/v1/messages/platforms` | 模拟不同平台消息后请求 | 平台维度统计正确 | [router_stats_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/router_stats_test.go)、[message_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/message_handler_test.go) | 通过 |

### 3. AI 与外部知识库

代码入口：

- [router.go](/Users/cui/Workspaces/servify/apps/server/internal/app/server/router.go)
- [ai_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ai_handler.go)
- [WEKNORA_INTEGRATION.md](WEKNORA_INTEGRATION.md)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| AI 查询 | `POST /api/v1/ai/query` | 发起普通问题请求 | 返回回答，结构稳定 | [ai_handler_comprehensive_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ai_handler_comprehensive_test.go) | 通过 |
| AI 状态 | `GET /api/v1/ai/status` | 请求状态接口 | 返回 AI 服务当前状态 | [ai_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ai_handler_test.go) | 通过 |
| AI 指标 | `GET /api/v1/ai/metrics` | 调用多次查询后请求 | query count、latency 等可见 | [ai_handler_comprehensive_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ai_handler_comprehensive_test.go) | 通过 |
| 知识上传 | `POST /api/v1/ai/knowledge/upload` | 上传合法文档 | 上传成功并返回任务或文档信息 | [ai_handler_comprehensive_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ai_handler_comprehensive_test.go)、[ai_scoped_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/app/server/ai_scoped_handler_test.go) | 部分通过 |
| 知识同步 | `POST /api/v1/ai/knowledge/sync` | 触发同步 | 返回同步结果，无 500 | [ai_handler_comprehensive_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ai_handler_comprehensive_test.go)、[ai_scoped_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/app/server/ai_scoped_handler_test.go) | 部分通过 |
| 启用 Knowledge Provider | `PUT /api/v1/ai/knowledge-provider/enable` | 启用外部知识增强能力 | 状态切换成功 | [ai_handler_comprehensive_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ai_handler_comprehensive_test.go) | 通过 |
| 禁用 Knowledge Provider | `PUT /api/v1/ai/knowledge-provider/disable` | 禁用外部知识增强能力 | 状态切换成功 | [ai_handler_comprehensive_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ai_handler_comprehensive_test.go) | 通过 |
| 熔断器重置 | `POST /api/v1/ai/circuit-breaker/reset` | 先制造失败再重置 | 熔断状态恢复 | [ai_handler_comprehensive_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ai_handler_comprehensive_test.go) | 通过 |
| 外部 Knowledge Provider 不可用回退 | AI 查询主链路 | 关闭当前 knowledge provider 或制造超时 | 系统进入 fallback，而不是整体不可用 | [orchestrated_ai_fallback_integration_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/orchestrated_ai_fallback_integration_test.go) | 通过 |
| pgvector 自建知识库 | `knowledge.provider=pgvector` | 配置 pgvector 并执行查询 | 向量检索正确返回结果 | [provider_test.go](/Users/cui/Workspaces/servify/apps/server/internal/platform/knowledgeprovider/pgvector/provider_test.go) | 未验 |

### 3A. 认证与会话

代码入口：

- [auth_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/auth_handler.go)
- [auth_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/auth_service_test.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 登录与 refresh token 轮转 | `POST /api/v1/auth/login` `POST /api/v1/auth/refresh` | 登录后刷新 token，再尝试复用旧 refresh token | 返回 access/refresh token，新 token 生效，旧 refresh token 失效；真实留档已生成 [manifest.json](/Users/cui/Workspaces/servify/scripts/test-results/auth-session-acceptance/real/manifest.json:1)、[auth-refresh.json](/Users/cui/Workspaces/servify/scripts/test-results/auth-session-acceptance/real/auth-refresh.json:1)、[auth-refresh-reuse-old.json](/Users/cui/Workspaces/servify/scripts/test-results/auth-session-acceptance/real/auth-refresh-reuse-old.json:1) | [auth_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/auth_service_test.go)、[auth_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/auth_handler_test.go)、[test_auth_session_acceptance_test.go](/Users/cui/Workspaces/servify/scripts/test_auth_session_acceptance_test.go)、[manifest.json](/Users/cui/Workspaces/servify/scripts/test-results/auth-session-acceptance/real/manifest.json:1) | 通过 |
| 当前会话列表 | `GET /api/v1/auth/sessions` | 登录后查看当前用户会话 | 返回会话列表、当前会话标记和基础风险字段；真实留档已生成 [auth-sessions-before.json](/Users/cui/Workspaces/servify/scripts/test-results/auth-session-acceptance/real/auth-sessions-before.json:1) 与 [auth-sessions-after-logout-others.json](/Users/cui/Workspaces/servify/scripts/test-results/auth-session-acceptance/real/auth-sessions-after-logout-others.json:1)，证明退出其它会话前后状态变化 | [auth_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/auth_handler_test.go)、[test_auth_session_acceptance_test.go](/Users/cui/Workspaces/servify/scripts/test_auth_session_acceptance_test.go)、[manifest.json](/Users/cui/Workspaces/servify/scripts/test-results/auth-session-acceptance/real/manifest.json:1) | 通过 |
| 退出当前会话/其它会话 | `POST /api/v1/auth/sessions/logout-current` `POST /api/v1/auth/sessions/logout-others` | 执行自助登出 | 当前或其它会话被正确失效；真实留档已生成 [auth-logout-others.json](/Users/cui/Workspaces/servify/scripts/test-results/auth-session-acceptance/real/auth-logout-others.json:1)、[auth-logout-current.json](/Users/cui/Workspaces/servify/scripts/test-results/auth-session-acceptance/real/auth-logout-current.json:1) 和 [auth-sessions-after-logout-current.json](/Users/cui/Workspaces/servify/scripts/test-results/auth-session-acceptance/real/auth-sessions-after-logout-current.json:1)，覆盖其它设备注销与当前 token 失效反证 | [auth_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/auth_handler_test.go)、[auth_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/auth_service_test.go)、[test_auth_session_acceptance_test.go](/Users/cui/Workspaces/servify/scripts/test_auth_session_acceptance_test.go)、[manifest.json](/Users/cui/Workspaces/servify/scripts/test-results/auth-session-acceptance/real/manifest.json:1) | 通过 |

### 4. 客户管理

代码入口：[customer_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/customer_handler.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 创建客户 | `POST /api/customers` | 提交合法客户资料 | 创建成功，返回 ID | [customer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/customer_handler_test.go) | 通过 |
| 客户列表 | `GET /api/customers` | 创建若干客户后查询 | 支持列表和筛选 | [customer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/customer_handler_test.go) | 未验 |
| 客户统计 | `GET /api/customers/stats` | 准备不同客户样本后查询 | 返回统计信息正确 | [customer_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/customer_service_test.go) | 通过 |
| 客户详情 | `GET /api/customers/:id` | 查询已存在客户 | 返回正确详情 | [customer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/customer_handler_test.go) | 通过 |
| 更新客户 | `PUT /api/customers/:id` | 更新字段后重新查询 | 数据被持久化 | [customer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/customer_handler_test.go) | 通过 |
| 活动轨迹 | `GET /api/customers/:id/activity` | 产生客户相关活动后查询 | 轨迹完整、排序正确 | [customer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/customer_handler_test.go) | 未验 |
| 添加备注 | `POST /api/customers/:id/notes` | 添加备注后重查 | 备注被记录 | [customer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/customer_handler_test.go) | 通过 |
| 更新标签 | `PUT /api/customers/:id/tags` | 更新标签后重查 | 标签覆盖或合并符合设计 | [customer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/customer_handler_test.go) | 通过 |

### 5. 客服管理

代码入口：[agent_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/agent_handler.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 创建客服 | `POST /api/agents` | 提交合法资料 | 创建成功 | [agent_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/agent_handler_test.go) | 未验 |
| 客服列表 | `GET /api/agents` | 创建后查询 | 返回客服列表 | [agent_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/agent_handler_test.go) | 通过 |
| 在线客服列表 | `GET /api/agents/online` | 切换在线离线状态后查询 | 仅返回在线客服 | [agent_handler_extended_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/agent_handler_extended_test.go) | 通过 |
| 客服统计 | `GET /api/agents/stats` | 准备不同负载样本后查询 | 统计值正确 | [agent_service_more_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/agent_service_more_test.go) | 通过 |
| 查找可用客服 | `GET /api/agents/find-available` | 准备多个客服状态 | 返回最合适客服 | [agent_service_assignment_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/agent_service_assignment_test.go) | 未验 |
| 更新状态 | `PUT /api/agents/:id/status` | 更新在线/忙碌状态 | 状态持久化 | [agent_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/agent_handler_test.go) | 通过 |
| 上下线切换 | `POST /api/agents/:id/online` `POST /api/agents/:id/offline` | 调用接口并重查 | 状态切换正确 | [agent_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/agent_handler_test.go)、[agent_handler_extended_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/agent_handler_extended_test.go)、[agent_service_more_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/agent_service_more_test.go) | 通过 |
| 会话分配/释放 | `POST /api/agents/:id/assign-session` `POST /api/agents/:id/release-session` | 分配后查看客服负载 | 负载变化正确 | [agent_service_assignment_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/agent_service_assignment_test.go) | 未验 |

### 6. 工单

代码入口：[ticket_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ticket_handler.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 创建工单 | `POST /api/tickets` | 提交合法工单 | 创建成功，返回 ID | [ticket_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ticket_handler_test.go) | 通过 |
| 批量更新工单 | `POST /api/tickets/bulk` | 批量更新多个工单 | 返回批量结果，数据一致 | [ticket_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ticket_handler_test.go)、[command_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/modules/ticket/application/command_service_test.go) | 通过 |
| 工单列表 | `GET /api/tickets` | 创建后查询 | 支持筛选和分页 | [query_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/modules/ticket/application/query_service_test.go)、[ticket_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ticket_handler_test.go) | 通过 |
| 导出工单 CSV | `GET /api/tickets/export` | 准备样本后导出 | 下载成功，字段完整 | [ticket_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ticket_handler_test.go) | 通过 |
| 工单统计 | `GET /api/tickets/stats` | 准备不同状态工单后查询 | 统计值正确 | [query_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/modules/ticket/application/query_service_test.go)、[handler_adapter_test.go](/Users/cui/Workspaces/servify/apps/server/internal/modules/ticket/delivery/handler_adapter_test.go) | 通过 |
| 工单详情 | `GET /api/tickets/:id` | 查询已存在工单 | 返回正确详情 | [ticket_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ticket_handler_test.go) | 通过 |
| 更新工单 | `PUT /api/tickets/:id` | 更新标题、优先级、状态 | 数据被持久化 | [ticket_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ticket_handler_test.go)、[command_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/modules/ticket/application/command_service_test.go) | 通过 |
| 指派工单 | `POST /api/tickets/:id/assign` | 指派给客服 | 负责人变化正确 | [ticket_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ticket_handler_test.go) | 通过 |
| 添加评论 | `POST /api/tickets/:id/comments` | 添加评论后重查 | 评论可读出 | [ticket_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ticket_handler_test.go)、[command_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/modules/ticket/application/command_service_test.go) | 通过 |
| 关闭工单 | `POST /api/tickets/:id/close` | 关闭后重查 | 状态变为关闭，关闭时间正确 | [ticket_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/ticket_handler_test.go)、[command_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/modules/ticket/application/command_service_test.go) | 通过 |

### 6A. 会话工作台

代码入口：

- [conversation_workspace_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/conversation_workspace_handler.go)
- [conversation_workspace_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/conversation_workspace_handler_test.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 会话详情 | `GET /api/omni/sessions/:id` | 查询已存在会话 | 返回正确会话详情与状态 | [conversation_workspace_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/conversation_workspace_handler_test.go) | 部分通过 |
| 消息列表 | `GET /api/omni/sessions/:id/messages` | 查询会话消息 | 返回按时间顺序排列的消息列表 | [conversation_workspace_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/conversation_workspace_handler_test.go) | 部分通过 |
| 发送消息 | `POST /api/omni/sessions/:id/messages` | 在工作台发送消息 | 消息写入持久层并触发 realtime 推送 | [conversation_workspace_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/conversation_workspace_handler_test.go) | 部分通过 |
| 指派会话 | `POST /api/omni/sessions/:id/assign` | 指派客服接管 | 返回成功并更新会话归属 | [conversation_workspace_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/conversation_workspace_handler_test.go) | 部分通过 |
| 转接会话 | `POST /api/omni/sessions/:id/transfer` | 转接到另一位客服 | 返回成功并更新转接结果 | [conversation_workspace_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/conversation_workspace_handler_test.go) | 部分通过 |
| 关闭会话 | `POST /api/omni/sessions/:id/close` | 关闭当前会话 | 会话状态切为 `closed` | [conversation_workspace_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/conversation_workspace_handler_test.go) | 部分通过 |

### 7. 会话转接

代码入口：[session_transfer_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/session_transfer_handler.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 转人工 | `POST /api/session-transfer/to-human` | 发起 AI 到人工转接 | 进入等待或转接成功 | [session_transfer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/session_transfer_handler_test.go) | 未验 |
| 指定客服转接 | `POST /api/session-transfer/to-agent` | 指定客服转接 | 记录目标客服并更新状态 | [session_transfer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/session_transfer_handler_test.go) | 未验 |
| 查询转接历史 | `GET /api/session-transfer/history/:session_id` | 转接后查询历史 | 历史完整可追溯 | [session_transfer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/session_transfer_handler_test.go) | 未验 |
| 查询等待队列 | `GET /api/session-transfer/waiting` | 制造排队后查询 | 返回等待记录 | [session_transfer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/session_transfer_handler_test.go) | 未验 |
| 取消等待 | `POST /api/session-transfer/cancel` | 取消等待记录 | 队列移除成功 | [session_transfer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/session_transfer_handler_test.go) | 未验 |
| 处理排队 | `POST /api/session-transfer/process-queue` | 触发处理 | 有可用客服时完成派发 | [session_transfer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/session_transfer_handler_test.go) | 未验 |
| 自动转接检查 | `POST /api/session-transfer/check-auto` | 构造触发条件后执行 | 符合策略的会话被处理 | [session_transfer_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/session_transfer_handler_test.go) | 未验 |

### 8. 满意度与公开问卷

代码入口：

- [satisfaction_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/satisfaction_handler.go)
- [csat_public_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/csat_public_handler.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 创建满意度记录 | `POST /api/satisfactions` | 提交评价 | 创建成功 | [satisfaction_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/satisfaction_handler_test.go) | 未验 |
| 满意度列表 | `GET /api/satisfactions` | 创建后查询 | 返回列表和筛选结果 | [satisfaction_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/satisfaction_handler_test.go) | 未验 |
| 满意度统计 | `GET /api/satisfactions/stats` | 准备多样本后查询 | 聚合结果正确 | [satisfaction_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/satisfaction_handler_test.go) | 未验 |
| 调查列表 | `GET /api/satisfactions/surveys` | 查询调查任务 | 返回调查清单 | [satisfaction_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/satisfaction_handler_test.go) | 未验 |
| 重发调查 | `POST /api/satisfactions/surveys/:id/resend` | 重发问卷 | 任务状态更新 | [satisfaction_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/satisfaction_handler_test.go) | 未验 |
| 查看满意度详情 | `GET /api/satisfactions/:id` | 查询单条记录 | 返回正确详情 | [satisfaction_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/satisfaction_handler_test.go) | 未验 |
| 更新满意度 | `PUT /api/satisfactions/:id` | 修改评分或备注 | 数据更新成功 | [satisfaction_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/satisfaction_handler_test.go) | 未验 |
| 删除满意度 | `DELETE /api/satisfactions/:id` | 删除后重查 | 记录不可再读 | [satisfaction_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/satisfaction_handler_test.go) | 未验 |
| 按工单查询满意度 | `GET /api/tickets/:id/satisfaction` | 工单已有评价时查询 | 返回对应满意度 | [satisfaction_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/satisfaction_handler_test.go) | 未验 |
| 公开问卷查看 | `GET /public/csat/:token` | 使用有效 token 访问 | 返回问卷内容 | [csat_public_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/csat_public_handler.go) | 通过 |
| 公开问卷提交 | `POST /public/csat/:token/respond` | 提交问卷响应 | 返回成功并持久化 | [csat_public_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/csat_public_handler.go) | 通过 |

### 9. 工作台、宏、集成、自定义字段

代码入口：

- [workspace_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/workspace_handler.go)
- [macro_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/macro_handler.go)
- [app_market_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/app_market_handler.go)
- [custom_field_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/custom_field_handler.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 工作台概览 | `GET /api/omni/workspace` | 请求工作台概览 | 返回聚合视图 | [workspace_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/workspace_handler_test.go)、[workspace_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/workspace_service_test.go) | 通过 |
| 宏列表/创建/更新/删除/应用 | `/api/macros` 系列 | 依次执行 CRUD 与 apply | 规则可持久化且可实际应用 | [macro_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/macro_handler_test.go) | 未验 |
| 应用集成列表/创建/更新/删除 | `/api/apps/integrations` 系列 | 执行 CRUD | 集成配置生效并可回读 | [app_market_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/app_market_handler_test.go) | 未验 |
| 自定义字段列表/详情/创建/更新/删除 | `/api/custom-fields` 系列 | 执行 CRUD | 字段定义持久化并可用于工单等实体 | [custom_field_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/custom_field_handler_test.go) | 未验 |

### 10. 统计、SLA、排班

代码入口：

- [statistics_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/statistics_handler.go)
- [sla_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/sla_handler.go)
- [shift_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/shift_handler.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 仪表盘统计 | `GET /api/statistics/dashboard` | 准备样本数据后查询 | 返回聚合概览 | [statistics_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/statistics_handler_test.go)、[statistics_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/statistics_service_test.go) | 通过 |
| 时间范围统计 | `GET /api/statistics/time-range` | 指定日期范围查询 | 返回按时间切片聚合 | [statistics_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/statistics_service_test.go)、[statistics_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/statistics_handler_test.go) | 通过 |
| 客服绩效统计 | `GET /api/statistics/agent-performance` | 准备客服样本后查询 | 返回绩效排序 | [statistics_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/statistics_service_test.go) | 未验 |
| 工单分类统计 | `GET /api/statistics/ticket-category` | 准备不同分类工单 | 分类聚合正确 | [statistics_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/statistics_handler_test.go) | 未验 |
| 工单优先级统计 | `GET /api/statistics/ticket-priority` | 准备不同优先级工单 | 优先级聚合正确 | [statistics_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/statistics_handler_test.go) | 未验 |
| 客户来源统计 | `GET /api/statistics/customer-source` | 准备不同来源客户 | 来源聚合正确 | [statistics_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/statistics_handler_test.go) | 未验 |
| 每日统计更新 | `POST /api/statistics/update-daily` | 指定日期触发更新 | 更新成功且可被后续查询读到 | [statistics_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/statistics_service_test.go) | 未验 |
| SLA 配置 CRUD | `/api/sla/configs` 系列 | 执行配置增删改查 | 配置生效并可回读 | [sla_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/sla_handler_test.go) | 通过 |
| SLA 优先级配置查询 | `GET /api/sla/configs/priority/:priority` | 查询指定优先级 | 返回正确规则 | [sla_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/sla_handler_test.go) | 通过 |
| SLA 违约列表 | `GET /api/sla/violations` | 准备违约工单后查询 | 返回违约记录 | [sla_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/sla_handler_test.go) | 通过 |
| SLA 违约解决 | `POST /api/sla/violations/:id/resolve` | 解决违约后重查 | 状态变化正确 | [sla_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/sla_handler_test.go) | 通过 |
| SLA 统计 | `GET /api/sla/stats` | 准备样本后查询 | 返回 SLA 统计视图 | [sla_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/sla_handler_test.go) | 通过 |
| 工单 SLA 检查 | `POST /api/sla/check/ticket/:ticket_id` | 对指定工单执行检查 | 返回 SLA 判定结果 | [sla_handler_check_ticket_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/sla_handler_check_ticket_test.go) | 通过 |
| 排班 CRUD 与统计 | `/api/shifts` 系列 | 创建、查询、更新、删除、统计 | 排班记录和统计正确 | [shift_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/shift_handler_test.go) | 未验 |

### 10A. 审计与安全

代码入口：

- [audit_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/audit_handler.go)
- [user_security_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/user_security_handler.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 用户安全态查询 | `GET /api/security/users/:id` | 查询指定用户安全状态 | 返回 `user_id`、`role`、`status`、`token_version` 等字段 | [user_security_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/user_security_handler_test.go) | 通过 |
| 用户 token 失效 | `POST /api/security/users/:id/revoke-tokens` | 触发旧 token 失效 | `token_version` 增加，后续状态可回读 | [user_security_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/user_security_handler_test.go) | 通过 |
| 用户会话列表 | `GET /api/security/users/:id/sessions` | 查询指定用户会话 | 返回会话列表或空列表，结构稳定 | [user_security_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/user_security_handler_test.go) | 通过 |
| 审计日志列表 | `GET /api/audit/logs` | 按 action 等条件查询 | 返回审计记录列表及分页信息 | [audit_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/audit_handler_test.go) | 通过 |
| 审计日志详情 | `GET /api/audit/logs/:id` | 查询单条审计记录 | 返回指定审计记录 | [audit_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/audit_handler_test.go) | 通过 |
| 审计日志差异预览 | `GET /api/audit/logs/:id/diff` | 查看变更 diff | 返回变更路径或明确无差异 | [audit_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/audit_handler_test.go) | 通过 |
| 审计日志导出 | `GET /api/audit/logs/export` | 导出过滤后的日志 | 返回 CSV 内容 | [audit_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/audit_handler_test.go) | 通过 |

### 11. 自动化、知识库、辅助建议、激励

代码入口：

- [automation_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/automation_handler.go)
- [knowledge_doc_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/knowledge_doc_handler.go)
- [suggestion_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/suggestion_handler.go)
- [gamification_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/gamification_handler.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 自动化触发器列表/创建/删除 | `/api/automations` 系列 | 执行 CRUD | 触发器生效并可管理 | [automation_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/automation_handler_test.go) | 未验 |
| 自动化运行记录 | `GET /api/automations/runs` | 触发自动化后查询 | 可查看运行结果 | [automation_service_more_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/automation_service_more_test.go) | 未验 |
| 自动化批量运行 | `POST /api/automations/run` | 触发批量运行 | 返回执行结果，失败项可见 | [automation_service_more_test.go](/Users/cui/Workspaces/servify/apps/server/internal/services/automation_service_more_test.go) | 未验 |
| 知识文档 CRUD | `/api/knowledge-docs` 系列 | 执行 CRUD | 文档可持久化；在已启用且支持对应能力的 knowledge provider 下，创建/更新会同步外部索引，删除若不支持则显式失败 | [knowledge_doc_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/knowledge_doc_handler_test.go)、[service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/modules/knowledge/application/service_test.go) | 部分通过 |
| 公开知识库查询 | `/public/kb/docs` 系列 | 访问公开知识文档 | 可读且权限边界正确 | [knowledge_doc_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/knowledge_doc_handler_test.go) | 通过 |
| 辅助建议 | `GET /api/assist/suggest` `POST /api/assist/suggest` | 传入上下文获取建议 | 返回建议结果 | [suggestion_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/suggestion_handler_test.go) | 未验 |
| 激励排行 | `GET /api/gamification/leaderboard` | 准备积分样本后查询 | 排名正确 | [gamification_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/gamification_handler_test.go) | 未验 |

### 12. 语音

代码入口：[voice_handler.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/voice_handler.go)

| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 协议列表 | `GET /api/voice/protocols` | 请求协议列表 | 返回已注册协议 | [voice_handler_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/voice_handler_test.go) | 通过 |
| 协议信令事件 | `POST /api/voice/protocols/:protocol/call-events/:event` | 发送 invite/answer/hangup 等事件 | 事件被正确映射和处理 | [voice_handler_integration_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/voice_handler_integration_test.go) | 通过 |
| 协议媒体事件 | `POST /api/voice/protocols/:protocol/media-events/:event` | 发送媒体事件 | 媒体状态正确更新 | [voice_handler_integration_test.go](/Users/cui/Workspaces/servify/apps/server/internal/handlers/voice_handler_integration_test.go) | 通过 |
| 开始录音 | `POST /api/voice/recordings/start` | 启动录音 | 返回 recording ID | [service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/modules/voice/application/service_test.go) | 通过 |
| 停止录音 | `POST /api/voice/recordings/stop` | 停止录音后查询 | 录音状态完成 | [recording_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/modules/voice/application/recording_service_test.go) | 通过 |
| 获取录音 | `GET /api/voice/recordings/:recordingID` | 查询录音详情 | 返回录音元数据 | [recording_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/modules/voice/application/recording_service_test.go) | 通过 |
| 转写追加 | `POST /api/voice/transcripts` | 追加语音转写片段 | 转写被保存 | [transcript_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/modules/voice/application/transcript_service_test.go) | 通过 |
| 转写列表 | `GET /api/voice/transcripts` | 追加后查询 | 返回转写内容 | [transcript_service_test.go](/Users/cui/Workspaces/servify/apps/server/internal/modules/voice/application/transcript_service_test.go) | 通过 |

## 权限与异常路径必须额外验

即使主流程通过，也不能直接判定完成，还必须补下面这些：

- 未登录访问 `/api/*` 是否被正确拦截。
- 权限不足访问不同资源是否返回正确错误。
- 非法参数是否返回 `400` 而不是 `500`。
- 查不存在 ID 是否返回 `404` 或明确错误。
- 外部依赖失败时是否有降级、重试或熔断行为。
- 重复提交、并发操作、幂等等是否符合预期。

相关入口见 [router.go](/Users/cui/Workspaces/servify/apps/server/internal/app/server/router.go) 中的鉴权与资源权限中间件注册。

## 建议的验收执行顺序

1. 环境级：`make migrate`、`make build`、`make build-knowledge-provider`、服务启动。
2. 存活级：`/health`、`/ready`、metrics。
3. 核心业务级：客户、客服、工单、会话转接。
4. 增值能力级：AI、知识库、自动化、统计、SLA。
5. 扩展能力级：语音、公开问卷、公开知识库。
6. 负向与权限级：未授权、非法参数、外部依赖失效。

## 对外宣称“已完成”的最低门槛

至少满足以下条件，才能说“该模块完成验收”：

- 核心路由的 happy path 与异常路径都跑通。
- 自动化测试能覆盖该模块主要读写路径。
- 数据落库、状态流转、导出或副作用经过实际核验。
- 对应条目在本清单中被逐项标记为 `通过`，并附证据。

否则，最多只能说：

- “代码入口已存在”
- “构建已通过”
- “部分测试已通过”
- “功能仍待联调/待验收”
