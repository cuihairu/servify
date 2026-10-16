# Servify Deployment Guide

This guide covers the complete flow from local development to production deployment.

---

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [Environment Requirements](#2-environment-requirements)
3. [Local Development Deployment](#3-local-development-deployment)
4. [Docker Compose Deployment](#4-docker-compose-deployment)
5. [Production Deployment](#5-production-deployment)
6. [Observability Configuration](#6-observability-configuration)
7. [Secrets and Configuration Management](#7-secrets-and-configuration-management)
8. [Health Checks and Monitoring](#8-health-checks-and-monitoring)
9. [Troubleshooting](#9-troubleshooting)

---

## 1. Architecture Overview

```text
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Web SDK    │     │  Admin UI    │     │  API Client  │
└──────┬───────┘     └──────┬───────┘     └──────┬───────┘
       │                    │                    │
       └────────────────────┼────────────────────┘
                            │ HTTPS
                     ┌──────▼───────┐
                     │  Servify API │  :8080
                     │  (Gin + Go)  │
                     └──┬───────┬───┘
                        │       │
              ┌─────────▼──┐ ┌──▼──────────┐
              │ PostgreSQL  │ │    Redis     │
              │ (pgvector)  │ │              │
              └─────────────┘ └─────────────┘
                        │
              ┌─────────▼──────────┐
              │ OpenTelemetry      │
              │ Collector → Jaeger │
              │ Prometheus /metrics│
              └────────────────────┘
```

### Port Allocation

| Service | Port | Description |
|------|------|------|
| Servify API | 8080 | HTTP + WebSocket |
| PostgreSQL | 5432 | Primary database |
| Redis | 6379 | Cache/session |
| OTel Collector | 4317/4318 | OTLP gRPC/HTTP |
| Jaeger UI | 16686 | Distributed tracing panel |
| WeKnora | 9000/9001 | Compatible knowledge base provider / mock acceptance service (optional) |

---

## 2. Environment Requirements

### Minimum Configuration

| Component | Version | Description |
|------|------|------|
| Go | 1.26.0 | Server-side compilation |
| Node.js | 24+ | SDK build, admin console, website |
| pnpm | 10+ | Admin console package manager |
| Docker | 24+ | Containerized deployment (optional) |
| Docker Compose | v2+ | Orchestration (optional) |
| PostgreSQL | 12+ | Primary database (required for production) |
| SQLite | 3+ | Development/test fallback option |
| Redis | 6+ | Cache |

### Recommended Production Configuration

| Resource | Minimum | Recommended |
|------|------|------|
| CPU | 1 core | 2 cores |
| Memory | 1 GB | 2 GB |
| Disk | 10 GB | 20 GB SSD |
| PostgreSQL | 1 GB memory | 2 GB memory |

---

## 3. Local Development Deployment

### 3.1 Clone the Repository

```bash
git clone https://github.com/cuihairu/servify.git
cd servify
```

### 3.2 Start Dependency Services

Only PostgreSQL and Redis are needed:

```bash
# 使用 Docker Compose 启动基础设施
docker compose -f infra/compose/docker-compose.yml up postgres redis -d
```

### 3.3 Configuration

```bash
# 使用默认配置（SQLite 开发模式）
cp config.yml config.local.yml

# 或使用 PostgreSQL
cp config.yml config.local.yml
# 编辑数据库连接和 AI 配置
# 必填项：
#   jwt.secret — JWT 签名密钥
#   ai.openai.api_key — OpenAI API Key
#   ai.provider / ai.anthropic.api_key — 切换 Anthropic 时配置（provider: "anthropic"）
#   database.* — PostgreSQL 连接参数（使用 PostgreSQL 时）
```

**Database options:**

- **Development/test**: PostgreSQL is used by default; for development/test scenarios without a local PostgreSQL, SQLite can be enabled explicitly via `DB_DRIVER=sqlite`
- **Production**: PostgreSQL must be used

### 3.4 Run the Service

```bash
# 方式一：Makefile
make run

# 方式二：Go 直接运行
go run ./apps/server/cmd/server

# 方式三：编译后运行
go build -o bin/servify ./apps/server/cmd/server
./bin/servify
```

### 3.5 Verification

```bash
# 健康检查
curl http://localhost:8080/health

# 就绪检查（含依赖检测）
curl http://localhost:8080/ready

# Prometheus 指标
curl http://localhost:8080/metrics

# 发布前自检（默认配置使用临时 SQLite）
make release-check CONFIG=./config.yml
```

---

## 4. Docker Compose Deployment

Suitable for development, testing, and small-scale production scenarios.

### 4.1 Base Deployment (API + DB + Redis)

```bash
# 构建并启动
docker compose -f infra/compose/docker-compose.yml up -d

# 查看日志
docker compose -f infra/compose/docker-compose.yml logs -f servify

# 停止
docker compose -f infra/compose/docker-compose.yml down
```

### 4.2 With WeKnora Mock Acceptance Environment

```bash
# 启动全套（含 mock WeKnora 服务）
docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.weknora.yml \
  up -d
```

Notes:

- This compose file currently builds `infra/compose/weknora-mock/`.
- It is intended for local protocol regression and acceptance evidence collection; it should not be treated as a real WeKnora production deployment template.
- To run `WEKNORA_ACCEPTANCE_MODE=real`, point it at a real WeKnora address instead.

Additional environment variables required:

```bash
# 知识库选择链 ragflow → dify → weknora → pgvector 直配：
# RAGFlow/WeKnora 为正统知识库双路径（RAGFlow 集成见 docs/RAGFLOW_INTEGRATION.md），
# Dify 为编排平台 dataset 兼容保留
export WEKNORA_ENABLED=true
export WEKNORA_API_KEY=your-weknora-api-key
export OPENAI_API_KEY=your-openai-api-key
```

#### Default Knowledge Base Convention for New Customer Delivery (signed off 2026-09-21)

Knowledge base capability is layered by delivery cadence, not an either/or single choice:

1. **Default (zero external configuration): pgvector direct**. The selection chain falls back to pgvector, so the full AI first-answer path works in the first week of delivery, without blocking on external knowledge base deployment; the delivery failure rate is the lowest. Retrieval quality depends on self-built chunking, suitable for the starting stage with small document volume and simple structure.
2. **Enhanced (when the customer has knowledge base operation needs)**: for lightweight Chinese-ecosystem scenarios, connect **WeKnora** (lightweight deployment, good Chinese fusion); for heavy-parsing scenarios such as complex PDFs/contracts/tables, connect **RAGFlow** (DeepDoc parsing is the deepest, resource usage is higher). Configuration and acceptance for each are in their respective integration documents; once enabled, they take effect in the selection chain in the priority order ragflow → dify → weknora.

### 4.3 With Observability Stack

```bash
# 启动 OTel Collector + Jaeger
docker compose -f infra/compose/docker-compose.observability.yml up -d
```

Configure Servify to connect to OTel (see the `monitoring` section in `config.production.secure.example.yml`; the default `config.yml` does not include this section):

```yaml
monitoring:
  enabled: true
  tracing:
    enabled: true
    endpoint: "http://localhost:4317"
    insecure: true
    sample_ratio: 0.1
    service_name: "servify"
  metrics_path: "/metrics"
```

Access the Jaeger UI: http://localhost:16686

### 4.4 Full Local Acceptance Deployment (One-Shot Start)

```bash
# API + DB + Redis + mock WeKnora + OTel + Jaeger
docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.weknora.yml \
  -f infra/compose/docker-compose.observability.yml \
  up -d
```

---

## 5. Production Deployment

### 5.1 Build the Production Image

```bash
# 使用项目 Dockerfile（多阶段构建）
docker build -t servify:latest .

# 带版本标签
docker build -t servify:v0.1.0 -t servify:latest .
```

Image characteristics:
- Compiled on `golang:1.26-alpine`, running on `alpine`
- Statically compiled (`CGO_ENABLED=0`), no external dependencies
- Image size approximately 20-30 MB

### 5.2 Fly.io Deployment

[Fly.io](https://fly.io) offers a free allowance, suitable for small-scale production.

```bash
# 安装 CLI
curl -L https://fly.io/install.sh | sh

# 登录
fly auth login

# 初始化（首次）
fly launch --dockerfile Dockerfile --name servify --region hkg

# 添加 PostgreSQL
fly postgres create
fly postgres attach <db-name>

# 设置密钥
fly secrets set JWT_SECRET=$(openssl rand -hex 32)
fly secrets set OPENAI_API_KEY=sk-xxx

# 部署
fly deploy

# 查看状态
fly status
fly logs
```

`fly.toml` configuration example:

```toml
app = "servify"
primary_region = "hkg"

[build]
  dockerfile = "Dockerfile"

[http_service]
  internal_port = 8080
  force_https = true
  auto_stop_machines = "stop"
  auto_start_machines = true
  min_machines_running = 0

[checks]
  [checks.health]
    grace_period = "30s"
    interval = "15s"
    method = "get"
    path = "/health"
    timeout = "5s"
```

### 5.3 Railway Deployment

[Railway](https://railway.app) offers a free $5/month allowance.

1. Connect the GitHub repository
2. Add the PostgreSQL plugin
3. Add the Redis plugin
4. Set environment variables (see [Section 7](#7-secrets-and-configuration-management))
5. Automatic deployment

### 5.4 Kubernetes Deployment

#### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: servify
  labels:
    app: servify
spec:
  # Servify 当前为单实例优先交付（见 docs/multi-instance-boundary.md）：
  # WebSocket/WebRTC 实时路由、进程内限流、webhook/automation worker
  # 认领都是单实例约束面，多副本会产生重复副作用或实时推送丢失。
  # 高可用通过进程级重启恢复（worker 状态在 DB、事件在 Redis stream、
  # agent presence TTL 自动收敛）+ 滚动重启满足，不通过多副本满足。
  replicas: 1
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 0
  selector:
    matchLabels:
      app: servify
  template:
    metadata:
      labels:
        app: servify
    spec:
      containers:
        - name: servify
          image: servify:latest
          ports:
            - containerPort: 8080
          env:
            - name: DB_HOST
              value: "postgres-service"
            - name: DB_PORT
              value: "5432"
            - name: REDIS_HOST
              value: "redis-service"
            - name: REDIS_PORT
              value: "6379"
          envFrom:
            - secretRef:
                name: servify-secrets
          readinessProbe:
            httpGet:
              path: /ready
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 15
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 30
            periodSeconds: 30
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 512Mi
```

#### Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: servify-service
spec:
  selector:
    app: servify
  ports:
    - port: 80
      targetPort: 8080
  type: ClusterIP
```

---

## 6. Observability Configuration

Servify has built-in Prometheus metrics, OpenTelemetry tracing, and structured logging.

### 6.1 Prometheus Metrics

Servify exposes Prometheus-format metrics at the `/metrics` endpoint.

**Prometheus scrape configuration:**

```yaml
scrape_configs:
  - job_name: 'servify'
    scrape_interval: 15s
    static_configs:
      - targets: ['servify:8080']
    metrics_path: '/metrics'
```

**Core metrics:**

| Metric | Type | Description |
|------|------|------|
| `http_requests_total` | Counter | Total HTTP requests (method, path, status_code) |
| `http_request_duration_seconds` | Histogram | Request latency |
| `conversations_created_total` | Counter | Conversations created |
| `tickets_created_total` | Counter | Tickets created |
| `routing_decisions_total` | Counter | Routing decisions |
| `ai_requests_total` | Counter | AI request volume (provider, model, outcome) |
| `ai_request_duration_seconds` | Histogram | AI request latency |
| `ai_llm_tokens_total` | Counter | Token consumption |
| `eventbus_published_total` | Counter | Events published |
| `eventbus_failed_total` | Counter | Event processing failures |
| `errors_total` | Counter | Classified error count (severity, category, module) |
| `worker_jobs_total` | Counter | Worker job rounds (outcome includes per-round business failures) |
| `worker_job_duration_seconds` | Histogram | Per-round job execution duration |

### 6.2 Grafana Cloud Integration (Recommended Free Option)

1. Sign up for [Grafana Cloud](https://grafana.com/auth/sign-up/)
2. Obtain the Prometheus remote_write endpoint and credentials
3. Configure Prometheus remote_write:

```yaml
# prometheus.yml
global:
  scrape_interval: 15s

remote_write:
  - url: "https://<your-grafana-id>.grafana.net/api/prom/push"
    basic_auth:
      username: "<user-id>"
      password: "<api-key>"

scrape_configs:
  - job_name: 'servify'
    static_configs:
      - targets: ['servify:8080']
```

4. Import dashboards:
   - Grafana → Dashboards → Import → upload `deploy/observability/dashboards/servify-service.json`
   - Import `servify-business.json` the same way

5. Import alert rules:
   - Add the contents of `deploy/observability/alerts/rules.yaml` to the Prometheus configuration

### 6.3 OpenTelemetry Tracing

Enable it in `config.production.secure.example.yml` (the default `config.yml` does not include the `monitoring` section):

```yaml
monitoring:
  tracing:
    enabled: true
    endpoint: "http://otel-collector:4317"
    insecure: true
    sample_ratio: 0.1
    service_name: "servify"
```

### 6.4 Structured Logging

Logs are output in JSON format, containing correlation fields such as `request_id`, `tenant_id`, and `trace_id`:

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

---

## 7. Secrets and Configuration Management

### 7.1 Required Secrets

| Secret | Description | Example |
|------|------|------|
| `JWT_SECRET` | JWT signing secret | `openssl rand -hex 32` |
| `OPENAI_API_KEY` | OpenAI API Key | `sk-proj-xxx` |
| `DB_PASSWORD` | PostgreSQL password | strong password |

### 7.2 Optional Secrets

| Secret | Description |
|------|------|
| `DIFY_API_KEY` | Dify API Key (Dify dataset compatibility retained; its selection-chain position is per the §4.2 signed-off convention) |
| `DIFY_DATASET_ID` | Dify dataset ID |
| `WEKNORA_API_KEY` | WeKnora compatibility knowledge base API Key |
| `WEKNORA_TENANT_ID` | WeKnora compatibility tenant ID |
| `REDIS_PASSWORD` | Redis password (recommended for production; only the config file `redis.password` field, no environment variable binding provided) |

### 7.3 Production Configuration Security Checklist

Use `config.production.secure.example.yml` as the production baseline; use `config.staging.example.yml` as the staging drill baseline.

- [ ] `jwt.secret` uses an environment variable reference `"${SERVIFY_JWT_SECRET}"`
- [ ] `security.cors.allowed_origins` is restricted to actual domains, not `*`
- [ ] `security.rbac.enabled` is set to `true`
- [ ] `security.rate_limiting.enabled` is set to `true`
- [ ] `log.level` is set to `info` (do not use `debug`)
- [ ] `log.format` is set to `json`
- [ ] `ai.openai.api_key` uses an environment variable reference (same when switching to Anthropic: `ai.provider: "anthropic"` + `ai.anthropic.api_key`)

After the first deployment or after adjusting security configuration, run the strict validation first:

```bash
make security-check CONFIG=config.yml
```

This check returns a non-zero exit code for risks such as the default JWT secret, open CORS, disabled rate limiting, anonymous entry points lacking independent path-level rate limiting, and empty provider keys.

If this deployment also involves metrics, tracing, or the observability stack, it is also recommended to run:

```bash
make observability-check CONFIG=config.yml
```

This check validates `monitoring.metrics_path`, basic tracing parameters, and whether the dashboard / alert / runbook / OTel collector configuration assets exist.

To run the current minimal self-check in one shot before a release, execute:

```bash
make release-check CONFIG=config.yml
```

This entry point runs `local-check`, `security-check`, `observability-check`, the focused Go regression tests, and build verification (`make build`) serially.

### 7.4 Docker Secrets Approach

The server does **not** resolve `*_FILE`-style environment variables (such as `JWT_SECRET_FILE`); it only reads regular environment variables. When using Docker Secrets, the container entrypoint must export the secret content as a regular environment variable itself:

```yaml
# docker-compose.yml 中引用
services:
  servify:
    entrypoint: ["/bin/sh", "-c", "export JWT_SECRET=$(cat /run/secrets/jwt_secret) && /app/servify"]
    secrets:
      - jwt_secret

secrets:
  jwt_secret:
    file: ./secrets/jwt_secret
```

### 7.5 Fly.io / Railway Approach

```bash
# Fly.io
fly secrets set JWT_SECRET=xxx OPENAI_API_KEY=sk-xxx

# Railway — 在 Dashboard 中添加环境变量
```

---

## 8. Health Checks and Monitoring

### 8.1 Endpoints

| Endpoint | Purpose | What It Checks |
|------|------|---------|
| `GET /health` | Liveness probe | Process is alive |
| `GET /ready` | Readiness probe | DB + Redis + optional LLM / knowledge provider |
| `GET /metrics` | Prometheus | Full metrics |

### 8.2 Docker Compose Health Check

Already built into `docker-compose.yml`:

```yaml
healthcheck:
  test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://localhost:8080/health"]
  interval: 30s
  timeout: 10s
  retries: 5
  start_period: 40s
```

### 8.3 Alert Rules

14 predefined alert rules live in `deploy/observability/alerts/rules.yaml` (that file is the single source of truth):

| Alert | Level | Trigger Condition |
|------|------|---------|
| HighHTTP5xxRate | Critical | 5xx ratio > 5% for 5 minutes |
| HighP99Latency | Warning | P99 latency > 5s for 10 minutes |
| HighRateLimitDrops | Info | rate-limit drop rate > 10/s for 5 minutes |
| HighGoroutineCount | Warning | goroutines > 10000 for 10 minutes |
| EventBusHandlerFailures | Warning | event processing failures for 5 minutes |
| EventBusDeadLetters | Info | dead letters for 10 minutes |
| WorkerJobFailures | Warning | Worker failures for 10 minutes |
| AIProviderDegraded | Critical | AI failure rate > 20% for 5 minutes |
| AIHighLatency | Warning | AI P95 latency > 10s for 10 minutes |
| AIFallbackRatioHigh | Warning | AI fallback ratio > 50% |
| VoiceTranslationDegradedHigh | Warning | voice translation degradation ratio > 20% |
| SLOAvailabilityFastBurn | Critical | availability SLO fast burn (14.4x, burning about 2% of the 30-day budget in 1 hour) |
| SLOAvailabilitySlowBurn | Warning | availability SLO slow burn (6x, burning about 5% of the 30-day budget in 6 hours) |
| SLOLatencyFastBurn | Critical | latency SLO fast burn (14.4x, burning about 2% of the 30-day budget in 1 hour) |

For production, start directly from `config.production.secure.example.yml` and explicitly keep:

- `monitoring.metrics_path: "/metrics"`
- `monitoring.tracing.enabled: true`
- `monitoring.tracing.endpoint: "http://otel-collector:4317"`
- `monitoring.tracing.sample_ratio: 0.1`
- `monitoring.tracing.service_name: "servify"`

---

## 9. Troubleshooting

### Common Problems

**Service fails to start**

```bash
# 检查端口占用
lsof -i :8080

# 检查数据库连接
psql -h localhost -U postgres -d servify -c "SELECT 1"

# 检查 Redis 连接
redis-cli ping
```

**Database migration problems**

Servify runs GORM AutoMigrate table creation automatically on first startup; versioned migration files live in `apps/server/internal/app/bootstrap/migrations/`. To execute them manually:

```bash
# 标准入口（见 Makefile:106）
make migrate
```

**Viewing logs**

```bash
# Docker Compose
docker compose logs -f servify

# 本地运行 — 日志在 ./logs/servify.log
tail -f logs/servify.log
```

**Observability not working**

```bash
# 检查 /metrics 端点
curl -s http://localhost:8080/metrics | head -20

# 检查 tracing 配置
grep -A 5 "tracing:" config.yml

# 检查 OTel Collector
docker compose -f infra/compose/docker-compose.observability.yml logs otel-collector
```

### Operations Manual

For detailed alert triage steps, see: [`deploy/observability/runbook/operational-runbook.md`](https://github.com/cuihairu/servify/blob/main/deploy/observability/runbook/operational-runbook.md)

---

## Quick Reference

```bash
# 本地开发
make run

# Docker 全套
docker compose -f infra/compose/docker-compose.yml up -d

# Docker + WeKnora
docker compose -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.weknora.yml up -d

# Docker + 可观测性
docker compose -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.observability.yml up -d

# 生产构建
docker build -t servify:latest .

# 健康检查
curl http://localhost:8080/health
curl http://localhost:8080/ready
curl http://localhost:8080/metrics
```
