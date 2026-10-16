> [English](web-sdk.en.md) | **中文**

> [English](web-sdk.en.md) | **中文**

# Web SDK

Servify Web SDK 的接入文档：浏览器端实时聊天、工单、远程协助、语音通道的完整 API 与配置面。

- 包：`@servify/core` / `@servify/react` / `@servify/vue` / `@servify/vanilla`（均 1.0.0，见 `sdk/packages/*/package.json:3`）
- 治理口径（命名、发布策略、公共 API 边界）：见 [`sdk/SURFACE_GOVERNANCE.md`](../sdk/SURFACE_GOVERNANCE.md)
- 移动端（Android/iOS）SDK 见 [移动端 SDK 接入](./mobile-sdk-integration)，不在本页范围

## 1. 定位与范围

Web SDK 是浏览器框架面：`core` 提供运行时与契约，`react` / `vue` / `vanilla` 是三个框架适配层，均构建在 `core` 之上。

| 包 | 定位 | 入口 |
| --- | --- | --- |
| `@servify/core` | 共享运行时：`ServifySDK`、`ApiClient`、`WebSocketManager`、语音通道、HTTP 传输、全部契约类型 | `sdk/packages/core/src/index.ts` |
| `@servify/react` | React 适配：`ServifyProvider` + hooks | `sdk/packages/react/src/index.ts` |
| `@servify/vue` | Vue 适配：`ServifyPlugin` + composables | `sdk/packages/vue/src/index.ts` |
| `@servify/vanilla` | 原生 JS 适配：`VanillaServifySDK`，UMD 挂 `window.Servify` | `sdk/packages/vanilla/src/index.ts` |

与 mobile SDK（`app-core` / `react-native`）的能力差异：

| 能力 | Web SDK | Mobile SDK |
| --- | --- | --- |
| 实时通道 | WebSocket（`/api/v1/ws`） | WebSocket + 原生重连/后台策略 |
| 离线队列 / 推送 / 会话恢复 / 本地存储 | 无（浏览器面不实现） | `app-core` 契约面：`offline-queue` / `push` / `session-restore` / `storage`（`sdk/packages/app-core/src/index.ts:1-5`） |
| 框架面 | react / vue / vanilla | react-native |
| 语音通道 | `VoiceChannel` / `MicCapture`（core 内，Web Audio） | 平台录音能力 |

其余包不在 Web 接入范围：`@servify/api-client` 为服务端 API 客户端；`transport-http` / `transport-websocket` 为预留传输（0.0.0，private，见 `sdk/packages/transport-*/package.json:3-4`）。

## 2. 快速开始

### 2.1 安装

```bash
npm install @servify/core
# 或带框架面
npm install @servify/react   # 内部依赖 @servify/core
npm install @servify/vue
npm install @servify/vanilla
```

包未发布到 npm registry（本地 workspace 消费，见 `sdk/SURFACE_GOVERNANCE.md`）；仓库内示例经 vite alias 直接引用 `sdk/packages/*/src`。

### 2.2 CDN / UMD（免构建）

`apps/demo-sdk/` 保存浏览器可直接引用的产物（由 `sdk/packages/vanilla/dist/` 同步，见 `apps/demo-sdk/README.md`）：

```html
<script src="/demo-sdk/servify-sdk.umd.js"></script>
<script src="/demo-sdk/widget.js"></script>
<script>
  ServifyWidget.create({ baseUrl: 'http://localhost:8080' });
</script>
```

`/demo-sdk/` 由服务端在非 production 环境直接服务（`apps/server/internal/app/server/static.go:23-43`）。

### 2.3 最小可跑示例（React）

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

`config` 字段全表见 §3.1。挂件壳层（`apps/demo-sdk/widget.js`）的完整用法见 §4 与 §6。

## 3. API 参考

以下签名以源码为准（`sdk/packages/*/src`）。

### 3.1 配置（core）

`WebServifyConfig`（`sdk/packages/core/src/types.ts:6-33`）：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `apiUrl` | `string` | 服务端地址（必填） |
| `wsUrl` | `string?` | WebSocket 地址；缺省由 `apiUrl` 推导 |
| `customerId` | `string?` | 客户标识 |
| `customerName` / `customerEmail` | `string?` | 客户名 / 邮箱 |
| `sessionId` | `string?` | 应用侧会话互认：同一 id 恢复同一会话 |
| `debug` | `boolean?` | 调试日志 |
| `autoConnect` | `boolean?` | 初始化后自动连接 |
| `reconnectPolicy` | `Partial<ReconnectPolicy>?` | 重连策略，默认 5 次 / 1s 起 / 2x 退避 / 30s 封顶（`sdk/packages/core/src/contracts/reconnect.ts:8-13`） |
| `authProvider` | `() => Promise<string>` | token 提供者 |
| `onTokenRefreshRequired` | `() => Promise<string>` | token 刷新回调 |
| `remoteAssist` | `WebRTCConfig?` | 远程协助 ICE / 音视频开关 |
| `webSocketFactory` | `() => WebSocket` | 自定义 WebSocket 工厂（测试用） |
| `capabilities` | `Record<string, unknown>?` | 能力声明 |
| `echoTimeoutMs` | `number?` | 消息回执超时 |

### 3.2 core：ServifySDK

类定义 `sdk/packages/core/src/sdk.ts:33`（`extends EventEmitter<ServifyEventMap>`，事件注册用 eventemitter3 的 `on` / `off` / `once`）。

| 方法 | 签名 | 位置 |
| --- | --- | --- |
| 初始化 | `initialize(): Promise<void>` | `sdk.ts:102` |
| 连接 | `connect(): Promise<void>` | `sdk.ts:129` |
| 断开 | `disconnect(): void` | — |
| 开始会话 | `startChat(options?: { priority?: string; message?: string }): Promise<ChatSession>` | `sdk.ts:182` |
| 发消息 | `sendMessage(content: string, options?: { type?: 'text'\|'image'\|'file'; … }): Promise<Message>` | `sdk.ts:223` |
| 结束会话 | `endSession(): Promise<void>` | `sdk.ts:278` |
| 建工单 | `createTicket(ticketData: {…}): Promise<Ticket>` | `sdk.ts:292` |
| 发起远程协助 | `startRemoteAssist(options?: RemoteAssistStartOptions): Promise<RTCPeerConnection>` | `sdk.ts:318` |
| 接受远端应答 | `acceptRemoteAnswer(answer: ServifyRTCSessionDescriptionInit): Promise<void>` | `sdk.ts:417` |
| 添加 ICE | `addRemoteIce(candidate: ServifyRTCIceCandidateInit): Promise<void>` | `sdk.ts:426` |
| 结束远程协助 | `endRemoteAssist(): Promise<void>` | `sdk.ts:434` |
| 协助会话号 | `setRemoteAssistSession(assistId: string \| number): void` / `getRemoteAssistSession(): string \| null` | `sdk.ts:443,452` |
| 满意度 | `submitSatisfaction(satisfaction: {…}): Promise<void>` | `sdk.ts:457` |
| AI 问答 | `askAI(question: string): Promise<{ answer: string; confidence: number }>` | `sdk.ts:482` |
| 翻译 | `translateText(text: string, targetLang: string, options?: {…}): Promise<…>` | `sdk.ts:496` |
| 初始推荐问 | `getInitialQuestions(limit?: number, options?: { sessionId?: string \| number }): Promise<InitialQuestionsResult>` | `sdk.ts:527` |
| 联想推荐问 | `getNextQuestions(query: string, options?: { sessionId?: string \| number; limit?: number }): Promise<NextQuestionsResult>` | `sdk.ts:535` |
| 上传 | `uploadFile(file: File): Promise<{ file_url: string; file_name: string; file_size: number }>` | `sdk.ts:544` |
| 历史消息 | `getMessages(options?: {…}): Promise<Message[]>` | `sdk.ts:558` |
| 当前客户 | `getCustomer(): Customer \| null` | `sdk.ts:575` |
| 当前会话 | `getSession(): ChatSession \| null` | `sdk.ts:580` |
| 当前坐席 | `getAgent(): Agent \| null` | `sdk.ts:585` |
| 身份 | `getIdentity(): SessionIdentity` | `sdk.ts:589` |
| 状态读写 | `getState(): Record<string, unknown>` / `updateState(patch): Record<string, unknown>` | `sdk.ts:597,607` |
| 连接态 | `isConnected(): boolean` | `sdk.ts:612` |
| 未读数 | `get unreadCount(): number` | `sdk.ts:648` |
| 面板可见性 | `markSessionVisible(): void` / `markSessionHidden(): void` | `sdk.ts:653,659` |

工厂：`createServifySDK(config: ServifyConfig)`（`sdk/packages/core/src/index.ts:46`）；Web 面用 `createWebServifySDK(config: WebServifyConfig)`（react / vue / vanilla 均经此构造）。

### 3.3 react

```tsx
import { ServifyProvider, useChat, useTickets, useAI, useSatisfaction, useRemoteAssist } from '@servify/react';
```

- `ServifyProviderProps`（`sdk/packages/react/src/ServifyProvider.tsx:19-24`）：`config: WebServifyConfig`、`children: ReactNode`、`onInitialized?: () => void`、`onError?: (error: Error) => void`
- `useChat(): UseChatReturn`（`sdk/packages/react/src/useChat.ts:63`）——返回 `session` / `messages` / `agent` / `agentAssigned` / `waitingInQueue` / `unreadCount` / `isLoading` / `error`
- `useTickets(): UseTicketsReturn`（`sdk/packages/react/src/hooks.ts:18`）
- `useAI(): UseAIReturn`（`sdk/packages/react/src/hooks.ts:82`）
- `useSatisfaction(): UseSatisfactionReturn`（`sdk/packages/react/src/hooks.ts:123`）
- `useRemoteAssist(): UseRemoteAssistReturn`（`sdk/packages/react/src/useRemoteAssist.ts:18`）
- 全部 core 类型与 `createWebServifySDK` 经 `export * from '@servify/core'` 再导出（`sdk/packages/react/src/index.ts`）

### 3.4 vue

```ts
import { ServifyPlugin, useServify, useServifyReady, useChat, useAI, useTickets, useSatisfaction, useRemoteAssist } from '@servify/vue';
```

- `ServifyPluginOptions`（`sdk/packages/vue/src/plugin.ts:8`）、`useServify(): WebServifyClient`（`plugin.ts:38`）、`useServifyReady()`（`plugin.ts:47`）
- composables：`useChat` / `useAI` / `useTickets` / `useSatisfaction` / `useRemoteAssist`（`sdk/packages/vue/src/composables.ts:41,255,282,314,350`）
- 全部 core 类型与 `createWebServifySDK` 再导出（`sdk/packages/vue/src/index.ts`）

### 3.5 vanilla

```js
import { VanillaServifySDK } from '@servify/vanilla';
// 或 UMD：window.Servify
```

- 构造：`new VanillaServifySDK(config: WebServifyConfig)`（`sdk/packages/vanilla/src/index.ts:69`），内部经 `createWebServifySDK` 构造并转发全部事件（`index.ts:70-93`）
- 方法：`init(): Promise<void>`（`index.ts:98`）、`connect(): Promise<void>`、`disconnect(): void`、`startChat(options?: { priority?: string; message?: string }): Promise<ChatSession>`、`sendMessage(content: string, type?: string): Promise<Message>`、`endChat(): Promise<void>`
- 语音工具集 named re-export：`VoiceChannel` / `MicCapture` / `VOICE_SAMPLE_RATE` / `floatToPcm16` / `resampleLinear` / `pcm16ToLeBytes`（`index.ts:22-23`）

## 4. widget `data-*` 配置全表

`apps/demo-sdk/widget.js` 支持 `<script data-servify-widget …>` 自动初始化，常用面全部经 `data-*` 承载（JSON 类属性值为 JSON 字符串）。**本表为唯一权威**；`docs/embedding-guide.md` §7.4 已收口为指向本节的引用。

| 属性 | 说明 |
| --- | --- |
| `data-base-url` | 服务端地址 |
| `data-session-id` | 应用侧会话互认 |
| `data-access-token` | guest token（访客反馈入口） |
| `data-icon` | 图标：预设名或对象（见 §6） |
| `data-icon-image` | 自定义图标（内联 SVG / data URL） |
| `data-icon-url` | 图标 URL |
| `data-color` | 主色调（`data-primary-color` 为兼容别名） |
| `data-border-radius` | 触发按钮圆角 |
| `data-position` | 位置（见 §6） |
| `data-size` | 尺寸档（见 §6） |
| `data-theme` | `light` / `dark` / `auto` |
| `data-theme-light` | 亮套主题覆盖（JSON） |
| `data-theme-dark` | 暗套主题覆盖（JSON） |
| `data-theme-tokens` | 当前主题覆盖（JSON） |
| `data-theme-from-color` | 品牌主色自动生成整套 token |
| `data-theme-url` | 远程主题 JSON URL |
| `data-brand-name` | 品牌位名称 |
| `data-brand-logo` | 品牌位 logo |
| `data-brand-welcome` | 品牌位欢迎语 |

## 5. 事件与后端契约

### 5.1 事件（core）

`ServifyEventMap`（`sdk/packages/core/src/types.ts:252-278`），经 `sdk.on(event, cb)` 注册：

| 事件 | 载荷 |
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
| `ai-stream:delta` / `ai-stream:end` | 流更新 |
| `transfer:assigned` / `transfer:waiting` | 转派更新 |
| `unread-change` | `count: number` |
| `remote-assist:session` | `assistId: string` |
| `remote-assist:recording` | 录制状态 |

vanilla 面转发同名事件（`sdk/packages/vanilla/src/index.ts:70-93`）。

### 5.2 后端契约

| 通道 | 端点 | 说明 |
| --- | --- | --- |
| 实时 | `/api/v1/ws` | 默认实时通道（WebSocket-first） |
| 语音 | `/ws/voice` | 语音通道 |
| 初始推荐问 | `GET /public/suggestions/initial` | `sdk/packages/core/src/api.ts:225` |
| 联想推荐问 | `GET /public/suggestions/next?…` | `sdk/packages/core/src/api.ts:240` |
| AI | `/api/v1/ai/query`、`/api/v1/ai/status` | — |
| 上传 | `/api/v1/upload` | — |
| 满意度 | `/api/satisfactions` | — |
| 会话查询/关闭 | `/api/omni/sessions/*` | — |

推荐问归因口径：`getInitialQuestions(limit?, { sessionId? })` 与 `getNextQuestions(query, { sessionId?, limit? })` 均接受 `sessionId`（string | number），与服务端会话互认同一口径（`sdk/packages/core/src/sdk.ts:527,535`）。

## 6. 主题与样式

挂件壳层（`apps/demo-sdk/widget.js`）的外观全部走初始化参数，改样式不改代码：

- **图标三档**（`widget.js:171-182`）：① 预设名——`chat` / `smile` / `headset` / `lifebuoy` / `dots` / `bolt` 六档（`widget.js:155-161`）；② `{ image: … }`——内联 SVG / data URL；③ `{ url: … }`——URL 引用
- **位置**（`widget.js:186`）：`bottom-right` / `bottom-left` / `top-right` / `top-left`
- **尺寸档**（`widget.js:188-192`）：`small` / `medium` / `large`，或按钮直径 px 数字
- **主色**（`widget.js:258`）：`themeFromColor` → `color` → `primaryColor` → 默认 `#667eea`；给一个品牌主色自动生成整套 token（气泡自色随主色、对比文字色按 WCAG 亮度切换）
- **主题**：`theme: 'light' | 'dark' | 'auto'`；`auto` 监听 `prefers-color-scheme` 实时切换（`widget.js:373`）；亮暗两套独立覆盖（`themeLight` / `themeDark`），`themeTokens` 同名字段同时盖两套，`themeUrl` 远程主题 JSON（拉取失败不阻塞，保持本地配置）
- **品牌位**：`{ logo, name, welcome }`——窗头 logo、名称、开场欢迎语

## 7. 版本与兼容

- 当前版本：core / react / vue / vanilla 均 `1.0.0`（`sdk/packages/*/package.json:3`）；transport-http / transport-websocket 为 `0.0.0` + `private`（预留，未发布）
- 包未发布到 npm registry，本地 workspace 消费；示例经 vite alias 引用源码
- 公共 API = 各包 `src/index.ts` 的导出面（治理见 `sdk/SURFACE_GOVERNANCE.md`）
- 重连策略：默认 5 次 / 1s 起 / 2x 退避 / 30s 封顶（`sdk/packages/core/src/contracts/reconnect.ts:8-13`）；旧参数 `reconnectAttempts` / `reconnectDelay` 已被 `reconnectPolicy` 取代

## 8. 测试方式

```bash
# 单门：core / api-client / app-core / react / vue / vanilla / react-native 链式测试
npm -C sdk run test

# 类型检查（9 个包）
npm -C sdk run typecheck

# 构建（7 个包）
npm -C sdk run build
```

demo-sdk 联跑：

1. 起服务端（仓库根，`make run` 或等效入口），非 production 环境自动服务 `/demo-sdk/`
2. 打开 `http://localhost:8080/demo-sdk/examples/page-light.html`（浅色品牌站）或 `page-dark.html`（深色站）
3. 组件门禁测试：`node --test apps/demo-sdk/widget.test.mjs`
