import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ServifySDK } from './sdk';
import { ServifyError } from './contracts/errors';
import type { RemoteAssistRecordingState, ServifyRTCIceServer } from './types';

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnyRecord = Record<string, any>;

class FakeRTCPeerConnection {
  static instances: FakeRTCPeerConnection[] = [];

  localDescription: RTCSessionDescription | null = {
    type: 'offer',
    sdp: 'v=0',
    toJSON: () => ({ type: 'offer', sdp: 'v=0' }),
  } as unknown as RTCSessionDescription;
  onicecandidate: unknown = null;
  onconnectionstatechange: unknown = null;
  ontrack: unknown = null;
  createdChannels: AnyRecord[] = [];

  constructor() {
    FakeRTCPeerConnection.instances.push(this);
  }

  createDataChannel(): AnyRecord {
    const channel: AnyRecord = { onmessage: null };
    this.createdChannels.push(channel);
    return channel;
  }

  addTrack(): void {}

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  async createOffer(): Promise<any> {
    return { type: 'offer', sdp: 'v=0' };
  }

  async setLocalDescription(): Promise<void> {}

  close(): void {}
}

class FakeMediaRecorder {
  static instances: FakeMediaRecorder[] = [];
  static supportedType = 'video/webm';

  mimeType: string;
  state = 'inactive';
  ondataavailable: ((event: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  constructor(_stream: MediaStream, options?: { mimeType?: string }) {
    this.mimeType = options?.mimeType || 'video/webm';
    FakeMediaRecorder.instances.push(this);
  }

  static isTypeSupported(type: string): boolean {
    return type === FakeMediaRecorder.supportedType;
  }

  start(): void {
    this.state = 'recording';
  }

  // 模拟真实 stop：flush 最后一个 chunk 再触发 onstop
  stop(): void {
    this.state = 'inactive';
    this.ondataavailable?.({ data: new Blob(['recorded-chunk'], { type: 'video/webm' }) });
    this.onstop?.();
  }
}

interface RecordedRequest {
  url: string;
  method: string;
  body: unknown;
}

function jsonResponse(payload: unknown): Response {
  return {
    ok: true,
    status: 200,
    json: async () => payload,
  } as unknown as Response;
}

function installFetchMocks(): RecordedRequest[] {
  const requests: RecordedRequest[] = [];
  const fetchMock = vi.fn(async (url: string | URL, init?: RequestInit) => {
    const urlText = String(url);
    let body: unknown = init?.body;
    if (typeof body === 'string') {
      body = JSON.parse(body);
    }
    requests.push({ url: urlText, method: init?.method || 'GET', body });

    if (urlText.includes('/api/v1/upload')) {
      return jsonResponse({ message: 'uploaded', filename: 'rec.webm', url: 'https://cdn.example/rec.webm', size: 1024 });
    }
    if (urlText.includes('/remote-assist/7/recording')) {
      return jsonResponse({ message: 'attached' });
    }
    return jsonResponse({ message: 'ok' });
  });
  vi.stubGlobal('fetch', fetchMock);
  return requests;
}

interface FakeTrackHandle {
  track: { stop: ReturnType<typeof vi.fn>; kind: string };
  endedListeners: Array<() => void>;
}

function installDisplayMedia(): FakeTrackHandle {
  const handle: FakeTrackHandle = {
    track: { stop: vi.fn(), kind: 'video' },
    endedListeners: [],
  };
  const stream = {
    getTracks: () => [
      {
        kind: 'video',
        stop: handle.track.stop,
        addEventListener: (type: string, listener: () => void) => {
          if (type === 'ended') {
            handle.endedListeners.push(listener);
          }
        },
      },
    ],
  };
  vi.stubGlobal('navigator', {
    ...globalThis.navigator,
    mediaDevices: {
      getDisplayMedia: vi.fn(async () => stream as unknown as MediaStream),
    },
  });
  return handle;
}

function createSDK(record: boolean): ServifySDK {
  const sdk = new ServifySDK({
    apiUrl: 'http://localhost:8080',
    autoConnect: false,
    customerId: '1',
    // 录制链路测试不关心 ICE 下发：显式空列表走宿主覆盖口，避免额外 REST 噪音
    remoteAssist: { captureScreen: true, record, iceServers: [] },
  });
  // 测试聚焦录制链路，直接注入已连接的 fake ws
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (sdk as AnyRecord).ws = { isConnected: () => true, send: vi.fn(async () => undefined) };
  return sdk;
}

describe('ServifySDK remote assist recording', () => {
  let recordingStates: RemoteAssistRecordingState[];

  beforeEach(() => {
    recordingStates = [];
    vi.stubGlobal('RTCPeerConnection', FakeRTCPeerConnection);
    FakeRTCPeerConnection.instances = [];
    FakeMediaRecorder.instances = [];
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('records while active and uploads + attaches on end', async () => {
    const requests = installFetchMocks();
    installDisplayMedia();
    vi.stubGlobal('MediaRecorder', FakeMediaRecorder);

    const sdk = createSDK(true);
    sdk.on('remote-assist:recording', (state) => recordingStates.push(state));
    sdk.setRemoteAssistSession(7);

    await sdk.startRemoteAssist();
    expect(FakeMediaRecorder.instances).toHaveLength(1);
    expect(FakeMediaRecorder.instances[0].state).toBe('recording');

    await sdk.endRemoteAssist();
    expect(recordingStates).toEqual(['recording', 'saving', 'saved']);

    expect(requests).toHaveLength(2);
    expect(requests[0].url).toContain('/api/v1/upload');

    expect(requests[1].url).toContain('/api/v1/remote-assist/7/recording');
    expect(requests[1].body).toMatchObject({
      recording_key: 'https://cdn.example/rec.webm',
      recording_mime: 'video/webm',
      recording_size: expect.any(Number),
      recording_duration_ms: expect.any(Number),
    });
    expect(sdk.getRemoteAssistSession()).toBeNull();
  });

  it('binds assist session id from a data channel message', async () => {
    installFetchMocks();
    installDisplayMedia();
    vi.stubGlobal('MediaRecorder', FakeMediaRecorder);

    const sdk = createSDK(false);
    const bound: string[] = [];
    sdk.on('remote-assist:session', (assistId) => bound.push(assistId));

    await sdk.startRemoteAssist();

    const peer = FakeRTCPeerConnection.instances[0];
    const dataChannel = peer.createdChannels[0];
    dataChannel.onmessage?.({ data: JSON.stringify({ type: 'assist-session', assist_id: 7 }) });

    expect(bound).toEqual(['7']);
    expect(sdk.getRemoteAssistSession()).toBe('7');
  });

  it('skips upload when no assist session is bound', async () => {
    const requests = installFetchMocks();
    installDisplayMedia();
    vi.stubGlobal('MediaRecorder', FakeMediaRecorder);

    const sdk = createSDK(true);
    sdk.on('remote-assist:recording', (state) => recordingStates.push(state));
    await sdk.startRemoteAssist();
    await sdk.endRemoteAssist();

    expect(requests).toHaveLength(0);
    expect(recordingStates).toEqual(['recording', 'failed']);
  });

  it('does not record when the record option is off', async () => {
    installFetchMocks();
    installDisplayMedia();
    vi.stubGlobal('MediaRecorder', FakeMediaRecorder);

    const sdk = createSDK(false);
    await sdk.startRemoteAssist();
    await sdk.endRemoteAssist();

    expect(FakeMediaRecorder.instances).toHaveLength(0);
  });

  it('degrades silently when MediaRecorder is unavailable', async () => {
    const requests = installFetchMocks();
    installDisplayMedia();
    // 不 stub MediaRecorder（node 环境本身没有）

    const sdk = createSDK(true);
    sdk.on('remote-assist:recording', (state) => recordingStates.push(state));
    await sdk.startRemoteAssist();
    await sdk.endRemoteAssist();

    expect(FakeMediaRecorder.instances).toHaveLength(0);
    expect(requests).toHaveLength(0);
    expect(recordingStates).toEqual(['unsupported']);
  });

  it('flushes pending recording when the shared track ends', async () => {
    const requests = installFetchMocks();
    const handle = installDisplayMedia();
    vi.stubGlobal('MediaRecorder', FakeMediaRecorder);

    const sdk = createSDK(true);
    sdk.on('remote-assist:recording', (state) => recordingStates.push(state));
    await sdk.startRemoteAssist();
    sdk.setRemoteAssistSession(7);

    handle.endedListeners.forEach((listener) => listener());
    // stopRemoteAssistRecording 异步收尾，等微任务排空
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(recordingStates).toEqual(['recording', 'saving', 'saved']);
    expect(requests).toHaveLength(2);
    expect(requests[1].url).toContain('/api/v1/remote-assist/7/recording');
  });
});

describe('ServifySDK suggested questions', () => {
  const initialPayload = {
    success: true,
    data: {
      questions: [
        { question: '如何重置密码', source: 'knowledge_doc', source_id: '3', category: 'account', score: 1 },
        { question: '如何导出账单', source: 'knowledge_doc', source_id: '2', category: 'billing', score: 0.5 },
      ],
      meta: { strategy: 'public_knowledge_recency' },
    },
  };
  const nextPayload = {
    success: true,
    data: {
      query: '密码',
      questions: [{ question: '如何重置密码', source: 'knowledge_doc', source_id: '3', score: 1 }],
      meta: { strategy: 'public_knowledge_scored', intent: 'general' },
    },
  };

  function installSuggestionFetch(): RecordedRequest[] {
    const requests: RecordedRequest[] = [];
    const fetchMock = vi.fn(async (url: string | URL, init?: RequestInit) => {
      const urlText = String(url);
      requests.push({ url: urlText, method: init?.method || 'GET', body: init?.body });
      if (urlText.includes('/public/suggestions/initial')) {
        return jsonResponse(initialPayload);
      }
      return jsonResponse(nextPayload);
    });
    vi.stubGlobal('fetch', fetchMock);
    return requests;
  }

  function createPlainSDK(): ServifySDK {
    return new ServifySDK({ apiUrl: 'http://localhost:8080', autoConnect: false, customerId: '1' });
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('fetches initial questions without auth and forwards limit', async () => {
    const requests = installSuggestionFetch();
    const sdk = createPlainSDK();

    const result = await sdk.getInitialQuestions(5);

    expect(result.questions).toHaveLength(2);
    expect(result.questions[0].question).toBe('如何重置密码');
    expect(requests[0].url).toBe('http://localhost:8080/public/suggestions/initial?limit=5');
  });

  it('forwards session id on initial questions for exposure attribution', async () => {
    const requests = installSuggestionFetch();
    const sdk = createPlainSDK();

    await sdk.getInitialQuestions(6, { sessionId: 'ws_1' });

    const url = new URL(requests[0].url);
    expect(url.pathname).toBe('/public/suggestions/initial');
    expect(url.searchParams.get('limit')).toBe('6');
    expect(url.searchParams.get('session_id')).toBe('ws_1');
  });

  it('fetches next questions with query, session id and limit', async () => {
    const requests = installSuggestionFetch();
    const sdk = createPlainSDK();

    const result = await sdk.getNextQuestions('密码', { sessionId: 42, limit: 3 });

    expect(result.query).toBe('密码');
    expect(result.questions[0].source).toBe('knowledge_doc');
    const url = new URL(requests[0].url);
    expect(url.pathname).toBe('/public/suggestions/next');
    expect(url.searchParams.get('query')).toBe('密码');
    expect(url.searchParams.get('session_id')).toBe('42');
    expect(url.searchParams.get('limit')).toBe('3');
  });

  it('throws when the initial questions request fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500, json: async () => ({ success: false, error: 'boom' }) }) as unknown as Response));
    const sdk = createPlainSDK();

    await expect(sdk.getInitialQuestions()).rejects.toThrow('boom');
  });

  it('throws when the next questions request fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500, json: async () => ({ success: false, error: 'boom' }) }) as unknown as Response));
    const sdk = createPlainSDK();

    await expect(sdk.getNextQuestions('密码')).rejects.toThrow('boom');
  });
});

describe('ServifySDK server ICE delivery', () => {
  beforeEach(() => {
    vi.stubGlobal('RTCPeerConnection', FakeRTCPeerConnection);
    FakeRTCPeerConnection.instances = [];
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  type PeerFactory = NonNullable<
    Parameters<ServifySDK['startRemoteAssist']>[0]
  >['peerConnectionFactory'];

  function createIceSDK(peerFactory: PeerFactory): ServifySDK {
    const sdk = new ServifySDK({
      apiUrl: 'http://localhost:8080',
      autoConnect: false,
      customerId: '1',
      remoteAssist: { peerConnectionFactory: peerFactory },
    });
    // 测试聚焦 ICE 回退链，直接注入已连接的 fake ws
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (sdk as AnyRecord).ws = { isConnected: () => true, send: vi.fn(async () => undefined) };
    return sdk;
  }

  function capturePeerFactory(captured: Array<{ iceServers?: ServifyRTCIceServer[] }>): PeerFactory {
    return (config) => {
      captured.push(config);
      return new FakeRTCPeerConnection() as unknown as RTCPeerConnection;
    };
  }

  it('falls back to the REST ice-servers endpoint and caches the result', async () => {
    const icePayload = {
      success: true,
      data: {
        ice_servers: [
          { urls: 'stun:stun.example.com:3478' },
          { urls: 'turn:turn.example.com:3478', username: '1790000300', credential: 'hmac', ttl: 300 },
        ],
      },
    };
    let iceRequests = 0;
    vi.stubGlobal('fetch', vi.fn(async (url: string | URL) => {
      if (String(url).includes('/rtc/ice-servers')) {
        iceRequests += 1;
      }
      return jsonResponse(icePayload);
    }));

    const captured: Array<{ iceServers?: ServifyRTCIceServer[] }> = [];
    const sdk = createIceSDK(capturePeerFactory(captured));

    await sdk.startRemoteAssist();
    await sdk.endRemoteAssist();
    await sdk.startRemoteAssist();

    // 第二次 start 命中缓存，REST 只打一次
    expect(iceRequests).toBe(1);
    expect(captured[0].iceServers).toEqual(icePayload.data.ice_servers);
    expect(captured[1].iceServers).toEqual(icePayload.data.ice_servers);
  });

  it('prefers host-provided iceServers over the server fallback', async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse({ success: true, data: { ice_servers: [{ urls: 'stun:server:3478' }] } }),
    );
    vi.stubGlobal('fetch', fetchMock);

    const captured: Array<{ iceServers?: ServifyRTCIceServer[] }> = [];
    const sdk = createIceSDK(capturePeerFactory(captured));

    await sdk.startRemoteAssist({ iceServers: [{ urls: 'stun:host:3478' }] });

    expect(captured[0].iceServers).toEqual([{ urls: 'stun:host:3478' }]);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('falls back to an empty list when the REST endpoint fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async () =>
      ({ ok: false, status: 500, json: async () => ({ success: false, error: 'boom' }) }) as unknown as Response,
    ));

    const captured: Array<{ iceServers?: ServifyRTCIceServer[] }> = [];
    const sdk = createIceSDK(capturePeerFactory(captured));

    await sdk.startRemoteAssist();

    expect(captured[0].iceServers).toEqual([]);
  });

  it('consumes a pushed webrtc-ice-config without calling REST', async () => {
    class PushFakeWebSocket {
      static instances: PushFakeWebSocket[] = [];
      readyState = 1;
      onopen: (() => void) | null = null;
      onmessage: ((event: { data: string }) => void) | null = null;
      onclose: ((event: { code: number; reason: string }) => void) | null = null;
      onerror: ((event: unknown) => void) | null = null;
      constructor() {
        PushFakeWebSocket.instances.push(this);
      }
      send(): void {}
      close(): void {}
    }
    let iceRequests = 0;
    vi.stubGlobal('fetch', vi.fn(async (url: string | URL) => {
      if (String(url).includes('/rtc/ice-servers')) {
        iceRequests += 1;
      }
      return jsonResponse({ success: true, data: { ice_servers: [] } });
    }));

    const received: ServifyRTCIceServer[][] = [];
    const captured: Array<{ iceServers?: ServifyRTCIceServer[] }> = [];
    const sdk = new ServifySDK({
      apiUrl: 'http://localhost:8080',
      autoConnect: false,
      customerId: '1',
      remoteAssist: { peerConnectionFactory: capturePeerFactory(captured) },
      webSocketFactory: (() => new PushFakeWebSocket() as unknown as WebSocket) as NonNullable<
        ConstructorParameters<typeof ServifySDK>[0]
      >['webSocketFactory'],
    });
    // 跳过 initialize 的 REST 建号（本测试只关心 WS 接线）
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (sdk as AnyRecord).currentCustomer = { id: '1' };
    sdk.on('webrtc:ice-config', (iceServers) => received.push(iceServers));

    const connectPromise = sdk.connect();
    await vi.waitFor(() => expect(PushFakeWebSocket.instances).toHaveLength(1));
    PushFakeWebSocket.instances[0].onopen?.();
    await connectPromise;

    PushFakeWebSocket.instances[0].onmessage?.({
      data: JSON.stringify({
        type: 'webrtc-ice-config',
        data: { ice_servers: [{ urls: 'stun:pushed:3478' }] },
      }),
    });
    await vi.waitFor(() => expect(received).toHaveLength(1));
    expect(received[0]).toEqual([{ urls: 'stun:pushed:3478' }]);

    await sdk.startRemoteAssist();
    expect(captured[0].iceServers).toEqual([{ urls: 'stun:pushed:3478' }]);
    expect(iceRequests).toBe(0);
    sdk.disconnect();
  });
});

describe('ServifySDK sendMessage echo confirmation (PROTOCOL §6.3)', () => {
  function createSendReadySDK(
    impl: (message: unknown, expectedContent: string) => Promise<void>,
  ): { sdk: ServifySDK; calls: string[] } {
    const sdk = new ServifySDK({ apiUrl: 'http://localhost:8080', autoConnect: false, customerId: '1' });
    // 测试聚焦 sendMessage 门面链路，直注已连接 fake 传输（同 ICE 测试手法）
    const calls: string[] = [];
    (sdk as AnyRecord).currentSession = { id: 'ws_e2e', status: 'active' };
    (sdk as AnyRecord).ws = {
      isConnected: () => true,
      sendWithEchoConfirmation: async (message: unknown, expectedContent: string) => {
        calls.push(expectedContent);
        await impl(message, expectedContent);
      },
    };
    return { sdk, calls };
  }

  it('resolves the local message once the echo confirmation succeeds', async () => {
    const { sdk } = createSendReadySDK(async () => undefined);

    const sent = await sdk.sendMessage('你好');

    expect(sent.content).toBe('你好');
    expect(sent.session_id).toBe('ws_e2e');
    expect(sent.sender_type).toBe('customer');
  });

  it('propagates echo timeout as retryable transport_timeout (no fabricated success)', async () => {
    const { sdk } = createSendReadySDK(async () => {
      throw new ServifyError('No echo within 10000ms', { code: 'transport_timeout', retryable: true });
    });

    await expect(sdk.sendMessage('你好')).rejects.toMatchObject({
      code: 'transport_timeout',
      retryable: true,
    });
  });

  it('serializes concurrent sends so at most one echo is pending at a time', async () => {
    let releaseFirst: () => void = () => undefined;
    const firstGate = new Promise<void>((resolve) => {
      releaseFirst = resolve;
    });
    const { sdk, calls } = createSendReadySDK(async (_message, expected) => {
      if (expected === '第一句') await firstGate;
    });

    const first = sdk.sendMessage('第一句');
    const second = sdk.sendMessage('第二句');

    // 第一条回显未到时，第二条不得进入发送（对齐移动端 sendMutex 串行）
    await vi.waitFor(() => expect(calls).toEqual(['第一句']));
    releaseFirst();
    await Promise.all([first, second]);
    expect(calls).toEqual(['第一句', '第二句']);
  });

  it('lets the next send proceed after a failed one (chain does not swallow)', async () => {
    let failing = true;
    const { sdk, calls } = createSendReadySDK(async () => {
      if (failing) throw new ServifyError('No echo within 10000ms', { code: 'transport_timeout', retryable: true });
    });

    await expect(sdk.sendMessage('第一句')).rejects.toMatchObject({ code: 'transport_timeout' });
    failing = false;
    await sdk.sendMessage('第二句');
    expect(calls).toEqual(['第一句', '第二句']);
  });

  it('throws when there is no active session', async () => {
    const sdk = new ServifySDK({ apiUrl: 'http://localhost:8080', autoConnect: false, customerId: '1' });

    await expect(sdk.sendMessage('你好')).rejects.toThrow('No active session');
  });
});
