# 性能与容量基线（P2-8）

servify 的第一版容量基线：回答"这个服务到底能承受多少负载"——不是感觉上"能跑"，而是有数字、可复跑、有证据的结论。

## 基线环境

| 项 | 值 |
| --- | --- |
| 数据库 | sqlite（本地文件，AutoMigrate） |
| LLM | 内嵌 mock（OpenAI 兼容 `chat/completions`，恒定响应，无网络延迟） |
| 知识库 | 无外部 provider（默认态，`knowledge/upload` 与 `sync` 返回 503 是预期口径） |
| 限流 | 放宽至 `requests_per_minute=100000 / burst=10000`（避免限流器成为被测对象） |
| 主机 | 单机（本机 127.0.0.1 回环，无网络损耗） |

基线口径说明：基线测的是**应用层的相对容量与延迟形态**，不含真实 LLM 往返（AI 场景的下游延迟为 mock 的亚毫秒级）。接入真实 LLM 后，AI 查询延迟应以 p95 + LLM 网络延迟估算。

## 工具链

- `scripts/perfbench`（Go 标准库实现，零三方压测依赖）：四场景负载器 + 手写 WebSocket 客户端（RFC 6455 握手与帧编解码）。
- `scripts/test-perf-baseline.sh`：编排脚本——构建 → mock LLM → 派生压测配置（`perfbench -gen-config`，yaml Node API 保留生产配置注释与占位符）→ sqlite 起服 → 401 负例 → admin → 四场景 → 结果校验 → manifest 写入。
- 两档规模：`smoke`（CI 驱动测试秒级）与 `full`（容量基线）。

```bash
make perf-baseline                     # smoke 档（CI 同款）
PERF_SCALE=full make perf-baseline     # full 档（容量基线）
```

## 场景与规模（full 档）

| 场景 | 负载 | 并发 |
| --- | --- | --- |
| tickets-mixed | 600 轮 ×（创建工单 + 发评论 + 列表查询）= 1800 ops | 16 |
| ai-query | 200 次 `POST /api/v1/ai/query` | 16 |
| upload-knowledge | 100 ×（文件上传 multipart + 知识文档上传）= 200 ops | 16 |
| ws-connections | 200 条 WebSocket 连接（阶梯建连，25 条/批）+ 每连接一条 text-message 往返 + `/api/v1/ws/stats` 对账 | 25/批 |

## 基线数据（2026-09-18，full 档）

数据来源：`scripts/test-results/perf-baseline/results.json`（manifest `provider=perf`）。

| 场景 | ops | 错误 | p50 | p95 | p99 | max | 吞吐 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| tickets-mixed | 1800 | 0 | 23.75ms | 267.47ms | 831.48ms | 1886.45ms | 208 ops/s |
| ai-query | 200 | 0 | 18.48ms | 1033.03ms | 1259.94ms | 2075.33ms | 83 ops/s |
| upload-knowledge | 200 | 0 | 8.59ms | 32.01ms | 35.89ms | 38.19ms | 1213 ops/s |
| ws-connections | 200 连接 | 0 | 4.83ms（握手 p50） | 9.26ms | 10.90ms | 13.26ms | 往返 6.41ms |

WebSocket 对账：客户端建连 200/200，服务端 `/api/v1/ws/stats` 报告 `connected_clients=200`，一致。

## 结论与解读

1. **ticket 主链路**：并发 16 下无错误，p50 25ms 内；p99 尾延迟 0.8s 量级，形态为 sqlite 单写锁在高并发写入下的排队（SQLite 串行写）。并发压力再上一个数量级时，第一个瓶颈是数据库写吞吐，而非应用层。生产部署（postgres）该尾延迟应显著收敛——本基线不含 postgres 数据，替换后建议重跑对照。
2. **AI 查询**：mock 下游亚毫秒延迟时 p95 仍达 1.0s——主要延迟来自查询管线本身（会话加载、超时/重试保护路径、运行时组装），而非 LLM。这是当前 AI 链路的真实成本下限，接真实 LLM 后延迟只会更高。
3. **文件上传**：吞吐最高的场景（1213 ops/s，p99 36ms），multipart 解析与本地盘写入不构成近期瓶颈。
4. **WebSocket**：200 连接全量建立且服务端统计精确对账（对账读取按峰值轮询：hub 登记在握手 101 之后异步完成，慢机上瞬时快照可能少计），握手 p50 5ms、消息往返 6.4ms。连接管理器在该规模无压力；容量上限探测（数万连接）不在本档位，需要独立的高连接数档位与系统级 fd 调优。
5. **限流**：压测前必须通过 `perfbench -gen-config` 放宽，否则限流器先于应用层到顶，测出的"容量"是限流阈值。

## 已知边界

- 基线于 sqlite 上取得；postgres（生产驱动）数据未覆盖，属已知差异而非等价结论。
- AI 场景使用 mock LLM；真实 LLM 的延迟与限流（429）未纳入。
- knowledge provider 为空是预期口径：上传/同步端点 503（`knowledge provider is not enabled`）已按预期处理（perfbench 记 note 不计错误），接入 pgvector/WeKnora 后该维度应重测。
- full 档在本机约 4 分钟；smoke 档秒级，随 CI（`TestPerfBaselineScriptWritesEvidence`）每次提交守护链路可用性。

## 复跑

```bash
PERF_SCALE=full make perf-baseline
bash scripts/validate-acceptance-manifest.sh scripts/test-results/perf-baseline/manifest.json
```

对比基线时关注：同场景 p95/p99 的数量级变化（>2x 需要解释）、WS 对账是否仍然一致、错误是否为 0。
