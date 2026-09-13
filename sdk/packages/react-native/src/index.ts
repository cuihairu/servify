// headless 绑定:SDK 构造、Provider 与 hooks
export { createRNServifySDK } from './createRNServifySDK';
export type { RNServifyInstance, RNServifyRuntimeOptions } from './createRNServifySDK';
export { RNServifyProvider, useServify } from './RNProvider';
export type { RNServifyProviderProps } from './RNProvider';
export {
  useChat,
  useTickets,
  useAI,
  useSatisfaction,
} from './hooks';
export type {
  UseChatReturn,
  UseTicketsReturn,
  UseAIReturn,
  UseSatisfactionReturn,
} from './hooks';

// 宿主组合所需的常用件
export { createMobileCapabilitySet } from '@servify/core';
export { MemoryMobileStorageAdapter, StorageSnapshotStore } from '@servify/app-core';
export type { MobileStorageAdapter, SessionSnapshot } from '@servify/app-core';
