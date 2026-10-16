# Token Lifecycle And Key Rotation

本文件用于推进 `11 / T5 security-baseline-for-operations` 中的 token 生命周期、密钥轮换与最小运维规范。

## 适用范围

本规范覆盖以下密钥与令牌：

- `jwt.secret`
- OpenAI / Dify / WeKnora / 第三方 provider API key
- service token / internal integration token
- 未来 tenant 或 workspace 级 secret reference 对应的外部凭据

## 基本原则

- 不在代码默认值、测试样例以外的配置文件中长期保留生产 secrets
- 生产 secrets 只通过环境变量、secret manager 或部署平台密文注入
- 任何长期凭据都必须有 owner、用途、签发时间和轮换时间
- 新旧凭据切换必须支持回滚窗口，避免一次性替换导致全量失效
- secrets 不进入审计日志、应用日志、错误信息和健康检查输出

## JWT 生命周期基线

### 访问令牌

- 默认有效期应保持短周期
- 推荐生产基线：`15m` 到 `24h` 之间，按调用场景收紧
- 管理面与 service 面不应共享同一类长期 token

### 签发约束

- `agent`、`admin`、`service` token 必须区分 principal kind
- token 中必须携带最小必要 scope，不允许用宽 scope 兜底
- service token 应绑定调用方身份，不复用人工账号 token

### 吊销与失效

- 轮换 `jwt.secret` 前必须确定旧 token 的最大容忍存活时间
- 高风险事件发生时，允许通过强制更换 `jwt.secret` 触发全量失效
- 现已具备 `session version` 与显式 `revoke list` 两类失效机制：refresh token 刷新会提升 `session_token_version`，管理面也可将单个 JWT 的 `jti` 加入 revoke list

## 密钥轮换流程

### 1. 建立清单

每个 secret 至少记录：

- 名称
- owner
- 使用位置
- 注入方式
- 最近轮换时间
- 下次计划轮换时间

### 2. 预发布新密钥

- 在 secret manager 或部署平台中创建新版本
- 不立即删除旧版本
- 先在 staging / canary 验证新密钥可用

### 3. 分批切换

- 先切无状态实例
- 再切后台任务 / worker
- 最后移除旧密钥

### 4. 验证与回滚

切换后至少验证：

- 登录与 JWT 验证正常
- `/api/v1/ai/query` 正常
- WeKnora 连接与知识库操作正常
- `/api/v1/metrics/ingest` 未出现认证回归
- 审计日志中未出现明文 secret

若验证失败：

- 回滚到旧 secret 版本
- 记录失败窗口、受影响接口与修复动作

## 推荐轮换频率

- `jwt.secret`：高敏部署建议 `30-90` 天轮换一次
- provider API key：按供应商能力与风险等级，建议 `30-90` 天
- service token：建议 `30-60` 天，或改为更短期可自动签发凭据
- 测试 / 临时集成 token：任务结束后立即清理

## 最小排查清单

发生疑似泄漏时，至少执行：

1. 确认泄漏的是哪类 token / key。
2. 立即停用或轮换受影响凭据。
3. 检查对应接口在泄漏窗口内的审计日志与访问日志。
4. 校验是否存在越权调用、配置变更、知识库上传或 metrics 伪造。
5. 补充事件复盘与后续防护项。

## 与当前代码的对应关系

- JWT 配置：`apps/server/internal/config/config.go`
- 认证与 principal kind：`apps/server/internal/platform/auth`
- token 失效策略钩子：`apps/server/internal/platform/auth/token_policy.go`
- 基于用户状态的 token 失效策略：`apps/server/internal/platform/auth/user_state_policy.go`
- 管理面主动失效入口：`POST /api/agents/:id/revoke-tokens`、`POST /api/customers/:id/revoke-tokens`
- 通用失效实现：`apps/server/internal/platform/usersecurity/revoke.go`
- 通用 admin/security 入口：`GET /api/security/users/:id`、`POST /api/security/users/:id/revoke-tokens`、`POST /api/security/tokens/revoke`、`GET /api/security/tokens/revoked`
- session 家族治理入口：`GET /api/security/users/:id/sessions`、`POST /api/security/users/:id/sessions/revoke`、`POST /api/security/users/:id/sessions/revoke-all`
- session 元数据：`user_auth_sessions` 当前会记录 `device_fingerprint`、`user_agent`、`client_ip`、`last_seen_at`，列表响应还会派生 `network_label`、`location_label`、`risk_score`、`risk_level`、`risk_reasons`，并提供 `family_public_ip_count`、`family_device_count`、`active_session_count`、`family_hot_refresh_count`、`reference_session_id`、`ip_drift`、`device_drift`、`rapid_ip_change`、`rapid_device_change`、`refresh_recency`、`rapid_refresh_activity` 作为 session-family 排查辅助字段
- 上述 session 风险字段当前定位为运维排查辅助信号，不应视为严格风控判定；Geo/IP 富化层现已具备可注入 provider adapter，默认仍使用启发式 IP 分类，真实 Geo/IP 数据源与更强异常检测仍留待后续增强
- auth 自助管理入口：`GET /api/v1/auth/sessions`、`POST /api/v1/auth/sessions/logout-current`、`POST /api/v1/auth/sessions/logout-others`
- 管理面 / service 面路由表面：`apps/server/internal/app/server/router.go`
- 审计脱敏：`apps/server/internal/platform/audit/gin_middleware.go`
