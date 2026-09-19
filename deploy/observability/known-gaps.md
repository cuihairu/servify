# Metrics Known Gaps

登记"已定义但当前无生产埋点"的指标。P2-4 起生效的一致性门禁
（`apps/server/internal/observability/metrics/consistency_test.go`）保证：

1. 本文件登记的指标必须存在于 telemetry 指标全集；
2. 告警规则与 dashboard 的 PromQL 表达式**禁止**引用本文件登记的指标——
   引用了就是"永真沉默"的死告警/死面板，CI 直接失败；
3. 指标接线完成后，从本文件删除对应条目，告警与面板即可恢复。

## 未接线指标

（当前无——全部已定义指标均已接线）

<!-- 行格式：- metric: <指标名>，行内可附计划说明 -->

## 已接线指标

### 第一刀（AI/provider 失败分类 + 三者一致门禁）

- `ai_requests_total{provider, model, outcome, strategy}` — strategy 取值
  primary（编排命中知识源）/ fallback（降级到基础问答）/ transfer（转人工）；
  provider 取值 dify / weknora / internal（转人工）/ none（未启用外部知识源）
- `ai_request_duration_seconds{provider, model}`
- `ai_llm_tokens_total{provider, token_type}`
- `ratelimit_dropped_total{path}` — HTTP 中间件对 429 响应计数
  （原 `servify_ratelimit_dropped_total` 手写 exposition 仅存在于
  HTTPMetrics 未装配的 legacy 端点，已不作为告警数据源）

### 第二刀（会话/工单/路由业务埋点）

- `conversations_created_total{tenant_id, channel}` — 会话创建成功计数
  （tenant 体系尚未落地，暂记 default）
- `tickets_created_total{tenant_id, priority}` — 工单创建成功计数
- `tickets_resolved_total{tenant_id, outcome}` — 状态迁移到 resolved 时计数
- `routing_decisions_total{tenant_id, strategy, outcome}` — strategy ∈
  handoff（入队等待）/ assign（定向指派）/ transfer（等待队列转出）
- business dashboard 的 Conversations/Tickets/Routing 三面板已随之恢复

### 第三刀（事件总线与 worker 观测接线）

- `eventbus_published_total{event_type, outcome}` — ObservableBus 装饰
  总线 Publish（cmd/server/main.go 一处接线覆盖所有发布方）；
  同步总线口径：outcome=error 表示分发未成功（含 handler 失败）
- `eventbus_handled_total{event_type}` / `eventbus_failed_total{event_type}`
  / `eventbus_handle_duration_seconds{event_type}` — ObservableBus
  Subscribe 时经 WrapHandler 包装全部订阅方
- `eventbus_dead_letter_total{event_type}` — 死信落库成功时计数
  （InMemoryDeadLetterRecorder，容量 1000）
- `worker_jobs_total{worker_name, outcome}` /
  `worker_active_jobs{worker_name}` — RegisterDefaultWorkers 注册完成后
  统一用 ObservableWorker 包装（sync.Once 保证 collector 进程级单例）
- service dashboard 的 Event Bus Throughput / Worker Jobs 面板与
  EventBusHandlerFailures / EventBusDeadLetters / WorkerJobFailures
  告警已随之恢复

### 第四刀（errors_total 统一出口 + SLI/SLO burn rate 告警）

- `errors_total{severity, error_category, error_module}` — 统一出口为
  HTTP 层 `observability/errors` 的 StatusMiddleware（与 HTTPMetrics 中间件
  同一挂载条件），响应完成后按最终状态码对 5xx 打点：502/504 归
  dependency/network（上游依赖失败语义），其余 5xx 归 system/internal，
  module 固定 http、code 为状态码；2xx/4xx 不计数（限流已有
  ratelimit_dropped_total 单独计数，避免双计）
- SLO 语义见 [slo.md](slo.md)：availability 99.9%（30d），latency
  99% 请求 < 2s；burn rate 告警 SLOAvailabilityFastBurn /
  SLOAvailabilitySlowBurn / SLOLatencyFastBurn 与 service dashboard 的
  SLO Error Budget 面板已随之恢复

### 第五刀（worker job 级埋点）

- `worker_job_duration_seconds{worker_name}` — 全部后台 worker 的周期循环
  收敛到 app/worker 的 `periodicJob` helper（jitter → 首跑 → ticker），
  每轮经 `TrackJob` 观测：duration + `worker_jobs_total` + 活跃 gauge；
  Statistics/SLA 的单轮 job 方法（`RunDailyStatsUpdate` /
  `RunMonitorOnce`）由 worker 侧驱动，循环不再沉在 service 内
- 口径升级（有意）：`worker_jobs_total{worker_name, outcome}` 从「worker
  Start 计数」（每生命周期一次，几乎恒 success）改为「job 轮次计数」——
  单轮业务失败（DB 抖动等）现以 outcome=failure 记录并进入
  WorkerJobFailures 告警视野；`worker_active_jobs` 同时承载 worker 存活
  （包装层 Start/Stop）与单轮 job 进行中（TrackJob）两个增量来源
- service dashboard 的 Worker Job Duration p99 面板随之新增；known-gaps
  清零
