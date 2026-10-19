# Chat Platform Integration Contract

Status: internal contract; no platform connector is implemented by this document. Decision date: 2026-10-19.

## First platform decision

Recommendation: **Telegram Bot API, using `getUpdates` long polling, as the first platform adapter**. This is a reversible choice. Revisit it if the product's first customer population does not use Telegram, cannot authorize full group access, or requires an existing corporate chat platform.

| Candidate | Receive / full group access | Verification complexity | Fit with Servify services | Maintenance |
|---|---|---|---|---|
| Telegram Bot API | Long polling or webhook; full group stream requires privacy mode disabled or bot admin | Low for polling; webhook has secret header/path but no request signature scheme | Poller fits a server worker; offset can be persisted before advancing | Low–medium; one HTTP API and explicit update mapping |
| Slack | Events HTTP push or Socket Mode; scopes and workspace installation constrain visibility | Medium–high: request signature, timestamp, retries, URL challenge | Push needs public endpoint/queue; Socket Mode adds a long-lived connection | Medium; scopes, event subscriptions, retries and API limits |
| Discord | Gateway WebSocket or interaction HTTP; intents and permissions constrain messages | Medium: Gateway heartbeats/session resume; HTTP interaction signatures | Persistent stateful connection is a larger service seam | Medium–high; resume, intents and gateway versions |
| Feishu (application bot) | Event subscription / long connection; app permission and admin authorization required | Medium: event verification/encryption configuration | Long connection can run as worker, outbound OpenAPI is separate | Medium; app permissions and tenant installation lifecycle |
| Microsoft Teams | Activity delivery; full channel/group messages need RSC and tenant/resource consent | High: Bot Framework auth plus tenant consent | Push/activity pipeline and Microsoft identity are new dependencies | High; consent and environment constraints |

Telegram wins the stated criteria: polling avoids a public callback and its signature/crypto setup, persistent offset is a natural worker checkpoint, and outbound is an ordinary Bot API call. It is not “full group by default”: privacy mode is enabled by default for ordinary bots; an administrator must grant access or disable it and re-add the bot. Therefore full-message capture is **off until an operator verifies the effective access and records consent for each selected group**. The Telegram docs describe both the privacy restriction and update confirmation via a higher `offset`; see [Bot Features](https://core.telegram.org/bots/features), [Bot API](https://core.telegram.org/bots/api) and [FAQ](https://core.telegram.org/bots/faq) (checked 2026-10-19).

This follows the research finding that platforms do not share a universal bot wire protocol, so Servify needs an internal event model with platform adapters ([research §1](research/chatbot-platforms-and-protocols.md#结论), checked 2026-10-19). It also follows the research warning that permissions and message lifecycle differ: `group + user + text` loses threads, edits, deletes and attachment/source provenance ([research §3–4](research/chatbot-platforms-and-protocols.md#跨平台共性与不可抹平的差异)).

## Scope and rollout

This document defines internal contracts only. Do not publish a public wire protocol or implement a platform adapter in this change. First implementation remains a Memex connector validation; its limited TEXT and at-least-once `msg_id` behavior must be declared rather than implied to support edits or threads.

1. **Phase 0 — authorization and retention:** verify target customer demand, group administrator consent, effective full-message access, allowed groups, retention schedule and data residency. Keep ingestion disabled until recorded.
2. **Phase 1 — contract plumbing:** define storage for external event keys, source references, capability snapshots and cursor/ACK state; add redacted observability and replay/dead-letter handling.
3. **Phase 2 — Memex validation:** implement the adapter against its pull/ACK API; prove at-least-once delivery, deduplication, source linking and no cross-tenant reads.
4. **Phase 3 — Telegram pilot:** only after Phase 0 approval, implement a long-poll worker, per-group authorization and rate-limited `send`/`notify`; then evaluate whether Telegram remains the right first production platform.

## Inbound contract

Every adapter emits one `InboundEvent`; platform-specific transport and payload stay inside the adapter. Required identifiers are opaque strings and scoped by installation. Times are UTC RFC3339. Missing platform features are represented as `null`/empty, never fabricated.

| Field | Type | Meaning |
|---|---|---|
| `platform` | string | Stable adapter key, e.g. `telegram`, `memex` |
| `installation_id` | string | Tenant-scoped app/bot installation |
| `tenant_id`, `workspace_id` | string | Servify security scope from trusted installation mapping, never payload authority |
| `event_id`, `idempotency_key` | string | Platform event identity and stable dedup key (`installation_id + event_id`) |
| `event_type` | enum | `message.created`, `message.edited`, `message.deleted`, `reaction.changed`, `member.changed`, `unknown` |
| `occurred_at`, `received_at` | timestamp | Source time and Servify receive time |
| `conversation` | object | `platform_id`, `kind`, `name?`, `is_group`, `thread_id?` |
| `sender` | object | `platform_id`, `display_name?`, `is_bot`, `servify_actor_id?` |
| `message` | object/null | `platform_id`, `kind`, `text?`, `content_parts[]`, `edited_at?`, `deleted_at?` |
| `message.content_parts[]` | object | `kind`, `text?`, `mime_type?`, `platform_file_id?`, `download_ref?`, `size_bytes?`, `checksum?`; content bytes are not embedded in events |
| `attachments[]` | object | Normalized attachment metadata and restricted local object reference; never a public URL by default |
| `parent_message_id`, `reply_to_message_id` | string/null | Thread and reply relation when platform provides it |
| `raw_payload_ref` | object/null | Local encrypted reference: `store`, `object_id`, `sha256`, `expires_at`; no inline raw payload |
| `platform_extensions` | object | Namespaced, size-limited platform fields; redact secrets before storage |

### Lifecycle and persistence

1. Authenticate/verify the transport, map installation to tenant scope, validate allowlisted group, then normalize.
2. Persist the event key and inbox record transactionally before acknowledging the platform or advancing a polling cursor. Duplicate `(installation_id, idempotency_key)` is a successful no-op. Preserve event ordering per conversation where the platform supplies an order key.
3. Project a created message to Servify `ConversationMessage` only after the inbox commit; edits/deletes become explicit lifecycle events and update source status, never silently rewrite an approved knowledge version. Unknown event types are quarantined with a bounded redacted reference.
4. If the existing message schema cannot represent platform IDs/lifecycle, keep those in connector-owned inbox/source-reference records. Do not overload `models.Message.Metadata` as the idempotency ledger.
5. A source reference must include platform, installation, conversation, message, sender, timestamps, lifecycle status, and a bounded content snapshot needed for review. Knowledge consumers store the reference with the candidate and on deletion mark the source unavailable.

Existing seams and limits:

- `ConversationMessage` currently carries only conversation ID, sender, kind, content, metadata and created time ([entities.go:58](../apps/server/internal/modules/conversation/domain/entities.go#L58)).
- `AppendMessage` scopes and inserts the message, but has no external-platform key or conflict handling ([gorm_repository.go:86](../apps/server/internal/modules/conversation/infra/gorm_repository.go#L86)). This is the archive insertion seam; connector inbox dedup must precede it.
- Existing session history reads are tenant/workspace scoped ([gorm_repository.go:100](../apps/server/internal/modules/conversation/infra/gorm_repository.go#L100)); adapters must pass trusted scope context.
- The conversation event bus exposes `conversation.message_received` and creates event IDs separately from platform IDs ([events.go:12](../apps/server/internal/modules/conversation/application/events.go#L12)); do not use bus event ID as inbound dedup key.
- Legacy storage has a `models.Message` row keyed by numeric ID and content/type/sender, with no platform ID field or external-message uniqueness ([models.go:255](../apps/server/internal/models/models.go#L255)).

## Connector and outbound contract

```go
type Connector interface {
    Capabilities(ctx context.Context, installationID string) (Capabilities, error)
    Receive(ctx context.Context, sink InboundSink) error // streams/polls; sink persists before ACK/cursor advance
    Send(ctx context.Context, req SendRequest) (SendResult, error)
    Notify(ctx context.Context, req NotifyRequest) (SendResult, error)
}
type InboundSink interface { Accept(ctx context.Context, event InboundEvent) (AckDecision, error) }
```

`Send` addresses a conversation and optionally a thread/reply; `Notify` is an operator/system announcement with explicit recipient scope and audit reason. Both accept a caller idempotency key, deadline, and structured content; both return platform message ID, accepted timestamp and delivery status (`accepted`, `unknown`, `rejected`). `unknown` must not be retried blindly unless the adapter can safely deduplicate. No adapter may broaden a target audience implicitly.

`Capabilities` is observed from granted platform permissions and contains: `receive_mode`, `message_scope` (`mention_only`, `all_authorized_groups`, `unknown`), `history_read`, `event_types[]`, `threads`, `replies`, `attachments`, `edits`, `deletes`, `reactions`, `send_text`, `send_rich`, `rate_limit` and `verified_at`. Capture it per installation and group; any unknown permission means ingestion is disabled.

## Security, credentials and data handling

- Prefer polling for the first adapter. If a future webhook is used, verify the platform-defined signature/secret, timestamp/replay policy and body size before parsing or queueing; never treat TLS alone as caller authentication.
- Credentials are supplied at runtime through environment variables or a local secret store, excluded from git, logs, telemetry, support bundles and chat. Do not paste tokens into conversation. Restrict file permissions and rotate/revoke on suspected exposure.
- Raw payload and attachment retention: **30 days maximum by default**, encrypted on local storage; delete earlier on tenant request/source deletion. Normalized message retention is tenant-configured and must be approved in Phase 0. Do not send raw messages/attachments to third-party analytics or AI providers without a separate explicit tenant policy.
- Cloud backup and data export are **off by default** for connector payloads. If later enabled, require tenant opt-in, documented region/retention, encryption and restore deletion semantics. Keep payload bytes out of routine backups.
- Tenant/workspace scope must be established from the stored installation mapping, not trusted from incoming payload. Apply access controls to source references and audit admin changes to group grants.

## Rate limits, retries and error classes

The adapter owns platform-specific limit parsing and backoff. Apply a per-installation token bucket and per-conversation ordering; honor platform `Retry-After`/retry metadata. Use exponential backoff with jitter for transient failures (starting at 1s, capped at 5m); stop after 10 attempts or 24h, then dead-letter and alert without exposing content. Never retry permanent errors.

| Class | Examples | Action |
|---|---|---|
| `auth` | expired/revoked token, invalid signature | Stop connector; alert operator; no retry until credentials fixed |
| `permission` | group not authorized, full-message access absent | Disable that group; require consent/config change |
| `rate_limited` | HTTP 429 / platform throttle | Honor retry delay; reduce bucket rate |
| `transient` | timeout, 5xx, disconnect | Retry with bounded exponential backoff; preserve dedup key |
| `invalid` | malformed/unsupported payload, oversized attachment | Quarantine or reject; no retry |
| `conflict` | duplicate idempotency key | ACK as already accepted |
| `permanent` | deleted target, unsupported operation | Report to caller; no retry |
| `unknown_delivery` | send timed out after request may have reached platform | Return `unknown`; query/reconcile if possible, otherwise require operator policy before resend |

Inbound ACK is allowed only after durable inbox persistence or a recognized duplicate. Outbound retries reuse the same idempotency key. A transport ACK does not mean downstream knowledge processing succeeded; processing state is tracked independently.

## References

- Internal research: [Chatbot platforms and protocols](research/chatbot-platforms-and-protocols.md) (repository evidence; review platform docs again before implementation).
- Telegram official docs: [Bot Features](https://core.telegram.org/bots/features), [Bot API](https://core.telegram.org/bots/api), [FAQ](https://core.telegram.org/bots/faq) — checked 2026-10-19.
