# @servify/react-native

Servify headless React Native 绑定:mobile 能力集、storage/会话快照接线、
Provider 与 hooks。**不 import react-native**——RN 环境自带的 react 与
WebSocket 由宿主注入,构建无需 RN 工具链;无 UI 组件。

## 安装

```bash
npm install @servify/react-native
```

## 能力面

挂 `createMobileCapabilitySet()`:chat / realtime / knowledge 启用,
remote_assist / voice 在协商层返回 `disabled` 拒绝(headless 不带
屏幕共享与 WebRTC 语音面)。

## 转人工与流式

`useChat()` 暴露两个事件驱动状态(纯状态流,不渲染 `message` 行,
对齐 core README 的 Human handoff 条目):

- `agentAssigned: TransferAssignmentUpdate | null` — 由
  `transfer:assigned` (`{agentId, message}`) 置位;指派同时清空
  `waitingInQueue`(`waiting_human → agent_chatting` 转移)
- `waitingInQueue: TransferWaitingUpdate | null` — 由
  `transfer:waiting` (`{message}`) 置位

- `unreadCount: number` — 未读数（§4.3）:会话页隐藏（`markSessionHidden()`）时
  到达的坐席/AI 内容 +1,`markSessionVisible()` 清零
- `markSessionVisible()` / `markSessionHidden()` — 会话页可见性接线（对齐移动端
  `onSessionVisible`/`onSessionHidden`）

两者在 `sessionEnded` / `endChat()` 时清空。AI 流式经 core
`ai-stream:delta` / `ai-stream:end` 事件消费(见 `sdk/PROTOCOL.md` §4.1)。

## 快速开始

```tsx
import AsyncStorage from '@react-native-async-storage/async-storage';
import {
  RNServifyProvider,
  useChat,
  type MobileStorageAdapter,
} from '@servify/react-native';

// 用 AsyncStorage 封装 app-core 的 MobileStorageAdapter 契约
const storage: MobileStorageAdapter = {
  getItem: (key) => AsyncStorage.getItem(key),
  setItem: (key, value) => AsyncStorage.setItem(key, value),
  removeItem: (key) => AsyncStorage.removeItem(key),
};

function App() {
  return (
    <RNServifyProvider
      config={{ apiUrl: 'https://api.example.com', customerId: currentUserId }}
      runtime={{ storage, snapshotKeyPrefix: `user:${currentUserId}:` }}
    >
      <Support />
    </RNServifyProvider>
  );
}

function Support() {
  const { messages, sendMessage, startChat, isLoading } = useChat();
  // ...
}
```

SDK 也可以脱离 Provider 直接构造:

```ts
import { createRNServifySDK } from '@servify/react-native';

const instance = createRNServifySDK(
  { apiUrl: 'https://api.example.com' },
  { storage, webSocketFactory: (url, protocols) => new WebSocket(url, protocols) },
);

await instance.sdk.initialize();
await instance.snapshots.persist({ sessionId, savedAt: new Date().toISOString() });
```

## 会话快照

`instance.snapshots` 是 `@servify/app-core` 的 `StorageSnapshotStore`,
挂在你注入的 storage 上(`capture/persist/restore/clear`,损坏自愈),
用于冷启动恢复会话。

## 依赖说明

- `@servify/core`:SDK 运行时(WebSocket 经 `webSocketFactory` 注入,
  非 DOM 宿主不触碰全局 WebSocket)
- `@servify/app-core`:storage / 快照契约与实现
- `react`:peer dependency(≥16.8)

## 许可

MIT
