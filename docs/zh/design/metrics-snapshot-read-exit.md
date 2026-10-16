# MetricsAggregator Snapshot 读取出口口径

> 状态：**已代决待核 + 本轮已落地**（2026-10-15 巡检代决，用户可推翻；
> 推翻 = 恢复 `Snapshot()` 方法）。对应 todo「MetricsAggregator
> Snapshot 读取出口（待产品/运维口径）」。

## 1. 现状（2026-10-15 实测）

- `MetricsAggregator`（`apps/server/internal/handlers/metrics_ingest.go`）
  是进程内 mutex 保护计数器，写入端唯一：`POST /api/v1/metrics/ingest`
  的 `Add`。它作为**局部变量**只在 `app/server/router_realtime.go:78`
  创建，不进 `Dependencies`/`Runtime`，其它包不可达。
- 已有两个读出口方法：
  - `SnapshotSeries()`：保留 labels 的分组快照，**生产唯一调用方**
    `MetricsPrometheusCollector.Collect`
    （`handlers/metrics_prometheus.go:36`）→ `/metrics`
    （`app/server/health.go:17`，路径默认 `/metrics`）。
  - `Snapshot()`：labels 折叠成 `k="v",...` 签名串的**有损**形态，
    **零生产调用方**（仅 2 个测试）。
- 生产装配恒设 `HTTPMetrics`（`app/server/runtime_assembly.go:112`），
  因此 `/metrics` 恒走 Prometheus 分支；JSON fallback
  （`handlers.MetricsHandler.GetMetrics`，仅 `HTTPMetrics == nil` 时）
  在生产不可达，且它**根本不读聚合器**（只出 `servify_*` 系统指标）。

## 2. 对比表

| 方案 | 形态 | 优点 | 缺点 |
| --- | --- | --- | --- |
| A. Prometheus `/metrics` 为唯一对外读取出口，退役有损 `Snapshot()` | 单出口 | 零新增 surface（无 auth/scope/限流/swagger/surface 守卫成本）；消除零调用方死代码；对外契约单一 | 无 Prometheus 的环境看不到客户端上报指标 —— 但生产装配恒走 Prometheus，且 JSON fallback 本就缺这份数据，属既有缺口而非回退 |
| B. 新增管理面 JSON 快照端点 | 如 `GET /api/v1/metrics/snapshot` | 无 Prometheus 也能读 | 新增开放面要走 auth/scope/限流白名单/swagger/`security_surface` 全套门禁；与 `/api/statistics/*`（DB 业务面）语义重叠 |
| C. 双出口：Prometheus + 修 JSON fallback 补聚合器数据 | 修补既有 fallback | fallback 环境可见客户端指标 | fallback 在生产不可达（`HTTPMetrics` 恒非 nil），修补价值低；多一条要长期维护的出口 |

## 3. 推荐（已代决）

**方案 A**：Prometheus `/metrics`（经 `SnapshotSeries` 桥）是唯一对外
读取出口；退役零生产调用方的有损 `Snapshot()`，聚合器观察面统一走
`SnapshotSeries`。

理由：客户端上报指标的消费者是 Prometheus 抓取方（`Monitoring.Enabled`
开启时自动注册 collector）；运维/产品侧需要业务统计时走
`/api/statistics/*`（DB 面），与本聚合器无关。新增 B 的开放面门禁成本
与 `Snapshot()` 的重复表达收益不匹配。

## 4. 落地状态（本轮已落地）

- 删除 `MetricsAggregator.Snapshot()`（`map[string]map[string]float64`
  有损形态）。
- 两个测试观察面改用 `SnapshotSeries()`，断言语义等价（指标名存在性、
  白名单拒绝、值归一化、labels 合并与 tenant/session 剔除）。
- 覆盖率不受影响（被删方法原有覆盖随方法消失；`SnapshotSeries`
  原有覆盖保留）。

## 5. 推翻方式

`git revert` 恢复 `Snapshot()` 即可（无调用方依赖，无 schema 变更）。
