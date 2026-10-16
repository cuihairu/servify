> **English** | [中文](current-architecture.md)

# Current Architecture Analysis

This document records the real architecture state of the current repo, used to bridge the target design in the root `ARCHITECTURE.md` and the implementation backlog under `docs/implementation/`.

> Snapshot verification: 2026-10-04 (V1.0 convergence refactor B0 batch filed; re-verified and updated after the `v1.0.0` release, aligning statistics closeout status, dependency graph, customer module layering with the day's code). The previous version's statement "the services directory still exists" is outdated: the old `internal/services` was removed entirely in P3-2 with references zeroed, business logic either sinks down to `modules/*/application` with the migration or exists as thin-shell modules (no `domain` layer).

## Conclusions

The current Servify is a **modular monolith that has essentially completed the services→modules migration**:

- The backend is still a single-process-first Go modular monolith, not a microservices architecture.
- The main entry point has sunk from `cmd/server` down to `internal/app/bootstrap` and `internal/app/server`.
- Business capabilities are all closed out under `modules/*`, of which 14 modules have the full `domain/application/infra/delivery` (agent, ai, analytics, assist, automation, conversation, customer, knowledge, quality, routing, ticket, translation, voice, webhook), plus 13 thin-shell modules (no `domain` layer: api_key, app_integration, auth, custom_field, email, gamification, macro, push, satisfaction, shift, sla, suggestion, workspace), whose business logic is still carried by legacy handler/service.
- `handlers` mostly already depend on module delivery contracts or handler-local contracts, rather than directly depending on concrete legacy services.
- `statistics` has closed out into the `analytics` module (V1.0 B2-3, `modules/analytics/delivery/statistics_*.go`); the top-level `internal/handlers` no longer retains the statistics business surface (the unit test file from the migration source was renamed to `session_transfer_unit_test.go` in the 2026-10-09 closeout; the file content always tested session transfer, the statistics prefix was a migration-period misattachment).

In other words, the current system is neither the pure target state nor the old architecture; it is a post-migration architecture with boundary guards. The V1.0 convergence refactor (B0–B4) was completed and `v1.0.0` released on 2026-10-04: 27 modules, no more added; the core 7 modules (conversation/ticket/routing/ai/knowledge/customer/agent) are polished around the Conversation central model; the 13 thin-shell modules' narrative is downgraded to sub-capabilities.

## Repo shape

Main directory responsibilities:

| Path | Current responsibility |
| --- | --- |
| `apps/server` | Go backend, the main battleground for the current architecture redesign |
| `apps/admin` | Admin console, UmiJS + Ant Design Pro |
| `apps/website` | Static marketing site |
| `sdk` | TypeScript SDK workspace, split into core, transport, framework binding |
| `docs` | Doc site, architecture explanations, acceptance, implementation backlog, runbooks |
| `infra` / `deploy` | Local compose, observability, deployment helper assets |
| `scripts` | Local and CI checks, acceptance scripts, generated-artifact governance |

## Backend runtime

The current HTTP backend startup chain can be understood in four layers:

1. `apps/server/cmd/server`
   - Retained as a thin entry point.
   - Handles startup arguments, error exit, startup order, and graceful shutdown.

2. `internal/app/bootstrap`
   - Builds config, logger, database, Redis, event bus, embedding provider.
   - Manages worker, HTTP runtime, server, and shutdown hooks.
   - Is the bootstrap root for process-level dependencies.

3. `internal/app/server`
   - Builds the HTTP runtime service graph.
   - Responsible for wiring AI, realtime, conversation, routing, voice, business handler services, metrics.
   - The router is split and registered by auth / management / public / realtime / static.

4. `internal/handlers` and `internal/modules/*/delivery`
   - Handlers handle HTTP DTOs, request parsing, status codes, and use case calls.
   - Delivery contracts are the main boundary handlers face toward business modules.

## Module layering

`apps/server/internal/modules` currently has **27 modules** (verified 2026-10-04; no more added from V1.0 convergence onward):

Full layering (14, with `domain`):

| Module | Layers |
| --- | --- |
| `agent` | `domain` / `application` / `infra` / `delivery` |
| `ai` | `domain` / `application` / `infra` / `delivery` |
| `analytics` | `domain` / `application` / `infra` / `delivery` / `contract` |
| `assist` | `domain` / `application` / `infra` / `delivery` |
| `automation` | `domain` / `application` / `infra` / `delivery` |
| `conversation` | `domain` / `application` / `infra` / `delivery` |
| `customer` | `domain` / `application` / `infra` / `delivery` / `api` |
| `knowledge` | `domain` / `application` / `infra` / `delivery` |
| `quality` | `domain` / `application` / `infra` / `delivery` |
| `routing` | `domain` / `application` / `infra` / `delivery` / `contract` |
| `ticket` | `domain` / `application` / `infra` / `delivery` / `contract` / `orchestration` |
| `translation` | `domain` / `application` / `infra` / `delivery` |
| `voice` | `domain` / `application` / `infra` / `delivery` / `provider` |
| `webhook` | `domain` / `application` / `infra` / `delivery` |

Thin-shell modules (13, no `domain` layer; business logic still carried by legacy handler/service, narrative downgraded to sub-capabilities after V1.0 convergence, code retained):

| Module | Layers |
| --- | --- |
| `api_key` | `application` / `delivery` / `infra` |
| `app_integration` | `application` / `delivery` / `infra` |
| `auth` | `application` / `delivery` (real logic in `platform/auth` + `handlers/auth_*`) |
| `custom_field` | `application` / `delivery` / `infra` |
| `email` | `application` / `delivery` / `infra` |
| `gamification` | `application` / `delivery` / `infra` / `contract` |
| `macro` | `application` / `delivery` / `infra` |
| `push` | `application` / `delivery` / `infra` |
| `satisfaction` | `application` / `delivery` / `infra` |
| `shift` | `application` / `delivery` / `infra` |
| `sla` | `application` / `delivery` / `infra` |
| `suggestion` | `application` / `delivery` / `infra` / `contract` |
| `workspace` | `application` / `delivery` / `infra` |

Target dependency direction (`internal/services` removed entirely in P3-2, the diagram no longer has a services layer):

```text
handlers -> modules/*/delivery
app/server -> modules/*/delivery
modules/*/delivery -> modules/*/application
modules/*/application -> modules/*/domain|infra
```

There are already `scripts/check-module-boundaries.sh` and `scripts/module-boundaries.rules` guarding key dependencies; already-closed modules should not be reconnected to concrete legacy services.

## Main paths already closed out

The following capabilities already have a relatively stable module delivery main path or controlled runtime contract:

- `ticket`
- `agent`
- `analytics`
- `routing / session transfer`
- `conversation / websocket runtime`
- `ai`
- `customer`
- `automation`
- `knowledge`
- `suggestion`
- `gamification`

This does not mean all legacy code has been deleted, but rather that the main entries for handler/router/runtime already have clear contracts and boundary checks.

## Areas still in transition

Areas that currently need continued careful handling:

| Area | Status | Risk |
| --- | --- | --- |
| `statistics` old handler | Closed out: `modules/analytics/delivery/statistics_handler.go` / `statistics_export_handler.go` (V1.0 B2-3) | No residual risk; migration-source tests renamed and relocated (2026-10-09) |
| `voice` | Highly modularized (full layering), but not the typical `services -> modules` migration form | provider, media, protocol, business state easily mix together; V1.0 freezes it as the extension boundary |
| `realtime` | The WebSocket hub is still a runtime core object | The connection runtime and business persistence boundary must continue to be held |
| `AI / Knowledge` | provider abstraction and mock/containerized acceptance closed out (P1-1); only real-mode acceptance is blocked on external credentials | Interface success does not mean the real provider main path was hit |
| `storage / uploads` | Currently has a local provider; code already annotates multi-node limitations | Multi-instance deployment needs an object storage boundary |
| `DailyStats` global aggregate | System-level aggregate tables like `DailyStats` have no tenant/workspace dimensional split spec yet | Aggregate spec ambiguity under multi-tenant semantics (see `tenant-workspace-boundaries.md`) |

## Document state sources

Read in the following order when judging architecture going forward:

1. `docs/current-architecture.md`
   - The current real architecture snapshot.
2. `ARCHITECTURE.md`
   - The target architecture and long-term design principles.
3. `docs/architecture-redesign-plan.md`
   - The architecture redesign plan (services→modules migration-period artifact, archived only).
4. `docs/implementation/10-service-to-module-migration.md`
   - The services to modules migration plan.
5. `docs/implementation/10-migration-scorecard.md`
   - Current module migration completeness.
6. `docs/implementation/10-module-boundaries.md`
   - Migration-period dependency rules.
7. `todo.md` and `docs/delivery-priorities.md`
   - Current delivery priorities and recovery points.
