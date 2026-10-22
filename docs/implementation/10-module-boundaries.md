# 10 Module Boundaries

> Status (2026-10-04 doc reconciliation): after the migration completed the `services` layer no longer
> exists; read the three-layer responsibility descriptions in this doc as migration-period archives.
> The current dependency direction follows current-architecture.md,
> while the executable rules remain `scripts/module-boundaries.rules` (still in force).

This file defines the responsibilities and dependency directions of the `handlers`, `services`, and `modules` layers during the migration phase, supporting boundary governance for M5.

Executable rule sources:

- scripts/module-boundaries.rules
  - This is the rule list for the current boundary check
- scripts/check-module-boundaries.sh
  - This is the rule executor

Maintenance conventions:

- When adding a newly closed-out module, update `scripts/module-boundaries.rules` first
- The doc's "currently locked boundaries" should stay consistent with the rule list
- If the doc and the rules conflict, the rule list and CI results win; backfill the doc afterwards

## Target rules

- `handlers/*`
  - Depend only on `modules/*/delivery` contracts, a few platform interfaces, and request/response DTOs
  - Do not depend directly on concrete legacy services as the default business entrypoint
- `app/server/*`
  - Owns runtime assembly
  - May hold legacy concrete services, but HTTP injection should prefer exposing `modules/*/delivery` contracts
  - Route registration and runtime assembly should be split into files by domain or access surface, avoiding re-concentration into a single giant entrypoint file
- `services/*`
  - Retained temporarily only when unmigrated runtime glue, state synchronization, or side-effect assembly genuinely remains
  - Once closed out and no longer serving a necessary role, delete it outright instead of letting it grow into the default business center
- `modules/*/application`
  - Owns new core business logic
- `modules/*/delivery`
  - Owns handler-facing / runtime-facing adapters and contracts

## Allowed dependency directions

- `handlers -> modules/*/delivery`
- `app/server -> modules/*/delivery`
- `app/server -> services/*`
- `services/* -> modules/*/application`
- `services/* -> modules/*/delivery`
- `modules/*/delivery -> modules/*/application`
- `modules/*/application -> modules/*/domain|infra`

## Patterns forbidden from spreading further

- `handlers -> *services.ConcreteType`
- `handlers -> gorm.DB`
- `handlers -> modules/*/application` assembling business logic directly
- `services/*` becoming the default handler entrypoint again for new requirements
- The runtime adding another legacy entrypoint parallel to an existing module contract
- Re-piling cross-domain assembly logic into a single `router.go` or `runtime.go` under `app/server`

## Currently locked boundaries

- `ticket`
  - The HTTP entrypoint must go through `modules/ticket/delivery.HandlerService`
- `agent`
  - The HTTP entrypoint must go through `modules/agent/delivery.HandlerService`
- `analytics`
  - The HTTP entrypoint must go through `modules/analytics/delivery.HandlerService`
- `routing / session transfer`
  - The HTTP entrypoint must go through `modules/routing/delivery.HandlerService`
- `ai`
  - The HTTP / health / metrics entrypoints must go through `modules/ai/delivery.HandlerService`
- `customer`
  - The HTTP entrypoint must go through `modules/customer/delivery.HandlerService`
- `automation`
  - The HTTP entrypoint must go through `modules/automation/delivery.HandlerService`
- `knowledge`
  - The HTTP entrypoint must go through `modules/knowledge/delivery.HandlerService`
- `suggestion`
  - The HTTP entrypoint must go through `modules/suggestion/delivery.HandlerService`
- `gamification`
  - The HTTP entrypoint must go through `modules/gamification/delivery.HandlerService`
- `conversation`
  - The websocket persistence entrypoint must go through `modules/conversation/delivery.WebSocketMessageWriter`
- handler-local contracts
  - For thin capabilities not yet fully modularized — `satisfaction`, `macro`, `app integration`, `custom field`, `shift`, `sla` — the HTTP handler and router/runtime surface must stay on handler-local contracts, avoiding regression to concrete legacy services

Corresponding rules:

- The `handler` / `runtime` rules are recorded in `scripts/module-boundaries.rules`
- The websocket persistence rule for `conversation` and the dual-entrypoint rules for `AIAssembly` are also recorded in the same rule file
- `scripts/check-module-boundaries.sh` additionally import-scans non-test handlers, forbidding direct connections to `modules/*/application`, `modules/*/infra`, and `gorm`

## Legacy Service Freeze Policy

- Capabilities already in `stabilized`:
  - legacy `services/*` only allows necessary runtime glue, state synchronization, and side-effect assembly changes
  - New business logic goes into `modules/*/application` first
  - New handler/router wiring must not regress to concrete services
  - If a legacy service no longer has a necessary role, delete it rather than keeping an empty-shell facade
- Capabilities still in `legacy`:
  - Temporary changes are allowed, but every addition of business logic should be annotated with its target module

## Retirement conditions

A legacy service can enter the "retirement candidate" state only if at least the following hold:

1. HTTP handlers all depend on `modules/*/delivery`
2. Runtime injection no longer needs to expose concrete legacy types to the router
3. Key compatibility responsibilities have been absorbed by adapters or the platform layer
4. Tests cover the key behaviors before and after the migration
5. The scorecard status reaches `stabilized` and stays regression-free for a period
