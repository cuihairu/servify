import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import type { ServifySDK } from '@servify/core';
import type { StorageSnapshotStore } from '@servify/app-core';
import { createRNServifySDK, type RNServifyRuntimeOptions } from './createRNServifySDK';

interface RNServifyContextType {
  sdk: ServifySDK | null;
  snapshots: StorageSnapshotStore | null;
  isInitialized: boolean;
  isConnected: boolean;
}

const RNServifyContext = createContext<RNServifyContextType>({
  sdk: null,
  snapshots: null,
  isInitialized: false,
  isConnected: false,
});

export interface RNServifyProviderProps {
  config: Parameters<typeof createRNServifySDK>[0];
  runtime: RNServifyRuntimeOptions;
  children: ReactNode;
  onInitialized?: () => void;
  onError?: (error: Error) => void;
}

/**
 * headless Provider:自持 Context(不跨包引用 @servify/react 私有上下文),
 * 初始化即挂 mobile 能力集;hooks 从此取 sdk。
 */
export function RNServifyProvider({
  config,
  runtime,
  children,
  onInitialized,
  onError,
}: RNServifyProviderProps): JSX.Element {
  const instanceRef = useRef<{ sdk: ServifySDK; snapshots: StorageSnapshotStore } | null>(null);
  const [isInitialized, setIsInitialized] = useState(false);
  const [isConnected, setIsConnected] = useState(false);

  useEffect(() => {
    const initSDK = async () => {
      try {
        if (!instanceRef.current) {
          const instance = createRNServifySDK(config, runtime);
          instanceRef.current = instance;

          instance.sdk.on('connected', () => setIsConnected(true));
          instance.sdk.on('disconnected', () => setIsConnected(false));
          instance.sdk.on('error', (error: Error) => onError?.(error));
        }

        await instanceRef.current.sdk.initialize();
        setIsInitialized(true);
        onInitialized?.();
      } catch (error) {
        onError?.(error as Error);
      }
    };

    void initSDK();

    return () => {
      if (instanceRef.current) {
        instanceRef.current.sdk.disconnect();
        instanceRef.current.sdk.removeAllListeners();
        instanceRef.current = null;
      }
    };
    // 挂载一次语义:config/runtime 以实例持有,不在依赖数组中重建
  }, []);

  const contextValue: RNServifyContextType = {
    sdk: instanceRef.current?.sdk ?? null,
    snapshots: instanceRef.current?.snapshots ?? null,
    isInitialized,
    isConnected,
  };

  return (
    <RNServifyContext.Provider value={contextValue}>
      {children}
    </RNServifyContext.Provider>
  );
}

export function useServify(): RNServifyContextType {
  return useContext(RNServifyContext);
}
