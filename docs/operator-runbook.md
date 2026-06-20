# Servify Operator Runbook

> 本文档面向运维人员和 SRE，覆盖 Servify 从部署、日常运维到故障排查的完整操作手册。
>
> 相关文档：
>
> - [部署指南](deployment.md) — 构建、配置与部署流程
> - [安全基线](security-baseline-operations.md) — 安全检查与生产配置清单
> - [可观测性运维手册](https://github.com/timebeau/servify/blob/main/deploy/observability/runbook/operational-runbook.md) — 告警排查与指标参考
> - [统计口径](metrics-glossary.md) — Dashboard 指标定义

---

## 目录

1. [环境与角色说明](#1-环境与角色说明)
2. [部署流程](#2-部署流程)
3. [发布前检查](#3-发布前检查)
4. [配置管理](#4-配置管理)
5. [密钥与凭证管理](#5-密钥与凭证管理)
6. [数据库迁移](#6-数据库迁移)
7. [健康检查与探针](#7-健康检查与探针)
8. [监控与告警](#8-监控与告警)
9. [日志查询](#9-日志查询)
10. [常见故障排查](#10-常见故障排查)
11. [回滚操作](#11-回滚操作)
12. [扩缩容](#12-扩缩容)
13. [安全运维操作](#13-安全运维操作)
14. [日常运维任务清单](#14-日常运维任务清单)

---

## 1. 环境与角色说明

### 环境分层

| 环境 | 用途 | 配置文件 | 数据 |
|------|------|----------|------|
| 本地开发 | 开发调试 | `config.yml` | 本地 PostgreSQL / Redis |
| 测试 / Staging | 集成验证 | `config.staging.example.yml` -> `config.staging.yml` | 独立测试数据库 |
| 生产 | 线上服务 | `config.production.yml` | 生产数据库 |

### 角色与职责

| 角色 | 权限 | 典型操作 |
|------|------|----------|
| Operator | 部署、监控、故障恢复 | 部署、回滚、扩缩容、日志查询 |
| SRE | 告警响应、性能调优 | 告警处理、容量规划、预案演练 |
| Admin | 管理后台操作 | 用户管理、权限分配、审计查询 |
| Developer | 代码变更、发布打包 | 构建、测试、提交发布 |

---

## 2. 部署流程

### 2.1 首次部署

```bash
# 1. 准备配置文件
cp config.production.secure.example.yml config.production.yml
# 编辑数据库连接、密钥、CORS 等配置
# 密钥通过环境变量注入，不要写在配置文件中

# 2. 启动基础设施
docker compose -f infra/compose/docker-compose.yml up postgres redis -d

# 3. 运行数据库迁移（首次启动时 GORM AutoMigrate 会自动建表）
make migrate DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=xxx DB_NAME=servify

# 4. 构建生产镜像
docker build -t servify:v0.1.0 -t servify:latest .

# 5. 启动服务
docker compose -f infra/compose/docker-compose.yml up -d servify

# 6. 验证部署
curl http://localhost:8080/health
curl http://localhost:8080/ready
```

### 2.2 Docker Compose 部署

```bash
# 基础部署（API + DB + Redis）
docker compose -f infra/compose/docker-compose.yml up -d

# 带 WeKnora mock 验收环境
docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.weknora.yml \
  up -d

# 带可观测性栈（OTel Collector + Jaeger）
docker compose -f infra/compose/docker-compose.observability.yml up -d

# 全套本地验收部署
docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.weknora.yml \
  -f infra/compose/docker-compose.observability.yml \
  up -d
```

### 2.3 Fly.io 部署

```bash
# 初始化
fly launch --dockerfile Dockerfile --name servify --region hkg

# 添加数据库
fly postgres create
fly postgres attach <db-name>

# 注入密钥
fly secrets set JWT_SECRET=$(openssl rand -hex 32)
fly secrets set OPENAI_API_KEY=sk-xxx

# 部署
fly deploy

# 验证
fly status
fly logs
```

### 2.4 Kubernetes 部署

参照 `docs/deployment.md` 第 5.4 节中的 Deployment 和 Service YAML 模板。关键要点：

- 设置 `readinessProbe` 指向 `/ready`，`livenessProbe` 指向 `/health`
- 通过 `Secret` 对象注入敏感配置
- 建议副本数 >= 2，配置 `PodDisruptionBudget`

---

## 3. 发布前检查

每次发布前必须通过以下检查，确保部署质量。

### 3.1 一键发布检查

```bash
make release-check CONFIG=config.production.yml
```

该命令会串行执行以下四项检查：

1. **Local Check** — 本地环境、仓库卫生、生成物一致性
2. **Security Check** — 安全基线（JWT secret、CORS、限流、密钥注入）
3. **Observability Check** — 可观测性基线（metrics、tracing、dashboard、alert、runbook）
4. **Regression Tests** — 核心 Go 回归测试

### 3.1A Staging 演练口径

在进入正式生产前，建议先对 staging 配置执行同一轮检查：

```bash
cp config.staging.example.yml config.staging.yml
make security-check CONFIG=config.staging.yml
make observability-check CONFIG=config.staging.yml
make release-check CONFIG=config.staging.yml
```

当前仓库内置的 `config.staging.example.yml` 已满足 strict baseline，用于演练：

- staging 环境标签
- staging 域名级 CORS
- 启用 RBAC、限流、审计与 observability
- secrets 仍通过环境变量注入，而不是写死在文件里

### 3.2 分项检查

```bash
# 本地环境校验
make local-check

# 安全基线严格检查
make security-check CONFIG=config.production.yml

# 可观测性基线严格检查
make observability-check CONFIG=config.production.yml

# 仓库卫生
make repo-hygiene

# Go 回归测试
go -C apps/server test ./cmd/cli ./internal/app/bootstrap
```

### 3.3 检查失败处理

| 检查项 | 失败原因 | 处理方式 |
|--------|----------|----------|
| Security: JWT secret | 使用了默认值 | 生成强随机密钥并注入环境变量 |
| Security: CORS | 配置为 `*` | 限制为实际域名 |
| Security: Rate limiting | 未启用 | 在配置中开启限流 |
| Observability: Metrics | 端点不可达 | 检查 `/metrics` 路由和 Prometheus 配置 |
| Observability: Dashboard | 资产缺失 | 确认 `deploy/observability/dashboards/` 文件存在 |
| Repo hygiene | 追踪了运行时产物 | 执行 `make clean-runtime` 并更新 `.gitignore` |

补充说明：

- `config.yml` 现在用于本地开发的同时，也保持通过当前 `security` / `observability` strict baseline 检查，便于在本地直接跑通自检。
- 这不意味着可以把 `config.yml` 直接视为生产配置；生产仍应从 `config.production.secure.example.yml` 或等价部署配置出发，显式收紧 CORS、密钥注入和限流阈值。

---

## 4. 配置管理

### 4.1 配置分层

Servify 配置遵循四层作用域，优先级从高到低：

| 层级 | 来源 | 典型内容 |
|------|------|----------|
| Runtime 覆盖 | 环境变量、启动参数 | 密钥、连接串、临时开关 |
| Workspace 级 | 数据库 | 工作区特定的 AI provider、路由策略 |
| Tenant 级 | 数据库 | 租户级门户配置、SLA 阈值 |
| System 级 | config.yml | 系统默认值、基础设施配置 |

详细规则见 [`docs/configuration-scopes.md`](configuration-scopes.md)。

### 4.2 关键配置项

```yaml
# config.yml 关键配置结构
server:
  port: 8080
  environment: "production"

database:
  host: "localhost"
  port: 5432
  name: "servify"
  password: "${DB_PASSWORD}"   # 通过环境变量注入

redis:
  host: "localhost"
  port: 6379

jwt:
  secret: "${JWT_SECRET}"      # 通过环境变量注入，禁止使用默认值

ai:
  openai:
    api_key: "${OPENAI_API_KEY}"

security:
  cors:
    allowed_origins:
      - "https://your-domain.com"
  rbac:
    enabled: true
  rate_limiting:
    enabled: true
    requests_per_minute: 300
    burst: 50
  session_risk:
    hot_refresh_window_minutes: 10
    recent_refresh_window_minutes: 30
    today_refresh_window_hours: 24
    rapid_change_window_hours: 12
    stale_activity_window_days: 14
    multi_public_ip_threshold: 2
    many_sessions_threshold: 3
    hot_refresh_family_threshold: 2
    medium_risk_score: 2
    high_risk_score: 4
  session_risk_profiles:
    production:
      hot_refresh_window_minutes: 10
      recent_refresh_window_minutes: 30
      rapid_change_window_hours: 12
      stale_activity_window_days: 14
      high_risk_score: 4
  session_ip_intelligence:
    enabled: false
    base_url: "https://geo.example.com/lookup/{ip}"
    api_key: "${SESSION_IP_API_KEY}"
    auth_header: "Authorization"
    timeout_ms: 1500

monitoring:
  enabled: true
  tracing:
    enabled: true
    endpoint: "http://otel-collector:4317"
    insecure: true
    sample_ratio: 0.1
  metrics_path: "/metrics"

log:
  level: "info"
  format: "json"
```

### 4.3 配置变更流程

1. 修改 `config.yml` 或对应环境变量
2. 运行 `make security-check CONFIG=config.yml` 验证
3. 重启服务生效
4. 配置变更会记录在审计日志中

---

## 5. 密钥与凭证管理

### 5.1 必需密钥

| 密钥 | 生成方式 | 轮换影响 |
|------|----------|----------|
| `JWT_SECRET` | `openssl rand -hex 32` | 轮换后所有已登录用户需重新登录 |
| `OPENAI_API_KEY` | OpenAI 平台获取 | 轮换后 AI 功能短暂不可用直到新 key 生效 |
| `DB_PASSWORD` | 强随机密码 | 需同步更新数据库和配置 |

### 5.2 密钥注入方式

**推荐：环境变量**

```bash
export JWT_SECRET=$(openssl rand -hex 32)
export OPENAI_API_KEY=sk-proj-xxx
export DB_PASSWORD=your-strong-password
```

**Docker Secrets**

```bash
echo "your-jwt-secret" > ./secrets/jwt_secret
# 在 docker-compose.yml 中通过 secrets 引用
```

**Fly.io**

```bash
fly secrets set JWT_SECRET=xxx OPENAI_API_KEY=sk-xxx
```

### 5.3 密钥轮换

详细的密钥生命周期和轮换流程见 [`docs/token-lifecycle-and-key-rotation.md`](token-lifecycle-and-key-rotation.md)。

JWT 密钥轮换步骤：

1. 生成新密钥：`openssl rand -hex 32`
2. 更新环境变量或 Secret
3. 重启服务：新 token 使用新密钥签名，旧 token 在过期前仍然有效
4. 如需立即失效所有旧 token：调用 `POST /api/agents/:id/revoke-tokens`

---

## 6. 数据库迁移

### 6.1 自动迁移

Servify 使用 GORM AutoMigrate，服务启动时会自动创建缺失的表和列。

### 6.2 手动迁移

```bash
# 运行迁移
make migrate DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=xxx DB_NAME=servify

# 带种子数据
make migrate-seed DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=xxx DB_NAME=servify
```

### 6.3 迁移问题排查

```bash
# 检查数据库连接
psql -h localhost -U postgres -d servify -c "SELECT 1"

# 检查表结构
psql -h localhost -U postgres -d servify -c "\dt"

# Docker 环境中手动初始化
docker compose exec postgres psql -U postgres -d servify -f /docker-entrypoint-initdb.d/01-init.sql
```

---

## 7. 健康检查与探针

### 7.1 端点说明

| 端点 | 用途 | 检查内容 | 超时建议 |
|------|------|----------|----------|
| `GET /health` | 存活探针 (Liveness) | 进程存活 | 5s |
| `GET /ready` | 就绪探针 (Readiness) | DB + Redis + 可选 LLM / knowledge provider 连接 | 10s |
| `GET /metrics` | Prometheus 指标 | 全量运行时指标 | 15s |

### 7.2 探针配置建议

**Kubernetes：**

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 30

readinessProbe:
  httpGet:
    path: /ready
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 15
```

**Docker Compose：**

```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
  interval: 30s
  timeout: 10s
  retries: 5
  start_period: 40s
```

### 7.3 /ready 失败排查

当 `/ready` 返回非 200 时，检查以下依赖：

```bash
# PostgreSQL
psql -h $DB_HOST -U $DB_USER -d servify -c "SELECT 1"

# Redis
redis-cli -h $REDIS_HOST ping

# 可选：外部 knowledge provider
curl -s http://$WEKNORA_HOST:9000/health
```

---

## 8. 监控与告警

### 8.1 可观测性架构

```
Servify :8080/metrics
       |
       v
  Prometheus (scrape /15s)
       |
       +---> Grafana (dashboards)
       +---> Alertmanager (alerts)

Servify --OTLP--> OTel Collector :4317 --> Jaeger :16686
```

### 8.2 预置 Dashboard

导入路径：`deploy/observability/dashboards/`

| Dashboard | 文件 | 内容 |
|-----------|------|------|
| 基础设施面板 | `servify-service.json` | HTTP 速率/延迟、错误率、事件总线、Worker、Go Runtime |
| 业务面板 | `servify-business.json` | 会话、工单、路由、AI 请求量/延迟/Token |

### 8.3 告警规则

定义文件：`deploy/observability/alerts/rules.yaml`

| 告警 | 级别 | 触发条件 | 紧急程度 |
|------|------|----------|----------|
| HighHTTP5xxRate | Critical | 5xx > 5% 持续 5 分钟 | 立即响应 |
| HighP99Latency | Warning | P99 > 5s 持续 10 分钟 | 30 分钟内响应 |
| HighRateLimitDrops | Info | 限流丢弃速率 > 10/s 持续 5 分钟 | 当班关注 |
| HighGoroutineCount | Warning | goroutines > 10000 持续 10 分钟 | 30 分钟内响应 |
| HighSystemErrorRate | Critical | 系统错误 > 0.01/s 持续 5 分钟 | 立即响应 |
| HighDependencyErrorRate | Warning | 依赖错误 > 0.1/s 持续 5 分钟 | 30 分钟内响应 |
| EventBusHandlerFailures | Warning | 事件处理失败持续 5 分钟 | 30 分钟内响应 |
| EventBusDeadLetters | Info | dead letter 持续 10 分钟 | 当班关注 |
| AIProviderDegraded | Critical | AI 失败率 > 20% 持续 5 分钟 | 立即响应 |
| AIHighLatency | Warning | AI P95 > 10s 持续 10 分钟 | 30 分钟内响应 |
| WorkerJobFailures | Warning | Worker 失败持续 10 分钟 | 1 小时内响应 |

生产口径补充：

- 上述阈值以 `deploy/observability/alerts/rules.yaml` 为准，runbook 只做解释和响应优先级说明
- 若 staging 演练需要更宽阈值，应在告警平台或环境级 Prometheus 规则中调整，不要直接改写 production baseline 文件
- `config.production.secure.example.yml` 现已显式给出 `monitoring.metrics_path` 与 tracing 基线，避免生产模板只靠代码默认值通过 strict 检查

### 8.4 告警排查详细手册

详见 [`deploy/observability/runbook/operational-runbook.md`](https://github.com/timebeau/servify/blob/main/deploy/observability/runbook/operational-runbook.md)。

### 8.5 指标参考

详见 [`docs/metrics-glossary.md`](metrics-glossary.md)。

---

## 9. 日志查询

### 9.1 日志格式

Servify 输出 JSON 结构化日志，包含以下关联字段：

```json
{
  "time": "2026-03-29T10:00:00Z",
  "level": "info",
  "msg": "request completed",
  "request_id": "a1b2c3d4",
  "tenant_id": "tenant-1",
  "trace_id": "abc123def456",
  "method": "GET",
  "path": "/api/tickets",
  "status": 200
}
```

### 9.2 日志查询方式

```bash
# Docker Compose
docker compose logs -f servify
docker compose logs --since 1h servify | grep '"level":"error"'

# 本地运行
tail -f logs/servify.log

# 按 request_id 查询（关联追踪）
docker compose logs servify | grep "request_id"

# 按 tenant_id 查询
docker compose logs servify | grep "tenant_id.*tenant-1"

# 错误日志
docker compose logs servify | grep '"level":"error"' | jq .
```

### 9.3 日志级别

| 级别 | 使用场景 | 生产建议 |
|------|----------|----------|
| `debug` | 开发调试 | 不要在生产使用 |
| `info` | 正常请求、启动、关闭 | 推荐 |
| `warn` | 可恢复的异常、降级 | 关注 |
| `error` | 需要人工介入的问题 | 必须处理 |

---

## 10. 常见故障排查

### 10.1 服务无法启动

**症状**：进程启动后立即退出或无法访问

```bash
# 检查端口占用
lsof -i :8080

# 检查数据库连接
psql -h localhost -U postgres -d servify -c "SELECT 1"

# 检查 Redis 连接
redis-cli ping

# 查看启动日志
docker compose logs servify | head -50
```

**常见原因**：
- 端口被占用：更换端口或停止冲突进程
- 数据库连接失败：检查 DB_HOST、DB_PORT、DB_PASSWORD
- Redis 连接失败：检查 Redis 是否启动
- 配置文件错误：检查 config.yml 格式

### 10.2 高 5xx 错误率

**症状**：HighHTTP5xxRate 告警触发

```bash
# 1. 查看 /metrics 中的错误分类
curl -s http://localhost:8080/metrics | grep http_requests_total | grep "5.."

# 2. 查看分类错误
curl -s http://localhost:8080/metrics | grep errors_total

# 3. 查看应用日志
docker compose logs --since 30m servify | grep '"level":"error"'

# 4. 检查数据库连接池
curl -s http://localhost:8080/metrics | grep go_sql_open_connections
```

**常见原因**：
- 数据库连接池耗尽：增加连接池大小或减少慢查询
- 外部依赖（LLM、knowledge provider，默认优先 Dify）不可用：检查 circuit breaker 状态
- 部署后配置错误：回滚或修正配置

### 10.3 AI 功能异常

**症状**：AIProviderDegraded 告警或 AI 请求失败

```bash
# 检查 AI 指标
curl -s http://localhost:8080/metrics | grep ai_requests_total

# 检查 API Key 有效性
curl -s http://localhost:8080/metrics | grep ai_requests_total.*outcome

# 重置 circuit breaker
curl -X POST http://localhost:8080/api/v1/ai/circuit-breaker/reset \
  -H "Authorization: Bearer $TOKEN"
```

**处理方式**：
- Rate limited：降低请求频率或升级 API tier
- Auth failed：轮换 API Key
- Timeout：增加超时配置或启用 circuit breaker
- 临时降级：启用 fallback provider

### 10.4 数据库性能下降

**症状**：P99 延迟升高或查询超时

```bash
# 检查连接池使用
curl -s http://localhost:8080/metrics | grep go_sql

# 检查活跃连接
psql -h localhost -U postgres -c "SELECT count(*) FROM pg_stat_activity"

# 检查慢查询
psql -h localhost -U postgres -c "SELECT query, mean_exec_time FROM pg_stat_statements ORDER BY mean_exec_time DESC LIMIT 10"
```

### 10.5 事件处理失败

**症状**：EventBusHandlerFailures 告警

```bash
# 检查失败事件类型
curl -s http://localhost:8080/metrics | grep eventbus_failed_total

# 查看死信队列
# 通过内部 API 或日志过滤 dead letter 事件
docker compose logs servify | grep "dead_letter"
```

**处理方式**：
- 瞬时错误：事件会被自动重试或进入死信队列
- 持续错误：定位并修复 handler 代码，重新部署
- 使用 replay 接口重新处理死信事件

---

## 11. 回滚操作

### 11.1 Docker Compose 回滚

```bash
# 1. 查看当前运行的镜像版本
docker compose images servify

# 2. 回滚到上一个版本
docker compose down servify
docker tag servify:v0.0.9 servify:v0.1.0  # 如果之前保留了旧版本
# 或者拉取已知的稳定版本
docker compose up -d servify

# 3. 验证回滚
curl http://localhost:8080/health
curl http://localhost:8080/ready
```

### 11.2 Fly.io 回滚

```bash
# 查看部署历史
fly deployments list

# 回滚到上一个版本
fly deploy --detach --image servify:previous-version

# 或者直接回滚
fly rollback
```

### 11.3 Kubernetes 回滚

```bash
# 查看部署历史
kubectl rollout history deployment/servify

# 回滚到上一个版本
kubectl rollout undo deployment/servify

# 回滚到指定版本
kubectl rollout undo deployment/servify --to-revision=3

# 查看回滚状态
kubectl rollout status deployment/servify
```

### 11.4 数据库回滚注意事项

- Servify 使用 GORM AutoMigrate，**只能向前兼容**，不支持自动回滚
- 如需回滚数据库变更，需手动编写并执行 SQL
- 建议在发布前备份数据库：

```bash
pg_dump -h $DB_HOST -U postgres servify > backup_$(date +%Y%m%d_%H%M%S).sql
```

---

## 12. 扩缩容

### 12.1 垂直扩容

调整单个实例资源配置：

| 资源 | 最低 | 推荐 | 高负载 |
|------|------|------|--------|
| CPU | 1 核 | 2 核 | 4 核 |
| 内存 | 1 GB | 2 GB | 4 GB |
| 数据库内存 | 1 GB | 2 GB | 4 GB |

### 12.2 水平扩容

**Kubernetes：**

```bash
# 手动扩容
kubectl scale deployment/servify --replicas=4

# 自动扩容（需配置 HPA）
kubectl autoscale deployment/servify --min=2 --max=8 --cpu-percent=70
```

**Fly.io：**

```bash
# 增加实例
fly scale count 3

# 调整实例规格
fly scale vm shared-cpu-2x
```

### 12.3 数据库扩容

```bash
# PostgreSQL 增加连接数
# 在 postgresql.conf 中
max_connections = 200

# 增加连接池（在 Servify 配置中）
database:
  max_open_conns: 50
  max_idle_conns: 10
```

---

## 13. 安全运维操作

### 13.1 用户 Token 失效

```bash
# 使指定用户的 token 失效
curl -X POST http://localhost:8080/api/security/users/:id/revoke-tokens \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# 使指定 Agent 的 token 失效
curl -X POST http://localhost:8080/api/agents/:id/revoke-tokens \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# 使指定 Customer 的 token 失效
curl -X POST http://localhost:8080/api/customers/:id/revoke-tokens \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

### 13.2 审计日志查询

```bash
# 查询审计日志（支持过滤）
curl "http://localhost:8080/api/audit/logs?action=create&resource=ticket&limit=50" \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# 支持的过滤参数
# action      — 操作类型
# resource    — 资源类型
# principal   — 操作主体
# actor       — 操作人
# success     — 是否成功 (true/false)
# start_date  — 开始时间
# end_date    — 结束时间
```

### 13.3 速率限制调整

编辑 `config.yml` 后重启：

```yaml
security:
  rate_limiting:
    enabled: true
    requests_per_minute: 300
    burst: 50
    # 路径级限流
    paths:
      - prefix: "/api/v1/ai/"
        requests_per_minute: 60
        burst: 10
      - prefix: "/public/"
        requests_per_minute: 120
        burst: 20
      - prefix: "/public/kb/"
        requests_per_minute: 60
        burst: 15
      - prefix: "/public/csat/"
        requests_per_minute: 30
        burst: 10
```

### 13.4 安全基线检查

```bash
# CLI 检查
go -C apps/server run ./cmd -c config.yml check-security-baseline --strict

# 脚本检查
sh ./scripts/check-security-baseline.sh config.yml

# Make 入口
make security-check CONFIG=config.yml
```

### 13.5 Session Risk 阈值调整

`security.session_risk` 控制 auth session 风险启发式的时间窗口与分级阈值。建议：

- 生产环境优先收紧 `hot_refresh_window_minutes` 与 `rapid_change_window_hours`
- 若业务经常经过共享出口或 NAT 网关，谨慎调整 `multi_public_ip_threshold`
- 若要更早将 session 标为高风险，可降低 `high_risk_score`
- 若希望开发 / staging / production 使用不同基线，优先通过 `server.environment + security.session_risk_profiles.{environment}` 设定环境级默认值，再用 tenant/workspace scoped config 做细化
- 当前 auth 自助 `/api/v1/auth/sessions` 已有回归测试验证环境级 profile 会实际影响 `risk_level` 输出，而不只是停留在配置层

示例：

```yaml
security:
  session_risk:
    hot_refresh_window_minutes: 10
    recent_refresh_window_minutes: 30
    today_refresh_window_hours: 24
    rapid_change_window_hours: 12
    stale_activity_window_days: 14
    multi_public_ip_threshold: 2
    many_sessions_threshold: 3
    hot_refresh_family_threshold: 2
    medium_risk_score: 2
    high_risk_score: 4
  session_risk_profiles:
    production:
      hot_refresh_window_minutes: 10
      recent_refresh_window_minutes: 30
      rapid_change_window_hours: 12
      stale_activity_window_days: 14
      high_risk_score: 4
```

### 13.6 Geo/IP 富化 Provider 接入

`security.session_ip_intelligence` 用于给 auth / management 的 session 风险视图接入外部 Geo/IP 情报。建议：

- 仅在完成隐私与合规评审后启用
- `base_url` 使用服务端可达的内部网关或受控第三方 endpoint
- 优先让 provider 直接返回 `network_label` / `location_label`
- 保持较短 timeout，失败时系统会自动回退到本地 heuristic 分类

示例：

```yaml
security:
  session_ip_intelligence:
    enabled: true
    base_url: "https://geo.example.com/lookup/{ip}"
    api_key: "${SESSION_IP_API_KEY}"
    auth_header: "Authorization"
    timeout_ms: 1500
```

---

## 14. 日常运维任务清单

### 每日

- [ ] 检查 Grafana Dashboard 是否有异常指标
- [ ] 检查是否有未处理的 Critical 告警
- [ ] 确认 `/health` 和 `/ready` 端点正常

### 每周

- [ ] 审计日志回顾，关注异常操作模式
- [ ] 检查磁盘空间和数据库大小
- [ ] 确认备份是否正常执行
- [ ] Review Worker 和 EventBus 失败趋势

### 每月

- [ ] 密钥轮换评估（特别是 JWT_SECRET 和 API Key）
- [ ] 安全基线检查：`make security-check CONFIG=config.production.yml`
- [ ] 可观测性基线检查：`make observability-check CONFIG=config.production.yml`
- [ ] 容量规划评估（CPU、内存、数据库连接数趋势）
- [ ] 更新告警阈值（根据实际流量模式）

### 发布前

- [ ] 执行 `make release-check CONFIG=config.production.yml`
- [ ] 备份数据库
- [ ] 准备回滚方案
- [ ] 通知相关团队
- [ ] 发布后验证：`/health`、`/ready`、关键业务流程

---

## 快速命令参考

```bash
# 构建
make build
docker build -t servify:latest .

# 启动 / 停止
make run
docker compose up -d
docker compose down

# 检查
make local-check
make security-check CONFIG=config.yml
make observability-check CONFIG=config.yml
make release-check CONFIG=config.yml

# 迁移
make migrate DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=xxx DB_NAME=servify

# 健康检查
curl http://localhost:8080/health
curl http://localhost:8080/ready
curl http://localhost:8080/metrics

# 日志
docker compose logs -f servify
tail -f logs/servify.log

# 清理
make clean
make clean-runtime
make repo-hygiene
```
