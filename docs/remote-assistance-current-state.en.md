> **English** | [中文](remote-assistance-current-state.md)

# Remote Assistance Current State Inventory

> ⚠️ **This document has been superseded by [remote-assistance-status.md](./remote-assistance-status.md)**: the `assist` permission group now carries the seven endpoints of `/api/remote-assist/*` (remote assistance has landed, `router_management.go` RequireResourcePermission("assist")). This document is kept as an archive of the 2026-09 "capability inventory / gap identification" point in time.

This document closes `Gap C / G3-2`: an inventory of which capabilities Servify currently has — on the server side, the admin side, and in product expression — that can already serve as a foundation for remote assistance, which remain only low-level capabilities, and which product entry points are still missing.

## Conclusion First

The current repo already has multiple foundational capabilities required for remote assistance, but they have not yet been converged into a standalone "remote assistance workspace" or a clear product operation flow.

The current state can be understood as:

- The server side already has foundational capabilities such as sessions, messages, transfers, suggestions, voice, WebSocket, and WebRTC statistics.
- The admin side already has scattered entry points such as session management, routing management, voice management, and ticket detail.
- The admin conversation page has already gained the minimal "start assistance / end assistance" entry, and can display session-level WebSocket / WebRTC connection states.
- The product narrative has already begun to uniformly emphasize remote assistance.
- But the admin side still has no page explicitly named "remote assistance", and has not yet chained the "start assistance → continuous takeover → transfer/create ticket" operation path into a single entry point.

## Existing Server-side Entry Points

### 1. Conversation and Human Takeover Main Path

The server-side entry points closest to the remote assistance main chain today are the conversation and transfer-related APIs:

- `GET /api/omni/sessions/:session_id`
- `GET /api/omni/sessions/:session_id/messages`
- `POST /api/omni/sessions/:session_id/messages`
- `POST /api/omni/sessions/:session_id/assign`
- `POST /api/omni/sessions/:session_id/transfer`
- `POST /api/omni/sessions/:session_id/close`

These entry points show:

- Servify already has a main line of "continuously handling issues around a session".
- Human takeover, message reception, and transfers all happen within the same session context.
- This chain naturally fits carrying the business states of "before, during, and after remote assistance".

### 2. Transfer and Waiting Queue

The routing layer currently also has a set of more operations-scheduling-oriented entry points:

- `GET /api/session-transfer/waiting`
- `GET /api/session-transfer/history/:session_id`
- `POST /api/session-transfer/to-agent`
- `POST /api/session-transfer/cancel`
- `POST /api/session-transfer/process-queue`

These entry points show:

- The system already supports managing "who continues handling a session" separately.
- After remote assistance ends, the capability to continue transferring to a more suitable handler already has a server-side foundation.
- But it currently behaves more like a routing/queue capability, not a product flow named "remote assistance".

### 3. Suggestions and Assist Capabilities

Under the `assist` permission, what currently hangs is the suggestion capability:

- `GET /api/assist/suggest`
- `POST /api/assist/suggest`

This means:

- The permission name "assist" already exists.
- But the current `/api/assist/*` leans toward suggestions and knowledge assistance; it is not the full remote assistance product entry point.

### 4. Real-time Runtime and Observability Entry Points

The server currently exposes remote-assistance-related real-time foundational capabilities:

- `GET /api/v1/ws`
- `GET /api/v1/ws/stats`
- `GET /api/v1/webrtc/stats`
- `GET /api/v1/webrtc/connections`

Along with voice protocol and recording-related entry points:

- `GET /api/voice/protocols`
- `POST /api/voice/recordings/start`
- `POST /api/voice/recordings/stop`
- `GET /api/voice/recordings/:recordingID`
- `POST /api/voice/transcripts`
- `GET /api/voice/transcripts`

These entry points show:

- Real-time connections, WebRTC connection observation, voice recording, and transcription are all already system capabilities.
- But many of them remain "foundational runtime / protocol capabilities", not remote assistance product interactions directly facing customer service.

## Existing Admin-side Entry Points

### 1. Related Pages That Already Exist

The admin side currently has no standalone "remote assistance" menu, but the capabilities are scattered across the following pages:

- `/conversation`
  - Conversation viewing, message reading and sending
  - Minimal remote assistance entry: start/end assistance, signaling state hints, remote media placeholder
- `/routing`
  - Waiting queue, transfer history, transfer to agent for handling
- `/voice`
  - Protocol, recording, transcription-related management
- `/ticket/detail/:id`
  - Conversation and ticket association viewing
- `/security`
  - Session security and device/IP risk view

### 2. Service Calls the Admin Side Has Already Wired

Looking at the frontend services, the following are already wired:

- `conversation.ts`
  - Get conversations
  - Get/send messages
  - Assign agents
  - Transfer conversations
  - Close conversations
- `sessionTransfer.ts`
  - Get waiting queue
  - Get transfer history
  - Initiate transfer
  - Cancel transfer
  - Process queue
- `voice.ts`
  - List protocols
  - Start/stop recording
  - Query recordings
  - Query transcriptions

This shows the admin side already has a set of remote assistance capabilities; they are just scattered, not yet uniformly named and aggregated by the product layer.

## The Most Obvious Gaps Today

### 1. Missing Unified Product Entry Point

What is currently missing:

- A standalone "remote assistance" menu
- A remote assistance status page
- A dedicated assistance workspace that can sustain operations after leaving the conversation page

So users can currently enter the minimal assistance state from the conversation page, but what they see is still not a complete, standalone product workflow.

### 2. Missing State Machine and Operation Semantics

What the current code already has:

- Conversation
- Assignment
- Transfer
- Close
- Ticket

But what is still missing is explicit product semantics, such as:

- When "remote assistance is initiated" counts
- What the difference is between in-assistance and a normal human conversation
- After assistance completes, whether it ends, transfers, or creates a ticket

### 3. Missing Minimal Demo Chain

What can currently be proven:

- WebSocket / WebRTC runtime is usable
- Conversations and messages are usable
- Transfers are usable
- Tickets can continue to advance

But what is still missing is a minimal product-demo-oriented chain description, for example:

1. The customer enters a Web conversation
2. The AI first answer fails or guidance is needed
3. Human takeover
4. Initiate remote assistance
5. Complete the operation or joint troubleshooting
6. Continue transferring or convert to a ticket

### 4. Missing Dedicated Admin-side View

What is still missing clear evidence that the admin side already has:

- A complete remote assistance dedicated panel
- Assistance process state hints
- Post-assistance result record fields
- An explicit linkage view between assistance and tickets

This is also the scope `G3-3` needs to continue defining.

## Current Judgment

Therefore, the most accurate product spec right now should be:

- Servify already has the real-time and conversation foundational capabilities required for remote assistance.
- These capabilities can already support the product direction of "upgrading from chat to real-time collaboration".
- But the admin side and the product experience layer still lack a unified entry point, a clear state machine, and a minimal deliverable demo chain.

Next steps should continue advancing:

1. Define the minimal deliverable remote assistance chain
2. Clarify demo steps, acceptance evidence, and page entry points
3. Decide whether to first enhance the unified conversation page or add a standalone remote assistance page
