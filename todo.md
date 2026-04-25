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
- 最近进展：**2026-04-22** 继续收紧 AI / Knowledge 真实验收口径：1) `test-dify-integration.sh` 的 real 模式除 upload/sync 外，新增要求 `knowledge_provider_enabled=true`、`knowledge_provider_healthy=true`，且 `ai-query` 不能只落入 `fallback`；2) `test-weknora-integration.sh` 的 real 模式新增要求当前 provider 必须是 `weknora`、provider 启用且健康，并强制验证 `disable -> fallback query` 的降级证据；3) 两个脚本现在都会额外输出 `manifest.json`，把 provider、mode、关键检查结果和证据文件列表结构化；4) 新增 `validate-acceptance-manifest.sh`、`check-acceptance-evidence.sh` 以及对应 `make` 入口，可在真实环境跑完后直接校验证据集，也可在仓库存在 tracked manifest 时做批量扫描；5) CI `script-checks` 已接入 acceptance evidence 校验，且只检查已纳入 git 的 manifest，避免把本地临时产物误算成正式证据；6) `docs/WEKNORA_INTEGRATION.md` 与脚本测试已同步更新，避免“接口成功但真实 provider 主路径未命中”仍被误填成通过
- 下一步：使用真实 WeKnora 或 Dify 服务完成 upload/sync 端到端验收，并把成功/失败证据回填到 `docs/acceptance-checklist.md`，将 `知识上传`、`知识同步`、`AI 控制面人工运行验证第二轮/第三轮` 从 `部分通过` 推进到 `通过`
- 阻塞项：真实 WeKnora / Dify 服务环境与凭证；当前 Windows 本机无法用 `go test ./scripts` 直接模拟 bash 脚本对 `httptest` 端口的访问，需要在 Linux/CI 或真实环境执行脚本留档

### [!] P1-2 Auth 自助 session 链路补齐真实验收

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

### [!] P1-3 会话工作台主操作补齐到“通过”

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
- 状态：`[ ]`
- 最近进展：当前已确认 `会话详情`、`消息列表`、`发送消息`、`指派会话`、`转接会话`、`关闭会话` 都有代码与自动化覆盖；`acceptance-checklist` 已按发布口径统一回调为 `部分通过`，因为剩余问题不再是“路由阻塞”，而是发布证据、异常路径和运行留档仍不足
- 下一步：补齐工作台主操作的真实请求留档，并补至少一条失败/拒绝路径证据
- 阻塞项：暂无

### [!] P1-4 运行基线最小事实补齐

- 范围：
  - `GET /ready`
  - `GET <metricsPath>`
  - `make build`
  - `GET /api/v1/messages/platforms`
- 风险：
  - 这些能力未验会直接影响上线口径
- 验收标准：
  - 全部回填到 `docs/acceptance-checklist.md`
- 状态：`[ ]`
- 最近进展：验收矩阵中仍有多个 `未验`
- 下一步：先跑最小可复现命令并回填证据
- 阻塞项：暂无

### [ ] P1-5 Ticket 主闭环剩余高频操作补齐

- 范围：
  - 更新工单
  - 评论
  - 关闭
  - 统计
  - 导出
- 验收标准：
  - 对应 API 从 `未验` 或 `部分通过` 推进到 `通过`
- 状态：`[ ]`
- 最近进展：工单主链路基础已通，但高频运营动作还未全部验收
- 下一步：按验收矩阵逐项补证据
- 阻塞项：暂无

---

## P2 企业级项目差距

这些不是当前最紧急代码缺陷，但属于“距离企业级项目”的重点 backlog。

### [ ] P2-0 客户侧推荐问题与上下文联想问题

- 当前判断：
  - 已有后台辅助推荐接口 `GET/POST /api/assist/suggest`
  - 当前返回能力主要是 `intent`、`similar_tickets`、`knowledge_docs`
  - 该能力挂在 management `assist` 权限路由下，更像后台辅助检索，不是客户咨询入口的正式产品能力
  - 当前未看到 Web 客户侧 / SDK / demo 已接入“首屏推荐问题”或“基于上下文的下一问联想”
- 代码证据：
  - `apps/server/internal/handlers/suggestion_handler.go`
  - `apps/server/internal/services/suggestion_service.go`
  - `apps/server/internal/app/server/router.go`
  - `sdk/packages/core/src/sdk.ts`
- 产品目标：
  - 客户刚进入咨询页时，能看到可点击的推荐问题
  - 客户发起几轮对话后，系统可基于当前上下文动态联想下一问
  - 推荐问题可与知识库、历史工单、热门问题、AI 意图识别联动
- 建议拆分：
  - `RQ-1` 首屏热门问题推荐
  - `RQ-2` 会话内上下文联想问题
  - `RQ-3` 客户侧推荐接口与权限边界
  - `RQ-4` Web SDK / demo / 官网接入
  - `RQ-5` 埋点、点击率、转化率与验收口径
- 验收标准：
  - 客户未输入前可拿到一组推荐问题
  - 客户输入后可拿到一组基于上下文变化的联想问题
  - 推荐项可点击进入提问，不只是展示静态文案
  - 前后端、SDK、验收文档有统一口径
- 状态：`[ ]`
- 最近进展：已确认当前只有后台辅助推荐接口，没有完整客户侧链路
- 下一步：先决定推荐策略走“规则+知识库”还是“AI 生成+规则兜底”
- 阻塞项：产品策略、推荐来源与客户侧接口边界尚未定稿

### [ ] P2-1 多实例与高可用边界明确化

- 范围：
  - agent online/runtime state
  - WebSocket / WebRTC 会话路由
  - event bus 异步链路
  - worker 幂等和重启恢复
- 目标：
  - 明确 Servify 当前是单实例优先，还是支持多实例协同
- 验收标准：
  - 文档、部署说明、运行时行为一致

### [ ] P2-2 配置治理与环境分层强化

- 范围：
  - dev/staging/prod 配置模板
  - 敏感配置来源
  - 启动前校验
  - 配置漂移检查
- 目标：
  - 防止示例配置、开发默认值和生产配置混用
- 验收标准：
  - 配置加载、校验、模板、文档完全对齐

### [ ] P2-3 数据恢复、备份与迁移演练

- 范围：
  - 数据库迁移回滚
  - 审计/工单/会话关键表恢复
  - 上传文件与知识文档资产恢复
- 目标：
  - 从“可迁移”升级到“可恢复”
- 验收标准：
  - 至少一轮备份恢复演练证据

### [ ] P2-4 可观测性从“有指标”升级到“可运维”

- 范围：
  - 关键业务 SLI/SLO
  - 异步失败告警
  - AI/provider 失败分类
  - 远程协助与实时链路诊断
- 目标：
  - 运维能在故障时快速定位，不依赖人工翻日志
- 验收标准：
  - 告警规则、dashboard、runbook 三者一致

### [ ] P2-5 安全治理继续收口到首批企业交付标准

- 范围：
  - session 风险策略
  - refresh token 治理
  - 审批与回滚证据
  - 公开接口治理
- 目标：
  - 从“最小可部署治理”提升到“企业试点可接受”
- 验收标准：
  - 对应安全面能力均有真实验收，不只靠单测

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

- 当前优先恢复任务：`P1-1 AI / Knowledge 验收闭环`
- 原因：本轮整体审核确认 P0 项均已收口，核心后端包与模块包测试通过；同时发现并修复了 voice 管理路由复用 `assist` 权限的治理错配。下一阶段仍应优先把 AI / Knowledge 主链路从“部分通过”推进到可交付的真实验收闭环
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

### [!] P0-6 SDK 与后端协议漂移收口

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

### [!] P0-7 SDK 工程门禁失效修复

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

### [!] P0-8 网站与部署脚本路径失配

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

### [ ] P1-7 Modules 与 legacy services/models 的边界收口

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
- 状态：`[ ]`
- 最近进展：已确认 `internal/modules` 下存在大量直连 `internal/models`，且部分 delivery 仍直连 `internal/services`
- 下一步：先输出依赖地图，找出最值得先切的模块边界
- 阻塞项：共享领域模型与持久化模型的拆分策略尚未定稿

### [ ] P1-8 文本编码、对外文案与仓库可读性修复

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
- 状态：`[ ]`
- 最近进展：已确认管理端类型检查可过，但多个高可见文件存在文本质量问题
- 下一步：先按“README -> todo.md -> admin 核心页面”顺序处理编码与文案
- 阻塞项：需确认历史文件是编码损坏还是终端显示问题

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
