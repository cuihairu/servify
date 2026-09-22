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
