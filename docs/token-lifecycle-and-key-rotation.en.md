> **English** | [中文](token-lifecycle-and-key-rotation.md)

# Token Lifecycle And Key Rotation

This document drives forward the token lifecycle, key rotation, and minimal operations spec within `11 / T5 security-baseline-for-operations`.

## Scope

This spec covers the following keys and tokens:

- `jwt.secret`
- OpenAI / Dify / WeKnora / third-party provider API keys
- service token / internal integration token
- future tenant- or workspace-level secret reference external credentials

## Basic Principles

- Do not keep production secrets long-term in any config file other than code defaults and test fixtures
- Production secrets are injected only via environment variables, a secret manager, or deployment-platform ciphertext
- Every long-lived credential must have an owner, purpose, issue time, and rotation time
- Switching between old and new credentials must support a rollback window; avoid single-shot replacement that invalidates everything at once
- Secrets must not enter audit logs, application logs, error messages, or health check output

## JWT Lifecycle Baseline

### Access Tokens

- Default validity should stay on a short period
- Recommended production baseline: between `15m` and `24h`, tightened per calling scenario
- The admin surface and the service surface should not share the same kind of long-lived token

### Issuance Constraints

- `agent`, `admin`, and `service` tokens must distinguish principal kind
- Tokens must carry the minimum necessary scope; no broad scope as a catch-all
- Service tokens should be bound to the caller identity; do not reuse a human account token

### Revocation and Expiry

- Before rotating `jwt.secret`, the maximum tolerated lifetime of the old tokens must be determined
- On a high-risk event, forcing a change to `jwt.secret` is allowed to trigger a full invalidation
- Two expiry mechanisms now exist, `session version` and an explicit `revoke list`: refreshing a refresh token bumps `session_token_version`, and the admin surface can also add a single JWT's `jti` to the revoke list

## Key Rotation Process

### 1. Build the Inventory

Each secret must record at least:

- Name
- Owner
- Usage location
- Injection method
- Last rotation time
- Next planned rotation time

### 2. Pre-release the New Key

- Create the new version in the secret manager or deployment platform
- Do not delete the old version immediately
- Validate the new key in staging / canary first

### 3. Staggered Switch

- Switch stateless instances first
- Then background tasks / workers
- Remove the old key last

### 4. Verify and Roll Back

After switching, verify at least:

- Login and JWT verification work
- `/api/v1/ai/query` works
- WeKnora connection and knowledge base operations work
- `/api/v1/metrics/ingest` shows no authentication regression
- No plaintext secrets appear in the audit log

If verification fails:

- Roll back to the old secret version
- Record the failure window, affected interfaces, and remediation action

## Recommended Rotation Frequency

- `jwt.secret`: high-sensitivity deployments should rotate once every `30-90` days
- provider API key: per vendor capability and risk level, `30-90` days recommended
- service token: `30-60` days recommended, or switch to shorter-lived auto-issued credentials
- test / temporary integration token: clean up immediately after the task ends

## Minimal Investigation Checklist

On suspected leakage, do at least:

1. Determine which kind of token / key leaked.
2. Immediately disable or rotate the affected credential.
3. Check the audit log and access log for the corresponding interfaces within the leak window.
4. Verify whether there were unauthorized calls, config changes, knowledge base uploads, or forged metrics.
5. Add an incident retrospective and follow-up protection items.

## Mapping to Current Code

- JWT configuration: `apps/server/internal/config/config.go`
- Authentication and principal kind: `apps/server/internal/platform/auth`
- Token expiry policy hook: `apps/server/internal/platform/auth/token_policy.go`
- User-state-based token expiry policy: `apps/server/internal/platform/auth/user_state_policy.go`
- Admin surface active expiry entries: `POST /api/agents/:id/revoke-tokens`, `POST /api/customers/:id/revoke-tokens`
- Generic expiry implementation: `apps/server/internal/platform/usersecurity/revoke.go`
- Generic admin/security entries: `GET /api/security/users/:id`, `POST /api/security/users/:id/revoke-tokens`, `POST /api/security/tokens/revoke`, `GET /api/security/tokens/revoked`
- Session family governance entries: `GET /api/security/users/:id/sessions`, `POST /api/security/users/:id/sessions/revoke`, `POST /api/security/users/:id/sessions/revoke-all`
- Session metadata: `user_auth_sessions` currently records `device_fingerprint`, `user_agent`, `client_ip`, `last_seen_at`; the list response also derives `network_label`, `location_label`, `risk_score`, `risk_level`, `risk_reasons`, and provides `family_public_ip_count`, `family_device_count`, `active_session_count`, `family_hot_refresh_count`, `reference_session_id`, `ip_drift`, `device_drift`, `rapid_ip_change`, `rapid_device_change`, `refresh_recency`, `rapid_refresh_activity` as session-family investigation helper fields
- Those session risk fields are currently positioned as operational investigation helper signals and should not be treated as strict risk-control verdicts; the Geo/IP enrichment layer now has an injectable provider adapter, but defaults to heuristic IP classification, with real Geo/IP data sources and stronger anomaly detection left for later enhancement
- Auth self-service entries: `GET /api/v1/auth/sessions`, `POST /api/v1/auth/sessions/logout-current`, `POST /api/v1/auth/sessions/logout-others`
- Admin surface / service surface route surface: `apps/server/internal/app/server/router.go`
- Audit redaction: `apps/server/internal/platform/audit/gin_middleware.go`
