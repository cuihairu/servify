import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { VoiceChannel } from './voice';

/**
 * VoiceChannel 单测（PROTOCOL.md §9）：握手参数拼装、二进制上行、
 * 下行四帧的必需字段校验（畸形静默忽略）与生命周期。fixture 回放
 * （protocol-fixtures.test.ts）覆盖契约面，本文件补齐分支面。
 */

class FakeVoiceSocket {
  static instances: FakeVoiceSocket[] = [];
  static OPEN = 1;

  readonly url: string;
  readyState = FakeVoiceSocket.OPEN;
  sent: Array<ArrayBuffer | string> = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: ((event: { code: number; reason: string }) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeVoiceSocket.instances.push(this);
  }

  send(data: ArrayBuffer | string): void {
    this.sent.push(data);
  }

  close(): void {
    this.onclose?.({ code: 1000, reason: 'closed' });
  }

  open(): void {
    this.onopen?.();
  }

  feed(frame: Record<string, unknown>): void {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }

  feedRaw(raw: string): void {
    this.onmessage?.({ data: raw });
  }

  fail(): void {
    this.onerror?.(new Event('error'));
  }
}

async function connectChannel(
  overrides: Partial<ConstructorParameters<typeof VoiceChannel>[0]> = {},
): Promise<{ channel: VoiceChannel; socket: FakeVoiceSocket }> {
  let socket: FakeVoiceSocket | undefined;
  const channel = new VoiceChannel({
    url: 'ws://localhost:8080/api/v1/ws/voice',
    sessionId: 'voice-session',
    webSocketFactory: (url) => {
      socket = new FakeVoiceSocket(url);
      return socket as unknown as WebSocket;
    },
    ...overrides,
  });
  const connectPromise = channel.connect();
  await vi.waitFor(() => expect(socket).toBeDefined());
  socket!.open();
  await connectPromise;
  return { channel, socket: socket! };
}

describe('VoiceChannel', () => {
  beforeEach(() => {
    FakeVoiceSocket.instances = [];
  });

  afterEach(() => {
    FakeVoiceSocket.instances = [];
    vi.unstubAllGlobals();
  });

  it('appends session_id, speaker and access_token handshake params to the endpoint url', async () => {
    const { socket } = await connectChannel({ speaker: 'agent', accessToken: 'tok-1' });
    const url = new URL(socket.url);
    expect(url.pathname).toBe('/api/v1/ws/voice');
    expect(url.searchParams.get('session_id')).toBe('voice-session');
    expect(url.searchParams.get('speaker')).toBe('agent');
    expect(url.searchParams.get('access_token')).toBe('tok-1');
  });

  it('omits optional handshake params when not configured', async () => {
    const { socket } = await connectChannel();
    const url = new URL(socket.url);
    expect(url.searchParams.get('speaker')).toBeNull();
    expect(url.searchParams.get('access_token')).toBeNull();
  });

  it('connect is idempotent while open: no second socket is created', async () => {
    const { channel } = await connectChannel();
    await channel.connect();
    expect(FakeVoiceSocket.instances).toHaveLength(1);
  });

  it('emits connected and disconnected lifecycle events', async () => {
    vi.stubGlobal('WebSocket', FakeVoiceSocket);
    const lifecycle: string[] = [];
    let socket: FakeVoiceSocket | undefined;
    const channel = new VoiceChannel({
      url: 'ws://localhost:8080/api/v1/ws/voice',
      sessionId: 's1',
      webSocketFactory: (url) => {
        socket = new FakeVoiceSocket(url);
        return socket as unknown as WebSocket;
      },
    });
    channel.on('connected', () => lifecycle.push('connected'));
    channel.on('disconnected', (reason) => lifecycle.push(`disconnected:${reason}`));

    const connectPromise = channel.connect();
    await vi.waitFor(() => expect(socket).toBeDefined());
    socket!.open();
    await connectPromise;
    socket!.close();

    expect(lifecycle).toEqual(['connected', 'disconnected:closed']);
    expect(channel.isConnected()).toBe(false);
  });

  it('rejects with transport_unavailable on socket error before open', async () => {
    vi.stubGlobal('WebSocket', FakeVoiceSocket);
    let socket: FakeVoiceSocket | undefined;
    const channel = new VoiceChannel({
      url: 'ws://localhost:8080/api/v1/ws/voice',
      sessionId: 's1',
      webSocketFactory: (url) => {
        socket = new FakeVoiceSocket(url);
        return socket as unknown as WebSocket;
      },
    });
    const connectPromise = channel.connect();
    await vi.waitFor(() => expect(socket).toBeDefined());
    await expect(async () => {
      socket!.fail();
      await connectPromise;
    }).rejects.toMatchObject({ code: 'transport_unavailable' });
  });

  it('throws when no WebSocket implementation is available', async () => {
    const channel = new VoiceChannel({ url: 'ws://localhost:8080/api/v1/ws/voice', sessionId: 's1' });
    vi.stubGlobal('WebSocket', undefined);
    await expect(channel.connect()).rejects.toThrow('WebSocket is not available');
  });

  it('sends binary audio chunks as-is and copies Uint8Array views to their own buffer', async () => {
    const { channel, socket } = await connectChannel();

    const plain = new ArrayBuffer(4);
    channel.sendAudio(plain);
    const view = new Uint8Array(new ArrayBuffer(6), 1, 4); // 偏移视图：上行前必须拷贝出有效区间
    channel.sendAudio(view);

    expect(socket.sent).toHaveLength(2);
    expect(socket.sent[0]).toBe(plain);
    expect(socket.sent[1]).toBeInstanceOf(ArrayBuffer);
    expect((socket.sent[1] as ArrayBuffer).byteLength).toBe(4);
  });

  it('throws transport_disconnected when sending audio while not connected', async () => {
    const { channel } = await connectChannel();
    channel.disconnect();
    try {
      channel.sendAudio(new ArrayBuffer(4));
      expect.unreachable('sendAudio should throw while disconnected');
    } catch (error) {
      expect(error).toMatchObject({ code: 'transport_disconnected' });
    }
  });

  it('ignores malformed downlink frames silently: bad json, missing type, non-object data, unknown type', async () => {
    const { channel, socket } = await connectChannel();
    const events: unknown[] = [];
    for (const name of ['voice:delta', 'voice:final', 'voice:audio', 'voice:error'] as const) {
      channel.on(name, (u) => events.push(u));
    }

    expect(() => socket.feedRaw('{not json')).not.toThrow();
    socket.feed({ data: { speaker: 'visitor' } }); // 缺 type
    socket.feed({ type: 'translation-delta', data: 'bare string' }); // data 非对象
    socket.feed({ type: 'voice-chimera', data: { speaker: 'visitor' } }); // 未知类型

    expect(events).toHaveLength(0);
  });

  it('drops voice frames missing their required fields, family by family', async () => {
    const { channel, socket } = await connectChannel();
    const events: unknown[] = [];
    for (const name of ['voice:delta', 'voice:final', 'voice:audio', 'voice:error'] as const) {
      channel.on(name, (u) => events.push(u));
    }

    socket.feed({ type: 'translation-delta', data: { speaker: 'visitor', turn_seq: 1 } }); // 缺 text
    socket.feed({ type: 'translation-delta', data: { speaker: 'visitor', text: 'x', turn_seq: '1' } }); // turn_seq 类型错
    socket.feed({
      type: 'translation-final',
      data: { speaker: 'visitor', seq: 1, original: 'o', content: 'c', source_lang: 'zh', target_lang: 'en' },
    }); // 缺 degraded
    socket.feed({ type: 'translation-final', data: { speaker: 'visitor', seq: 1, degraded: false } }); // 缺文本四元组
    socket.feed({ type: 'translation-audio', data: { speaker: 'visitor', seq: 1, format: 'mp3' } }); // 缺 audio
    socket.feed({ type: 'voice-error', data: { code: 'disabled' } }); // 缺 message

    expect(events).toHaveLength(0);
  });

  it('delivers fully-formed frames on their own events with wire-order payloads', async () => {
    const { channel, socket } = await connectChannel();
    const deltas: unknown[] = [];
    const finals: unknown[] = [];
    const audios: unknown[] = [];
    const errors: unknown[] = [];
    channel.on('voice:delta', (u) => deltas.push(u));
    channel.on('voice:final', (u) => finals.push(u));
    channel.on('voice:audio', (u) => audios.push(u));
    channel.on('voice:error', (u) => errors.push(u));

    socket.feed({ type: 'translation-delta', data: { speaker: 'agent', turn_seq: 3, text: '正在说' } });
    socket.feed({
      type: 'translation-final',
      data: {
        speaker: 'agent',
        seq: 2,
        original: '原',
        content: '译',
        source_lang: 'zh',
        target_lang: 'en',
        degraded: true,
      },
    });
    socket.feed({ type: 'translation-audio', data: { speaker: 'agent', seq: 2, format: 'mp3', audio: 'QQ==' } });
    socket.feed({ type: 'voice-error', data: { code: 'stream_broken', message: 'audio stream broken' } });

    expect(deltas).toEqual([{ speaker: 'agent', turn_seq: 3, text: '正在说' }]);
    expect(finals).toEqual([
      {
        speaker: 'agent',
        seq: 2,
        original: '原',
        content: '译',
        source_lang: 'zh',
        target_lang: 'en',
        degraded: true,
      },
    ]);
    expect(audios).toEqual([{ speaker: 'agent', seq: 2, format: 'mp3', audio: 'QQ==' }]);
    expect(errors).toEqual([{ code: 'stream_broken', message: 'audio stream broken' }]);
  });
});
