# Servify Operator Runbook

> This document is aimed at operations staff and SREs. It covers Servify's complete operations manual, from deployment and daily operations through troubleshooting.
>
> Related documents:
>
> - [Deployment Guide](deployment.md) — build, configuration, and deployment flow
> - [Security Baseline](security-baseline-operations.md) — security checks and production configuration checklist
> - [Observability Operations Manual](https://github.com/cuihairu/servify/blob/main/deploy/observability/runbook/operational-runbook.md) — alert triage and metrics reference
> - [Metrics Glossary](metrics-glossary.md) — dashboard metric definitions

---

## Table of Contents

1. [Environments and Roles](#1-environments-and-roles)
2. [Deployment Flow](#2-deployment-flow)
3. [Pre-Release Checks](#3-pre-release-checks)
4. [Configuration Management](#4-configuration-management)
5. [Secrets and Credential Management](#5-secrets-and-credential-management)
6. [Database Migrations](#6-database-migrations)
7. [Health Checks and Probes](#7-health-checks-and-probes)
8. [Monitoring and Alerting](#8-monitoring-and-alerting)
9. [Log Queries](#9-log-queries)
10. [Common Troubleshooting](#10-common-troubleshooting)
11. [Rollback Procedures](#11-rollback-procedures)
12. [Scaling](#12-scaling)
13. [Security Operations](#13-security-operations)
14. [Daily Operations Task List](#14-daily-operations-task-list)

---

## 1. Environments and Roles

### Environment Layering

| Environment | Purpose | Configuration File | Data |
|------|------|----------|------|
| Local development | Development and debugging | `config.yml` | Local PostgreSQL / Redis |
| Test / Staging | Integration verification | `config.staging.example.yml` -> `config.staging.yml` | Separate test database |
| Production | Live service | `config.production.yml` | Production database |

### Roles and Responsibilities

| Role | Permissions | Typical Operations |
|------|------|----------|
| Operator | Deployment, monitoring, incident recovery | Deploy, roll back, scale, query logs |
| SRE | Alert response, performance tuning | Handle alerts, capacity planning, runbook drills |
| Admin | Admin console operations | User management, permission assignment, audit queries |
| Developer | Code changes, release packaging | Build, test, submit for release |

---

## 2. Deployment Flow

### 2.1 First Deployment

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

### 2.2 Docker Compose Deployment

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

### 2.3 Fly.io Deployment

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

### 2.4 Kubernetes Deployment

Refer to the Deployment and Service YAML templates in section 5.4 of `docs/deployment.md`. Key points:

- Set `readinessProbe` to point at `/ready`, and `livenessProbe` to point at `/health`
- Inject sensitive configuration via `Secret` objects
- Keep the replica count at `replicas: 1`: the current delivery boundary is single-instance-first; multiple replicas cause duplicate side effects or lost real-time push (see `docs/deployment.md` §5 and the convergence conditions in `docs/multi-instance-boundary.md`)

---

## 3. Pre-Release Checks

Every release must pass the following checks before going out, to ensure deployment quality.

### 3.1 One-Shot Release Check

```bash
make release-check CONFIG=config.production.yml
```

This command runs the following five checks serially:

1. **Local Check** — local environment, repository hygiene, generated-artifact consistency
2. **Security Check** — security baseline (JWT secret, CORS, rate limiting, secret injection)
3. **Observability Check** — observability baseline (metrics, tracing, dashboard, alert, runbook)
4. **Regression Tests** — core Go regression tests
5. **Build Verification** — `make build` build verification and artifact inspection

### 3.1A Staging Drill Convention

Before entering production, it is recommended to run the same round of checks against the staging configuration:

```bash
cp config.staging.example.yml config.staging.yml
make security-check CONFIG=config.staging.yml
make observability-check CONFIG=config.staging.yml
make release-check CONFIG=config.staging.yml
```

The repository's built-in `config.staging.example.yml` already satisfies the strict baseline, for drill use:

- staging environment tag
- staging domain-level CORS
- RBAC, rate limiting, audit, and observability enabled
- secrets still injected via environment variables, not hard-coded in the file

### 3.2 Individual Checks

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
go -C apps/server test ./cmd/cli ./internal/app/bootstrap ./internal/releasecheck
```

### 3.3 Handling Check Failures

| Check Item | Failure Cause | Handling |
|--------|----------|----------|
| Security: JWT secret | Default value used | Generate a strong random secret and inject it via environment variable |
| Security: CORS | Configured as `*` | Restrict to actual domains |
| Security: Rate limiting | Not enabled | Enable rate limiting in the configuration |
| Observability: Metrics | Endpoint unreachable | Check the `/metrics` route and the Prometheus configuration |
| Observability: Dashboard | Assets missing | Confirm files exist under `deploy/observability/dashboards/` |
| Repo hygiene | Runtime artifacts tracked | Run `make clean-runtime` and update `.gitignore` |

Additional notes:

- `config.yml` now serves local development while also passing the current `security` / `observability` strict baseline checks, so self-checks can be run straight through locally.
- This does not mean `config.yml` can be treated directly as production configuration; production should still start from `config.production.secure.example.yml` or an equivalent deployment configuration, explicitly tightening CORS, secret injection, and rate-limit thresholds.

---

## 4. Configuration Management

### 4.1 Configuration Layering

Servify configuration follows a four-layer scope, in descending priority order:

| Layer | Source | Typical Content |
|------|------|----------|
| Runtime override | Environment variables, startup parameters | Secrets, connection strings, temporary toggles |
| Workspace level | Database | Workspace-specific AI providers, routing policy |
| Tenant level | Database | Tenant-level portal configuration, SLA thresholds |
| System level | config.yml | System defaults, infrastructure configuration |

See [`docs/configuration-scopes.md`](configuration-scopes.md) for the detailed rules.

### 4.2 Key Configuration Items

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

### 4.3 Configuration Change Flow

1. Modify `config.yml` or the corresponding environment variables
2. Run `make security-check CONFIG=config.yml` to validate
3. Restart the service for the change to take effect
4. Configuration changes are recorded in the audit log

---

## 5. Secrets and Credential Management

### 5.1 Required Secrets

| Secret | Generation Method | Rotation Impact |
|------|----------|----------|
| `JWT_SECRET` | `openssl rand -hex 32` | After rotation, all logged-in users must log in again |
| `OPENAI_API_KEY` | Obtain from the OpenAI platform | After rotation, AI features are briefly unavailable until the new key takes effect |
| `DB_PASSWORD` | Strong random password | The database and configuration must be updated together |

### 5.2 Secret Injection Methods

**Recommended: environment variables**

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

### 5.3 Secret Rotation

See [`docs/token-lifecycle-and-key-rotation.md`](token-lifecycle-and-key-rotation.md) for the detailed secret lifecycle and rotation flow.

JWT secret rotation steps:

1. Generate a new secret: `openssl rand -hex 32`
2. Update the environment variable or Secret
3. Restart the service: new tokens are signed with the new secret, old tokens remain valid until they expire
4. To invalidate all old tokens immediately: call `POST /api/agents/:id/revoke-tokens`

---

## 6. Database Migrations

### 6.1 Automatic Migration

Servify uses GORM AutoMigrate; missing tables and columns are created automatically when the service starts.

### 6.2 Manual Migration

```bash
# 运行迁移
make migrate DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=xxx DB_NAME=servify

# 带种子数据
make migrate-seed DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=xxx DB_NAME=servify
```

### 6.3 Migration Troubleshooting

```bash
# 检查数据库连接
psql -h localhost -U postgres -d servify -c "SELECT 1"

# 检查表结构
psql -h localhost -U postgres -d servify -c "\dt"

# 版本化迁移在 apps/server/internal/app/bootstrap/migrations/，手动执行走标准入口：
make migrate
```

---

## 7. Health Checks and Probes

### 7.1 Endpoint Description

| Endpoint | Purpose | What It Checks | Recommended Timeout |
|------|------|----------|----------|
| `GET /health` | Liveness probe | Process is alive | 5s |
| `GET /ready` | Readiness probe | DB + Redis + optional LLM / knowledge provider connections | 10s |
| `GET /metrics` | Prometheus metrics | Full runtime metrics | 15s |

### 7.2 Probe Configuration Recommendations

**Kubernetes:**

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

**Docker Compose:**

```yaml
healthcheck:
  test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://localhost:8080/health"]
  interval: 30s
  timeout: 10s
  retries: 5
  start_period: 40s
```

### 7.3 /ready Failure Troubleshooting

When `/ready` returns a non-200, check the following dependencies:

```bash
# PostgreSQL
psql -h $DB_HOST -U $DB_USER -d servify -c "SELECT 1"

# Redis
redis-cli -h $REDIS_HOST ping

# 可选：外部 knowledge provider
curl -s http://$WEKNORA_HOST:9000/health
```

---

## 8. Monitoring and Alerting

### 8.1 Observability Architecture

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

### 8.2 Prebuilt Dashboards

Import path: `deploy/observability/dashboards/`

| Dashboard | File | Content |
|-----------|------|------|
| Infrastructure panel | `servify-service.json` | HTTP rate/latency, rate-limit drops, event bus throughput, Worker jobs, Go runtime |
| Business panel | `servify-business.json` | Sessions, tickets, routing, AI request volume (including strategy dimension)/latency/tokens, strategy distribution, fallback ratio |

### 8.3 Alert Rules

Definition file: `deploy/observability/alerts/rules.yaml`

| Alert | Level | Trigger Condition | Urgency |
|------|------|----------|----------|
| HighHTTP5xxRate | Critical | 5xx > 5% for 5 minutes | Respond immediately |
| HighP99Latency | Warning | P99 > 5s for 10 minutes | Respond within 30 minutes |
| HighRateLimitDrops | Info | rate-limit drop rate > 10/s for 5 minutes | On-call attention |
| HighGoroutineCount | Warning | goroutines > 10000 for 10 minutes | Respond within 30 minutes |
| EventBusHandlerFailures | Warning | event handler failure rate > 0 for 5 minutes | Respond within 30 minutes |
| EventBusDeadLetters | Info | dead-letter events present for 10 minutes | On-call attention |
| WorkerJobFailures | Warning | Worker failure rate > 0 for 10 minutes | Respond within 30 minutes |
| AIProviderDegraded | Critical | AI failure rate > 20% for 5 minutes | Respond immediately |
| AIHighLatency | Warning | AI P95 > 10s for 10 minutes | Respond within 30 minutes |
| AIFallbackRatioHigh | Warning | AI fallback ratio > 50% for 10 minutes (excluding transfer-to-human) | Respond within 30 minutes |

Production convention additions:

- The thresholds above are governed by `deploy/observability/alerts/rules.yaml`; this runbook only explains them and describes response priority
- If staging drills need wider thresholds, adjust them in the alert platform or at the environment-level Prometheus rules; do not edit the production baseline file directly
- The alert rules, Grafana dashboards, and `deploy/observability/runbook/operational-runbook.md` are kept consistent by a CI consistency test (`apps/server/internal/observability/metrics/consistency_test.go`); metrics that are defined but not yet wired up are registered in `deploy/observability/known-gaps.md`, and their corresponding alerts and dashboard panels have been removed, to be restored once wiring is complete
- `config.production.secure.example.yml` now explicitly sets `monitoring.metrics_path` and the tracing baseline, so the production template no longer passes the strict checks by relying on code defaults alone

### 8.4 Detailed Alert Triage Manual

See [`deploy/observability/runbook/operational-runbook.md`](https://github.com/cuihairu/servify/blob/main/deploy/observability/runbook/operational-runbook.md) for details.

### 8.5 Metrics Reference

See [`docs/metrics-glossary.md`](metrics-glossary.md) for details.

---

## 9. Log Queries

### 9.1 Log Format

Servify outputs JSON structured logs containing the following correlation fields:

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

### 9.2 Log Query Methods

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

### 9.3 Log Levels

| Level | Use Case | Production Recommendation |
|------|----------|----------|
| `debug` | Development and debugging | Do not use in production |
| `info` | Normal requests, startup, shutdown | Recommended |
| `warn` | Recoverable anomalies, degradation | Watch |
| `error` | Problems requiring human intervention | Must be handled |

---

## 10. Common Troubleshooting

### 10.1 Service Fails to Start

**Symptom**: the process exits immediately after starting, or is unreachable

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

**Common causes**:
- Port already in use: change the port or stop the conflicting process
- Database connection failure: check DB_HOST, DB_PORT, DB_PASSWORD
- Redis connection failure: check whether Redis is running
- Configuration file error: check the config.yml format

### 10.2 High 5xx Error Rate

**Symptom**: the HighHTTP5xxRate alert fires

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

**Common causes**:
- Database connection pool exhausted: increase the pool size or reduce slow queries
- External dependency (LLM, knowledge provider) unavailable: the knowledge source selection chain is direct per provider (pgvector/local) + ragflow→dify→weknora; check the circuit breaker state
- Configuration error after deployment: roll back or correct the configuration

### 10.3 AI Features Misbehaving

**Symptom**: the AIProviderDegraded alert fires or AI requests fail

```bash
# 检查 AI 指标
curl -s http://localhost:8080/metrics | grep ai_requests_total

# 检查 API Key 有效性
curl -s http://localhost:8080/metrics | grep ai_requests_total.*outcome

# 检查答复策略分布（primary=知识源命中 / fallback=降级 / transfer=转人工）
curl -s http://localhost:8080/metrics | grep ai_requests_total.*strategy

# 重置 circuit breaker
curl -X POST http://localhost:8080/api/v1/ai/circuit-breaker/reset \
  -H "Authorization: Bearer $TOKEN"
```

**Handling**:
- Rate limited: lower the request rate or upgrade the API tier
- Auth failed: rotate the API Key
- Timeout: increase the timeout configuration or enable the circuit breaker
- Temporary degradation: enable the fallback provider

### 10.4 Database Performance Degradation

**Symptom**: P99 latency rises or queries time out

```bash
# 检查连接池使用
curl -s http://localhost:8080/metrics | grep go_sql

# 检查活跃连接
psql -h localhost -U postgres -c "SELECT count(*) FROM pg_stat_activity"

# 检查慢查询
psql -h localhost -U postgres -c "SELECT query, mean_exec_time FROM pg_stat_statements ORDER BY mean_exec_time DESC LIMIT 10"
```

### 10.5 Event Processing Failures

**Symptom**: the EventBusHandlerFailures alert fires

```bash
# 检查失败事件类型
curl -s http://localhost:8080/metrics | grep eventbus_failed_total

# 查看死信队列
# 通过内部 API 或日志过滤 dead letter 事件
docker compose logs servify | grep "dead_letter"
```

**Handling**:
- Transient errors: events are automatically retried or enter the dead-letter queue
- Persistent errors: locate and fix the handler code, then redeploy
- Dead-letter replay currently has only an internal implementation (`internal/observability/async/replay.go`) with no HTTP interface exposed; replay requires an internal call or a new interface

---

## 11. Rollback Procedures

### 11.1 Docker Compose Rollback

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

### 11.2 Fly.io Rollback

```bash
# 查看部署历史
fly deployments list

# 回滚到上一个版本
fly deploy --detach --image servify:previous-version

# 或者直接回滚
fly rollback
```

### 11.3 Kubernetes Rollback

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

### 11.4 Database Rollback Notes

- Servify uses GORM AutoMigrate, which is **forward-compatible only** and does not support automatic rollback
- To roll back database changes, SQL must be written and executed manually
- Back up the database before a release:

```bash
pg_dump -h $DB_HOST -U postgres servify > backup_$(date +%Y%m%d_%H%M%S).sql
```

---

## 12. Scaling

### 12.1 Vertical Scaling

Adjust the resource allocation of a single instance:

| Resource | Minimum | Recommended | High Load |
|------|------|------|--------|
| CPU | 1 core | 2 cores | 4 cores |
| Memory | 1 GB | 2 GB | 4 GB |
| Database memory | 1 GB | 2 GB | 4 GB |

### 12.2 Scaling Boundaries

The current delivery boundary is **single-instance-first** (`docs/deployment.md` §5: `replicas: 1`; scaling demand is not met with multiple replicas — multiple replicas cause duplicate side effects or lost real-time push). Scaling happens within the single-instance boundary:

**Kubernetes:**

```bash
# 垂直扩容：调整资源规格
kubectl set resources deployment/servify --limits=cpu=4,memory=8Gi

# 滚动重启（单副本滚动替换，不中断）
kubectl rollout restart deployment/servify
```

**Fly.io:**

```bash
# 调整实例规格（不增加实例数）
fly scale vm shared-cpu-2x
```

Demands for multiple replicas / horizontal splitting must first satisfy the convergence conditions in `docs/multi-instance-boundary.md` (Redis+DB sharing, worker leases, cross-instance WebSocket routing, etc.); that document lists these as pending convergence items and they are not done now.

### 12.3 Database Scaling

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

## 13. Security Operations

### 13.1 Invalidating User Tokens

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

### 13.2 Audit Log Queries

```bash
# 查询审计日志（支持过滤）
curl "http://localhost:8080/api/audit/logs?action=create&resource_type=ticket&page=1&page_size=50" \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# 支持的过滤参数
# action         — 操作类型
# resource_type — 资源类型
# resource_id   — 资源 ID
# principal_kind — 操作主体类型
# actor_user_id  — 操作人用户 ID
# success        — 是否成功 (true/false)
# page           — 页码（默认 1）
# page_size      — 每页条数（默认 20）
```

### 13.3 Rate Limit Adjustments

Edit `config.yml` and restart:

```yaml
security:
  rate_limiting:
    enabled: true
    requests_per_minute: 300
    burst: 50
    # 路径级限流（每条路径需显式 enabled: true 才生效）
    paths:
      - enabled: true
        prefix: "/api/v1/ai/query"
        requests_per_minute: 30
        burst: 10
      - enabled: true
        prefix: "/public/"
        requests_per_minute: 120
        burst: 30
      - enabled: true
        prefix: "/public/kb/"
        requests_per_minute: 60
        burst: 15
      - enabled: true
        prefix: "/public/csat/"
        requests_per_minute: 30
        burst: 10
```

### 13.4 Security Baseline Check

```bash
# CLI 检查
go -C apps/server run ./cmd -c config.yml check-security-baseline --strict

# 脚本检查
sh ./scripts/check-security-baseline.sh config.yml

# Make 入口
make security-check CONFIG=config.yml
```

### 13.5 Session Risk Threshold Adjustments

`security.session_risk` controls the time windows and grading thresholds of the auth session risk heuristics. Recommendations:

- In production, tighten `hot_refresh_window_minutes` and `rapid_change_window_hours` first
- If the business often traverses shared egress or NAT gateways, adjust `multi_public_ip_threshold` cautiously
- To mark sessions as high risk earlier, lower `high_risk_score`
- If you want development / staging / production to use different baselines, prefer setting environment-level defaults via `server.environment + security.session_risk_profiles.{environment}`, then refine with tenant/workspace scoped config
- The current auth self-service `/api/v1/auth/sessions` already has regression tests verifying that the environment-level profile actually affects the `risk_level` output

Example:

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

### 13.6 Geo/IP Enrichment Provider Integration

`security.session_ip_intelligence` is used to wire external Geo/IP intelligence into the auth / management session risk view. Recommendations:

- Enable it only after privacy and compliance review is complete
- For `base_url`, use an internal gateway reachable from the server or a controlled third-party endpoint
- Prefer having the provider return `network_label` / `location_label` directly
- Keep the timeout short; on failure the system automatically falls back to local heuristic classification

Example:

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

## 14. Daily Operations Task List

### Daily

- [ ] Check Grafana Dashboard for anomalous metrics
- [ ] Check for unhandled Critical alerts
- [ ] Confirm the `/health` and `/ready` endpoints are normal

### Weekly

- [ ] Audit log review, watching for abnormal operation patterns
- [ ] Check disk space and database size
- [ ] Confirm backups are executing normally
- [ ] Review Worker and EventBus failure trends

### Monthly

- [ ] Secret rotation assessment (especially JWT_SECRET and API Keys)
- [ ] Security baseline check: `make security-check CONFIG=config.production.yml`
- [ ] Observability baseline check: `make observability-check CONFIG=config.production.yml`
- [ ] Capacity planning assessment (CPU, memory, database connection count trends)
- [ ] Update alert thresholds (based on actual traffic patterns)

### Before a Release

- [ ] Run `make release-check CONFIG=config.production.yml`
- [ ] Back up the database
- [ ] Prepare a rollback plan
- [ ] Notify the relevant teams
- [ ] Post-release verification: `/health`, `/ready`, key business flows

---

## Quick Command Reference

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
