# Metrics Known Gaps

登记"已定义但当前无生产埋点"的指标。P2-4 起生效的一致性门禁
（`apps/server/internal/observability/metrics/consistency_test.go`）保证：

1. 本文件登记的指标必须存在于 telemetry 指标全集；
2. 告警规则与 dashboard 的 PromQL 表达式**禁止**引用本文件登记的指标——
   引用了就是"永真沉默"的死告警/死面板，CI 直接失败；
3. 指标接线完成后，从本文件删除对应条目，告警与面板即可恢复。

## 未接线指标

未接线指标对应的告警规则与 dashboard 面板已一并摘除，接线完成后随条目
删除一并恢复（business dashboard 的 Conversations/Tickets/Routing 三面板
在第二刀恢复）。

<!-- 行格式：- metric: <指标名>，行内可附计划说明 -->

- metric: conversations_created_total — 计划 P2-4 第二刀：会话创建埋点
- metric: tickets_created_total — 计划 P2-4 第二刀：工单生命周期埋点
- metric: tickets_resolved_total — 计划 P2-4 第二刀：工单生命周期埋点
- metric: routing_decisions_total — 计划 P2-4 第二刀：路由决策埋点
- metric: eventbus_published_total — 计划 P2-4 第三刀：BusMetrics 已有设施，装配处接线
- metric: eventbus_handled_total — 计划 P2-4 第三刀：同上（async.BusMiddleware）
- metric: eventbus_failed_total — 计划 P2-4 第三刀：同上
- metric: eventbus_handle_duration_seconds — 计划 P2-4 第三刀：同上
- metric: eventbus_dead_letter_total — 计划 P2-4 第三刀：死信计数器尚未注册
- metric: worker_jobs_total — 计划 P2-4 第三刀：ObservableWorker/TrackJob 已有设施，装配处接线
- metric: worker_job_duration_seconds — 计划 P2-4 第三刀：同上
- metric: worker_active_jobs — 计划 P2-4 第三刀：同上
- metric: errors_total — 计划 P2-4 第四刀：errors.RecordError 已有，需在错误统一出口调用

## 已接线指标（本刀完成）

- `ai_requests_total{provider, model, outcome, strategy}` — strategy 取值
  primary（编排命中知识源）/ fallback（降级到基础问答）/ transfer（转人工）；
  provider 取值 dify / weknora / internal（转人工）/ none（未启用外部知识源）
- `ai_request_duration_seconds{provider, model}`
- `ai_llm_tokens_total{provider, token_type}`
- `ratelimit_dropped_total{path}` — HTTP 中间件对 429 响应计数
  （原 `servify_ratelimit_dropped_total` 手写 exposition 仅存在于
  HTTPMetrics 未装配的 legacy 端点，已不作为告警数据源）
