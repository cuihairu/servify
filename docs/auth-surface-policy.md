# Auth Surface Policy

This document defines the authorization surface classification for Servify's HTTP routes. The goal is to converge "should this entry point really be anonymous, should it require a management token, or should it allow service only" from scattered handler logic into a unified rule.

## Classification

### 1. Public Surface

Scope:

- Public read interfaces aimed at anonymous visitors, browsers, or end users
- Expose only minimal read-only or connection capability

Constraints:

- Does not depend on a management JWT
- Carries no back-office administrative write operations
- Must have an independent security review and a path-level rate limiting baseline

Current routes:

- `/health`
- `/ready`
- `/public/portal/config`
- `/public/kb/*`
- `/public/csat/*`
- `/api/v1/ws`
- `/api/v1/ws/voice` (voice translation channel; not registered at the wiring layer when `ai.asr` is not configured)
- `/uploads/*`

### 2. Auth Surface

Scope:

- Entry points related to authentication and session self-service
- Allows anonymous entry into the pre-authentication flow, or lets logged-in users manage their own auth session

Constraints:

- Anonymous entry points must have dedicated path-level rate limiting
- Logged-in self-service entry points may only operate on the caller's own authentication state and do not reuse the management surface's resource write permissions
- Carries no cross-user, cross-tenant back-office administrative actions
- The entire auth group is mounted with an audit middleware that records failures (successes and 4xx/5xx all persisted, credential fields masked); failed logins and logins blocked by risk policy can be reconciled via `GET /api/audit/logs`
- Optional login risk enforcement (`security.session_risk.login_enforcement`, default `off`): once the `session_ip_intelligence` intelligence source is wired in, high-risk network origins are forced into a second factor (`step_up`) or rejected outright (`block`) by tier; built-in heuristic tags do not trigger the enforcement tiers
- Refresh token replay handling (`security.session_risk.refresh_reuse_policy`, default `off`): under the `revoke_family` tier, a replay of an already-rotated old token revokes the entire session family (the newest token is invalidated with it), forcing a fresh login; replay rejections are recorded in the audit trail

Current routes:

- Anonymous authentication entry points: `/api/v1/auth/login`, `/api/v1/auth/register`, `/api/v1/auth/refresh`, `/api/v1/auth/2fa/verify` (2FA login challenge step, anonymous OTP submission), `/api/v1/auth/oidc/start`, `/api/v1/auth/oidc/callback`, `/api/v1/auth/oidc/status` (OIDC SSO redirect; status returns a fallback response when no provider is configured)
- Logged-in self-service entry points: `/api/v1/auth/me`, `/api/v1/auth/sessions`, `/api/v1/auth/sessions/logout-current`, `/api/v1/auth/sessions/logout-others`

### 3. Management Surface

Scope:

- Admin console, agent workspace, operations queries, manually triggered back-office actions

Constraints:

- Must pass through `AuthMiddleware`
- principal kind allows only `agent`, `admin`, `service`
- Resource-level access keeps layering `RequireResourcePermission(...)` on top
- `end_user` tokens are not allowed onto this surface

Current routes:

- `/api/*`
- `/api/v1/ws/stats`
- `/api/v1/webrtc/*`
- `/api/v1/messages/platforms`
- `/api/v1/ai/*`

### 4. Service Surface

Scope:

- Machine-to-machine calls, ingestion reporting, controlled integration entry points

Constraints:

- Must pass through `AuthMiddleware`
- principal kind allows only `service`
- Does not reuse management `agent/admin` tokens
- Should be designed first as a narrow-contract, low-privilege, independently rate-limitable entry point

Current routes:

- `/api/v1/metrics/ingest`

## Admission Rules for New Routes

When adding an HTTP route, answer these 4 questions first, then decide which surface to mount it on:

1. Who is the caller: an anonymous user, an end user, an agent/admin, or a service account?
2. Is this entry point public read, pre-authentication, back-office administration, or machine reporting?
3. On failure, should it return `401`, `403`, or let the anonymous caller continue into business validation?
4. Does it need a dedicated path-level rate limit rather than relying only on the global limit?

Default policy:

- When unsure, do not mount directly onto the anonymous `public` surface
- Authentication-related entry points go to the `auth` surface first, rather than being mixed into `public` or `management`
- Administrative actions go to the `management` surface first
- Machine reporting, callbacks, and ingestion go to the `service` surface first

## Implementation Locations

- Route wiring: [router.go](https://github.com/cuihairu/servify/blob/main/apps/server/internal/app/server/router.go)
- Route security surface catalog: [security_surface.go](https://github.com/cuihairu/servify/blob/main/apps/server/internal/app/server/security_surface.go)
- JWT and claims normalization: [platform/auth](https://github.com/cuihairu/servify/tree/main/apps/server/internal/platform/auth)
- Gin-compatible entry points: [internal/middleware](https://github.com/cuihairu/servify/tree/main/apps/server/internal/middleware)

## X-API-Key (service principal, open platform)

P1 introduces machine credentials: besides `Authorization: Bearer`, `AuthMiddleware` now accepts the `X-API-Key` header.

Authentication flow:

1. A request carrying `X-API-Key: sv_...` takes the API Key branch (without this header it takes the original JWT path entirely, line for line unchanged)
2. The plaintext key is sha256-hashed and looked up in `api_keys.key_hash` (unique index); a high-entropy random key resists brute force, so no slow hash is needed
3. Revoked (`revoked_at`), expired (`expires_at`), or nonexistent all return a uniform 401 with no reason distinguished
4. On success, injects `principal_kind=service`, tenant/workspace scope, and a read-only permission set
5. `last_used_at` is updated with a 60s throttle to avoid write amplification under high request rates

Permission surface:

- Read-only by default: `tickets.read`, `conversations.read`
- The `scopes` field is reserved for extension (comma-separated); it can be set explicitly at issuance
- Revocation takes effect immediately: every request is validated against the database by the resolver

Open-platform routes (mounted on the management surface, gated by `RequireResourcePermission`):

- `GET /api/tickets` (existing tickets read permission; note that `/api/v1/tickets` is the visitor no-login POST ticket-creation entry, not an API-Key read entry)
- `GET /api/conversations`, `GET /api/conversations/:id`, `GET /api/conversations/:id/messages` (conversations read)
- `POST /api/v1/metrics/ingest` (a duplicate entry for the existing service-surface capability)

Admin routes (`api_keys` resource permission):

- `GET /api/api-keys` (list; echoes back only the prefix and fingerprint status, never the plaintext)
- `POST /api/api-keys` (issue; the plaintext appears exactly once in that response)
- `POST /api/api-keys/:id/revoke` (revocation takes effect immediately)
- `DELETE /api/api-keys/:id`

## Future Extensions

- If a genuine end-user authenticated business API appears later, add a separate surface instead of falling back to reusing the management surface
- If callback signing, API keys, or mTLS are introduced, further subdivision can happen inside the service surface
- If exposed interfaces keep growing, the `security surface catalog` should be extended into an input source for automated review and approval flows
