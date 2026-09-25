# @servify/core

Shared Web SDK runtime and contract package.

## Public API

- `createServifySDK`
- `createWebServifySDK`
- `WebSocketManager`
- `ServifySDK.startRemoteAssist`
- `ServifySDK.acceptRemoteAnswer`
- `ServifySDK.addRemoteIce`
- `ServifySDK.endRemoteAssist`
- exported contracts from `src/index.ts`

## Usage

```ts
import { createWebServifySDK } from '@servify/core';

const sdk = createWebServifySDK({
  apiUrl: 'http://localhost:8080',
  wsUrl: 'ws://localhost:8080/api/v1/ws',
  autoConnect: false,
});
```

## Contract Summary

- Default WebSocket endpoint: `/api/v1/ws`
- Chat flow is WebSocket-first
- `initialize()` prepares local customer identity and can open the realtime channel
- `startChat()` creates a client-side session identity and does not call legacy REST session creation APIs
- `sendMessage()` sends `text-message` frames over WebSocket and awaits echo confirmation (PROTOCOL §6.3, mirroring the mobile `EchoGate`): the promise resolves when the customer's own echo frame arrives (content-matched — a stale echo never completes a later send) and rejects with a retryable `transport_timeout` after 10s (configurable via `echoTimeoutMs`) otherwise. Concurrent sends are serialized (one pending echo at a time, same as the mobile `sendMutex`)
- Session history and lifecycle helpers use `/api/omni/sessions/*`
- Reconnect reconcile: after every successful WebSocket connect (first connect included), the SDK pulls `GET /api/v1/sessions/{session_id}/messages?after_id=` (the visitor-only endpoint) and re-emits messages missed while disconnected as regular `message` events — pagination via `after_id` cursor + `has_more`, dedup of the pre-cursor WS window via a fingerprint table, and silence on all failures (404 = session row not created yet, IO/HTTP errors never break the WS flow). `/api/omni/sessions/*` history helpers remain admin-surface utilities (unreachable for visitors)
- AI streaming: `ai-response-delta` frames assemble into `ai-stream:delta` events (content = running total; upsert the bubble by `id`) plus one `ai-stream:end` — `interrupted: false` after the final `ai-response` `message` (drop the bubble, the final frame supersedes), `true` on disconnect mid-stream (keep the partial content and prompt a retry; no timeout — same semantics as the mobile SDKs). Streaming never emits `message` events, so the reconcile fingerprint table stays untouched; not subscribing is safe (final frames render as before)
- Human handoff: `transfer_notification` / `waiting_notification` drive `transfer:assigned` (`{agentId, message}`) / `transfer:waiting` (`{message}`) events — pure state-stream events mirroring the mobile `agentAssigned`/`waitingInQueue` flows (assigned also clears the queue state, the `waiting_human → agent_chatting` transition); they never render `message` rows and never touch session state
- AI uses `/api/v1/ai/query` and `/api/v1/ai/status`
- Upload uses `/api/v1/upload`
- Satisfaction submission uses `/api/satisfactions`

The current server contract does not expose:

- REST session creation via `/api/sessions`
- queue REST APIs
- WebRTC call REST APIs

## Remote Assistance

`@servify/core` exposes the Web SDK remote-assistance baseline:

- capability flag: `remote_assist@1`
- WebSocket signaling events: `webrtc:offer`, `webrtc:answer`, `webrtc:candidate`, `webrtc:state`
- `webrtc:state` also reflects server-pushed `webrtc-state-change` runtime updates
- `webrtc:ice-config` fires with server-pushed ICE configuration (`webrtc-ice-config` message; TURN entries carry short-lived `username`/`credential`/`ttl`)
- ICE resolution order in `startRemoteAssist`: host-provided `iceServers` wins; otherwise the SDK consumes the server-pushed config, falling back to `GET /api/v1/rtc/ice-servers`
- runtime methods:
  - `startRemoteAssist({ captureScreen?: boolean, audio?: boolean, iceServers?: RTCIceServer[] })`
  - `acceptRemoteAnswer(answer)`
  - `addRemoteIce(candidate)`
  - `endRemoteAssist()`

Current scope is session-level WebRTC signaling and optional screen capture. It is not a full co-browsing UI.
