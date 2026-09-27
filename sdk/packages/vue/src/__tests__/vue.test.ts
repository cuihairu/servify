/**
 * @servify/vue 单测：node 环境、零 DOM 依赖（手搓 vue runtime-dom 最小宿主）、
 * 零网络（SDK 整体以假对象注入）。与 packages/react/src/__tests__/react.test.tsx
 * 同语义成对覆盖（unreadCount 暴露、可见性转发、unread-change 接线、
 * sessionEnded/endChat 清零、plugin 注入与错误分支、remote assist 状态映射）。
 */
import { describe, it, expect, vi, afterEach } from 'vitest';

// vue runtime-dom 在模块加载期就把 document/Element/SVGElement/window 固化进
// 内部引用（doc = typeof document !== 'undefined' ? document : null），桩必须
// 先于 vue import 就位——vi.hoisted 保证这一点。
vi.hoisted(() => {
  const g = globalThis as unknown as Record<string, unknown>;
  g.window = globalThis;
  g.Element = class Element {};
  g.SVGElement = class SVGElement {};
  g.document = {
    createComment: (text: string) => ({ nodeType: 8, nodeName: `!--${text}--`, parentNode: null }),
    createElement: (tag: string) => ({ nodeType: 1, nodeName: tag, parentNode: null }),
    createTextNode: (text: string) => ({ nodeType: 3, nodeName: `#${text}`, parentNode: null }),
  };
});

// plugin.ts 是包内唯一消费 @servify/core 运行时值的模块（composables 只
// import 类型，编译期擦除），mock 只需 createWebServifySDK 工厂。
const sdkHolder = vi.hoisted(() => ({ sdk: null as unknown }));
vi.mock('@servify/core', () => ({
  createWebServifySDK: () => sdkHolder.sdk,
}));

import { createApp, defineComponent, nextTick, toRaw } from 'vue';
import type { App } from 'vue';
import { ServifyPlugin, useServify, type ServifyPluginOptions } from '../plugin';
import { useChat, useRemoteAssist } from '../composables';
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
    startRemoteAssist: vi.fn(async () => undefined),
    acceptRemoteAnswer: vi.fn(async () => undefined),
    addRemoteIce: vi.fn(async () => undefined),
    endRemoteAssist: vi.fn(async () => undefined),
  };
}

/**
 * vue mount 的最小宿主元素：runtime-dom 只在 patch 过程读写这些成员
 * （insertBefore/removeChild 空实现、fake 节点 parentNode 为 null 时
 * remove 直接短路，与 react 侧 fakeContainer 同一手法）。
 */
function fakeContainer(): Element {
  return {
    nodeType: 1,
    nodeName: 'DIV',
    tagName: 'div',
    namespaceURI: 'http://www.w3.org/1999/xhtml',
    appendChild: () => null,
    insertBefore: () => null,
    removeChild: () => null,
    querySelector: () => null,
    firstChild: null,
  } as unknown as Element;
}

/**
 * vue 的 ref 对对象值套深响应 reactive 代理（react 的 useState 存原始引用，
 * 无此差异）：断言“就是同一个实例”必须先剥代理。
 */
function raw<T>(value: T): T {
  return value === null || value === undefined ? value : (toRaw(value as object) as T);
}

/** 挂一个跑 setup 的最小应用：plugin 经 app.use 注入，setup 捕获 composable。 */
async function mountHarness(
  pluginOptions: Partial<ServifyPluginOptions>,
  setup: () => void,
): Promise<App> {
  const app = createApp(
    defineComponent({
      setup() {
        setup();
        return () => null;
      },
    }),
  );
  app.use(ServifyPlugin, { config: { apiUrl: 'http://localhost:8080' }, ...pluginOptions });
  app.mount(fakeContainer());
  await nextTick();
  return app;
}

// 探针变量放模块作用域 + 顶层具名 setup 函数（与 react 测试文件同构）：
// 若把 let 声明在 it 回调内、再于同作用域的箭头里赋值，tsc 的流程收窄会把
// 后续读取判成 null（build/typecheck 直接报 never）。
let chat: ReturnType<typeof useChat> | null = null;
function ChatSetup() {
  chat = useChat();
}

let assist: ReturnType<typeof useRemoteAssist> | null = null;
function AssistSetup() {
  assist = useRemoteAssist();
}

afterEach(() => {
  sdkHolder.sdk = null;
  chat = null;
  assist = null;
  vi.restoreAllMocks();
});

describe('ServifyPlugin', () => {
  it('provides the sdk instance and reports initialization', async () => {
    const sdk = createMockSdk();
    sdkHolder.sdk = sdk;
    const onInitialized = vi.fn();
    let injected: unknown = null;

    const app = await mountHarness({ onInitialized }, () => {
      injected = useServify();
    });

    expect(injected).toBe(sdk);
    expect(app.config.globalProperties.$servify).toBe(sdk);
    await vi.waitFor(() => {
      expect(onInitialized).toHaveBeenCalledTimes(1);
    });
    // config 跨 use 稳定：install 只发生一次创建 + 一次 initialize。
    expect(sdk.initialize).toHaveBeenCalledTimes(1);

    app.unmount();
  });

  it('reports initialization failure through onError', async () => {
    const sdk = createMockSdk();
    sdk.initialize.mockRejectedValueOnce(new Error('init failed'));
    sdkHolder.sdk = sdk;
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
    const onError = vi.fn();

    const app = await mountHarness({ onError }, () => undefined);

    await vi.waitFor(() => {
      expect(onError).toHaveBeenCalledTimes(1);
    });
    expect((onError.mock.calls[0][0] as Error).message).toBe('init failed');
    expect(consoleError).toHaveBeenCalled();

    app.unmount();
  });

  it('throws from useServify when the plugin is not installed', () => {
    // setup 外调用 inject 会先走 vue 的开发期告警（console.warn），静音后
    // 只观察本包的显式抛错语义。
    const consoleWarn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    expect(() => useServify()).toThrow('Make sure to install the ServifyPlugin');
    consoleWarn.mockRestore();
  });
});

describe('useChat', () => {
  it('syncs session/agent on mount and exposes unreadCount starting at zero', async () => {
    const sdk = createMockSdk();
    sdk.getSession.mockReturnValue(fakeSession);
    sdk.getAgent.mockReturnValue(fakeAgent);
    sdkHolder.sdk = sdk;
    const app = await mountHarness({}, ChatSetup);

    expect(raw(chat?.session.value)).toBe(fakeSession);
    expect(raw(chat?.agent.value)).toBe(fakeAgent);
    expect(chat?.unreadCount.value).toBe(0);

    app.unmount();
  });

  it('mirrors unread-change events into unreadCount', async () => {
    const sdk = createMockSdk();
    sdkHolder.sdk = sdk;
    const app = await mountHarness({}, ChatSetup);

    sdk.emit('unread-change', 3);
    expect(chat?.unreadCount.value).toBe(3);
    sdk.emit('unread-change', 0);
    expect(chat?.unreadCount.value).toBe(0);

    app.unmount();
  });

  it('forwards markSessionVisible/markSessionHidden to the sdk', async () => {
    const sdk = createMockSdk();
    sdkHolder.sdk = sdk;
    const app = await mountHarness({}, ChatSetup);

    chat?.markSessionVisible();
    expect(sdk.markSessionVisible).toHaveBeenCalledTimes(1);
    chat?.markSessionHidden();
    expect(sdk.markSessionHidden).toHaveBeenCalledTimes(1);

    app.unmount();
  });

  it('tracks transfer:assigned/waiting and clears them with unreadCount on session_ended', async () => {
    const sdk = createMockSdk();
    sdk.getSession.mockReturnValue(fakeSession);
    sdk.getAgent.mockReturnValue(fakeAgent);
    sdkHolder.sdk = sdk;
    const app = await mountHarness({}, ChatSetup);

    sdk.emit('transfer:waiting', { position: 2 });
    expect(chat?.waitingInQueue.value).toEqual({ position: 2 });
    sdk.emit('transfer:assigned', { agentId: 3 });
    expect(chat?.agentAssigned.value).toEqual({ agentId: 3 });
    expect(chat?.waitingInQueue.value).toBeNull();

    sdk.emit('unread-change', 5);
    expect(chat?.unreadCount.value).toBe(5);

    // §4.3 语义：会话结束 → 全部本地态清零。
    sdk.emit('session_ended', fakeSession);
    expect(chat?.session.value).toBeNull();
    expect(chat?.messages.value).toEqual([]);
    expect(chat?.agent.value).toBeNull();
    expect(chat?.agentAssigned.value).toBeNull();
    expect(chat?.waitingInQueue.value).toBeNull();
    expect(chat?.unreadCount.value).toBe(0);

    app.unmount();
  });

  it('endChat clears session, messages, transfer state and unreadCount', async () => {
    const sdk = createMockSdk();
    sdk.getSession.mockReturnValue(fakeSession);
    sdk.getAgent.mockReturnValue(fakeAgent);
    sdkHolder.sdk = sdk;
    const app = await mountHarness({}, ChatSetup);

    sdk.emit('unread-change', 5);
    sdk.emit('transfer:waiting', { position: 1 });
    expect(chat?.unreadCount.value).toBe(5);

    await chat?.endChat();
    expect(sdk.endSession).toHaveBeenCalledTimes(1);
    expect(chat?.session.value).toBeNull();
    expect(chat?.messages.value).toEqual([]);
    expect(chat?.agent.value).toBeNull();
    expect(chat?.waitingInQueue.value).toBeNull();
    expect(chat?.unreadCount.value).toBe(0);

    app.unmount();
  });

  it('records the error and rethrows when startChat fails', async () => {
    const sdk = createMockSdk();
    sdk.startChat.mockRejectedValueOnce(new Error('no agent available'));
    sdkHolder.sdk = sdk;
    const app = await mountHarness({}, ChatSetup);

    await expect(chat?.startChat()).rejects.toThrow('no agent available');
    expect(chat?.error.value?.message).toBe('no agent available');
    expect(chat?.isLoading.value).toBe(false);
    expect(chat?.session.value).toBeNull();

    app.unmount();
  });
});

describe('useRemoteAssist', () => {
  it('maps webrtc state to isActive and drops the remote stream on terminal states', async () => {
    const sdk = createMockSdk();
    sdkHolder.sdk = sdk;
    const app = await mountHarness({}, AssistSetup);

    expect(assist?.state.value).toBe('idle');
    expect(assist?.isActive.value).toBe(false);

    sdk.emit('webrtc:state', 'connecting');
    expect(assist?.state.value).toBe('connecting');
    expect(assist?.isActive.value).toBe(true);

    const answer = { type: 'answer', sdp: 'v=0' };
    sdk.emit('webrtc:answer', answer);
    expect(sdk.acceptRemoteAnswer).toHaveBeenCalledWith(answer);

    const stream = { id: 'remote-1' };
    sdk.emit('webrtc:track', { streams: [stream] });
    expect(raw(assist?.remoteStream.value)).toBe(stream);

    // vue 语义（与 react 的差异面，有意保持）：idle/ended/failed 状态事件
    // 本身即清流。
    sdk.emit('webrtc:state', 'ended');
    expect(assist?.isActive.value).toBe(false);
    expect(assist?.remoteStream.value).toBeNull();

    app.unmount();
  });

  it('records the error and rethrows when startRemoteAssist fails', async () => {
    const sdk = createMockSdk();
    sdk.startRemoteAssist.mockRejectedValueOnce(new Error('offer rejected'));
    sdkHolder.sdk = sdk;
    const app = await mountHarness({}, AssistSetup);

    await expect(assist?.startRemoteAssist()).rejects.toThrow('offer rejected');
    expect(assist?.error.value?.message).toBe('offer rejected');
    expect(assist?.state.value).toBe('idle');

    app.unmount();
  });

  it('endRemoteAssist clears the remote stream through the sdk', async () => {
    const sdk = createMockSdk();
    sdkHolder.sdk = sdk;
    const app = await mountHarness({}, AssistSetup);

    sdk.emit('webrtc:track', { streams: [{ id: 'remote-1' }] });
    await assist?.endRemoteAssist();
    expect(sdk.endRemoteAssist).toHaveBeenCalledTimes(1);
    expect(assist?.remoteStream.value).toBeNull();

    app.unmount();
  });
});
