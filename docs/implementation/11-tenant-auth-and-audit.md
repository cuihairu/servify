# 11 Tenant Auth And Audit

Scope:

- Multi-tenancy and workspace boundaries
- Authentication and authorization convergence
- Audit logging
- Configuration hierarchy governance
- A security baseline aimed at real deployments

## T1 tenant-and-workspace-boundaries

- [-] Map the ownership relationships of tenant, workspace, agent, customer, and knowledge base
- [-] Define cross-tenant access prohibition rules
- [-] Define tenant isolation semantics for queries, writes, exports, and background jobs
- [-] Review tenant fields and indexing strategies for core models

Acceptance:

- Any core data object can clearly answer "which tenant/workspace it belongs to"

Current progress:

- The current tenant/workspace ownership status of core objects has been inventoried; see `docs/tenant-workspace-boundaries.md`
- JWT claims now support normalization and pass through `tenant_id` / `workspace_id`
- The auth middleware projects `tenant_id` / `workspace_id` into the request context for audit, provider calls, and later data filtering
- The management surface `/api/*` and the protected `/api/v1/*` now have a unified request-scope guard that rejects requests where the token scope conflicts with the header/query scope, and prevents non-admin/service principals from widening scope through requests
- `KnowledgeDoc`, `CustomField`, `SLAConfig`, and `AppIntegration` now carry explicit scope fields and filter by context by default at the service/repository layer
- `SLAViolation` now carries explicit `tenant_id/workspace_id` fields, and the `sla` service filters by context in the violation creation, listing, resolution, statistics, and monitoring scan paths
- `ShiftSchedule` now carries explicit `tenant_id/workspace_id` fields, and the `shift` service filters by context in the create, list, update, delete, and statistics paths
- `Macro` now carries explicit `tenant_id/workspace_id` fields, and the `macro` service filters by context in the list, create, update, delete, and apply entry paths
- `CustomerSatisfaction` / `SatisfactionSurvey` now carry explicit `tenant_id/workspace_id` fields, and the `satisfaction` service filters by context in the survey send, response, list, statistics, update, and delete paths
- `SuggestionService` now has context-filtered similar-ticket/knowledge-base candidate queries and no longer reads scoped `Ticket` / `KnowledgeDoc` across workspaces
- `GamificationService` leaderboard aggregation now filters `Agent` / `Ticket` / `CustomerSatisfaction` by context, avoiding cross-workspace rollups of agent performance
- `SatisfactionService.GetSatisfactionStats` now has scoped regression tests, further locking satisfaction category statistics and trend aggregation inside workspace boundaries
- `Session`, `Message`, `TransferRecord`, and `WaitingRecord` now carry explicit `tenant_id/workspace_id` fields and basic indexes, and the `conversation/routing` repositories filter by context by default
- The legacy `MessageRouter` persistence path now extracts `tenant_id/workspace_id` from `UnifiedMessage.Metadata` into `Session/Message`, and rejects reusing the same `sessionID` across workspaces
- `Ticket` now carries explicit `tenant_id/workspace_id` fields and basic indexes, and the `ticket` repository filters by context in the main query, statistics, and create paths
- `Customer` now carries explicit `tenant_id/workspace_id` fields, and the `customer` repository scopes reads, lists, statistics, and activity queries through the extended-table scope
- The `customer` module activity aggregation chain now has an integration test confirming that `recent_sessions` / `recent_tickets` / `recent_messages` keep filtering by request scope and do not mix other workspaces' customer history into the current timeline
- The compatibility layer `CustomerService.GetCustomerStats` now has scoped regression tests, and fixed a legacy customer statistics join whose `created_at` condition lacked a table prefix, avoiding ambiguous column errors in scoped statistics during multi-table aggregation
- `Agent` now carries explicit `tenant_id/workspace_id` fields, and the `agent` repository filters by context in the create, read, list, and statistics paths
- The compatibility layer `AgentService.GetAgentStats` now has scoped regression tests, confirming the legacy agent statistics entry does not mix other workspaces' agent master data
- The management surface `security/users/*` and `security/tokens/*` now additionally validate tenant/workspace ownership against the target user's associated `Agent` / `Customer` scope; cross-scope targets are uniformly treated as not found, preventing enumeration of or accidental operations on other tenants' users/tokens
- `WorkspaceService`, the `analytics` module aggregation repositories, and the agent transfer runtime load sync paths now filter scoped master data such as `Session/Agent/Ticket/Message/Customer` by context
- `analytics.GetAgentPerformanceStats` and the compatibility layer `StatisticsService.GetAgentPerformanceStats` now have scoped regression tests, and fixed the average resolution time aggregation in the analytics repository to be compatible with sqlite / postgres and other SQL dialects, so the legacy agent performance report is no longer left untested
- `analytics.GetTicketCategoryStats` / `GetTicketPriorityStats` / `GetCustomerSourceStats` and the compatibility layer `StatisticsService` entries now have scoped regression tests, further closing the cross-workspace leak risk of legacy category, priority, and customer-source aggregation
- `SLAService.GetSLAStats` trend aggregation now has scoped regression tests, confirming the last-7-day compliance view does not mix other workspaces' tickets or violation data
- The remaining gap has narrowed further from "core master data unmodeled" to "if tenant-level daily reports are needed later, system-level global rollup tables such as `DailyStats` need separately designed tenant/workspace dimension versions"; see `docs/tenant-workspace-boundaries.md`

## T2 auth-and-rbac-convergence

- [x] Inventory the current actual entries for JWT, claims, permissions, and middleware
- [x] Converge the RBAC model and the permission resolution chain
- [x] Distinguish authentication semantics for end-user, agent, admin, and service tokens
- [x] Define different authorization policies for the admin console, public interfaces, and internal interfaces
- [x] Add negative and privilege-escalation tests for key permissions

Acceptance:

- Permission decisions flow through a single testable path and do not depend on scattered handler logic

Current progress:

- JWT validation, claims normalization, role -> permission expansion, and principal kind determination have been converged into `internal/platform/auth`
- `internal/middleware` keeps only compatibility entries and no longer carries independent permission logic
- The management surface `/api/*` explicitly rejects `end_user` tokens; currently only `agent`, `admin`, and `service` are allowed
- `public` routes stay anonymous; `/api/v1` has been split into three surfaces: public realtime, management realtime/AI, and service-only metrics ingest
- Surface classification and new-route admission rules are documented; see `docs/auth-surface-policy.md`

## T3 audit-log-foundation

- [x] Define the audit event model
- [x] Cover key write operations such as ticket changes, routing assignments, configuration changes, and permission changes
- [x] Record actor, tenant, resource, before/after, and request metadata
- [x] Design the query interface and retention policy
- [x] Provide minimal traceability for sensitive operations

Acceptance:

- Key changes are traceable, meeting basic troubleshooting and compliance needs

Current progress:

- A persistent `AuditLog` model has been added and included in the application migration set
- Write requests on the management surface `/api/*` are uniformly wired to the audit middleware; successful writes record actor, principal, action, resource, request metadata, and the request payload
- `before/after` has a middleware extension point; high-value writes such as `scoped config` and `ticket` update/assign/close, `agent` online/offline/status switch, `customer` update/note/tag/token revoke, and `security` user revoke token now write fine-grained state snapshots
- A `GET /api/audit/logs` query interface has been added, supporting filtering by action/resource/principal/actor/success/time-range
- The management surface now has `GET /api/audit/logs/:id` for single-record queries, reusing the request scope constraints and supporting reading a specific audit record per tenant/workspace
- The management surface now has `GET /api/audit/logs/:id/diff`, a generic diff preview interface for field-level `before/after` changes
- The management surface now has `GET /api/audit/logs/export`, a CSV export interface reusing the list filter parameters and supporting lightweight scope-constrained export
- Audit query and retention baselines are documented; see `docs/audit-log-policy.md`
- A background audit cleanup worker is wired in, batch-deleting expired records on a default 180-day retention window
- Hot/cold tiered archiving has landed (2026-09-28): once `security.audit.archive_dir` configures an archive directory, the cleanup worker first writes a gzip JSON snapshot of the whole batch to the cold tier before deleting expired records; rows keep `prev_hash`/`entry_hash` (the cold tier stays verifiable on the same chain), and archive failure interrupts cleanup without deleting; the default empty value keeps existing behavior; see the policy in `docs/audit-log-policy.md`

## T4 configuration-scopes

- [-] Distinguish system-level, tenant-level, workspace-level, and runtime-level configuration
- [-] Define scopes for AI provider, knowledge provider, routing policy, and similar configuration
- [-] Clarify configuration loading, override, and fallback rules
- [-] Add audit and rollback constraints for configuration changes

Acceptance:

- Configuration is no longer mixed across environment variables, the database, and code defaults

Current progress:

- `docs/configuration-scopes.md` has been added, defining the system / tenant / workspace / runtime four configuration layers
- A recommended scope matrix is defined for AI provider, knowledge provider, routing policy, portal, and security baseline configuration
- A unified override order is defined: `runtime -> workspace -> tenant -> system -> code default`
- Audit and rollback constraints for configuration changes are defined, distinguishing system-level release rollback from tenant/workspace configuration restore
- `internal/platform/configscope` has been added as the unified resolver skeleton, currently covering layered resolution of portal config, OpenAI config, and knowledge provider config
- Portal public config reads, AI runtime assembly, and knowledge provider display info in the enhanced health check have switched to the resolver, instead of each stitching system default / runtime override on its own
- The `portal` / `OpenAI` / knowledge provider resolvers now have tenant/workspace provider interfaces and override order skeletons, currently preferring Dify with WeKnora compatibility retained
- Persistent `TenantConfig` / `WorkspaceConfig` models and GORM providers have been added, and `portal` tenant/workspace override reads are wired in
- The `OpenAI` / knowledge provider resolvers can now consume the same batch of persistent providers; the management surface `AI` handler main path and the `RuntimeService` main path both resolve scoped providers by request scope
- `security.session_risk` is covered by the same scoped config documentation and resolver; both the auth self-service session list and the management user security session list dynamically resolve tenant/workspace override thresholds by request scope
- Calls that carry no scope, such as `context.Background()` or anonymous websockets, still naturally fall back to system config
- The management surface now has minimal `GET/PUT` interfaces for tenant/workspace scoped config, reading and writing `portal` / `OpenAI` / knowledge provider / `session_risk` overrides
- Scoped config now has `history` / `rollback` management interfaces; config writes and restores both write `AuditLog` and keep before/after snapshots; rollback runs only when an explicit confirmation parameter is submitted
- `PUT` / `rollback` / `verify` for tenant/workspace scoped config now require `change_ref` and `reason`, supported via JSON body or `X-Change-Ref` / `X-Change-Reason` headers, which also land in the audit request metadata
- High-risk scoped config changes have advanced from "an approval number exists" to "a real approval record exists": when field risk assessment hits a high-risk provider endpoint, KB mapping, session risk threshold, or rollback operation, `PUT` / `rollback` must not only submit `approval_ref` but also resolve a recorded `scoped_config.{scope}.approve` audit event by `approval_ref + change_ref`, and the approver and executor must be different people; the interface continues to support JSON body or the `X-Approval-Ref` header
- The management surface now supports `POST /security/config/{scope}/verify/:audit_id` for writing post-execution verification conclusions for update / rollback results; besides `status` / `notes` / `evidence`, the verification body must also submit `checks` aligned with `verification_template.checks`
- Scoped config verification has converged from free text to templated check items: `passed` must add evidence, and every required check item in the template must be explicitly marked `passed`; `failed` must add notes, and at least one template check item must be marked `failed`
- `verification_template` is now further broken down by real `changed_paths` and returns `changed_paths` at the template root; individual checks also return `risk_level` and the `changed_paths` they cover, directly driving frontend or automation scripts to verify by field risk
- The current fine-grained templates cover these high-risk configuration clusters: portal public surface / locale, OpenAI endpoint / model contract, knowledge provider endpoint / knowledge mapping / health check, and session risk score / window / concurrency
- Change governance now has a unified state machine: responses additionally return `governance_status` / `governance_policy`, with current state values covering at least `awaiting_approval`, `awaiting_verification`, `verified`, `verification_failed`, `approved`, and `not_required`
- Scoped config verification now has a minimal two-person review constraint: the verification reviewer must be an authenticated operator carrying a `user_id`, and must be different from the original update / rollback executor
- The management surface now returns `change_control` / `change_risk` / `approval_policy` metadata directly in scoped config write responses, history lists, single-record details, and verify responses; `change_control` includes `approval_ref`, `change_risk` gives `risk_level` / `risk_reasons` / `changed_paths`, and `approval_policy` declares whether approval must be added, whether the approval record is persisted, the latest approval audit ID / time / approver, and other real pre-execution approval information
- Verify responses also return `source_governance_status` / `source_governance_policy`, for directly judging whether the verified source change has converged
- The management surface now returns `operation` / preview / rollback / verify / `verification_status` / `latest_verification` / `verification_template` / `verification_policy` metadata directly in scoped config history lists, single-record details, and verify responses; `verification_policy` additionally declares `checks_required`, `required_check_ids`, `template_check_count`, and failed-state check constraints, so frontend or automation scripts can render the post-execution verification flow by template
- `GET /security/config/{scope}/history` now works directly as a governance workbench data source, supporting `governance_status` / `risk_level` / `approval_status` / `verification_status` / `needs_action` filters and returning `governance_summary` aggregate counts plus `applied_filters`, for rendering the pending change queue
- Still missing are a more general cross-configuration-domain write interface, plus more complete two-person review orchestration, automated acceptance templates, and post-release verification rule orchestration

## T5 security-baseline-for-operations

- [x] Inventory high-risk interfaces and high-risk operations
- [x] Add rate limiting, permission fallbacks, and logging for critical operations
- [x] Add minimal standards for token lifecycle, key rotation, and sensitive field masking
- [x] Add a basic security checklist for externally exposed interfaces

Acceptance:

- The project has the minimal security governance skeleton needed before entering real deployment

Current progress:

- `docs/security-baseline-operations.md` has been added, inventorying the three high-risk surfaces (management / service / public) and key interfaces
- Base routes uniformly mount `RateLimitMiddlewareFromConfig`, with global and path-prefix limits keyed by socket peer IP; untrusted request headers cannot rotate identities or bypass the limiter
- Management write interfaces are confirmed to uniformly stack `AuthMiddleware`, `EnforceRequestScope`, `RequirePrincipalKinds`, `RequireResourcePermission`, and `AuditMiddleware`
- The audit middleware now uniformly masks sensitive fields such as `password`, `secret`, `api_key`, and `token`, preventing plaintext secrets from landing in audit logs
- `docs/token-lifecycle-and-key-rotation.md` has been added, defining minimal lifecycles and rotation flows for JWT / provider keys / service tokens
- `config.production.secure.example.yml` has been added, providing a production example template with rate limiting enabled, tightened CORS, and secrets injected via environment variables
- `docs/public-surface-security-checklist.md` has been added, converging the minimal review items for anonymous / externally exposed interfaces and a per-entry checklist
- Bootstrap startup security warnings have been added, warning on high-risk configurations such as the default `jwt.secret`, open CORS, disabled rate limiting, and empty provider keys
- Startup security warnings and `check-security-baseline --strict` now further cover the public / high-risk entry rate limiting baselines: if `/public/`, `/public/kb/`, `/public/csat/`, `/api/v1/auth/`, `/api/v1/ws`, `/uploads/`, `/api/v1/metrics/ingest`, or `/api/` has no dedicated path-level rate limit, an explicit warning fires at startup and at the pre-deployment validation stage
- The default development config `config.yml` now passes the `security` / `observability` strict baseline checks, but the docs make clear its purpose is still local development and it must not replace the production deployment config template
- `internal/platform/auth` now has composable token policy hooks, supporting issued-before invalidation by `iat` and minimal session version eviction by `token_version`, reserving integration points for later account-level revocation policies
- `internal/platform/auth` now has a token policy based on `users` table status and `user_auth_sessions`, wired into the router auth middleware: non-active users are rejected; setting `token_valid_after` / `token_version` invalidates old tokens; login and refresh now also carry `session_id + session_token_version`, so old tokens of the same auth session become invalid after refresh or a targeted revoke
- `/api/v1/auth/login`, `/api/v1/auth/register`, and `/api/v1/auth/refresh` now return a separate `refresh_token`; the refresh path no longer depends on a still-valid access token; the refresh token carries `token_use=refresh` and the current `session_token_version`; after a refresh the old refresh token becomes invalid immediately, and remains subject to user/session revoke, `token_valid_after`, and `token_version` constraints
- The auth issuance chain now injects a unique `jti` into access / refresh tokens; `internal/platform/auth` has added a revoke list policy, and the management surface can explicitly add a single JWT to the denylist via `POST /api/security/tokens/revoke`, invalidating it immediately before expiry
- The management surface now has `GET /api/security/tokens/revoked` for querying the revoke list, filterable by `jti` / `user_id` / `session_id` / `token_use`, and supports viewing only denylist records that have not yet expired
- The revoked token denylist is wired to a background cleanup worker that automatically purges expired revoke records by token `expires_at`, preventing long-term denylist bloat
- The agent management surface now has `POST /api/agents/:id/revoke-tokens`, which proactively bumps the user's `token_version` and refreshes `token_valid_after` to invalidate old tokens
- The customer management surface now has `POST /api/customers/:id/revoke-tokens`, which proactively triggers old token invalidation for scope-constrained customer accounts
- The underlying token revocation logic has converged to `internal/platform/usersecurity`; the agent/customer management paths reuse the same `token_valid_after + token_version` update implementation
- The management surface now has a unified `security` surface: `GET /api/security/users/:id` queries a single user's security status; `POST /api/security/users/query` batch-previews users' current security state and the next `token_version` after a revoke; `GET /api/security/users/:id/sessions` and `POST /api/security/users/:id/sessions/revoke` view and invalidate a single auth session; `POST /api/security/users/:id/revoke-tokens` and `POST /api/security/users/revoke-tokens` execute single-user / batch user revokes respectively, all protected by `security.read` / `security.write` permissions
- The above `security/users/*` entries now validate target user scope: if the request context carries `tenant_id/workspace_id`, only users associated with `Agent` / `Customer` in that scope may be operated on, and cross-scope targets uniformly return not found
- `security/tokens/revoke` and `security/tokens/revoked` follow the same scoped user constraint: scoped requests can only revoke or view token records of users associated with `Agent` / `Customer` in their own scope, and cannot operate the denylist across tenants
- The management surface now has `POST /api/security/users/:id/sessions/revoke-all`, which batch-invalidates all active auth sessions of the same user and supports keeping one session via `except_session_id`, for "log out all devices" or "kick only other devices" scenarios
- The route governance layer now has a code-level `security surface catalog` that validates anonymous / open surfaces against the real `BuildRouter()` route table; `/api/v1/auth/*` and `/uploads/*` are also covered by the same public surface governance and dedicated rate limiting baseline
- `UserAuthSession` now has minimal client metadata: login and refresh record and update `user_agent`, `client_ip`, and `last_seen_at`, and the management surface `GET /api/security/users/:id/sessions` shows a basic device view directly
- The auth self-service surface now has `GET /api/v1/auth/sessions`, `POST /api/v1/auth/sessions/logout-current`, and `POST /api/v1/auth/sessions/logout-others`; logged-in users can view their own auth sessions and perform "log out current session" or "kick other devices"
- `UserAuthSession` now further carries `device_fingerprint`: `X-Device-ID` takes precedence, otherwise a stable hash is derived from `User-Agent + ClientIP`, making it easy to group repeated refreshes / multiple sessions of the same device
- Both the auth / management session lists now return derived security view fields `network_label`, `location_label`, `risk_score`, `risk_level`, `risk_reasons`, plus session-family metrics such as `family_public_ip_count` / `family_device_count` / `active_session_count` / `family_hot_refresh_count` / `reference_session_id` / `ip_drift` / `device_drift` / `rapid_ip_change` / `rapid_device_change` / `refresh_recency` / `rapid_refresh_activity`; they currently distinguish loopback/private/public, provide minimal location hints for documentation-reserved ranges / shared address space, and turn multiple public IPs under one account, excessive active sessions, device/IP drift relative to the most recently active session, rapid switching within a 24-hour window, and short-window refresh activity into explainable risk hints
- The Geo/IP enrichment layer of session risk has been extracted from inline handler rules into an injectable `session IP intelligence` adapter; the default still uses the existing heuristic provider, and both the auth self-service and management user-security sides can plug in a real Geo/IP provider without touching the core risk logic, with an HTTP provider and handler-level automated evidence already in place on both sides
- An `HTTP session IP intelligence provider` that can land on real data sources and the `security.session_ip_intelligence` config item have been added; when `enabled + base_url` is configured, the auth / management session lists pull `network_label` / `location_label` from an external endpoint, automatically falling back to local heuristic classification on failure; the adapter now has compatibility regressions for nested `data` payloads and custom auth headers beyond `Authorization`
- The session risk heuristic thresholds are now centralized in a unified `session risk policy` wired to the `security.session_risk` config surface; an environment-level baseline via `server.environment + security.session_risk_profiles.{environment}` has been added, still supporting tenant / workspace / runtime layer-by-layer refinement, with automated coverage on both the auth self-service and management user-security sides
- This round's `G2-*` closure criteria are met: legacy aggregation boundaries, the HTTP Geo/IP provider adapter, the environment-level session risk policy, and the audit/rollback operational constraints all have code, test, and documentation evidence; plugging in any specific external Geo/IP vendor later should be treated only as provider selection or deployment acceptance enhancement, not as a blocker for this round
- `servify check-security-baseline --strict` and `scripts/check-security-baseline.sh` have been added, upgrading the existing security warning rules into explicit failing checks before deployment, instead of relying only on startup log reminders
- T5 has reached the "minimal security governance skeleton" acceptance goal; later enhancement items should be tracked separately and are no longer closure blockers for this round

Example session list response (the auth / management side field sets are identical; `is_current` is returned only on the auth self-service side):

```json
{
  "session_id": "sess-current",
  "status": "active",
  "token_version": 3,
  "device_fingerprint": "fp-2d9c6c",
  "network_label": "public",
  "location_label": "public_unknown",
  "risk_score": 5,
  "risk_level": "high",
  "risk_reasons": [
    "public_network",
    "multi_public_ip_family",
    "ip_drift"
  ],
  "family_public_ip_count": 2,
  "family_device_count": 2,
  "active_session_count": 2,
  "family_hot_refresh_count": 2,
  "reference_session_id": "sess-latest",
  "ip_drift": true,
  "device_drift": false,
  "rapid_ip_change": true,
  "rapid_device_change": false,
  "refresh_recency": "hot",
  "rapid_refresh_activity": true
}
```

Explicitly left for separate future items:

- Real Geo/IP intelligence integration
- Stronger time-series / cross-session anomaly detection
- Session risk policies tiered by environment, tenant, or role
- Finer-grained approval / automated review rules for public interfaces
- Fuller user security operation capabilities such as approval rollback
