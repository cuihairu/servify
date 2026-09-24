import { afterEach, describe, expect, it, vi } from 'vitest';

import { ServifySDK } from './sdk';
import type { Message, WebSocketFactory } from './types';

/**
 * 断线补拉（D7 流程 3；§10 #1 端点）：连接成功后 GET
 * /api/v1/sessions/{id}/messages?after_id=<游标> 增量对账——升序合并、
 * has_more 续拉、全失败面静默、指纹去重。用例与 Android
 * ReconcileMessagesTest / iOS ReconcileMessagesTests 逐一镜像（三端同构纪律）。
 *
 * 纯 REST 用例经 initialize+connect 触发 onOpen 对账（fetch mock 队列响应）；
 * 指纹去重走重连集成用例（指纹只在 WS 渲染点积累，生产路径是
 * 「WS 渲染 → 断线 → 重连补拉拉回同批」）。
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
}

interface MessagePage {
  status?: number;
  body?: unknown;
  /** json() 抛错：畸形响应体（"not-json" 走到 fetch mock 的等价面） */
  failJson?: boolean;
  /** fetch 直接 reject：传输层失败 */
  reject?: boolean;
}

function msg(id: number, sender: string, content: string): Record<string, unknown> {
  return {
    id: String(id),
    conversation_id: 'test-session',
    sender,
    kind: 'text',
    content,
    created_at: '2026-09-24T12:00:00Z',
  };
}

function installMessageApi(pages: MessagePage[]): { requests: string[] } {
  const requests: string[] = [];
  const respond = (page: MessagePage): Response => {
    if (page.reject) {
      throw new Error('transport down');
    }
    const status = page.status ?? 200;
    return {
      ok: status >= 200 && status < 300,
      status,
      statusText: '',
      json: async () => {
        if (page.failJson) {
          throw new Error('invalid json body');
        }
        return page.body;
      },
    } as unknown as Response;
  };
  vi.stubGlobal('fetch', vi.fn(async (url: string | URL) => {
    const urlText = String(url);
    if (!urlText.includes('/api/v1/sessions/test-session/messages')) {
      return respond({ body: { message: 'ok' } });
    }
    requests.push(urlText);
    const page = pages.shift();
    if (!page) {
      throw new Error('unexpected extra reconcile request');
    }
    return respond(page);
  }));
  return { requests };
}

describe('ServifySDK 断线补拉（reconnect reconcile）', () => {
  let messagesSpy: ReturnType<typeof vi.fn<(message: Message) => void>>;

  afterEach(() => {
    vi.unstubAllGlobals();
    FakeWebSocket.instances = [];
  });

  function newSdk(): ServifySDK {
    const sdk = new ServifySDK({
      apiUrl: 'http://localhost:8080',
      sessionId: 'test-session',
      autoConnect: false,
      webSocketFactory: ((url: string) => new FakeWebSocket(url)) as unknown as WebSocketFactory,
    });
    messagesSpy = vi.fn<(message: Message) => void>();
    sdk.on('message', messagesSpy);
    return sdk;
  }

  async function connectOpen(sdk: ServifySDK): Promise<FakeWebSocket> {
    // 等待「新增」实例而非仅 >0：重连用例里旧 socket 仍在 instances 中占位，
    // 抓错会把 open() 打在旧管理器上（其 onopen 已消费，新 connectPromise 永挂）。
    const before = FakeWebSocket.instances.length;
    const connectPromise = sdk.connect();
    await vi.waitFor(() => expect(FakeWebSocket.instances.length).toBe(before + 1));
    const socket = FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
    socket.open();
    await connectPromise;
    return socket;
  }

  /** 静默面公共断言：补拉请求已到达但零消息事件。 */
  async function expectSilent(requests: string[]): Promise<void> {
    await vi.waitFor(() => expect(requests.length).toBe(1));
    // 再让事件循环转几拍，确认没有迟到的事件
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(messagesSpy).not.toHaveBeenCalled();
  }

  it('merges history and emits all sender types', async () => {
    const { requests } = installMessageApi([
      { body: { messages: [msg(1, 'customer', '在吗'), msg(2, 'agent', '您好'), msg(3, 'system', '会话已建立'), msg(4, 'ai', '猜你想问：如何重置密码')], has_more: false } },
    ]);
    const sdk = newSdk();
    await sdk.initialize();
    await connectOpen(sdk);

    await vi.waitFor(() => expect(messagesSpy).toHaveBeenCalledTimes(4));
    const contents = messagesSpy.mock.calls.map(([m]: [Message]) => m.content);
    expect(contents).toEqual(['在吗', '您好', '会话已建立', '猜你想问：如何重置密码']);
    const sent = messagesSpy.mock.calls.map(([m]: [Message]) => m);
    expect(sent[0].sender_type).toBe('customer');
    expect(sent[1].sender_type).toBe('agent');
    expect(sent[2].sender_type).toBe('system');
    // ai 映射为 system + is_ai_response（与 WS 帧口径同构）
    expect(sent[3].sender_type).toBe('system');
    expect(sent[3].is_ai_response).toBe(true);
    expect(requests[0].endsWith('/api/v1/sessions/test-session/messages?limit=100')).toBe(true);
    sdk.disconnect();
  });

  it('paginates on has_more with after_id cursor', async () => {
    const { requests } = installMessageApi([
      { body: { messages: [msg(5, 'agent', '第一页')], has_more: true } },
      { body: { messages: [msg(9, 'agent', '第二页')], has_more: false } },
    ]);
    const sdk = newSdk();
    await sdk.initialize();
    await connectOpen(sdk);

    await vi.waitFor(() => expect(messagesSpy).toHaveBeenCalledTimes(2));
    const second = requests[1];
    expect(second).toContain('after_id=5');
    sdk.disconnect();
  });

  it('cursor advances monotonically within page', async () => {
    // 页内乱序（升序契约外的防御面）：游标取 max 而非首条
    const { requests } = installMessageApi([
      { body: { messages: [msg(7, 'agent', '后到的'), msg(3, 'agent', '先落的')], has_more: true } },
      { body: { messages: [], has_more: false } },
    ]);
    const sdk = newSdk();
    await sdk.initialize();
    await connectOpen(sdk);

    await vi.waitFor(() => expect(messagesSpy).toHaveBeenCalledTimes(2));
    expect(requests[1]).toContain('after_id=7');
    sdk.disconnect();
  });

  it('skips malformed entries', async () => {
    const { requests } = installMessageApi([
      { body: { messages: ['不是对象', { conversation_id: 'x' }, { id: 'abc' }, msg(12, 'agent', '合法')], has_more: false } },
    ]);
    const sdk = newSdk();
    await sdk.initialize();
    await connectOpen(sdk);

    await vi.waitFor(() => expect(messagesSpy).toHaveBeenCalledTimes(1));
    expect(messagesSpy.mock.calls[0][0].content).toBe('合法');
    expect(requests).toHaveLength(1);
    sdk.disconnect();
  });

  it('silent when session missing', async () => {
    // 404 = 会话行未建过（首连/未发过消息常态），无历史可拉
    const { requests } = installMessageApi([
      { status: 404, body: { error: 'Session not found' } },
    ]);
    const sdk = newSdk();
    await sdk.initialize();
    await connectOpen(sdk);
    await expectSilent(requests);
    sdk.disconnect();
  });

  it('silent on http error', async () => {
    const { requests } = installMessageApi([
      { status: 500, body: { error: 'boom' } },
    ]);
    const sdk = newSdk();
    await sdk.initialize();
    await connectOpen(sdk);
    await expectSilent(requests);
    sdk.disconnect();
  });

  it('silent on transport failure', async () => {
    const { requests } = installMessageApi([
      { reject: true },
    ]);
    const sdk = newSdk();
    await sdk.initialize();
    await connectOpen(sdk);
    await expectSilent(requests);
    sdk.disconnect();
  });

  it('silent on malformed body', async () => {
    const { requests } = installMessageApi([
      { failJson: true },
    ]);
    const sdk = newSdk();
    await sdk.initialize();
    await connectOpen(sdk);
    await expectSilent(requests);
    sdk.disconnect();
  });

  it('silent when messages field missing', async () => {
    const { requests } = installMessageApi([
      { body: { foo: 1 } },
    ]);
    const sdk = newSdk();
    await sdk.initialize();
    await connectOpen(sdk);
    await expectSilent(requests);
    sdk.disconnect();
  });

  /**
   * 指纹去重的生产路径（重连集成）：首连 WS 渲染坐席帧（指纹入表）→ 断线重连
   * → 补拉拉回同一消息。游标此时仍未确立（WS 帧无服务端 ID、空页不推进），
   * 去重只能靠指纹——命中条跳过渲染，同批新消息正常发出；再连一次验证游标
   * 已越过指纹命中条推进（命中也推进——服务端落库事实已确认）。
   */
  it('fingerprint skips rendered messages on reconcile', async () => {
    const { requests } = installMessageApi([
      { body: { messages: [], has_more: false } }, // 首连对账：空页
      { body: { messages: [msg(5, 'agent', '断线期间的消息'), msg(6, 'agent', '重连后的新消息')], has_more: false } }, // 重连补拉
      { body: { messages: [], has_more: false } }, // 第三次连接：游标应已到 6
    ]);
    const sdk = newSdk();
    await sdk.initialize();

    // 首连：WS#1 → REST#1（空页，游标不确立）
    const socket1 = await connectOpen(sdk);
    await vi.waitFor(() => expect(requests.length).toBe(1));
    expect(requests[0].endsWith('?limit=100')).toBe(true);

    // 服务端下发坐席帧（指纹入表）
    socket1.onmessage?.({
      data: JSON.stringify({ type: 'agent-message', data: { content: '断线期间的消息', sender: '坐席' } }),
    });
    await vi.waitFor(() => expect(messagesSpy).toHaveBeenCalledTimes(1));

    // 断线重连：WS#2 → REST#2，指纹命中跳过、新消息发出
    sdk.disconnect();
    await connectOpen(sdk);
    await vi.waitFor(() => expect(requests.length).toBe(2));
    expect(requests[1].includes('after_id')).toBe(false); // 游标未确立时全量拉取
    await vi.waitFor(() => expect(messagesSpy).toHaveBeenCalledTimes(2));
    const rendered = messagesSpy.mock.calls.map(([m]: [Message]) => m.content);
    expect(rendered.filter((c: string) => c === '断线期间的消息')).toHaveLength(1);
    expect(rendered).toContain('重连后的新消息');

    // 第三次连接：游标已越过指纹命中条（msg5）推进到 msg6
    sdk.disconnect();
    await connectOpen(sdk);
    await vi.waitFor(() => expect(requests.length).toBe(3));
    expect(requests[2]).toContain('after_id=6');
    sdk.disconnect();
  });
});
