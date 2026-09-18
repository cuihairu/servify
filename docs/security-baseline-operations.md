# Security Baseline For Operations

本文件用于推进 `11 / T5 security-baseline-for-operations`，先把当前高风险接口、已有控制和最小上线基线收口成统一清单。

## 高风险接口盘点

### Management Surface 写接口

范围：

- `/api/*` 下的工单、客户、坐席、转接、集成、自动化、知识库等写操作
- `/api/v1/ai/knowledge/upload`
- `/api/v1/ai/knowledge/sync`
- `/api/v1/ai/knowledge-provider/enable`
- `/api/v1/ai/knowledge-provider/disable`
- `/api/v1/ai/circuit-breaker/reset`

风险：

- 会改变业务主数据、路由行为或 AI/knowledge 运行状态
- 错误授权会直接造成跨租户写入、配置污染或服务降级

当前控制：

- `AuthMiddleware`
- `EnforceRequestScope`
- `RequirePrincipalKinds("agent", "admin", "service")`
- `RequireResourcePermission(...)`
- 管理面写请求统一经过 `AuditMiddleware`

### Service Surface

范围：

- `/api/v1/metrics/ingest`

风险：

- 属于机器入口，若 token 泄漏可能被用于伪造监控数据或造成放大写入

当前控制：

- `AuthMiddleware`
- `EnforceRequestScope`
- `RequirePrincipalKinds("service")`
- 全局 HTTP rate limiting middleware 可按路径单独限流

### Public Surface

范围：

- `/public/portal/config`
- `/public/kb/*`
- `/public/csat/*`
- `/api/v1/auth/*` 中的匿名入口（`login` / `register` / `refresh`）
- `/api/v1/ws`
- `/uploads/*`

风险：

- 匿名暴露，容易成为枚举、抓取、滥用或流量放大入口

当前控制：

- 路由表面已与 management / service 分离，避免复用后台 JWT 语义
- 全局 HTTP rate limiting middleware 已挂到基础路由
- 运行时现已维护代码级 `security surface catalog`，用于校验公开/匿名表面是否被显式登记，避免新增开放路由只留在文档评审中

## 已落地的安全基线

### 认证与授权

- 管理面与 service 面统一走 `internal/platform/auth`
- `end_user` token 被显式拒绝进入 management surface
- request scope 会校验 token scope 与 header/query scope 冲突，阻止请求扩大作用域
- `security/users/*` 与 `security/tokens/*` 在资源权限之外，现已继续按目标 user 关联的 `Agent` / `Customer` scope 校验 tenant/workspace 归属；跨 scope 目标统一按不存在处理

### 审计

- 管理面写请求默认记录 actor、principal、tenant、workspace、resource、request metadata
- 审计查询接口：`GET /api/audit/logs`
- 审计中间件现已对 `password`、`secret`、`api_key`、`token` 等敏感字段做统一脱敏，避免把明文 secrets 写入审计库
- auth 公开面（`/api/v1/auth/*`：登录、注册、refresh、2FA 挑战、登出）统一挂载含失败留痕的审计中间件：成功与 4xx/5xx 一并落库（`success=false` + 真实 status_code），失败登录、被风险策略拦截的登录都可对账；凭据字段沿用同一脱敏规则

### 登录风险执行（P2-5 第二刀）

风险判定依赖 `security.session_ip_intelligence` 情报源返回的网络标签：内建启发式只产 `public` / `private` / `loopback` / `unknown` 四类安全标签，永不构成高风险；只有接入外部情报源、来源被标注为 `hosting`、`proxy` 等富标签时才判定为高风险来源。`security.session_risk.login_enforcement` 档位：

- `off`（默认）：不执行，登录行为与既有版本完全一致
- `step_up`：高风险来源登录强制第二因子——已绑定 TOTP 的用户进入挑战步（即使 2FA 总开关关闭）；未绑定 TOTP 的用户直接拒绝
- `block`：高风险来源登录凭据验证通过后仍拒绝（403，响应不回显判定依据）

情报源未启用或档位未配置时零行为变化。真实运行验收：`make auth-audit-acceptance`（stub 情报源标注 hosting → block 阶段 403 拦截、错误密码 401；off 阶段同库重启正常登录 200；经 `GET /api/audit/logs` 对账三类登录行与 `[REDACTED]` 脱敏）。

### 速率限制

- `registerBaseMiddleware` 已统一挂载 `RateLimitMiddlewareFromConfig`
- 支持全局限流、按路径前缀限流、按 header key 限流，以及 IP / key 白名单
- 默认开发配置 `config.yml` 已启用基础限流，并为 `/public/`、`/public/kb/`、`/public/csat/`、`/api/v1/auth/`、`/api/v1/ws`、`/uploads/`、`/api/v1/ai/query`、`/api/v1/metrics/ingest`、`/api/` 提供独立路径级限流
- 生产环境仍应基于 `config.production.secure.example.yml` 或等价部署配置收紧阈值、域名和 secrets 注入方式，而不是直接把开发配置原样上线

### 启动期告警

- `InitLogging` 现在会在启动时输出基础安全告警
- 当前已覆盖默认或空 `jwt.secret`、开放式 `CORS=*`、关闭限流、空 OpenAI API key、启用外部 knowledge provider 但缺少必要 credentials 等场景
- 若启用了限流但没有为 `/public/`、`/public/kb/`、`/public/csat/`、`/api/v1/auth/`、`/api/v1/ws`、`/uploads/`、`/api/v1/metrics/ingest`、`/api/` 配置独立路径级限流，也会输出 warning，避免公开入口、高成本 service 面和管理面只落在全局限流下
- 启动期仍为 warning-only，不阻断本地开发或测试启动；部署前应再执行 `check-security-baseline --strict` 做显式失败校验

### 部署前校验入口

- CLI：`go -C apps/server run ./cmd -c config.yml check-security-baseline --strict`
- 脚本：`sh ./scripts/check-security-baseline.sh config.yml`
- Make：`make security-check CONFIG=config.production.secure.example.yml`
- 该检查复用启动期 `SecurityWarnings` 规则，但在 `--strict` 下会以非零退出，适合 CI、CD 或上线前人工校验

## 建议的最小生产配置

建议至少开启以下限流基线：

- 全局 `security.rate_limiting.enabled = true`
- `/api/`：保护管理面写操作与批量读取
- `/api/v1/auth/`：单独更严格限流，优先保护登录、注册、refresh 这类匿名认证入口
- `/api/v1/ai/`：单独更严格限流，尤其是 `knowledge/upload`、`knowledge/sync`
- `/api/v1/metrics/ingest`：单独限流，并优先使用 service token 或专用 header key
- `/public/`：按匿名入口单独限流，避免爬取与爆破
- `/uploads/`：单独限流，避免文件枚举、热点资源刷取或带宽放大

建议同时满足以下约束：

- `jwt.secret`、OpenAI / Dify / WeKnora API key 不使用代码默认值
- secrets 只通过环境变量或外部 secret manager 注入
- 管理面必须部署在 TLS 后
- `security.cors.allowed_origins` 不使用生产环境全量 `*`
- `X-Request-ID` 由网关或入口统一注入，便于审计追踪

## 仍待补齐的缺口

- 当前已具备单用户查询、批量安全态预览、单用户 / 批量用户 token 吊销、独立 refresh token、单 token revoke list、revoke list 查询，以及 auth session 列表查询、单 session 吊销与按用户批量 session 吊销入口；session 列表现已返回基础 client metadata（`device_fingerprint` / `user_agent` / `client_ip` / `last_seen_at`）以及衍生字段（`network_label` / `location_label` / `risk_score` / `risk_level` / `risk_reasons`），并补充 `family_public_ip_count` / `family_device_count` / `active_session_count` / `family_hot_refresh_count` / `reference_session_id` / `ip_drift` / `device_drift` / `rapid_ip_change` / `rapid_device_change` / `refresh_recency` / `rapid_refresh_activity` 这类同账号并发、漂移、短窗口切换与 refresh 活跃度上下文；denylist 过期记录会由后台 worker 自动清理，`security.session_risk` 也已成为正式配置面
- 上述 `security/users/*` 与 `security/tokens/*` 管理入口现已补 tenant/workspace 目标用户隔离，不再允许 scoped 请求跨租户枚举、吊销或查看其它 `Agent` / `Customer` 关联用户的 token / session 记录
- 当前剩余缺口应视为后续增强，而不是本轮 T5 阻塞：真实 Geo/IP 情报富化、更强异常模型、按环境/租户差异化策略
- 本轮已完成的部分是：Geo/IP 已具备可注入 adapter、HTTP provider、配置项、router wiring 以及 auth / management 两侧自动化覆盖；后续再接具体外部情报厂商时，不应回退为“基础能力未完成”
- 外部开放接口已开始通过代码级 `security surface catalog` 做最小登记校验；后续仍可继续增强为更细粒度的公开接口审批和自动化审查
- scoped config 关键配置变更现已强制要求 `change_ref` 与 `reason`，并把这些字段带入审计 request metadata 与 history 元数据；这补上了最小变更追踪基线
- 高风险 scoped config 变更现已补真实执行前审批约束：命中 provider endpoint、knowledge mapping、session risk 核心阈值或 rollback 这类高风险场景时，执行接口不仅必须提供 `approval_ref`，还必须能按 `approval_ref + change_ref` 查到已记录审批事件，且审批人与执行人必须分离
- scoped config 现已补执行后 verification 入口，可对单条 update / rollback 审计记录写入 `passed` / `failed` 结论，并在 history 列表与详情中回显 `verification_status`、最新 verification 与 verification policy
- 当前 verification 已补模板化检查项与最小双人复核约束：reviewer 不能与原始执行人相同，verification 请求必须提交与 `verification_template.checks` 对齐的 `checks`，`passed` 必须带 evidence 且所有必填检查项都要 `passed`，`failed` 必须带 notes 且至少要有一个检查项 `failed`
- `verification_template` 已进一步按字段风险拆分，并返回根级 `changed_paths` 以及单 check 的 `risk_level` / `changed_paths`，便于对 provider endpoint、KB mapping、session risk threshold 这类高风险配置执行标准化验收
- 写入响应、history 列表、单条详情和 verify 响应会同步返回 `change_risk` 与 `approval_policy`，把当前配置变更的风险等级、触发原因、审批记录落库状态以及最新审批人信息直接暴露给前端或自动化脚本
- 同一批响应现在还会统一返回 `governance_status` / `governance_policy`，把审批前置和执行后验证合并为单一治理状态；verify 响应会进一步返回 source change 的闭环状态
- history 列表、单条详情和 verify 响应会同步返回 `verification_template` 与更细粒度的 `verification_policy`，用于前端或自动化脚本按模板执行变更后验证
- history 列表现在还能按 `governance_status` / `risk_level` / `approval_status` / `verification_status` / `needs_action` 直接筛选治理队列，并返回 `governance_summary` 汇总，方便运营或安全值班直接拉取待办视图
- 后续仍需把关键配置变更沉淀成完整的“执行前审批 / 执行后验证 / 回滚记录”操作手册，并补自动化验收与更细粒度审批编排

## 审计与回滚操作约束

- 审计导出默认只适合轻量排障或留档；单次 `GET /api/audit/logs/export` 默认最多导出 `1000` 条，服务端硬上限 `5000`
- 超过轻量导出规模时，不应通过反复分页脚本直接从在线管理面拉全量历史；应走离线归档或后续异步导出方案
- 在线审计库当前默认保留 `180` 天，后台 worker 会按批次清理过期记录；涉及安全事件、权限变更、关键配置变更的长期留档仍需落到外部归档策略
- scoped config rollback 不能视为普通“恢复按钮”:
  - 必须先用 `GET /api/security/config/{scope}/history/:audit_id` 查看 snapshot diff
  - 高风险回滚必须提交 `approval_ref`，且审批人与执行人分离
  - 执行后还要补 `verify` 结论，确认 portal / provider / session risk 等受影响路径已经恢复
- 运营值班在执行 rollback 前后，至少要留存三类证据：source audit ID、rollback audit ID、verification audit ID

## 代码落点

- 路由与表面分类：`apps/server/internal/app/server/router.go`
- 基础中间件装配：`apps/server/internal/app/server/middleware.go`
- 速率限制：`apps/server/internal/middleware/ratelimit.go`
- 审计中间件：`apps/server/internal/platform/audit/gin_middleware.go`
- 启动安全告警：`apps/server/internal/app/bootstrap/security.go`
- 部署前严格校验：`apps/server/cmd/cli/check_security_baseline.go`
- 路由安全表面目录与覆盖校验：`apps/server/internal/app/server/security_surface.go`
