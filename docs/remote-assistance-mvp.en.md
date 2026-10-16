> **English** | [中文](remote-assistance-mvp.md)

# Remote Assistance Minimum Deliverable Path

This document closes `Gap C / G3-3`: defining the minimum remotely assistable path Servify can express externally at the current stage, and the corresponding acceptance spec.

## Goals

This minimum path only proves the following, it does not chase a full remote-control product:

- Servify can already place customer sessions, manual takeover, real-time collaboration basics, transfers, and the full ticket flow in one continuous service path
- The admin console already has enough existing pages and interfaces to support one demo round
- Product narrative and actual capability do not contradict each other

## Minimum path definition

The current recommendation defines the minimum remotely assistable path as:

1. Customer initiates a session from the Web entry
2. AI or plain text communication cannot directly resolve the issue
3. Agent takes over the current session in the conversation page
4. Agent continues sending messages in the same session, and if necessary transfers to a more suitable agent
5. If real-time collaboration basics are needed, confirm the WebSocket / WebRTC runtime is connected, and the voice/real-time protocol-side entry is available
6. After assistance is complete, the session can continue to be closed, or the issue can be distilled into ticket details for continued progression

This path emphasizes:

- "Escalation to assistance" happens within an existing session context
- After assistance it stays within Servify's internal path, not jumping to external systems
- Right now acknowledge "real-time collaboration basics are in place" first, rather than fabricating a heavyweight assistance UI that has not been implemented yet

## Corresponding pages and interfaces

### 1. Session reception and takeover

Admin console pages:

- `/conversation`

Core interfaces:

- `GET /api/omni/sessions/:session_id`
- `GET /api/omni/sessions/:session_id/messages`
- `POST /api/omni/sessions/:session_id/messages`
- `POST /api/omni/sessions/:session_id/assign`
- `POST /api/omni/sessions/:session_id/transfer`
- `POST /api/omni/sessions/:session_id/close`

Current product semantics:

- This is the existing entry closest to a "main workbench" for remote assistance
- It can currently complete actions like viewing sessions, sending messages, takeover, transfer, and close

### 2. Transfer and dispatch

Admin console pages:

- `/routing`

Core interfaces:

- `GET /api/session-transfer/waiting`
- `GET /api/session-transfer/history/:session_id`
- `POST /api/session-transfer/to-agent`
- `POST /api/session-transfer/cancel`
- `POST /api/session-transfer/process-queue`

Current product semantics:

- When a front-line agent cannot continue handling, remote assistance should not break the chain but be able to enter transfer dispatch

### 3. Real-time collaboration basics

Admin console pages:

- `/voice`

Server-side observability/protocol entries:

- `GET /api/v1/ws/stats`
- `GET /api/v1/webrtc/stats`
- `GET /api/v1/webrtc/connections`
- `GET /api/voice/protocols`
- `POST /api/voice/protocols/:protocol/call-events/:event`
- `POST /api/voice/protocols/:protocol/media-events/:event`
- `POST /api/voice/recordings/start`
- `POST /api/voice/recordings/stop`
- `GET /api/voice/transcripts`

Current product semantics:

- This part proves the system has real-time collaboration, voice, and connection observability basics
- But currently it is still mainly runtime / protocol / operations capability, not equal to a standalone "remote assistance UI"

### 4. Post-assistance full ticket flow

Admin console pages:

- `/ticket/detail/:id`

Core interfaces:

- `GET /api/tickets/:id`
- `GET /api/tickets/:id/conversations`
- `POST /api/tickets/:id/comments`
- `POST /api/tickets/:id/close`

Current product semantics:

- When an issue cannot be resolved immediately in the session, the context can continue to be distilled into a ticket
- The ticket detail page can continue viewing associated sessions, rather than the assistance record being completely disconnected

## Content currently not included in the MVP commitment

The following should not currently be written as "already delivered" capabilities:

- A standalone "remote assistance" menu or workbench
- Standardized co-browsing / screen-sharing operation interfaces
- Dedicated state machine fields during assistance, e.g. `assisting` / `assistance_completed`
- Assistance result templates, assistance record tables, or dedicated reports

These belong to later product enhancement items and should not be mixed with the current minimum deliverable scope.

## Minimum acceptance spec

### Automated evidence

At minimum continue retaining the following automated evidence:

- Web sessions and real-time basics
  - `go test ./apps/server/internal/handlers -run 'Test(ConversationWorkspaceHandler_(ListMessages|SendMessage|AssignAgent|Transfer|CloseSession|GetSession)|AuthHandlerRefreshToken|AuthHandlerSelfServiceSessions)'`
- Public entries and real-time entry runtime evidence
  - The acceptance checklist already has actual-run records for `GET /api/v1/ws/stats`, `GET /api/v1/webrtc/stats`, `GET /api/v1/webrtc/connections`, and voice protocol entries
- Ticket-associated sessions
  - The `ticket` detail page and `GET /api/tickets/:id/conversations` already have backend test coverage

### Manual demo steps

The minimum manual demo is recommended in the following order:

1. Open `/conversation`, select an active session
2. Send a message, confirm the session context persists
3. Execute "take over session"
4. Execute one "transfer session" or view the waiting queue/transfer history in `/routing`
5. Verify the real-time basics are still online:
   - View `/voice`
   - Or call `GET /api/v1/ws/stats`, `GET /api/v1/webrtc/stats`
6. Open the associated ticket detail `/ticket/detail/:id`, confirm the session context is still traceable

### Pass criteria

This round's pass for "remote assistance MVP" is defined as:

- Session, takeover, transfer, close actions can all be completed in the same session main path
- Real-time basic capability has a clear entry and runtime evidence
- After assistance it can continue into transfer or tickets, rather than breaking at the session page
- README, doc site home page, topic page, and current implementation use the same spec

## Next enhancement directions

If iteration continues later, the recommended priority order is:

1. Add a clearer "remote assistance" entry or conversation-page enhanced state in the admin console
2. Add a state machine and record model for the assistance process
3. Upgrade the demo path to a real product acceptance script
