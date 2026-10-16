> **English** | [中文](security-baseline-operations.md)

# Security Baseline For Operations

This document drives `11 / T5 security-baseline-for-operations`, first consolidating current high-risk interfaces, existing controls, and the minimum go-live baseline into one list.

## High-risk interface inventory

### Management Surface write interfaces

Scope:

- Write operations under `/api/*` for tickets, customers, agents, transfers, integrations, automation, knowledge base, etc.
- `/api/v1/ai/knowledge/upload`
- `/api/v1/ai/knowledge/sync`
- `/api/v1/ai/knowledge-provider/enable`
- `/api/v1/ai/knowledge-provider/disable`
- `/api/v1/ai/circuit-breaker/reset`

Risk:

- Changes business main data, routing behavior, or AI/knowledge runtime state
- Misauthorization causes cross-tenant writes, config pollution, or service degradation directly

Current controls:

- `AuthMiddleware`
- `EnforceRequestScope`
- `RequirePrincipalKinds("agent", "admin", "service")`
- `RequireResourcePermission(...)`
- Management-plane write requests uniformly pass through `AuditMiddleware`

### Service Surface

Scope:

- `/api/v1/metrics/ingest`

Risk:

- This is a machine entry point; if the token leaks, it can be used to forge monitoring data or cause amplified writes

Current controls:

- `AuthMiddleware`
- `EnforceRequestScope`
- `RequirePrincipalKinds("service")`
- Global HTTP rate limiting middleware supports per-path rate limiting

### Public Surface

Scope:

- `/public/portal/config`
- `/public/kb/*`
- `/public/csat/*`
- Anonymous entries under `/api/v1/auth/*` (`login` / `register` / `refresh` / `2fa/verify` (anonymous OTP challenge step, using the auth prefix rate limit 25rpm) / `oidc/start|callback|status`)
- `/api/v1/ws`
- `/api/v1/ws/voice` (voice translation channel; uses the `/api/v1/ws` prefix rate limit entry)
- `/uploads/*`

Risk:

- Anonymously exposed; easily becomes an entry for enumeration, scraping, abuse, or traffic amplification

Current controls:

- The route surface has been separated from management / service to avoid reusing backend JWT semantics
- Global HTTP rate limiting middleware is already mounted on the base routes
- The runtime now maintains a code-level `security surface catalog`, used to verify whether public/anonymous surfaces are explicitly registered, avoiding new open routes being left to doc review only

## Security baseline already in place

### Authentication and authorization

- Management plane and service plane both go through `internal/platform/auth`
- `end_user` tokens are explicitly rejected from entering the management surface
- Request scope validates token scope against header/query scope conflicts, blocking requests from expanding their scope
- `security/users/*` and `security/tokens/*`, beyond resource permissions, now continue to validate tenant/workspace ownership via the target user's associated `Agent` / `Customer` scope; cross-scope targets are uniformly treated as nonexistent

### Audit

- Management-plane write requests record actor, principal, tenant, workspace, resource, and request metadata by default
- Audit query interface: `GET /api/audit/logs`
- The audit middleware now uniformly redacts sensitive fields like `password`, `secret`, `api_key`, `token`, avoiding plaintext secrets being written into the audit store
- The auth public surface (`/api/v1/auth/*`: login, register, refresh, 2FA challenge, logout) uniformly mounts an audit middleware with failure logging: both successes and 4xx/5xx are written to the store (`success=false` + real status_code); failed logins and logins blocked by risk policy are both reconcilable; credential fields use the same redaction rules

### Login risk enforcement (P2-5 second cut)

Risk determination depends on the network labels returned by the `security.session_ip_intelligence` source: built-in heuristics only produce the four security labels `public` / `private` / `loopback` / `unknown`, all judged as low-risk; only when an external intelligence source is connected and the source is labeled with rich labels like `hosting`, `proxy` is it judged as a high-risk source. The `security.session_risk.login_enforcement` tiers:

- `off` (default): no enforcement; login behavior is identical to existing versions
- `step_up`: high-risk-source logins require a second factor — users with TOTP bound enter the challenge step (even if the 2FA master switch is off); users without TOTP bound are rejected directly
- `block`: high-risk-source logins are still rejected after credential verification passes (403, response does not reveal the determination basis)

When the intelligence source is not enabled or the tier is not configured, there is zero behavior change. Real runtime acceptance: `make auth-audit-acceptance` (stub intelligence source labeled hosting → block stage 403 interception, wrong password 401; off stage same-store restart normal login 200; reconcile via `GET /api/audit/logs` for three login row types and `[REDACTED]` redaction).

### Refresh token replay handling (P2-5 third cut)

Every refresh rotates the refresh token (the session row `token_version` increments), and old tokens are rejected uniformly. The `security.session_risk.refresh_reuse_policy` tier determines the handling depth of replay (reuse):

- `off` (default): old token replay is rejected but the session is retained, and the legitimate client's latest token can continue refreshing (existing behavior)
- `revoke_family`: replay is a session-leak signal — when "token's claimed version is less than the store's current version" is detected, the whole session family is revoked, including the latest token held by the legitimate client, forcing re-login; the replier and the victim are treated without distinction (fail-closed)

One session is one family: all refresh rotations after login share the same session row. Forged/abnormal tokens whose version is greater than the store's current value are only rejected, not triggering revocation; requests that fail revocation are also rejected (no silent pass-through). Externally a uniform 401, not revealing revocation state or version differences; replay rejection is logged via the auth-plane audit middleware. Real runtime acceptance: `make refresh-reuse-acceptance` (off stage replay 401 and session survives; revoke_family stage replay 401 and the family's latest token is also 401, re-login works; reconcile via `GET /api/audit/logs?action=auth.refresh` for rejection rows and success rows).

### Scoped config approval rollback path (P2-5 fourth cut)

The write path for scoped config (tenant/workspace scope config documents) already has full governance semantics: PUT/rollback/approve/verify all enforce change control (`change_ref` + `reason`, JSON body or corresponding header); rollback is always judged as high-risk, requiring independent approval (`validateScopedConfigApproval`: approver ≠ executor, otherwise 403); verify requires verifier ≠ original operator (403), and the `passed` conclusion must cover all `required` checks of the verify template plus evidence. The capability surface has long existed; the fourth cut adds **real runtime acceptance**:

- `make approval-rollback-acceptance` starts a real service (sqlite), constructs operator and reviewer via a "pre-seeded dual admin" seed flow (the second registration request with `role=admin` being downgraded to `customer` by the registration convergence logic is itself an acceptance point; the privilege-escalation action goes through a seed-style DB update and re-login, equivalent to the ops flow of pre-seeding mutual-review admins in production), then walks the full governance path:
  - Unauthenticated PUT → 401; PUT missing `change_ref` → 400 `Change control required`
  - Two updates (medium, no approval needed) land in the store, `change_control.change_ref` echoed consistently
  - Rollback without approval → 400; operator self-review then rollback → 403 `Approval reviewer separation required`
  - Reviewer records approval → operator carries the approval and executes `?confirm=true` rollback → 200; GET confirms what is restored is the target snapshot's complete AfterJSON (not an intermediate version)
  - Operator submits verify for an update/rollback they executed themselves → 403 `Verification reviewer separation required`; reviewer verify (dynamically fetches the history template's required checks, all passed + evidence) → 200
  - history reconciliation: operation/`can_rollback`/`verification_status` metadata closed out; audit reconciliation: `scoped_config.tenant.{update,rollback,approve,verify}` success row counts, `resource_type=scoped_config`, actor_user_id distinguishing operator/approver/verifier
- The manifest validates 14 checks and evidence files per `provider=approval-rollback` (`scripts/validate-acceptance-manifest.sh`)

### Rate limiting

- `registerBaseMiddleware` uniformly mounts `RateLimitMiddlewareFromConfig`
- Supports global rate limiting, per-path-prefix rate limiting, per-header-key rate limiting, and IP / key allowlists
- The default dev config `config.yml` already enables basic rate limiting, and provides independent path-level rate limiting for `/public/`, `/public/kb/`, `/public/csat/`, `/api/v1/auth/`, `/api/v1/ws`, `/uploads/`, `/api/v1/ai/query`, `/api/v1/metrics/ingest`, `/api/`
- Production environments should still tighten thresholds, domains, and secrets injection based on `config.production.secure.example.yml` or equivalent deployment config, rather than taking the dev config to production as-is

### Startup alerts

- `InitLogging` now outputs basic security alerts at startup
- Currently covers scenarios like default or empty `jwt.secret`, open `CORS=*`, rate limiting disabled, empty OpenAI API key, external knowledge provider enabled but missing necessary credentials, etc.
- If rate limiting is enabled but no independent path-level rate limiting is configured for `/public/`, `/public/kb/`, `/public/csat/`, `/api/v1/auth/`, `/api/v1/ws`, `/uploads/`, `/api/v1/metrics/ingest`, `/api/`, a warning is also output, avoiding public entries, high-cost service surfaces, and management surfaces falling under global rate limiting only
- Startup-time is still warning-only, not blocking local dev or test startup; before deployment, run `check-security-baseline --strict` for an explicit failing check

### Pre-deployment check entry points

- CLI: `go -C apps/server run ./cmd -c config.yml check-security-baseline --strict`
- Script: `sh ./scripts/check-security-baseline.sh config.yml`
- Make: `make security-check CONFIG=config.production.secure.example.yml`
- This check reuses the startup-time `SecurityWarnings` rules, but under `--strict` exits non-zero, suitable for CI, CD, or manual pre-release validation

## Recommended minimum production config

Recommend enabling at least the following rate-limit baseline:

- Global `security.rate_limiting.enabled = true`
- `/api/`: protects management-plane write operations and batch reads
- `/api/v1/auth/`: separate stricter rate limiting, prioritizing protection for anonymous auth entries like login, register, refresh
- `/api/v1/ai/`: separate stricter rate limiting, especially `knowledge/upload`, `knowledge/sync`
- `/api/v1/metrics/ingest`: separate rate limiting, preferring service token or dedicated header key
- `/public/`: separate rate limiting per anonymous entry, avoiding scraping and credential stuffing
- `/uploads/`: separate rate limiting, avoiding file enumeration, hot-resource scraping, or bandwidth amplification

Recommend simultaneously satisfying these constraints:

- `jwt.secret`, OpenAI / Dify / WeKnora API keys do not use code defaults
- Secrets are injected only via environment variables or an external secret manager
- The management plane must be deployed behind TLS
- `security.cors.allowed_origins` does not use the production-wide `*`
- `X-Request-ID` is uniformly injected by the gateway or ingress, for audit traceability

## Gaps still to close

- Currently available: single-user query, batch security-state preview, single-user / batch-user token revocation, independent refresh token, single token revoke list, revoke list query, plus auth session list query, single-session revocation, and batch session revocation per user; the session list now returns basic client metadata (`device_fingerprint` / `user_agent` / `client_ip` / `last_seen_at`) and derived fields (`network_label` / `location_label` / `risk_score` / `risk_level` / `risk_reasons`), and adds context like `family_public_ip_count` / `family_device_count` / `active_session_count` / `family_hot_refresh_count` / `reference_session_id` / `ip_drift` / `device_drift` / `rapid_ip_change` / `rapid_device_change` / `refresh_recency` / `rapid_refresh_activity` for same-account concurrency, drift, short-window switching, and refresh activity; denylist expired records are cleaned up by a background worker, and `security.session_risk` is now an official config surface
- The above `security/users/*` and `security/tokens/*` management entries now add tenant/workspace target-user isolation, no longer allowing scoped requests to enumerate, revoke, or view token/session records of users associated with other `Agent` / `Customer` across tenants
- The remaining gaps above should be treated as follow-up enhancements, not as blockers for this T5 round: real Geo/IP intelligence enrichment, stronger anomaly models, differentiated policies per environment/tenant
- What was completed this round: Geo/IP now has an injectable adapter, HTTP provider, config entry, router wiring, and automation coverage on both auth and management sides; when connecting specific external intelligence vendors later, do not regress to "basic capabilities incomplete"
- External open interfaces now begin using a code-level `security surface catalog` for minimal registration validation; follow-up can continue to enhance this into finer-grained public-interface approval and automated review
- Scoped config key config changes now require `change_ref` and `reason`, and carry these fields into audit request metadata and history metadata; this closes the minimum change-tracking baseline
- High-risk scoped config changes now have real pre-execution approval constraints: when hitting high-risk scenarios like provider endpoint, knowledge mapping, session risk core thresholds, or rollback, the execution interface must not only provide `approval_ref`, but must be able to look up a recorded approval event by `approval_ref + change_ref`, and the approver and executor must be separated
- Scoped config now has a post-execution verification entry, which can write a `passed` / `failed` conclusion for a single update / rollback audit record, and echo `verification_status`, the latest verification, and the verification policy in the history list and detail
- Current verification now has templated check items and a minimum two-person review constraint: the reviewer cannot be the same as the original executor, the verification request must submit `checks` aligned with `verification_template.checks`, `passed` must carry evidence and all required checks must be `passed`, `failed` must carry notes and at least one check must be `failed`
- `verification_template` is further split by field risk, and returns root-level `changed_paths` plus per-check `risk_level` / `changed_paths`, for standardized acceptance of high-risk configs like provider endpoint, KB mapping, session risk threshold
- Write responses, history lists, single detail, and verify responses now synchronously return `change_risk` and `approval_policy`, directly exposing the current config change's risk level, trigger reason, approval record persistence state, and latest approver info to the frontend or automation scripts
- The same batch of responses now also uniformly returns `governance_status` / `governance_policy`, merging pre-execution approval and post-execution verification into a single governance state; the verify response further returns the source change's governance completion state
- History lists, single detail, and verify responses now synchronously return `verification_template` and finer-grained `verification_policy`, for the frontend or automation scripts to execute post-change verification per template
- The history list can now also be filtered directly by `governance_status` / `risk_level` / `approval_status` / `verification_status` / `needs_action` to pull the governance queue, and returns a `governance_summary` aggregate, so ops or security on-call can directly pull the to-do view
- Follow-up still needs to distill key config changes into a complete "pre-execution approval / post-execution verification / rollback record" ops manual, and add automated acceptance and finer-grained approval orchestration

## Audit and rollback operational constraints

- Audit export is by default only suitable for lightweight troubleshooting or archiving; a single `GET /api/audit/logs/export` defaults to exporting at most `1000` rows, with a server hard cap of `5000`
- When exceeding the lightweight export scale, do not pull the full history directly from the online management plane via repeated pagination scripts; go through offline archiving or a follow-up async export approach
- The online audit store currently retains `180` days by default; a background worker cleans up expired records in batches; long-term archiving involving security events, permission changes, and key config changes still needs to land in an external archiving policy
- Scoped config rollback cannot be treated as an ordinary "restore button":
  - Must first use `GET /api/security/config/{scope}/history/:audit_id` to view the snapshot diff
  - High-risk rollback must submit `approval_ref`, and the approver and executor must be separated
  - After execution, a `verify` conclusion must also be completed, confirming that affected paths like portal / provider / session risk have recovered
- On-call ops must retain at least three types of evidence before and after rollback: source audit ID, rollback audit ID, verification audit ID
- The end-to-end real-runtime acceptance entry for the governance path: `make approval-rollback-acceptance` (P2-5 fourth cut, see the scoped config approval rollback path section above)

## Code landing points

- Route and surface classification: `apps/server/internal/app/server/router.go`
- Base middleware wiring: `apps/server/internal/app/server/middleware.go`
- Rate limiting: `apps/server/internal/middleware/ratelimit.go`
- Audit middleware: `apps/server/internal/platform/audit/gin_middleware.go`
- Startup security alerts: `apps/server/internal/app/bootstrap/security.go`
- Pre-deployment strict check: `apps/server/cmd/cli/check_security_baseline.go`
- Route security surface catalog and coverage check: `apps/server/internal/app/server/security_surface.go`
