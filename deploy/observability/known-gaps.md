# Metrics Known Gaps

登记"已定义但当前无生产埋点"的指标。P2-4 起生效的一致性门禁
（`apps/server/internal/observability/metrics/consistency_test.go`）保证：

1. 本文件登记的指标必须存在于 telemetry 指标全集；
2. 告警规则与 dashboard 的 PromQL 表达式**禁止**引用本文件登记的指标——
   引用了就是"永真沉默"的死告警/死面板，CI 直接失败；
3. 指标接线完成后，从本文件删除对应条目，告警与面板即可恢复。

## 未接线指标

未接线指标对应的告警规则与 dashboard 面板已一并摘除，接线完成后随条目
删除一并恢复。

<!-- 行格式：- metric: <指标名>，行内可附计划说明 -->

- metric: worker_job_duration_seconds — ObservableWorker 只记启动计数与活跃 gauge；duration 需周期 job 级 TrackJob 接线（后续刀）
- metric: errors_total — 计划 P2-4 第四刀：errors.RecordError 已有，需在错误统一出口调用

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
