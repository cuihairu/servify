# Audit Log Policy

This file defines the minimal query and retention policy for the current audit log.

## Query Entry Points

- Admin surface query: `GET /api/audit/logs`
- Admin surface single-record query: `GET /api/audit/logs/:id`
- Admin surface diff preview: `GET /api/audit/logs/:id/diff`
- Admin surface full-chain verification: `GET /api/audit/verify` (entry-by-entry verification over the whole hash chain; 404 when the verifier is not assembled)

Supported filters:

- `action`
- `resource_type`
- `resource_id`
- `principal_kind`
- `actor_user_id`
- `success=true|false`
- `from=<RFC3339>`
- `to=<RFC3339>`
- `page`
- `page_size`
- plus `tenant_id` / `workspace_id` projected through the unified request scope

## Current Recording Scope

Currently covered uniformly:

- Successful write requests on the admin surface `/api/*`
- Records of subject type, operator, action, resource, request metadata, request payload

Currently not fully covered:

- The anonymous public surface
- Read-only query requests
- Fine-grained per-handler `before/after` business snapshots
- More advanced export formats or offline-task capability for audit query export

Diff preview notes:

- `GET /api/audit/logs/:id/diff` reads a single audit record under the current request scope
- If `before_json` / `after_json` can be parsed as JSON, object field-level changes return `changed_paths` and `changes`
- Currently a generic JSON diff, suitable for troubleshooting and quick previews; a finer domain-level diff can still be enhanced per resource type

Export notes:

- `GET /api/audit/logs/export` reuses filter parameters consistent with the list endpoint, and additionally supports `limit`
- `limit` defaults to `1000`, maximum `5000`; anything above the cap is truncated by the server, preventing the admin surface from doing unlimited exports directly
- The current export format is CSV, suitable for ad-hoc investigation, retention, or handing to operations for lightweight secondary analysis
- If the export volume keeps growing, adding asynchronous export tasks, object storage persistence, and permission watermarks is recommended

## Retention Policy

Current recommended baseline:

- 180 days of online audit log retention by default
- Records beyond 180 days should be archived or purged
- Logs involving security incidents, permission changes, and critical configuration changes should support longer retention

Current repository state:

- Persistence model and query endpoints are in place
- A scheduled cleanup worker is wired in, deleting expired records on the default 180-day retention window
- The cleanup logic scans and deletes `created_at < cutoff` records in batches; hitting the retention boundary never mistakenly deletes logs equal to cutoff
- Hot/cold tiered archiving is wired in (see next section); with no archive directory configured it stays pure deletion behavior

## Hot/Cold Tiered Archiving

- After configuring `security.audit.archive_dir` to point at a persistent directory, the cleanup worker first writes each whole batch of expired rows as a gzip JSON snapshot (cold tier) before deletion; an empty directory (the default) means no archiving, behavior identical to before
- Snapshot files are named like `audit-archive-<UTC时间戳>-<首行ID>-<末行ID>.json.gz`, naturally ordered by time and id, allowing direct reconciliation per file
- Rows fully retain `prev_hash` / `entry_hash` — the cold tier and the hot tier are on the same hash chain, so after archiving, chain-style verification can still reconcile, without a "cold tier is unauditable" broken chain
- An archive failure interrupts this cleanup round without deleting: the hot tier archives before deleting, so there is no "deleted before archived" data-loss window; the next round retries automatically after the fault is resolved
- Archive writes use a temporary file + rename for an atomic persist; a killed process never leaves a half-written snapshot to be mistaken for complete cold-tier data
- Later, `archive_dir` can point at an object storage mount, or an external archive task can be added on top, without changing the cleanup semantics

## Coordination with Configuration Rollback

- The `update` / `rollback` of `scoped_config` writes before / after snapshots into the audit store, reused by `history`, `diff`, and rollback previews
- High-risk rollbacks still require explicit `confirm=true`, `change_ref`, `reason`, plus a real `approval_ref` when a high-risk rule is hit
- After a rollback is executed, the verification conclusion should continue to be written back via `POST /api/security/config/{scope}/verify/:audit_id`; audit export carries no governance function

## Follow-up Implementation Suggestions

1. Hot/cold tiered archiving has landed (`security.audit.archive_dir`); if high-value events such as permission changes and configuration changes need longer retention, an independent long-term archive policy can be built on top of the cold-tier snapshots
2. Extend a more stable long-term archive policy for permission changes and configuration changes, instead of relying only on the online store
3. Add asynchronous tasks, object storage persistence, and permission watermarks for large-volume exports
