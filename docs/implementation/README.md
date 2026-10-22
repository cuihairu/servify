# Servify Implementation Backlogs

This directory splits architecture implementation tasks by topic.

Purpose:

- Keep each backlog readable
- Let server, AI, SDK, and protocol integration each advance independently
- Support interrupt and resume

File descriptions:

- Overall Architecture Design
  - Macro boundaries, runtime layering, and future extension directions
- Current Architecture Analysis
  - Current real architecture snapshot, module migration status, and transition risks
- Architecture Redesign Plan
  - Architecture redesign plan (a services→modules migration-era artifact, archived only)
- [01-platform-and-runtime.md](./01-platform-and-runtime.md)
  - Platform tasks such as entry, bootstrap, router, auth, event bus, and realtime

- [02-ai-and-knowledge.md](./02-ai-and-knowledge.md)
  - provider, AI orchestration, tooling, knowledge indexing, and related tasks

- [03-business-modules.md](./03-business-modules.md)
  - conversation, routing, agent, ticket, customer, automation, analytics, voice

- [04-sdk-and-channel-adapters.md](./04-sdk-and-channel-adapters.md)
  - sdk core, web sdk, future api/app sdk reservations, channel adapters, SIP adapter
- [05-engineering-hardening.md](./05-engineering-hardening.md)
  - CI, testing pyramid, release versioning, documentation site
- [06-voice-and-protocol-expansion.md](./06-voice-and-protocol-expansion.md)
  - deeper voice protocol entries, provider landing, and more common voice protocol reservations
- [07-sdk-multi-surface.md](./07-sdk-multi-surface.md)
  - web sdk wrap-up, deeper future api/app sdk contracts, transport evolution
- [08-ai-provider-expansion.md](./08-ai-provider-expansion.md)
  - LLM/knowledge provider expansion, orchestration layer stability, AI observability
- [09-runtime-and-repo-hygiene.md](./09-runtime-and-repo-hygiene.md)
  - runtime artifact cleanup, repository hygiene, ignore policy, cross-platform dev environment convergence
- [10-service-to-module-migration.md](./10-service-to-module-migration.md)
  - migrating legacy services/handlers to the modules architecture, adapter layers, and boundary convergence
- [10-migration-inventory.md](./10-migration-inventory.md)
  - current handlers/services/modules migration inventory and priority suggestions
- [10-migration-scorecard.md](./10-migration-scorecard.md)
  - current migration completeness table and module status tracking
- [10-module-boundaries.md](./10-module-boundaries.md)
  - handlers/services/modules migration-era boundary rules and freeze policy
- [11-tenant-auth-and-audit.md](./11-tenant-auth-and-audit.md)
  - multi-tenancy, permission model, audit logging, configuration boundaries
- [12-operator-observability.md](./12-operator-observability.md)
  - tracing, metrics, logging, alerting, replay, and operational diagnostics
- [13-ai-agent-loop.md](./13-ai-agent-loop.md)
  - P0: wire up the AI agent loop (tool-calling loop + streaming), so the intelligent customer service can act in multiple steps

Status conventions:

- `[ ]` not started
- `[-]` in progress
- `[x]` done

Execution suggestions:

1. Do `01-platform-and-runtime` first
2. Then do `02-ai-and-knowledge`
3. Then split `03-business-modules`
4. Plan `04-sdk-and-channel-adapters` in parallel (web sdk is implemented; Android/iOS native
   SDKs landed with M1–M3; api/app sdk remain reservations)
5. After the first phase is cleared, enter the `05` to `08` engineering and expansion phase

Current progress (updated 2026-10-04, `v1.0.0` released):

- `01` through `08` are all cleared
- `09` completed repository hygiene and runtime artifact governance
- `10` services→modules migration is complete (`internal/services` fully removed, capped at 27
  first-level modules); the accompanying inventory / scorecard / boundaries docs are migration-era archives
- `11` and `12` have mostly landed (tenant/audit/security baseline wired into the management surface; tracing/metrics/
  alerting pass the observability baseline gate)
- `13` the AI agent loop main loop is wired (tool-calling loop + streaming + three-tool registration;
  wiring the tool Port data sources and permission checks is left over; see the
  verification note at the top of [13-ai-agent-loop.md](./13-ai-agent-loop.md))
- The V1.0 convergence batches (B0–B4) have all passed the gate; the only active status sources are
  todo.md and the acceptance matrix; for future
  evolution see "Evolution Directions"

Current reading order:

1. Read Current Architecture Analysis first to confirm the repository's real state.
2. Then read the V1.0 Convergence Plan to confirm the current product/architecture baseline.
3. The historical implementation mainline (architecture redesign plan, 10-series migration archives) serves only as migration history reference.

Companion topics:

- Release Versioning Strategy
- Testing Pyramid
- Mermaid Compatibility
- Repository Hygiene and Generated Asset Boundaries
