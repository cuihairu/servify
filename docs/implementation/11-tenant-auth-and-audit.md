# 11 Tenant Auth And Audit

范围：

- 多租户与 workspace 边界
- 认证授权收口
- 审计日志
- 配置层级治理
- 面向真实部署的安全基线

## T1 tenant-and-workspace-boundaries

- [-] 梳理 tenant、workspace、agent、customer、knowledge base 的归属关系
- [-] 定义跨租户访问禁止规则
- [-] 定义查询、写入、导出、后台任务的租户隔离语义
- [-] 为核心模型补租户字段与索引策略审查

验收：

- 任何核心数据对象都能明确回答“属于哪个 tenant/workspace”

当前进展：

- 已盘点当前核心对象的 tenant/workspace 归属现状，见 `docs/tenant-workspace-boundaries.md`
- JWT claims 现已支持归一化并透传 `tenant_id` / `workspace_id`
- 认证中间件会将 `tenant_id` / `workspace_id` 投影到请求上下文，供审计、provider 调用和后续数据过滤使用
- 管理面 `/api/*` 与受保护的 `/api/v1/*` 已增加统一 request-scope 守卫，拒绝 token scope 与 header/query scope 冲突的请求，并限制非 admin/service principal 通过请求扩大作用域
- `KnowledgeDoc`、`CustomField`、`SLAConfig`、`AppIntegration` 已补显式 scope 字段，并在 service/repository 层默认按上下文过滤
- `SLAViolation` 已补显式 `tenant_id/workspace_id` 字段，并在 `sla` service 的违约创建、列表、解决、统计与监控扫描路径按上下文过滤
- `ShiftSchedule` 已补显式 `tenant_id/workspace_id` 字段，并在 `shift` service 的创建、列表、更新、删除与统计路径按上下文过滤
- `Macro` 已补显式 `tenant_id/workspace_id` 字段，并在 `macro` service 的列表、创建、更新、删除与应用入口按上下文过滤
- `CustomerSatisfaction` / `SatisfactionSurvey` 已补显式 `tenant_id/workspace_id` 字段，并在 `satisfaction` service 的调查发送、响应、列表、统计、更新与删除路径按上下文过滤
- `SuggestionService` 已补按上下文过滤的相似工单/知识库候选查询，不再跨 workspace 读取已 scope 化的 `Ticket` / `KnowledgeDoc`
- `GamificationService` 的排行榜聚合已改为按上下文过滤 `Agent` / `Ticket` / `CustomerSatisfaction`，避免跨 workspace 汇总客服绩效
- `SatisfactionService.GetSatisfactionStats` 现已补 scoped 回归测试，进一步锁定满意度分类统计与趋势聚合在 workspace 边界内运行
- `Session`、`Message`、`TransferRecord`、`WaitingRecord` 已补显式 `tenant_id/workspace_id` 字段与基础索引，并在 `conversation/routing` 仓储默认按上下文过滤
- 旧 `MessageRouter` 兼容落库路径已开始从 `UnifiedMessage.Metadata` 提取 `tenant_id/workspace_id` 写入 `Session/Message`，并拒绝同一 `sessionID` 跨 workspace 复用
- `Ticket` 已补显式 `tenant_id/workspace_id` 字段与基础索引，并在 `ticket` 仓储主查询、统计与创建路径按上下文过滤
- `Customer` 已补显式 `tenant_id/workspace_id` 字段，并在 `customer` 仓储读取、列表、统计与活动查询通过扩展表 scope 收口
- `customer` 模块的 activity 聚合链路现已补 integration test，确认 `recent_sessions` / `recent_tickets` / `recent_messages` 会继续按请求 scope 过滤，不会把其它 workspace 的客户历史混入当前时间线
- 兼容层 `CustomerService.GetCustomerStats` 现已补 scoped 回归测试，并修正 legacy 客户统计 join 中未限定表前缀的 `created_at` 条件，避免 scoped 统计在多表聚合时出现歧义列错误
- `Agent` 已补显式 `tenant_id/workspace_id` 字段，并在 `agent` 仓储创建、读取、列表与统计路径按上下文过滤
- 兼容层 `AgentService.GetAgentStats` 现已补 scoped 回归测试，确认 legacy agent 统计入口不会跨 workspace 混入其它 agent 主数据
- 管理面 `security/users/*` 与 `security/tokens/*` 现已在资源权限之外，继续按目标 user 关联的 `Agent` / `Customer` scope 校验 tenant/workspace 归属；跨 scope 目标统一按 not found 处理，避免枚举或误操作其它租户用户/令牌
- `WorkspaceService`、`analytics` 模块聚合仓储与 agent transfer runtime load 同步路径已开始按上下文过滤 `Session/Agent/Ticket/Message/Customer` 等已 scope 化主数据
- `analytics.GetAgentPerformanceStats` 与兼容层 `StatisticsService.GetAgentPerformanceStats` 现已补 scoped 回归测试，并修正 analytics 仓储中的平均解决时长聚合以兼容 sqlite / postgres 等不同 SQL 方言，避免 legacy agent performance 报表长期处于未测状态
- `analytics.GetTicketCategoryStats` / `GetTicketPriorityStats` / `GetCustomerSourceStats` 及兼容层 `StatisticsService` 入口现已补 scoped 回归测试，进一步收口 legacy 分类、优先级与客户来源聚合的跨 workspace 泄漏风险
- `SLAService.GetSLAStats` 的趋势聚合现已补 scoped 回归测试，确认最近 7 天合规率视图不会混入其它 workspace 的工单或违约数据
- 当前剩余缺口已从“核心主数据未建模”进一步收敛到“后续若要做租户级日报时，需为 `DailyStats` 这类 system 级全局汇总表单独设计 tenant/workspace 维度版本”，详见 `docs/tenant-workspace-boundaries.md`

## T2 auth-and-rbac-convergence

- [x] 盘点当前 JWT、claims、permissions、middleware 的实际入口
- [x] 收口 RBAC 模型与权限解析链路
- [x] 区分 end-user、agent、admin、service token 的认证语义
- [x] 为管理端、公开接口、内部接口定义不同授权策略
- [x] 为关键权限补负向测试与越权测试

验收：

- 权限判断路径单一且可测试，不依赖散落的 handler 逻辑

当前进展：

- JWT 校验、claims 归一化、role -> permission 展开、principal kind 判定已经统一收口到 `internal/platform/auth`
- `internal/middleware` 仅保留兼容入口，不再承载独立权限逻辑
- 管理面 `/api/*` 已显式拒绝 `end_user` token，当前仅允许 `agent`、`admin`、`service`
- `public` 路由保持匿名访问；`/api/v1` 已拆分为 public realtime、management realtime/AI、service-only metrics ingest 三类表面
- 表面分类与新增路由准入规则已文档化，见 `docs/auth-surface-policy.md`

## T3 audit-log-foundation

- [x] 定义审计事件模型
- [x] 覆盖关键写操作，例如工单变更、路由分配、配置变更、权限变更
- [x] 记录 actor、tenant、resource、before/after、request metadata
- [x] 设计查询接口与保留策略
- [x] 为敏感操作提供最小可追溯能力

验收：

- 关键变更可追溯，满足问题排查和合规基础需求

当前进展：

- 已新增 `AuditLog` 持久化模型，并纳入应用迁移集合
- 管理面 `/api/*` 写请求已统一接入审计中间件，成功写操作会记录 actor、principal、action、resource、request metadata 与请求载荷
- `before/after` 已有中间件扩展点，`scoped config`、`ticket` 的更新/分配/关闭、`agent` 的上线/下线/状态切换、`customer` 的更新/备注/标签/token revoke，以及 `security` user revoke token 等高价值写操作已开始写入精细状态快照
- 已新增 `GET /api/audit/logs` 查询接口，支持按 action/resource/principal/actor/success/time-range 过滤
- 管理面现已补 `GET /api/audit/logs/:id` 单条查询接口，并沿用 request scope 约束，支持按租户/工作区读取具体审计记录
- 管理面现已补 `GET /api/audit/logs/:id/diff` 通用差异预览接口，可直接查看 `before/after` 的字段级变化
- 管理面现已补 `GET /api/audit/logs/export` CSV 导出接口，复用列表过滤参数并支持带作用域约束的轻量导出
- 审计查询与保留基线已文档化，见 `docs/audit-log-policy.md`
- 已接入后台审计清理 worker，默认按 180 天保留窗口批量删除过期记录
- 冷热分层归档已落地（2026-09-28）：`security.audit.archive_dir` 配置归档目录后，清理 worker 删除过期记录前先整批落 gzip JSON 快照冷层，行内保留 `prev_hash`/`entry_hash`（冷层同链可对账），归档失败即中断清理不删除；默认空关闭保持既有行为，策略见 `docs/audit-log-policy.md`

## T4 configuration-scopes

- [-] 区分系统级、租户级、工作区级、运行时级配置
- [-] 为 AI provider、knowledge provider、routing policy 等配置定义作用域
- [-] 明确配置加载、覆盖、回退规则
- [-] 为配置变更补审计与回滚约束

验收：

- 配置不再混杂在环境变量、数据库和代码默认值之间

当前进展：

- 已新增 `docs/configuration-scopes.md`，明确 system / tenant / workspace / runtime 四层配置作用域
- 已定义 AI provider、knowledge provider、routing policy、portal、security baseline 的推荐作用域矩阵
- 已定义统一覆盖顺序：`runtime -> workspace -> tenant -> system -> code default`
- 已定义配置变更的审计与回滚约束，区分系统级发布回滚与租户/工作区配置恢复
- 已新增 `internal/platform/configscope` 作为统一 resolver 骨架，当前已覆盖 portal config、OpenAI config 与 knowledge provider config 的分层解析
- 门户公开配置读取、AI runtime 装配与增强健康检查中的 knowledge provider 展示信息已切到 resolver，不再各自拼接 system default / runtime override
- 已为 `portal` / `OpenAI` / knowledge provider resolver 补齐 tenant/workspace provider 接口与覆盖顺序骨架，当前按 Dify 优先、WeKnora compatibility 保留
- 已新增 `TenantConfig` / `WorkspaceConfig` 持久化模型与 GORM provider，当前已接入 `portal` 的 tenant/workspace 覆盖读取
- `OpenAI` / knowledge provider resolver 已可消费同一批持久化 provider；管理面 `AI` handler 主路径与 `RuntimeService` 主路径都已按 request scope 解析 scoped provider
- `security.session_risk` 已纳入同一套 scoped config 文档与 resolver；auth 自助 session 列表、管理面 user security session 列表都会按请求 scope 动态解析 tenant/workspace 覆盖阈值
- 对于 `context.Background()` 或匿名 websocket 等未携带 scope 的调用，仍会自然回退到 system config
- 管理面已新增 tenant/workspace scoped config 的最小 `GET/PUT` 接口，可读写 `portal` / `OpenAI` / knowledge provider / `session_risk` override
- scoped config 现已新增 `history` / `rollback` 管理接口，配置写入与恢复都会写入 `AuditLog`，并保留 before/after 快照；rollback 需显式提交确认参数才会执行
- tenant/workspace scoped config 的 `PUT` / `rollback` / `verify` 现已强制要求提交 `change_ref` 与 `reason`，支持 JSON body 或 `X-Change-Ref` / `X-Change-Reason` header，并会一并进入审计 request metadata
- 高风险 scoped config 变更现已从“审批单号存在”推进到“真实审批记录存在”：当字段风险评估命中高风险 provider endpoint、KB mapping、session risk threshold 或 rollback 操作时，`PUT` / `rollback` 不仅必须提交 `approval_ref`，还必须能按 `approval_ref + change_ref` 查到已记录的 `scoped_config.{scope}.approve` 审计事件，且审批人与执行人必须分离；接口继续支持 JSON body 或 `X-Approval-Ref` header
- 管理面现已支持 `POST /security/config/{scope}/verify/:audit_id`，用于给 update / rollback 结果写入执行后验证结论；verification body 除 `status` / `notes` / `evidence` 外，还需提交与 `verification_template.checks` 对齐的 `checks`
- scoped config verification 现已从自由文本收口为模板化检查项：`passed` 必须补 evidence，且模板中所有必填检查项都必须显式标记为 `passed`；`failed` 必须补 notes，且至少要有一个模板检查项标记为 `failed`
- `verification_template` 现已进一步按真实 `changed_paths` 细分检查项，并在模板根节点返回 `changed_paths`；单个 check 还会返回 `risk_level` 与自身覆盖的 `changed_paths`，可直接驱动前端或自动化脚本按字段风险执行验证
- 当前细分模板已覆盖 portal public surface / locale、OpenAI endpoint / model contract、knowledge provider endpoint / knowledge mapping / health check、session risk score / window / concurrency 这几类高风险配置簇
- 变更治理现已补统一状态机：响应会额外返回 `governance_status` / `governance_policy`，当前状态值至少覆盖 `awaiting_approval`、`awaiting_verification`、`verified`、`verification_failed`、`approved`、`not_required`
- scoped config verification 现已具备最小双人复核约束：verification reviewer 必须是带 `user_id` 的已认证操作者，且不能与原始 update / rollback 执行人相同
- 管理面现已支持在 scoped config 写入响应、history 列表、单条详情和 verify 响应中直接返回 `change_control` / `change_risk` / `approval_policy` 元数据；其中 `change_control` 会包含 `approval_ref`，`change_risk` 会给出 `risk_level` / `risk_reasons` / `changed_paths`，`approval_policy` 会声明是否必须补审批、审批记录是否已落库、最新审批审计 ID / 时间 / 审批人等真实执行前审批信息
- verify 响应还会返回 `source_governance_status` / `source_governance_policy`，用于直接判断被验证的那条 source change 当前是否已经闭环
- 管理面现已支持在 scoped config history 列表、单条详情和 verify 响应中直接返回 `operation` / preview / rollback / verify / `verification_status` / `latest_verification` / `verification_template` / `verification_policy` 元数据；其中 `verification_policy` 会额外声明 `checks_required`、`required_check_ids`、`template_check_count` 与失败态检查约束，便于前端或自动化脚本按模板渲染执行后验证流程
- `GET /security/config/{scope}/history` 现已可直接作为治理工作台数据源，支持 `governance_status` / `risk_level` / `approval_status` / `verification_status` / `needs_action` 筛选，并返回 `governance_summary` 聚合计数与 `applied_filters`，用于渲染待处理变更队列
- 当前仍缺更通用的跨配置域写接口，以及更完整的双人复核编排、自动化验收模板与发布后验证规则编排

## T5 security-baseline-for-operations

- [x] 盘点高风险接口和高风险操作
- [x] 增加关键操作的速率限制、权限兜底和日志
- [x] 为 token 生命周期、密钥轮换、敏感字段脱敏补最小规范
- [x] 为对外开放接口补基础安全清单

验收：

- 项目具备进入真实部署前的最小安全治理骨架

当前进展：

- 已新增 `docs/security-baseline-operations.md`，盘点 management / service / public 三类高风险表面与关键接口
- 已确认基础路由统一挂载 `RateLimitMiddlewareFromConfig`，具备全局、路径前缀与 header key 级限流能力
- 已确认管理面写接口统一叠加 `AuthMiddleware`、`EnforceRequestScope`、`RequirePrincipalKinds`、`RequireResourcePermission` 与 `AuditMiddleware`
- 审计中间件现已对 `password`、`secret`、`api_key`、`token` 等敏感字段做统一脱敏，避免明文 secrets 落入审计日志
- 已新增 `docs/token-lifecycle-and-key-rotation.md`，定义 JWT / provider key / service token 的最小生命周期与轮换流程
- 已新增 `config.production.secure.example.yml`，提供启用限流、收紧 CORS、使用环境变量注入 secrets 的生产示例模板
- 已新增 `docs/public-surface-security-checklist.md`，收口匿名 / 对外开放接口的最小评审项与逐类入口检查清单
- 已新增 bootstrap 启动安全告警，对默认 `jwt.secret`、开放式 CORS、关闭限流、空 provider key 等高风险配置输出 warning
- 启动安全告警与 `check-security-baseline --strict` 现已进一步覆盖公开 / 高风险入口限流基线，若 `/public/`、`/public/kb/`、`/public/csat/`、`/api/v1/auth/`、`/api/v1/ws`、`/uploads/`、`/api/v1/metrics/ingest` 或 `/api/` 未配置独立路径级 rate limit，会在启动与部署前校验阶段明确告警
- 默认开发配置 `config.yml` 现已保持通过 `security` / `observability` strict baseline 检查，但文档已明确其用途仍是本地开发，不应替代生产部署配置模板
- `internal/platform/auth` 现已新增可组合 token policy 钩子，支持按 `iat` 做 issued-before 失效、按 `token_version` 做最小会话版本淘汰，为后续账号级吊销策略预留接入点
- `internal/platform/auth` 现已新增基于 `users` 表状态与 `user_auth_sessions` 的 token policy，并接入 router auth middleware：非 active 用户会被拒绝，设置 `token_valid_after` / `token_version` 后可使旧 token 失效；登录与 refresh 现在也会携带 `session_id + session_token_version`，支持同一 auth session 的旧 token 在 refresh 或 targeted revoke 后失效
- `/api/v1/auth/login`、`/api/v1/auth/register`、`/api/v1/auth/refresh` 现已返回独立 `refresh_token`，refresh 路径不再依赖仍然有效的 access token；refresh token 会携带 `token_use=refresh` 与当前 `session_token_version`，刷新后旧 refresh token 会立即失效，并继续受 user/session revoke、`token_valid_after`、`token_version` 约束
- 认证签发链路现已为 access / refresh token 注入唯一 `jti`；`internal/platform/auth` 已新增 revoke list policy，管理面可通过 `POST /api/security/tokens/revoke` 将单个 JWT 显式加入 denylist，使其在到期前立即失效
- 管理面现已补 `GET /api/security/tokens/revoked` revoke list 查询接口，可按 `jti` / `user_id` / `session_id` / `token_use` 过滤，并支持只看当前仍未过期的 denylist 记录
- revoked token denylist 已接入后台 cleanup worker，会按 token `expires_at` 自动清理过期 revoke 记录，避免 denylist 长期膨胀
- agent 管理面现已新增 `POST /api/agents/:id/revoke-tokens`，可主动提升用户 `token_version` 并刷新 `token_valid_after`，将旧 token 作废
- customer 管理面现已新增 `POST /api/customers/:id/revoke-tokens`，可对受作用域约束的客户账号主动触发旧 token 失效
- token 主动失效底层逻辑现已收敛到 `internal/platform/usersecurity`，agent/customer 两条管理面路径复用同一套 `token_valid_after + token_version` 更新实现
- 管理面现已新增统一 `security` surface：`GET /api/security/users/:id` 可查询单用户安全状态，`POST /api/security/users/query` 可批量预览用户当前安全态与下一次 revoke 后的 `token_version`，`GET /api/security/users/:id/sessions` 与 `POST /api/security/users/:id/sessions/revoke` 可查看并失效单个 auth session，`POST /api/security/users/:id/revoke-tokens` 与 `POST /api/security/users/revoke-tokens` 可分别执行单用户 / 批量 user revoke，统一由 `security.read` / `security.write` 权限保护
- 上述 `security/users/*` 入口现已补目标用户 scope 校验：若请求上下文携带 `tenant_id/workspace_id`，则只允许操作该 scope 下的 `Agent` / `Customer` 关联用户，跨 scope 目标统一返回 not found
- `security/tokens/revoke` 与 `security/tokens/revoked` 也已沿用同一套 scoped user 约束：scoped 请求只能吊销或查看本 scope 下 `Agent` / `Customer` 关联用户的 token 记录，不能跨租户操作 denylist
- 管理面现已补 `POST /api/security/users/:id/sessions/revoke-all`，可批量失效同一用户的全部活跃 auth session，并支持通过 `except_session_id` 保留一个 session，用于“退出所有设备”或“仅踢其它设备”场景
- 路由治理层现已新增代码级 `security surface catalog`，会用真实 `BuildRouter()` 路由表校验匿名 / 开放表面是否被显式登记；`/api/v1/auth/*` 与 `/uploads/*` 也已纳入同一套公开表面治理与专属限流基线
- `UserAuthSession` 现已补最小 client metadata：登录与 refresh 会记录并更新 `user_agent`、`client_ip`、`last_seen_at`，管理面 `GET /api/security/users/:id/sessions` 可直接看到基础设备视图
- 认证自助表面现已补 `GET /api/v1/auth/sessions`、`POST /api/v1/auth/sessions/logout-current`、`POST /api/v1/auth/sessions/logout-others`，已登录用户可查看自己的 auth sessions，并执行“退出当前会话”或“踢掉其它设备”
- `UserAuthSession` 现已进一步补 `device_fingerprint`：优先使用 `X-Device-ID`，否则基于 `User-Agent + ClientIP` 生成稳定哈希，便于同一设备多次 refresh / 多会话归并查看
- auth / management 两侧 session 列表现已返回衍生安全视图字段 `network_label`、`location_label`、`risk_score`、`risk_level`、`risk_reasons`，以及 `family_public_ip_count` / `family_device_count` / `active_session_count` / `family_hot_refresh_count` / `reference_session_id` / `ip_drift` / `device_drift` / `rapid_ip_change` / `rapid_device_change` / `refresh_recency` / `rapid_refresh_activity` 等 session-family 指标；当前已能区分 loopback/private/public、文档保留网段 / shared address space 等最小 location hint，并将同账号多公网 IP、过多活跃会话、相对最近活跃 session 的设备/IP 漂移、24 小时窗口内的快速切换，以及短时间 refresh 活跃度纳入可解释风险提示
- session risk 的 Geo/IP 富化层现已从 handler 内联规则抽成可注入的 `session IP intelligence` adapter；默认仍使用现有 heuristic provider，auth 自助与 management user-security 两侧都可在不改风控主逻辑的前提下接入真实 Geo/IP provider，且当前已补 HTTP provider 与两侧 handler 级自动化证据
- 已新增可直接落真实数据源的 `HTTP session IP intelligence provider` 与 `security.session_ip_intelligence` 配置项；当配置 `enabled + base_url` 时，auth / management 会话列表会通过外部 endpoint 拉取 `network_label` / `location_label`，失败时自动回退到本地 heuristic 分类；当前 adapter 已补对嵌套 `data` payload、`Authorization` 之外自定义鉴权头的兼容回归
- 上述 session 风险启发式阈值现已集中到统一 `session risk policy`，并接入 `security.session_risk` 配置面；当前已新增 `server.environment + security.session_risk_profiles.{environment}` 环境级基线，并继续支持 tenant / workspace / runtime 逐层细化，auth 自助与 management user-security 两侧都已有自动化覆盖
- 本轮 `G2-*` 收口标准已满足：legacy 聚合边界、HTTP Geo/IP provider adapter、环境级 session risk policy，以及 audit/rollback 运营约束都已有代码、测试与文档证据；后续若要接入某个特定外部 Geo/IP 厂商，只应视为 provider 选择或部署验收增强，而不是本轮阻塞
- 已新增 `servify check-security-baseline --strict` 与 `scripts/check-security-baseline.sh`，可在部署前将现有 security warning 规则升级为显式失败检查，不再只依赖启动日志提醒
- T5 当前已达到“最小安全治理骨架”验收目标；后续增强项应单独立项，不再视为本轮收尾阻塞

session 列表示例响应（auth / management 侧字段集合一致，`is_current` 仅 auth 自助侧返回）：

```json
{
  "session_id": "sess-current",
  "status": "active",
  "token_version": 3,
  "device_fingerprint": "fp-2d9c6c",
  "network_label": "public",
  "location_label": "public_unknown",
  "risk_score": 5,
  "risk_level": "high",
  "risk_reasons": [
    "public_network",
    "multi_public_ip_family",
    "ip_drift"
  ],
  "family_public_ip_count": 2,
  "family_device_count": 2,
  "active_session_count": 2,
  "family_hot_refresh_count": 2,
  "reference_session_id": "sess-latest",
  "ip_drift": true,
  "device_drift": false,
  "rapid_ip_change": true,
  "rapid_device_change": false,
  "refresh_recency": "hot",
  "rapid_refresh_activity": true
}
```

当前明确留待后续单独立项的内容：

- 真实 Geo/IP 情报接入
- 更强时间序列 / 跨会话异常检测
- 按环境、租户或角色分层的 session risk policy
- 更细粒度的公开接口审批/自动审查规则
- 审批回滚等更完整的 user security 操作能力
