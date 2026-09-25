import { afterEach, describe, expect, it, vi } from 'vitest';

import { ServifySDK } from './sdk';
import type { Message, WebSocketFactory } from './types';

/**
 * 未读数（§4.3 unreadCount 语义；Android/iOS 同构镜像）：面板不可见时到达的
 * 坐席/AI 内容 +1（WS 与补拉同口径），可见时清零。customer 回显、system 提示、
 * 流式 delta 不计（终帧才计）。可见性由宿主经 markSessionVisible/Hidden 接线。
 */

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static OPEN = 1;

  readonly url: string;
  readyState = FakeWebSocket.OPEN;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: ((event: { code: number; reason: string }) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  send(): void {}

  close(): void {
    this.onclose?.({ code: 1000, reason: 'closed' });
  }

  open(): void {
    this.onopen?.();
  }

  feed(frame: Record<string, unknown>): void {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }
}

function installMessageApi(pages: Array<{ body: unknown }>): void {
  vi.stubGlobal('fetch', vi.fn(async (url: string | URL) => {
    const urlText = String(url);
    if (!urlText.includes('/api/v1/sessions/test-session/messages')) {
      return { ok: true, status: 200, json: async () => ({ message: 'ok' }) } as unknown as Response;
    }
    const page = pages.shift();
    return {
      ok: true,
      status: 200,
      json: async () => page?.body ?? { messages: [], has_more: false },
    } as unknown as Response;
  }));
}

describe('ServifySDK unread count (§4.3)', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    FakeWebSocket.instances = [];
  });

  function newSdk(): ServifySDK {
    return new ServifySDK({
      apiUrl: 'http://localhost:8080',
      sessionId: 'test-session',
      autoConnect: false,
      webSocketFactory: ((url: string) => new FakeWebSocket(url)) as unknown as WebSocketFactory,
    });
  }

  async function connectOpen(sdk: ServifySDK): Promise<FakeWebSocket> {
    const connectPromise = sdk.connect();
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    const socket = FakeWebSocket.instances[0];
    socket.open();
    await connectPromise;
    return socket;
  }

  it('counts WS agent messages while hidden and broadcasts unread-change', async () => {
    installMessageApi([{ body: { messages: [], has_more: false } }]);
    const sdk = newSdk();
    const socket = await connectOpen(sdk);
    const seen: number[] = [];
    sdk.on('unread-change', (count) => seen.push(count));

    expect(sdk.unreadCount).toBe(0);
    socket.feed({ type: 'agent-message', data: { content: '您好', sender: '坐席' } });
    socket.feed({ type: 'agent-message', data: { content: '请描述问题', sender: '坐席' } });

    expect(sdk.unreadCount).toBe(2);
    expect(seen).toEqual([1, 2]);
  });

  it('counts the ai-response final frame but not streaming deltas', async () => {
    installMessageApi([{ body: { messages: [], has_more: false } }]);
    const sdk = newSdk();
    const socket = await connectOpen(sdk);

    socket.feed({ type: 'ai-response-delta', data: { id: 's1', content_delta: '你好', done: false } });
    socket.feed({ type: 'ai-response-delta', data: { id: 's1', content_delta: '，欢迎', done: false } });
    expect(sdk.unreadCount).toBe(0);

    socket.feed({ type: 'ai-response', data: { content: '你好，欢迎', confidence: 1, source: 'llm' } });
    expect(sdk.unreadCount).toBe(1);
  });

  it('does not count customer echoes or system messages', async () => {
    installMessageApi([{ body: { messages: [], has_more: false } }]);
    const sdk = newSdk();
    const socket = await connectOpen(sdk);

    socket.feed({ type: 'text-message', data: { content: '我发的' } });
    socket.feed({ type: 'transfer_notification', data: { message: '已转人工', agent_id: 7 } });
    socket.feed({ type: 'waiting_notification', data: { message: '排队中' } });

    expect(sdk.unreadCount).toBe(0);
  });

  it('clears on markSessionVisible, ignores frames while visible, resumes on hidden', async () => {
    installMessageApi([{ body: { messages: [], has_more: false } }]);
    const sdk = newSdk();
    const socket = await connectOpen(sdk);
    const seen: number[] = [];
    sdk.on('unread-change', (count) => seen.push(count));

    socket.feed({ type: 'agent-message', data: { content: '第一条', sender: '坐席' } });
    expect(sdk.unreadCount).toBe(1);

    sdk.markSessionVisible();
    expect(sdk.unreadCount).toBe(0);
    expect(seen).toEqual([1, 0]);

    // 可见时到达：渲染即消费，不计
    socket.feed({ type: 'agent-message', data: { content: '第二条', sender: '坐席' } });
    expect(sdk.unreadCount).toBe(0);

    sdk.markSessionHidden();
    socket.feed({ type: 'agent-message', data: { content: '第三条', sender: '坐席' } });
    expect(sdk.unreadCount).toBe(1);
    expect(seen).toEqual([1, 0, 1]);
  });

  it('counts reconciled agent/ai messages once (fingerprint hits do not double-count)', async () => {
    installMessageApi([{
      body: {
        messages: [
          { id: '1', conversation_id: 'test-session', sender: 'customer', kind: 'text', content: '在吗', created_at: '2026-09-24T12:00:00Z' },
          { id: '2', conversation_id: 'test-session', sender: 'agent', kind: 'text', content: '您好', created_at: '2026-09-24T12:00:01Z' },
          { id: '3', conversation_id: 'test-session', sender: 'system', kind: 'text', content: '会话已建立', created_at: '2026-09-24T12:00:02Z' },
          { id: '4', conversation_id: 'test-session', sender: 'ai', kind: 'text', content: '猜你想问', created_at: '2026-09-24T12:00:03Z' },
        ],
        has_more: false,
      },
    }]);
    const sdk = newSdk();
    await connectOpen(sdk);

    await vi.waitFor(() => expect(sdk.unreadCount).toBe(2));
    expect(sdk.unreadCount).toBe(2);
  });

  it('starts at zero for a fresh instance', () => {
    const sdk = newSdk();
    expect(sdk.unreadCount).toBe(0);
    sdk.markSessionHidden();
    expect(sdk.unreadCount).toBe(0);
  });
});
