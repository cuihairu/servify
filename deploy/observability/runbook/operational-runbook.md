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
| `eventbus_` | Event bus processing（known gap，见 known-gaps.md） |
| `worker_` | Background job processing（known gap，见 known-gaps.md） |
| `errors_` | Classified errors（known gap，见 known-gaps.md） |

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

## Known Gaps

部分指标已定义但尚未接线（eventbus_\*、worker_\*、errors_total），
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

### Infrastructure Metrics

`go_*` / `process_*` 由 Prometheus runtime collectors 直接产出。

其余业务与异步指标（conversations/tickets/routing/eventbus/worker/errors）
处于未接线状态，见 `deploy/observability/known-gaps.md`。
