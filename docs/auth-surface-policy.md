# Auth Surface Policy

本文定义 Servify HTTP 路由的授权表面分类，目标是把“这个入口到底该不该匿名、该不该走 management token、该不该只允许 service”从零散 handler 逻辑里收口到统一规则。

## 分类

### 1. Public Surface

适用范围：

- 面向匿名访问者、浏览器或终端用户的公开读接口
- 只暴露最小只读能力或建连能力

约束：

- 不依赖 management JWT
- 不承载后台管理写操作
- 必须有独立的安全评审和路径级限流基线

当前路由：

- `/health`
- `/ready`
- `/public/portal/config`
- `/public/kb/*`
- `/public/csat/*`
- `/api/v1/ws`
- `/uploads/*`

### 2. Auth Surface

适用范围：

- 认证与会话自助相关入口
- 允许匿名进入认证前置流程，或允许已登录用户管理自己的 auth session

约束：

- 匿名入口必须具备独立路径级限流
- 已登录自助入口只允许操作自己的认证状态，不复用 management surface 的资源写权限
- 不承载跨用户、跨租户的后台管理动作
- 整个 auth 组挂载含失败留痕的审计中间件（成功与 4xx/5xx 一并落库，凭据字段脱敏），失败登录与被风险策略拦截的登录可经 `GET /api/audit/logs` 对账
- 可选登录风险执行（`security.session_risk.login_enforcement`，默认 `off`）：接入 `session_ip_intelligence` 情报源后，高风险网络来源按档位强制第二因子（`step_up`）或直接拒绝（`block`）；内建启发式标签永不触发
- refresh token 重放处置（`security.session_risk.refresh_reuse_policy`，默认 `off`）：`revoke_family` 档位下检测到已轮换旧 token 重放即吊销整个会话家族（最新 token 一并失效），迫使重新登录；重放拒绝经审计留痕

当前路由：

- 匿名认证入口：`/api/v1/auth/login`、`/api/v1/auth/register`、`/api/v1/auth/refresh`
- 已登录自助入口：`/api/v1/auth/me`、`/api/v1/auth/sessions`、`/api/v1/auth/sessions/logout-current`、`/api/v1/auth/sessions/logout-others`

### 3. Management Surface

适用范围：

- 管理后台、客服工作台、运营查询、人工触发的后台动作

约束：

- 必须经过 `AuthMiddleware`
- principal kind 仅允许 `agent`、`admin`、`service`
- 资源级访问继续叠加 `RequireResourcePermission(...)`
- 不允许 `end_user` token 进入该表面

当前路由：

- `/api/*`
- `/api/v1/ws/stats`
- `/api/v1/webrtc/*`
- `/api/v1/messages/platforms`
- `/api/v1/ai/*`

### 4. Service Surface

适用范围：

- 机器到机器调用、采集上报、受控集成入口

约束：

- 必须经过 `AuthMiddleware`
- principal kind 仅允许 `service`
- 不复用 management `agent/admin` token
- 应优先设计成窄契约、低权限、可独立限流的入口

当前路由：

- `/api/v1/metrics/ingest`

## 新增路由准入规则

新增 HTTP 路由时，先回答这 4 个问题，再决定挂到哪个 surface：

1. 调用方是谁：匿名用户、终端用户、客服/管理员，还是服务账号？
2. 这个入口是公开读取、认证前置、后台管理，还是机器上报？
3. 失败时应该返回 `401`、`403`，还是允许匿名继续进入业务校验？
4. 是否需要专属路径级 rate limit，而不是只依赖全局限流？

默认策略：

- 拿不准时，不要直接挂到匿名 `public` surface
- 认证相关入口优先进入 `auth` surface，而不是混进 `public` 或 `management`
- 管理动作优先进入 `management` surface
- 机器上报、回调、采集优先进入 `service` surface

## 实现落点

- 路由装配：[router.go](https://github.com/timebeau/servify/blob/main/apps/server/internal/app/server/router.go)
- 路由安全表面目录：[security_surface.go](https://github.com/timebeau/servify/blob/main/apps/server/internal/app/server/security_surface.go)
- JWT 与 claims 归一化：[platform/auth](https://github.com/timebeau/servify/tree/main/apps/server/internal/platform/auth)
- Gin 兼容入口：[internal/middleware](https://github.com/timebeau/servify/tree/main/apps/server/internal/middleware)

## X-API-Key（service principal，开放平台）

P1 引入机器凭据：`Authorization: Bearer` 之外，`AuthMiddleware` 现在接受 `X-API-Key` 头。

认证流程：

1. 请求带 `X-API-Key: sv_...` 时走 API Key 分支（无此头则完全走原 JWT 路径，逐行不变）
2. 明文密钥 sha256 哈希后查 `api_keys.key_hash`（唯一索引）；高熵随机密钥抗穷举，无需慢哈希
3. 已吊销（`revoked_at`）、已过期（`expires_at`）或不存在一律统一 401，不区分原因
4. 成功后注入 `principal_kind=service`、租户/工作区 scope、只读权限集
5. `last_used_at` 按 60s 节流条件更新，避免高频请求写放大

权限面：

- 默认只读：`tickets.read`、`conversations.read`
- `scopes` 字段保留扩展（逗号分隔），签发时可显式指定
- 吊销即时生效：每次请求都经解析器查库校验

开放面路由（挂 management surface，`RequireResourcePermission` 控权）：

- `GET /api/v1/tickets`（既有 tickets read 权限）
- `GET /api/v1/conversations`、`GET /api/v1/conversations/:id`、`GET /api/v1/conversations/:id/messages`（conversations read）
- `POST /api/v1/metrics/ingest`（既有 service surface 能力的重复入口）

管理面路由（`api_keys` 资源权限）：

- `GET /api/v1/api-keys`（列表，只回显 prefix 与指纹状态，永不回显明文）
- `POST /api/v1/api-keys`（签发，明文仅在该响应出现一次）
- `POST /api/v1/api-keys/:id/revoke`（吊销即时生效）
- `DELETE /api/v1/api-keys/:id`

## 后续扩展

- 如果未来出现真正的 end-user authenticated business API，应新增独立 surface，而不是回退复用 management surface
- 如果引入回调签名、API key 或 mTLS，可在 service surface 内继续细分
- 如果开放接口继续增多，应把 `security surface catalog` 扩展成自动审查和审批流的输入源
