/**
 * @servify/react 单测：node 环境、零 DOM 依赖（手搓 react-dom 最小宿主容器）、
 * 零网络（SDK 整体以假对象注入）。形态对齐 packages/react-native/src/__tests__/
 * rn.test.ts 与 packages/core/src/unread.test.ts 的 mock 口径。
 */
import { describe, it, expect, vi, afterEach } from 'vitest';

// react-dom 18 在 node 环境直接读全局 window / HTMLIFrameElement（无 jsdom）：
// 桩必须先进 globalThis，vi.hoisted 保证先于本文件一切 import 求值。
vi.hoisted(() => {
  const g = globalThis as unknown as Record<string, unknown>;
  g.window = globalThis;
  g.IS_REACT_ACT_ENVIRONMENT = true;
  g.HTMLIFrameElement = class HTMLIFrameElement {};
});

// ServifyProvider 是包内唯一消费 @servify/core 运行时值的模块（useChat /
// useRemoteAssist 只 import 类型，编译期擦除），mock 只需 createWebServifySDK
// 工厂；假 SDK 经 holder 由每个用例注入。
const sdkHolder = vi.hoisted(() => ({ sdk: null as unknown }));
vi.mock('@servify/core', () => ({
  createWebServifySDK: () => sdkHolder.sdk,
}));

import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { ServifyProvider, useServify } from '../ServifyProvider';
import { useChat, type UseChatReturn } from '../useChat';
import { useRemoteAssist, type UseRemoteAssistReturn } from '../useRemoteAssist';
import type { Agent, ChatSession } from '@servify/core';

const fakeSession: ChatSession = {
  id: 'sess-1',
  customer_id: 7,
  status: 'active',
  channel: 'web',
  priority: 'normal',
  started_at: '2026-09-27T00:00:00.000Z',
  created_at: '2026-09-27T00:00:00.000Z',
  updated_at: '2026-09-27T00:00:00.000Z',
};

const fakeAgent: Agent = {
  id: 3,
  name: 'Agent A',
  email: 'agent-a@example.com',
  status: 'online',
  created_at: '2026-09-27T00:00:00.000Z',
  updated_at: '2026-09-27T00:00:00.000Z',
};

type Handler = (payload: unknown) => void;

/** 假 WebServifyClient：事件面是自实现 emitter，方法面全部 vi.fn。 */
function createMockSdk() {
  const handlers = new Map<string, Set<Handler>>();
  return {
    on: vi.fn((event: string, handler: Handler) => {
      const set = handlers.get(event) ?? new Set<Handler>();
      set.add(handler);
      handlers.set(event, set);
    }),
    off: vi.fn((event: string, handler: Handler) => {
      handlers.get(event)?.delete(handler);
    }),
    emit(event: string, payload?: unknown) {
      handlers.get(event)?.forEach((handler) => handler(payload));
    },
    initialize: vi.fn(async () => undefined),
    disconnect: vi.fn(),
    removeAllListeners: vi.fn(),
    isConnected: vi.fn(() => false),
    getSession: vi.fn((): Agent | ChatSession | null => null),
    getAgent: vi.fn((): Agent | null => null),
    startChat: vi.fn(async () => fakeSession),
    sendMessage: vi.fn(async () => undefined),
    endSession: vi.fn(async () => undefined),
    getMessages: vi.fn(async () => ({ messages: [], has_more: false })),
    uploadFile: vi.fn(async () => ({ file_url: 'https://x/f.png', file_name: 'f.png', file_size: 3 })),
    markSessionVisible: vi.fn(),
    markSessionHidden: vi.fn(),
    askAI: vi.fn(async () => ({ answer: 'ok' })),
    startRemoteAssist: vi.fn(async () => undefined),
    acceptRemoteAnswer: vi.fn(async () => undefined),
    addRemoteIce: vi.fn(async () => undefined),
    endRemoteAssist: vi.fn(async () => undefined),
  };
}

type MockSdk = ReturnType<typeof createMockSdk>;

/**
 * react-dom createRoot 接受的最小宿主容器（node 无 DOM，逐项补齐
 * hostConfig 真正读取的字段：nodeName/tagName/namespaceURI/ownerDocument
 * 必须显式为 null，DOM 事件注册与子节点挂载全部空实现）。
 */
function fakeContainer(): HTMLElement {
  return {
    nodeType: 1,
    nodeName: 'DIV',
    tagName: 'div',
    namespaceURI: 'http://www.w3.org/1999/xhtml',
    ownerDocument: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    appendChild: () => null,
    insertBefore: () => null,
    removeChild: () => null,
    firstChild: null,
  } as unknown as HTMLElement;
}

async function render(node: ReactNode): Promise<Root> {
  const root = createRoot(fakeContainer());
  await act(async () => {
    root.render(node);
  });
  return root;
}

async function unmount(root: Root): Promise<void> {
  await act(async () => {
    root.unmount();
  });
}

// Provider deps 为 [config, onInitialized, onError]：config 必须跨渲染
// 稳定引用，否则 isInitialized 状态更新触发重渲染 → effect 重跑 → 重复
// initialize/disconnect，断言失真。
const TEST_CONFIG = { apiUrl: 'http://localhost:8080' };

function withProvider(sdk: MockSdk, children: ReactNode, options?: {
  onInitialized?: () => void;
  onError?: (error: Error) => void;
}) {
  sdkHolder.sdk = sdk;
  return (
    <ServifyProvider config={TEST_CONFIG} onInitialized={options?.onInitialized} onError={options?.onError}>
      {children}
    </ServifyProvider>
  );
}

let ctx: ReturnType<typeof useServify> | null = null;
function ContextProbe() {
  ctx = useServify();
  return null;
}

let chat: UseChatReturn | null = null;
function ChatProbe() {
  chat = useChat();
  return null;
}

let assist: UseRemoteAssistReturn | null = null;
function RemoteAssistProbe() {
  assist = useRemoteAssist();
  return null;
}

afterEach(() => {
  sdkHolder.sdk = null;
  ctx = null;
  chat = null;
  assist = null;
  vi.restoreAllMocks();
});

describe('ServifyProvider', () => {
  it('creates the sdk once, initializes it and injects it via context', async () => {
    const sdk = createMockSdk();
    const onInitialized = vi.fn();
    const root = await render(withProvider(sdk, <ContextProbe />, { onInitialized }));

    expect(sdk.initialize).toHaveBeenCalledTimes(1);
    expect(ctx?.sdk).toBe(sdk);
    expect(ctx?.isInitialized).toBe(true);
    expect(onInitialized).toHaveBeenCalledTimes(1);

    await unmount(root);
  });

  it('maps connected/disconnected events onto isConnected and forwards sdk errors', async () => {
    const sdk = createMockSdk();
    const onError = vi.fn();
    const root = await render(withProvider(sdk, <ContextProbe />, { onError }));

    expect(ctx?.isConnected).toBe(false);
    await act(async () => {
      sdk.emit('connected');
    });
    expect(ctx?.isConnected).toBe(true);

    const failure = new Error('socket dropped');
    await act(async () => {
      sdk.emit('error', failure);
    });
    expect(onError).toHaveBeenCalledWith(failure);

    await act(async () => {
      sdk.emit('disconnected');
    });
    expect(ctx?.isConnected).toBe(false);

    await unmount(root);
  });

  it('reports initialization failure through onError and stays uninitialized', async () => {
    const sdk = createMockSdk();
    sdk.initialize.mockRejectedValueOnce(new Error('init failed'));
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
    const onError = vi.fn();
    const root = await render(withProvider(sdk, <ContextProbe />, { onError }));

    await vi.waitFor(() => {
      expect(onError).toHaveBeenCalledTimes(1);
    });
    expect(onError.mock.calls[0][0]).toBeInstanceOf(Error);
    expect(ctx?.isInitialized).toBe(false);
    expect(consoleError).toHaveBeenCalled();

    await unmount(root);
  });

  it('disconnects and removes listeners on unmount', async () => {
    const sdk = createMockSdk();
    const root = await render(withProvider(sdk, <ContextProbe />));

    await unmount(root);
    expect(sdk.disconnect).toHaveBeenCalledTimes(1);
    expect(sdk.removeAllListeners).toHaveBeenCalledTimes(1);
  });
});

describe('useChat', () => {
  it('syncs session/agent on mount and exposes unreadCount starting at zero', async () => {
    const sdk = createMockSdk();
    sdk.getSession.mockReturnValue(fakeSession);
    sdk.getAgent.mockReturnValue(fakeAgent);
    const root = await render(withProvider(sdk, <ChatProbe />));

    expect(chat?.session).toBe(fakeSession);
    expect(chat?.agent).toBe(fakeAgent);
    expect(chat?.unreadCount).toBe(0);

    await unmount(root);
  });

  it('mirrors unread-change events into unreadCount', async () => {
    const sdk = createMockSdk();
    const root = await render(withProvider(sdk, <ChatProbe />));

    await act(async () => {
      sdk.emit('unread-change', 3);
    });
    expect(chat?.unreadCount).toBe(3);
    await act(async () => {
      sdk.emit('unread-change', 0);
    });
    expect(chat?.unreadCount).toBe(0);

    await unmount(root);
  });

  it('forwards markSessionVisible/markSessionHidden to the sdk', async () => {
    const sdk = createMockSdk();
    const root = await render(withProvider(sdk, <ChatProbe />));

    act(() => {
      chat?.markSessionVisible();
    });
    expect(sdk.markSessionVisible).toHaveBeenCalledTimes(1);
    act(() => {
      chat?.markSessionHidden();
    });
    expect(sdk.markSessionHidden).toHaveBeenCalledTimes(1);

    await unmount(root);
  });

  it('tracks transfer:assigned/waiting and clears them with unreadCount on session_ended', async () => {
    const sdk = createMockSdk();
    sdk.getSession.mockReturnValue(fakeSession);
    sdk.getAgent.mockReturnValue(fakeAgent);
    const root = await render(withProvider(sdk, <ChatProbe />));

    await act(async () => {
      sdk.emit('transfer:waiting', { position: 2 });
    });
    expect(chat?.waitingInQueue).toEqual({ position: 2 });
    await act(async () => {
      sdk.emit('transfer:assigned', { agentId: 3 });
    });
    expect(chat?.agentAssigned).toEqual({ agentId: 3 });
    expect(chat?.waitingInQueue).toBeNull();

    await act(async () => {
      sdk.emit('unread-change', 5);
    });
    expect(chat?.unreadCount).toBe(5);

    // §4.3 语义（与 vue composables 对齐）：会话结束 → 全部本地态清零。
    await act(async () => {
      sdk.emit('session_ended', fakeSession);
    });
    expect(chat?.session).toBeNull();
    expect(chat?.messages).toEqual([]);
    expect(chat?.agent).toBeNull();
    expect(chat?.agentAssigned).toBeNull();
    expect(chat?.waitingInQueue).toBeNull();
    expect(chat?.unreadCount).toBe(0);

    await unmount(root);
  });

  it('endChat clears session, messages, transfer state and unreadCount', async () => {
    const sdk = createMockSdk();
    sdk.getSession.mockReturnValue(fakeSession);
    sdk.getAgent.mockReturnValue(fakeAgent);
    const root = await render(withProvider(sdk, <ChatProbe />));

    await act(async () => {
      sdk.emit('unread-change', 5);
      sdk.emit('transfer:waiting', { position: 1 });
    });
    expect(chat?.unreadCount).toBe(5);

    await act(async () => {
      await chat?.endChat();
    });
    expect(sdk.endSession).toHaveBeenCalledTimes(1);
    expect(chat?.session).toBeNull();
    expect(chat?.messages).toEqual([]);
    expect(chat?.agent).toBeNull();
    expect(chat?.waitingInQueue).toBeNull();
    expect(chat?.unreadCount).toBe(0);

    await unmount(root);
  });
});

describe('useRemoteAssist', () => {
  it('throws on every action when the sdk is not mounted', async () => {
    // 不经 Provider 渲染：useServify 返回默认上下文（sdk null）→ 未装配分支。
    const root = await render(<RemoteAssistProbe />);

    await expect(assist?.startRemoteAssist()).rejects.toThrow('SDK not initialized');
    await expect(assist?.acceptRemoteAnswer({ sdp: 'x', type: 'answer' })).rejects.toThrow('SDK not initialized');
    await expect(assist?.addRemoteIce({ candidate: 'c' })).rejects.toThrow('SDK not initialized');
    await expect(assist?.endRemoteAssist()).rejects.toThrow('SDK not initialized');
    expect(assist?.error).toBeNull();

    await unmount(root);
  });

  it('records the error and rethrows when startRemoteAssist fails', async () => {
    const sdk = createMockSdk();
    sdk.startRemoteAssist.mockRejectedValueOnce(new Error('offer rejected'));
    const root = await render(withProvider(sdk, <RemoteAssistProbe />));

    let caught: unknown = null;
    await act(async () => {
      try {
        await assist?.startRemoteAssist();
      } catch (error) {
        caught = error;
      }
    });
    expect((caught as Error).message).toBe('offer rejected');
    expect(assist?.error?.message).toBe('offer rejected');
    expect(assist?.state).toBe('idle');
    expect(assist?.isActive).toBe(false);

    await unmount(root);
  });

  it('maps webrtc state events, auto-accepts answers and tracks the remote stream', async () => {
    const sdk = createMockSdk();
    const root = await render(withProvider(sdk, <RemoteAssistProbe />));

    await act(async () => {
      sdk.emit('webrtc:state', 'connecting');
    });
    expect(assist?.state).toBe('connecting');
    expect(assist?.isActive).toBe(true);

    const answer = { type: 'answer', sdp: 'v=0' };
    await act(async () => {
      sdk.emit('webrtc:answer', answer);
    });
    expect(sdk.acceptRemoteAnswer).toHaveBeenCalledWith(answer);

    const stream = { id: 'remote-1' };
    await act(async () => {
      sdk.emit('webrtc:track', { streams: [stream] });
    });
    expect(assist?.remoteStream).toBe(stream);

    await act(async () => {
      sdk.emit('remote-assist:session', 'assist-9');
      sdk.emit('remote-assist:recording', 'recording');
    });
    expect(assist?.assistSessionId).toBe('assist-9');
    expect(assist?.recordingState).toBe('recording');

    // react 语义：状态事件只驱动 isActive，remoteStream 由 endRemoteAssist 清空。
    await act(async () => {
      sdk.emit('webrtc:state', 'ended');
    });
    expect(assist?.state).toBe('ended');
    expect(assist?.isActive).toBe(false);

    await act(async () => {
      await assist?.endRemoteAssist();
    });
    expect(sdk.endRemoteAssist).toHaveBeenCalledTimes(1);
    expect(assist?.remoteStream).toBeNull();
    expect(assist?.recordingState).toBe('idle');
    expect(assist?.assistSessionId).toBeNull();

    await unmount(root);
  });
});
