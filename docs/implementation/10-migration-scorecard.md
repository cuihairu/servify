# 10 Migration Scorecard

> ⚠️ **Snapshot note (2026-10-04, V1.0 convergence B0 batch)**: This scorecard is a 2026-09-25 migration-period snapshot. Since then the `services` directory has been removed entirely (P3-2), event subscriptions for automation and statistics are now assembled by module delivery (`internal/app/server/runtime_assembly.go:408,436`), and agent runtime state has moved into `modules/agent/infra/redis_registry.go`. The current authoritative snapshot is `docs/current-architecture.md` (verified 2026-10-04); the legacy service wording in a few rows (agent/automation) is migration history only and no longer reflects the present.

This file records the current completion state of `10-service-to-module-migration`, supporting continuous tracking for M5.

Status definitions:

- `legacy`
  - handler/runtime still mainly depends on the old `services/*`
- `mixed`
  - partially wired into `modules/*`, but the HTTP or runtime main entrypoint is not closed out yet
- `contracted`
  - the handler-facing or runtime-facing main entrypoint is closed out to `modules/*/delivery`
- `stabilized`
  - closed out, with CI / docs / compatibility-responsibility rules continuously guarding it

Judging supplements:

- If a capability is closed out but not yet covered by `scripts/module-boundaries.rules`, it cannot be marked `stabilized`
- If the docs declare closeout but CI rules do not cover it yet, it can be marked `contracted` at most

## Scorecard

| Capability | Handler Entry | Runtime Entry | Legacy Service Role | Status | Notes |
| --- | --- | --- | --- | --- | --- |
| `ticket` | `modules/ticket/delivery.HandlerService` | `ticketdelivery.NewHandlerServiceWithDependencies(...)` | legacy service removed | `stabilized` | handler/router/runtime closed out, the old `services.TicketService` is deleted, and ticket orchestration is assembled directly by module delivery |
| `agent` | `modules/agent/delivery.HandlerService` | `services.AgentService` as contract impl | compatibility facade + runtime state holder | `stabilized` | handler DTO and the transfer runtime contract have been recovered into module delivery; runtime state now lives in `modules/agent/infra/redis_registry.go` (verified in the 2026-10-04 snapshot, no longer in a legacy service); the narrowed runtime surface is guarded by the boundary script |
| `analytics` | `modules/analytics/delivery.HandlerService` | `services.StatisticsService` as contract impl | compatibility facade + event bus glue | `stabilized` | DTOs are extracted into the module contract; the handler no longer connects directly to the concrete service |
| `routing / session transfer` | `modules/routing/delivery.HandlerService` | `modules/routing/delivery.NewHandlerService(...)` | legacy service removed | `stabilized` | session reads/assignment/system messages go through the conversation runtime adapter, waiting queue/transfer records go through the routing module, ticket assignment goes through the ticket module runtime adapter, and agent load adjustments go through the agent module runtime adapter; the websocket notification dependency is narrowed to a notifier interface, and the main runtime no longer goes through `services.SessionTransferService` |
| `conversation / websocket runtime` | n/a | `modules/conversation/delivery.WebSocketMessageWriter` | runtime connection hub | `stabilized` | both the main runtime and the lightweight realtime runtime use adapter injection; the `WebSocketHub` DB direct-write fallback is deleted |
| `ai` | `modules/ai/delivery.HandlerService` | `AIAssembly.RuntimeService` | narrow runtime contract for websocket/router | `stabilized` | the handler main path has switched to orchestrated enhanced AI; the assembly no longer exposes a legacy concrete AI service, and the old `AIServiceInterface` is deleted |
| `customer` | `modules/customer/delivery.HandlerService` | `modules/customer/delivery.NewHandlerService(db)` | compatibility facade + DTO mapping for old callers | `stabilized` | the handler/router/runtime main path goes directly through module delivery, guarded by integration tests and the boundary script |
| `automation` | `modules/automation/delivery.HandlerService` | `modules/automation/delivery.NewHandlerService(db)` | compatibility facade + event bus glue | `stabilized` | the handler/router/runtime main path goes directly through module delivery; subscribers are assembled by module delivery (`runtime_assembly.go:408`, verified in the 2026-10-04 snapshot) |
| `knowledge` | `modules/knowledge/delivery.HandlerService` | `modules/knowledge/delivery.NewHandlerServiceWithProvider(db, ...)` | compatibility facade retained for old callers | `stabilized` | the handler/router/runtime main path goes through module delivery; index job repositories and knowledge provider assembly are on the runtime main path and guarded by the boundary script, with the runtime assembling Dify first and WeKnora compatibility |
| `suggestion` | `modules/suggestion/delivery.HandlerService` | `modules/suggestion/delivery.NewHandlerService(db)` | legacy service removed | `stabilized` | the handler/router/runtime main path has switched to module delivery; the old `services.SuggestionService` is deleted, and helper unit tests and DB integration tests have sunk into module application / delivery |
| `gamification` | `modules/gamification/delivery.HandlerService` | `modules/gamification/delivery.NewHandlerService(db)` | legacy service removed | `stabilized` | the handler/router/runtime main path has switched to module delivery; the old `services.GamificationService` is deleted, and points / badge unit tests and DB integration tests have sunk into module application / delivery |
| `voice` | already module coordinator | module coordinator | not primarily legacy | `mixed` | fairly modularized already, but not the typical `services -> modules` migration shape of this round |

## Current conclusions

- `ticket`, `agent`, `analytics`, `routing`, `conversation runtime`, `ai`, `customer`, `automation`, and `knowledge` now meet the conditions for continuous guarding
- The old `customer` facade retains only historical-caller compatibility and DTO mapping responsibilities
- The old `automation` facade retains only the event bus subscriber and a little compatibility glue
- `knowledge` has completed handler/runtime closeout, with index job repositories and provider integration on the module delivery main path
- A batch of thin handler capabilities not yet fully modularized has completed `app/server` assembly-surface closeout: `satisfaction`, `macro`, `app integration`, `custom field`, `shift`
- `suggestion` has completed handler/runtime closeout, the old `services.SuggestionService` is deleted, and helpers and integration tests have sunk into the module layer
- `gamification` has completed handler/runtime closeout, the old `services.GamificationService` is deleted, and points / badge logic and tests have sunk into the module layer
- `message router` has completed runtime/router/handler assembly-surface closeout, keeping the concrete lifecycle implementation in `internal/platform/realtime.MessageRouter` (`internal/platform/realtime/message_router.go:27`)
- `sla` has completed assembly-surface closeout of the handler/runtime/router, but the service itself still retains automation glue and runtime assembly responsibilities
- `realtime runtime` has dropped the top-level concrete exposure of `WebSocketHub` / `WebRTCService`, keeping only the gateway contract and internal startup details
- All the `stabilized` entries above are covered by `scripts/module-boundaries.rules`, and the handler integration tests for `customer` / `automation` / `knowledge` have also switched to the module delivery main path
- For closed-out capabilities, old `services/*` should be deleted outright when there is no retained value, rather than keeping a facade long-term
- The next phase's focus is no longer "find another module to close out", but rather:
  - expand scorecard coverage
  - keep compressing the remaining `legacy` entries and fill in the indexing/runtime strategy for `knowledge`
  - define freeze policies and retirement conditions for `legacy` entries
