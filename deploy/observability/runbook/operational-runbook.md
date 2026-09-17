# Servify Operational Runbook

## Observability Architecture

Servify exposes metrics at `/metrics` in Prometheus format. The stack:

- **Prometheus** scrapes `/metrics` (default every 15s)
- **Jaeger** receives traces via OTel Collector (OTLP gRPC on :4317)
- **Grafana** queries Prometheus for dashboards and alerts

一致性门禁：`apps/server/internal/observability/metrics/consistency_test.go`
保证本文件的告警段落、`deploy/observability/alerts/rules.yaml`、两个
Grafana dashboard 三者引用的指标与告警名完全一致；未接线指标登记在
`deploy/observability/known-gaps.md`，禁止出现在任何 PromQL 表达式中。

### Metric Naming

All metrics follow Prometheus conventions: `subsystem_name_units`. Key prefixes:

| Prefix | Domain |
|--------|--------|
| `http_` | HTTP request metrics |
| `ai_` | AI/LLM interactions |
| `ratelimit_` | Rate-limited (429) requests |
| `eventbus_` | Event bus processing |
| `worker_` | Background job processing（`worker_job_duration_seconds` 见 known-gaps.md） |
| `errors_` | Classified errors（HTTP 5xx 统一出口打点） |

## Alert Runbooks

### HighHTTP5xxRate

**Severity**: Critical | **Threshold**: >5% 5xx rate for 5 minutes

**Investigation**:
1. Check Grafana "Servify Service" dashboard: which `path` labels drive the 5xx
2. Review application logs filtered by `request_id` from the dashboard
3. Check if a recent deployment coincides with the error spike

**Common causes**:
- Database connection pool exhaustion
- External dependency (LLM, WeKnora) outage
- Configuration error after deployment

**Resolution**:
- Database: check `go_sql_open_connections` metric, increase pool if needed
- External deps: check circuit breaker state, enable fallback mode
- Config: verify environment variables and config.yml

### HighP99Latency

**Severity**: Warning | **Threshold**: P99 HTTP latency > 5s for 10 minutes

**Investigation**:
1. Check "HTTP Request Latency" panel (P50/P95/P99) on the service dashboard
2. Correlate with "HTTP Request Rate" — a traffic spike is the usual trigger
3. Check `ai_request_duration_seconds` to rule out slow AI dependencies

**Resolution**:
- Traffic spike: raise rate limits or scale out
- Slow dependency: check downstream latency, tighten timeouts
- Persistent single-path latency: profile the handler for that `path`

### HighRateLimitDrops

**Severity**: Info | **Threshold**: >10 rate-limited requests/s for 5 minutes

**Investigation**:
1. Check "Rate Limit Drops" panel, group by `path` to find the offending route
2. Determine whether traffic is legitimate growth or abuse (per-IP patterns)

**Resolution**:
- Legitimate growth: raise `security.rate_limiting.requests_per_minute`
- Abuse: block at the reverse proxy / WAF before it reaches the app
- Per-path tuning: add an entry under `security.rate_limiting.paths`

### HighGoroutineCount

**Severity**: Warning | **Threshold**: goroutines > 10000 for 10 minutes

**Investigation**:
1. Check "Go Runtime" panel for a monotonic climb vs. a step change
2. Correlate with connection count and worker restarts
3. Capture `curl http://<host>/debug/pprof/goroutine?debug=1` for a snapshot

**Resolution**:
- Leak in a handler: fix the missing cancel/defer, redeploy
- Load-driven: scale horizontally

### EventBusHandlerFailures

**Severity**: Warning | **Threshold**: any handler failures for 5 minutes

**Investigation**:
1. Check "Event Bus Throughput" panel: `eventbus_failed_total` by `event_type`
2. Check dead letter entries via the in-memory recorder
3. Review handler code for the failing event type

**Resolution**:
- Transient errors: events will be retried or dead-lettered
- Persistent errors: fix handler code and redeploy
- Use replay interface to reprocess dead-lettered events

### EventBusDeadLetters

**Severity**: Info | **Threshold**: dead-lettered events for 10 minutes

**Investigation**:
1. Check `eventbus_dead_letter_total` by `event_type`
2. List dead letter entries (in-memory recorder, up to 1000) to see the error text
3. Correlate with `eventbus_failed_total` — dead letters are the persisted tail of failures

**Resolution**:
- Fix the underlying handler error, then replay dead-lettered events
- If a poison event loops, disable the subscriber and quarantine it

### WorkerJobFailures

**Severity**: Warning | **Threshold**: any worker start failures for 10 minutes

**Investigation**:
1. Check "Worker Jobs" panel: `worker_jobs_total` by `worker_name` and `outcome`
2. Check `worker_active_jobs` — a gauge stuck at 0 means the worker never came up
3. Review worker-specific logs

### AIProviderDegraded

**Severity**: Critical | **Threshold**: >20% failure rate for 5 minutes

**Investigation**:
1. Check `ai_requests_total` by `provider` and `outcome`
2. Check `ai_request_duration_seconds` for latency spikes
3. Verify API key validity and rate limit status

**Resolution**:
- Rate limited: reduce request rate or increase tier
- Auth failed: rotate API keys
- Timeout: increase timeout config or enable circuit breaker
- Enable fallback to alternative provider

### AIHighLatency

**Severity**: Warning | **Threshold**: P95 latency > 10s for 10 minutes

**Investigation**:
1. Check `ai_request_duration_seconds` by `provider`
2. Compare latency increase with token volume spikes in `ai_llm_tokens_total`
3. Check whether the affected provider is also approaching failure alerts

**Resolution**:
- Reduce concurrent AI load or expensive prompt paths
- Switch to fallback mode when latency makes the product flow unusable
- Validate provider-side latency before increasing timeouts

### AIFallbackRatioHigh

**Severity**: Warning | **Threshold**: fallback share > 50% for 10 minutes (transfers excluded)

**Investigation**:
1. Check "AI Strategy Breakdown" / "AI Fallback Ratio" panels on the business dashboard
2. fallback 占比高但 failure 低：知识源无命中（内容覆盖不足）或编排层
   持续报错被静默降级——看应用日志里 "served by fallback" 的 error 字段
3. Check circuit breaker state in `GET /api/v1/ai/status`

**Resolution**:
- 知识覆盖不足：补充知识文档，观察 primary 占比回升
- 编排层持续失败：按日志 error 修复（常见为 WeKnora/Dify 凭证或网络）
- 熔断开启：等半开恢复或 `POST /api/v1/ai/circuit-breaker/reset`

### SLOAvailabilityFastBurn

**Severity**: Critical | **Threshold**: 5xx 错误率 > 0.144%（14.4x 预算燃烧率），1h 与 5m 双窗同时成立

含义：按此速度 1 小时烧掉约 2% 的 30 天可用性预算（99.9% SLO，见
`deploy/observability/slo.md`）。这是 page 级告警，需要立即响应。

**Investigation**:
1. 先看症状告警（HighHTTP5xxRate）是否同时触发——触发则按其 runbook 处理现场
2. Service dashboard "SLO Error Budget" 面板确认预算消耗趋势；
   "HTTP Request Rate" 面板按 status_code 拆分定位 5xx 来源
3. 若症状告警未触发（错误率 0.15%~5% 之间）：查 `errors_total` 按
   severity/error_category 拆分——dependency 类看外部依赖健康，
   system 类看应用日志（按 request_id）

**Resolution**:
- 外部依赖降级：启用对应 fallback（AI 链路见 AIFallbackRatioHigh）
- 应用缺陷：考虑回滚最近发布，止血优先于根因
- 无法立即修复：接受预算烧穿，但停止非关键发布直到预算回正

### SLOAvailabilitySlowBurn

**Severity**: Warning | **Threshold**: 5xx 错误率 > 0.06%（6x），6h 与 30m 双窗同时成立

含义：6 小时烧掉约 5% 的 30 天预算。票务级告警，工作时间处理即可。

**Investigation**:
1. "SLO Error Budget" 面板看 30d 预算剩余——剩余 < 50% 时提高处理优先级
2. 对比 6h 窗内是否有间歇性 5xx（低水平持续漏损）：按 path 拆分
   `http_requests_total{status_code=~"5.."}`，找占比最高的路径
3. 查该路径近期变更（发布记录、配置调整、依赖版本）

**Resolution**:
- 定位到单一路径：按路径所属模块排查（通常是边界输入未覆盖的 500）
- 全局低水平漏损：检查依赖超时配置与重试策略是否产生长尾 502/504
- 预算充足且已定位 backlog 项：转工单跟踪，不需即时响应

### SLOLatencyFastBurn

**Severity**: Critical | **Threshold**: 慢于 2s 的请求 > 14.4%（14.4x 预算燃烧率），1h 与 5m 双窗同时成立

含义：按此速度 1 小时烧掉约 2% 的 30 天延迟预算（99% 请求 < 2s 的 SLO，
见 `deploy/observability/slo.md`）。

**Investigation**:
1. 先看 HighP99Latency 是否同时触发——触发则按其 runbook 处理
2. "HTTP Request Latency" 面板对比 P50/P95/P99：P50 也在涨 → 全局资源
   问题（DB、连接池、CPU）；只有 P99 涨 → 长尾问题（慢查询、依赖超时）
3. 注意口径差异：本告警按"请求慢于 2s 的比例"计算，AI 生成类慢请求
   会直接推高比例——先看 "AI Request Duration" 面板排除 AI 流量占比变化

**Resolution**:
- 资源瓶颈：扩容或提高连接池，参照 HighP99Latency 处置
- AI 流量导致：确认 SLO 是否需要按路由分组（AI 路径单列），
  属口径问题则提 SLO 修订，不是故障

## Known Gaps

部分指标已定义但尚未接线（`worker_job_duration_seconds`），
对应告警与面板已摘除。当前清单与接线计划见
`deploy/observability/known-gaps.md`；接线完成前不要在告警规则或
dashboard 中引用这些指标。

## Common Operations

### Check Dead Letter Queue

The in-memory dead letter recorder stores up to 1000 entries. Access via internal APIs or application logs filtered by dead letter events.

### Adjust Rate Limits

Edit `config.yml`:
```yaml
security:
  rate_limiting:
    enabled: true
    requests_per_minute: 300
    burst: 50
```

### Enable/Disable Tracing

Edit `config.yml`:
```yaml
monitoring:
  tracing:
    enabled: true
    endpoint: "http://otel-collector:4317"
    sample_ratio: 0.1
```

### Circuit Breaker Management

Reset circuit breaker via API:
```
POST /api/v1/ai/circuit-breaker/reset
```

## Metric Reference

### HTTP Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `http_requests_total` | Counter | method, path, status_code | Total HTTP requests |
| `http_request_duration_seconds` | Histogram | method, path | Request latency |
| `http_response_size_bytes` | Histogram | method, path | Request response size |
| `ratelimit_dropped_total` | Counter | path | HTTP 429 responses due to rate limiting |
| `errors_total` | Counter | severity, error_category, error_module | Classified server errors (HTTP 5xx via unified exit; 502/504 → dependency/network, other 5xx → system/internal) |

### AI Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `ai_requests_total` | Counter | provider, model, outcome, strategy | AI requests; strategy ∈ primary / fallback / transfer |
| `ai_request_duration_seconds` | Histogram | provider, model | AI latency |
| `ai_llm_tokens_total` | Counter | provider, token_type | Token consumption |

### Business Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `conversations_created_total` | Counter | tenant_id, channel | New conversations |
| `tickets_created_total` | Counter | tenant_id, priority | New tickets |
| `tickets_resolved_total` | Counter | tenant_id, outcome | Tickets moved to resolved |
| `routing_decisions_total` | Counter | tenant_id, strategy, outcome | Routing decisions; strategy ∈ handoff / assign / transfer |

### Async Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `eventbus_published_total` | Counter | event_type, outcome | Events published; outcome ∈ success / error（同步总线含 handler 失败） |
| `eventbus_handled_total` | Counter | event_type | Events handled successfully |
| `eventbus_failed_total` | Counter | event_type | Handler failures |
| `eventbus_handle_duration_seconds` | Histogram | event_type | Handler duration |
| `eventbus_dead_letter_total` | Counter | event_type | Events dead-lettered |
| `worker_jobs_total` | Counter | worker_name, outcome | Worker starts; outcome ∈ success / failure |
| `worker_active_jobs` | Gauge | worker_name | Currently active workers |

### Infrastructure Metrics

`go_*` / `process_*` 由 Prometheus runtime collectors 直接产出。

`worker_job_duration_seconds` 仍处未接线状态，
见 `deploy/observability/known-gaps.md`。SLO 语义与 burn rate
告警口径见 `deploy/observability/slo.md`。
