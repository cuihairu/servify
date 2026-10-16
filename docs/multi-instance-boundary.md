# Servify Multi-instance and HA Boundary (P2-1)

> Last updated: 2026-09-17 (P2-1 finalized)
> Maintenance rule: this document declares the runtime topology boundary face by face. **Any code change
> that alters one of the faces below must synchronously update this document and the deployment spec in
> [deployment.md](deployment.md)**; the event bus broadcast semantics are anchored by tests (see "Evidence" at the end).

## Conclusion

**Servify is currently single-instance-first**: production deployments run with
`replicas: 1`. Multi-instance coordination holds only on the faces explicitly
marked "multi-instance safe" below; the remaining faces produce duplicate side
effects or functional failure under multiple replicas, with no protection in the current version.

This is a deliberate delivery boundary, not "unfinished multi-instance support":
which faces are already shared state (carried by Redis/DB) and which are process-local
state (the single-instance constraint) — this document aligns them face by face against code facts.

## Face-by-face Boundary

### 1. Agent presence / load / assignment — multi-instance safe ✅

- Presence (online/busy), load, and post-transfer session load are all stored in Redis
  (`agent/infra/redis_registry.go`, 30s heartbeat, 60s TTL).
- After an instance crashes, its agents' state auto-expires with the TTL; `cleanupInactiveAgents` of
  `agentRuntimeMaintenance` (runs on every instance, idempotent operations) does the fallback cleanup.
- The behavior of two instances mutually observing presence and transfer load while sharing
  Redis + DB is locked by `agent/infra/redis_registry_multi_instance_test.go`
  (the original integration test `services/agent_service_redis_integration_test.go` was deleted
  with the P3-2 removal of `internal/services`; an equivalent anchor was restored on 2026-10-14).
- Status messages (`servify:agent:status`) are only presence notifications: cross-instance echo is
  merged via `InMemoryRegistry.MergeRemote` — metadata defers to the remote snapshot, while load
  accounting, session tables, and online timestamps stay local (load is owned by the instance holding
  the sessions). The pub/sub broadcast includes the publisher itself; on 2026-10-15 this anchor caught
  a real race in CI with a full rebuild (the echo landing between the local load change and its Redis
  sync → the increment was reset to a stale snapshot); the reproduction window and merge semantics are
  locked by `agent/infra/redis_registry_status_echo_test.go`.
- Prerequisite: all instances must connect to **the same Redis** (the shared client injected by `BuildRuntime`).

### 2. WebSocket / WebRTC Real-time Routing — single-instance hard constraint ❌

- The `WebSocketHub` connection table is a process-local map (`platform/realtime/websocket_hub.go`,
  struct definition :88); `SendToSession` (:559) can only deliver to sessions this instance holds.
- WebRTC signaling and voice media negotiation are likewise forwarded in-process through the hub.
- Failure mode under multiple replicas: the customer's WS is connected to instance A while the agent
  is connected to instance B → real-time pushes are mutually invisible (HTTP messages still land in the DB).
- Therefore: **all participants needing real-time interaction (customer + agent) must land on the same
  instance**. Rolling updates (maxSurge > 0) also create transient dual instances; real-time conversations
  can lose pushes within the rolling window — see the deployment strategy notes in deployment.md.

### 3. Event Bus — broadcast semantics (Redis provider) ⚠️

The delivery model of the Redis provider (`eventbus/redis_bus.go`):

- Publish: the event is XAdded to a per-event stream (persisted, MaxLen ≈10000,
  queryable for troubleshooting) + a notification is PUBLISHed to `servify:events:pubsub`.
- Every instance's `subscribeLoop` subscribes to the notification channel, and on receiving
  a notification each instance reads from the stream and invokes **local** handlers.
- **The semantics are broadcast, not competing consumption**: N instances → each event is handled once
  by each instance's subscribers (N-fold side effects). There is no `consumerGroup` constant in the code,
  and XREADGROUP is never called.
- Consumption is not guaranteed delivered: when a notification is lost (the instance happens to be
  reconnecting pub/sub), that instance skips this event; there is no pending/ACK/replay mechanism.
  The records in the stream remain usable for troubleshooting, but are not automatically redistributed.
- InMemory provider: purely process-local and lost on process restart (dev/demo semantics).

Inference: **side effects written onto the bus by subscribers (e.g. webhook enqueueing) must be
evaluated as "executed once per N instances"**. The current webhook subscriber (see the next section)
has no event-level dedup, so under multiple instances the same event creates N pending deliveries
for each matching endpoint.

### 4. Worker Claiming and Idempotency — differs per worker ⚠️

Worker state is fully persisted in DB and resumes from DB after process restart — **the restart-recovery
semantics hold** ✅. The difference is task claiming under multiple instances:

| Worker | Claim mechanism | Multi-instance behavior |
|--------|----------|------------|
| WaitingQueueWorker (routing waiting queue) | **Atomic lease**: `UPDATE ... WHERE claimed_at IS NULL OR < lease` CAS + lease-timeout reclaim + failure return (`ClaimQueueEntries` in `routing/infra/gorm_repository.go`) | ✅ Competing consumption, no duplicates |
| AutomationTimerWorker | Non-atomic claim read (`ClaimDueTimers` is a lock-free Find); after execution `CompleteTimer` CASes on `status=pending` | ⚠️ Actions may execute multiple times, only one counts as done (side-effect dedup relies on the action's own idempotency) |
| WebhookDeliveryWorker | Non-atomic claim read (`ClaimDueDeliveries` is a lock-free Find, no status CAS) | ❌ Duplicate delivery to the customer endpoint |
| SurveyEmailWorker / StatisticsWorker / SLAMonitorWorker / QualityReviewWorker | Periodic scan style, no claiming | ⚠️ Duplicate scans/aggregation/emails (email-sending ones carry the highest risk) |
| EmailPollWorker | IMAP single-mailbox polling | ⚠️ Multiple instances contend for the same mailbox; duplicate ingestion risk |

HTTP-layer addition: **the rate limiter is a process-local token bucket**
(`middleware/ratelimit.go`); under multiple replicas the effective limit = configured value × replica count;
a gateway-layer rate limiter must be placed in front when global limiting is needed.

## Deployment Spec

- Production: `replicas: 1` (already aligned in deployment.md). HA needs are met through **process-level
  restart recovery** (worker state in DB, events in Redis streams, presence TTL auto-convergence)
  plus fast bring-up, not through multiple replicas.
- Event bus: production uses `event_bus.provider = "redis"`; `inmemory` is dev/demo only (this boundary
  is covered by P0-1 and the config examples; production startup validation and the assembly layer reject
  `inmemory`; staging has no independent validation branch — P3-3).
- Maximum loss surface of a single-instance process crash: in-flight HTTP requests, undelivered real-time
  pushes (conversation data is already persisted and visible after reconnect), and event consumption
  within the pub/sub notification window (stream records remain).

## Upgrade Path (accounted for per face, not committed all at once)

1. **Worker competing consumption**: change `ClaimDueDeliveries` / `ClaimDueTimers` to the same atomic
   lease pattern as `ClaimQueueEntries` (a positive in-repo example already exists; changes concentrate
   in the infra layer + regression tests).
2. **Event bus competing consumption**: enable Redis stream consumer groups (XREADGROUP + pending
   list ACK), changing consumption semantics from "broadcast per instance" to "compete within a group";
   dead-letter and replay strategies need to be added.
3. **Cross-instance real-time routing**: add a Redis pub/sub cross-instance broadcast layer to the WS hub,
   or introduce sticky-session constraints; both need reconnect/migration semantics clarified first.
4. **Global rate limiting**: move the token bucket to Redis (INCR+EXPIRE) or put a gateway in front.

Once any item lands, update the corresponding face in this document and in deployment.md.

## Evidence

- Broadcast semantics test anchor: `platform/eventbus/redis_bus_multi_instance_test.go` — two `RedisBus`
  instances sharing one Redis; one event is received by both instances' subscribers (locks the claim in
  section 3 of this document, preventing a silent future change to competing consumption or a lost broadcast).
- Agent multi-instance evidence: `agent/infra/redis_registry_multi_instance_test.go` — two `RedisRegistry`
  instances sharing one Redis and DB, mutually observing presence, transfer load, and TTL expiry after
  instance crash (the original anchor `services/agent_service_redis_integration_test.go` was deleted with
  P3-2; an equivalent anchor was restored on 2026-10-14).
- Echo merge semantics evidence: `agent/infra/redis_registry_status_echo_test.go` — an online echo
  must not overwrite local load/sessions/online timestamp (2026-10-15).
- Routing lease reference implementation: `routing/infra/gorm_repository.go`
  `ClaimQueueEntries` / `ReleaseQueueClaim`.
