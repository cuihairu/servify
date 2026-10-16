> **English** | [中文](mobile-sdk-design.md)

# Mobile SDK Planning: Android and iOS Embedded Customer-service Capability

Status: planning document (no implementation). This document answers "what to do, why do it this way, in what order", implementation details are left to the technical design at each phase's kickoff.

Related documents:

- [07 SDK Multi Surface](/implementation/07-sdk-multi-surface): this document is the landing plan for its M3 `app-sdk-reservation`; the four contracts already reserved in app-core (offline queue / push token registration / reconnect + session restore / mobile storage) are this document's foundation, not reinvented.
- [v1 product scope](/v1-product-scope): the product contract for AI first answer, human handoff, ticket full flow.
- Web SDK source: `sdk/packages/core` (protocol source of truth), `apps/demo-sdk/widget.js` (minimal integration form reference).

---

## 1. Product Positioning and Boundaries

### Decision D1: build an embedded SDK, not a standalone customer-service app

**Conclusion**: the deliverables are two client SDKs embeddable into host apps (Android AAR / iOS XCFramework), the host launches customer-service sessions in its own page; no standalone "Servify customer-service" app is published.

**Why**:

- The value anchor of a customer-service session is "the moment the user hits a problem in the business flow". A standalone app means the user leaves the host business context, context (order page, error page) breaks, conversion rate will necessarily be worse than launching in place. Industry precedents (Intercom, Zendesk, Meiqia) are all SDK form; no standalone customer-service app has a surviving sample.
- A standalone app needs the trio of account system, app market review, independent release cadence; each is a continuous cost; the SDK's release cost converges to "integrator upgrades the dependency", consistent with servify's existing "no release operations, integrator fetches on its own" delivery mode.
- The v1 product scope lists "brand base configuration (name, welcome message, theme color, offline hint)" as channel requirements; an embedded SDK naturally has the host pass in brand config, a standalone app would need to build its own config console, which v1 cannot afford.

**Boundaries (v1 explicitly not doing)**:

- No agent-side mobile app (agent workbench stays Web);
- No voice/video (backend WebRTC capability serves remote assistance first, mobile v1 does not expose, `createMobileCapabilitySet` already sets voice/remote_assist to off, stay consistent);
- No forwarding of messages from inside the host app to system SMS/email and other system-level integrations.

### Decision D2: align capabilities with the Web SDK (protocol alignment is a hard constraint, the visitor-reachable surface is the real boundary)

Capabilities the Web currently truly has and mobile v1 must align with (based on `sdk/packages/core` source and server route registration, not launch marketing):

| Capability | Web status | Mobile v1 |
|---|---|---|
| Session window | Floating button + bottom panel (widget self-drawn, bypasses SDK) | Floating button + bottom drawer + full-screen session page |
| Message send/receive | WS `text-message` (customer uplink) / `agent-message` (agent) / `ai-response` + `ai-response-delta` (AI first answer, streaming increments + final frame) | Same protocol, message model aligned field by field |
| WebSocket link | `/api/v1/ws?session_id=` (publicV1 group auth-free; the `access_token` parameter is optional, not consumed by default, once `security.guest_token.required` is on the handshake strictly validates signature/freshness/session binding, validation failure 401 rejects upgrade), server protocol layer ping 54s keepalive (JSON heartbeat removed, see contract review below), exponential backoff reconnect (5 times, starting 1s, doubling, capped at 30s) | Same path; reconnect policy stacked with foreground/background switching and network-reachability awareness; auth see D6 guest token scheme (✅ server landed with knife 8) |
| Unread count & new message notifications | **No implementation at all** (no count, no badge) | In-app unread count is the baseline; system push is an optional module (D7) |
| AI first answer & citations | WS `ai-response` frame first-class carries `{content, confidence, source}` + orchestration additional outputs `sources`/`strategy`/`next_action`/`handoff_reason` (zero values omitted); streaming via `ai-response-delta` increment frames (server connected 2026-09) | Increment concatenation + completion-frame replacement rendering; citation sources expandable list; confidence-gate suggestion (next_action=handoff) surfaces a "transfer to human" hint |
| Transfer-to-human status | `transfer_notification` (with message/agent_id) / `waiting_notification` (queued) two frames; widget does not special-case, goes through the "render as bot text" fallback branch. **Note: `session_update`/`agent_status` frames the core SDK once declared and handled are never sent by the server (dead branch, removed from types and runtime with the core cleanup knife)** | State machine driven by real frames instead: ai_answering → (transfer/waiting_notification) → waiting_human → (agent-message arrives) → agent_chatting (see D5 state machine) |
| Ticket creation entry | REST `POST /api/tickets` (api-client has a wrapper, widget does not expose), **admin surface endpoint, visitor unreachable** | "Escalate to ticket" entry inside the session page, with AI summary pre-filled; depends on visitor-usable endpoints (backend companion item, see §10) |

**REST surface visitor reachability (key fact-check at planning time)**: nearly all existing REST endpoints hang under the admin middleware chain (`AuthMiddleware` + `RequirePrincipalKinds("agent","admin","service")` + resource permissions), including `/api/v1/ai/query`, `/api/tickets`, `/api/omni/sessions/:id/messages` once planned to be depended on by this document. **The only existing channel for the visitor client is `/api/v1/ws`**. This directly constrains the architecture: mobile v1's realtime send/receive, AI first answer (including streaming) all go over WS; the two kinds of REST dependencies "incremental re-pull" and "ticket creation" must first have visitor-usable server companions (§10 list #1/#4, push registration likewise see #5), cannot be assumed usable by directly copying the admin endpoints' parameter shapes.

**Why align field by field instead of "redesigning the message model for mobile"**: plan 07's M3 acceptance wording explicitly says "the subsequent mobile SDK will not directly copy the Web SDK structure", this sentence constrains **code structure** (don't copy JS's EventEmitter/SDK class hierarchy to native), not the **protocol contract**. The cost of cross-endpoint protocol inconsistency is that the server maintains two broadcast branches, and message history portability (switching devices to continue chatting) breaks. Therefore: protocol and message model alignment is a hard constraint, runtime structure is restructured per platform convention. Dead branches in the alignment baseline (`session_update`/`agent_status`) do not enter the mobile contract; being compatible with server behavior that doesn't exist only solidifies phantom frames into a three-endpoint burden; this legacy has been closed with the core cleanup knife (type union and runtime case removed wholesale, four-framework package consumer surface removed in sync, PROTOCOL §5 retains the boundary record).

---

## 2. Android Technology Selection

### Decision D3: Kotlin + Jetpack Compose, minSdk 24

**Language & UI**: Kotlin (officially recommended language, coroutines naturally fit WS long connections and async message streams) + Jetpack Compose (declarative UI fits customer-service's "state-driven rendering" message stream: `messages: List<Message>` one state source drives the whole session page, no imperative diff needed).

**Why not the XML View system**: the core interaction of a customer-service session page is "infinite-scrolling message list + bottom input bar + keyboard coordination"; under the View system the combination of these three (RecyclerView + WindowInsets + input-method height animation) is a notoriously fragmented disaster area; Compose's `LazyColumn` + `imePadding()` converges these three into declarative layout, and is independent of whether the host app uses Compose (SDK internally self-consistent, host interop via `ComposeView` is enough).

**minSdk justification (why 24 and not 21 or 26)**:

- **21 (Android 5.0) rejected**: Compose's minimum support is 21, but 21-23 device share has fallen to statistical noise level (Android Studio's new project template defaults to minSdk 24); more critically TLS and network security: below API 20 TLS 1.2 is not enabled by default, 21-23 has known defects in some vendor ROMs' TLS/certificate chain implementations; the customer-service link carries visitor tokens, not worth carrying security debugging costs for permille-level devices.
- **26 (Android 8.0) rejected**: although 26+ has conveniences like notification channels (NotificationChannel), it would cut about 5-8% of live devices (stock low-end phones are still common in emerging markets), while the notification-channel compatibility pattern is just a thin wrapper of `if (Build.VERSION.SDK_INT >= 26)`, not enough to exchange for coverage loss.
- **24 (Android 7.0) wins**: covers about 97%+ of live devices; `Java 8` APIs fully available (okHttp 4.x requires 21+, kotlinx.serialization has no version gate); the Jetpack full stack (Lifecycle, Security, DataStore) fully covers the 21-24 range. This is the optimal intersection of coverage and maintenance cost, and consistent with industry SDK conventions (Intercom is at minSdk 24 level).

**Why not cross-platform frameworks as the main path**:

- **Flutter**: introducing the Flutter engine base (about 4-6MB starting) just for one customer-service session page, package size penalty unacceptable (see D9 budget); and Flutter's mixed stack with host native pages (gestures, keyboard, deep links) is a long-term friction source.
- **Kotlin Multiplatform**: technically attractive, but KMP's UI still needs Compose Multiplatform (iOS side still in evolution period) or each platform writes its own UI; the "sharing" benefit is mainly at the protocol layer, and the protocol layer can get 80% of the benefit with "shared contract + per-platform codegen/mutual test verification" (D5), without bearing KMP toolchain contagion to integrators' build environments (Gradle plugin versions, Xcode configuration).
- **React Native (existing `@servify/react-native` binding)**: positioned as an optional fast path for RN hosts (headless binding + app-core contracts already ready), but RN hosts are a minority among integrators, not the promoted form. The native SDK planned by this document is the default path, the RN binding continues independent evolution.

**Dependency whitelist (Android)**: kotlin-stdlib, kotlinx-coroutines, kotlinx-serialization-json, okHttp (`api` dependency, host almost certainly already has it, gradle will dedupe), Compose Runtime/UI/Foundation (`compileOnly` semantics aligned to host-version scheme to be finalized in M1). Forbidden: any DI framework (Hilt/Koin, SDK scale doesn't need it, manual injection is enough), any image loading library (v1 message types only have text, attachments are P2).

---

## 3. iOS Technology Selection

### Decision D4: Swift + SwiftUI, minimum iOS 15

**Why SwiftUI rather than UIKit**: same reasoning as the Android side, message streams are state-driven rendering, SwiftUI's `List`/`ScrollViewReader` + `ObservableObject` happens to map one-to-one; UIKit's `UICollectionView` combination of keyboard coordination and auto-height cells has a large amount of glue code that is hard to test. Risk control: the SDK's UI surface is narrow (one session page + floating button), SwiftUI is stable enough in this complexity range.

**Minimum OS version justification (why 15 and not 16 or 17)**:

- **iOS 17 rejected**: cuts too many stock devices (two years after iOS 16's release the 15 series still has considerable share; customer-service SDK integrators, e-commerce, local services, are exactly categories with high old-device share).
- **iOS 16 rejected, the reason and the retained temptation**: 16 brings `NavigationStack` and `presentationDetents` (half-screen drawer), very tempting for session pages; but both have 15-era alternatives (`NavigationView` + programmatic navigation; custom `sheet` height), what's given up is only API elegance. Customer service is "low-frequency but critical", integrators are extremely sensitive to coverage, giving up 2-3% of devices for navigation API elegance, ROI doesn't hold.
- **iOS 15 wins**: coverage about 98%; since iOS 14 SwiftUI's `List`/`sheet`/state management were available, 15 completed stabilization improvements like `AsyncImage`, `task` modifier, `refreshable`; `URLSessionWebSocketTask` (iOS 13+) satisfies WS needs. If 16-exclusive APIs (like rich media preview) are needed in future, raising the min version is a one-line deployment target modification, cost reversible.

**Why no third-party networking/WS library (Starscream etc.)**: `URLSessionWebSocketTask` natively covers connect, send/receive, ping (`sendPing`) capabilities, and automatically gets correct TLS and proxy configuration behavior with the system; third-party libraries introduce 1-2 dependency transitive and App Store privacy manifest burdens, with no corresponding benefit.

**Distribution form**: XCFramework + Swift Package Manager binary distribution as the primary (integrators zero CocoaPods configuration), CocoaPods spec maintained as a compatibility layer until M3 evaluates retention (✅ evaluated, 2026-09-23: SPM-only single track maintained, no known CocoaPods-only integrators so no spec built, on-demand rebuild trigger conditions see `docs/mobile-sdk-cocoapods-evaluation.md`). Forbidden dependencies (iOS): any third-party networking/serialization/DI library, serialization uses `Codable`.

---

## 4. Shared Design: Cross-platform Consistency of Protocol and Message Model

### Decision D5: single protocol source of truth (backend WS contract) + native implementations per platform + contract mutual-verification tests

**Structure (no shared code, shared contract)**:

1. **Protocol source of truth**: the backend `apps/server`'s WS contract (`/api/v1/ws`, the visitor's only existing channel) + visitor-reachable REST contract (currently zero, `/api/omni/*`, `/api/v1/ai/query`, `/api/tickets` are all admin endpoints, visitor companion endpoints see §10 list) is the only source of truth.
2. **Contract documentation**: `sdk/PROTOCOL.md` first version already in repo (2026-09), listing per-frame message types, payload fields, directions and boundary semantics frame by frame, all entries verified against server source with file-line-number anchors. Key fact-check outputs solidified into the contract: the six dead-branch frames declared by core types (`session_update`/`agent_status`/`typing`/`message`/`error`/`system`) are separately tabled declaring "mobile contract does not include" (**subsequent core cleanup knife removed them wholesale from the type union and runtime, §5 table retained as boundary record**); heartbeat dual-direction mechanism clarified (server protocol layer Ping 54s, Web core's JSON `system/ping` frame is not handled by the server, mobile uses platform-native protocol layer keepalive; **this JSON heartbeat was also subsequently removed from core**); boundary semantics written into the contract like slow-client 256-frame buffer kick, send-success criterion = receiving own echo. Naming mixing (`transfer_notification`/`waiting_notification` using snake_case, `text-message`/`ai-response`/`ai-response-delta`/`agent-message` using kebab-case) **frozen as-is in the contract, not renamed on this occasion**, renaming is a server breaking change, v1 doesn't do it.
3. **Mutual-verification tests**: Android/iOS each build a set of "contract replay tests", using the same set of JSON samples (placed in `sdk/protocol-fixtures/`, shared with core tests) to drive deserialization and state machine assertions. When the server WS contract changes, changing fixtures makes all three endpoints' tests red simultaneously, more reliable than "humans remembering to change all three places".

**Message model (cross-endpoint canonical form)**, aligned field by field with core `Message` (`types.ts:77-88`):

```
ConversationMessage {
  id: String                 // 服务端消息 id；WS 事件缺省时用 "ws-" + 本地序号回退（对齐 core normalizeMessage 行为）
  session_id: String
  sender_type: Enum { customer, agent, system }
  content: String            // 取值顺序 data.content ?? data.message ?? 原始字符串（对齐 normalizeMessage）
  message_type: Enum { text, image, file, system }   // V1 仅产生/消费 text 与 system
  is_ai_response: Bool       // ai-response 事件置 true
  metadata: Map<String, Any> // WS 事件的原始 data 整体入此（sources/confidence/strategy 由此透出）
  created_at: Instant
}
```

**Session state machine (the core cross-platform contract of the transfer-to-human link)**, transition events all use frames the server actually sends (D2 fact-check conclusion):

```
ai_answering ──(transfer_notification：已分配坐席)──> agent_chatting
ai_answering ──(waiting_notification：入等待队列)──> waiting_human
waiting_human ──(transfer_notification：队列派发)──> agent_chatting
agent_chatting ──(system 帧或增量补拉发现会话 closed)──> closed
任意状态 ──(WS 断连)──> reconnecting ──(恢复)──> 原状态 + 增量补拉（D7）
```

**Why the state machine is explicitly a contract**: the Web's transfer-to-human state is scattered in the widget's if/else (no state machine), causing "AI racing to answer after transfer to human" to need the server's `HasActiveHumanAgent` fallback. After mobile makes the state machine a first-class contract, the same state transition table can be replayed in Android/iOS mutual-verification tests, and server fallback logic degrades to a fuse rather than a correctness source.

**The role of SSE**: the server has connected WS streaming first answer, the AI orchestration layer `HandleStream` and non-streaming share the same pipeline, the WS side pushes increments on the `text-message` link as `ai-response-delta` increment frames (`{content_delta, done}`, `message_id` reserved), on completion appends a terminal delta (`done=true`) then sends a full `ai-response` final frame (increment concatenation + completion-frame replacement), frame contract consistent with this document's reserved slot; when streaming capability is absent or stream start fails immediately the server auto-falls back to single `ai-response`, mobile v1 can render directly per this contract. This way the protocol goes first and implementation follows, the three endpoints won't each invent frame formats when the server connects streaming.

---

## 5. Core API and Initialization Flow

### Decision D6: API form aligned with the Web SDK's mental model (config → connect → chat → events), auth defaults to guest token

The two platforms' API surfaces deliberately keep same names and same meanings, reducing cross-endpoint documentation and integration mental cost; differences are only language idioms (Kotlin coroutine suspend functions / Swift async-await).

**Android (Kotlin, pseudocode)**:

```kotlin
// 1. 最简接入（访客模式）
val servify = ServifyChat.create(
    context = context,
    config = ServifyConfig(
        apiUrl = "https://support.example.com",
        guestToken = hostProvidedGuestToken,      // 见 D6 鉴权；为空则走匿名会话（兼容现状）
        branding = Branding(title = "帮助中心", primaryColor = 0xFF667EEA.toInt()),
    ),
)

// 2. 事件订阅（StateFlow 暴露，UI 层 collect）
scope.launch {
    servify.events.unreadCount.collect { badgeView.count = it }
}
servify.events.messages.onEach { render(it) }.launchIn(scope)

// 3. 拉起会话（底部抽屉；activity 为宿主任意前台 Activity）
servify.show(activity)

// 4. 发送 / 工单
servify.sendMessage("我的订单 #123 退款到账了吗")
servify.createTicket(title = "退款咨询", description = "…")
```

**iOS (Swift, pseudocode)**:

```swift
// 1. 最简接入
let servify = ServifyChat.create(config: ServifyConfig(
    apiUrl: "https://support.example.com",
    guestToken: hostProvidedGuestToken,
    branding: Branding(title: "帮助中心", primaryColor: 0x667EEA)
))

// 2. 事件（AsyncStream）
for await unread in servify.events.unreadCount { badge.text = "\(unread)" }
for await message in servify.events.messages { append(message) }

// 3. 拉起
servify.show(from: presentingViewController)

// 4. 发送 / 工单
try await servify.sendMessage("我的订单 #123 退款到账了吗")
try await servify.createTicket(title: "退款咨询", description: "…")
```

**Initialization sequence (same on both platforms)**:

1. `create`: load persisted snapshot (app-core `SessionSnapshot` aligned shape: sessionId / lastMessageId / savedAt);
2. Lazy connect: WS built only on first `show` (aligned with widget's lazy strategy, most users don't click customer service, don't build connections for them);
3. Connection success → use `lastMessageId` for incremental re-pull, merge local message table → unread count accumulates by "agent/system source and not locally rendered yet". **The re-pull channel v1 is designed as "visitor-usable session message endpoint" (§10 list #1, ✅ landed with M3 knife 7: `GET /api/v1/sessions/:session_id/messages`)**, the existing `/api/omni/sessions/:id/messages` is an admin endpoint, visitor-unreachable (D2 fact check), the M0 joint-debug probe first uses WS replay (the server has no history resend mechanism after reconnect, the probe phase records "silent message loss during disconnect" as a known boundary, doesn't pretend to have re-pull);
4. Foreground/background switching: no keepalive in background (iOS background long connections are inherently unreliable), on return to foreground first probe then reconnect + incremental re-pull.

**Why the API doesn't provide a "custom message-page ViewController/Fragment"-level UI injection point**: v1's customization surface converges to brand config (title, primary color, welcome message), this is the industry SDK's mature boundary. Exposing whole-page replacement hooks amounts to promising UI contract stability, which would significantly slow subsequent iteration; integrators with real deep-customization needs should build their own UI at the protocol layer (PROTOCOL.md supports this usage).

### Auth and Security (Decision D6 continued)

**Current gap (must face it, verified against source)**: the WS route hangs under the publicV1 group (no AuthMiddleware), the handshake only requires `session_id` non-empty to upgrade the connection; the `access_token` parameter in the URL is **not read** by the server, any client knowing session_id can listen to that session stream, the Origin allowlist (configurable to allow all sources) is the only barrier. The Web side relies on "session_id doesn't leave the browser" for lucky security, mobile can't copy this assumption (mobile often has multi-device/device-switch/log-desensitization scenarios, id leakage surface larger).

**v1 scheme: guest token**:

1. Backend adds a lightweight endpoint (suggested `POST /api/v1/guest/session`): the host backend uses server-to-server credentials (reusing api-client's auth contract) to exchange for a **short-lived signed guest token** (HMAC/JWT, payload carries session_id binding + expiry), delivered to the client;
2. The SDK carries it via the `access_token` parameter when connecting WS (same form as Web's `AuthProvider`), server validates signature and session binding;
3. Compatibility path: when `guestToken` is empty, anonymous session_id mode is allowed (aligned with Web status quo), but marked in PROTOCOL.md as "transitional form, the server can configure it off in future".

**Why v1 doesn't do full OAuth/device binding**: customer-service session data sensitivity is "conversation content", the risk exposure is a single session, a short-lived signed token already converges the leakage impact into a single-session time window; a full identity system is the host's responsibility (the host can compile its own user identity into the token payload via the guest exchange endpoint), the SDK doesn't overstep.

**Landing status (2026-09-24, M3 knife 8)**: the server has landed per the above scheme, `POST /api/v1/guest/session` hangs under the service auth chain (host backend calls with service API key), issuing a short-lived HS256 token bound to session_id (payload `{typ:"guest", sid, iat, exp}`, self-signed self-verified reusing `jwt.secret`'s trust domain), **does not require the conversation row to already exist** (the row is created when the first message is persisted); the SDK handshake carries it via `access_token`, the hub validates three layers: signature + freshness + session binding (a token valid but bound to another session is also 401). Config `security.guest_token.required` (default false preserving existing deployment behavior) + `security.guest_token.ttl` (default 24h); the combination of required + default jwt.secret warns at the config gate, production/staging rejects startup, wiring layer blank secret fallback rejects startup (double-layer gate). The SDK-side `guestToken` parameter form is already in place (2214bb5, the server didn't consume it before, backward compatible).

**Other security baselines**: tokens live only in memory + system secure storage (Android EncryptedSharedPreferences / iOS Keychain), never in plain files; WS and REST enforce HTTPS/WSS; logs redact by default (don't print message bodies and tokens, only output with an explicit debug switch).

---

## 6. UI Form

### Decision D8: three forms = floating button + bottom drawer (default) / full-screen session page (optional + auto on small screens)

- **Floating button**: 56dp/56pt circular floating, position configurable (default bottom-right), the host can fully self-draw the entry and only call `servify.show()`, the floating button is just the default entry, not a hard-bound component.
- **Bottom drawer (default session form)**: Android uses ModalBottomSheet (max height ≈ 85% of screen height), iOS uses sheet (a 15-level alternative implementation of `presentationDetents`). **Why drawer by default**: customer-service sessions are "interludes in the business", half-screen preserves host context, the user can look at their order while asking, this is the core experience of an embedded SDK versus a standalone app (echoing D1).
- **Full-screen session page**: used when the host configures `presentationStyle = .fullscreen`; on small-screen devices (height < 600dp) the drawer auto-upgrades to full-screen, avoiding the keyboard squeezing out an unusable layout in half-screen.
- **Brand injection**: primary color, title, welcome message enter the theme layer via `Branding` config; no CSS-level/layout-level customization (reasons see section 5).
- **Message rendering**: AI messages and agent messages visually distinguished (AI carries a badge and a "transfer to human" button); AI first answer streaming rendering, `ai-response-delta` increments concatenate on arrival, after receiving the terminal increment with `done=true`, the whole is replaced by the `ai-response` final frame (the increment-concatenated result is consistent with the final frame content, replacement is an idempotent closure); stream interruption semantics (terminal increment arrived but no ai-response final frame) = keep rendered part + append a "answer interrupted, please retry" hint line, no silent clearing and no auto-resend; citation sources (`sources`) rendered as an expandable source list (document title + score-sorted), clicking opens the link in an in-app browser, this is the mobile carrier of the v1 product scope "answers should carry traceable sources"; the confidence-gate suggestion (`next_action=handoff`) renders as the emphasized state of the "transfer to human" button.
- **Unread presentation**: drawer collapsed shows the floating button badge count; when the host self-draws the entry, present it via `events.unreadCount` itself.

---

## 7. Push and Offline Messages

### Decision D7: push is an optional module (FCM/APNs), offline messages = push wake + `lastMessageId` incremental re-pull, no message persistence sync protocol introduced

**Why push must be an optional module rather than a built-in dependency**: when the user switches away from the app mid-session and an agent reply arrives, without push the user never sees it, this is a **real gap** of mobile versus Web (Web didn't do it because browser push permission rates are low and widget scenarios are mostly on the current page). But push depends on the host integrating FCM/APNs (needs the host's push credentials and backend config), SDK hard-binding push drags integrators with no push demand into integration cost. Therefore: the SDK exposes a `pushTokenProvider` registration entry (aligned with app-core's push token registration contract), the host provides a token to enable it, otherwise in-app notifications only.

**Flow**:

1. The host registers an FCM/APNs token with the SDK → the SDK reports it to the backend via the push registration contract (binding session/customer);
2. When the session is not foreground, agent/system messages → the server sends silent + alert push via FCM/APNs;
3. The user taps the push or the app returns to foreground → the SDK reconnects + `lastMessageId` cursor incremental re-pull → merge render + unread count.

**Landing status (2026-09-24, M3 knife 10)**: flow 3's client side is connected, both SDK endpoints automatically call the incremental pull endpoint to reconcile the disconnect window after reconnect success (onOpen): `after_id` cursor + `has_more` continuation (single page 100, up to 10 pages), agent/ai sources merge into history and count as unread when the panel is invisible (same convention as WS frames, customer echo and system hints don't count, customer first-connect full replay goes into history); total-failure surface silent (404 = conversation row never created, normal state, IO/HTTP errors don't affect WS). Because WS frames don't carry server message IDs, the cursor only advances from re-pull results, deduplicating the "already rendered by WS before cursor established" window with a 「sender|content」 fingerprint table (capacity 200 ring); the server persists before broadcasting, guaranteeing WS rendering is necessarily within the re-pull surface, fingerprint hits only skip rendering and the cursor still advances; the fingerprint table isn't cleared at the end (frames arriving during reconciliation would be erased by clearing, creating duplicates, reproduced in Kotlin testing then removed), reclaimed by capacity eviction. AI answers aren't persisted and system hints don't go over WS, naturally no duplicate surface.

**Landing status (2026-09-24, M3 knife 9)**: flow 3's unread alignment with the server enhancement surface is ready, `POST /api/v1/sessions/:session_id/read` + `GET /api/v1/sessions/:session_id/unread` (§10 list #3, auth-free, same prefix and security model as #1): response `{unread_count, last_read_message_id}`, unread convention = sender ∈ {agent, system} and message ID greater than the server cursor (aligned word-for-word with the client-side derivation above, customer uplink and AI first answer don't count); the cursor only moves forward (opening a push/returning to foreground submits the local read position once, repeated/rollback submissions are idempotent), SDK v1 still uses client-side derivation as the baseline, the server cursor is an enhancement for hosts needing cross-device/cross-entry consistent unread.

**Landing status (2026-09-30, submission half)**: both SDK endpoints' `syncReadState()` is in place (same-shape enhancement entry as registerPushToken, v1 frozen surface "add-only no-modification"), submitting the latest server message ID held by the re-pull chain as the server read cursor (`POST /read`); no confirmed message silently false, IO/HTTP failure `network` + false, response body not consumed (local derivation semantics not overwritten by the echo). **The read half (GET /unread merged into unread count) is still registered as a gap**, it would switch the unread baseline from client derivation to server cursor (changing semantics, not add-only), per the v1 frozen-surface discipline it waits until the host actually raises a cross-device consistent unread need.

**Why not "offline queue reverse sync" (client messages can also be sent offline)**: the sender of a customer-service conversation is the user, and the user is necessarily online when on the customer-service page; typical offline-sending scenarios (weak-network flapping) are covered by "send timeout + local failure marker + manual resend", consistent with the Web's behavior. A full offline-first sync protocol (cursor, conflict resolution, idempotent dedup) is for the "server→client data surface", i.e. the incremental re-pull above; client→server direction v1 doesn't do queue replay, avoiding customer-service scene accidents of "user thought they sent it, actually it went out three days later". app-core's offline queue contract is retained as a P2 evolution slot.

---

## 8. Package Size and Dependency Constraints

### Decision D9: Android AAR increment ≤ 1.5MB, iOS XCFramework ≤ 2MB, closed dependency whitelist

**Why size is a hard constraint**: the customer-service SDK is the most cost-performance-sensitive dependency in the host app, it produces no DAU, yet integrators pay download conversion cost for it. Industry baseline: Intercom Android SDK increment about 2-4MB (including image library), Servify v1's capability surface is narrower (no attachments, no rich media, no offline queue implementation), should be significantly smaller than this number, and this is the qualification line for marketing "lightweight" to integrators.

**Budget breakdown (Android, bytes inside rar/jar)**:

| Item | Budget |
|---|---|
| SDK's own classes + resources | ≤ 600KB |
| Compose UI (only the runtime/ui/foundation subset used by the session page, R8 post increment) | ≤ 600KB |
| kotlinx-serialization increment | ≤ 150KB |
| Coroutines increment | ≤ 150KB |
| okHttp | 0 (`api` dependency, deduped with host; counted separately not in SDK increment) |

**R8 rules**: the SDK ships consumer proguard rules (preserving message models and reflection points); kotlinx-serialization's rules come with the library.

**iOS budget**: XCFramework ≤ 2MB (SwiftUI/Combine are system libraries with zero increment; main size is own code + protocol models). App Store privacy manifest: the SDK collects no data (conversations go through the host's network stack to the server), declares "no tracking".

**Dependency freeze mechanism**: the Android dependency whitelist is written into a build script assertion (`dependencies` locked list, CI validates any new dependency is red); iOS zero third-party dependencies structurally guaranteed by "Package.swift has no dependencies declaration". Any whitelist change must go through a planning review, this is the spirit of 07's M4 surface governance extended to the dependency dimension.

---

## 9. Phased Milestones and Acceptance Criteria

### Decision D10: four phases, protocol first, Android before iOS

**Why Android first**: CI and build environment bootstrap cost is low (the repo already has a JVM toolchain), first get protocol joint-debugging running on Android, iOS copies the contract replay test sample set, reducing protocol oscillation cost when both endpoints develop in parallel.

**M0 — Protocol contract and joint-debug probe (no UI)**

- Deliverables: `sdk/PROTOCOL.md` first version (✅ in repo); `sdk/protocol-fixtures/` sample set; Android probe CLI/unit tests: WS connection, text-message send/receive, ai-response + ai-response-delta parsing (including increment concatenation and stream-interruption samples), transfer/waiting_notification state machine transitions all pass via fixtures replay.
- Acceptance: ① fixtures fed through the same sample set on core and Android with consistent assertions; ② one full-link joint-debug with a real backend environment (connect → AI first answer streaming → transfer to human → agent reply); ③ the contract document covers all the server's current broadcast message types (including the "mobile v1 does not consume" explicit annotation for webrtc types, and the "server doesn't send, client contract doesn't include" explicit annotation for the core SDK's dead branches `session_update`/`agent_status`, this annotation solidified with the core cleanup knife into PROTOCOL §5 boundary record, the type surface has converged in sync).
- Status (2026-09-22): **completed**. ③ achieved via PROTOCOL.md (3bbee3e2); ① sample set 10 cases + core 9 tests / Android 10 tests replay the same sample set with consistent assertions (39f2a67, 2b96be6, including CI `android-probe` job); ② `make mobile-probe-acceptance` runs the full link against a real service and archives a manifest (ff8c49b, embeds an OpenAI-compatible streaming mock LLM, `scripts/test-results/mobile-probe/manifest.json` overall=passed).

**M1 — Android SDK Alpha**

- Deliverables: three-form UI, message send/receive, AI first answer (streaming rendering + citation display + confidence-gate hint), transfer-to-human state machine, unread count, lazy connection and reconnect, guest token integration (including the backend guest token endpoint, **backend companion item, needs to enter backend scheduling**).
- Acceptance: ① contract replay tests all green; ② demo host app integration ≤ 10 lines of code for init + launch; ③ AAR increment ≤ 1.5MB (CI size gate); ④ weak-network/disconnect/background-switch manual test matrix passes (including stream-interruption rendering degradation); ⑤ unit tests cover the protocol layer and state machine (no 100% coverage target, but the state machine transition table must be exhaustive).
- Status (2026-09-22): **completed (Android Alpha; real-device manual items pending execution, anchored item by item in `sdk/android/ACCEPTANCE-M1.md`)**. ① the same sample set core/Android dual-endpoint replay assertions consistent + SDK's full 65 tests all green (39f2a67, 2b96be6, 42ba7f6); ② demo integration 3 lines (02bb8c1); ③ size gate takes the R8 measure (un-obfuscated Compose full-stack increment 5MB+ not the acceptance measure, D9 review record), delta ≈1.19MB ≤ 1.5MB, CI independent step (02bb8c1); ④ automated items (disconnect reconnect/send timeout/stream-interruption rendering degradation/reconnect-exhaustion recovery/handshake failure classification) CI anchored all green, real-device manual items (foreground-background/airplane mode/cellular WiFi switching/transfer state across disconnect) ⏳ pending execution, silent message loss during disconnect is a v1 known boundary (the server has no history resend, doesn't pretend to have re-pull); ⑤ transfer-to-human state machine 3 legal transitions + illegal rejection, connection state machine §4.4 transition table (including idle→connecting→connected, idempotent connect, reconnect-exhausted→disconnected→connect recovery) exhaustive (StateMachineTest 4 tests, ConnectionLifecycleTest 5 tests, 42ba7f6). Guest token: SDK handshake parameter already in place (2214bb5, PROTOCOL §1 server currently doesn't consume it, backward compatible), the guest token endpoint is §10 #2 backend companion item, enters backend scheduling.

**M2 — iOS SDK Alpha**

- Deliverables: an iOS version with the same capability surface as M1 + Swift Package distribution.
- Acceptance: ① the same fixtures replay all green; ② XCFramework ≤ 2MB; ③ demo host integration ≤ 10 lines of code; ④ Keychain storage, background switching, push registration entry joint-debugging pass.
- Status (2026-09-23): **completed (iOS Alpha; ③ demo project and ④ Keychain/push registration entry marked truthfully by layer, anchored item by item in `sdk/ios/ACCEPTANCE-M2.md`)**. ① protocol-fixtures same sample set core/Android/Swift three-endpoint replay assertions consistent (`FixtureReplayTests` 11 tests), SDK's full 61 tests all green on Linux + macOS `ios-macos` job runs the Darwin-side 63 (since 0f99bee, dda9e16/a646e2a/51d1bf0); ② size gate takes the distribution form zip'd bytes ≤2MB (`scripts/check-ios-sdk-size.sh`, Release + BUILD_LIBRARY_FOR_DISTRIBUTION, SwiftPM package packaging must go through archive, c83199b review record), CI independent step; ③ integration surface proven (`ServifyView(config:)` one-line launch), real demo project needs Xcode project files, pending macOS environment (M4 integration documentation site phase); ④ background switching visibility semantics already automated-anchored (unreadCountsOnlyWhileSessionHidden + historySnapshot replay), stream-interruption rendering degradation anchored same as Kotlin, Keychain storage/push registration entry are M3 surface and depend on backend companion items (§10 #2/#3), v1 anonymous session mode has no credential storage need, truthfully marked unimplemented; real-device manual items (foreground-background/airplane mode/cellular WiFi) same as M1 matrix pending execution. State machine exhaustive same as M1⑤ (transfer 4 + connection §4.4 transition table 6 + backoff policy 4); coverage takes the line-level coverable surface 100% (Darwin branch covered by the macOS job, a646e2a).

**M3 — Push + ticket + brand-finalization closure (both endpoints)**

- Deliverables: FCM/APNs optional push module, in-session-page ticket creation entry (AI summary pre-filled), Branding full configuration, CocoaPods compatibility layer evaluation.
- Acceptance: ① push end-to-end (background → system → tap → incremental re-pull → unread cleared) passes on both endpoints; ② ticket creation carries AI summary and is visible agent-side; ③ size gate continuously green.
- Status (2026-09-23): **code surface complete (both endpoints; acceptance ① registration link connected (knife 5), push end-to-end still pending delivery/credential companions, truthfully marked, doesn't pretend acceptable; anchored item by item in `sdk/ACCEPTANCE-M3.md`)**. Knife sequence: knife 1 Branding offlineText (4fd2ead) → knife 2 server visitor ticket endpoint (48813a6, §10 #4; migration watermark assertion increments with 000011 337f613/9a36216) → knife 3a createTicket facade + TicketSummary summary (7b6ced4) → knife 3b dual-endpoint UI entry (11ae3df, iOS 15 compatibility + test timing fix 8d928de) → knife 4 pushTokenProvider registration entry + CocoaPods evaluation (5a81558) → knife 5 push registration link (server §10 #5 registration endpoint + SDK dual-endpoint real reporting) → knife 6 push delivery orchestration + FCM/APNs HTTP transport (code surface fully landed: dispatcher subscription+dedup+WS-online suppression, hand-rolled JWT dual transport zero new dependencies, config push section double-layer gate; real-credential end-to-end joint-debugging P1-1 truthfully left as a gap) → knife 7 server visitor message incremental pull endpoint (§10 #1: `GET /api/v1/sessions/:session_id/messages`, message ID monotonic cursor + has_more probing, session-invalid 404 vs empty-increment 200 semantics distinguished; SDK dual-endpoint integration with D7 disconnect re-pull) → knife 8 server visitor token issuance + WS handshake validation wiring (§10 #2: issuance endpoint service chain + hub three-layer validation + config double-layer gate, D6 scheme landed) → knife 9 server visitor unread count/read cursor endpoints (§10 #3: `POST .../read` + `GET .../unread`, sessions.visitor_read_message_id cursor only forward, unread convention aligned word-for-word with D7 client, migration 000013) → knife 10 SDK dual-endpoint disconnect re-pull integration (D7 flow 3 client surface connected: onOpen auto-reconcile + after_id cursor continuation + fingerprint dedup "already rendered by WS before cursor established" window + agent/ai silent-period unread count; §10 #1 endpoint consumer in place). ① registration link connected: `pushTokenProvider` unconfigured→unsupported / provider returns nil→silent (host not authorized is a normal state) / has token→real report `POST /api/v1/push/register` (2xx→true; IO/HTTP failure→network+false reportable; knife 5 dual-endpoint 6 test cases mirrored; server migration 000012_push_tokens, idempotent key session_id+platform, token not echoed, three-package tests orchestration 5 + delivery 7 + handlers 5); the end-to-end link (background→system→tap→incremental re-pull→unread cleared)'s server delivery orchestration and FCM/APNs HTTP transport have landed with knife 6's code surface (subscribes conversation.message_received, sender=agent filter, EventID ring dedup, visitor WS online suppression, push only when offline; config push section + double-layer gate), still missing real-credential end-to-end joint-debugging (FCM service account / APNs .p8, P1-1), §10 #1 cursor endpoint landed with knife 7 (SDK dual-endpoint integration already connected with knife 10); offlineText disconnect hint anchored on both endpoints (handshake failure/reconnect exhaustion/destroy no hint/no unread count, ConnectionLifecycleTest ↔ ConnectionLifecycleTests test names correspond one-to-one). ② dual-endpoint through: facade createTicket auto-assembles ai_summary (TicketSummary latest 10 entries, [Visitor]/[AI]/[Agent] labels, System hint lines skipped, empty means no key) + UI entry (title required/description optional/submit-disabled/failure-retry hint/success appendSystemHint into history without unread count) + server three-package tests (orchestration 5 + delivery 3 + handlers 5); agent-side visibility goes through the admin's existing tickets list/detail (same table same response chain, ai_summary into ticketResponse), no new dev surface. ③ size gate continuously green: Android R8 measure + iOS distribution zip measure CI independent step re-checked on every push, no drift during M3. CocoaPods compatibility layer evaluation: `docs/mobile-sdk-cocoapods-evaluation.md`, zero known CocoaPods-only integrators, SPM-only single track maintained, on-demand rebuild trigger conditions recorded, review checkpoint M4 documentation site finalization. Real-device manual items (push/foreground-background/weak network) pending execution together with acceptance ①'s external companions.

**M4 — Stabilization and integration readiness**

- Deliverables: integration documentation site (VitePress mobile chapter), disconnect/weak-network automated test matrix, crash and performance baselines (session page first frame < 300ms, memory increment < 30MB).
- Acceptance: ① dual-endpoint demo continuous 72h stability (reconnect loops, foreground-background round trips) no leaks; ② documentation site build green and mounted to the publish slot; ③ external deliverables (AAR/XCFramework artifact flow) ready, **the release action itself is still executed manually by the maintainer, the SDK repo does not contain any automatic release steps**.
- Status (2026-09-23): **deliverable surface complete (② ③ ✅; ① and performance baselines are environment-dependent items truthfully marked pending execution; anchored item by item and 72h execution plan see `sdk/ACCEPTANCE-M4.md`)**. ② integration guide `docs/mobile-sdk-integration.md` (both endpoints, API signatures verified against source) + nav/sidebar mounting + local vitepress build green + docs-pages.yml existing automatic publish (6ba98c7); ③ CI artifact archiving servify-sdk-aar / servify-kit-xcframework (`if-no-files-found: error`, retention 30 days, f4a129d) + fetch path documentation + SwiftPM remote reference boundary truthfully recorded (`sdk/ios/Package.swift` not at repo root, v1 is manual XCFramework embedding, remote distribution pending maintainer manually creating a separate distribution repo); weak-network matrix confirmed via gap analysis to be exhausted by the M1-M3 anchored surface (high-latency echo/disconnect reconnect/exhaustion/handshake failure/stream interruption/backoff sequence, mirrored test cases correspond one-to-one), delay injection doesn't touch new paths and doesn't add time-sensitive test cases; ① 72h stability and performance baselines (first frame < 300ms, memory < 30MB) blocked by GitHub Actions 6h job limit + no real device/emulator locally + demo project pending macOS (M2 acceptance ③ same-source constraint), execution plan (stress script/criteria/evidence-archiving convention) frozen in the acceptance matrix, execute per plan once the environment is in place.

**Explicit exclusions (recording the decisions not to do, preventing scope rebound)**: agent-side mobile, voice/video, rich media messages (image/file), offline send queue, message recall/edit, built-in multilingual (v1 Chinese, Branding reserves i18n hooks).

---

## 10. Backend Companion Requirements List (SDK dependencies, need to enter server scheduling)

> Common background (D2 fact check): existing REST endpoints almost all hang under the admin middleware chain (auth + agent/admin/service principal + resource permissions), the visitor's only existing channel is `/api/v1/ws`. The following #1/#2/#5 are essentially three faces of the same thing, **opening the minimal reachable surface for visitors**, server scheduling is recommended to converge them in one design review as "visitor companion endpoints".

| # | Requirement | Serves SDK phase | Status |
|---|---|---|---|
| 1 | Visitor-usable session message incremental pull endpoint (`lastMessageId` cursor semantics) | M1 | ✅ landed (M3 knife 7): `GET /api/v1/sessions/:session_id/messages?after_id=&limit=` auth-free (same `/api/v1` prefix as WS); cursor takes message ID monotonic order (auto-increment primary key, not created_at, multiple entries in the same second would be misordered and missed), empty cursor=pull full from start, invalid cursor contractually rejected (400 `invalid message cursor`); limit default 50, clamp cap 200, limit+1 probes `has_more`; session doesn't exist→404 (distinguished from "no new messages" 200 empty page); security model same as WS (session_id is the capability credential, visitor auth upgrade see #2); four-package tests (infra cursor semantics + application validation mapping + delivery pagination probing + handlers routing/error mapping), 100% coverage per package |
| 2 | Visitor token issuance endpoint (server-to-server exchange) + WS handshake validation wiring | M1 | ✅ landed (M3 knife 8): `POST /api/v1/guest/session` hangs under the service auth chain (host backend calls with service API key), issues a short-lived HS256 token bound to session_id (payload typ:"guest"+sid+iat+exp, self-signed self-verified reusing jwt.secret's trust domain), doesn't require the conversation row to already exist (the row is created when the first message is persisted); WS handshake validation controlled by `security.guest_token.required` (default false preserving existing behavior, once on it validates three layers: signature+freshness+session binding, failure 401 rejects upgrade), `security.guest_token.ttl` default 24h; the required+default jwt.secret combination warns at the config gate, production/staging rejects startup + wiring layer blank secret fallback (double-layer gate); tests: platform/auth issuance validation + delivery service and validation closures + hub 401 branch + config gate + wiring five layers full coverage |
| 3 | Unread count (server-side session-level unreadable or client-derivable read cursor) | M1 | ✅ landed (M3 knife 9): `POST /api/v1/sessions/:session_id/read` (advances the cursor and returns the post-advance state) + `GET /api/v1/sessions/:session_id/unread` (queries current state) auth-free (same `/api/v1` prefix and security model as #1); response `{unread_count, last_read_message_id}`, unread convention = sender ∈ {agent, system} and message ID greater than the cursor (aligned word-for-word with D7 client derivation, customer uplink and AI first answer don't count); cursor takes messages.id monotonic order, only forward (rollback submission doesn't error but cursor doesn't move), messages must belong to that session (cross-session anchoring 400 `invalid message cursor`, same wording as #1), session doesn't exist 404; stored as sessions.visitor_read_message_id (migration 000013, targeted-column UPDATE avoiding full-column-update miswrites); D7 client's "server cursor as enhancement" enhancement surface in place |
| 4 | Visitor-usable ticket creation endpoint, accepts `ai_summary` field | M3 | ✅ landed (M3 knife 2, 48813a6): `POST /api/v1/tickets` auth-free (same `/api/v1` prefix as WS), `session_id`/`title` required + `description`/`ai_summary`; category/priority/source server-fixed defaults, tenant scope inherited from the session row; migration 000011_ticket_ai_summary; agent-side admin tickets list/detail same table same response chain naturally visible (status history/auto-dispatch/`ticket.created` event isomorphic) |
| 5 | Push token registration + session message push delivery | M3 | Registration link ✅ landed (knife 5): `POST /api/v1/push/register` auth-free (body `{session_id, platform, token}`, platform allowlist ios/android), idempotent key (session_id, platform) repeated registration keepalive refresh, separate rows for platform switch, token persisted but not echoed (migration 000012); SDK dual-endpoint `registerPushToken()` real reporting (2xx→true, failure network+false). Delivery orchestration+HTTP transport ✅ code surface landed (knife 6): PushOutboundDispatcher subscribes conversation.message_received (sender=agent, EventID dedup, WS online suppression, push only when offline) → FCM HTTP v1 / APNs token-based hand-rolled JWT transport (zero new dependencies, httptest full coverage); config push section (disabled default + double-layer gate: production missing credentials warns and rejects startup, wiring layer malformed credentials return error). End-to-end joint-debugging ⏳ real FCM service account / APNs .p8 credentials not connected (P1-1), truthfully marked not acceptable |
| 6 | (Completed, on record) WS `ai-response` carries `sources`/`strategy`/`next_action`/`handoff_reason` (zero values omitted, backward compatible) + `ai-response-delta` streaming frame (increment/terminal increment/full final frame three-stage contract, stream-interruption semantics clear) | M1 | ✅ landed (2026-09 AI main-link intelligence batch), M0 fixtures can be extracted directly from the server implementation |

> Of these #6 completed with the AI main-link intelligence batch; #1/#2/#3 are mobile's independent companions (cursor and auth can be reviewed jointly), the three have all landed with knife 7/knife 8/knife 9 respectively; #4/#5 serve M3.
