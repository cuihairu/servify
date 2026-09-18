# Servify Execution Todo

这个文件现在作为“可中断、可恢复”的总控待办，不再只是索引页。

使用规则：

1. 先处理 `P0-代码审查问题`，再处理 `P1-上线与交付闭环`，最后处理 `P2/P3-企业级增强`。
2. 一次只把一个任务推进到“有明确产出”的状态：代码、测试、文档、证据，至少完成一类。
3. 每次中断前必须更新：
   - `状态`
   - `最近进展`
   - `下一步`
   - `阻塞项`
4. 恢复执行时，优先从最近一个 `[-]` 项继续，其次处理最近一个 `[!]` 项。
5. 如果某项已经拆到专题文档，`todo.md` 仍保留摘要、优先级和恢复指针，避免执行上下文丢失。

状态约定：

- `[ ]` 未开始
- `[-]` 进行中 / 中断后从这里恢复
- `[x]` 已完成
- `[!]` 高优先级问题，优先于普通 backlog
- `[?]` 需进一步确认

关联文档：

1. [README.md](README.md)
2. [ARCHITECTURE.md](ARCHITECTURE.md)
3. [docs/acceptance-checklist.md](docs/acceptance-checklist.md)
4. [docs/delivery-priorities.md](docs/delivery-priorities.md)
5. [docs/implementation/README.md](docs/implementation/README.md)

---

## 当前结论

基于当前仓库代码审查，现阶段最优先的问题不是“功能完全没写”，而是以下几类真实交付风险：

1. 运行时仍保留生产路径中的 `InMemory` / `mock` / `legacy` 兼容实现，导致“本地可演示”与“生产可交付”之间仍有断层。
2. 验收清单里仍存在一批主链路 `部分通过 / 未验 / 阻塞` 条目，说明代码存在不等于已可交付。
3. 启动、配置、事件、语音、客服运行态等关键基础设施还有明确的企业级硬伤，应先收口再继续扩展功能面。

---

## P0 代码审查问题

这些问题来自本轮直接审查代码后的判断，应优先进入执行序列。

### [!] P0-1 事件总线仍默认使用进程内 `InMemoryBus`

- 现状：
  - `apps/server/cmd/server/main.go` 直接使用 `eventbus.NewInMemoryBus()`
  - `apps/server/internal/app/bootstrap/app.go` 默认也初始化 `eventbus.NewInMemoryBus()`
- 风险：
  - 事件不持久化，进程重启即丢失 in-flight event
  - 自动化、统计、审计等异步能力的交付边界仍依赖单进程存活
  - 不利于企业部署下的重启恢复、扩缩容、可回放排障
- 代码证据：
  - `apps/server/cmd/server/main.go`
  - `apps/server/internal/app/bootstrap/app.go`
  - `apps/server/internal/platform/eventbus/inmemory_bus.go`
- 执行要求：
  - 明确“生产支持的事件总线边界”
  - 如果短期不引入外部 MQ，至少要把 runtime boundary、失败补偿、恢复策略、运维告警写实
  - 若引入可持久化总线，需要保留兼容接口并补回归测试
- 验收标准：
  - 明确 dev/demo 与 prod 的事件总线策略
  - 至少一条异步链路可证明重启后不会静默丢失关键业务结果，或明确声明当前不承诺 durability 并落实监控/告警/死信审计
- 状态：`[x]`
- 最近进展：已补齐 Redis client 的 bootstrap 初始化与共享注入链路，`BuildApp -> BuildEventBus -> BuildRuntime` 现可真正装配 `redis` event bus；同时补齐 `miniredis` 级别的 RedisBus 持久化、订阅分发、health 检查测试，并修复 `dispatchFromStream()` 依赖 `XRead(..., "$")` 导致刚发布消息可能被跳过的窗口丢消息问题，当前已能证明 Redis provider 下至少一条真实异步链路可持久化并被本地订阅者消费
- 完成证据：
  - 代码文件：
    - `apps/server/internal/app/bootstrap/redis.go`
    - `apps/server/internal/app/bootstrap/app.go`
    - `apps/server/internal/app/bootstrap/app_test.go`
    - `apps/server/internal/platform/eventbus/redis_bus.go`
    - `apps/server/internal/platform/eventbus/redis_bus_test.go`
    - `apps/server/internal/app/server/runtime.go`
    - `apps/server/internal/app/server/router.go`
    - `apps/server/internal/app/server/health.go`
    - `apps/server/internal/handlers/health_enhanced.go`
    - `apps/server/internal/handlers/health_enhanced_test.go`
    - `apps/server/cmd/cli/run_enhanced.go`
  - 验证命令：
    - `go test ./internal/platform/eventbus`
    - `go test ./internal/app/bootstrap ./internal/app/server ./internal/handlers ./internal/modules/agent/infra`
    - `go test ./cmd/cli ./internal/releasecheck`
- 完成证据：
  - 代码文件：
    - `apps/server/internal/config/config.go`
    - `apps/server/internal/config/config_test.go`
    - `apps/server/internal/app/bootstrap/eventbus.go`
    - `apps/server/internal/app/bootstrap/app.go`
    - `apps/server/internal/app/bootstrap/app_test.go`
    - `apps/server/cmd/server/main.go`
    - `apps/server/cmd/cli/run.go`
    - `apps/server/cmd/cli/run_enhanced.go`
    - `config.yml`
    - `config.staging.example.yml`
    - `config.production.secure.example.yml`
  - 验证命令：
    - `go test ./internal/app/bootstrap ./internal/config`
    - `go test ./cmd/server ./cmd/cli`

### [!] P0-2 Voice 运行时仍依赖 mock provider

- 现状：
  - `apps/server/internal/app/server/runtime.go` 中 `RecordingService` / `TranscriptService` 仍注入 `voice/provider/mock`
- 风险：
  - 录音、转写链路目前不具备真实生产语义
  - 虽然仓储已落 GORM，但 provider 仍是 mock，会让“已持久化”掩盖“未真正接入外部能力”
- 代码证据：
  - `apps/server/internal/app/server/runtime.go`
  - `apps/server/internal/modules/voice/provider/mock/*`
- 执行要求：
  - 明确 provider 抽象和真实 provider 的接入方式
  - 至少把 mock provider 从默认 prod runtime 中剥离，改为显式 dev/test 配置
- 验收标准：
  - 默认生产配置下不再隐式使用 mock provider
  - dev/test 配置与 prod 配置边界清晰
  - voice 基础链路至少有一条真实 provider 或明确的 no-op/disabled product boundary
- 状态：`[x]`
- 最近进展：已将 voice 录音/转写 provider 改为显式配置，默认值收口为 `disabled`，开发样例配置显式使用 `mock`，并阻止 `production` 环境继续装配 mock provider
- 完成证据：
  - 代码文件：
    - `apps/server/internal/config/config.go`
    - `apps/server/internal/config/config_test.go`
    - `apps/server/internal/modules/voice/provider/disabled/provider.go`
    - `apps/server/internal/app/server/voice_runtime.go`
    - `apps/server/internal/app/server/voice_runtime_test.go`
    - `apps/server/internal/app/server/runtime.go`
    - `apps/server/internal/handlers/voice_handler.go`
    - `apps/server/internal/handlers/voice_handler_test.go`
    - `config.yml`
    - `config.staging.example.yml`
    - `config.production.secure.example.yml`
  - 验证命令：
    - `go test ./internal/config ./internal/app/server ./internal/handlers ./internal/modules/voice/...`

### [!] P0-3 Agent 运行态仍依赖内存注册表与 legacy runtime 适配层

- 现状：
  - `apps/server/internal/services/agent_service_assembly.go` 仍使用 `agentinfra.NewInMemoryRegistry()`
  - `legacyRuntime` / `maintenance sync` 仍存在
- 风险：
  - 客服在线状态、负载、会话分配仍偏向单进程运行时模型
  - 重启、扩容、多实例下的一致性边界不够明确
- 代码证据：
  - `apps/server/internal/services/agent_service_assembly.go`
  - `apps/server/internal/services/agent_service.go`
  - `apps/server/internal/services/agent_legacy_runtime_adapter.go`
- 执行要求：
  - 明确 agent presence / load / assignment 的真实来源
  - 决定是持久化 registry、外部协调层，还是显式声明单实例约束
- 验收标准：
  - 客服上下线、负载、会话接管在重启后行为明确
  - 文档与实现对齐，不再让内存态伪装成企业能力
- 状态：`[x]`
- 最近进展：运行时已改为复用 bootstrap 注入的共享 Redis client，`BuildRuntime` 不再固定回退到 `nil`；同时已修复 `RedisRegistry.handleStatusChange("online:123")` 的 payload 解析 bug，并补回归测试锁定 malformed/offline payload 行为。当前进一步新增 integration 测试，证明两套 `BuildAgentServiceAssembly(...)` 在共享 Redis + DB 下，第一套写入的 online 状态会被第二套观测到，且 session transfer 后 `from/to agent` 的 `CurrentLoad` 也会在另一实例被正确观测到，说明 presence + load/transfer 两个关键面已有多实例证据
- 完成证据：
  - 代码文件：
    - `apps/server/internal/app/bootstrap/redis.go`
    - `apps/server/internal/app/bootstrap/app.go`
    - `apps/server/internal/app/server/runtime.go`
    - `apps/server/internal/modules/agent/infra/redis_registry.go`
    - `apps/server/internal/modules/agent/infra/redis_registry_test.go`
    - `apps/server/internal/services/agent_service_redis_integration_test.go`
    - `apps/server/internal/modules/agent/application/repositories.go`
    - `apps/server/internal/modules/agent/application/service.go`
    - `apps/server/internal/modules/agent/infra/gorm_repository.go`
    - `apps/server/internal/services/agent_service.go`
    - `apps/server/internal/services/agent_service_assembly.go`
    - `apps/server/internal/services/agent_runtime_maintenance.go`
    - `apps/server/internal/modules/routing/delivery/handler_adapter.go`
    - `apps/server/internal/modules/agent/application/service_test.go`
    - `apps/server/internal/services/agent_service_assignment_test.go`
    - `apps/server/internal/services/agent_service_more_test.go`
    - `apps/server/internal/services/agent_runtime_maintenance_test.go`
    - `apps/server/internal/services/agent_legacy_runtime_adapter_test.go`
  - 验证命令：
    - `go test ./internal/modules/agent/... ./internal/services ./internal/modules/routing/delivery`
    - `go test -tags integration ./internal/services -run 'Test(BuildAgentServiceAssembly_UsesRedisRegistryAcrossInstances|BuildAgentServiceAssembly_SyncsTransferLoadAcrossInstances)'`

### [!] P0-4 配置加载仍存在直接 `panic`，且默认模型配置偏旧

- 现状：
  - `apps/server/internal/config/config.go` 的 `Load()` 在 `viper.Unmarshal` 失败时直接 `panic`
  - 默认 OpenAI 模型仍为 `gpt-3.5-turbo`
- 风险：
  - 配置错误时服务启动失败不可控，不利于企业部署排障
  - 默认模型值容易与当前产品策略、真实支持矩阵脱节
- 代码证据：
  - `apps/server/internal/config/config.go`
- 执行要求：
  - 把配置错误改为显式返回错误，由上层决定退出方式
  - 清理默认模型策略，避免误导性默认值进入生产
- 验收标准：
  - 配置解析错误可被测试覆盖、日志可读
  - 默认 AI 配置与 README / docs / 实际 provider 策略一致
- 状态：`[x]`
- 最近进展：已将 `config.Load()` 改为显式错误返回，`LoadConfig`/CLI 调用链同步收口，并统一默认 OpenAI 模型常量为 `gpt-4.1-mini`
- 完成证据：
  - 代码文件：
    - `apps/server/internal/config/config.go`
    - `apps/server/internal/config/config_test.go`
    - `apps/server/internal/app/bootstrap/config.go`
    - `apps/server/cmd/cli/token.go`
    - `apps/server/cmd/cli/token_decode.go`
    - `apps/server/internal/platform/llm/openai/provider.go`
    - `apps/server/internal/services/ai.go`
    - `config.yml`
    - `config.weknora.yml`
  - 验证命令：
    - `go test ./internal/config ./internal/app/bootstrap ./cmd/cli`

### [!] P0-5 Fallback 配置仍暴露 `legacy_kb_enabled`，兼容语义未完全收口

- 现状：
  - 配置结构中仍有 `Fallback.LegacyKBEnabled`
  - 服务与文档中仍存在大量 `legacy` / `WeKnora compatibility` 语义
- 风险：
  - 对外能力命名与内部实现命名混用
  - 长期会放大配置理解成本和运维误判
- 代码证据：
  - `apps/server/internal/config/config.go`
  - `apps/server/internal/services/ai.go`
  - `apps/server/internal/services/ai_enhanced.go`
  - `apps/server/internal/handlers/ai_handler.go`
- 执行要求：
  - 把“兼容实现”与“对外能力”命名分层
  - 逐步清理公开配置中的 `legacy` 表述
- 验收标准：
  - 面向用户/运维的配置与状态接口不再以 `legacy` 作为核心能力命名
  - compatibility 路径只保留在内部实现或迁移文档中
- 状态：`[x]`
- 最近进展：已将 fallback 公共配置收口到 `knowledge_base_enabled`，保留 `legacy_kb_enabled` 仅作兼容输入，并同步收口 AI/Enhanced/Orchestrated 状态输出中的 legacy/weknora 核心命名
- 完成证据：
  - 代码文件：
    - `apps/server/internal/config/config.go`
    - `apps/server/internal/config/config_test.go`
    - `apps/server/internal/services/ai.go`
    - `apps/server/internal/services/ai_interface_test.go`
    - `apps/server/internal/services/ai_enhanced.go`
    - `apps/server/internal/services/ai_enhanced_unit_test.go`
    - `apps/server/internal/services/orchestrated_ai_enhanced.go`
    - `apps/server/internal/services/orchestrated_ai_enhanced_test.go`
    - `config.weknora.yml`
    - `WEKNORA_IMPLEMENTATION_COMPLETE.md`
  - 验证命令：
    - `go test ./internal/config ./internal/services ./internal/handlers`
    - `go test -tags integration -run "TestOrchestratedEnhancedAIService" ./internal/services`

---

## P1 上线与交付闭环

这些是“代码已有，但必须补齐证据或收尾”的上线级事项。

### [!] P1-1 AI / Knowledge 主链路验收闭环

- 目标：
  - 把 `upload` / `sync` / `enable-disable` / `fallback` 从“部分通过”推进到“通过”
- 关联文档：
  - `docs/acceptance-checklist.md`
  - `docs/delivery-priorities.md`
- 关键动作：
  - 补真实 provider 场景下的运行证据
  - 区分 Dify 主路径与 WeKnora compatibility 路径
  - 为失败、超时、fallback 生成可留档证据
  - 收口 `knowledge-docs` 与外部 knowledge provider 的索引一致性边界，避免管理端 CRUD 与 AI 检索链路继续脱节
- 验收标准：
  - 至少一条真实文档上传、同步、查询命中成功
  - fallback 有实际日志、响应、状态三类证据
- 状态：`[-]`
- 最近进展：**2026-09-15** fallback 三类证据（日志 / 响应 / 状态）本地真实留证完成：1) 前置代码修复——`orchestrated_ai_enhanced.go` 的 fallback 回退分支原先**没有任何日志语句**（fallback 发生时运维无感知），已补 `strategy=fallback` warn 日志（含 session_id 与触发错误详情）并配套日志断言回归；2) 新增 `scripts/test-ai-fallback-acceptance.sh`（`make ai-fallback-acceptance` 一键留证）：sqlite 起真实服务且不配任何外部 AI provider，真实完成 `GET /api/v1/ai/status`（`fallback_enabled=true`，状态证据）、`POST /api/v1/ai/query`（`strategy=fallback` 且中文回复非空，响应证据；fallback 由 openai provider 真实收到 `401 auth_failed` 触发）、`GET /api/v1/ai/metrics`（`fallback_usage_count` 字段在，指标证据）、无 token 401 拒绝路径；日志证据为 `server-log.txt` 中 fallback 专属 warn 行 + GIN query access log；3) 已如实记录的结构性事实：`/api/v1/ai/metrics` 走全局服务实例而 query 每请求构建独立 service，故 metrics 端点的 `fallback_usage_count` 不累积 per-request fallback 次数（留证以结构与真实 200 为准）；4) manifest 已入库 `scripts/test-results/ai-fallback/manifest.json`，`validate-acceptance-manifest.sh` 新增 `ai-fallback` 分支，mock 回归 `go test ./scripts -run TestAIFallbackAcceptanceScriptWritesEvidence` 通过；5) `docs/acceptance-checklist.md` 已补“AI fallback 真实运行验收（P1-1）”专项记录
- 前序进展：**2026-09-14** 本地收口 WeKnora 删除链路：1) `pkg/weknora` 客户端 `WeKnoraInterface` 新增 `DeleteDocument(ctx, kbID, docID)`，`Client` 实现对接 `DELETE /api/v1/knowledge/:kb_id/documents/:doc_id`（与 `UploadDocument` 端点风格自洽，响应 `success=false` 或非 2xx 均显式报错）；2) `knowledgeprovider/weknora` Provider 的 `DeleteDocument` 从返回 `ErrOperationNotSupported` 改为真实调用（client 未配置 / 外部文档 ID 缺失 / knowledge base ID 缺失均显式报错）；3) capability matrix 中 WeKnora 的 `deletion` 能力翻转为已启用，与 Dify 对齐，`knowledge-docs` 删除链路对两大 provider 均可同步外部索引；4) 补齐 client / provider / capabilities 单测回归，受影响包覆盖率保持 100%；5) `docs/acceptance-checklist.md` 已更新 knowledge-docs 与 provider 索引一致性边界描述
- 前序进展：**2026-04-22** 继续收紧 AI / Knowledge 真实验收口径：1) `test-dify-integration.sh` 的 real 模式除 upload/sync 外，新增要求 `knowledge_provider_enabled=true`、`knowledge_provider_healthy=true`，且 `ai-query` 不能只落入 `fallback`；2) `test-weknora-integration.sh` 的 real 模式新增要求当前 provider 必须是 `weknora`、provider 启用且健康，并强制验证 `disable -> fallback query` 的降级证据；3) 两个脚本现在都会额外输出 `manifest.json`，把 provider、mode、关键检查结果和证据文件列表结构化；4) 新增 `validate-acceptance-manifest.sh`、`check-acceptance-evidence.sh` 以及对应 `make` 入口，可在真实环境跑完后直接校验证据集，也可在仓库存在 tracked manifest 时做批量扫描；5) CI `script-checks` 已接入 acceptance evidence 校验，且只检查已纳入 git 的 manifest，避免把本地临时产物误算成正式证据；6) `docs/WEKNORA_INTEGRATION.md` 与脚本测试已同步更新，避免“接口成功但真实 provider 主路径未命中”仍被误填成通过
- 下一步：使用真实 WeKnora 或 Dify 服务完成 upload/sync 端到端验收，并把成功/失败证据回填到 `docs/acceptance-checklist.md`，将 `知识上传`、`知识同步`、`AI 控制面人工运行验证第二轮/第三轮` 从 `部分通过` 推进到 `通过`
- 阻塞项：真实 WeKnora / Dify 服务环境与凭证；当前 Windows 本机无法用 `go test ./scripts` 直接模拟 bash 脚本对 `httptest` 端口的访问，需要在 Linux/CI 或真实环境执行脚本留档

### [x] P1-2 Auth 自助 session 链路补齐真实验收

- 范围：
  - `login`
  - `refresh`
  - `sessions`
  - `logout-current`
  - `logout-others`
- 风险：
  - 企业客户会把这组能力视为安全基本面
- 验收标准：
  - refresh token 轮转成功
  - 旧 refresh token 复用失败
  - 当前会话与其它会话退出都能看到状态变化
- 状态：`[x]`
- 最近进展：**2026-04-25** 已在本机用 SQLite 运行口径启动 Servify，并实际执行 `AUTH_ACCEPTANCE_MODE=real SERVIFY_URL=http://127.0.0.1:18081 ./scripts/test-auth-session-acceptance.sh`；生成了 `scripts/test-results/auth-session-acceptance/real/manifest.json` 与对应接口响应留档，且 `validate-acceptance-manifest.sh` 校验通过。`docs/acceptance-checklist.md` 的 3A 条目现已按真实留档更新为 `通过`
- 下一步：把 `scripts/test-results/auth-session-acceptance/real/manifest.json` 纳入 git，并继续推进 AI / Knowledge real 证据留档，避免验收闭环只停在 auth-session
- 阻塞项：无代码阻塞；剩余是证据入库和 AI / Knowledge 外部依赖环境

### [x] P1-3 会话工作台主操作补齐到“通过”

- 范围：
  - 会话详情
  - 发消息
  - 接管
  - 转接
  - 关闭
- 风险：
  - 客服主链路不能长期停留在“整体通过、分项阻塞”
- 验收标准：
  - 每个操作都有可回溯的真实请求结果与状态变化证据
  - 发布口径下不再仅依赖自动化通过
- 状态：`[x]`
- 完成证据：
  - 代码文件：`scripts/test-workspace-acceptance.sh`、`scripts/test_workspace_acceptance_test.go`、`scripts/validate-acceptance-manifest.sh`（workspace provider）
  - 测试命令：`make workspace-acceptance`（真实服务 `DB_DRIVER=sqlite`，2026-09-14）；`go test ./scripts -run TestWorkspaceAcceptance`
  - 文档或验收回填位置：`scripts/test-results/workspace-acceptance/manifest.json` 已入库；`docs/acceptance-checklist.md` 会话工作台六项置“通过”并附真实留档链接，专项验证记录新增“会话工作台真实运行验收（P1-3）”
- 附注：首轮真实运行暴露 `text-message` 契约差异（内容必须在 `data.content`，顶层 content 被服务端静默丢弃导致会话未创建），已修正脚本并收紧 mock 契约

### [x] P1-4 运行基线最小事实补齐

- 范围：
  - `GET /ready`
  - `GET <metricsPath>`
  - `make build`
  - `GET /api/v1/messages/platforms`
- 风险：
  - 这些能力未验会直接影响上线口径
- 验收标准：
  - 全部回填到 `docs/acceptance-checklist.md`
- 状态：`[x]`
- 完成证据：
  - 代码文件：`scripts/test-security-acceptance.sh`、`scripts/test-runtime-baseline-acceptance.sh`、`scripts/test_runtime_baseline_acceptance_test.go`、`scripts/validate-acceptance-manifest.sh`（security-baseline / runtime-baseline provider）
  - 测试命令：`make security-acceptance` 与 `make runtime-baseline-acceptance`（2026-09-15，本机真实执行；runtime-baseline 内含 `make build` + sqlite 真实服务）；`go test ./scripts -run "Test(Security|RuntimeBaseline)Acceptance"`
  - 文档或验收回填位置：`scripts/test-results/security-baseline/manifest.json` 与 `scripts/test-results/runtime-baseline/manifest.json` 已入库；`docs/acceptance-checklist.md` 就绪检查 / Prometheus 指标 / CLI 标准构建 / 平台消息路由统计 / 安全基线严格校验五行补真实留档链接，专项验证记录新增“安全基线真实配置留证（P1-4）”与“运行基线真实运行验收（P1-4）”
- 附注：前置修复 12 处 `sh` 调用 bash 语法脚本的问题（dash 下 `${BASH_SOURCE[0]}`/`[[` 直接报错，`make security-check`/`release-check`/`local-check` 此前在 Linux 本机无法执行）；并修正“staging 口径 release readiness 演练”旧记录——staging 示例配置含占位凭证，security-check/release-check 对其如实拒绝才是正确行为

### [x] P1-5 Ticket 主闭环剩余高频操作补齐

- 范围：
  - 更新工单
  - 评论
  - 关闭
  - 统计
  - 导出
- 验收标准：
  - 对应 API 从 `未验` 或 `部分通过` 推进到 `通过`
- 状态：`[x]`
- 完成证据：
  - 代码文件：`scripts/test-ticket-acceptance.sh`、`scripts/test_ticket_acceptance_test.go`、`scripts/validate-acceptance-manifest.sh`（ticket provider）
  - 测试命令：`make ticket-acceptance`（真实服务 `DB_DRIVER=sqlite`，2026-09-14）；`go test ./scripts -run TestTicketAcceptance`
  - 文档或验收回填位置：`scripts/test-results/ticket-acceptance/manifest.json` 与 `ticket-export.csv` 已入库；`docs/acceptance-checklist.md` 工单更新/评论/关闭/统计/导出行已附真实留档链接，专项验证记录新增“Ticket 高频操作真实运行验收（P1-5）”

---

## P2 企业级项目差距

这些不是当前最紧急代码缺陷，但属于“距离企业级项目”的重点 backlog。

### [-] P2-0 客户侧推荐问题与上下文联想问题（核心链路已落地，RQ-5 埋点待做）

- 当前判断：
  - 已有后台辅助推荐接口 `GET/POST /api/assist/suggest`（保留，坐席辅助定位）
  - 新增客户侧公开接口 `GET /public/suggestions/initial` 与 `GET|POST /public/suggestions/next`，匿名可用，仅返回公开知识文档标题
  - 推荐策略定为“规则+公开知识库”（不依赖外部 AI）：首屏按 recency 逆序，下一问按 token 命中评分排序（`ScoreText`），meta 携带 intent/tokens/doc_candidates
  - 隐私边界：`is_public=false` 知识文档与工单标题不出现在客户侧响应（工单候选仅进入 meta 计数）
- 代码证据：
  - `apps/server/internal/modules/suggestion/application/customer_questions.go`（InitialQuestions / NextQuestions）
  - `apps/server/internal/modules/suggestion/infra/gorm_repository.go`（FindPublicKnowledgeDocs / FindPublicKnowledgeDocCandidates，均带 is_public 过滤）
  - `apps/server/internal/handlers/suggestion_handler.go`（RegisterPublicSuggestionRoutes）
  - `apps/server/internal/app/server/router_public.go` + `security_surface.go`（public-suggestions 目录项）
  - `sdk/packages/core/src/sdk.ts`（getInitialQuestions / getNextQuestions）、`sdk/packages/vanilla/src/index.ts`
  - `apps/demo-sdk/widget.js`（“猜你想问” chips，点击即发送，发送后刷新联想）
  - `scripts/test-suggestion-acceptance.sh` + `scripts/test-results/suggestion-acceptance/manifest.json`
- 产品目标：
  - 客户刚进入咨询页时，能看到可点击的推荐问题
  - 客户发起几轮对话后，系统可基于当前上下文动态联想下一问
  - 推荐问题可与知识库、历史工单、热门问题、AI 意图识别联动
- 建议拆分：
  - `RQ-1` 首屏热门问题推荐 ✅ initial 接口 + 首屏 chips
  - `RQ-2` 会话内上下文联想问题 ✅ next 接口（query=客户最近一条消息）
  - `RQ-3` 客户侧推荐接口与权限边界 ✅ 公开路由 + security surface + 仅公开文档
  - `RQ-4` Web SDK / demo / 官网接入 ✅ core/vanilla SDK + demo widget
  - `RQ-5` 埋点、点击率、转化率与验收口径 ⬜ 未做（接口 meta 已留扩展位）
- 验收标准：
  - 客户未输入前可拿到一组推荐问题 ✅ initial（recency 逆序，验收断言顺序）
  - 客户输入后可拿到一组基于上下文变化的联想问题 ✅ next GET/POST（评分命中，验收断言唯一命中）
  - 推荐项可点击进入提问，不只是展示静态文案 ✅ demo widget 点击即填入并发送
  - 前后端、SDK、验收文档有统一口径 ✅ 脚本断言 meta.strategy 两态
- 状态：`[-]`（核心链路收口；RQ-5 埋点转 P2 backlog）
- 最近进展：后端公开接口 + core/vanilla SDK + demo widget + 真实验收脚本（sqlite 自起，断言公开/私有隔离、recency 顺序、GET/POST 命中、400 拒绝路径）全部通过，manifest 已留档
- 下一步：RQ-5 埋点（点击/曝光/转化）需产品口径定稿后再做
- 阻塞项：无

### [x] P2-1 多实例与高可用边界明确化

- 范围：
  - agent online/runtime state
  - WebSocket / WebRTC 会话路由
  - event bus 异步链路
  - worker 幂等和重启恢复
- 目标：
  - 明确 Servify 当前是单实例优先，还是支持多实例协同
- 验收标准：
  - 文档、部署说明、运行时行为一致
- 状态：`[x]`
- 最近进展：**2026-09-17** 逐面审计四个范围的真实运行时行为后定稿交付边界——**单实例优先（single-instance-first），生产 `replicas: 1`**：1) 新增 [docs/multi-instance-boundary.md](docs/multi-instance-boundary.md) 逐面声明边界（每条带代码证据）：agent presence/load（Redis + TTL + 双实例 integration 测试）多实例安全 ✅；WS/WebRTC 实时路由为进程内 hub，单实例硬约束 ❌；event bus（Redis provider）为广播语义（每实例订阅 pub/sub 通知后各自读 stream，每条事件 N 实例各处理一次，`consumerGroup` 常量未启用，通知丢失即跳过无重放）⚠️；worker 重启恢复成立（状态全落 DB），认领逐 worker 差异——routing 等待队列是原子租约 CAS（正面样板）✅，automation timer 认领读非原子但 CompleteTimer CAS、webhook 认领读非原子（多实例重复投递）❌、扫描式 worker 重复扫描 ⚠️；rate limiter 进程内 token bucket（多副本实际限额 = 配置 × N）；2) `deployment.md` K8s 示例 `replicas: 2` → `1` 并加 `maxSurge: 0` 滚动策略（消除瞬时双实例窗口），附约束说明——这是 P2-1 要修的“文档与运行时不一致”实锤点；3) 运行时声明用测试锚定：`redis_bus_multi_instance_test.go` 锁死 event bus 广播语义（两实例共享 Redis 各收一次），未来改竞争消费会显式失败并强制同步文档；4) 文档注册进 Docs Pages 站点（operationsPages）；5) 升级路径按面拆账写入文档（worker 租约化 → bus consumer group → WS 跨实例广播 → 全局限流），不一次性承诺
- 完成证据：
  - 代码文件：`docs/multi-instance-boundary.md`（新增）、`docs/deployment.md`（replicas/strategy 修正）、`docs/.vitepress/site-structure.ts`（站点注册）、`apps/server/internal/platform/eventbus/redis_bus_multi_instance_test.go`（新增）
  - 测试命令：`go test -count=1 -race ./internal/platform/eventbus`
  - 文档回填位置：本文条目 + `docs/multi-instance-boundary.md`

### [x] P2-2 配置治理与环境分层强化

- 范围：
  - dev/staging/prod 配置模板
  - 敏感配置来源
  - 启动前校验
  - 配置漂移检查
- 目标：
  - 防止示例配置、开发默认值和生产配置混用
- 验收标准：
  - 配置加载、校验、模板、文档完全对齐
- 状态：`[x]`
- 最近进展：**2026-09-17** 审计四个模板与校验链路后三处收口：1) **模板漂移门禁**——新增 `config/config_template_lint_test.go`：反射 Config 结构生成 yaml schema 树（map 键通配、slice-of-struct 元素继续校验），四个模板（config.yml / staging / production.secure / weknora）逐一比对，未知键（typo、残留键被 viper 静默忽略）CI 显式失败；审计结论是现有四个模板均无未知键，此前"模板漂移"实为有意省略 + 缺失；2) **实锤缺口修复**——staging 与 production 模板此前**均无 database 节**，回退 GetDefaultConfig 的 dev 密码（`dev-password-change-in-production` 在 InsecureDatabasePasswords 黑名单）→ 两个模板按原样根本无法通过启动校验（模板不可用）；已为两模板补 `database` 节（`${DB_*}` 占位符），门禁同时断言 staging/production 的 `database.password` 与 `jwt.secret` 必须显式 `${ENV}` 占位、递归拒绝疑似明文凭证叶子键（password/secret/api_key/auth_token/access_key 等）、environment 声明与文件用途一致；3) **环境严格度收口**——`server.environment` 此前是自由字符串且只有 `== "production"` 才硬校验，写 "prod" 即绕过；新增 `canonicalEnvironment`（prod/PROD/Production → production，stage/staging → staging，dev/development → development）在 normalizeConfig 归一；staging（预生产）提升为与 production 同等严格度——不安全默认值直接拒绝启动（占位凭证未注入展开为空同样被拒，属预期行为）；4) 顺手修正 production 模板 event_bus 注释与 P2-1 边界文档冲突的"multi-instance deployment"表述；5) 文档对齐——`docs/configuration-scopes.md` 新增"模板治理与环境严格度（P2-2）"节（门禁规则、严格度分级、凭证注入链路）
- 完成证据：
  - 代码文件：`apps/server/internal/config/config_template_lint_test.go`（新增）、`apps/server/internal/config/config.go`（canonicalEnvironment + Validate 严格度）、`apps/server/internal/config/config_test.go`（staging 严格度 / 别名归一 / prod 别名经 Load 拒绝三组回归）、`config.staging.example.yml` 与 `config.production.secure.example.yml`（补 database 节）、`docs/configuration-scopes.md`
  - 测试命令：`go test -count=1 ./internal/config`；受影响的 staging 环境消费包 `go test -count=1 ./internal/platform/configscope ./internal/handlers`
  - 端到端验证：两模板注入 env 后 `check-security-baseline --strict` 均通过（`go run ./cmd -c config.staging.example.yml check-security-baseline --strict` + DB/SERVIFY_JWT_SECRET/OPENAI/DIFY 环境变量）；不注入时按预期拒绝（`database.password is empty or using a default value`），证明补 database 节后模板真实可用
  - acceptance 脚本适配：`scripts/test-security-acceptance.sh` 原设计前提是"模板不带 database 段、脚本追加 deployment 段"——P2-2 后模板显式带 `${DB_*}` 占位 database 节，追加会产生 yaml duplicate key；改为纯环境变量注入（production 正例注入 DB_*/JWT/AI 凭证 strict 通过；staging 负例注入基础设施凭证过启动校验、空 AI 凭证被基线检查点名拒绝），`go test ./scripts` 全包绿
  - 文档回填位置：`docs/configuration-scopes.md` + 本条目

### [x] P2-3 数据恢复、备份与迁移演练（2026-09-18 闭环）

- 范围：
  - 数据库迁移回滚
  - 审计/工单/会话关键表恢复
  - 上传文件与知识文档资产恢复
- 目标：
  - 从“可迁移”升级到“可恢复”
- 验收标准：
  - 至少一轮备份恢复演练证据
- 进展（2026-09-18）：
  - `internal/platform/recovery` 包（100% 覆盖，race ×5）：数据库逻辑备份
    （JSONL 逐表 + manifest sha256 + 自增序列位置）、事务内整体校验恢复
    （单连接 + 单事务，PRAGMA foreign_keys 连接级状态固定）、上传文件
    tar.gz 归档与字节级校验恢复/对账（VerifyFiles）
  - `cmd/dbrecovery` 运维入口（100% 覆盖）：db-backup / db-restore /
    files-backup / files-restore 四子命令；db 子命令在连接前显式拒绝
    postgres 方言（pg 形态走 pg_dump SOP，防 sqlite 逻辑备份误灌生产库）
  - 迁移回滚口径定稿：迁移无 down，回滚 = 恢复备份；postgres 演练落 CI
    Integration job（pg_dump -Fc → 删光 users → DROP WITH (FORCE) →
    pg_restore → schema_migrations=8|f 与行数随备份回来）
  - sqlite 演练脚本 `scripts/test-backup-restore.sh`（make
    backup-restore-acceptance）：播种关键表 → 备份 → 注入迁移窗口增量 +
    损坏 → 恢复 → 逐表行数/内容/序列对账；上传分支篡改+删除+新增检测与
    字节级恢复；evidence 留档 `scripts/test-results/backup-restore/`，
    manifest 经 validate-acceptance-manifest.sh 新 case 校验，纳入
    script-checks 门禁
  - 文档 `docs/backup-and-recovery.md`：双轨定位、RPO/RTO、关键表清单、
    知识文档外部 provider 边界（Dify/WeKnora 内容不在 servify 备份范围）、
    失败迁移处置；已注册站点结构
  - 演练证据：`scripts/test-results/backup-restore/manifest.json`
    （provider=backup-restore，五 checks 全 true，db 44 表 10 行 + 上传
    2 文件对账归零）

### [-] P2-4 可观测性从“有指标”升级到“可运维”（四刀全部完成，验收口径见下）

- 范围：
  - 关键业务 SLI/SLO
  - 异步失败告警
  - AI/provider 失败分类
  - 远程协助与实时链路诊断
- 目标：
  - 运维能在故障时快速定位，不依赖人工翻日志
- 验收标准：
  - 告警规则、dashboard、runbook 三者一致
- 进展（分刀推进）：
  - 第一刀（AI/provider 失败分类 + 三者一致门禁）✅
    - `ai_requests_total` 增加 strategy 标签（primary/fallback/transfer），
      provider 取值 dify/weknora/internal/none；编排服务五条路径全打点，
      token 与时长指标接线（`orchestrated_ai_enhanced.go`）
    - HTTP 中间件对 429 计数，`ratelimit_dropped_total{path}` 真正进入
      scrape 面（原指标只存在于 legacy 手写 exposition，主路径不可见）
    - 一致性门禁 `consistency_test.go`：rules.yaml / dashboard JSON /
      runbook 告警段三者对齐，未接线指标（known-gaps.md）禁止出现在
      任何 PromQL 中；摘除 6 条永真沉默的死告警与对应死面板
    - 新增 AIFallbackRatioHigh 告警 + AI 策略分布/降级占比面板与处置条目
  - 第二刀（conversations/tickets/routing 埋点 + business 面板恢复）✅
    - 会话创建、工单创建/解决、路由决策（handoff/assign/transfer）三处
      业务服务接线 BusinessMetrics（链式 AttachBusinessMetrics，nil 安全）
    - tenant 体系尚未落地，tenant_id 标签暂记 default
    - business dashboard 的 Conversations/Tickets/Routing 三面板恢复
  - 第三刀（eventbus/worker middleware 装配接线 + 异步失败告警恢复）✅
    - ObservableBus 装饰总线：cmd/server/main.go 一处接线，Subscribe 经
      WrapHandler 覆盖全部订阅方（handled/failed/duration），Publish 记
      published{event_type, outcome}；同步总线口径 outcome=error 含
      handler 失败（另有 failed_total 单独计数）
    - 死信计数补齐：`eventbus_dead_letter_total{event_type}`，死信落库
      成功时计数（BusMiddleware 挂 InMemoryDeadLetterRecorder，容量 1000）
    - worker 接线：RegisterDefaultWorkers 注册完成后统一 ObservableWorker
      包装（jobs_total{worker_name, outcome} + active_jobs）；
      sync.Once 保证 collector 进程级单例（测试多次装配不撞注册）
    - 接线点选 cmd/server/main.go + app/worker：server→async→bootstrap
      依赖循环使 bootstrap 内无法装配 worker
    - 恢复 3 条异步告警（EventBusHandlerFailures/EventBusDeadLetters/
      WorkerJobFailures）+ service dashboard 事件总线/Worker 两面板；
      known-gaps 只剩 errors_total（第四刀）与 worker_job_duration_seconds
      （需周期 job 级 TrackJob，留后续）
  - 第四刀（errors_total 统一出口 + SLI/SLO burn rate 告警）✅
    - `errors_total{severity, error_category, error_module}` 接线：
      `observability/errors` 新增 `StatusMiddleware`，与 HTTPMetrics 中间件
      同一挂载条件（监控启用时），响应完成后按最终状态码对 5xx 打点——
      502/504 归 dependency/network（上游依赖语义），其余 5xx 归
      system/internal；2xx/4xx 不计数（限流 429 已有
      ratelimit_dropped_total，避免双计）。`RegisterErrorMetrics` 加
      sync.Once 保证多次装配不重复注册（runtime_assembly.go 一处调用）
    - SLO 首批定稿（`deploy/observability/slo.md`）：availability 99.9%
      （30d，坏事件=5xx）、latency 99% 请求 <2s；Google SRE 多窗 burn
      rate 告警三条（SLOAvailabilityFastBurn 14.4x 1h+5m / 
      SLOAvailabilitySlowBurn 6x 6h+30m / SLOLatencyFastBurn 14.4x 1h+5m），
      与 HighHTTP5xxRate/HighP99Latency 症状告警分层（预算告警+症状告警）
    - service dashboard 新增 SLO Error Budget 面板（30d 两类预算剩余）；
      runbook 新增三条处置段；known-gaps 摘除 errors_total，只剩
      worker_job_duration_seconds（需周期 job 级 TrackJob，独立遗留项）
    - 一致性门禁自动覆盖全部新增资产（四类文件一致性测试全绿）

### [-] P2-5 安全治理继续收口到首批企业交付标准

- 范围：
  - session 风险策略
  - refresh token 治理
  - 审批与回滚证据
  - 公开接口治理
- 目标：
  - 从“最小可部署治理”提升到“企业试点可接受”
- 验收标准：
  - 对应安全面能力均有真实验收，不只靠单测
- 进展（2026-09-18，第一刀：公开接口运行时安全基线）：
  - 安全响应头中间件（nosniff/frame-anticsniff 框架拒绝/Referrer-Policy/
    可选 CSP/可选 HSTS），`security.headers` 配置节，默认关闭保持既有部署
    行为；安全头挂载在限流/body 上限之前，429/413 中止响应同样携带
  - 全局请求体上限 `security.max_body_bytes`：Content-Length 超限 413，
    分块传输由 http.MaxBytesReader 兜底
  - CORS 多 origin 白名单按请求回显 Origin + Vary: Origin（修复
    strings.Join 输出非法多值 ACAO 头），白名单外不带 ACAO
  - WS 建连 Origin 白名单 `security.websocket_allowed_origins`
    （atomic.Value 承载；空白名单放行保持 WebView 访客兼容）
  - 本地 /uploads/* 弃用 gin Static：只服务具体文件，目录/缺失/根路径
    一律 404（http.Dir 拒绝 `..` 逃逸）
  - 三份配置模板同步 security 新节（漂移门禁通过）；生产/staging 显式
    开启基线，开发模板默认关闭
  - 真实验收：scripts/test-public-surface-acceptance.sh 起服实测 11 项
    （安全头/CORS echo+拒绝+预检/413/429/uploads 文件+目录 404/WS 403+101），
    manifest provider=public-surface 入库留档，validate-acceptance-manifest
    正反 case 齐备；文档 public-surface-security-checklist 同步落地章节
- 进展（2026-09-18，第二刀：auth 审计与登录风险执行）：
  - audit 平台扩展 Options.AuditFailures：4xx/5xx 一并落库（success=false
    + 真实 status_code），MiddlewareWithOptions 保持管理面默认行为不变；
    auth 组挂载含失败留痕的审计中间件（登录/注册/refresh/2FA/登出成败
    全留痕，凭据字段沿用 [REDACTED] 脱敏）
  - 登录风险执行下沉 service 层：LoginRiskIntel 接口依赖反转，HTTP 情报
    provider 直接实现；高风险判定=富网络标签（内建四类安全标签永不触发，
    接入外部情报源标注 hosting/proxy 才构成）；档位 off（默认零行为变化）/
    step_up（已绑 TOTP 强制挑战绕 kill-switch、未绑定拒绝）/block（403
    不回显判定依据）；`security.session_risk.login_enforcement` 配置，
    生产/staging 模板 step_up、开发模板 off
  - 真实验收：scripts/test-auth-audit-acceptance.sh stub 情报源标注
    hosting → block 阶段正确凭据 403、错误密码 401；off 阶段同库重启
    正常登录 200；admin token 经 GET /api/audit/logs 对账 blocked/bad/
    clean 三类登录行与 [REDACTED] 脱敏且无明文泄漏、register 行留痕；
    manifest provider=auth-audit 入库留档，validate 正反 case 齐备；
    文档 security-baseline-operations 与 auth-surface-policy 同步
- 进展（2026-09-18，第三刀：refresh reuse 家族吊销）：
  - refresh token 重放处置下沉 service 层：WithRefreshReusePolicy 档位
    （off / revoke_family）；rotateRefreshSession 的版本不匹配分支细分——
    token 声明版本 < 库内当前版本（即已轮换旧 token 重放）且档位开启时
    吊销整个会话（家族），最新 token 一并失效迫使重新登录；版本大于库内
    的伪造 token 只拒绝不吊销；吊销 UPDATE 失败 fail-closed（仍拒绝）；
    对外统一 401 不回显吊销状态与版本差异
  - `security.session_risk.refresh_reuse_policy` 配置，生产/staging 模板
    revoke_family、开发模板 off；off 档位行为与既有版本完全一致（拒绝但
    会话存活）
  - 真实验收：scripts/test-refresh-reuse-acceptance.sh 两阶段同库对比——
    off 阶段旧 token 重放 401 且最新 token 继续刷新 200（会话存活）；
    revoke_family 阶段重放 401 且家族最新 token 一并 401、重新登录 200；
    经 GET /api/audit/logs?action=auth.refresh 对账拒绝行（3 次 401）与
    成功行；manifest provider=refresh-reuse 入库留档，validate 正反 case
    齐备；文档 security-baseline-operations 与 auth-surface-policy 同步

### [ ] P2-6 管理端产品化收尾

- 范围：
  - Satisfaction
  - Session Transfer
  - Customer/Agent/Workspace 运营细节
  - Ticket/Statistics 细分运营查询
- 目标：
  - 让运营团队可持续日常使用，而不只是跑演示
- 验收标准：
  - 高频后台功能不存在大面积 `未验`

### [ ] P2-7 SDK 与多端 contract 稳定性治理

- 范围：
  - `sdk/packages/*`
  - surface governance
  - 版本同步
  - 示例可运行性
- 目标：
  - 防止 API 面和 SDK 面发生漂移
- 验收标准：
  - SDK smoke test、example smoke test、surface governance 全绿

### [ ] P2-8 性能、容量与压测基线

- 范围：
  - WebSocket 连接数
  - AI 查询延迟
  - ticket/conversation 高并发读写
  - 文件上传与知识同步
- 目标：
  - 从“能跑”提升到“知道能承受多少负载”
- 验收标准：
  - 至少有一版容量基线和压测结论

---

## P3 架构与技术债清理

### [ ] P3-1 清理公开语义中的 legacy/compat 混名

- 目标：
  - 内部兼容层继续保留，但对外命名统一到产品语义

### [ ] P3-2 收拢 services 与 modules 的最终边界

- 目标：
  - 继续减少 glue code 和 facade 长期滞留

### [ ] P3-3 清理 demo/mock/in-memory 资产的默认暴露面

- 目标：
  - 默认生产路径不再误接入 demo/mock 能力

### [ ] P3-4 统一运行时装配方式

- 范围：
  - `cmd/server`
  - `cmd/cli/run`
  - `cmd/cli/run_enhanced`
- 目标：
  - 减少重复 wiring 与配置漂移

---

## 执行顺序

建议严格按这个顺序推进：

1. `P0-1` 事件总线边界收口
2. `P0-2` Voice mock provider 剥离
3. `P0-3` Agent runtime 内存态收口
4. `P0-4` 配置 panic 与默认值治理
5. `P0-5` legacy 配置命名清理
6. `P1-1` AI / Knowledge 验收闭环
7. `P1-2` Auth session 验收闭环
8. `P1-3` 会话工作台收口
9. `P1-4` 基线事实补齐
10. `P1-5` Ticket 高频操作验收
11. 再进入 `P2/P3`

---

## 中断恢复模板

每次执行某个任务前，把对应条目下面更新为：

- 状态：`[-]`
- 最近进展：一句话说明已完成什么
- 下一步：一句话说明下次继续做什么
- 阻塞项：没有就写“暂无”

如果任务完成，更新为：

- 状态：`[x]`
- 完成证据：
  - 代码文件
  - 测试命令
  - 文档或验收回填位置

---

## 当前恢复点

- 当前优先恢复任务：P2-5 最后一刀（审批回滚链路真实验收）或按用户指示推进下一项（第一刀公开接口运行时安全基线、第二刀 auth 审计与登录风险执行、第三刀 refresh reuse 家族吊销均已落地，见 P2-5 进展）
- 原因：P2-0 核心链路已收口（RQ-5 埋点等产品口径定稿后启动）；P2-1 完成“文档、部署说明、运行时行为一致”三收口；P2-2 完成“配置加载、校验、模板、文档完全对齐”四收口；P2-3 完成“可迁移→可恢复”（recovery 包 + dbrecovery 工具 + sqlite/pg 双轨演练证据 + 文档）；P2-4 完成 AI/provider 失败分类、业务埋点、异步观测、errors_total 统一出口与 SLO burn rate 四刀；P2-5 前三刀（公开接口运行时安全基线、auth 审计与登录风险执行、refresh reuse 家族吊销）完成——安全响应头/body 上限/CORS 多 origin 回显/WS Origin 白名单/uploads 禁目录列举 + auth 面含失败审计 + 登录风险执行 + refresh 家族吊销，三份真实验收入库。`P1-1` 仅剩真实 Dify/WeKnora 双路径运行证据（等外部环境与凭证）
- 附注（2026-09-17）：P2-4 第四刀完成——`errors_total` 经 HTTP 层 StatusMiddleware 统一出口接线（5xx 分类打点，2xx/4xx 不计），SLO 首批定稿 availability 99.9% / latency 99%<2s，三条多窗 burn rate 告警 + SLO Error Budget 面板 + runbook 处置段，一致性门禁覆盖；known-gaps 只剩 worker_job_duration_seconds（需周期 job 级 TrackJob，独立遗留项）
- 附注（2026-09-14）：`P1-3` / `P1-5` 已真实运行闭环——`make workspace-acceptance` / `make ticket-acceptance` 在 sqlite 真实服务上跑通并入库 manifest（本机无 Postgres/Redis/Docker；server 原生支持 `DB_DRIVER=sqlite`，Redis 仅 `event_bus.provider=redis` 时必需）
- 附注（2026-09-15）：`P1-4` 已整体闭环——`make security-acceptance`（security-check 真实配置留证）与 `make runtime-baseline-acceptance`（build / ready / metrics / platforms 真实运行留证）均已入库 manifest；顺带修复 12 处 `sh` 调用 bash 脚本导致 `make security-check` / `release-check` / `local-check` 在 Linux 本机无法执行的问题。`P1-1` 的 fallback 三类证据（日志 / 响应 / 状态）也已本地真实留证闭环（`make ai-fallback-acceptance`，manifest 已入库）。同日：`P1-8` 闭环（乱码存量经复扫已清零，新增 `make text-encoding-check` 仓库级编码门禁并挂入 CI script-checks）；`P0-6` / `P0-7` / `P0-8` / `P1-2` 标题标记与已完成的条目状态对齐翻转为 `[x]`。P1 序列只剩 `P1-1` 验收标准第一条——真实文档上传 / 同步 / 查询命中的 Dify/WeKnora 双路径运行证据（等外部环境）
- 如果本轮无法推进实现，至少先补：
  - 真实边界文档
  - 默认 prod 策略
  - 风险说明
  - 告警/死信/恢复规则

## 2026-04-22 全局基础功能审核补充

- 审核结论：后端主干基础能力不是“未实现”，当前主要风险集中在验收闭环、权限语义一致性、真实环境证据和少量输入/错误边界。
- 已验证：`go test ./apps/server/internal/services ./apps/server/internal/handlers ./apps/server/internal/app/server ./apps/server/internal/app/bootstrap` 通过；`go test ./apps/server/internal/modules/...` 通过。
- 已修复：`/api/voice/*` 从 `assist` 权限拆分为独立 `voice` 权限，默认 agent 权限补 `voice.read` / `voice.write`，并新增路由鉴权回归，避免辅助推荐权限误授权语音能力。
- 已确认：P1-4 运行基线最小事实在 `docs/acceptance-checklist.md` 已记录为通过，并已有 `/ready`、`/metrics`、`/api/v1/messages/platforms` 自动化/发布自检证据。
- 残留风险：`config.yml` 与 `config.staging.example.yml` 的 strict security baseline 会因占位 JWT、数据库、OpenAI/Dify 凭证失败；这是开发/模板配置占位问题，不是本轮代码改动引入。继续交付前应使用真实安全配置执行 `make security-check CONFIG=...` 留证。

---

## 2026-04-16 全库审查补充

本节补充的是“全仓级”问题，不只覆盖 server，也覆盖 SDK、demo-sdk、管理端文本质量、启动脚本和架构收口情况。

### [x] P0-6 SDK 与后端协议漂移收口

- 现状：
  - `sdk/packages/core/src/api.ts` 仍在调用旧接口：`/api/sessions`、`/api/messages`、`/api/ai/ask`、`/api/ai/status`、`/api/upload`、`/api/webrtc/call/*`、`/api/satisfaction`、`/api/queue/*`、`/api/customers/:id/tickets`
  - 服务端当前公开路由已收口到 `/api/omni/*`、`/api/v1/ai/*`、`/api/v1/upload`、`/api/satisfactions`、`/api/v1/ws`
  - `sdk/packages/core/src/sdk.ts` 的默认 WebSocket 地址仍是 `this.config.apiUrl.replace(/^http/, 'ws') + '/ws'`
- 风险：
  - SDK 默认调用路径与后端真实 contract 不一致，集成方按 SDK 接入会直接失败或进入伪成功状态
  - WebSocket 默认地址错误会让实时会话链路在默认配置下失效
  - demo、文档、前端接入层会继续围绕错误 contract 叠加兼容逻辑
- 代码证据：
  - `sdk/packages/core/src/api.ts`
  - `sdk/packages/core/src/sdk.ts`
  - `apps/server/internal/app/server/router.go`
- 执行要求：
  - 先明确“SDK 公开 contract 是否继续兼容旧 REST 语义”，不要边实现边漂移
  - 如果以后端当前路由为准，需要统一修正 SDK path、WebSocket path、测试样例和文档
  - 如果保留兼容层，必须把兼容入口显式落到服务端而不是留给调用方猜测
- 验收标准：
  - SDK 默认 HTTP / WebSocket 配置可直接连通当前 server
  - SDK contract、server router、demo-sdk、README 对外口径一致
  - 至少有一条 SDK smoke test 覆盖会话创建、消息发送、WebSocket 建连
- 状态：`[x]`
- 最近进展：已把 README、React/Vue/Vanilla 示例、`apps/demo-sdk` 文档与预构建 bundle 全部收口到当前 WebSocket-first contract，并通过脚本完成 demo-sdk 产物回归同步
- 完成证据：
  - `sdk/packages/core/src/sdk.ts`
  - `sdk/packages/core/src/api.ts`
  - `sdk/packages/core/src/websocket.ts`
  - `sdk/packages/core/README.md`
  - `sdk/examples/react/src/App.tsx`
  - `sdk/examples/react/src/components/ChatDemo.tsx`
  - `sdk/examples/vue/src/main.ts`
  - `sdk/examples/vue/src/App.vue`
  - `sdk/examples/vanilla/index.html`
  - `apps/demo-sdk/README.md`
  - `apps/demo-sdk/servify-sdk.esm.js`
  - `apps/demo-sdk/servify-sdk.umd.js`
  - `scripts/sync-sdk-to-admin.sh`
  - 验证命令：`bash ./scripts/sync-sdk-to-admin.sh`
  - 验证命令：`npm -C sdk run typecheck`
  - 验证命令：`npm -C sdk run test:core`
  - 验证命令：`npm -C sdk run test:examples`

### [x] P0-7 SDK 工程门禁失效修复

- 现状：
  - `sdk/package.json` 聚合了多个 workspace 的 `typecheck`
  - `sdk/packages/core/package.json`、`sdk/packages/transport-http/package.json`、`sdk/packages/transport-websocket/package.json` 仍使用 `npx tsc --noEmit`
  - 实际执行 `npm -C sdk run typecheck` 失败，说明当前门禁不能稳定反映 SDK 是否健康
- 风险：
  - SDK 改动无法通过稳定的类型检查门禁
  - CI / 本地环境对 `tsc` 解析结果不一致，会让错误被环境噪音掩盖
  - 在 contract 已漂移的情况下，缺少可靠门禁会放大回归风险
- 代码证据：
  - `sdk/package.json`
  - `sdk/packages/core/package.json`
  - `sdk/packages/transport-http/package.json`
  - `sdk/packages/transport-websocket/package.json`
- 执行要求：
  - 把 `typecheck` 明确绑定到 workspace 内的 TypeScript 编译器，不要依赖不稳定的 `npx` 解析
  - 补一条 SDK 根目录可复现的本地门禁命令，并纳入 CI
  - 修复后再补跑 SDK 主要包的构建与测试
- 验收标准：
  - `npm -C sdk run typecheck` 稳定通过或给出真实类型错误
  - CI 与本地使用相同门禁入口
  - SDK 各包不再因为错误的 `tsc` 解析导致假失败
- 状态：`[x]`
- 最近进展：已把所有 workspace 的 `typecheck` 改为显式调用根目录 `typescript` 编译器，并把 `sdk/package.json` 的根级门禁扩展到 `api-client`、`app-core`、`transport-http`、`transport-websocket`
- 完成证据：
  - `sdk/package.json`
  - `sdk/packages/core/package.json`
  - `sdk/packages/react/package.json`
  - `sdk/packages/vue/package.json`
  - `sdk/packages/vanilla/package.json`
  - `sdk/packages/api-client/package.json`
  - `sdk/packages/app-core/package.json`
  - `sdk/packages/transport-http/package.json`
  - `sdk/packages/transport-websocket/package.json`
  - 测试命令：`npm -C sdk run typecheck`

### [x] P0-8 网站与部署脚本路径失配

- 现状：
  - `Makefile` 的 `website-dev`、`website-deploy` 仍指向 `apps/website-worker`
  - 仓库实际存在的是 `apps/website`
  - `website-pages-deploy` 与 `apps/website/README.md` 才和当前目录结构一致
- 风险：
  - 官网本地启动和部署命令会直接失败
  - 新同事或 CI 按 `Makefile` 操作会得到错误路径，影响交付和演示
  - 这类基础脚本失配会降低仓库可信度
- 代码证据：
  - `Makefile`
  - `apps/website/README.md`
- 执行要求：
  - 统一网站开发、部署、README 的目录口径
  - 删除失效脚本，或补回缺失目录，二者必须二选一
  - 顺手检查其他 `Makefile` 入口是否还有同类陈旧路径
- 验收标准：
  - `website-dev`、`website-deploy`、`website-pages-deploy` 与实际目录一致
  - README、Makefile、部署配置使用同一套路径
  - 新环境按文档执行可直接跑通
- 状态：`[x]`
- 最近进展：已把 `website-dev`、`website-deploy` 改到真实目录 `apps/website`，并将 deploy 配置切换到现有 `apps/website/wrangler.jsonc`
- 完成证据：
  - `Makefile`
  - `apps/website/README.md`
  - 验证命令：`rg -n "website-dev:|website-deploy:|website-pages-deploy:|apps/website/wrangler.jsonc" Makefile`

### [ ] P1-6 Bootstrap 落地与入口 wiring 收口

- 现状：
  - `apps/server/internal/app/bootstrap/app.go` 仍是骨架，只收集最小运行时依赖
  - 注释中明确写明后续再把 config、logging、db、router、worker wiring 迁移进来
  - 实际启动 wiring 仍大量堆在 `apps/server/cmd/server/main.go`
  - `ARCHITECTURE.md` 已写到 bootstrap / app wiring 抽取目标，但实现成熟度还没跟上
- 风险：
  - 架构文档与真实入口不一致，增加维护和排障成本
  - 新能力继续接入时，会把 `main.go` 变成长期的装配垃圾场
  - 测试和复用入口难以围绕统一 bootstrap 构建
- 代码证据：
  - `apps/server/internal/app/bootstrap/app.go`
  - `apps/server/cmd/server/main.go`
  - `ARCHITECTURE.md`
- 执行要求：
  - 明确 bootstrap 的职责边界，避免继续出现“双入口装配”
  - 把 logging、db、router、workers、shutdown 逐步迁入 bootstrap
  - 文档只描述已经落地的结构，不提前透支成熟度
- 验收标准：
  - `cmd/server/main.go` 只保留薄入口职责
  - bootstrap 成为唯一可信的运行时装配根
  - `ARCHITECTURE.md` 与实际目录、责任划分一致
- 状态：`[-]`
- 最近进展：已把 server 启动的 flag/env 覆盖解析、数据库重试连接、默认 worker 注册、runtime attach、router/server 绑定以及统一 shutdown 生命周期收口到 `bootstrap` / `app` 层，并已同步回写 `ARCHITECTURE.md` 的当前落地边界；本轮继续收口 logger wiring，`BuildApp()` 已改为直接初始化共享 logger，`cmd/server` / `cmd/cli run` / `cmd/cli run_enhanced` 不再各自重复初始化并覆盖 `app.Logger`，默认 worker 也开始复用 `app.Logger` 而不是回退到 `logrus.StandardLogger()`
- 下一步：继续减少 legacy/compat 入口对具体 `internal/services` 的直连，优先处理 `P1-7 Modules 与 legacy services/models 的边界收口`
- 阻塞项：暂无

### [x] P1-7 Modules 与 legacy services/models 的边界收口

- 现状：
  - 多个 `internal/modules/*` 仍直接依赖 `internal/models`
  - 部分 delivery adapter / contract 仍直接依赖 `internal/services`
  - 典型位置包括 `apps/server/internal/modules/ai/delivery/handler_adapter.go`、`apps/server/internal/modules/knowledge/delivery/handler_contract.go`、`apps/server/internal/modules/customer/delivery/handler_contract.go`
- 风险：
  - modules 只是目录拆分，不是真正的边界拆分
  - 新老架构长期并存会让依赖方向持续失控
  - 未来做测试替身、模块复用、职责下沉时成本会越来越高
- 代码证据：
  - `apps/server/internal/modules/ai/delivery/handler_adapter.go`
  - `apps/server/internal/modules/knowledge/delivery/handler_contract.go`
  - `apps/server/internal/modules/customer/delivery/handler_contract.go`
  - `apps/server/internal/modules/*`
- 执行要求：
  - 先定义哪些 `internal/models` 属于共享领域模型，哪些只是 legacy GORM model
  - delivery 层不要继续直接耦合 legacy services，改为依赖明确的 application contract
  - 避免为了“看起来模块化”继续新增 adapter 叠层
- 验收标准：
  - modules 对 `internal/services` 的直接依赖显著收缩
  - 共享模型、持久化模型、对外 DTO 三者边界清晰
  - 新增模块不再默认引用 legacy services/models
- 状态：`[x]`
- 最近进展：**2026-09-15** 完成两件事：1) 依赖地图已输出到 [docs/modules-dependency-map.md](docs/modules-dependency-map.md)——modules→services 直连 5 文件（ai/customer/knowledge 的 delivery 层）、modules→models 直连 15 模块约 60 文件，类型按"共享领域核心 / 模块自有"分类并给出迁移顺序，**拆分策略已定稿**（共享核心保留 shared kernel、模块自有类型按引用面从小到大迁入模块、别名过渡、禁止新增），解除本条阻塞项；2) 第一刀已落地——modules 对 `internal/services` 的直连清零：契约类型下沉（`modules/ai/delivery/contract_types.go`、`modules/customer/application/contract_types.go`、`modules/knowledge/application/contract_types.go`），legacy `internal/services` 改为类型别名反向引用（`ai.go`/`ai_enhanced.go`/`customer_service.go`/`knowledge_doc_service.go`），两侧是同一 Go 类型，wire contract 与既有测试零漂移；新增 parser 级边界门禁 `TestModulesDoNotImportLegacyServices` 并纳入 CI；顺带修复 CI script-checks 的 `-run` 白名单历史缺口（security/runtime-baseline/ai-fallback/auth-session/workspace/ticket 的 mock 回归与 9 个 Validate/Check 变体此前从未在 CI 执行，现已全部命中，22/22）；3) 推送后 Docs Pages 工作流因 swag 无法解析跨包类型别名失败（swag 按包声明名索引限定符，`application` 跨模块重名），已把 customer 契约从 `modules/customer/application` 迁至唯一命名包 `modules/customer/api`（package `customerapi`），`customer_handler.go` 注解改写 `customerapi.CustomerX` 后 swag 无 flag 解析成功、受控 API 文档资产已再生成；4) 模块自有 models 类型迁移第一刀（voice）完成——`VoiceCall` / `VoiceRecording` / `VoiceTranscript` 三个 GORM 模型迁入 `modules/voice/infra/models.go`，`internal/models` 保留类型别名（migrate 注册与 webhook 快照只读零改动），voice/infra 全部文件去掉 `internal/models` import；5) 第二刀（webhook）完成——`models/webhook.go` 整文件迁入 `modules/webhook/domain/models.go`（`WebhookEndpoint` / `WebhookDelivery` + 四个投递状态常量，模块三层全消费故落 domain），`internal/models` 保留类型与常量别名，migrate 注册与 handlers 测试零改动；6) 顺序纠偏——gamification / suggestion 经逐类型核对没有模块自有类型（只消费共享核心与 knowledge 自有的 KnowledgeDoc），无需迁移，剩余顺序为 automation → quality → assist → knowledge；7) 第三刀（automation）完成——`models/automation.go` 整文件迁入 `modules/automation/domain/models.go`（`AutomationTrigger` / `AutomationRun` / `AutomationTimer`），`internal/models` 保留类型别名，migrate 注册、legacy `services/automation_*` 与 handlers 测试经别名零改动，剩余 quality → assist → knowledge；8) 第四刀（quality）完成——`QualityReview` 迁入 `modules/quality/domain/models.go`，包声明用唯一的 `qualitydomain`（首个被 swag 注解引用的迁移对象：`quality_handler.go` 注解经 models 别名链解析，实证 swag 产物零漂移），migrate 注册与 handlers 测试零改动，剩余 assist → knowledge；9) 第五刀（assist）完成——`RemoteAssistSession` / `RemoteAssistAnnotation` 迁入 `modules/assist/domain/models.go`（package 声明用唯一的 `assistdomain`，5 处 swag 注解经 models 别名链解析、产物零漂移），migrate 注册与 handlers 零改动，剩余 knowledge；10) 第六刀（knowledge，最后一刀）完成——`KnowledgeDoc` / `KnowledgeIndexJob` 迁入 `modules/knowledge/domain/models.go`，向量列值对象 `Embedding` / `NewEmbedding` 从 `models/embedding.go` 整文件随迁 `domain/embedding.go`（不随迁则 domain 反向依赖 models 成环），`internal/models` 保留类型别名与 `NewEmbedding` 转发函数，migrate 注册、pgvector provider、legacy services 与 handlers 经别名零改动；knowledge 的 handler 无 swag 注解，domain 包声明保持通用 `domain`；knowledge 模块 6 个文件全部去掉 `internal/models` import，Scan/Value 测试随文件迁入 domain 包保住覆盖率——**至此 P1-7 的"模块自有 models 类型迁移"全部完成**（voice/webhook/automation/quality/assist/knowledge 六刀收口）。**2026-09-16 收口判定：三条验收标准全部达成，状态翻转为完成**——1) modules 对 internal/services 的直接依赖已清零且由 parser 级门禁 `TestModulesDoNotImportLegacyServices`（纳入 CI）禁止回潮；2) 共享模型（shared kernel）/ 持久化模型（模块自有 GORM 迁入各模块 domain·infra）/ 对外 DTO（契约类型下沉模块、legacy 侧别名过渡）三层边界经 [docs/modules-dependency-map.md](docs/modules-dependency-map.md) 定稿并全部落地；3) 新增引用由门禁强制禁止（modules 不得 import internal/services；不得新增引用 internal/models 中"模块自有"分类类型）
- 下一步：模块自有类型迁移已全部完成；剩余方向是第 3 节定稿的 shared kernel 收口（modules 对共享核心的引用约束为"只读领域形状"）与第 1 节别名的最终态清理（跟随 P3-2 services/modules 最终边界收口）
- 阻塞项：暂无（拆分策略已定稿）

### [x] P1-8 文本编码、对外文案与仓库可读性修复

- 现状：
  - 仓库存在多处乱码或编码异常，包括 `todo.md`、`README.md`、`apps/admin/config/routes.ts`、`apps/admin/src/pages/Login/index.tsx`、`apps/admin/src/app.tsx`
  - 当前 `pnpm -C apps/admin typecheck` 虽然通过，但这只能说明 TS 语法可过，不代表文本可交付
- 风险：
  - README、管理端页面、待办文档会直接影响对外可读性和团队协作
  - 乱码文件会让评审、交接、验收和后续补丁变得脆弱
  - 继续在异常编码文件上叠加修改，后续修复成本会更高
- 代码证据：
  - `todo.md`
  - `README.md`
  - `apps/admin/config/routes.ts`
  - `apps/admin/src/pages/Login/index.tsx`
  - `apps/admin/src/app.tsx`
- 执行要求：
  - 统一仓库文本编码策略，优先收口为 UTF-8
  - 先修对外文档、主导航、登录页这类高可见文件
  - 修复时避免语义漂移，先保真再润色
- 验收标准：
  - 高可见文档和管理端核心页面不再出现乱码
  - 新提交文本文件编码策略明确且可检查
  - 评审、编辑、补丁工具可稳定处理这些文件
- 状态：`[x]`
- 完成证据：
  - 代码文件：`scripts/check-text-encoding.sh`（新增 `make text-encoding-check`，并已挂入 CI `script-checks` 的 `Validate text encoding` 步骤）
  - 复核结论：历史记录中所列乱码文件（`todo.md` / `README.md` / admin `routes.ts` / Login 页 / `app.tsx`）在后续迭代中已被清理，2026-09-15 全仓库复扫 1464 个 tracked 文本文件：0 个非 UTF-8、0 个 UTF-8 BOM、0 个 U+FFFD；并已用 U+FFFD 负例验证门禁能真实拦截（拦截后清理恢复通过）
  - 检查命令：`make text-encoding-check`（本地与 CI 同口径）
  - 文档回填位置：`todo.md` 本条目
- 最近进展：**2026-09-15** 乱码存量已在历史迭代中清零，本轮补上“编码策略明确且可检查”的缺口——新增仓库级文本编码门禁（tracked 文本文件必须 UTF-8、无 BOM、无 U+FFFD），纳入本地 Makefile 与 CI script-checks，防止评审/交接/补丁流程再次引入编码异常
- 下一步：无
- 阻塞项：暂无

### [ ] P1-9 demo-sdk 生成链路与源码一致性回归

- 现状：
  - `apps/demo-sdk` 目录内存在 `servify-sdk.esm.js`、`servify-sdk.umd.js`、`widget.js` 等产物
  - 当前无法从目录结构直接证明这些产物是否由 `sdk/packages/*` 自动生成，还是手工同步
  - 在 SDK contract 已漂移的前提下，这些产物极可能继续放大不一致
- 风险：
  - demo 展示的能力与真实 SDK 源码不一致
  - 调试时可能误以为问题在 server，实际是 demo 引用陈旧 bundle
  - 发布链路不清晰会让回归验证失真
- 代码证据：
  - `apps/demo-sdk/servify-sdk.esm.js`
  - `apps/demo-sdk/servify-sdk.umd.js`
  - `apps/demo-sdk/widget.js`
  - `sdk/packages/*`
- 执行要求：
  - 明确 demo-sdk 产物来源、生成命令、更新时间和发布责任
  - 如果是构建产物，补可复现脚本，不要手工维护 bundle
  - 如果是快照产物，至少要建立与 SDK 源码版本的对应关系
- 验收标准：
  - demo-sdk 的 bundle 来源可追溯、可重建
  - SDK 变更后能一键回归 demo 产物
  - demo 展示行为与当前 SDK contract 一致
- 状态：`[x]`
- 最近进展：已把 demo-sdk 产物同步入口显式收口到 `scripts/sync-sdk-to-demo.sh`，保留 `sync-sdk-to-admin.sh` 兼容包装，并同步更新 Makefile、CI、生成物文档和 demo/mock 边界文档；当前 `apps/demo-sdk` 三个受控产物已具备“源码来源 -> 一键同步 -> 漂移校验”的闭环
- 完成证据：
  - 代码文件：
    - `scripts/sync-sdk-to-demo.sh`
    - `scripts/sync-sdk-to-admin.sh`
    - `scripts/regenerate-generated-assets.sh`
    - `Makefile`
    - `.github/workflows/ci.yml`
    - `docs/generated-assets.md`
    - `docs/demo-and-mock-boundaries.md`
    - `apps/demo-sdk/README.md`
  - 验证命令：
    - `bash -n scripts/sync-sdk-to-demo.sh scripts/sync-sdk-to-admin.sh`
    - `bash ./scripts/sync-sdk-to-demo.sh`
    - `./scripts/check-generated-drift.sh "Demo SDK generated assets" "sh scripts/sync-sdk-to-demo.sh" apps/demo-sdk`

---

## 本轮全库审查结论

- 当前最优先恢复项已经不是单点功能缺失，而是 contract、工程门禁和装配边界三类基础问题
- 如果只继续补功能而不先收口 `P0-6`、`P0-7`、`P0-8`，后续新增能力会继续建立在漂移的 SDK、失效的脚本和不稳定的门禁之上
- 下一轮执行建议优先级：
  - `P0-6 SDK 与后端协议漂移收口`
  - `P0-7 SDK 工程门禁失效修复`
  - `P0-8 网站与部署脚本路径失配`
