# 10 Migration Inventory

> Status (2026-10-04 doc reconciliation): The migration is complete — the `internal/services` directory has been removed entirely and all business capabilities are closed out under `internal/modules/*` (capped at 27 top-level modules). This doc is a migration-history archive of the M1 inventory phase; the "closeout complete" conclusions per capability have been delivered.

This file records the migration state of `handlers -> services -> modules` (M1 inventory snapshot), supporting the M1 inventory phase of `10-service-to-module-migration`.

## Current observations

- `ticket`
  - Already has a fairly strong modularization base
  - `handlers/ticket_handler.go` works through the `ticketHandlerService` interface
  - A module delivery adapter layer already exists at `modules/ticket/delivery/handler_adapter.go`
  - The runtime already uses `modules/ticket/delivery.NewHandlerServiceWithDependencies(...)` directly
  - The old `services/TicketService` is deleted
  - Current rule: HTTP handlers should depend on `modules/ticket/delivery.HandlerService`
  - Conclusion: `ticket` has completed main-path closeout and finished by deleting the legacy service

- `agent`
  - `services/AgentService` has clearly become a compatibility layer
  - Core reads/writes go through `modules/agent/application.Service`
  - The handler-facing DTO and transfer runtime contract have been recovered into `modules/agent/delivery`
  - `agentQueues` is removed; the remaining online-agent compatibility cache backfill, background cleanup scheduling, and service assembly are split into standalone runtime helpers/adapters/assembly, and background jobs now start explicitly only at the server runtime assembly point
  - Downstream services such as `workspace` / `session transfer` now depend on narrow interfaces instead of binding directly to `*AgentService`
  - The `app/server` runtime/router no longer exposes `*services.AgentService` fields either, keeping only handler-facing and business-specific dependencies
  - The `workspace handler` and `websocket hub` have further switched to workspace/transfer interfaces, continuing to shrink `app/server`'s concrete exposure of `WorkspaceService` / `SessionTransferService`
  - Current rule: HTTP handlers should depend on `modules/agent/delivery.HandlerService`
  - Conclusion: falls into the "module landed, but runtime compatibility state not yet closed out" category

- `analytics`
  - `services/StatisticsService` is basically a module facade
  - Core statistics capabilities go through `modules/analytics/application.Service`
  - It still keeps an event bus subscription and DTO-mapping compatibility wrapper
  - The `app/server` runtime no longer exposes `*services.StatisticsService` fields, keeping only the handler-facing contract and local wiring
  - Conclusion: low migration risk; a good template for later closeouts

- `routing / session transfer`
  - The main runtime no longer assembles `services.SessionTransferService`
  - handler/runtime already uses `modules/routing/delivery.HandlerServiceAdapter` directly
  - Session assignment sync goes through the `modules/conversation/delivery` runtime adapter and no longer updates `Session` directly
  - Transfer / waiting system message writes go through the `modules/conversation/delivery` runtime adapter and no longer write `Message` directly
  - Waiting queue reads, inserts, cancels, queries, and status synchronization, plus transfer record writes, are closed out into the routing module state machine
  - waiting -> transferred and the transfer record write are inside the same transaction, no longer relying on post-transaction patch-up sync
  - The system message write on waiting queue cancel has also switched to the `modules/conversation/delivery` runtime adapter, reducing legacy `Message` direct-write branches
  - The ticket assignment sync triggered by session transfer goes through the `modules/ticket/delivery` runtime adapter and no longer writes `Ticket` / `TicketStatus` directly
  - Agent load adjustments go through the `modules/agent/delivery` runtime adapter; the import cycle previously caused by the handler contract back-depending on `services` has been removed
  - The websocket notification dependency is narrowed to `sessionTransferNotifier`; `WebSocketHub` is no longer kept as a concrete field inside the service
  - The session read entrypoint has switched to `modules/conversation/delivery.RuntimeService.LoadTransferSession(...)`
  - Under the default assembly, adapter errors from `LoadTransferSession(...)` propagate directly instead of silently falling back to legacy DB queries, avoiding masking conversation runtime failures
  - The session active/unassigned sync when joining the waiting queue has also switched to `modules/conversation/delivery.RuntimeService.SyncWaitingAssignment(...)`
  - Websocket notifications are narrowed to a notifier interface; `WebSocketHub` is no longer kept as an internal concrete dependency
  - The old `services/SessionTransferService` is deleted; the leftover `services/session_transfer_legacy_store.go` (a DB compatibility read/write orphan with no production callers) is also deleted
  - Conclusion: `routing / session transfer` has completed main-path closeout and finished by deleting the legacy service

- `ai`
  - The old `services/AIService` and the enhanced AI implementation still exist, but are no longer exposed to the handler/runtime top level through `AIServiceInterface`
  - The handler has started closing out to `modules/ai/delivery.HandlerService`
  - `AIAssembly` has dropped the unconsumed `LegacyService *services.AIService` exposure, keeping only the handler-facing contract, the runtime-facing `RuntimeService`, and knowledge provider state
  - The old `services/AIServiceInterface` / `services.OrchestratedAIService` are deleted
  - `services/router.go` and `services/websocket.go` depend only on a local minimal AI runtime interface
  - Conclusion: `ai` has completed handler/runtime main-path closeout; the follow-up focus is continuing to shrink the local runtime interfaces

- `legacy handler-only capabilities`
  - Capabilities such as `satisfaction`, `csat public`, `macro`, `app integration`, `custom field`, and `shift` are not yet modularized
  - These handlers now depend on minimal interfaces defined inside the `handlers` package, instead of exposing `*services.*` concrete types to the `app/server` runtime/router surface
  - Conclusion: although not yet completed `services -> modules` migration items, they have gone through a round of "assembly-surface closeout", reducing the spread of concrete legacy services across the top-level runtime

- `message router`
  - Both the lightweight runtime and the main runtime still need the `Start/Stop` lifecycle, but the HTTP layer only cares about platform statistics reads
  - `services.MessageRouterRuntime` has been extracted; `app/server` and `handlers.MessageHandler` no longer expose the concrete `*services.MessageRouter`
  - The unused message router injection into `MetricsHandler` has been removed

- `realtime runtime internals`
  - `WebSocketHub` / `WebRTCService` are runtime assembly details and should not keep being exposed as public concrete fields of `Runtime` / `RealtimeRuntime`
  - They are now closed into internal `Run()` startup dependencies and gateway adapter assembly details; externally only `RealtimeGateway` / `RTCGateway` / `MessageRouterRuntime` remain

- `sla`
  - `SLAHandler` has long worked internally against a minimal method set, but the `app/server` top-level assembly still exposed `*services.SLAService`
  - The handler contract has now been explicitly promoted to `handlers.SLAService` / `handlers.SLATicketReader`
  - `ticketdelivery.ReaderServiceAdapter` is reused directly as the ticket-read dependency; the top-level runtime/router no longer exposes the concrete `*services.SLAService`

- `customer`
  - `services/CustomerService` is already a lightweight facade over `modules/customer/application.Service`
  - The HTTP handler consumes only request/response DTOs and compatibility methods; core business has sunk into module application + infra repository
  - The `app/server` runtime no longer exposes `*services.CustomerService` fields, and customer handler assembly has switched directly to `modules/customer/delivery.NewHandlerService(db)`, no longer relaying through the legacy facade
  - Conclusion: the handler main path sits directly on the module; the old facade mainly keeps DTO compatibility for historical callers

- `automation`
  - `services/AutomationService` has sunk the core triggers and execution queries into `modules/automation/application.Service`
  - The legacy service mainly retains event bus subscriber registration, test-compatibility helpers, and module assembly
  - The `app/server` runtime/router no longer exposes `*services.AutomationService` fields, and automation handler assembly has switched directly to `modules/automation/delivery.NewHandlerService(db)`
  - Conclusion: the handler main path sits directly on the module; the old service mainly retains the event bus subscriber and test-compatibility glue

- `knowledge`
  - `modules/knowledge/application` exists, but previously lacked a Gorm repository and a handler-facing delivery contract
  - The old `services/KnowledgeDocService` mainly carries CRUD and tag/category DTOs and fits being turned into a module facade
  - The `app/server` runtime no longer exposes `*services.KnowledgeDocService` fields; knowledge handler assembly has switched directly to `modules/knowledge/delivery.NewHandlerServiceWithProvider(db, ...)`, no longer relaying through the legacy facade
  - `modules/knowledge/infra` has filled in the Gorm persistence repository for `KnowledgeIndexJob`, and module delivery now supports runtime-injected providers
  - Conclusion: knowledge's handler/runtime main paths both sit close to the module; the legacy facade retains only historical-caller compatibility

- `suggestion`
  - `modules/suggestion/application`, `contract`, `infra`, and `delivery` have landed
  - handler/runtime already consumes `modules/suggestion/delivery.HandlerService` directly
  - The old `services/SuggestionService` is deleted; DTO aliases and helpers have all sunk into module application / contract
  - Conclusion: `suggestion` has completed main-path closeout and finished by deleting the legacy service

- `gamification`
  - `modules/gamification/application`, `contract`, `infra`, and `delivery` have landed
  - handler/runtime already consumes `modules/gamification/delivery.HandlerService` directly
  - The old `services/GamificationService` is deleted; points / badge logic and DTO aliases have all sunk into module application / contract
  - Conclusion: `gamification` has completed main-path closeout and finished by deleting the legacy service

## Grouped by migration maturity

### A. A clear module facade already exists

- `ticket`
- `agent`
- `analytics`
- `customer`
- `automation`
- `knowledge`
- `suggestion`
- `gamification`

Common traits of these modules:

- `modules/*/application` already serves as the core business entrypoint
- Old `services/*` mostly carry compatibility wrappers, DTO mapping, and runtime glue
- They suit defining the "single entrypoint" first and freezing new logic in old services

### B. Module adapter wired in, but runtime hardening still needed

- websocket/runtime paths related to `conversation`

Common traits of these modules:

- The module exists
- Key persistence entrypoints have switched to the module delivery adapter
- Runtime objects such as the connection hub, notifications, and transfer coordination still need to stay behind narrow interfaces
- The follow-up focus is avoiding reintroducing business direct-write paths inside the realtime runtime, so the completed migration does not regress

### C. Multiple implementations coexist; the default main path must be decided first

- Atypical `services -> modules` capabilities such as `voice`

Traits:

- They do not fully follow this round's `services -> modules` migration template
- The next step looks more like boundary tightening and runtime assembly cleanup than simple call-site replacement

## Recommended migration order

1. `ticket`
2. `agent`
3. `analytics`
4. `customer`
5. `automation`
6. `knowledge`
7. `suggestion`
8. `gamification`
9. Continuously harden `conversation / websocket runtime`
10. Expand scorecard coverage

## Current high-risk points

- The same business capability has multiple entry layers at once: handler interface, legacy service, module service, delivery adapter
- Old services still mix in runtime state and side effects, preventing simple replacement
- The `ai` path still has multiple provider / enhanced implementations; without continued narrowing, the runtime contract could fork again
- `conversation`'s main persistence entrypoint is closed out, but the realtime connection hub remains a core runtime object; business write paths must keep being kept from flowing back
- Without continued guarding, un-modularized but thin legacy handler services could spread concrete types back into the `app/server` runtime/router

## Next-step suggestions

- Keep holding the line on the `conversation` runtime write paths; avoid bypassing module contracts
- Clarify which of the remaining `services/*` types are runtime state holders / event bus glue / historical compatibility entrypoints
- Split minimal task packages for capabilities still in `legacy` and record them in the scorecard in sync

## Confirmed entrypoint rules

- `ticket`
  - HTTP handler entrypoint: `modules/ticket/delivery.HandlerService`
  - Old `services/TicketService` status: deleted
- `agent`
  - HTTP handler entrypoint: `modules/agent/delivery.HandlerService`
  - Old `services/AgentService` role: compatibility facade + runtime state holder
- `analytics`
  - HTTP handler entrypoint: `modules/analytics/delivery.HandlerService`
  - Old `services/StatisticsService` role: compatibility facade + event bus subscriber / DTO mapping glue
- `customer`
  - HTTP handler entrypoint: `modules/customer/delivery.HandlerService`
  - Old `services/CustomerService` role: compatibility facade + DTO mapping + module application assembly
- `automation`
  - HTTP handler entrypoint: `modules/automation/delivery.HandlerService`
  - Old `services/AutomationService` role: compatibility facade + event bus subscriber + module application assembly
- `knowledge`
  - HTTP handler entrypoint: `modules/knowledge/delivery.HandlerService`
  - Runtime assembly entrypoint: `modules/knowledge/delivery.NewHandlerServiceWithProvider(db, ...)`
  - Old `services/KnowledgeDocService` role: compatibility facade + module application / repository assembly
- `suggestion`
  - HTTP handler entrypoint: `modules/suggestion/delivery.HandlerService`
  - Runtime assembly entrypoint: `modules/suggestion/delivery.NewHandlerService(db)`
  - Old `services/SuggestionService` status: deleted
- `gamification`
  - HTTP handler entrypoint: `modules/gamification/delivery.HandlerService`
  - Runtime assembly entrypoint: `modules/gamification/delivery.NewHandlerService(db)`
  - Old `services/GamificationService` status: deleted
- `routing / session transfer`
  - HTTP handler entrypoint: `modules/routing/delivery.HandlerService`
  - Old `services/SessionTransferService` status: deleted
- `conversation / websocket runtime`
  - Websocket persistence entrypoint: `modules/conversation/delivery.WebSocketMessageWriter`
  - `services/WebSocketHub` role: runtime connection hub; no longer defines its own conversation-private persistence interface
- `ai`
  - HTTP handler entrypoint: `modules/ai/delivery.HandlerService`
  - `AIAssembly` role: explicitly separates the handler-facing AI contract from the runtime-facing `RuntimeService`

## Confirmed compatibility responsibility boundaries

- `services/AgentService`
  - Allowed to retain: compatibility entrypoints for old callers, a few compatibility methods
  - Must not add: new direct HTTP handler dependencies, core business write paths outside `modules/agent/application.Service`
- `services/StatisticsService`
  - Allowed to retain: compatibility entrypoints for old callers, event bus subscription registration, DTO mapping, statistics background job scheduling
  - Must not add: new direct HTTP handler dependencies, main statistics read/write paths that bypass `modules/analytics/application.Service`
- `services/CustomerService`
  - Allowed to retain: compatibility entrypoints for old callers, DTO mapping, assembly of `modules/customer/application.Service` and repositories
  - Must not add: new direct HTTP handler dependencies, core customer read/write paths that bypass `modules/customer/application.Service`
- `services/AutomationService`
  - Allowed to retain: compatibility entrypoints for old callers, event bus subscriber registration, test helper methods, assembly of `modules/automation/application.Service`
  - Must not add: new direct HTTP handler dependencies, main trigger and execution-record entrypoints that bypass `modules/automation/application.Service`
- `services/KnowledgeDocService`
  - Allowed to retain: compatibility entrypoints for old callers, DTO mapping, compatibility assembly of `modules/knowledge/application.Service` and repositories
  - Must not add: new direct HTTP handler dependencies, main document CRUD entrypoints that bypass `modules/knowledge/application.Service`
- `services/WebSocketHub`
  - Allowed to retain: connection management, broadcast, protocol message dispatch, runtime glue for AI/transfer
  - Must not add: new conversation-private persistence interfaces, message persistence paths that bypass `modules/conversation/delivery.WebSocketMessageWriter`
- legacy AI types under `services`
  - Allowed to retain: provider implementations, the enhanced chain, local runtime coordination
  - Must not add: new direct HTTP handler dependencies, re-exposing wide interfaces like the global `AIServiceInterface`

## Current automated guards

- `scripts/check-module-boundaries.sh`
  - Verifies that handler constructors for `ticket` / `agent` / `analytics` / `customer` / `automation` / `knowledge` / `routing` / `ai` must depend on `modules/*/delivery.HandlerService`
  - Verifies that router/runtime injection types for these eight modules must stay on the handler-facing contract, that the `knowledge` runtime must be assembled directly through module delivery, and that the `conversation` websocket persistence entrypoint must go through the module delivery adapter
  - Verifies that thin handler dependencies such as `satisfaction` / `macro` / `app integration` / `custom field` / `shift` must stay on handler-local contracts, preventing the `app/server` top-level assembly from regressing to expose concrete legacy services
  - Verifies that handler/runtime entrypoints for `suggestion` / `gamification` must stay on module delivery contracts
  - Verifies that newly narrowed points such as `workspace` / `websocket transfer` must depend on interfaces like `WorkspaceOverviewReader` and `SessionTransferRuntime`, and forbids the `app/server` runtime/router from regressing to expose several concrete legacy services
  - Purpose: lock down the entrypoints of completed migrations first, avoiding regression to handlers wired directly to the old concrete services
