# 10 Service To Module Migration

> Status (2026-10-04 doc reconciliation): M1–M6 are all complete — `internal/services` has been removed
> entirely (P3-2), business capabilities are closed out under `internal/modules/*` (capped at 27 top-level
> modules), so this doc is now a migration-history archive. The current active status source is todo.md.

Scope:

- Close out the old `services` / `handlers`
- Solidify the `modules/*` application entrypoints
- Legacy service retirement and deletion
- Migration route and completion governance

## M1 migration-inventory

- [x] Inventory the old `handlers -> services -> repo` chain
- [x] Inventory the coverage of the new `delivery -> application -> domain -> infra` chain
- [x] Map the current-vs-target entrypoints for every domain module
- [x] Flag high-risk cross-dependencies, shared models, and circular reference points

Acceptance:

- Every core capability can answer "which chain it uses today, and which chain it targets"

Deliverables:

- [10-migration-inventory.md](./10-migration-inventory.md)

## M2 module-entry-contracts

- [x] Define the single application entrypoint for `conversation`, `routing`, `ticket`, `analytics`, `ai`
- [x] Make explicit that handlers can depend only on adapter / application contracts, not assemble business logic directly
- [x] Define the compatibility responsibilities old services are allowed to retain
- [x] Establish one-way constraints for repo / gorm dependencies

Acceptance:

- New requirements no longer land in both the old service and the new module implementations at once

Currently landed:

- `ticket`
  - The handler-facing contract is closed out to `modules/ticket/delivery.HandlerService`
  - The runtime now uses `modules/ticket/delivery.NewHandlerServiceWithDependencies(...)`
  - The old `services/TicketService` is deleted
- `agent`
  - The handler-facing contract is closed out to `modules/agent/delivery.HandlerService`
  - The runtime still reuses `services.AgentService` as the contract implementation
  - `services.AgentService` is explicitly retained as a compatibility facade + runtime state holder
- `analytics`
  - The handler-facing contract is closed out to `modules/analytics/delivery.HandlerService`
  - The runtime still reuses `services.StatisticsService` as the contract implementation
  - `services.StatisticsService` is explicitly retained as a compatibility facade + event bus glue
- `routing / session transfer`
  - The handler-facing contract is closed out to `modules/routing/delivery.HandlerService`
  - The runtime now uses `modules/routing/delivery.HandlerServiceAdapter`
  - The old `services.SessionTransferService` is deleted
- `conversation / websocket runtime`
  - The websocket persistence entrypoint is explicitly closed out to `modules/conversation/delivery.WebSocketMessageWriter`
  - Both the main runtime and the lightweight realtime runtime inject the conversation module through `WebSocketMessageAdapter`
  - `WebSocketHub` has deleted the gorm direct-write fallback for `sessions/messages`, keeping only the conversation delivery path
- `ai`
  - The handler-facing contract is closed out to `modules/ai/delivery.HandlerService`
  - `AIAssembly` is split into a handler-facing service and a runtime-facing service
  - The default handler main path has been promoted to orchestrated enhanced AI instead of directly exposing the old `AIServiceInterface`
  - The server/runtime/router/websocket main chain now depends on `modules/ai/delivery.RuntimeService` or local runtime contracts and no longer passes `services.AIServiceInterface` through
  - The old `services/AIServiceInterface` / `EnhancedAIServiceInterface` are deleted
- `suggestion`
  - The handler-facing contract is closed out to `modules/suggestion/delivery.HandlerService`
  - The runtime now uses `modules/suggestion/delivery.NewHandlerService(db)`
  - The old `services.SuggestionService` is deleted; DTO aliases and helpers have sunk into module application / contract
- `gamification`
  - The handler-facing contract is closed out to `modules/gamification/delivery.HandlerService`
  - The runtime now uses `modules/gamification/delivery.NewHandlerService(db)`
  - The old `services.GamificationService` is deleted; points / badge logic and DTO aliases have sunk into module application / contract
- Boundary guards
  - CI has added `scripts/check-module-boundaries.sh`
  - It currently auto-verifies that the handler constructors, router dependencies, and runtime wiring of `ticket` / `agent` / `analytics` / `customer` / `automation` / `knowledge` / `routing` / `ai` / `suggestion` / `gamification`, plus the `conversation` websocket persistence wiring, must all stay on module delivery contracts

## M3 legacy-service-retirement

- [x] Add narrow interfaces or adapters for runtime entrypoints still in transition, instead of keeping handlers directly wired to old services
- [x] Move migratable business rules from old services into the module application / delivery layers
- [x] Delete the legacy service outright for closed-out capabilities instead of keeping a facade
- [x] Add tests for the adapter layer to ensure identical behavior before and after migration

Acceptance:

- Closed-out capabilities no longer keep dual entrypoints; unfinished capabilities have explicit freeze boundaries

Currently landed:

- `routing`
  - Behavior tests were added for `modules/routing/delivery.SessionTransferAdapter` covering waiting lifecycle, assignment/history, and transaction-scoped repository behavior
  - The handler/runtime main path has switched to `modules/routing/delivery.HandlerServiceAdapter`
  - The old `services/SessionTransferService` is deleted and no compatibility facade is retained
- `ticket`
  - The handler/runtime main path has switched to `modules/ticket/delivery.NewHandlerServiceWithDependencies(...)`
  - The old `services/TicketService` is deleted and no compatibility facade is retained
- `ai`
  - The handler/runtime main path has switched to `modules/ai/delivery.HandlerService` and `AIAssembly.RuntimeService`
  - The old `services/AIServiceInterface` / `services.OrchestratedAIService` are deleted; the runtime keeps only local narrow interfaces
- `conversation`
  - `WebSocketHub` has deleted the gorm direct-write fallback for `sessions/messages`
  - Websocket persistence must go through `modules/conversation/delivery.WebSocketMessageWriter`

## M4 core-module-migrations

- [x] Migrate `conversation` first
- [x] Migrate `routing` first
- [x] Migrate `ticket` first
- [x] Migrate `ai` first
- [x] Split each module into 1 to 3 independently committable small task packages

Acceptance:

- Core-chain modules close out first, and later modules can reuse the migration pattern

Suggested task packages:

- `conversation`
  - C1: Keep closing out the conversation write paths beyond websocket message persistence into `modules/conversation/application`
  - C2: Keep shrinking the conversation/protocol orchestration inside `WebSocketHub`, so conversation-private write paths do not regrow
- `routing`
  - R1: Keep sinking the waiting queue / assignment / cancel runtime orchestration into `modules/routing/application`
  - R2: Add before/after behavior-consistency tests for the routing runtime adapter
- `ticket`
  - T1: Keep adding cross-module behavior tests for module delivery orchestration
  - T2: Review for leftover comments, docs, or call sites referencing the deleted `TicketService`
- `ai`
  - A1: Keep slicing the runtime surface still shared by websocket / router into explicit contracts
  - A2: Add regression tests for the AI handler/runtime dual entrypoints, locking the assembly boundary

## M5 boundary-enforcement-and-scorecard

- [x] Add the migration completion scorecard
- [x] Document directory responsibilities and dependency directions
- [x] Add boundary constraint checks to code review / CI
- [x] Define the freeze policy and retirement conditions for `services/*`

Acceptance:

- Migration work is trackable and quantifiable, no longer stuck in "refactoring in progress"

Deliverables:

- [10-migration-scorecard.md](./10-migration-scorecard.md)
- [10-module-boundaries.md](./10-module-boundaries.md)

## M6 app-server-assembly-hardening

- [x] Split `app/server/router.go` by auth / management / public / realtime
- [x] Split `app/server/runtime.go` into composable phase-based assembly functions
- [x] Keep current behavior unchanged and verify via `app/server` and `app/bootstrap` tests

Acceptance:

- `app/server` no longer relies on a single giant assembly file to carry all startup complexity
- New modules or access surfaces have a clear assembly landing point instead of piling into the main entrypoint
