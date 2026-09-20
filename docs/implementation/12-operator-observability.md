# 12 Operator Observability

范围：

- tracing
- metrics
- structured logging
- 错误分级
- 告警与回放
- 运营诊断能力

## O1 telemetry-conventions

- [x] 统一 tracing、metrics、logging 的命名约定
- [x] 为 HTTP、WebSocket、AI、routing、voice 链路定义公共标签
- [x] 明确 request id、session id、tenant id、trace id 的透传规则
- [x] 统一结构化日志字段规范

验收：

- 不同模块输出的观测数据可以关联起来，而不是各写各的

当前进展：

- `internal/observability/telemetry/names.go` — 全量指标名、标签名、span 名、日志字段常量
- `internal/observability/telemetry/context.go` — request_id / session_id / tenant_id / trace_id 上下文传播
- `internal/observability/telemetry/request_id.go` — Gin RequestID 中间件，自动生成 UUID 并注入上下文
- `internal/observability/telemetry/logfields.go` — `FieldsFromContext()` 从上下文构建 logrus.Fields
- 已集成到 `internal/app/server/middleware.go`，RequestID 中间件在 gin.Logger() 之前执行

## O2 core-service-level-indicators

- [x] 定义 API、会话、工单、路由、AI、语音的关键指标
- [x] 明确成功率、延迟、错误率、积压量等 SLI
- [x] 为后台任务和事件消费定义吞吐与失败指标
- [x] 为 SDK 与服务端交互补最小体验指标

验收：

- 系统核心健康度可被量化，而不是靠日志猜测

当前进展：

- `internal/observability/metrics/registry.go` — Prometheus Registry 封装，进程级单例 `DefaultRegistry`
- `internal/observability/metrics/http_metrics.go` — HTTP 请求计数、延迟直方图、响应大小中间件
- `internal/observability/metrics/business_metrics.go` — 会话/工单/路由/AI 计数器与直方图
- `internal/observability/metrics/prometheus.go` — `PrometheusHandler()` 替代手写 metrics endpoint
- 已添加 `prometheus/client_golang` 依赖
- 已集成到 router（HTTPMetrics 中间件）和 health（/metrics Prometheus endpoint）
- 旧 `internal/metrics/metrics.go` 保持兼容，新 Prometheus 注册器并存

## O3 error-taxonomy-and-diagnostics

- [x] 定义错误分级与错误类别
- [x] 区分用户错误、依赖错误、配置错误、系统错误
- [x] 为关键模块补统一错误映射与日志策略
- [x] 为常见故障建立排查手册

验收：

- 出问题时可以快速知道”哪类错误、在哪里、怎么查”

当前进展：

- `internal/observability/errors/errors.go` — `AppError` type，Severity（user/dependency/config/system）、Category（auth/database/ai/routing/validation/rate_limit/internal/network）、Option 模式
- `internal/observability/errors/http_middleware.go` — `RecordHTTPStatus()` 按响应状态码分类（502/504 归 dependency，其余 5xx 归 system）+ `StatusMiddleware()` 统一出口（原 `classify.go`/`httpstatus.go` 的 `Classify`/`HTTPStatusFromError` 链因生产零消费已随 2026-09-20 死代码清理移除）
- `internal/observability/errors/metrics.go` — `errors_total` Prometheus counter + `RecordError()`

## O4 async-reliability-observability

- [x] 为 event bus 消费、worker、索引任务、AI fallback 增加幂等观测
- [x] 记录重试次数、死信、跳过、回退路径
- [x] 为长耗时任务增加阶段性进度与结果摘要
- [x] 为异步任务建立最小 replay / rerun 接口预留

验收：

- 异步链路从”黑盒”变为可观察、可重试、可解释

当前进展：

- `internal/observability/async/bus_middleware.go` — `BusMiddleware` 装饰 eventbus.Handler，记录时长/成功/失败/死信
- `internal/observability/async/worker_tracker.go` — `ObservableWorker` 装饰 bootstrap.Worker，记录 job 指标
- `internal/observability/async/dead_letter.go` — `DeadLetterRecorder` interface + `InMemoryDeadLetterRecorder`
- `internal/observability/async/replay.go` — `ReplayService` interface（stub 预留）

## O5 dashboards-alerts-and-replay

- [x] 盘点需要 dashboard 的核心视角
- [x] 定义关键告警触发条件与阈值策略
- [x] 预留会话、AI 请求、路由决策的 replay 边界
- [x] 为运营排障定义最小诊断面板需求

验收：

- 运营和研发都具备足够的现场信息，不必直接翻原始日志救火

当前进展：

- `deploy/observability/dashboards/servify-service.json` — 基础设施面板：HTTP 速率/延迟、错误率、速率限制、事件总线、Worker、Go Runtime
- `deploy/observability/dashboards/servify-business.json` — 业务面板：会话、工单、路由、AI 请求量/延迟/Token
- `deploy/observability/alerts/rules.yaml` — Prometheus 告警规则：5xx、P99 延迟、系统错误、事件失败、AI 降级、Worker 失败
- `deploy/observability/runbook/operational-runbook.md` — 运维手册：告警排查步骤、常见操作、Metric 参考
- 已新增 `servify check-observability-baseline --strict` 与 `scripts/check-observability-baseline.sh`，可在部署前检查 metrics/tracing 配置与 dashboard/alert/runbook/collector 资产是否齐备
