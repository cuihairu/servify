import { describe, expect, it, vi } from 'vitest';

import {
  MicCapture,
  VOICE_SAMPLE_RATE,
  floatToPcm16,
  pcm16ToLeBytes,
  resampleLinear,
} from './voice-capture';

/**
 * MicCapture 单测：编码/重采样纯函数的数值面 + 采集图接线的分支面
 * （注入 ports 替身，对齐 voice.test.ts 的 Fake* 口径）。ScriptProcessor
 * 回调是浏览器主线程事件，测试直接调用替身的 onaudioprocess 驱动管线。
 */

// ── floatToPcm16 ───────────────────────────────────────────────

describe('floatToPcm16', () => {
  it('quantizes nominal values with rounding', () => {
    // JS Math.round(-0.5 * 32767) = Math.round(-16383.5) = -16383 (rounds toward +inf for .5)
    expect(Array.from(floatToPcm16(Float32Array.from([0, 1, -1, 0.5, -0.5])))).toEqual([
      0, 32767, -32767, 16384, -16383,
    ]);
  });

  it('clamps out-of-range and NaN samples', () => {
    expect(Array.from(floatToPcm16(Float32Array.from([2, -2, Number.NaN])))).toEqual([
      32767, -32767, -32767,
    ]);
  });

  it('returns empty output for empty input', () => {
    expect(floatToPcm16(new Float32Array(0)).length).toBe(0);
  });
});

// ── resampleLinear ─────────────────────────────────────────────

describe('resampleLinear', () => {
  it('returns the same array when rates match', () => {
    const input = Int16Array.from([1, 2, 3]);
    expect(resampleLinear(input, 24000, 24000)).toBe(input);
  });

  it('returns the same array for empty input', () => {
    const input = new Int16Array(0);
    expect(resampleLinear(input, 48000, 24000)).toBe(input);
  });

  it('downsamples by an even ratio exactly', () => {
    // 48k → 24k：ratio=2，整点位取样（pos=0,2）
    expect(Array.from(resampleLinear(Int16Array.from([100, -7, 200, -9]), 48000, 24000))).toEqual([
      100, 200,
    ]);
  });

  it('interpolates at fractional positions for odd ratios', () => {
    // 3k → 2k：ratio=1.5；pos(1)=1.5 → (100+200)/2=150
    expect(Array.from(resampleLinear(Int16Array.from([0, 100, 200]), 3000, 2000))).toEqual([0, 150]);
  });

  it('upsamples holding the last sample at the tail', () => {
    // 8k → 16k：ratio=0.5，outLen=2；pos(1)=0.5 → 100*0.5+100*0.5=100
    expect(Array.from(resampleLinear(Int16Array.from([100]), 8000, 16000))).toEqual([100, 100]);
  });

  it('rejects non-positive sample rates', () => {
    expect(() => resampleLinear(Int16Array.from([1]), 0, 24000)).toThrow(RangeError);
    expect(() => resampleLinear(Int16Array.from([1]), 48000, 0)).toThrow(RangeError);
  });
});

// ── pcm16ToLeBytes ─────────────────────────────────────────────

describe('pcm16ToLeBytes', () => {
  it('packs little-endian explicitly regardless of platform order', () => {
    expect(Array.from(pcm16ToLeBytes(Int16Array.from([1, 256, -1, -32768])))).toEqual([
      0x01, 0x00, 0x00, 0x01, 0xff, 0xff, 0x00, 0x80,
    ]);
  });

  it('returns empty bytes for empty input', () => {
    expect(pcm16ToLeBytes(new Int16Array(0)).length).toBe(0);
  });
});

// ── 采集图替身 ─────────────────────────────────────────────────

interface FakeTrack {
  stop: ReturnType<typeof vi.fn>;
}

class FakeMediaStream {
  readonly tracks: FakeTrack[] = [];
  getTracks(): FakeTrack[] {
    return this.tracks;
  }
}

class FakeAudioContext {
  static sampleRate = 48000;

  readonly sampleRate = FakeAudioContext.sampleRate;
  readonly destination = {};
  readonly closed = vi.fn(async (): Promise<void> => undefined);
  readonly source: { connect: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn> };
  readonly processor: {
    onaudioprocess: ((event: AudioProcessingEvent) => void) | null;
    connect: ReturnType<typeof vi.fn>;
    disconnect: ReturnType<typeof vi.fn>;
  };
  readonly mute: { gain: { value: number }; connect: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn> };

  constructor() {
    this.source = { connect: vi.fn(), disconnect: vi.fn() };
    this.processor = { onaudioprocess: null, connect: vi.fn(), disconnect: vi.fn() };
    this.mute = { gain: { value: 1 }, connect: vi.fn(), disconnect: vi.fn() };
  }

  createMediaStreamSource() {
    return this.source;
  }

  createScriptProcessor() {
    return this.processor;
  }

  createGain() {
    return this.mute;
  }

  close() {
    return this.closed();
  }

  /** 测试驱动：以给定输入帧触发一次 ScriptProcessor 回调。 */
  dispatch(input: Float32Array): void {
    if (!this.processor.onaudioprocess) {
      throw new Error('onaudioprocess is not wired');
    }
    this.processor.onaudioprocess({
      inputBuffer: { getChannelData: () => input },
    } as unknown as AudioProcessingEvent);
  }
}

interface CapturedGraph {
  stream: FakeMediaStream;
  ctx: FakeAudioContext;
}

function makePorts(stream: FakeMediaStream, context: FakeAudioContext) {
  return {
    getUserMedia: vi.fn(async (): Promise<MediaStream> => stream as unknown as MediaStream),
    audioContextFactory: vi.fn((): AudioContext => context as unknown as AudioContext),
  };
}

async function startCapture(overrides: {
  ports?: { getUserMedia: (constraints: MediaStreamConstraints) => Promise<MediaStream>; audioContextFactory: () => AudioContext };
  targetSampleRate?: number;
}): Promise<{ capture: MicCapture; graph: CapturedGraph; chunks: Uint8Array[] }> {
  const graph: CapturedGraph = {
    stream: new FakeMediaStream(),
    ctx: new FakeAudioContext(),
  };
  const capture = new MicCapture({
    ...(overrides.targetSampleRate !== undefined ? { targetSampleRate: overrides.targetSampleRate } : {}),
    ports: overrides.ports ?? makePorts(graph.stream, graph.ctx),
  });
  const chunks: Uint8Array[] = [];
  await capture.start((chunk) => chunks.push(chunk));
  return { capture, graph, chunks };
}

// ── MicCapture 接线 ────────────────────────────────────────────

describe('MicCapture', () => {
  it('wires source → processor → zero-gain mute → destination and reports active', async () => {
    const { capture, graph } = await startCapture({});
    expect(capture.isActive()).toBe(true);
    expect(graph.ctx.processor.connect).toHaveBeenCalledWith(graph.ctx.mute);
    expect(graph.ctx.mute.connect).toHaveBeenCalledWith(graph.ctx.destination);
    expect(graph.ctx.mute.gain.value).toBe(0);
    await capture.stop();
    expect(capture.isActive()).toBe(false);
  });

  it('delivers 24kHz little-endian pcm16 chunks through the pipeline', async () => {
    const { capture, graph, chunks } = await startCapture({});
    // 4096 samples @48kHz → 2048 samples @24kHz → 4096 字节
    // ratio=2：resampleLinear 取 pos=0, 2, 4... 即 input[0], input[2], input[4]...
    const input = new Float32Array(4096);
    input[0] = 1;     // → 32767 → 0xff 0x7f
    input[2] = 0.5;   // → 16384 → 0x00 0x40
    graph.ctx.dispatch(input);
    expect(chunks.length).toBe(1);
    expect(chunks[0].length).toBe(4096);
    // 首两个重采样样本经 LE 打包：[32767(1.0), 16384(0.5)]
    expect(Array.from(chunks[0].subarray(0, 4))).toEqual([0xff, 0x7f, 0x00, 0x40]);
    await capture.stop();
  });

  it('resamples from the context sample rate (odd ratio keeps length floor)', async () => {
    const ports = makePorts(new FakeMediaStream(), new FakeAudioContext());
    const oddCtx = new FakeAudioContext();
    Object.defineProperty(oddCtx, 'sampleRate', { value: 44100 });
    const oddPorts = {
      getUserMedia: ports.getUserMedia,
      audioContextFactory: vi.fn((): AudioContext => oddCtx as unknown as AudioContext),
    };
    const { capture, chunks } = await startCapture({ ports: oddPorts });
    oddCtx.dispatch(new Float32Array(4096));
    // floor(4096 / (44100/24000)) = floor(2229.66) = 2229
    expect(chunks[0].length).toBe(2229 * 2);
    await capture.stop();
  });

  it('skips empty chunks after resampling', async () => {
    const { capture, graph, chunks } = await startCapture({});
    graph.ctx.dispatch(new Float32Array(1)); // 1 sample → floor(1/2)=0 帧
    expect(chunks.length).toBe(0);
    await capture.stop();
  });

  it('maps permission denial to capture_denied and never builds the graph', async () => {
    const context = new FakeAudioContext();
    const ports = {
      getUserMedia: vi.fn(async (): Promise<MediaStream> => {
        const err = new Error('denied');
        err.name = 'NotAllowedError';
        throw err;
      }),
      audioContextFactory: vi.fn((): AudioContext => context as unknown as AudioContext),
    };
    const capture = new MicCapture({ ports });
    await expect(capture.start(() => {})).rejects.toMatchObject({
      code: 'capture_denied',
      retryable: false,
    });
    expect(ports.audioContextFactory).not.toHaveBeenCalled();
  });

  it('maps device failures to retryable capture_unavailable', async () => {
    const ports = {
      getUserMedia: vi.fn(async (): Promise<MediaStream> => {
        const err = new Error('no device');
        err.name = 'NotFoundError';
        throw err;
      }),
      audioContextFactory: vi.fn((): AudioContext => new FakeAudioContext() as unknown as AudioContext),
    };
    const capture = new MicCapture({ ports });
    await expect(capture.start(() => {})).rejects.toMatchObject({
      code: 'capture_unavailable',
      retryable: true,
    });
  });

  it('reports capture_unavailable when neither ports nor globalThis provide getUserMedia', async () => {
    const capture = new MicCapture(); // 无 ports；测试 Node 环境无 navigator.mediaDevices
    await expect(capture.start(() => {})).rejects.toMatchObject({ code: 'capture_unavailable' });
  });

  it('rejects a second start while active', async () => {
    const { capture } = await startCapture({});
    await expect(capture.start(() => {})).rejects.toMatchObject({ code: 'capture_already_active' });
    await capture.stop();
  });

  it('stop tears down tracks, graph and context', async () => {
    const { capture, graph } = await startCapture({});
    const track = { stop: vi.fn() };
    graph.stream.tracks.push(track);
    await capture.stop();
    expect(track.stop).toHaveBeenCalled();
    expect(graph.ctx.processor.disconnect).toHaveBeenCalled();
    expect(graph.ctx.source.disconnect).toHaveBeenCalled();
    expect(graph.ctx.mute.disconnect).toHaveBeenCalled();
    expect(graph.ctx.closed).toHaveBeenCalled();
    expect(graph.ctx.processor.onaudioprocess).toBeNull();
  });

  it('stop is idempotent and swallows context close failures', async () => {
    const ports = makePorts(new FakeMediaStream(), new FakeAudioContext());
    const brokenCtx = new FakeAudioContext();
    brokenCtx.closed.mockRejectedValueOnce(new Error('already closed'));
    const brokenPorts = {
      getUserMedia: ports.getUserMedia,
      audioContextFactory: vi.fn((): AudioContext => brokenCtx as unknown as AudioContext),
    };
    const { capture } = await startCapture({ ports: brokenPorts });
    await expect(capture.stop()).resolves.toBeUndefined();
    await expect(capture.stop()).resolves.toBeUndefined();
  });

  it('restarts cleanly after stop', async () => {
    const { capture, graph } = await startCapture({});
    await capture.stop();
    graph.ctx.closed.mockClear();
    const ports = makePorts(new FakeMediaStream(), new FakeAudioContext());
    const second = new MicCapture({ ports });
    const chunks: Uint8Array[] = [];
    await second.start((chunk) => chunks.push(chunk));
    expect(second.isActive()).toBe(true);
    expect(chunks.length).toBe(0);
    await second.stop();
  });

  it('defaults the target rate to the protocol constant', () => {
    expect(VOICE_SAMPLE_RATE).toBe(24000);
  });
});
