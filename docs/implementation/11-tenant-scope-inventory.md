# 11 Tenant Scope Inventory

This file records the current inventory results of `11-tenant-auth-and-audit` during the `T1 tenant-and-workspace-boundaries` phase.

> Note: when this inventory was first written, the core business tables did not yet commonly carry scope columns; **the 2026-10-08 documentation consistency audit rewrote it wholesale against the current code** — core tables such as Session/Ticket/Message/Customer/Agent now all carry `tenant_id`/`workspace_id` columns and composite indexes, and the workspace/statistics repository layers filter by request context. The authoritative view of current progress is `11-tenant-auth-and-audit.md` T1.

## Currently Confirmed Scope Sources

- JWT / auth subject
  - `platform/auth/claims.go`
  - Currently normalized out of the token:
    - `tenant_id`
    - `workspace_id`
    - `token_type`
    - `principal_type`
  - Subject / scope normalization happens inside `platform/auth.AuthMiddleware`: `extractClaims` writes the normalized claims (`tenant_id` / `workspace_id` / `roles`, etc.) into the gin context; the request-level scope headers (`X-Tenant-ID` / `X-Workspace-ID`) are validated by `platform/auth.EnforceRequestScope()`, preventing callers from widening or conflicting with the scope carried by the token itself (the former standalone `SubjectFromGin` / `ScopeFromGin` read entries were removed with the 2026-09-20 dead code cleanup)
  - Scope is projected into the request context via `platform/auth/context.go` for repository-layer use (injected by `gin_middleware.go`)

- AI / knowledge provider default configuration
  - `config.WeKnora.TenantID`
  - `config.WeKnora.KnowledgeBaseID`
  - Defaults currently come from configuration, not from request-level or workspace-level overrides

- knowledge provider namespace resolution
  - `platform/knowledgeprovider.ResolveNamespace(defaultTenantID, defaultKnowledgeID, tenantID, knowledgeID)`
  - Current namespace semantics:
    - Take the request tenant / knowledge first
    - Fall back to the default configuration when absent
    - If knowledge is empty but tenant is not, use the tenant as the knowledge id

- event bus
  - `platform/eventbus.BaseEvent`
  - The event model reserves `EventTenantID`
  - But there is not yet a unified "all key events must carry tenant" constraint

## Current Persistence and Filtering Status (verified against code on 2026-10-08)

- Core business tables now commonly carry explicit tenant / workspace columns
  - `Session` (`models/models.go:237-238`), `Ticket` (:131-132), `Message` (:262-263), `Customer` (:88-89), `Agent` (:107-108), and 16+ other models all carry `TenantID` + `WorkspaceID` composite indexes (`idx_sessions_scope`, etc.)
  - The database-level boundary is established, no longer relying only on calling conventions
  - **Remaining gap: the `User` table has no tenant column** (user principals are globally unique by design, logging in across tenants)

- The workspace console now filters by scope
  - `modules/workspace/infra/gorm_repository.go:24-28,87-100`: queries filter by the tenant / workspace scope of the request context
  - The two entry rules — `workspace` scope cannot escape the `tenant` scope, and the default `global` view is allowed only for internal principals — remain fixed at the entry surface
  - Note: the early `scope_enforced` / `scope_warning` response fields no longer exist in any contract — isolation is now actually carried by repository-layer filtering, no longer declared via "entry pass-through visibility"

- statistics / analytics now filter by scope
  - `modules/analytics/infra/gorm_repository.go:188-198,316-347`: statistics queries filter by context scope
  - This chain is anchored by the `analytics/infra/gorm_repository_scope_integration_test.go` integration test (`integration && sqlite_integration` tags)
  - `DailyStats` remains a system-level read model (no scope fields in `models.go`, consistent with `current-architecture.md`)

- Customer master data now carries tenant columns
  - `models/models.go:88-89`; the create chain's scope entry rules (workspace does not escape tenant, global only for internal) remain in effect

## Questions That Can Currently Be Answered

- "Where do tenant / workspace scopes come from now?"
  - Auth token claims (normalized via `platform/auth` + projected into the request context)
  - WeKnora default configuration
  - knowledge provider request namespace
  - The event bus's optional tenant field

- "Which core capabilities already have scope infrastructure?"
  - auth subject / scope reading
  - knowledge provider namespace resolution
  - event bus tenant field reservation
  - Core business table columns + workspace/statistics repository-layer filtering (with integration test anchors)

- "Which core capabilities are still not truly tenant-isolated?"
  - `User` master data (no tenant column, global by design)
  - The `DailyStats` aggregation table (system-level read model, no scope columns by convention)
  - The event bus tenant field is still a reservation, without a chain-wide mandatory constraint

## Next-Step Suggestions

- Enforce tenant chain-wide on the event bus: turn "all key events must carry tenant" from a reservation into a gate
- Keep aligning the scope semantics of knowledge retrieval with business master data (knowledge already has namespaces; the ticket/conversation/customer chains already have columns; the remainder is unifying reconciliation-style read surfaces)
