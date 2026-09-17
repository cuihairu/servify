# Servify SLO 定义

P2-4 第四刀定稿的首批 SLI/SLO。burn rate 告警见
[rules.yaml](alerts/rules.yaml) 的 `servify.slo` 组，处置手册见
[operational-runbook.md](runbook/operational-runbook.md)。

## SLI 与 SLO

| SLI | 数据源（PromQL 事实） | SLO 目标（30d 滚动窗口） |
|-----|------------------------|--------------------------|
| Availability | `http_requests_total{status_code=~"5.."}` 占全部请求比例 | 非 5xx ≥ **99.9%**（错误预算 0.1%） |
| Latency | `http_request_duration_seconds_bucket{le="2"}` 覆盖的请求比例 | 响应时间 < **2s** 的请求 ≥ **99%**（预算 1%） |

口径说明：

1. **Availability 的"坏事件"只算 5xx。** 4xx 是请求方错误（含限流
   429），不消耗可用性预算；限流有自己的容量视角（`ratelimit_dropped_total`
   与 HighRateLimitDrops 告警）。
2. **Latency 的"坏事件"是 > 2s 的请求。** 目标定为 99% 而非 P99 单点，
   两者数学等价，但比例口径可直接参与 burn rate 计算；2s 门槛覆盖
   正常 API 请求，AI 生成类慢请求由 `ai_request_duration_seconds`
   单独观测（AIHighLatency）。
3. 窗口为 30 天滚动；当前单实例部署（见 P2-1），SLI 按整个 scrape
   目标聚合，不分实例/区域。

## Burn rate 告警

采用 Google SRE 多窗口多燃烧率做法，长窗给趋势、短窗做确认，
避免单窗毛刺误报：

| 告警 | 燃烧率 | 长窗 / 短窗 | 30d 预算消耗 | 级别 |
|------|--------|-------------|--------------|------|
| SLOAvailabilityFastBurn | 14.4x | 1h / 5m | 1h 烧掉 ~2% | critical（page） |
| SLOAvailabilitySlowBurn | 6x | 6h / 30m | 6h 烧掉 ~5% | warning（ticket） |
| SLOLatencyFastBurn | 14.4x | 1h / 5m | 1h 烧掉 ~2% | critical（page） |

数学推导（以 availability 为例，预算 = 0.1%）：

- 14.4x：`14.4 × 0.1% = 1.44%` 错误率，持续 1h 恰好消耗
  `1.44% × 1h / 720h ≈ 2%` 的 30 天预算 —— 一天烧完预算的速度量级。
- 6x：`6 × 0.1% = 0.6%` 错误率，持续 6h 消耗 `0.6% × 6h / 720h ≈ 5%`
  —— 周末堆到周一处理的量级。
- latency 的预算是 1%，同样燃烧率对应错误率阈值放大 10 倍
  （`14.4 × 1% = 14.4%` 的请求慢于 2s）。

## 与症状告警的关系

`HighHTTP5xxRate`（5xx > 5% 持续 5m）与 `HighP99Latency`（P99 > 5s）
保留为**症状告警**：阈值绝对、响应快，抓"已经坏了"的时刻。burn rate
告警是**预算告警**：抓"坏得还不够离谱但正在快速烧预算"的时段
（例如 2% 的持续错误率不会触发 5% 的症状告警，却会在一天内烧穿预算）。
两层同时存在，runbook 中先看 burn rate 判断趋势，再用症状告警定位现场。

## 面板

service dashboard 的 **SLO Error Budget** 面板展示两类预算的 30d
剩余比例（100% = 全新预算，0% = 预算烧穿，负值 = 超支）。计算式与
本文档口径一致，由一致性门禁
（`apps/server/internal/observability/metrics/consistency_test.go`）
保证与 rules.yaml / dashboard 三者不漂移。
