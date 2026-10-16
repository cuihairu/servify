> **English** | [中文](public-surface-security-checklist.md)

# Public Surface Security Checklist

This document drives the "baseline security checklist for externally exposed interfaces" under `11 / T5 security-baseline-for-operations`.

Scope:

- `/public/*`
- Anonymous websocket / realtime connection entry points
- Anonymous authentication entry points such as `/api/v1/auth/login`, `/api/v1/auth/register`, `/api/v1/auth/refresh`, `/api/v1/auth/2fa/verify`, `/api/v1/auth/oidc/*`
- Public file access entry points such as `/uploads/*`
- Public APIs that may later be opened to third parties or end users

## What Must Be Confirmed Before Adding an Exposed Interface

1. Does this interface really need anonymous access?
2. Can it be turned into a `management surface` or `service surface` instead?
3. Would it leak internal identifiers such as tenant, workspace, agent, customer?
4. Does it need pagination, filtering, or result trimming to prevent bulk enumeration?
5. Does it need dedicated path-level rate limiting rather than relying only on the global limit?
6. Would it return provider, infrastructure, or internal topology information?
7. Do access events or anomalous traffic need auditing?

## Minimum Checklist

### Input

- Accept only parameters with an explicit schema
- Apply length and format limits to query, path, and header
- Do not trust client-supplied tenant / workspace scope

### Output

- Never return secrets, tokens, internal configuration, or provider credentials
- Do not expose unnecessary internal IDs, database primary keys, or filesystem paths
- Error messages must not echo internal stacks, SQL, or third-party provider details

### Traffic Control

- Must assess whether dedicated path-level rate limiting is needed
- Use stricter quotas on interfaces that can trigger expensive computation, retrieval, or uploads
- If websocket / streaming is supported, cap concurrent connections or connections per unit time

### Browser Exposure

- Check that CORS allows only the necessary origins
- With a multi-origin allowlist, the server echoes back the matched Origin per request (with `Vary: Origin`),
  and anything outside the allowlist gets no `Access-Control-Allow-Origin` at all; a multi-valued ACAO header must never be emitted
  (fixed in the first cut of 2026-09 P2-5: the previous `strings.Join` emitted an illegal multi-valued header that browsers universally reject)
- If the interface can be embedded by pages or called from scripts, confirm whether extra origin / referer restrictions are needed

### Observability

- Set up basic monitoring for anomalous traffic, `429`, `401`, `403`
- Be able to trace issues via `X-Request-ID` or an equivalent request identifier

## Recommendations for Current Interfaces

### `/public/portal/config`

- Return only branding and locale information
- Do not include internal tenant configuration, provider parameters, or feature flag details
- Keep the dedicated rate limit

### `/public/kb/*`

- Expose only knowledge content that has been publicly published
- Result sets must support pagination / row limits
- Watch for scraping, enumeration, and full-text crawling risks
- The runtime baseline requires a dedicated path-level rate limit for `/public/kb/`

### `/public/csat/*`

- Avoid enumerating other people's satisfaction data via predictable IDs
- Apply a separate limit on submission frequency
- The runtime baseline requires a dedicated path-level rate limit for `/public/csat/`

### `/public/suggestions/*`

- Return only `is_public` knowledge document titles; no ticket / other-customer information
- Exposure attribution writes one row to `suggestion_exposure_logs` per successful response (a write-amplification surface), so a dedicated path-level rate limit is mandatory (P2-0 RQ-5; staging 45rpm / production 30rpm baseline)
- The dedicated prefix must be listed before `/public/` in `rate_limiting.paths` (the middleware takes the limit from the first matching prefix)

### Anonymous `/api/v1/auth/*` entry points

- `/login`, `/register`, `/refresh` must have dedicated path-level rate limits
- `/2fa/verify` (the anonymous OTP challenge step) and `/oidc/start|callback|status` belong to this surface as well: the 2FA verification endpoint focuses on preventing OTP brute-force enumeration (keeping the auth-prefix rate limit of 25rpm), while the OIDC callback focuses on preventing open redirects and state forgery
- Do not leak account existence, internal state, or policy details in error responses
- Set up alerts for brute-force attempts, refresh abuse, and bulk registration
- The global `security.max_body_bytes` returns a direct 413 for oversized requests (16MB in the production template)

### `/api/v1/ws`

- The anonymous connection entry point must control connection frequency and concurrency
- Internal state, scheduling information, or tenant-sensitive metadata must not be exposed during the handshake
- The runtime baseline requires a dedicated path-level rate limit for `/api/v1/ws`
- When `security.websocket_allowed_origins` is non-empty, any connection carrying an Origin header outside the allowlist gets a 403; an empty list stays permissive (anonymous visitor origins such as native WebViews cannot be enumerated);
  non-browser clients without an Origin header are let through (caught later by token/tenant validation)

### `/api/v1/ws/voice`

- The voice translation channel (Phase 2 cut 2b-2): an anonymous connection surface of the same kind as `/api/v1/ws`, in the same visitor token trust domain (same-origin check when `guest_token.required` is enabled)
- Wiring-layer gate: routes are not registered when `ai.asr` is not configured (the hub handshake 503 is the directly-constructed fallback), so the surface does not exist when the deployment has no voice capability
- The handshake returns only missing `session_id` (400) / invalid speaker (400) / token failure
  (401) / channel not wired (503), never internal state or provider details; stream-level errors
  are delivered via the `voice-error` code vocabulary (`disabled` / `asr_unavailable` /
  `stream_broken`) and then the line is closed
- Shares the same upgrader as `/api/v1/ws`; the `security.websocket_allowed_origins`
  allowlist (and the no-Origin-header pass-through) convention is fully identical
- Path-level rate limiting reuses the `/api/v1/ws` prefix entry (that prefix matches by prefix and covers
  `/api/v1/ws/voice`; no separate entry is created to avoid the same connection being counted twice)

### `/uploads/*`

- Expose only uploaded assets with a genuine public need
- Do not echo real disk paths or internal storage key rules
- The runtime baseline requires a dedicated path-level rate limit for `/uploads/`
- Local storage mode only allows hits on concrete files: directory listing, missing paths, and the storage root path all return 404
  (gin Static is no longer mounted since the first cut of 2026-09 P2-5)

## Runtime Security Baseline (Landed in the First Cut of 2026-09 P2-5)

The following capabilities have landed along with the `security.headers` / `security.max_body_bytes` /
`security.websocket_allowed_origins` config sections, with real-run acceptance
(`scripts/test-public-surface-acceptance.sh`, provider=`public-surface`):

- Unified security response headers: `X-Content-Type-Options: nosniff`, `X-Frame-Options`,
  `Referrer-Policy`, optional CSP, optional HSTS (off by default: TLS is usually terminated at a reverse proxy,
  and HSTS is injected by the proxy). The security-header middleware is mounted before rate limiting / the body cap, so aborted responses such as `429`/`413`
  also carry the security headers.
- Global request body cap `security.max_body_bytes`: a Content-Length over the cap returns a direct 413,
  and chunked transfer is caught at read time by `http.MaxBytesReader`.
- CORS multi-origin allowlist echoes per request + `Vary: Origin`.
- WS connection Origin allowlist.
- Local `/uploads/*` serves only concrete files; directories and misses all return 404.

The production and staging templates (`config.production.secure.example.yml` /
`config.staging.example.yml`) explicitly enable the baseline above; the development template `config.yml`
keeps it off by default to preserve existing deployment behavior.

## Relation to Existing Documentation

- Route surface classification: [auth-surface-policy.md](auth-surface-policy.md)
- Runtime security baseline: [security-baseline-operations.md](security-baseline-operations.md)
- token / key rotation: [token-lifecycle-and-key-rotation.md](token-lifecycle-and-key-rotation.md)
- Runtime acceptance: `make public-surface-acceptance` (evidence archived under
  `scripts/test-results/public-surface/`, manifest provider=`public-surface`)
