import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ServifySDK } from './sdk';
import type { RemoteAssistRecordingState } from './types';

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
    remoteAssist: { captureScreen: true, record },
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
