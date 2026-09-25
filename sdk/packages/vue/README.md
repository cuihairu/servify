# @servify/vue

Vue surface for the Servify Web SDK.

## Usage

```ts
import { ServifyPlugin } from '@servify/vue';
```

Available composables:

- `useServify()`
- `useServifyReady()`
- `useChat()`
- `useAI()`
- `useTickets()`
- `useSatisfaction()`
- `useRemoteAssist()`

## Streaming & Human Handoff

`useChat()` exposes two event-driven refs on top of `@servify/core`:

- `agentAssigned: Ref<TransferAssignmentUpdate | null>` — set by `transfer:assigned` (`{agentId, message}`); an assignment also clears `waitingInQueue` (the `waiting_human → agent_chatting` transition)
- `waitingInQueue: Ref<TransferWaitingUpdate | null>` — set by `transfer:waiting` (`{message}`)

Both are pure state-stream surfaces: they never render `message` rows and are cleared on `sessionEnded` / `endChat()`. AI streaming is available through the core `ai-stream:delta` / `ai-stream:end` events (see `sdk/PROTOCOL.md` §4.1).

## Remote Assistance

`useRemoteAssist()` now exposes the same remote-assistance baseline as the React surface:

- `state`
- `isActive`
- `error`
- `remoteStream`
- `startRemoteAssist(options?)`
- `acceptRemoteAnswer(answer)`
- `addRemoteIce(candidate)`
- `endRemoteAssist()`
