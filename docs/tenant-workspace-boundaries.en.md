> **English** | [中文](tenant-workspace-boundaries.md)

# Tenant And Workspace Boundaries

This document records the ownership relationships and isolation semantics of core objects in the current repo, as the baseline inventory for `11 / T1 tenant-and-workspace-boundaries`.

## Current minimum ownership model

- `tenant`
  - Represents the deployment-level business tenant boundary
  - Already present in config and provider parameters, e.g. the knowledge provider's `tenant_id`
  - Core business tables generally carry `tenant_id` / `workspace_id` (per-object ownership below),
    residual unlanded surfaces at the end-of-file list

- `workspace`
  - Represents an operational or management space under a tenant
  - `workspace.read` already appears in permission semantics
  - The current repo has a workbench overview capability but has not yet formed a standalone persistent `workspace` model

Supplementary notes:

- The current main business model and main operational aggregation paths can largely answer "which tenant/workspace it belongs to"
- What is not fully closed is mainly a few legacy aggregation tail items, plus the question of whether system-level global aggregate tables like `DailyStats` need tenant/workspace dimensional splitting

## Core object ownership status

### Objects directly or implicitly belonging to workspace / tenant

- `Ticket`
  - Explicit `tenant_id` / `workspace_id` added
  - The `ticket` repository main query, stats, and creation paths filter by context by default

- `Session`
  - Explicit `tenant_id` / `workspace_id` added
  - The `conversation` repository and runtime adapter filter by context by default

- `Message`
  - Explicit `tenant_id` / `workspace_id` added
  - The `conversation` repository, customer activity, and analytics stats filter by context

- `TransferRecord`
  - Explicit `tenant_id` / `workspace_id` added
  - The `routing` repository filters by context by default

- `WaitingRecord`
  - Explicit `tenant_id` / `workspace_id` added
  - The `routing` repository filters by context by default

- `Customer`
  - Explicit `tenant_id` / `workspace_id` added
  - The `customer` repository closes off user reads, lists, stats, and activity queries via extension table join
  - `GetCustomerActivity` now has an integration test added, confirming the recent sessions / tickets / messages aggregation chains all filter by request scope, not dragging the same customer_id's history from other workspaces into the current context
  - The stats entry `CustomerHandler.GetCustomerStats` now has a scoped regression test added, and fixes the `created_at` condition in legacy stat queries under the `users/customers` join that lacked a table prefix, avoiding ambiguous column errors in scoped customer stats during multi-table aggregation

- `Agent`
  - Explicit `tenant_id` / `workspace_id` added
  - The `agent` repository and transfer runtime load sync paths filter by context
  - The stats entry `AgentHandler.GetAgentStats` now has a scoped regression test added, confirming the legacy stats entry does not mix in other agent main data across workspaces

- `KnowledgeDoc`
  - Provider layer supports `tenant_id`
  - Treated as a tenant/workspace scoped resource, filtering by context by default

- `CustomField`
  - Explicit scope fields added
  - service/repository filter by context by default

- `SLAConfig`
  - Explicit scope fields added
  - service filters by context by default

- `SLAViolation`
  - Explicit `tenant_id` / `workspace_id` added
  - The `sla` service's violation creation, list, resolution, stats, and monitoring scan paths filter by context by default

- `AppIntegration`
  - Explicit scope fields added
  - service filters by context by default

- `ShiftSchedule`
  - Explicit `tenant_id` / `workspace_id` added
  - The `shift` service's creation, list, update, delete, and stats paths filter by context by default

- `Macro`
  - Explicit `tenant_id` / `workspace_id` added
  - The `macro` service's list, creation, update, delete, and application entries filter by context by default

- `CustomerSatisfaction` / `SatisfactionSurvey`
  - Explicit `tenant_id` / `workspace_id` added
  - The `satisfaction` service's survey send, response, list, stats, update, and delete paths filter by context by default
  - `GetSatisfactionStats` now has a scoped regression test added, confirming the category stats and trend data aggregation views do not mix in satisfaction records from other workspaces

- `SuggestionService`
  - Depends on already-scoped `Ticket` / `KnowledgeDoc`
  - Similar ticket and knowledge base candidate queries now filter by request context by default, avoiding cross-workspace suggestion leakage

- `GamificationService`
  - Leaderboard aggregation now filters `Agent` / `Ticket` / `CustomerSatisfaction` by request context
  - Avoids cross-workspace aggregation of agent performance and satisfaction data

- `MessageRouter` compatibility persistence path
  - Now extracts `tenant_id` / `workspace_id` from `UnifiedMessage.Metadata` and writes to `Session` / `Message`
  - The same `sessionID` attempting cross-workspace reuse is directly refused for persistence, avoiding the old compatibility path crossing sessions
  - For existing already-scoped `session`s, subsequent messages inherit the session's known scope even if they do not repeat the scope metadata, avoiding the compatibility path writing unscored records
- `routing -> agent` transfer runtime sync
  - `ApplySessionTransfer` now explicitly passes through the current request `context`, avoiding the compatibility layer writing agent load/runtime state with an unscored `context.Background()` after session transfer

## Current runtime context

JWT claims already support pass-through:

- `tenant_id`
- `workspace_id`

and they are projected into the request context by the auth middleware, for subsequent:

- Data access filtering
- Audit log dimension enrichment
- Provider call pass-through
- Background task isolation

Also, these two fields now enter the `request context`, so the service / repository layer can read them directly without depending on the gin transport context.

## Current isolation semantics

- Management-plane and service-plane requests can carry `tenant_id` / `workspace_id` context
- Protected routes on the management and service planes now uniformly intercept explicit scope selectors:
  - `agent` / `end_user` are not allowed to additionally declare `tenant_id` / `workspace_id` in the request beyond the token's
  - `admin` / `service` may only narrow scope via request when the token has no explicit scope
  - Conflicting scope values in header / query cause the request to be directly refused
- Audit logs record these two fields
- The following objects have begun explicit scoping and filter by context by default:
  - `KnowledgeDoc`
  - `CustomField`
  - `SLAConfig`
  - `SLAViolation`
  - `AppIntegration`
  - `ShiftSchedule`
  - `Macro`
  - `CustomerSatisfaction` / `SatisfactionSurvey`
  - `SuggestionService` candidate queries
- The following conversation-chain objects now have explicit scope fields, filtering by context in the `conversation/routing` repositories by default:
  - `Session`
  - `Message`
  - `TransferRecord`
  - `WaitingRecord`
- `Ticket` now has explicit scope fields, filtering by context in the `ticket` repository's main query, stats, and creation paths
- `Customer` now has explicit scope fields; the `customer` repository closes off user reads, lists, stats, and activity queries via the `customers` extension table join
- `Agent` now has explicit scope fields, filtering by context in the `agent` repository's creation, read, list, and stats paths
- `WorkspaceService`, `analytics` module aggregation repositories, `gamification` leaderboard aggregation, and the agent transfer load sync paths have begun filtering already-scoped main data by context
  - The `customer` module's activity aggregation chain now has regression tests added, confirming the `recent_sessions` / `recent_tickets` / `recent_messages` history views do not mix in data from other workspaces into the current customer timeline via legacy join
  - The `recent_sessions` association chain in `WorkspaceService.GetOverview` now requires `sessions -> tickets -> customers -> agents` to maintain the same `tenant_id/workspace_id`, avoiding legacy joins dragging customer or agent names from other workspaces into the current workbench due to dirty foreign keys
- `SLAService.ListSLAViolations`
  - Now continues to apply the current scope to `Ticket` / `SLAConfig` preload, avoiding scoped violation lists dragging ticket or SLA config details from other workspaces back into the admin plane due to dirty foreign keys
- `SLAService.CheckSLAViolation` / `CreateSLAViolation`
  - Now re-validates `Ticket` and `SLAConfig` by the current scope within the service, no longer trusting externally passed-in `Ticket` / `SLAViolation` structs
  - Avoids callers triggering violation detection/persistence with another workspace's ticket or config object, causing cross-workspace violation record writes
- `SLAService.GetSLAStats`
  - When statting violations by priority / customer level, now requires `sla_violations -> sla_configs` to maintain the same `tenant_id/workspace_id`
  - Avoids scoped stats dragging other workspaces' priority or customer-level dimensions into the current report due to dirty `sla_config_id` associations
  - `TrendData` now has a scoped regression test added, confirming the recent 7-day SLA compliance trend does not mix in other workspaces' same-day ticket or violation counts into the current view
- `SatisfactionService`
  - `CreateSatisfaction` / `GetSatisfaction` / `ListSatisfactions` / `GetSatisfactionByTicket` / `UpdateSatisfaction` now uniformly use scope-aware preload
  - `Ticket` / `Customer` continue to filter by current scope; `Agent` is validated for `tenant_id/workspace_id` after association via `agents.user_id`, avoiding satisfaction lists and details echoing tickets, customers, or agent identities across workspaces
  - `ScheduleSurvey` now re-validates `ticket.ID` by the current scope within the service, avoiding callers passing another workspace's `Ticket` struct to create a survey with elevated privileges
  - The ticket preview in `GetSurveyPreviewByToken` also continues to apply scope validation to the `Agent` preload, avoiding survey token previews echoing agent names across workspaces
- `ShiftService.ListShifts`
  - Now adds `agents.user_id` + current scope validation to the `Agent` preload, avoiding shift lists backfilling agent user profiles from other workspaces into the current scheduling result
  - `ShiftService.CreateShift` now requires the target `Agent` itself to belong to the current scope, avoiding cross-workspace scheduling writes
- `MacroService.ApplyToTicket`
  - Now confirms the target `Ticket` exists under the current scope before creating the comment, avoiding the old macro service writing macro content into other workspaces or nonexistent tickets
- The `analytics` module now further avoids reading global `DailyStats` directly in scoped dashboard / time-range requests
  - `GetDashboardStats`' `AIUsageToday` / `KnowledgeProviderUsageToday` / `WeKnoraUsageToday` consume `DailyStats` only under system-level requests
  - `GetTimeRangeStats` no longer backfills global `DailyStats.AvgResponseTime` / `CustomerSatisfaction` under scoped requests; satisfaction is recomputed at the daily dimension by scoped `CustomerSatisfaction`, avoiding cross tenant/workspace leakage
  - `GetAgentPerformanceStats` now has a scoped integration test added, and average resolution time aggregation now selects the expression by SQL dialect; the compatibility layer `StatisticsService.GetAgentPerformanceStats` also has a regression test added, confirming the legacy performance leaderboard does not aggregate other workspaces' tickets into the current report
  - `GetTicketCategoryStats` / `GetTicketPriorityStats` / `GetCustomerSourceStats` and their compatibility layer `StatisticsService` entries now have scoped regression tests added, confirming legacy category/priority/source aggregation does not mix in other workspaces' tickets or customer dimensions
- Main remaining surfaces not yet uniformly scoped:
  - A few compatibility layer entries still remaining in the old `services/*`, not fully migrated to modules; however all legacy aggregation queries currently covered already have scoped regression tests and are no longer blockers for this round
  - Cross-tenant global aggregate tables like `DailyStats` are currently explicitly treated as system-level stats; `GetDashboardStats` / `GetTimeRangeStats` do not consume it under scoped reads, and `UpdateDailyStats` is now fixed to recompute global main data, no longer affected by request scope

## Next step recommendations

1. Make `T4 configuration-scopes` the focus of the next stage, clarifying the system-level and tenant-level config boundary
2. If tenant-level reporting is needed later, add daily or aggregate tables with tenant/workspace dimensions, rather than directly reusing the current system-level `DailyStats`
3. To further reduce compatibility layer risk, gradually migrate old `MessageRouter` / old service aggregation paths to the modules boundary
