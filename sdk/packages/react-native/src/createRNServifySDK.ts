import {
  createMobileCapabilitySet,
  createServifySDK,
  type ServifyConfig,
  type ServifySDK,
  type WebSocketFactory,
} from '@servify/core';
import { StorageSnapshotStore, type MobileStorageAdapter } from '@servify/app-core';

export interface RNServifyRuntimeOptions {
  /** 宿主持久化适配器(AsyncStorage 封装/内存实现),承载会话快照。 */
  storage: MobileStorageAdapter;
  /** WebSocket 工厂:RN 宿主注入其全局 WebSocket;缺省回落 config.webSocketFactory。 */
  webSocketFactory?: WebSocketFactory;
  /** 会话快照存储键前缀,多租户/多账号隔离用。 */
  snapshotKeyPrefix?: string;
}

export interface RNServifyInstance {
  /** core ServifySDK 实例,已挂 mobile 能力集(chat/realtime/knowledge 启用)。 */
  readonly sdk: ServifySDK;
  readonly storage: MobileStorageAdapter;
  /** 注入 storage 之上的 SessionSnapshot 读写件(capture/persist/restore/clear)。 */
  readonly snapshots: StorageSnapshotStore;
}

/**
 * headless React Native 绑定的 SDK 构造:不 import react-native,
 * 依赖宿主注入 storage 与 WebSocket 工厂;能力集固定为 mobile 集,
 * remote_assist/voice 在协商层返回 disabled 拒绝。
 */
export function createRNServifySDK(
  config: ServifyConfig,
  options: RNServifyRuntimeOptions,
): RNServifyInstance {
  if (!options?.storage) {
    throw new Error('createRNServifySDK requires options.storage (a MobileStorageAdapter)');
  }

  const sdk = createServifySDK({
    ...config,
    webSocketFactory: options.webSocketFactory ?? config.webSocketFactory,
    capabilities: createMobileCapabilitySet(),
  });

  return {
    sdk,
    storage: options.storage,
    snapshots: new StorageSnapshotStore(options.storage, {
      keyPrefix: options.snapshotKeyPrefix,
    }),
  };
}
