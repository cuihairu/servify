# Servify 部署指南

本指南覆盖从本地开发到生产部署的完整流程。

---

## 目录

1. [架构概览](#1-架构概览)
2. [环境要求](#2-环境要求)
3. [本地开发部署](#3-本地开发部署)
4. [Docker Compose 部署](#4-docker-compose-部署)
5. [生产部署](#5-生产部署)
6. [可观测性配置](#6-可观测性配置)
7. [密钥与配置管理](#7-密钥与配置管理)
8. [健康检查与监控](#8-健康检查与监控)
9. [故障排查](#9-故障排查)

---

## 1. 架构概览

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

### 端口分配

| 服务 | 端口 | 说明 |
|------|------|------|
| Servify API | 8080 | HTTP + WebSocket |
| PostgreSQL | 5432 | 主数据库 |
| Redis | 6379 | 缓存/会话 |
| OTel Collector | 4317/4318 | OTLP gRPC/HTTP |
| Jaeger UI | 16686 | 分布式追踪面板 |
| WeKnora | 9000/9001 | 兼容知识库 provider / mock 验收服务（可选） |

---

## 2. 环境要求

### 最小配置

| 组件 | 版本 | 说明 |
|------|------|------|
| Go | 1.25.0 (toolchain go1.25.7) | 服务端编译 |
| Node.js | 24+ | SDK 构建、管理端、网站 |
| pnpm | 10+ | 管理端包管理器 |
| Docker | 24+ | 容器化部署（可选） |
| Docker Compose | v2+ | 编排（可选） |
| PostgreSQL | 12+ | 主数据库（生产必需） |
| SQLite | 3+ | 开发/测试回退选项 |
| Redis | 6+ | 缓存 |

### 推荐 production 配置

| 资源 | 最低 | 推荐 |
|------|------|------|
| CPU | 1 核 | 2 核 |
| 内存 | 1 GB | 2 GB |
| 磁盘 | 10 GB | 20 GB SSD |
| PostgreSQL | 1 GB 内存 | 2 GB 内存 |

---

## 3. 本地开发部署

### 3.1 克隆仓库

```bash
git clone https://github.com/timebeau/servify.git
cd servify
```

### 3.2 启动依赖服务

只需 PostgreSQL 和 Redis：

```bash
# 使用 Docker Compose 启动基础设施
docker compose -f infra/compose/docker-compose.yml up postgres redis -d
```

### 3.3 配置

```bash
# 使用默认配置（SQLite 开发模式）
cp config.yml config.local.yml

# 或使用 PostgreSQL
cp config.yml config.local.yml
# 编辑数据库连接和 AI 配置
# 必填项：
#   jwt.secret — JWT 签名密钥
#   ai.openai.api_key — OpenAI API Key
#   database.* — PostgreSQL 连接参数（使用 PostgreSQL 时）
```

**数据库选项：**

- **开发/测试**：默认配置支持 SQLite 回退（`DB_DRIVER=sqlite`），无需本地 PostgreSQL
- **生产**：必须使用 PostgreSQL

### 3.4 运行服务

```bash
# 方式一：Makefile
make run

# 方式二：Go 直接运行
go run ./apps/server/cmd/server

# 方式三：编译后运行
go build -o bin/servify ./apps/server/cmd/server
./bin/servify
```

### 3.5 验证

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

## 4. Docker Compose 部署

适用于开发、测试、小规模生产场景。

### 4.1 基础部署（API + DB + Redis）

```bash
# 构建并启动
docker compose -f infra/compose/docker-compose.yml up -d

# 查看日志
docker compose -f infra/compose/docker-compose.yml logs -f servify

# 停止
docker compose -f infra/compose/docker-compose.yml down
```

### 4.2 带 WeKnora mock 验收环境

```bash
# 启动全套（含 mock WeKnora 服务）
docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.weknora.yml \
  up -d
```

注意：

- 该 compose 文件当前构建的是 `infra/compose/weknora-mock/`。
- 它适用于本地协议回归和验收证据采集，不应被当作真实 WeKnora 生产部署模板。
- 若要执行 `WEKNORA_ACCEPTANCE_MODE=real`，请改连真实 WeKnora 地址。

需要额外环境变量：

```bash
# 知识库选择链 ragflow → dify → weknora → pgvector 直配：
# RAGFlow/WeKnora 为正统知识库双路径（RAGFlow 集成见 docs/RAGFLOW_INTEGRATION.md），
# Dify 为编排平台 dataset 兼容保留
export WEKNORA_ENABLED=true
export WEKNORA_API_KEY=your-weknora-api-key
export OPENAI_API_KEY=your-openai-api-key
```

### 4.3 带可观测性栈

```bash
# 启动 OTel Collector + Jaeger
docker compose -f infra/compose/docker-compose.observability.yml up -d
```

配置 Servify 连接 OTel（在 `config.yml` 中）：

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

访问 Jaeger UI：http://localhost:16686

### 4.4 全套本地验收部署（一键启动）

```bash
# API + DB + Redis + mock WeKnora + OTel + Jaeger
docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.weknora.yml \
  -f infra/compose/docker-compose.observability.yml \
  up -d
```

---

## 5. 生产部署

### 5.1 构建生产镜像

```bash
# 使用项目 Dockerfile（多阶段构建）
docker build -t servify:latest .

# 带版本标签
docker build -t servify:v0.1.0 -t servify:latest .
```

镜像特点：
- 基于 `golang:1.25-alpine` 编译，`alpine` 运行
- 静态编译（`CGO_ENABLED=0`），无外部依赖
- 镜像大小约 20-30 MB

### 5.2 Fly.io 部署

[Fly.io](https://fly.io) 提供免费额度，适合小规模生产。

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

`fly.toml` 配置示例：

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

### 5.3 Railway 部署

[Railway](https://railway.app) 提供免费 $5/月额度。

1. 连接 GitHub 仓库
2. 添加 PostgreSQL 插件
3. 添加 Redis 插件
4. 设置环境变量（见 [第 7 节](#7-密钥与配置管理)）
5. 自动部署

### 5.4 Kubernetes 部署

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

## 6. 可观测性配置

Servify 内置 Prometheus 指标、OpenTelemetry 追踪和结构化日志。

### 6.1 Prometheus 指标

Servify 在 `/metrics` 端点暴露 Prometheus 格式指标。

**Prometheus scrape 配置：**

```yaml
scrape_configs:
  - job_name: 'servify'
    scrape_interval: 15s
    static_configs:
      - targets: ['servify:8080']
    metrics_path: '/metrics'
```

**核心指标：**

| 指标 | 类型 | 说明 |
|------|------|------|
| `http_requests_total` | Counter | HTTP 请求总数（method, path, status_code） |
| `http_request_duration_seconds` | Histogram | 请求延迟 |
| `conversations_created_total` | Counter | 新建会话数 |
| `tickets_created_total` | Counter | 新建工单数 |
| `routing_decisions_total` | Counter | 路由决策数 |
| `ai_requests_total` | Counter | AI 请求量（provider, model, outcome） |
| `ai_request_duration_seconds` | Histogram | AI 请求延迟 |
| `ai_llm_tokens_total` | Counter | Token 消耗量 |
| `eventbus_published_total` | Counter | 事件发布量 |
| `eventbus_failed_total` | Counter | 事件处理失败数 |
| `errors_total` | Counter | 分类错误数（severity, category, module） |
| `worker_jobs_total` | Counter | Worker job 轮次数（outcome 含单轮业务失败） |
| `worker_job_duration_seconds` | Histogram | 单轮 job 执行时长 |

### 6.2 Grafana Cloud 接入（推荐免费方案）

1. 注册 [Grafana Cloud](https://grafana.com/auth/sign-up/)
2. 获取 Prometheus remote_write 端点和凭证
3. 配置 Prometheus remote_write：

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

4. 导入 Dashboard：
   - Grafana → Dashboards → Import → 上传 `deploy/observability/dashboards/servify-service.json`
   - 同理导入 `servify-business.json`

5. 导入告警规则：
   - 将 `deploy/observability/alerts/rules.yaml` 内容添加到 Prometheus 配置

### 6.3 OpenTelemetry 追踪

在 `config.yml` 中启用：

```yaml
monitoring:
  tracing:
    enabled: true
    endpoint: "http://otel-collector:4317"
    insecure: true
    sample_ratio: 0.1
    service_name: "servify"
```

### 6.4 结构化日志

日志输出 JSON 格式，包含 `request_id`、`tenant_id`、`trace_id` 等关联字段：

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

## 7. 密钥与配置管理

### 7.1 必需密钥

| 密钥 | 说明 | 示例 |
|------|------|------|
| `JWT_SECRET` | JWT 签名密钥 | `openssl rand -hex 32` |
| `OPENAI_API_KEY` | OpenAI API Key | `sk-proj-xxx` |
| `DB_PASSWORD` | PostgreSQL 密码 | 强密码 |

### 7.2 可选密钥

| 密钥 | 说明 |
|------|------|
| `DIFY_API_KEY` | Dify API Key（推荐主知识源） |
| `DIFY_DATASET_ID` | Dify dataset ID |
| `WEKNORA_API_KEY` | WeKnora compatibility 知识库 API Key |
| `WEKNORA_TENANT_ID` | WeKnora compatibility 租户 ID |
| `REDIS_PASSWORD` | Redis 密码（生产建议启用） |

### 7.3 生产配置安全清单

使用 `config.production.secure.example.yml` 作为生产基线；使用 `config.staging.example.yml` 作为 staging 演练基线。

- [ ] `jwt.secret` 使用环境变量引用 `"${SERVIFY_JWT_SECRET}"`
- [ ] `security.cors.allowed_origins` 限制为实际域名，不用 `*`
- [ ] `security.rbac.enabled` 设为 `true`
- [ ] `security.rate_limiting.enabled` 设为 `true`
- [ ] `log.level` 设为 `info`（不要用 `debug`）
- [ ] `log.format` 设为 `json`
- [ ] `ai.openai.api_key` 使用环境变量引用

在首次部署或调整安全配置后，先执行严格校验：

```bash
make security-check CONFIG=config.yml
```

该检查会对默认 JWT secret、开放 CORS、关闭限流、匿名入口缺少独立路径级限流、空 provider key 等风险返回非零退出码。

若本次部署同时涉及 metrics、tracing 或 observability stack，也建议执行：

```bash
make observability-check CONFIG=config.yml
```

该检查会校验 `monitoring.metrics_path`、tracing 基本参数，以及 dashboard / alert / runbook / OTel collector 配置资产是否存在。

如果要在发版前一次性跑完当前最小自检，可直接执行：

```bash
make release-check CONFIG=config.yml
```

该入口会串行执行 `local-check`、`security-check`、`observability-check` 和聚焦的 Go 回归测试。

### 7.4 Docker Secrets 方式

```bash
# 创建密钥文件
echo "your-strong-jwt-secret" > ./secrets/jwt_secret
echo "sk-your-openai-key" > ./secrets/openai_api_key

# docker-compose.yml 中引用
services:
  servify:
    environment:
      - JWT_SECRET_FILE=/run/secrets/jwt_secret
    secrets:
      - jwt_secret

secrets:
  jwt_secret:
    file: ./secrets/jwt_secret
```

### 7.5 Fly.io / Railway 方式

```bash
# Fly.io
fly secrets set JWT_SECRET=xxx OPENAI_API_KEY=sk-xxx

# Railway — 在 Dashboard 中添加环境变量
```

---

## 8. 健康检查与监控

### 8.1 端点

| 端点 | 用途 | 检查内容 |
|------|------|---------|
| `GET /health` | 存活探针 | 进程存活 |
| `GET /ready` | 就绪探针 | DB + Redis + 可选 LLM / knowledge provider |
| `GET /metrics` | Prometheus | 全量指标 |

### 8.2 Docker Compose 健康检查

已内置在 `docker-compose.yml` 中：

```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
  interval: 30s
  timeout: 10s
  retries: 5
  start_period: 40s
```

### 8.3 告警规则

预定义 10 条告警规则在 `deploy/observability/alerts/rules.yaml`：

| 告警 | 级别 | 触发条件 |
|------|------|---------|
| HighHTTP5xxRate | Critical | 5xx 比例 > 5% 持续 5 分钟 |
| HighP99Latency | Warning | P99 延迟 > 5s 持续 10 分钟 |
| HighRateLimitDrops | Info | 限流丢弃速率 > 10/s 持续 5 分钟 |
| HighGoroutineCount | Warning | goroutines > 10000 持续 10 分钟 |
| HighSystemErrorRate | Critical | 系统错误 > 0.01/s 持续 5 分钟 |
| HighDependencyErrorRate | Warning | 依赖错误 > 0.1/s 持续 5 分钟 |
| EventBusHandlerFailures | Warning | 事件处理失败持续 5 分钟 |
| EventBusDeadLetters | Info | dead letter 持续 10 分钟 |
| AIProviderDegraded | Critical | AI 失败率 > 20% 持续 5 分钟 |
| AIHighLatency | Warning | AI P95 延迟 > 10s 持续 10 分钟 |
| WorkerJobFailures | Warning | Worker 失败持续 10 分钟 |

生产建议直接从 `config.production.secure.example.yml` 启动，并显式保留：

- `monitoring.metrics_path: "/metrics"`
- `monitoring.tracing.enabled: true`
- `monitoring.tracing.endpoint: "http://otel-collector:4317"`
- `monitoring.tracing.sample_ratio: 0.1`
- `monitoring.tracing.service_name: "servify"`

---

## 9. 故障排查

### 常见问题

**服务无法启动**

```bash
# 检查端口占用
lsof -i :8080

# 检查数据库连接
psql -h localhost -U postgres -d servify -c "SELECT 1"

# 检查 Redis 连接
redis-cli ping
```

**数据库迁移问题**

Servify 使用 GORM AutoMigrate，首次启动会自动建表。如需手动初始化：

```bash
# Docker 环境
docker compose exec postgres psql -U postgres -d servify -f /docker-entrypoint-initdb.d/01-init.sql
```

**查看日志**

```bash
# Docker Compose
docker compose logs -f servify

# 本地运行 — 日志在 ./logs/servify.log
tail -f logs/servify.log
```

**可观测性不工作**

```bash
# 检查 /metrics 端点
curl -s http://localhost:8080/metrics | head -20

# 检查 tracing 配置
grep -A 5 "tracing:" config.yml

# 检查 OTel Collector
docker compose -f infra/compose/docker-compose.observability.yml logs otel-collector
```

### 运维手册

详细的告警排查步骤见：[`deploy/observability/runbook/operational-runbook.md`](https://github.com/timebeau/servify/blob/main/deploy/observability/runbook/operational-runbook.md)

---

## 快速参考

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
