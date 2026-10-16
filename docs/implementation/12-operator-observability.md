# 12 Operator Observability

Scope:

- tracing
- metrics
- structured logging
- error classification
- alerting and replay
- operational diagnostics capability

## O1 telemetry-conventions

- [x] Unify the naming conventions for tracing, metrics, and logging
- [x] Define common labels for the HTTP, WebSocket, AI, routing, and voice chains
- [x] Clarify the pass-through rules for request id, session id, tenant id, and trace id
- [x] Unify the structured log field standards

Acceptance:

- Observability data emitted by different modules can be correlated instead of each writing its own

Current progress:

- `internal/observability/telemetry/names.go` — the full set of metric name, label name, span name, and log field constants
- `internal/observability/telemetry/request_id.go` — Gin RequestID middleware that generates a UUID and injects it into the context (with the `FieldRequestID` log field)
- Context propagation status: request_id propagation has landed (request_id.go + `internal/app/server/middleware.go` mounts the RequestID middleware, running ahead of gin.Logger()); session_id logging is injected by the gin middleware (`internal/platform/auth/gin_middleware.go:102-103`)
- tenant_id / trace_id pass-through has landed (2026-10-11): `telemetry/logfields.go` — `FieldsFromContext` gathers request_id (injected into the gin ctx by RequestIDMiddleware) / tenant_id (stamped into the request ctx by the auth middleware, same source as repository writes) / trace_id (OTel span first, falling back to the upstream-passed X-Trace-ID header, caught by RequestIDMiddleware); `AccessLogger` replaces gin.Logger, access logs go through the standard logrus exit (sharing InitLogger's JSON/text formats and output targets) and carry all pass-through fields + method/path/status/latency_ms/client_ip

## O2 core-service-level-indicators

- [x] Define key metrics for API, sessions, tickets, routing, AI, and voice
- [x] Clarify SLIs such as success rate, latency, error rate, and backlog
- [x] Define throughput and failure metrics for background jobs and event consumption
- [x] Add minimal experience metrics for SDK-server interactions

Acceptance:

- Core system health is quantifiable instead of guessed from logs

Current progress:

- `internal/observability/metrics/registry.go` — Prometheus Registry wrapper, process-level singleton `DefaultRegistry`
- `internal/observability/metrics/http_metrics.go` — HTTP request count, latency histogram, and response size middleware
- `internal/observability/metrics/business_metrics.go` — session/ticket/routing/AI counters and histograms
- `internal/observability/metrics/prometheus.go` — `PrometheusHandler()` replaces the hand-written metrics endpoint
- The `prometheus/client_golang` dependency has been added
- Integrated into the router (HTTPMetrics middleware) and health (/metrics Prometheus endpoint)
- The old `internal/metrics/metrics.go` stays compatible; the new Prometheus registry coexists

## O3 error-taxonomy-and-diagnostics

- [x] Define error severity levels and error categories
- [x] Distinguish user errors, dependency errors, configuration errors, and system errors
- [x] Add unified error mapping and logging policies for key modules
- [x] Build a troubleshooting playbook for common failures

Acceptance:

- When something goes wrong, you can quickly tell "what kind of error, where, and how to investigate"

Current progress:

- `internal/observability/errors/errors.go` — the `AppError` type, Severity (user/dependency/config/system), Category (auth/database/ai/routing/validation/rate_limit/internal/network), Option pattern
- `internal/observability/errors/http_middleware.go` — `RecordHTTPStatus()` classifies by response status code (502/504 map to dependency, the remaining 5xx to system) + `StatusMiddleware()` as the unified exit (the former `Classify`/`HTTPStatusFromError` chain in `classify.go`/`httpstatus.go` was removed with the 2026-09-20 dead code cleanup due to zero production consumers)
- `internal/observability/errors/metrics.go` — the `errors_total` Prometheus counter + `RecordError()`

## O4 async-reliability-observability

- [x] Add idempotency observability for event bus consumption, workers, indexing jobs, and AI fallback
- [x] Record retry counts, dead letters, skips, and fallback paths
- [x] Add staged progress and result summaries for long-running jobs
- [x] Reserve minimal replay / rerun interfaces for async jobs

Acceptance:

- Async chains go from "black box" to observable, retryable, and explainable

Current progress:

- `internal/observability/async/bus_middleware.go` — `BusMiddleware` decorates eventbus.Handler, recording duration/success/failure/dead letters
- `internal/observability/async/worker_tracker.go` — `ObservableWorker` decorates bootstrap.Worker, recording job metrics
- `internal/observability/async/dead_letter.go` — `DeadLetterRecorder` interface + `InMemoryDeadLetterRecorder`
- `internal/observability/async/replay.go` — `ReplayService` interface (stub reservation)

## O5 dashboards-alerts-and-replay

- [x] Inventory the core perspectives that need dashboards
- [x] Define key alert trigger conditions and threshold policies
- [x] Reserve replay boundaries for sessions, AI requests, and routing decisions
- [x] Define minimal diagnostic panel requirements for operational troubleshooting

Acceptance:

- Operations and engineering both have enough on-site information and do not have to dig through raw logs to fight fires

Current progress:

- `deploy/observability/dashboards/servify-service.json` — infrastructure panels: HTTP rate/latency, error rate, rate limiting, event bus, workers, Go runtime
- `deploy/observability/dashboards/servify-business.json` — business panels: sessions, tickets, routing, AI request volume/latency/tokens
- `deploy/observability/alerts/rules.yaml` — Prometheus alert rules: 5xx, P99 latency, system errors, event failures, AI degradation, worker failures
- `deploy/observability/runbook/operational-runbook.md` — operations runbook: alert troubleshooting steps, common operations, metric reference
- `servify check-observability-baseline --strict` and `scripts/check-observability-baseline.sh` have been added to check metrics/tracing configuration and dashboard/alert/runbook/collector assets before deployment
