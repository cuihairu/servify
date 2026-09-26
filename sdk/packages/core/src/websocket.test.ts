import { afterEach, describe, expect, it, vi } from 'vitest';

import { WebSocketManager } from './websocket';

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static OPEN = 1;

  readonly url: string;
  readonly protocols?: string | string[];
  readyState = FakeWebSocket.OPEN;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: ((event: { code: number; reason: string }) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(url: string, protocols?: string | string[]) {
    this.url = url;
    this.protocols = protocols;
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

describe('WebSocketManager', () => {
  afterEach(() => {
    FakeWebSocket.instances = [];
    vi.unstubAllGlobals();
  });

  it('adds access token from auth provider before connecting', async () => {
    vi.stubGlobal('WebSocket', FakeWebSocket);

    const manager = new WebSocketManager({
      url: 'ws://localhost:8080/api/v1/ws?customer_id=1',
      authProvider: {
        getToken: async () => ({ accessToken: 'token-123' }),
      },
    });

    const connectPromise = manager.connect();
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    FakeWebSocket.instances[0].open();
    await connectPromise;

    expect(FakeWebSocket.instances[0].url).toContain('access_token=token-123');
    expect(FakeWebSocket.instances[0].url).toContain('customer_id=1');
  });

  it('refreshes token when the current token is unavailable', async () => {
    vi.stubGlobal('WebSocket', FakeWebSocket);

    const onTokenRefreshRequired = vi.fn(async () => undefined);
    const refreshToken = vi.fn(async () => ({ accessToken: 'refreshed-token' }));

    const manager = new WebSocketManager({
      url: 'ws://localhost:8080/api/v1/ws',
      authProvider: {
        getToken: async () => null,
        refreshToken,
      },
      onTokenRefreshRequired,
    });

    const connectPromise = manager.connect();
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    FakeWebSocket.instances[0].open();
    await connectPromise;

    expect(onTokenRefreshRequired).toHaveBeenCalledTimes(1);
    expect(refreshToken).toHaveBeenCalledTimes(1);
    expect(FakeWebSocket.instances[0].url).toContain('access_token=refreshed-token');
  });

  it('fails with auth_refresh_required when refresh hook cannot produce a token', async () => {
    vi.stubGlobal('WebSocket', FakeWebSocket);

    const manager = new WebSocketManager({
      url: 'ws://localhost:8080/api/v1/ws',
      authProvider: {
        getToken: async () => null,
        refreshToken: async () => null,
      },
      onTokenRefreshRequired: async () => undefined,
    });

    await expect(manager.connect()).rejects.toMatchObject({
      code: 'auth_refresh_required',
    });
  });

  it('emits WebRTC signaling events from websocket messages', async () => {
    vi.stubGlobal('WebSocket', FakeWebSocket);

    const manager = new WebSocketManager({
      url: 'ws://localhost:8080/api/v1/ws?session_id=test-session',
    });

    const offerSpy = vi.fn();
    const answerSpy = vi.fn();
    const candidateSpy = vi.fn();
    const stateSpy = vi.fn();
    manager.on('webrtc:offer', offerSpy);
    manager.on('webrtc:answer', answerSpy);
    manager.on('webrtc:candidate', candidateSpy);
    manager.on('webrtc:state', stateSpy);

    const connectPromise = manager.connect();
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    FakeWebSocket.instances[0].open();
    await connectPromise;

    FakeWebSocket.instances[0].onmessage?.({
      data: JSON.stringify({
        type: 'webrtc-offer',
        data: { type: 'offer', sdp: 'offer-sdp' },
      }),
    });
    FakeWebSocket.instances[0].onmessage?.({
      data: JSON.stringify({
        type: 'webrtc-answer',
        data: { type: 'answer', sdp: 'answer-sdp' },
      }),
    });
    FakeWebSocket.instances[0].onmessage?.({
      data: JSON.stringify({
        type: 'webrtc-candidate',
        data: {
          candidate: {
            candidate: 'candidate:1 1 UDP 2122260223 192.0.2.1 3478 typ host',
            sdpMid: '0',
            sdpMLineIndex: 0,
          },
        },
      }),
    });
    FakeWebSocket.instances[0].onmessage?.({
      data: JSON.stringify({
        type: 'webrtc-state-change',
        data: {
          connection_id: 'webrtc_demo_1',
          state: 'connected',
        },
      }),
    });

    expect(offerSpy).toHaveBeenCalledWith({ type: 'offer', sdp: 'offer-sdp' });
    expect(answerSpy).toHaveBeenCalledWith({ type: 'answer', sdp: 'answer-sdp' });
    expect(candidateSpy).toHaveBeenCalledWith({
      candidate: 'candidate:1 1 UDP 2122260223 192.0.2.1 3478 typ host',
      sdpMid: '0',
      sdpMLineIndex: 0,
    });
    expect(stateSpy).toHaveBeenCalledWith('connected');
  });

  it('emits message-translated as an annotation event without touching messages (PROTOCOL §4.5)', async () => {
    vi.stubGlobal('WebSocket', FakeWebSocket);

    const manager = new WebSocketManager({
      url: 'ws://localhost:8080/api/v1/ws?session_id=test-session',
    });

    const translatedSpy = vi.fn();
    const messageSpy = vi.fn();
    manager.on('message-translated', translatedSpy);
    manager.on('message', messageSpy);

    const connectPromise = manager.connect();
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    FakeWebSocket.instances[0].open();
    await connectPromise;

    FakeWebSocket.instances[0].onmessage?.({
      data: JSON.stringify({
        type: 'message-translated',
        data: {
          original: '你好，我想咨询退货政策',
          content: 'Hello, I would like to ask about the return policy',
          source_lang: 'zh',
          target_lang: 'en',
        },
        session_id: 'fixture-session',
      }),
    });

    // 注解帧语义：译文经独立事件暴露（按 original 关联渲染），绝不并入
    // messages、绝不产生 error。
    expect(translatedSpy).toHaveBeenCalledTimes(1);
    expect(translatedSpy).toHaveBeenCalledWith({
      original: '你好，我想咨询退货政策',
      content: 'Hello, I would like to ask about the return policy',
      source_lang: 'zh',
      target_lang: 'en',
    });
    expect(messageSpy).not.toHaveBeenCalled();
  });

  it('creates the transport socket through the injected factory', async () => {
    const created: FakeWebSocket[] = [];
    const factory = vi.fn((url: string, protocols?: string | string[]) => {
      const socket = new FakeWebSocket(url, protocols);
      created.push(socket);
      return socket as unknown as WebSocket;
    });

    // 不 stub 全局 WebSocket：工厂是唯一的构造来源
    const manager = new WebSocketManager({
      url: 'ws://localhost:8080/api/v1/ws',
      webSocketFactory: factory,
    });

    const connectPromise = manager.connect();
    await vi.waitFor(() => expect(created).toHaveLength(1));
    created[0].open();
    await connectPromise;

    expect(factory).toHaveBeenCalledWith('ws://localhost:8080/api/v1/ws', []);
    expect(FakeWebSocket.instances).toHaveLength(1);
  });

  it('throws a clear error when no WebSocket is available and no factory is injected', async () => {
    vi.stubGlobal('WebSocket', undefined);

    const manager = new WebSocketManager({ url: 'ws://localhost:8080/api/v1/ws' });

    await expect(manager.connect()).rejects.toThrow('provide options.webSocketFactory');
  });
});

describe('WebSocketManager ice-config', () => {
  afterEach(() => {
    FakeWebSocket.instances = [];
    vi.unstubAllGlobals();
  });

  it('normalizes webrtc-ice-config payloads and drops malformed entries', async () => {
    vi.stubGlobal('WebSocket', FakeWebSocket);

    const manager = new WebSocketManager({
      url: 'ws://localhost:8080/api/v1/ws?session_id=ice',
    });
    const iceSpy = vi.fn();
    manager.on('webrtc:ice-config', iceSpy);

    const connectPromise = manager.connect();
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    FakeWebSocket.instances[0].open();
    await connectPromise;

    FakeWebSocket.instances[0].onmessage?.({
      data: JSON.stringify({
        type: 'webrtc-ice-config',
        data: {
          ice_servers: [
            { urls: 'stun:stun.example.com:3478' },
            { urls: ['turn:turn.example.com:3478'], username: '1790000300', credential: 'hmac', ttl: 300 },
            { urls: 42 },
            'not-an-object',
            { username: 'missing-urls' },
            null,
          ],
        },
      }),
    });
    // 非 ice_servers 形状 / 整体非对象：归一为空数组
    FakeWebSocket.instances[0].onmessage?.({
      data: JSON.stringify({ type: 'webrtc-ice-config', data: { ice_servers: 'junk' } }),
    });
    FakeWebSocket.instances[0].onmessage?.({
      data: JSON.stringify({ type: 'webrtc-ice-config', data: null }),
    });

    await vi.waitFor(() => expect(iceSpy).toHaveBeenCalledTimes(3));
    expect(iceSpy.mock.calls[0][0]).toEqual([
      { urls: 'stun:stun.example.com:3478' },
      { urls: ['turn:turn.example.com:3478'], username: '1790000300', credential: 'hmac', ttl: 300 },
    ]);
    expect(iceSpy.mock.calls[1][0]).toEqual([]);
    expect(iceSpy.mock.calls[2][0]).toEqual([]);
  });
});

describe('WebSocketManager echo confirmation (PROTOCOL §6.3)', () => {
  afterEach(() => {
    FakeWebSocket.instances = [];
    vi.unstubAllGlobals();
  });

  async function createConnectedManager(echoTimeoutMs?: number): Promise<{
    manager: WebSocketManager;
    ws: FakeWebSocket;
  }> {
    vi.stubGlobal('WebSocket', FakeWebSocket);
    const manager = new WebSocketManager({
      url: 'ws://localhost:8080/api/v1/ws',
      ...(echoTimeoutMs !== undefined ? { echoTimeoutMs } : {}),
    });
    const connectPromise = manager.connect();
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    const ws = FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
    ws.open();
    await connectPromise;
    return { manager, ws };
  }

  function echoFrame(content: string): { data: string } {
    return { data: JSON.stringify({ type: 'text-message', data: { content } }) };
  }

  it('completes the pending send when the echoed text-message arrives', async () => {
    const { manager, ws } = await createConnectedManager();

    const pending = manager.sendWithEchoConfirmation(
      { type: 'text-message', data: { content: '你好' } },
      '你好',
    );

    ws.onmessage?.(echoFrame('你好'));
    await pending;
  });

  it('rejects with retryable transport_timeout when no echo arrives', async () => {
    const { manager } = await createConnectedManager(25);

    await expect(
      manager.sendWithEchoConfirmation({ type: 'text-message', data: { content: '你好' } }, '你好'),
    ).rejects.toMatchObject({
      code: 'transport_timeout',
      retryable: true,
      name: 'ServifyError',
    });
  });

  it('ignores echoes with different content (stale echo does not complete a new send)', async () => {
    const { manager, ws } = await createConnectedManager(25);

    const pending = manager.sendWithEchoConfirmation(
      { type: 'text-message', data: { content: '第二句' } },
      '第二句',
    );

    // 迟到的旧回显不解闸
    ws.onmessage?.(echoFrame('第一句'));
    await expect(pending).rejects.toMatchObject({ code: 'transport_timeout' });

    // 超时清闸后，下一次发送照常确认
    const next = manager.sendWithEchoConfirmation(
      { type: 'text-message', data: { content: '第二句' } },
      '第二句',
    );
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    FakeWebSocket.instances[0].onmessage?.(echoFrame('第二句'));
    await next;
  });

  it('clears the gate and rethrows when the underlying send fails', async () => {
    const { manager } = await createConnectedManager();

    await manager.disconnect();

    await expect(
      manager.sendWithEchoConfirmation({ type: 'text-message', data: { content: '你好' } }, '你好'),
    ).rejects.toMatchObject({ code: 'transport_disconnected' });

    // 发送失败已清闸：重连后旧内容回显不会误完成新发送
    const reconnect = manager.connect();
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(2));
    const ws2 = FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
    ws2.open();
    await reconnect;

    const next = manager.sendWithEchoConfirmation(
      { type: 'text-message', data: { content: '你好' } },
      '你好',
    );
    ws2.onmessage?.(echoFrame('你好'));
    await next;
  });

  it('propagates a non-text-message frame without touching the gate', async () => {
    const { manager, ws } = await createConnectedManager(25);

    const pending = manager.sendWithEchoConfirmation(
      { type: 'text-message', data: { content: '你好' } },
      '你好',
    );

    // 无关帧（畸形 data / 非字符串 content）不炸不解闸
    ws.onmessage?.({ data: JSON.stringify({ type: 'text-message', data: null }) });
    ws.onmessage?.({ data: JSON.stringify({ type: 'agent-message', data: { content: '你好' } }) });

    await expect(pending).rejects.toMatchObject({ code: 'transport_timeout' });
  });
});
