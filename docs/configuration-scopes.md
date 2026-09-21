# Configuration Scopes

本文件定义 Servify 当前配置的作用域边界、覆盖顺序和变更约束，用于推进 `11 / T4 configuration-scopes`。

目标：

- 区分哪些配置只能是系统级
- 区分哪些配置应该下沉到 tenant / workspace
- 避免把 secrets、provider endpoint、业务策略、运行态开关继续混在同一个 `config.yml`
- 为后续配置持久化、审计、回滚提供统一规则

## 当前配置来源

当前仓库存在 4 类配置来源：

1. 代码默认值
   - 入口：`apps/server/internal/config/config.go`
   - 用途：保证本地开发和缺省启动可运行
   - 限制：只能承载安全的默认值，不能承载生产 secrets

2. 系统启动配置文件
   - 入口：`config.yml`、`config.weknora.yml`
   - 装载：`apps/server/internal/app/bootstrap/config.go`
   - 用途：部署级基础设施和系统默认策略；其中 `config.weknora.yml` 主要用于 WeKnora compatibility 回归；知识库选择链为 ragflow → dify → weknora → pgvector 直配（RAGFlow 与 WeKnora 为正统知识库双路径，Dify 为编排平台 dataset 兼容保留；选型全景见 KNOWLEDGE_BASE_LANDSCAPE，RAGFlow 集成见 RAGFLOW_INTEGRATION）

3. 环境变量覆盖
   - 入口：Viper `AutomaticEnv()`
   - 用途：容器部署、CI、secret 注入
   - 限制：应只覆盖系统级配置，不应承载 tenant / workspace 业务策略

4. 数据库存储的 scoped 配置对象
   - 当前已存在：
     - `SLAConfig`
     - `CustomField`
     - `AppIntegration`
   - 特征：已带 `tenant_id` / `workspace_id`，可按上下文过滤

另外还存在一类非持久化的运行态作用域输入：

- JWT / request context 中的 `tenant_id`、`workspace_id`
- AI / knowledge 请求中的临时参数，例如 `knowledge_id`
- 路由、转接、会话中的临时执行态参数

这些属于 runtime scope，而不是长期配置。

## 作用域模型

### 1. System Scope

定义：

- 整个部署实例共享
- 只能由运维、部署流程或系统管理员修改
- 不随 tenant / workspace 切换

典型内容：

- `server`
- `database`
- `redis`
- `jwt.secret`
- `log`
- `monitoring`
- tracing / exporter endpoint
- 全局 CORS baseline
- 全局 rate limiting baseline
- 上传根目录、文件系统路径
- AI / knowledge provider 的基础 endpoint、client credentials、全局超时

存放原则：

- 首选配置文件 + 环境变量
- secrets 只通过环境变量或外部 secret manager 注入
- 不应存入租户配置表

### 2. Tenant Scope

定义：

- 同一部署中的某个业务租户共享
- 对租户下所有 workspace 生效
- 允许被 workspace 做更细粒度覆盖

典型内容：

- tenant 默认 AI provider 选择
- tenant 默认 knowledge provider / knowledge base namespace
- tenant 默认 routing / SLA / automation 策略
- tenant 默认 portal branding
- tenant 级集成开关和 capability 策略
- tenant 级安全策略补充，例如 API surface allowlist、公开知识库开关

存放原则：

- 应进入数据库配置对象或专门的 tenant config 模型
- 不应继续写死在 `config.yml`
- 若涉及 credentials，优先存“secret reference”而不是明文

### 3. Workspace Scope

定义：

- tenant 下某个具体工作空间的业务配置
- 优先服务于客服团队、业务线、渠道面差异

典型内容：

- `CustomField`
- `SLAConfig`
- `AppIntegration`
- workspace 级 routing policy
- workspace 级 AI prompt/profile
- workspace 级 knowledge source 选择
- workspace 级 portal 外观、默认语言、渠道开关

存放原则：

- 应持久化到数据库
- 必须带 `tenant_id` + `workspace_id`
- 必须走审计

### 4. Runtime Scope

定义：

- 单次请求、单次任务、单次会话执行时临时生效
- 不应被当作长期配置持久化

典型内容：

- request context 中的 `tenant_id` / `workspace_id`
- 本次 AI 请求是否启用 retrieval / fallback
- 本次转接的优先级、目标技能、notes
- 本次导出的时间范围、过滤器、排序

存放原则：

- 仅存在于 request / job / event / session 上下文
- 默认不落库
- 若影响高风险行为，应进入审计日志

## 配置矩阵

### 基础设施与平台

- `server`：`system`
- `database`：`system`
- `redis`：`system`
- `webrtc.stun_server`：`system`
- `jwt.secret` / token 生命周期基线：`system`
- `log` / `monitoring` / tracing exporter：`system`
- `upload.storage_path`：`system`

### 安全与接入

- `security.cors`：`system`
- `security.rbac.roles`：`system`
- `security.rate_limiting` 默认值：`system`
- `security.session_risk` 基线：`system`，允许 `tenant` / `workspace` 在已授权 scope 内细化风险阈值
- `security.session_risk_profiles.{environment}`：`system`，用于按 `server.environment` 选择部署环境级风险基线，再叠加 tenant / workspace override
- `security.session_ip_intelligence` provider endpoint / API key / timeout：`system`
- 某 tenant / workspace 是否允许公开知识库、Portal、特定集成：`tenant` 或 `workspace`

### AI Provider

- provider endpoint、API key、client secret、全局 timeout：`system`
- 默认 provider registry 与 fallback baseline：`system`
- tenant 默认 provider 选择、默认模型、默认 prompt profile：`tenant`
- workspace 针对某业务线的模型选择、temperature、tool policy：`workspace`
- 单次请求是否强制禁用 retrieval / fallback：`runtime`

结论：

- provider credentials 不应成为 workspace 配置
- workspace 只能覆盖“策略选择”，不能直接持有系统级基础设施参数

### Knowledge Provider

- provider endpoint、全局 API key：`system`
- tenant 默认 `knowledge_base_id` / namespace 映射：`tenant`
- workspace 默认知识库、检索 threshold / topK / strategy：`workspace`
- 单次查询临时 `knowledge_id`、临时检索策略：`runtime`

### Routing Policy

- 全局兜底策略和实现开关：`system`
- tenant 默认排队/分配/负载策略：`tenant`
- workspace 渠道差异、技能优先级、转人工阈值：`workspace`
- 单次会话的转接 reason / target skills / priority：`runtime`

### Portal / UX / Business Policy

- 系统默认 `portal` 外观和 locale：`system`
- tenant branding：`tenant`
- workspace portal 展示、公开帮助中心、渠道入口：`workspace`

## 覆盖顺序

统一覆盖顺序定义为：

1. `runtime`
2. `workspace`
3. `tenant`
4. `system`
5. `code default`

解释：

- `code default` 只负责兜底
- `system` 负责部署级默认值
- `tenant` 负责租户级业务默认值
- `workspace` 负责团队/渠道级差异
- `runtime` 只做本次执行的临时覆盖

约束：

- 低层级不能覆盖高层级 secrets
- `runtime` 不能突破权限边界，只能在已授权 scope 内窄化或临时选择
- 没有显式 workspace 配置时，应回退到 tenant
- 没有 tenant 配置时，应回退到 system

## 当前建议落点

### 继续保留在系统配置文件中的内容

- `server`
- `database`
- `redis`
- `jwt`
- `log`
- `monitoring`
- provider endpoint / credentials
- upload root path

### 应逐步从系统配置移出、转入 scoped config 的内容

- `portal.brand_name`、`logo_url`、颜色、locale
- AI provider 的默认模型选择与 prompt profile
- knowledge base 默认映射
- routing policy
- workspace 级 integrations enablement

当前代码骨架：

- `internal/platform/configscope.Resolver` 已支持 `portal`、`OpenAI`、`Dify / RAGFlow / WeKnora knowledge provider` 的 `tenant -> workspace -> runtime` provider 覆盖顺序
- `internal/platform/configscope.Resolver` 现已支持 `session_risk` 的 `system -> tenant -> workspace -> runtime` 风险阈值解析链路
- 已新增数据库持久化的 `TenantConfig` / `WorkspaceConfig` GORM provider；当前 public portal 已接入这套读取链路
- 管理面已具备 tenant/workspace scoped config 的最小读写入口，当前覆盖 `portal` / `OpenAI` / knowledge provider / `session_risk`
- scoped config 写接口现已记录 before/after 快照；tenant/workspace 级 `PUT` / `rollback` / `verify` 还会强制要求 `change_ref` 与 `reason`，并把这些变更控制字段写入审计 request metadata
- 对命中高风险配置簇的 `PUT` / `rollback`，当前不仅要求提供 `approval_ref`，还要求 `approval_ref + change_ref` 能匹配到已记录的 `scoped_config.{scope}.approve` 审计事件，且审批人与实际执行人必须分离；接口仍支持 JSON body 或 `X-Approval-Ref` header，当前高风险主要覆盖 provider endpoint、knowledge mapping、session risk 核心阈值以及 rollback 操作本身
- scoped config 同时提供 tenant/workspace 级变更历史列表元数据、单条历史详情的字段路径级差异预览与带 `added/removed/updated` 类型的 current/snapshot 值对，以及要求显式确认的按审计记录回滚入口
- 管理面现已补执行后验证入口，可为单条 update / rollback 审计记录写入 `passed` / `failed` verification 结果；verification 提交除 `status` / `notes` / `evidence` 外，还需携带与 `verification_template.checks` 对齐的 `checks`
- 当前 verification 已按模板化检查项收口：`passed` 必须带 evidence，且模板内所有必填检查项都要显式 `passed`；`failed` 必须带 notes，且至少要有一个模板检查项标记为 `failed`
- `verification_template` 现已同时返回根级 `changed_paths`，并把单个检查项按字段风险细分到 portal public surface / locale、OpenAI endpoint / model、knowledge provider endpoint / KB mapping / health check、session risk score / window / concurrency；每个 check 还会带 `risk_level` 和自身命中的 `changed_paths`
- 写入响应、history 列表、详情与 verify 响应还会同步返回 `change_control`、`change_risk` 和 `approval_policy`；其中 `change_control` 会带 `approval_ref`，`change_risk` 会给出 `risk_level` / `risk_reasons` / `changed_paths`，`approval_policy` 现在会进一步区分仅提供单号、已落库审批事件和最新审批人等真实审批状态
- 当前还会同步返回统一的 `governance_status` / `governance_policy`，把审批前置和执行后验证组合成单一状态机；verify 响应会额外返回 `source_governance_status` / `source_governance_policy`
- 当前 verification 还会执行最小双人复核约束：reviewer 必须带 `user_id`，且不能与原始变更执行人相同；history 列表、详情与 verify 响应会同步给出 `verification_status`、最新 verification 记录、verification history、`verification_template` 和带 `checks_required` / `required_check_ids` / `template_check_count` 的 `verification_policy`
- `history` 列表还支持直接按 `governance_status` / `risk_level` / `approval_status` / `verification_status` / `needs_action` 查询治理队列，并额外返回 `governance_summary` 与 `applied_filters`，便于管理后台直接渲染待审批/待验证工作台

### 当前数据库配置对象的建议定位

- `SLAConfig`：`workspace`
- `CustomField`：`workspace`
- `AppIntegration`：`workspace`
- 后续应新增：
  - `TenantConfig`
  - `WorkspaceConfig`
  - 或更细的 `AIConfigOverride` / `RoutingPolicyConfig`

## 审计与回滚约束

### 系统级配置

- 修改入口应视为运维变更
- 必须通过部署记录、Git 变更或配置中心版本记录留痕
- 禁止在管理面 API 中直接明文修改系统 secrets

### Tenant / Workspace 配置

- 必须走管理面写接口
- 必须记录审计日志
- 必须支持读取最近一次生效版本
- 修改前后应保留 before / after 快照

### Runtime 配置

- 默认不持久化
- 若属于高风险动作，例如强制切 provider、绕过 fallback、切换知识源，应至少记录审计或结构化日志

### 回滚

- system 级配置通过配置版本 / 发布回滚处理
- tenant / workspace 配置通过数据库版本快照或“上一版本恢复”处理
- runtime 不存在通用回滚，只允许重新发起执行

## 当前缺口

- 当前代码仍只有 system 级 `Config` 结构，没有统一的 `TenantConfig` / `WorkspaceConfig`
- Viper + `config.yml` 仍承载了部分未来应下沉到 tenant/workspace 的业务默认值
- `portal` 仍是 system 级配置，尚未 tenant/workspace 化
- AI / knowledge / routing 的 tenant/workspace override 目前规则已定义，但尚未统一实现
- `DailyStats` 仍是系统级全局聚合，不带 tenant/workspace 维度

## 模板治理与环境严格度（P2-2）

**模板漂移门禁**：`apps/server/internal/config/config_template_lint_test.go`
把仓库四个模板（`config.yml`、`config.staging.example.yml`、
`config.production.secure.example.yml`、`config.weknora.yml`）与
`Config` 结构的 yaml schema 对齐：

1. **未知键直接失败**——viper 对 schema 外的键静默忽略，typo 会无声
   回退默认值；本门禁使其在 CI 阶段显式失败。新增配置项时改结构、
   改模板，测试自动覆盖。
2. **staging / production 模板的 `database` 节与 `jwt.secret` 必须显式
   存在且走 `${ENV}` 占位符**——模板缺失会回退代码默认 dev 值并被
   启动校验拒绝，模板失去可用性；明文凭证不允许进任何模板。
3. 模板有意省略的非敏感键（走 `GetDefaultConfig` 默认值）是允许的。

**环境严格度**：`server.environment` 支持 `dev/development`、
`stage/staging`、`prod/production` 及大小写别名，加载时归一
（`canonicalEnvironment`），防止拼写绕过严格校验。

- `development`：已知不安全默认值（dev JWT secret、dev 数据库密码、
  WeKnora default key）仅产生警告，本地可运行。
- `staging` 与 `production`：任何不安全默认值直接拒绝启动——占位
  凭证（`${ENV}` 未注入时展开为空）同样会被拒绝，这是预期行为：
  预生产与生产不允许带占位凭证起服务。

**凭证注入路径**：模板中的 secrets 一律 `${ENV_VAR}` 占位，加载链为
`expandEnvVarsInConfig`（读取前展开）→ viper 解析 → `normalizeConfig`
（含 environment 归一）→ `Validate` 严格度分级 → `applyConfigEnvOverrides`
（OPENAI_API_KEY 等显式环境覆盖）。环境变量覆盖只用于系统级配置，
不承载 tenant/workspace 策略（见"作用域模型"）。

## 后续实现顺序

1. 新增 `TenantConfig` / `WorkspaceConfig` 文档化数据模型
2. 将 `portal`、AI provider default、knowledge default、routing policy 从 `config.yml` 逐步迁出
3. 为配置读取建立统一 resolver：`runtime -> workspace -> tenant -> system -> default`
4. 为配置变更补审计快照与恢复入口
