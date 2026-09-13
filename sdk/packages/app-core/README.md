# @servify/app-core

App / Mobile SDK 的共享基础层：storage、离线队列、会话快照与 push token
四组零 DOM 依赖契约,附内存版 runtime 实现。iOS / Android / React Native
SDK 在其上组合,避免直接复制 Web SDK 结构。

## 安装

```bash
npm install @servify/app-core
```

## 契约

- `MobileStorageAdapter` — 键值持久化抽象(`getItem/setItem/removeItem`)
- `OfflineQueueStore` — 离线操作队列(`enqueue/peek/acknowledge/clear`)
- `SessionRestoreStrategy` / `SessionSnapshot` — 会话快照的采集/恢复/清理
- `PushTokenRegistrar` / `PushTokenRegistration` — push token 注册/注销

## Runtime

| 实现 | 契约 | 说明 |
| --- | --- | --- |
| `MemoryMobileStorageAdapter` | `MobileStorageAdapter` | 内存键值,单测/无持久化宿主默认实现 |
| `MemoryOfflineQueueStore` | `OfflineQueueStore` | FIFO 队列,`peek` 返回快照副本,`acknowledge` 出队 |
| `StorageSnapshotStore` | `SessionRestoreStrategy` | storage 之上的快照读写:capture 读取(损坏自愈为 null)、persist/restore 写入、clear 按 sessionId 匹配清理 |
| `RestPushTokenRegistrar` | `PushTokenRegistrar` | REST 形态注册/注销,端点与 fetch 全部注入,线格式 snake_case(`token/platform/device_id/environment`) |

## 快速开始

```ts
import {
  MemoryMobileStorageAdapter,
  StorageSnapshotStore,
} from '@servify/app-core';

const storage = new MemoryMobileStorageAdapter();
const snapshots = new StorageSnapshotStore(storage, { keyPrefix: 'tenant-a:' });

await snapshots.persist({
  sessionId: 'sess-1',
  customerId: 'cust-9',
  savedAt: new Date().toISOString(),
});

const snapshot = await snapshots.capture(); // 重启后恢复会话
if (snapshot) {
  await snapshots.restore(snapshot);
}
```

push registrar(服务端 push 端点尚未落地,端点与 fetch 注入,不预设路径):

```ts
import { RestPushTokenRegistrar } from '@servify/app-core';

const push = new RestPushTokenRegistrar({
  endpoint: 'https://api.example.com/api/v1/push/tokens',
  headers: { authorization: `Bearer ${token}` },
});

await push.register({ token: deviceToken, platform: 'ios', deviceId });
await push.unregister(deviceId);
```

非 2xx 与传输层失败抛 `PushTokenRegistrationError`(携带 `status`)。

## 许可

MIT
