> **English** | [中文](web-sdk.md)

# Web SDK

Integration docs for the Servify Web SDK: the complete API and configuration surface for browser-side real-time chat, tickets, remote assistance, and voice channels.

- Packages: `@servify/core` / `@servify/react` / `@servify/vue` / `@servify/vanilla` (all 1.0.0, see `sdk/packages/*/package.json:3`)
- Governance spec (naming, release strategy, public API boundary): see [`sdk/SURFACE_GOVERNANCE.md`](../sdk/SURFACE_GOVERNANCE.md)
- Mobile (Android/iOS) SDK: see [mobile SDK integration](./mobile-sdk-integration); out of scope for this page

## 1. Positioning and Scope

The Web SDK is the browser framework surface: `core` provides the runtime and contracts, while `react` / `vue` / `vanilla` are three framework adapters all built on top of `core`.

| Package | Positioning | Entry |
| --- | --- | --- |
| `@servify/core` | Shared runtime: `ServifySDK`, `ApiClient`, `WebSocketManager`, voice channel, HTTP transport, all contract types | `sdk/packages/core/src/index.ts` |
| `@servify/react` | React adapter: `ServifyProvider` + hooks | `sdk/packages/react/src/index.ts` |
| `@servify/vue` | Vue adapter: `ServifyPlugin` + composables | `sdk/packages/vue/src/index.ts` |
| `@servify/vanilla` | Vanilla JS adapter: `VanillaServifySDK`, UMD attached to `window.Servify` | `sdk/packages/vanilla/src/index.ts` |

Capability differences versus the mobile SDK (`app-core` / `react-native`):

| Capability | Web SDK | Mobile SDK |
| --- | --- | --- |
| Real-time channel | WebSocket (`/api/v1/ws`) | WebSocket + native reconnect/background policies |
| Offline queue / push / session restore / local storage | None (not implemented on the browser surface) | `app-core` contract surface: `offline-queue` / `push` / `session-restore` / `storage` (`sdk/packages/app-core/src/index.ts:1-5`) |
| Framework surface | react / vue / vanilla | react-native |
| Voice channel | `VoiceChannel` / `MicCapture` (in core, Web Audio) | Platform recording capabilities |

The remaining packages are outside Web integration scope: `@servify/api-client` is a server-side API client; `transport-http` / `transport-websocket` are reserved transports (0.0.0, private, see `sdk/packages/transport-*/package.json:3-4`).

## 2. Quick Start

### 2.1 Installation

```bash
npm install @servify/core
# 或带框架面
npm install @servify/react   # 内部依赖 @servify/core
npm install @servify/vue
npm install @servify/vanilla
```

Packages are not published to the npm registry (consumed from the local workspace, see `sdk/SURFACE_GOVERNANCE.md`); in-repo examples reference `sdk/packages/*/src` directly via vite alias.

### 2.2 CDN / UMD (no build)

`apps/demo-sdk/` holds browser-ready artifacts (synced from `sdk/packages/vanilla/dist/`, see `apps/demo-sdk/README.md`):

```html
<script src="/demo-sdk/servify-sdk.umd.js"></script>
<script src="/demo-sdk/widget.js"></script>
<script>
  ServifyWidget.create({ baseUrl: 'http://localhost:8080' });
</script>
```

`/demo-sdk/` is served directly by the server in non-production environments (`apps/server/internal/app/server/static.go:23-43`).

### 2.3 Minimal Runnable Example (React)

```tsx
import { ServifyProvider, useChat } from '@servify/react';

function Chat() {
  const { session, messages, sendMessage } = useChat();
  // messages: Message[]；sendMessage(content) 发送文本
  return null;
}

export default function App() {
  return (
    <ServifyProvider config={{ apiUrl: 'http://localhost:8080' }}>
      <Chat />
    </ServifyProvider>
  );
}
```

The full `config` field table is in §3.1. Complete usage of the widget shell (`apps/demo-sdk/widget.js`) is in §4 and §6.

## 3. API Reference

The signatures below follow the source (`sdk/packages/*/src`).

### 3.1 Configuration (core)

`WebServifyConfig` (`sdk/packages/core/src/types.ts:6-33`):

| Field | Type | Description |
| --- | --- | --- |
| `apiUrl` | `string` | Server address (required) |
| `wsUrl` | `string?` | WebSocket address; defaults to being derived from `apiUrl` |
| `customerId` | `string?` | Customer identifier |
| `customerName` / `customerEmail` | `string?` | Customer name / email |
| `sessionId` | `string?` | App-side session mutual recognition: the same id restores the same session |
| `debug` | `boolean?` | Debug logging |
| `autoConnect` | `boolean?` | Auto-connect after initialization |
| `reconnectPolicy` | `Partial<ReconnectPolicy>?` | Reconnect policy; defaults to 5 attempts / 1s start / 2x backoff / 30s cap (`sdk/packages/core/src/contracts/reconnect.ts:8-13`) |
| `authProvider` | `() => Promise<string>` | Token provider |
| `onTokenRefreshRequired` | `() => Promise<string>` | Token refresh callback |
| `remoteAssist` | `WebRTCConfig?` | Remote assistance ICE / audio-video toggles |
| `webSocketFactory` | `() => WebSocket` | Custom WebSocket factory (for tests) |
| `capabilities` | `Record<string, unknown>?` | Capability declarations |
| `echoTimeoutMs` | `number?` | Message receipt timeout |

### 3.2 core: ServifySDK

Class definition `sdk/packages/core/src/sdk.ts:33` (`extends EventEmitter<ServifyEventMap>`; event registration via eventemitter3's `on` / `off` / `once`).

| Method | Signature | Location |
| --- | --- | --- |
| Initialize | `initialize(): Promise<void>` | `sdk.ts:102` |
| Connect | `connect(): Promise<void>` | `sdk.ts:129` |
| Disconnect | `disconnect(): void` | — |
| Start chat | `startChat(options?: { priority?: string; message?: string }): Promise<ChatSession>` | `sdk.ts:182` |
| Send message | `sendMessage(content: string, options?: { type?: 'text'\|'image'\|'file'; … }): Promise<Message>` | `sdk.ts:223` |
| End session | `endSession(): Promise<void>` | `sdk.ts:278` |
| Create ticket | `createTicket(ticketData: {…}): Promise<Ticket>` | `sdk.ts:292` |
| Start remote assistance | `startRemoteAssist(options?: RemoteAssistStartOptions): Promise<RTCPeerConnection>` | `sdk.ts:318` |
| Accept remote answer | `acceptRemoteAnswer(answer: ServifyRTCSessionDescriptionInit): Promise<void>` | `sdk.ts:417` |
| Add ICE | `addRemoteIce(candidate: ServifyRTCIceCandidateInit): Promise<void>` | `sdk.ts:426` |
| End remote assistance | `endRemoteAssist(): Promise<void>` | `sdk.ts:434` |
| Assist session id | `setRemoteAssistSession(assistId: string \| number): void` / `getRemoteAssistSession(): string \| null` | `sdk.ts:443,452` |
| Satisfaction | `submitSatisfaction(satisfaction: {…}): Promise<void>` | `sdk.ts:457` |
| AI Q&A | `askAI(question: string): Promise<{ answer: string; confidence: number }>` | `sdk.ts:482` |
| Translation | `translateText(text: string, targetLang: string, options?: {…}): Promise<…>` | `sdk.ts:496` |
| Initial suggested questions | `getInitialQuestions(limit?: number, options?: { sessionId?: string \| number }): Promise<InitialQuestionsResult>` | `sdk.ts:527` |
| Autocomplete suggested questions | `getNextQuestions(query: string, options?: { sessionId?: string \| number; limit?: number }): Promise<NextQuestionsResult>` | `sdk.ts:535` |
| Upload | `uploadFile(file: File): Promise<{ file_url: string; file_name: string; file_size: number }>` | `sdk.ts:544` |
| Message history | `getMessages(options?: {…}): Promise<Message[]>` | `sdk.ts:558` |
| Current customer | `getCustomer(): Customer \| null` | `sdk.ts:575` |
| Current session | `getSession(): ChatSession \| null` | `sdk.ts:580` |
| Current agent | `getAgent(): Agent \| null` | `sdk.ts:585` |
| Identity | `getIdentity(): SessionIdentity` | `sdk.ts:589` |
| State read/write | `getState(): Record<string, unknown>` / `updateState(patch): Record<string, unknown>` | `sdk.ts:597,607` |
| Connection state | `isConnected(): boolean` | `sdk.ts:612` |
| Unread count | `get unreadCount(): number` | `sdk.ts:648` |
| Panel visibility | `markSessionVisible(): void` / `markSessionHidden(): void` | `sdk.ts:653,659` |

Factories: `createServifySDK(config: ServifyConfig)` (`sdk/packages/core/src/index.ts:46`); the Web surface uses `createWebServifySDK(config: WebServifyConfig)` (react / vue / vanilla all construct via this).

### 3.3 react

```tsx
import { ServifyProvider, useChat, useTickets, useAI, useSatisfaction, useRemoteAssist } from '@servify/react';
```

- `ServifyProviderProps` (`sdk/packages/react/src/ServifyProvider.tsx:19-24`): `config: WebServifyConfig`, `children: ReactNode`, `onInitialized?: () => void`, `onError?: (error: Error) => void`
- `useChat(): UseChatReturn` (`sdk/packages/react/src/useChat.ts:63`) — returns `session` / `messages` / `agent` / `agentAssigned` / `waitingInQueue` / `unreadCount` / `isLoading` / `error`
- `useTickets(): UseTicketsReturn` (`sdk/packages/react/src/hooks.ts:18`)
- `useAI(): UseAIReturn` (`sdk/packages/react/src/hooks.ts:82`)
- `useSatisfaction(): UseSatisfactionReturn` (`sdk/packages/react/src/hooks.ts:123`)
- `useRemoteAssist(): UseRemoteAssistReturn` (`sdk/packages/react/src/useRemoteAssist.ts:18`)
- All core types and `createWebServifySDK` are re-exported via `export * from '@servify/core'` (`sdk/packages/react/src/index.ts`)

### 3.4 vue

```ts
import { ServifyPlugin, useServify, useServifyReady, useChat, useAI, useTickets, useSatisfaction, useRemoteAssist } from '@servify/vue';
```

- `ServifyPluginOptions` (`sdk/packages/vue/src/plugin.ts:8`), `useServify(): WebServifyClient` (`plugin.ts:38`), `useServifyReady()` (`plugin.ts:47`)
- Composables: `useChat` / `useAI` / `useTickets` / `useSatisfaction` / `useRemoteAssist` (`sdk/packages/vue/src/composables.ts:41,255,282,314,350`)
- All core types and `createWebServifySDK` re-exported (`sdk/packages/vue/src/index.ts`)

### 3.5 vanilla

```js
import { VanillaServifySDK } from '@servify/vanilla';
// 或 UMD：window.Servify
```

- Constructor: `new VanillaServifySDK(config: WebServifyConfig)` (`sdk/packages/vanilla/src/index.ts:69`), internally constructed via `createWebServifySDK` and forwarding all events (`index.ts:70-93`)
- Methods: `init(): Promise<void>` (`index.ts:98`), `connect(): Promise<void>`, `disconnect(): void`, `startChat(options?: { priority?: string; message?: string }): Promise<ChatSession>`, `sendMessage(content: string, type?: string): Promise<Message>`, `endChat(): Promise<void>`
- Named re-exports of the voice toolkit: `VoiceChannel` / `MicCapture` / `VOICE_SAMPLE_RATE` / `floatToPcm16` / `resampleLinear` / `pcm16ToLeBytes` (`index.ts:22-23`)

## 4. widget `data-*` Configuration — Full Table

`apps/demo-sdk/widget.js` supports `<script data-servify-widget …>` auto-initialization; all common surfaces go through `data-*` (JSON-typed attribute values are JSON strings). **This table is the single authority**; `docs/embedding-guide.md` §7.4 has been converged to a reference pointing here.

| Attribute | Description |
| --- | --- |
| `data-base-url` | Server address |
| `data-session-id` | App-side session mutual recognition |
| `data-access-token` | Guest token (visitor feedback entry) |
| `data-icon` | Icon: preset name or object (see §6) |
| `data-icon-image` | Custom icon (inline SVG / data URL) |
| `data-icon-url` | Icon URL |
| `data-color` | Primary color (`data-primary-color` is a compatibility alias) |
| `data-border-radius` | Trigger button border radius |
| `data-position` | Position (see §6) |
| `data-size` | Size tier (see §6) |
| `data-theme` | `light` / `dark` / `auto` |
| `data-theme-light` | Light theme override (JSON) |
| `data-theme-dark` | Dark theme override (JSON) |
| `data-theme-tokens` | Current theme override (JSON) |
| `data-theme-from-color` | Auto-generate a full token set from a brand primary color |
| `data-theme-url` | Remote theme JSON URL |
| `data-brand-name` | Brand slot name |
| `data-brand-logo` | Brand slot logo |
| `data-brand-welcome` | Brand slot welcome message |

## 5. Events and Backend Contracts

### 5.1 Events (core)

`ServifyEventMap` (`sdk/packages/core/src/types.ts:252-278`), registered via `sdk.on(event, cb)`:

| Event | Payload |
| --- | --- |
| `connected` | — |
| `disconnected` | `reason: string` |
| `reconnecting` | `attempt: number` |
| `message` | `message: Message` |
| `message-translated` | `translation: MessageTranslation` |
| `session_created` / `session_ended` | `session: ChatSession` |
| `error` | `error: Error` |
| `ticket_created` / `ticket_updated` | `ticket: Ticket` |
| `webrtc:offer` / `webrtc:answer` | SDP |
| `webrtc:candidate` | ICE candidate |
| `webrtc:track` | track event |
| `webrtc:state` | `RemoteAssistState` |
| `webrtc:ice-config` | ICE servers |
| `ai-stream:delta` / `ai-stream:end` | Stream update |
| `transfer:assigned` / `transfer:waiting` | Reassignment update |
| `unread-change` | `count: number` |
| `remote-assist:session` | `assistId: string` |
| `remote-assist:recording` | Recording state |

The vanilla surface forwards the same-named events (`sdk/packages/vanilla/src/index.ts:70-93`).

### 5.2 Backend Contracts

| Channel | Endpoint | Description |
| --- | --- | --- |
| Real-time | `/api/v1/ws` | Default real-time channel (WebSocket-first) |
| Voice | `/ws/voice` | Voice channel |
| Initial suggested questions | `GET /public/suggestions/initial` | `sdk/packages/core/src/api.ts:225` |
| Autocomplete suggested questions | `GET /public/suggestions/next?…` | `sdk/packages/core/src/api.ts:240` |
| AI | `/api/v1/ai/query`, `/api/v1/ai/status` | — |
| Upload | `/api/v1/upload` | — |
| Satisfaction | `/api/satisfactions` | — |
| Session query/close | `/api/omni/sessions/*` | — |

Suggested-question attribution spec: `getInitialQuestions(limit?, { sessionId? })` and `getNextQuestions(query, { sessionId?, limit? })` both accept `sessionId` (string \| number), on the same mutual-recognition convention as the server-side session (`sdk/packages/core/src/sdk.ts:527,535`).

## 6. Theming and Styling

The widget shell's (`apps/demo-sdk/widget.js`) appearance all goes through initialization parameters — change styles without changing code:

- **Three icon tiers** (`widget.js:171-182`): ① preset names — six tiers: `chat` / `smile` / `headset` / `lifebuoy` / `dots` / `bolt` (`widget.js:155-161`); ② `{ image: … }` — inline SVG / data URL; ③ `{ url: … }` — URL reference
- **Position** (`widget.js:186`): `bottom-right` / `bottom-left` / `top-right` / `top-left`
- **Size tiers** (`widget.js:188-192`): `small` / `medium` / `large`, or a button diameter as px number
- **Primary color** (`widget.js:258`): `themeFromColor` → `color` → `primaryColor` → default `#667eea`; give one brand primary color and the whole token set is generated automatically (bubble self-color follows the primary color; contrasting text color switches by WCAG luminance)
- **Theme**: `theme: 'light' | 'dark' | 'auto'`; `auto` watches `prefers-color-scheme` and switches live (`widget.js:373`); light and dark are independent override sets (`themeLight` / `themeDark`), `themeTokens` same-named fields override both sets, `themeUrl` is a remote theme JSON (fetch failure does not block; local config is kept)
- **Brand slot**: `{ logo, name, welcome }` — window header logo, name, and opening welcome message

## 7. Versioning and Compatibility

- Current versions: core / react / vue / vanilla all `1.0.0` (`sdk/packages/*/package.json:3`); transport-http / transport-websocket are `0.0.0` + `private` (reserved, unpublished)
- Packages are not published to the npm registry; consumed from the local workspace; examples reference source via vite alias
- Public API = the export surface of each package's `src/index.ts` (governance in `sdk/SURFACE_GOVERNANCE.md`)
- Reconnect policy: default 5 attempts / 1s start / 2x backoff / 30s cap (`sdk/packages/core/src/contracts/reconnect.ts:8-13`); legacy parameters `reconnectAttempts` / `reconnectDelay` have been superseded by `reconnectPolicy`

## 8. How to Test

```bash
# 单门：core / api-client / app-core / react / vue / vanilla / react-native 链式测试
npm -C sdk run test

# 类型检查（9 个包）
npm -C sdk run typecheck

# 构建（7 个包）
npm -C sdk run build
```

Running with demo-sdk:

1. Start the server (repo root, `make run` or equivalent entry point); in non-production environments `/demo-sdk/` is served automatically
2. Open `http://localhost:8080/demo-sdk/examples/page-light.html` (light brand site) or `page-dark.html` (dark site)
3. Component gate test: `node --test apps/demo-sdk/widget.test.mjs`
