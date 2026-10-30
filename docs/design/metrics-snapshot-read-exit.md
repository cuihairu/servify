# MetricsAggregator Snapshot Read-Path Exit Policy

> Status: **decided on the user's behalf pending confirmation + landed this round**
> (decided during the 2026-10-15 inspection; the user may overturn it; overturning =
> restoring the `Snapshot()` method). Corresponds to the todo item "MetricsAggregator
> Snapshot read-path exit (pending product/ops decision)".
>
> **2026-10-30 已代决复核（docs/review/2026-10-30-decisions.md 第 6 项）**:
> 维持 Prometheus `/metrics` 为 MetricsAggregator 唯一对外读取出口(`SnapshotSeries` →
> `MetricsPrometheusCollector`),旧有损 `Snapshot()` 保持退役。导出路径验证:handlers 包
> 8 条 metrics 用例全绿(含 `TestMetricsPrometheusCollector_ExportsAggregatedSeries` 与
> `_EmptyAggregator`,2026-10-30 实跑)。运维验收时从 `/metrics` 抽查指标名/标签/数值
> 是否满足看板需求——该抽查登记为运维验收项,出现具体缺失项且确有消费者需求时再提新读取能力;
> 不恢复旧 Snapshot、不再造同内容 JSON 读取 API、不以指标快照实现收费。

## 1. Current State (measured 2026-10-15)

- `MetricsAggregator` (`apps/server/internal/handlers/metrics_ingest.go`)
  is an in-process mutex-guarded counter whose only writer is the `Add` of
  `POST /api/v1/metrics/ingest`. It is created only in `app/server/router_realtime.go:78`
  as a **local variable**, never enters `Dependencies`/`Runtime`, and is
  unreachable from other packages.
- There are already two read-exit methods:
  - `SnapshotSeries()`: a grouped snapshot that preserves labels, with a **single
    production caller** `MetricsPrometheusCollector.Collect`
    (`handlers/metrics_prometheus.go:36`) → `/metrics`
    (`app/server/health.go:17`, path defaults to `/metrics`).
  - `Snapshot()`: a **lossy** form that folds labels into a `k="v",...` signature
    string, with **zero production callers** (2 tests only).
- Production assembly always sets `HTTPMetrics` (`app/server/runtime_assembly.go:112`),
  so `/metrics` always takes the Prometheus branch; the JSON fallback
  (`handlers.MetricsHandler.GetMetrics`, only when `HTTPMetrics == nil`)
  is unreachable in production, and it **never reads the aggregator at all**
  (it only emits `servify_*` system metrics).

## 2. Comparison Table

| Option | Form | Pros | Cons |
| --- | --- | --- | --- |
| A. Prometheus `/metrics` as the only external read exit, retire the lossy `Snapshot()` | Single exit | Zero new surface (no auth/scope/rate-limit/swagger/security-surface guard cost); eliminates dead code with zero callers; single external contract | Environments without Prometheus cannot see client-reported metrics — but production assembly always goes through Prometheus, and the JSON fallback never had this data anyway; an existing gap, not a regression |
| B. Add an admin-plane JSON snapshot endpoint | e.g. `GET /api/v1/metrics/snapshot` | Readable without Prometheus | The new exposed surface must pass the full set of gates: auth/scope/rate-limit whitelist/swagger/`security_surface`; overlaps semantically with `/api/statistics/*` (DB business surface) |
| C. Dual exits: Prometheus + fix the JSON fallback to include aggregator data | Patch the existing fallback | Client-visible metrics in the fallback environment | The fallback is unreachable in production (`HTTPMetrics` is always non-nil), so the fix has low value; one more exit to maintain long-term |

## 3. Recommendation (decided on behalf)

**Option A**: Prometheus `/metrics` (via the `SnapshotSeries` bridge) is the only external
read exit; retire the lossy `Snapshot()` with zero production callers, and unify the
aggregator observation surface on `SnapshotSeries`.

Rationale: the consumer of client-reported metrics is the Prometheus scraper
(the collector is auto-registered when `Monitoring.Enabled` is on); when ops/product
needs business statistics, they go through `/api/statistics/*` (the DB surface), which is
unrelated to this aggregator. The exposed-surface gate cost of adding option B does not
match the duplicate-expression benefit of `Snapshot()`.

## 4. Implementation Status (landed this round)

- Deleted `MetricsAggregator.Snapshot()` (the lossy
  `map[string]map[string]float64` form).
- The two test observation surfaces switched to `SnapshotSeries()`, with
  semantically equivalent assertions (metric-name existence,
  whitelist rejection, value normalization, label merging, and
  tenant/session exclusion).
- Coverage unaffected (the deleted method's coverage vanished with the method;
  `SnapshotSeries`'s original coverage is retained).

## 5. How to Overturn

A `git revert` restores `Snapshot()` (no callers depend on it, no schema change).
