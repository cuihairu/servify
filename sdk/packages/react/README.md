# @servify/react

React surface for the Servify Web SDK.

## Usage

```tsx
import { ServifyProvider } from '@servify/react';
```

Examples live in `sdk/examples/react`.

## Streaming & Human Handoff

`useChat()` exposes two event-driven states on top of `@servify/core`:

- `agentAssigned: TransferAssignmentUpdate | null` — set by `transfer:assigned` (`{agentId, message}`); an assignment also clears `waitingInQueue` (the `waiting_human → agent_chatting` transition)
- `waitingInQueue: TransferWaitingUpdate | null` — set by `transfer:waiting` (`{message}`)
- `unreadCount: state<number>` — unread count (§4.3): incremented while the session page is hidden (`markSessionHidden()`), cleared by `markSessionVisible()`; agent/AI content only

Both are pure state-stream surfaces: they never render `message` rows and are cleared on `sessionEnded` / `endChat()`. AI streaming is available through the core `ai-stream:delta` / `ai-stream:end` events (see `sdk/PROTOCOL.md` §4.1).

## Remote Assistance

The React surface now provides `useRemoteAssist()` on top of `@servify/core`.

Returned API:

- `state`
- `isActive`
- `remoteStream`
- `startRemoteAssist(options?)`
- `acceptRemoteAnswer(answer)`
- `addRemoteIce(candidate)`
- `endRemoteAssist()`
