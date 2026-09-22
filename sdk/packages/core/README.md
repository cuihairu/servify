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
- `sendMessage()` sends `text-message` frames over WebSocket
- Session history and lifecycle helpers use `/api/omni/sessions/*`
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
