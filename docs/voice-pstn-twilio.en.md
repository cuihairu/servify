> **English** | [中文](voice-pstn-twilio.md)

# Twilio PSTN Webhook Integration Guide

Servify consumes the two kinds of Status Callback from Twilio Programmable Voice through the hosted-vendor-webhook protocol (`platform/twiliovoice`), mapping carrier calls into the unified voiceprotocol event stream and persisting them.

## Scope and Boundaries

**What it does:**

- Consumes **Call Status Callback** (queued / ringing / in-progress / completed /
  busy / no-answer / failed / canceled), mapping them to call events and maintaining
  the `voice_calls` state machine (started → answered → ended).
- Consumes **Recording Status Callback**, writing `RecordingUrl` into
  `voice_recordings.storage_uri` (upsert semantics; also accepted when it arrives before our own record).
- Forwards the Gather result `Digits` as a DTMF event.
- `X-Twilio-Signature` signature verification (HMAC-SHA1, full URL + form parameters
  concatenated in lexicographic name order, constant-time comparison).

**What it does not do:**

- **Does not produce TwiML responses**: the webhook always returns an empty 200 response (Status Callback
  only needs 2xx). Inbound calls are not answered, not transferred to an agent, and no IVR is played.
- **Does not place outbound calls**: outbound calls are out of scope for this path.
- **Does not transcribe**: the Deepgram transcription chain is independent of the PSTN callback and not connected.

## Configuration

```yaml
server:
  public_base_url: "https://support.example.com"   # 必须:验签 URL 的组成部分

voice:
  pstn:
    provider: "twilio"          # disabled(默认,路由不存在)| twilio
    validate_signature: true    # 默认 true;仅本地调试时才建议关闭
  twilio:
    auth_token: "${TWILIO_AUTH_TOKEN}"   # 与 Twilio 控制台一致
```

- `auth_token` reuses `voice.twilio.auth_token`; no new secret surface is added.
- `provider: twilio` with an empty `auth_token` ⇒ **startup fails** (refusing to bring an
  anonymous endpoint online without signature verification).
- `public_base_url` must be set: Twilio signs the full URL, a reverse proxy rewrites the Host, and only
  an explicitly configured external base address can reproduce the signing material. It can be left empty locally when there is no reverse proxy (falls back to the
  request's own scheme/host).

## Twilio Console Side

Configure **Voice Configuration → Call Status Callback** for the target phone number (or SIP domain):

- Check all statuses under **When a call status changes**;
- URL: `https://<your-domain>/public/voice/webhooks/twilio` (POST);
- Recording: fill the same URL into the Recording Status Callback;
- This path is fixed and cannot be changed — the path is part of the signing material.

## Idempotency and Retry Semantics

Twilio retries non-2xx responses with exponential backoff. This path therefore:

- **Determines event identity**: `twilio-<kind>-<CallSid>`,
  `twilio-recording-<RecordingSid>`;
- **Coordinator state guard**: for duplicates such as invite/hangup/hold, calls that have already reached the state are
  short-circuited directly (200, no duplicate outbound `call.*` webhook); when hangup arrives before invite
  it returns 500, borrowing Twilio's retry to self-heal after the invite lands;
- **Recording completion is an upsert**: repeated callbacks for the same RecordingSid are safe;
- Parameter errors (unknown CallStatus, missing CallSid) return 400 — errors that retries cannot self-heal,
  naturally terminated by Twilio's retry limit.

No per-EventID replay ledger: the guards above already cover Twilio's actual retry patterns.

## Security Surface

- Anonymous endpoint; signature verification is the authentication. Signature failure returns 403, and so does a missing signature header.
- POST only, fixed path, and `/public/voice/webhooks/` is registered in the security surface
  catalog (the route exists only when the provider is not disabled).
- The global rate limiting middleware covers this path; responses never echo internal error details (signature failure uniformly
  returns "webhook signature rejected").

## Acceptance

`scripts/test-voice-pstn-acceptance.sh` performs end-to-end verification against a real running service
(signed invite → idempotency → answer → hangup → recording persistence → 403/400 negative samples):

```bash
TWILIO_AUTH_TOKEN=<token> SERVIFY_URL=http://localhost:8080 \
  SQLITE_DB=/path/to/servify.db \
  bash scripts/test-voice-pstn-acceptance.sh
```

Optional enhancement: `ADMIN_TOKEN=<jwt>` adds outbound assertions (create a call.started subscription + verify receipt by a local
receiver); `PSQL_CMD="ssh host psql -t -A servify"` substitutes postgres
for sqlite to assert persistence.

## Known Boundaries (Deferred Items)

- Call audio is not stored in Servify storage: `RecordingUrl` points to Twilio-side media, and long-term
  archiving requires fetching it separately (a sync task can be added on top of this path).
- hold/resume/transfer are outside the expressible scope of Call Status Callback; the adapter
  explicitly reports these three event types as unsupported.
- per-EventID replay ledger, TwiML responses, outbound calls: see "What it does not do" above.
