# @servify/vanilla

Vanilla JavaScript surface for the Servify Web SDK.

## Usage

```html
<script src="../../packages/vanilla/dist/index.js"></script>
```

Examples live in `sdk/examples/vanilla`.

## Remote Assistance

The vanilla surface now forwards the core Web SDK remote-assistance API:

- `startRemoteAssist(options?)`
- `acceptRemoteAnswer(answer)`
- `addRemoteIce(candidate)`
- `endRemoteAssist()`

Forwarded events:

- `webrtc:offer`
- `webrtc:answer`
- `webrtc:candidate`
- `webrtc:track`
- `webrtc:state`

## Streaming & Human Handoff

The vanilla surface also forwards the core Web SDK streaming and handoff event surfaces:

- `ai-stream:delta` / `ai-stream:end` — AI streaming contract (see `sdk/PROTOCOL.md` §4.1)
- `transfer:assigned` (`{agentId, message}`) / `transfer:waiting` (`{message}`) — human-handoff state-stream events mirroring the mobile `agentAssigned`/`waitingInQueue` flows; pure events, they never render `message` rows
- `unread-change` (`count`) — unread count (§4.3); plus `getUnreadCount()` / `markSessionVisible()` / `markSessionHidden()` for panel-visibility wiring
