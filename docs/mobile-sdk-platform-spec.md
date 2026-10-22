# Mobile SDK Platform Spec: Android/iOS selection engineering and core API surface

Status: technical design started (no implementation). mobile-sdk-design.md answers "what to do, why, and in what order"; this document answers "what shape exactly" — layered architecture, engineering details for both platform selections, the V1-frozen API surface, and the executable refinement of the integration flow. Implementation code is out of scope for this document, but this is the reference baseline when M0/M1/M2 start: code review is checked line by line against this document; deviations require changing this document first.

Protocol contract see sdk/PROTOCOL.md (frame-level source of truth); this document does not repeat frame definitions, only references them.

---

## 1. Layered architecture (isomorphic across both platforms)

```
┌─ L2 UI 层 ────────────────────────────┐
│ Compose 会话页/抽屉/浮钮 (Android)      │
│ SwiftUI 会话页/抽屉/浮钮 (iOS)          │
└──────────────┬───────────────────────┘
               │ 只读状态（StateFlow / AsyncStream）
┌─ L1 会话核心（headless）──────────────┐
│ 状态机 · 消息仓库 · 未读计数 · 补拉编排  │
│ 快照持久化 · 推送注册编排 · 事件分发     │
└──────────────┬───────────────────────┘
               │ 发送帧 / 接收帧
┌─ L0 协议层 ───────────────────────────┐
│ WS 传输 · 帧编解码 · 保活 · 重连退避    │
│ （契约唯一事实源：sdk/PROTOCOL.md）      │
└──────────────────────────────────────┘
```

**Dependency direction is one-way: L2 → L1 → L0**. The host only sees the `ServifyChat` facade (L1+L2 assembled); all three layers are `internal`; the published surface is only the facade and the config/event models.

**Why the core must be headless (runnable without UI)**: The M0 integration probe must run WS send/receive and state-machine replay without a UI; contract replay tests must directly assert L1 state transitions rather than UI snapshots; the RN binding (react-native package already in the repo) must reuse the conversation core without Compose/SwiftUI. One headless core simultaneously serves four consumers — probe, tests, RN, native UI — this is the positive landing point for "do not copy the Web SDK structure" (07th M3): the Web structure is a "protocol+UI-in-one client class", the mobile side is split into three layers.

## 2. Android selection engineering (D3 refinement)

| Item | Decision | Why |
|---|---|---|
| Language/UI | Kotlin + Jetpack Compose (D3 signed off) | State-driven rendering is isomorphic with the conversation page |
| Kotlin/toolchain | Kotlin 2.x + JDK 17 build, compileSdk 35 | Required by Compose 2.x; compileSdk takes the current stable for SDK compilation, runtime behavior is still backstopped by minSdk 24 (a higher SDK compile target does not raise the integrator's runtime requirement) |
| Delivery form | Single AAR, internal multi-package (`connect`/`model`/`ui`), `api` surface only the facade | Multiple AARs would leak internal layering as the integrator's dependency management burden; single AAR + internal layering preserves refactoring freedom |
| Concurrency model | Facade holds an application-level `SupervisorJob` + internal `CoroutineScope`; state exposed as `StateFlow`, event flow as `SharedFlow`; public suspend functions | Supervisor isolates single-point failure without taking down the scope; StateFlow gives the UI convergent replay semantics; integrators do not touch any coroutine scope creation |
| Persistence | Snapshot (sessionId/lastMessageId/savedAt) via DataStore Preferences; guest token via EncryptedSharedPreferences | Snapshot has no sensitive fields so lightweight storage suffices; the token must go into system encrypted storage (D6 security baseline) |
| Network | OkHttp 4.x (`api` dependency, deduplicated by host, D9 allowlist) | WSS + protocol-layer keepalive `pingInterval` configured in one place (PROTOCOL.md §2 mobile spec) |
| R8 | Ships consumer proguard rules: keep message models (kotlinx-serialization reflection points) and facade entry points | Integrators enabling obfuscation can use it as-is, no hand-written rules required |
| Host interop | View-system hosts carry the conversation page via `ComposeView`; Compose hosts call directly | D3 already argued the host is not required to use Compose |

**CI and release boundary**: CI build matrix runs compile/lint/contract replay/AAR size gate (≤1.5MB, D9); **artifact publishing (Maven Central / direct AAR distribution) is executed manually by the maintainer; the repo and CI do not build in any automatic release steps** — consistent with the M4 acceptance spec, and an extension of the repo-wide release prohibition.

## 3. iOS selection engineering (D4 refinement)

| Item | Decision | Why |
|---|---|---|
| Language/UI | Swift + SwiftUI (D4 signed off), Swift tools 5.10 | State-driven rendering isomorphic with Android; 5.10 is the stable tools line since Xcode 15.3 |
| Delivery form | XCFramework + SPM binary target (D4 signed off); source projects use SPM single target (`ServifyKit`) | Same as Android: the published surface converges to one artifact, source-period layering preserves refactoring freedom |
| Concurrency model | structured concurrency; state classes `@MainActor`; events exposed as `AsyncStream`; public methods `async` | One-to-one mapping with the Kotlin side's StateFlow/suspend functions (D6 cross-platform same name same meaning); `@MainActor` converges UI state write contention |
| Persistence | token via Keychain (kSecClassGenericPassword); snapshot via a JSON file under Application Support (no sensitive fields) | Aligned with D6 security baseline; avoids introducing Keychain complexity for the snapshot |
| Network | `URLSessionWebSocketTask` + `sendPing` keepalive (PROTOCOL.md §2) | Zero third-party dependencies (D4/D9) |
| Privacy | `PrivacyInfo.xcprivacy` declares "no data collection, no tracking"; the only required system permission is network (ATS enforces WSS, no cleartext exception provided) | App Store review passes first time; not opening the ATS exception is a security posture and saves integrators filling one item |
| Host interop | UIKit hosts carry via `UIHostingController`; sheet presents the conversation page (iOS 15 has no `presentationDetents`, custom height, D4 already argued) | Both host systems can integrate |

## 4. Core API surface spec (V1 frozen surface)

Both platforms share the same name and same meaning (D6); differences are only language idioms. **What "V1 frozen" means**: the fields and methods in this section only grow, never change semantics, during M1-M3; deletion or semantic change is a breaking change and must go through surface review (extending the spirit of sdk/SURFACE_GOVERNANCE to the native surface).

### 4.1 Config object `ServifyConfig`

| Field | Type | Required | Default | Behavior |
|---|---|---|---|---|
| `apiUrl` | String | Yes | — | Server root address; enforces https/wss, http values error at construction time (error model `config_invalid`) |
| `guestToken` | String? | No | null | Guest token (D6); null = anonymous session mode (transitional form, PROTOCOL.md §1); only in memory + secure storage |
| `branding.title` | String | No | "在线客服" | Conversation page title |
| `branding.primaryColor` | Int/Color | No | Platform default primary color | Theme color (buttons, bubble emphasis) |
| `branding.welcomeText` | String | No | Platform default welcome message | First system prompt on the conversation page |
| `presentationStyle` | Enum `{drawer, fullscreen}` | No | `drawer` | Drawer/fullscreen; small screens auto-upgrade to fullscreen (D8) |
| `pushTokenProvider` | `(async) -> String?` | No | null | M3 push: if the host provides an FCM/APNs token fetching closure, the push module is enabled (D7 optionality) |
| `loggingEnabled` | Bool | No | false | Debug logging switch; **when off, message bodies and tokens are not printed** (D6 redaction baseline) |

### 4.2 Facade methods (`ServifyChat`)

| Method | Semantics | Notes |
|---|---|---|
| `create(context?, config)` | Constructs the facade: loads the SessionSnapshot, **does not establish a connection** | Lazy connection (plan §5); repeated create is the integrator's responsibility to avoid (singleton semantics, explicitly documented) |
| `show(hostActivity / presentingVC)` | Brings up the conversation UI (drawer or fullscreen); the first call triggers the WS connection | The UI parameter is the host's current foreground context; the SDK does not infer it itself |
| `hide()` | Collapses the conversation UI, connection retained | Foreground/background switching is handled internally by the SDK (plan §5 timing 4); integrators do not need to hook anything |
| `sendMessage(text)` | Sends `text-message`; success criterion = receiving your own echo frame (PROTOCOL.md §6.3), timeout marks local failure | V1 is text-only; suspend function / async |
| `createTicket(title, description?)` | Escalates to a ticket (M3): actually reports `POST /api/v1/tickets` (§10 #4 already landed), body contains session_id + title/description + auto-assembled AI conversation summary | Success returns `TicketReceipt(ticketId)`; failure returns null and the `error` flow gives the code (IO→`network`, non-2xx/malformed→`ticket_failed`), no silent success |
| `registerPushToken()` (M3) | Calls `pushTokenProvider` to get the token → reports per the app-core `PushTokenRegistration` shape | Calling when `pushTokenProvider` is not configured returns `unsupported` |
| `syncReadState()` (enhancement surface) | Submits the latest server message ID held by the catch-up zipper as the server read cursor: `POST {apiUrl}/api/v1/sessions/{id}/read` (§10 #3, body `{"last_read_message_id": "<id>"}`) | No confirmed messages (not yet reconciled) → silently false and zero requests (normal state, not an error); IO/HTTP failure → `error` flow `network` + false (re-reportable); response body is not consumed — V1 unread is still client-derived as the baseline; this method is the enhancement submission port for hosts needing cross-device/cross-entry consistent unread; the cursor only moves forward, never backward, repeated/rollback submissions are idempotent |
| `destroy()` | Disconnects, cancels the scope, clears subscriptions; the snapshot is retained (restored on next create) | Called when the host truly exits; do **not** call on ordinary backgrounding |

### 4.3 Event flow (aligned with core `ServifyEventMap` naming)

| Event | Payload | Source |
|---|---|---|
| `messages` | `ConversationMessage` (plan D5 message model) | `text-message` echo (after dedup) / `agent-message` / `ai-response` / streaming increments (with `isStreaming` flag, the UI renders in-progress bubbles accordingly) |
| `unreadCount` | Int | L1 derived: agent/system sources not yet rendered locally accumulate; cleared when the conversation page is visible |
| `connectionState` | Enum | See 4.4 |
| `reconnecting` | attempt: Int | Aligned with the core same-name event |
| `agentAssigned` | `{agentId: Long, message: String}` | `transfer_notification` (PROTOCOL.md §4.2) |
| `waitingInQueue` | `{message: String}` | `waiting_notification`; together with `agentAssigned` drives the state machine |
| `error` | `ServifyError` (4.5) | Aligned with the core same-name event |
| `ticketCreated` / `ticketUpdated` (M3) | Ticket | Aligned with the core same-name event |

**Explicitly excluded**: `agent_typing` (core has it, server never emits — PROTOCOL.md §5 dead branch, mobile does not define this event); all webrtc-family events.

### 4.4 Connection state machine

```
idle ──(首次 show)──> connecting ──> connected ──(断)──> reconnecting(n/5)──> connected
                                       │                        └─(5 次耗尽)──> disconnected
                                       └─(握手失败)──────────────> disconnected
disconnected ──(用户再次打开会话页)──> connecting
```

Parameters follow core defaults: exponential backoff starting at 1s, doubling, capped at 30s, 5 attempts (verified in source, `contracts/reconnect.ts`). Background switching does not produce disconnected — internal disconnect + auto-restore on return to foreground, externally manifested as "connection retained" (avoiding pointless host UI flicker).

### 4.5 Error model

Both platforms present: Kotlin `sealed class ServifyError` / Swift `enum ServifyError: Error`, uniformly carrying `{code, message, retryable}`:

| code | Scenario | retryable |
|---|---|---|
| `config_invalid` | apiUrl not https, required fields missing | false |
| `network` | WS connect/send failure, DNS | true |
| `server_unavailable` | Handshake 5xx | true |
| `handshake_rejected` | 400 session_id missing / Origin allowlist rejection | false (config problem) |
| `send_timeout` | Sent but no echo received within timeout | true |
| `unsupported` | Calling an M3 capability whose dependency is not enabled (e.g. `pushTokenProvider` not configured) | false |
| `ticket_failed` | M3 ticket creation failure | true |

### 4.6 app-core contract mapping (four contracts reserved, landing correspondence)

| app-core contract | Mobile landing point |
|---|---|
| `SessionSnapshot {sessionId, lastMessageId?, savedAt}` | Loaded at create / persisted on disconnect and backgrounding (Kotlin DataStore / iOS JSON file) |
| `PushTokenRegistration {token, platform, deviceId, environment?}` | The reported payload shape for `registerPushToken` (M3) |
| `MobileStorageAdapter {getItem/setItem/removeItem}` | Storage injection point: default implementation (DataStore/file), replaceable for tests and special hosts — keep the interface shape, replace the injection |
| `AppReconnectPolicy` | The typed carrier for 4.4 reconnect parameters, default value = core default, allows host override (override does not enter the V1 frozen surface, marked experimental) |

## 5. Integration flow refinement (executable version of M1 acceptance "≤10 lines")

### 5.1 Prerequisites

- Android: the host manifest already has `INTERNET` permission (most apps already do); hosts with minSdk ≥ 24 depend directly, below 24 refuse dependency resolution (this is the cost side of the coverage decision, declared on the first line of the integration doc).
- iOS: host iOS 15+; no Info.plist additions (when push is not enabled); ATS/WSS enforced.

### 5.2 Minimal integration (8-line level, corresponds to 4.2/4.3 API)

Android:

```kotlin
val servify = ServifyChat.create(context, ServifyConfig(apiUrl = "https://support.example.com"))
lifecycleScope.launch {
    servify.events.messages.collect { render(it) }        // 消息（含流式增量标记）
}
servify.events.unreadCount.observe { badge.count = it }   // 未读（自绘入口时）
servify.show(this)                                         // 首次调用触发连接
```

iOS:

```swift
let servify = ServifyChat.create(config: ServifyConfig(apiUrl: "https://support.example.com"))
Task { for await msg in servify.events.messages { render(msg) } }
Task { for await n in servify.events.unreadCount { badge.count = n } }
servify.show(from: self)
```

### 5.3 Integration checklist (integrator self-check)

1. WSS reachable: `wss://<host>/api/v1/ws?session_id=test` handshake 101;
2. `apiUrl` has no trailing slash, no path suffix (the SDK appends WS and future REST paths itself);
3. Obfuscated build (Android R8) / stripped build (iOS) passes — the SDK ships consumer rules, integrators need no extra config;
4. Background → foreground: conversation page state and unread count are not lost (handled internally by the SDK);
5. Size reconciliation: AAR ≤ 1.5MB / XCFramework ≤ 2MB (D9 budget, the integration doc gives the generation method).

### 5.4 Common integration errors reference

| Symptom | Cause | Handling |
|---|---|---|
| Handshake 400 `BadRequest` | session_id missing / apiUrl misspelled | Check apiUrl and the guest token flow |
| Handshake 403 | Origin allowlist rejection (mobile native WS has no Origin header, the server allowlist must allow empty Origin — **server config item, explicitly stated in the integration doc**) | Add an allowlist entry or configure to allow empty |
| Connection repeatedly drops (1006-class) | Proxy/firewall killing WSS long connections | Check corporate network; SDK reconnect backoff auto-recovers |
| Message sent but no echo | PROTOCOL.md §6.3 has no ACK — mark per send_timeout | UI prompts resend; do not auto-resend (plan D7 incident boundary) |

## 6. Milestone alignment

| This document's section | Serves milestone |
|---|---|
| §1 Layered / §2 Android engineering / §3 iOS engineering | M0 probe implemented per the L1 headless core spec; M1/M2 engineering scaffolding lands per the tables |
| §4 API surface | M1 freezes the Android surface; M2 iOS surface checks item by item (same name same meaning); M3 increments only touch M3-marked items |
| §5 Integration flow | The direct reference for M1 acceptance item ② (≤10 line integration); the integration doc site (M4) uses this as its basis |

This document and PROTOCOL.md are both "contract-first" assets: deviations during the implementation phase always change the doc first, then the code; review uses the doc as the baseline.
