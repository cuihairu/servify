# Servify 多实例与高可用边界（P2-1）

> 最后更新：2026-09-17（P2-1 定稿）
> 维护规则：本文档逐面声明运行时拓扑边界。**任何改变下列某一面的代码
> 变更必须同步更新本文档与 [deployment.md](deployment.md) 的部署口径**；
> event bus 广播语义由测试锚定（见文末"证据"）。

## 结论

**Servify 当前是单实例优先（single-instance-first）**：生产部署按
`replicas: 1` 运行。多实例协同只在下文明确标注"多实例安全"的面成立；
其余面在多副本下会产生重复副作用或功能失效，且当前版本不做防护。

这不是"未完成的多实例支持"，而是明确的交付边界：哪些面已经是
共享状态（Redis/DB 承载），哪些面是进程内状态（单实例约束），
本文档逐面对齐代码事实。

## 逐面边界

### 1. Agent presence / load / assignment —— 多实例安全 ✅

- presence（online/busy）、负载、会话转接后的 load 都存 Redis
  （`agent/infra/redis_registry.go`，30s 心跳、60s TTL）。
- 实例崩溃后其 agent 状态随 TTL 过期自动消失，`agentRuntimeMaintenance`
  的 `cleanupInactiveAgents`（每实例运行、操作幂等）做兜底清理。
- 两实例共享 Redis + DB 时互相观测 presence 与 transfer load 的行为
  有 integration 测试锁定
  （`services/agent_service_redis_integration_test.go`）。
- 前提：所有实例必须连**同一个 Redis**（`BuildAgentServiceAssembly`
  注入的共享 client）。

### 2. WebSocket / WebRTC 实时路由 —— 单实例硬约束 ❌

- `WebSocketHub` 的连接表是进程内 map（`services/websocket.go`），
  `SendToSession` 只能投递到**本实例**持有连接的会话。
- WebRTC 信令、语音媒体协商同样经 hub 进程内转发。
- 多副本下的失效模式：客户 WS 连在实例 A、坐席连在实例 B →
  实时推送互相不可见（HTTP 消息仍会入库，但实时性丢失）。
- 因此：**所有需要实时互动的参与方（客户+坐席）必须落到同一实例**。
  滚动更新（maxSurge > 0）也会造成瞬时双实例，实时会话在滚动窗口内
  可能丢推送——见 deployment.md 的部署策略说明。

### 3. Event bus —— 广播语义（Redis provider）⚠️

Redis provider（`eventbus/redis_bus.go`）的投递模型：

- Publish：事件 XAdd 到 per-event stream（持久化，MaxLen ≈10000，
  排障可回查）+ PUBLISH 一条通知到 `servify:events:pubsub`。
- 每个实例的 `subscribeLoop` 都订阅通知频道，收到通知后各自从 stream
  读取并调用**本地** handlers。
- **语义是广播，不是竞争消费**：N 个实例 → 每条事件被每个实例的
  订阅方各处理一次（N 倍副作用）。代码中的 `consumerGroup` 常量
  尚未启用 consumer group。
- 消费不保证送达：通知丢失（实例正好在重连 pub/sub）时该实例跳过
  这条事件，没有 pending/ACK/重放机制。stream 里的记录仍可用于
  排障，但不会被自动重新分发。
- InMemory provider：纯进程内，进程重启即丢（dev/demo 语义）。

推论：**把订阅方写到 bus 上的副作用（如 webhook 入队）必须按
"N 实例各执行一次"来评估**。当前 webhook 订阅方（见下节）没有
event 级去重，多实例下同一事件会为每个匹配端点建 N 条 pending 投递。

### 4. Worker 认领与幂等 —— 逐 worker 差异 ⚠️

Worker 状态全部落 DB，进程重启后从 DB 续跑——**重启恢复语义成立** ✅。
差异在多实例下的任务认领：

| Worker | 认领机制 | 多实例行为 |
|--------|----------|------------|
| WaitingQueueWorker（routing 等待队列） | **原子租约**：`UPDATE ... WHERE claimed_at IS NULL OR < lease` CAS + 租约超时回收 + 失败归还（`routing/infra/gorm_repository.go` `ClaimQueueEntries`） | ✅ 竞争消费，无重复 |
| AutomationTimerWorker | 认领读非原子（`ClaimDueTimers` 无锁 Find），执行后 `CompleteTimer` 按 `status=pending` CAS | ⚠️ 动作可能被执行多次，只有一次计入 done（副作用去重靠动作自身幂等） |
| WebhookDeliveryWorker | 认领读非原子（`ClaimDueDeliveries` 无锁 Find，无 status CAS） | ❌ 重复投递到客户 endpoint |
| SurveyEmailWorker / StatisticsWorker / SLAMonitorWorker / QualityReviewWorker | 周期扫描式，无认领 | ⚠️ 重复扫描/聚合/发信（发信类风险最高） |
| EmailPollWorker | IMAP 单邮箱轮询 | ⚠️ 多实例竞争同一邮箱，重复摄取风险 |

HTTP 层补充：**rate limiter 是进程内 token bucket**
（`middleware/ratelimit.go`），多副本下实际限额 = 配置值 × 副本数；
需要全局限流时必须前置网关层限流。

## 部署口径

- 生产：`replicas: 1`（deployment.md 已对齐）。HA 需求通过
  **进程级重启恢复**（worker 状态在 DB、事件在 Redis stream、
  presence TTL 自动收敛）+ 快速拉起满足，不通过多副本满足。
- 事件总线：生产用 `event_bus.provider = "redis"`；`inmemory` 仅限
  dev/demo（该边界见 P0-1 与 config 示例）。
- 单实例进程崩溃的最大损失面：in-flight HTTP 请求、未投递的实时
  推送（会话数据已入库，重连后可见）、pub/sub 通知窗口内的事件消费
  （stream 记录仍在）。

## 升级路径（按面拆账，不一次性承诺）

1. **Worker 竞争消费**：把 `ClaimDueDeliveries` / `ClaimDueTimers`
   改成与 `ClaimQueueEntries` 相同的原子租约模式（仓库内已有正面
   样板，改动集中在 infra 层 + 回归测试）。
2. **Event bus 竞争消费**：启用 Redis stream consumer group
   （XREADGROUP + pending list ACK），消费语义从"每实例广播"
   变为"组内竞争"；需要补死信与重放策略。
3. **实时路由跨实例**：WS hub 增加 Redis pub/sub 跨实例广播层，
   或引入 sticky session 约束；二者都需要先明确重连/迁移语义。
4. **全局限流**：token bucket 迁 Redis（INCR+EXPIRE）或前置网关。

任一项落地后更新本文档对应面与 deployment.md。

## 证据

- 广播语义测试锚点：`platform/eventbus/redis_bus_multi_instance_test.go`
  —— 两个 `RedisBus` 实例共享同一 Redis，一条事件两边订阅方都收到
  （锁定本文档第 3 节的声明，防止未来静默改成竞争消费或丢失广播）。
- Agent 多实例证据：`services/agent_service_redis_integration_test.go`
  （P0-3）。
- routing 租约样板：`routing/infra/gorm_repository.go`
  `ClaimQueueEntries` / `ReleaseQueueClaim`。
