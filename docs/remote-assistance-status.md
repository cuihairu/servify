# Remote Assistance Capability Matrix and Gap List

> Inventory date: 2026-09-25 (updated 2026-09-26: the RA-1..RA-4 fixes and the RA-8 server half have landed, verdicts refreshed accordingly; updated 2026-10-14: the RA-8 SDK half (resending the offer after WS reconnect) has landed and the WebRTC recovery entry is now marked implemented; updated 2026-10-15: RA-7 media bridging (route A SFU-lite) and RA-5 signaling ownership-directed delivery have landed, and screen sharing is now connected end-to-end over the server loopback path). This document is the **current source of truth** for remote assistance capability, replacing
> [remote-assistance-current-state.md](./remote-assistance-current-state.md)
> (the old 2026-04 inventory) as the factual baseline; product direction and V1 positioning are in
> [remote-assistance.md](./remote-assistance.md), and the minimal path is in
> [remote-assistance-mvp.md](./remote-assistance-mvp.md).
>
> Verdict convention: **implemented** = the end-to-end path is genuinely usable and backed by test/acceptance evidence;
> **partially implemented** = the pipeline or a sub-path exists, but the end-to-end path has a break or a semantic gap;
> **not implemented** = no corresponding code in the repo.

## Conclusions First

- The assistance **record surface** (start/end/list/detail/annotation/recording metadata) is implemented with real run acceptance;
- The assistance **realtime surface** has both endpoint skeletons in place (visitor SDK capture, admin-side video placeholder, WS signaling frames, TURN delivery), and **server-side media bridging (RA-7 route A SFU-lite) has landed**: visitor published tracks are rebuilt by the server and forwarded to the agent PC, and the agent side receives tracks via the server-initiated renegotiation; the end-to-end picture is connected over the server loopback path (real pion dual PC + RTP) (`TestWebRTC_LoopbackMediaBridge`), but a **browser-level two-ended acceptance script is still missing**;
- The **counterparty consent (consent)** minimal runnable path is implemented (visitor endpoint + declined terminal state + recording write-back block); **remote control** is not implemented; **WebRTC disconnect recovery** has landed (the server reclaims terminal-state old PCs before re-issuing an offer + the SDK resends the unfinished offer after WS reconnect);
- Tenant/workspace isolation and the single-active constraint are fixed (RA-1/2, see "This Round's Fix Log");
- The signaling ambiguity for dual-ended PCs on the same sessionID is fixed (RA-5): the server delivers answer/candidate/state frames directed by WS connection identity, and legacy callers without ownership fall back to the first hit in the session.

## Capability Matrix

### 1. Initiating and Accepting Assistance: Partially Implemented

| Sub-capability | Verdict | Evidence |
| --- | --- | --- |
| Start (admin surface) | Implemented | `POST /api/remote-assist/sessions` (`apps/server/internal/handlers/assist_handler.go:44`) creates an active record; the admin-side `handleStartRemoteAssist` (`apps/admin/src/pages/Conversation/index.tsx:645`) writes the record via REST then opens a dedicated signaling WS |
| Start (visitor surface) | Implemented | SDK `startRemoteAssist` (`sdk/packages/core/src/sdk.ts:318`): creates PC + DataChannel + screen capture + recording |
| End | Implemented | `POST /api/remote-assist/sessions/:id/end` (`assist_handler.go:84`, repeat end returns 409); recording metadata can be persisted along with the end |
| Accept/join | Partially implemented | The agent joins via the same-session WS + `RTCPeerConnection` (`index.tsx:690-797`); **there is no "invite → accept/decline" handshake** — the visitor side calls `startRemoteAssist` on its own from the host page and does not know an assistance was initiated |
| Invitation ID delivery | Not implemented (end-to-end) | The agent sends `{type:'assist-session', assist_id}` over the DataChannel (`index.tsx:704`); the server's `OnDataChannel` broadcasts it as a WS `data-channel-message` frame (`apps/server/internal/platform/realtime/webrtc_service.go:188`); but the visitor SDK **only listens on its own DataChannel onmessage** (`sdk.ts:342`), nobody consumes that frame at the WS layer, so assist_id never actually reaches the visitor side |

### 2. Permissions and Counterparty Consent: Implemented (minimal runnable path; the UI notification surface depends on RA-10)

| Sub-capability | Verdict | Evidence |
| --- | --- | --- |
| Admin-surface RBAC | Implemented | `apps/server/internal/app/server/router_management.go:15-31`: AuthMiddleware + EnforceRequestScope + RequirePrincipalKinds(agent/admin/service) + RequireResourcePermission("assist") |
| Visitor-surface ownership check | Implemented | Both recording write-back and consent response validate session ownership (`apps/server/internal/modules/assist/application/service.go` `AttachRecording`/`RespondConsent` check owner) |
| Counterparty consent (consent) | Implemented (minimal runnable path) | `ConsentStatus/ConsentAt` fields (`domain/models.go`) + `POST /api/v1/remote-assist/:id/consent` (`assist_recording_handler.go`): granting keeps active, declining ends the session and blocks recording write-back; the same response is idempotent, an opposite response returns 409. Gap: notifying the visitor to pop the consent UI after the agent initiates depends on the RA-10 invitation frame |
| Tenant/workspace isolation | Implemented (fixed this round) | `StartCommand` no longer accepts a request-body self-declared scope (`assist_handler.go`); values taken from ctx + repository `applyScopeFilter`/sessions subquery guards (`infra/gorm_repository.go`), same convention as the macro/satisfaction modules; cross-scope 404 semantics |

### 3. Screen Sharing and Remote Control: Screen sharing implemented (server loopback evidence) / Remote control not implemented

| Sub-capability | Verdict | Evidence |
| --- | --- | --- |
| Visitor screen capture | Implemented | `getDisplayMedia` (`sdk.ts:987-996`) + `addTrack` (`sdk.ts:374-384`); errors explicitly when capture capability is absent |
| Agent video placeholder | Implemented | `remoteAssistVideoRef` + `peer.ontrack` attaches srcObject (`index.tsx:738-746`), the offer carries `offerToReceiveAudio/Video` (`index.tsx:796-797`) |
| Media path (server-side bridging) | Implemented (RA-7 route A, 2026-10-15) | Signaling remains a **server-terminated mode**: visitor and agent each connect one PC to the server; the server PC's `OnTrack` registers forwarding tracks (one read + N writes, `webrtc_service.go` `trackForwarder`), rebuilt via `NewTrackLocalStaticRTP` and attached to the session's other live PCs, with the **server actively initiating renegotiation** (`negotiate` pushes a `webrtc-offer` frame, the agent side answers and returns it); late joiners are topped up via `replicateTracksTo`. The admin-side `ontrack` has a group-stream fallback for bridged tracks without a MediaStream. Loopback evidence: `TestWebRTC_LoopbackMediaBridge` (real dual pion PCs, visitor RTP → server forwarding → agent receives a VP8 track); browser-level acceptance is still pending |
| DataChannel passthrough | Partially implemented | The server's `OnDataChannel` broadcasts received text back to the same session as `data-channel-message` (`webrtc_service.go:188-207`), and `SendDataChannelMessage` can deliver downstream (`webrtc_service.go:387`); `SendText` is plain-text only, no binary, and see the "visitor side does not consume WS frames" break above |
| Remote control (mouse/keyboard) | Not implemented | No co-browsing / input-event forwarding code anywhere in the repo (Go + apps/admin + sdk); annotations (rect/freehand/arrow) are **recording-playback annotations** (`domain/models.go:38`), not live screen annotations |
| WebRTC connection observability | Implemented | `GET /api/v1/webrtc/stats`, `/api/v1/webrtc/connections`, `/api/v1/rtc/ice-servers` (`apps/server/internal/app/server/router_realtime.go:51-54`); connection state changes are delivered via the `webrtc-state-change` frame |

### 4. File Transfer: Implemented (one-shot upload form)

- `POST /api/v1/upload` (`apps/server/internal/handlers/upload_handler.go:55-90`): single-request multipart, default 32MB cap (`router_auth.go:127`), extension allowlist, dual local/S3 provider (S3 mode 302-redirects via a presigned URL).
- The SDK's `uploadFile` hits the same endpoint, with the URL attached to the message (`sdk/packages/core/src/sdk.ts:544`).
- Assistance recording files upload over the same path, with metadata written back via `POST /api/v1/remote-assist/:id/recording` (`assist_recording_handler.go:44`).
- Boundary: **no WS streaming / chunking / resumable upload**; large files are bounded by the 32MB cap. Good enough for the support scenario; registered as a known boundary rather than a gap.

### 5. Disconnect Reconnect and Session Recovery: Partially Implemented

| Sub-capability | Verdict | Evidence |
| --- | --- | --- |
| WS auto-reconnect | Implemented | SDK exponential backoff: default 5 attempts, starting at 1s, ×2, capped at 30s (`sdk/packages/core/src/contracts/reconnect.ts:8-13`); no reconnect on manual close |
| Message backfill | Implemented | After reconnect, `reconcileMissedMessages` backfills page-by-page on the lastMessageId cursor and dedups (`sdk.ts:686 起`); the server provides `ListAfter` (`router_realtime.go:35`) |
| Assistance record recovery | Implemented | Assistance sessions / annotations / recording metadata are all persisted and survive process restarts (`infra/gorm_repository.go`) |
| WebRTC recovery | Implemented (RA-8 in place at both ends, unit-tested) | Before `HandleOffer`, the server reclaims terminal-state old PCs on the same session (`webrtc_service.go` `closeTerminalConnections`, only disconnected/failed/closed; live PCs untouched); on the SDK side, when WS fires `connected` and the PC is alive while the offer has not finished negotiating (offered/connecting states), it resends the same offer (`sdk.ts` `resendRemoteAssistOfferIfNeeded`; the reference is cleared once negotiation enters a terminal state to prevent duplicate sends; the PC survives WS reconnects, media does not depend on WS). hub unregister deliberately does not clean up rtc connections: a PC staying alive across WS reconnects is RA-8's existing semantics, and reclamation happens at the next offer's terminal-state reclaim or an explicit `CloseConnection` |

### 6. Concurrent Sessions: Implemented

| Sub-capability | Verdict | Evidence |
| --- | --- | --- |
| Multiple support sessions in parallel | Implemented | The session model and WS hub broadcast by session_id, naturally supporting multiple concurrent sessions; assistance records are keyed by conversation_session_id |
| One active assistance per session | Implemented (fixed this round) | `StartSession` first checks for an active record on the same session; on a hit it returns `ErrAssistSessionActive` (HTTP 409, `application/service.go`); can be re-initiated after ending |
| Dual-ended PC ambiguity on the same session | Implemented (RA-5, 2026-10-15) | Each PC records the owning WS connection ID for signaling (`WebRTCConnection.ownerClientID`, derived from the WS connection of the offer that created it; the client does not need to know); answer/candidate are looked up by ownership via `connByOwner`, and server-delivered candidate/state/renegotiation offers are delivered in a directed fashion via `wsHub.SendToClient` (`sendToOwner`); legacy callers without ownership (REST surface, adapter) fall back to the first hit in the session, with existing behavior unchanged |

### 7. Audit Logging: Implemented

| Sub-capability | Verdict | Evidence |
| --- | --- | --- |
| Admin-surface audit | Implemented | Assistance management routes sit under `AuditMiddleware` (`router_management.go:19`); write operations persist action/resource inferred from the route (`apps/server/internal/platform/audit/gin_middleware.go:31-67`) |
| Visitor-surface audit | Implemented (fixed this round) | The two visitor routes recording/consent are mounted with `AuthMiddleware + AuditMiddleware` (`router_auth.go`); both recording write-back and consent response leave an audit trace |
| Aggregate reporting | Implemented | `GetRemoteAssistTicketStats` (`apps/server/internal/modules/analytics/infra/gorm_repository.go`) is wired into the stats surface (empty set COALESCEs to zero; no 500 in environments without tickets) |

### 8. Security and Authentication: Partially Implemented

| Sub-capability | Verdict | Evidence |
| --- | --- | --- |
| Admin-surface auth chain | Implemented | See the admin-surface RBAC in row 2; the audit group has a separate audit permission to view traces |
| TURN/STUN | Implemented | coturn deployed standalone + time-limited HMAC short-lived credentials (`apps/server/internal/platform/iceturn/iceturn.go:65-77`), with a two-layer config gate (`config.go:828-834` warning + `iceturn.go:37-43` Validate rejection); ICE config is delivered over two channels, WS `webrtc-ice-config` (`websocket_hub.go:250-254`) and REST `GET /api/v1/rtc/ice-servers`; deliverable asset `infra/compose/docker-compose.coturn.yml` |
| Agent-side ICE | Implemented (fixed this round) | The admin side consumes server-delivered config: WS `webrtc-ice-config` push cache → REST `GET /api/v1/rtc/ice-servers` fallback (`services/remoteAssist.ts` `getIceServers`); only when both are empty does it fall back to public STUN — same convention as the visitor SDK (`sdk.ts` `resolveServerIceServers`) — and under strict networks the server TURN catches it |
| Data-surface tenant isolation | Implemented (fixed this round) | See "tenant/workspace isolation" in row 2 |

## Gap List and Priorities

Ordered by "security > correctness > audit > product semantics > large realtime-surface items":

| ID | Gap | Priority | Type | Status |
| --- | --- | --- | --- | --- |
| RA-1 | assist tenant/workspace isolation (values from ctx + repository filtering + cross-scope 404) | P0 | Security | Fixed this round |
| RA-2 | Single active assistance per support session (repeat initiation 409) | P0 | Correctness | Fixed this round |
| RA-3 | Audit of visitor-surface (recording/consent) write operations | P1 | Audit | Fixed this round |
| RA-4 | Counterparty consent (consent) state machine: pending→granted/declined + visitor endpoint + declined rejects recording write-back | P1 | Product semantics | Fixed this round (minimal runnable path) |
| RA-5 | Dual-ended PC lookup ambiguity on the same session (signaling frames carry no peer identity; a protocol extension seemed needed) | P2 | Correctness/protocol | Landed (signaling ownership-directed delivery, no client protocol change needed; see the 2026-10-15 section) |
| RA-6 | Hardcoded agent-side ICE (consume the webrtc-ice-config delivery) | P2 | Usability | Fixed this round (admin side) |
| RA-7 | Screen-share media bridging (server-side track forwarding SFU-lite, or signaling switched to peer relay) | P2 | Large realtime-surface item | Landed (route A; see the 2026-10-15 section) |
| RA-8 | WebRTC disconnect recovery (renegotiation after reconnect + server-side old-PC cleanup) | P3 | Large realtime-surface item | Landed (server-side terminal-state old-PC reclaim + SDK resends the offer on disconnect) |
| RA-9 | Remote control (input events relayed over DataChannel + visitor-side authorization switch) | P3 | Large realtime-surface item | Registered as design |
| RA-10 | End-to-end invitation handshake (WS assist-invite frame, or fix data-channel-message consumption) | P2 | Protocol | Registered as design |

### Implementation Direction for RA-9/10 (Not This Round)

- **Media bridging (RA-7)**: landed via route A (2026-10-15, see the section below); route B (signaling peer relay) is no longer needed.
- **Disconnect recovery (RA-8)**: done (both ends in place). Before `HandleOffer`, the server first closes terminal-state old PCs on the same session (only terminal-state PCs are reclaimed; live PCs under the RA-5 ambiguity are untouched); the SDK resends the same unfinished offer after WS `connected` — the PC survives WS reconnects (media does not depend on WS; only the `webrtc-offer` signaling frame is lost), minting a new offer mid-negotiation violates the signaling state machine, so the original offer is resent and the server answers idempotently.
- **Remote control (RA-9)**: a visitor-side switch (off by default) + DataChannel JSON events (pointer/scroll/key) + synthetic dispatch on the visitor side; consent must be granted before enabling is allowed (linked with RA-4).
- **Invitation handshake (RA-10)**: a new WS downstream frame `assist-invite {assist_id}` (reusing session broadcast); the visitor SDK pops the consent UI on it, converging with RA-4's consent endpoint; the frame contract must be synced into `sdk/PROTOCOL.md` and fixtures.

## This Round's Fix Log (2026-09-25)

RA-1..RA-4, RA-6, and the RA-8 server half have landed under the "server-side testable first" approach (the media items RA-5/RA-7 were closed out on 2026-10-15, see the corresponding sections; RA-9/RA-10 remain registered per the direction in the section above):

- **RA-1 tenant/workspace isolation**: `StartCommand` no longer accepts request-body self-declared `tenant_id/workspace_id`; values are taken from the ctx injected by the auth middleware (`platformauth.TenantIDFromContext/WorkspaceIDFromContext`, same pattern as the macro/satisfaction modules); the repository filters `GetSession/ListSessions` by scope, guards `ListAnnotations/DeleteAnnotation` via a sessions subquery, and the new `FindActiveSessionIDByConversation` also goes through scope (`infra/gorm_repository.go`). Cross-scope reads and writes uniformly return `ErrAssistNotFound` (404 semantics, no existence leak). With no scope ctx (local dev / legacy paths) there is no filtering; behavior stays compatible.
- **RA-2 single-active constraint**: `StartSession` first checks for an active record on the same session; on a hit it returns `ErrAssistSessionActive` (HTTP 409); can be re-initiated after ending.
- **RA-3 visitor-surface audit**: the two visitor routes recording/consent in `router_auth.go` are mounted with `AuthMiddleware + AuditMiddleware`; sensitive visitor-side write operations leave a trace.
- **RA-4 consent state machine (minimal runnable path)**: `RemoteAssistSession` gains `ConsentStatus` (`pending/granted/declined`) + `ConsentAt`; new endpoint `POST /api/v1/remote-assist/:id/consent` (visitor surface, validates session ownership): granting keeps active, declining ends the assistance; the same response is idempotent, an opposite response returns 409 (`ErrAssistConsentDecided`); after `declined`, recording write-back is rejected (409 `ErrAssistConsentDeclined`). Legacy empty consent values are treated as "did not go through the consent flow" and do not block the existing write-back path (the acceptance script convention is unchanged). Migration `000014_remote_assist_consent.up.sql` (ALTER ADD COLUMN, water level 13→14).
- **RA-8 server half (terminal-state old-PC reclaim)**: `WebRTCService.HandleOffer` first calls `closeTerminalConnections(sessionID)` to reclaim old PCs on the same session that are disconnected/failed/closed (including voice EndCall), then builds the new answer (`platform/realtime/webrtc_service.go`). **Deliberately reclaims terminal states only**: under the current topology where the visitor and the agent each hold one live PC (the RA-5 ambiguity, closed out on 2026-10-15), a full per-session reclaim would damage the peer's connection.
- **RA-6 agent-side ICE consumes the delivery**: before creating a PC, the admin side resolves ICE with "server delivery first" (`apps/admin/src/pages/Conversation/index.tsx` `resolveRemoteAssistIceServers`): WS `webrtc-ice-config` push cache → REST `GET /api/v1/rtc/ice-servers` fallback → public STUN as the last resort, same convention as the visitor SDK. TURN credentials are short-lived; the cache is reused across attempts and expiry is naturally refreshed by the next REST fallback.
- Tests: application layer — `RespondConsent` all branches (ownership/idempotency/conflict/persistence), `StartSession` single-active and scope injection; infra layer — cross-tenant isolation, annotation guard, consent lifecycle; handlers layer — consent endpoint and 409 mapping; realtime layer — re-offer reclaim (live stays live / terminal-state reclaim / voice EndCall, nil voice, removal still happens when close errors, other sessions untouched); package coverage held at 100%.
- Not yet wired (registered): notifying the visitor to pop the consent UI after the agent initiates depends on the RA-10 invitation frame; the WS/RTC-side cleanup of declined sessions can now use RA-5 ownership-directed delivery to deliver/reclaim to the peer individually, left for implementation when the wiring happens.

## RA-8 SDK Half Closed Out (2026-10-14)

- **Resend the unfinished offer after WS reconnect**: in `startRemoteAssist` the SDK records the offer payload and negotiation state (`sdk/packages/core/src/sdk.ts` `lastRemoteAssistOffer`/`remoteAssistState`); on the WS `connected` event, if the PC is alive, the offer has not finished negotiating (`offered`/`connecting` states), and the connection is usable, it resends the **same** offer (`resendRemoteAssistOfferIfNeeded`). Rationale: the RTCPeerConnection survives WS reconnects (media does not depend on WS; only the `webrtc-offer` frame can be lost), and minting a new offer mid-negotiation violates the signaling state machine; the server's `HandleOffer` answers idempotently and reclaims terminal-state old PCs first, so a duplicate offer has no side effects. Once negotiation enters `connected`/`ended`/`failed`, the offer reference is cleared, so nothing is resent after a successful connection.
- Tests: five RA-8 describe cases added to `sdk/packages/core/src/sdk.test.ts` (resend when reconnecting while awaiting the answer / resend when the answer is lost mid-flight / no resend once connected / no resend after `endRemoteAssist` / no resend without an active session); the two `apps/demo-sdk` bundles were rebuilt with the SDK build and committed.

## RA-7 Media Bridging and RA-5 Signaling Ownership Closed Out (2026-10-15)

Landed per route A registered on 2026-09-26 (server-side SFU-lite), with zero client protocol changes (the visitor SDK is untouched; the admin side only adds answer handling for the server's renegotiation offer):

- **Forwarding core (`webrtc_service.go` `trackForwarder`)**: after the server PC's `OnTrack`, a published track is registered (key = publishing connection ID + track ID; duplicate publishes are skipped idempotently), **one read + N writes** — a single goroutine loops reading `TrackRemote` and writes each packet into every subscriber's `TrackLocalStaticRTP` local track (pure pion forwarding, no transcoding). If the subscriber write side errors (PC already closed), it is torn down; if the publisher read side errors (went offline), the forwarding goroutine exits naturally.
- **Delivery paths**: `fanOutTrack` attaches a newly published track to the session's other live PCs (terminal-state connections skipped); `replicateTracksTo` back-fills the session's existing published tracks after a new PC answers an offer (late joiners see them immediately).
- **Server-initiated renegotiation**: after bridging adds a track, `negotiate()` runs CreateOffer/SetLocalDescription on that PC and delivers the offer in a directed fashion as a `webrtc-offer` frame (serialized per connection by `negotiationMu`). It deliberately **does not hook `OnNegotiationNeeded`** — local changes such as data channels would trigger it too, and pushing an offer unrequested to clients that have not implemented server-offer handling would break the existing signaling flow (proven by `TestWebRTC_FullSignalingFlow`).
- **RA-5 signaling ownership**: each server PC records the WS connection of the offer that created it (`ownerClientID`); `HandleAnswer`/`HandleICECandidate` look up by ownership via `connByOwner`; server-delivered candidate/state-change/renegotiation offers are delivered in a directed fashion via `wsHub.SendToClient` (delivered inside the mutex, avoiding the race with unregister closing channels), falling back to session broadcast / first hit when there is no owner (REST surface).
- **Offline cleanup**: `CloseConnection`/terminal-state reclaim calls `dropForwarderStateLocked` while holding the lock — deleting the connection's forwarder entries as a publisher and removing its subscriber write side on other published tracks.
- **Admin-side support (`apps/admin/src/pages/Conversation/index.tsx`)**: `onmessage` handles `webrtc-offer` (answered directly in steady state; in the glare case where an answer is in flight it is queued and applied after `onsignalingstatechange` returns to steady state, and failure only warns without failing the assistance); `ontrack` uses `new MediaStream([event.track])` as a group-stream fallback for bridged tracks without a MediaStream.
- Tests: `webrtc_bridge_test.go` — forwarder register/replace/teardown/loop branches, fanOut/replicate filtering, attach error branches (construction failure / PC already closed / idempotent), negotiate three states (successful directed delivery / broadcast fallback / seam error not sent out), SendToClient three branches, connByOwner ownership hit / miss / fallback; **`TestWebRTC_LoopbackMediaBridge`** walks the full path with real dual pion PCs (visitor offer carries a published track → ICE/DTLS connect → visitor writes RTP → server OnTrack forwards → agent receives `webrtc-offer` and answers the renegotiation → agent ontrack receives the VP8 bridged track), with candidates/answers routed by ownership-directed delivery throughout (the path cannot hold if routing is wrong, doubling as an RA-5 regression). realtime package coverage 100%, race passed three times in a row.

## Relation to Acceptance Evidence

- The 20 checks on the REST record surface have real run evidence: `scripts/test-remote-assist-acceptance.sh` (`make remote-assist-acceptance`), with the manifest committed under `scripts/test-results/remote-assist/`.
- The realtime surface (WS signaling, WebRTC connection setup, media bridging) has unit tests and loopback-path tests in `websocket_hub`/`webrtc_service` (`TestWebRTC_LoopbackMediaBridge`, real dual pion PCs + RTP forwarding + renegotiation); a **browser-level two-ended real-media acceptance script is still missing**, registered as a follow-up (real browser getDisplayMedia + an assertion on the admin-side video frame).
