# Mobile SDK Integration Guide (Android / iOS)

Both platform SDKs share the same protocol and server (WS `wss://<host>/api/v1/ws` + guest ticket `POST /api/v1/tickets`),
with API semantics mirrored one for one. This page targets integrators; protocol details see `sdk/PROTOCOL.md` (frame contract) and
platform spec §4 (frozen API surface); architecture and decisions see the design doc.

## 1. Prerequisites

| | Android | iOS |
|---|---|---|
| OS version | minSdk 24+ | iOS 15+ |
| Permissions/config | Host manifest already has `INTERNET` (most apps already do) | No Info.plist additions (when push not enabled); ATS/WSS enforced |
| Dependency injection | Zero third-party (allowlist: kotlin-stdlib, kotlinx-coroutines, kotlinx-serialization-json, okhttp, Compose) | Zero third-party (SwiftUI/Combine are all system libraries; Package.swift having no dependencies is a structural guarantee) |
| Size delta | ≤ 1.5MB (R8 measure) | ≤ 2MB (XCFramework zip measure) |

Server side: `wss://<host>/api/v1/ws` handshake and `POST /api/v1/tickets` reachable without auth; mobile native WS
does not send an Origin header; if the server has an Origin allowlist enabled, empty Origin must be allowed.

## 2. Getting the SDK artifacts

Currently in-repo distribution (no automatic release built in before an official publishing channel exists):

- **Android**: the AAR artifact from the CI `android-probe` job (artifact `servify-sdk-aar`), or build locally:
  `cd sdk/android && ./gradlew :servify-sdk:assembleRelease` (artifact at `servify-sdk/build/outputs/aar/`).
- **iOS**: XCFramework (static, `BUILD_LIBRARY_FOR_DISTRIBUTION`), produced by the CI `ios-macos` job,
  or built locally: `scripts/check-ios-sdk-size.sh` (artifact at `sdk/ios/build/ServifyKit.xcframework`).
- CocoaPods is not provided (M3 evaluation conclusion: SPM/XCFramework single track; conditions for retriggering the evaluation see the
  evaluation doc).

Import methods:

```kotlin
// Android：libs/ 放入 AAR
implementation(files("libs/servify-sdk-release.aar"))
```

```swift
// iOS：XCFramework 拖入工程（General → Frameworks, Embed 静态库选 Do Not Embed），
// 或本地路径 SwiftPM 引用（远程 URL 引用需仓库根的 Package.swift，暂不适用——见评估文档）
```

## 3. Minimal integration (≤10 lines)

### Android

```kotlin
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main) // 宿主自己的业务界面

        val chat = ServifyChat.create(
            this,
            ServifyConfig(apiUrl = "https://your-servify-host"),
        )
        chat.show(this) // 挂浮动按钮 + 首次触发 WS 连接
    }
}
```

- `show(hostActivity)`: floating button (bottom-right 56dp, primary color from `branding.primaryColor`) + panel;
  `hide()` collapses the panel but the **connection is retained**; agent/AI messages arriving in the meantime count as unread (floating button badge).
- The host Activity must be a `ComponentActivity` (the panel is mounted via ComposeView); non-bridged hosts degrade automatically:
  the floating button still shows, the panel is not mounted.
- The AAR ships consumer-rules.pro (R8 rules for kotlinx.serialization); hosts need no manual configuration.

### iOS (SwiftUI)

```swift
struct ContentView: View {
    var body: some View {
        // 浮钮 + 抽屉/全屏面板一行拉起
        ServifyView(config: try! ServifyConfig(apiUrl: "https://your-servify-host"))
    }
}
```

- When facade-level control (event subscription / tickets / push registration) is needed: after `let chat = ServifyChat.create(config:)`
  mount with `ServifyView(chat: chat)`.
- Collapse (tap ✕ / scrim) retains the connection; expand clears unread and replays messages accumulated during hide.

## 4. Config reference (`ServifyConfig`)

| Field | Type | Default | Description |
|---|---|---|---|
| `apiUrl` | String | Required | Server address, must have `https://`/`wss://` prefix (otherwise `config_invalid` at construction time); no trailing slash, no path suffix — the SDK appends WS and REST paths itself |
| `guestToken` | String? | null | Guest token (issued by the server via `POST /api/v1/guest/session`; the host backend exchanges for it holding a service key; when `security.guest_token.required=false` (default) the handshake does not consume it; once enabled, signature/ttl/session-binding are strictly validated, failure 401 rejects escalation) |
| `branding` | Branding | All defaults | Branding four-piece set, see table below |
| `presentationStyle` | Drawer / FullScreen | Drawer | Panel form |
| `loggingEnabled` | Bool | false | Debug logging |
| `pushTokenProvider` | Closure | null/nil | Push token fetching closure (host FCM/APNs integration surface); null = push not enabled |

**Branding four-piece set**: `title` (component title, default「在线客服」), `primaryColor` (Int/UInt32, ARGB, default `0xFF2563EB` brand blue),
`welcomeText` (welcome line), `offlineText` (disconnection system prompt line, null = no prompt line shown).

## 5. Ticket creation / push registration / offline hint (M3 API)

```kotlin
// Android
val config = ServifyConfig(
    apiUrl = "https://your-servify-host",
    branding = Branding(offlineText = "客服当前不在线，请稍后再来"),
    pushTokenProvider = { fetchFcmToken() },
)

val receipt: TicketReceipt? = chat.createTicket(title = "无法登录", description = "选填补充")
chat.registerPushToken() // 挂起，返回 Boolean
chat.syncReadState() // 增强面：提交服务端已读游标，挂起，返回 Boolean
```

```swift
// iOS
let config = try ServifyConfig(
    apiUrl: "https://your-servify-host",
    branding: Branding(offlineText: "客服当前不在线，请稍后再来"),
    pushTokenProvider: { await fetchAPNsToken() }
)

let receipt: TicketReceipt? = await chat.createTicket(title: "无法登录", description: "选填补充")
_ = await chat.registerPushToken()
_ = await chat.syncReadState() // 增强面：提交服务端已读游标
```

Semantics:

- **createTicket**: the server auto-assembles a conversation summary (last 10 entries, tagged `[访客]`/`[AI]`/`[客服]`) submitted with the ticket;
  success returns `TicketReceipt(ticketId)` and a system prompt line appears in the conversation "工单 #N 已创建…";
  failure returns null and the `error` flow gives the error (IO→`network`, non-2xx/malformed→`ticket_failed`). Can also be initiated from the
  "工单" button in the conversation panel title bar (form: title required + description optional).
- **registerPushToken**: provider not configured → `error` flow `unsupported`; provider returns null
  (host not authorized, normal state) → silently false, **not an error**; token obtained → actually reports to
  `POST {apiUrl}/api/v1/push/register` (2xx→`true`; IO/HTTP failure→`false`, re-reportable).
  Server-side dispatch orchestration and FCM/APNs delivery are in place (offline guests receive push); end-to-end real-credential integration is in progress.
- **offlineText**: automatically appends a system prompt line after disconnection (reconnect exhausted / handshake failure); no prompt when the user closes the panel manually.
- **syncReadState** (optional enhancement): submits the latest server message ID confirmed by the SDK's internal reconciliation (disconnection catch-up) as the
  server read cursor `POST {apiUrl}/api/v1/sessions/{id}/read`. The SDK's unread badge in V1 is still client-derived
  (this method does not change its behavior); call it only when the host needs **cross-device/cross-entry consistent unread**
  (e.g. its own backend shows unread by cursor) — typical timing: conversation page returns to foreground, user taps a push. No reconciled messages → silently
  false (normal state); IO/HTTP failure → `error` flow `network` + false, retryable at the next opportunity; the cursor only moves
  forward, never backward; repeated submissions are idempotent.

## 6. Event flows (`chat.events`)

| Flow | Content |
|---|---|
| `messages` | Messages (with streaming increment flag `isStreaming`; System prompt lines do not come from server frames) |
| `unreadCount` | Unread count (0 while the panel is visible) |
| `connectionState` | idle / connecting / connected / reconnecting(n) / disconnected |
| `agentAssigned` | Agent takeover |
| `waitingInQueue` | Queue notification |
| `error` | Error events (code table below) |

Android uses Kotlin Flow (`collect`), iOS uses `EventStream`/`StateStream` (`for await`).
Subscriptions take effect immediately: history that occurred before subscribing is obtained via `historySnapshot()` (in-memory conversation continuity; messages are not lost while the panel is hidden).

## 7. Error codes

`config_invalid` (thrown at construction time) / `network` / `server_unavailable` / `handshake_rejected` /
`send_timeout` / `unsupported` / `ticket_failed` — both platforms' `ServifyError` share the same seven codes. Detailed semantics see
platform spec §4.5.

## 8. Integration self-check list

1. `wss://<host>/api/v1/ws?session_id=test` handshake 101;
2. `apiUrl` has no trailing slash, no path suffix;
3. Obfuscated build (Android R8) / stripped build (iOS) passes — no extra integrator config needed;
4. Background → foreground: conversation page state and unread count are not lost;
5. After ticket creation, the tickets list is visible in the admin console (including the AI summary column);
6. Size reconciliation: AAR ≤ 1.5MB / XCFramework ≤ 2MB.

Common issues (handshake 400/403, repeated disconnects, no echo) reference table see
platform spec §5.4.

## 9. V1 boundaries (explicitly out of scope, to prevent integrator mis-expectations)

Offline send queue, message recall/edit, rich media messages (images/files), agent mobile client, built-in multi-language
(V1 is Chinese; Branding reserves i18n hooks). Customer messages during disconnection are not auto-resent (send timeouts are marked
failed, manual resend) — **after a successful reconnect the SDK auto-incrementally catches up** (`GET
/api/v1/sessions/:session_id/messages`, `after_id` cursor + has_more continuation): agent/AI messages
during disconnection are auto-merged into history and counted as unread; integrators need no reconciliation of their own; the server unread
cursor endpoint is also in place (`POST /api/v1/sessions/:session_id/read` +
`GET .../unread`); SDK V1 unread is client-derived as the baseline (the server cursor is the enhancement surface for hosts needing cross-entry
consistent unread). Keychain credential storage is not in V1 (anonymous sessions have no credentials).
