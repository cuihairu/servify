import { ServifyError } from './contracts/errors';

/**
 * 麦克风采集 → pcm16 24kHz 单声道分片（PROTOCOL.md §9 上行形态）。产出直接
 * 可喂 `VoiceChannel.sendAudio`（宿主自行接线 `onChunk → sendAudio`，两解耦
 * 便于在中间插入限流/静音门控）。
 *
 * 采集图：getUserMedia → MediaStreamSource → ScriptProcessorNode → 零增益
 * GainNode → destination（ScriptProcessor 必须接入渲染图才有回调；零增益
 * 避免回声/啸叫）。选 ScriptProcessor 而非 AudioWorklet：AudioWorklet 的
 * processor 模块经 Blob URL 加载，会被服务端 `default-src 'self'` CSP 拦截
 * （blob: 不在 script-src 内）；ScriptProcessor 无模块加载、全浏览器可用，
 * 代价仅是主线程回调（已弃用但各厂商无移除时间表，AudioWorklet 升级是采集
 * 图半边的直换）。重采样在主线程经下方纯函数完成——全部可测。
 */
/** 上行目标采样率（PROTOCOL §9：pcm16 24kHz 单声道）。 */
export const VOICE_SAMPLE_RATE = 24000;

/** float 采样 → pcm16：clamp [-1,1] 后线性量化（四舍五入；NaN 归 -1）。 */
export function floatToPcm16(input: Float32Array): Int16Array {
  const out = new Int16Array(input.length);
  for (let i = 0; i < input.length; i++) {
    let s = input[i];
    if (!(s >= -1)) {
      s = -1; // NaN 与 < -1 同落 -1
    } else if (s > 1) {
      s = 1;
    }
    out[i] = Math.round(s * 32767);
  }
  return out;
}

/**
 * 线性插值重采样（int16 域——语音上行精度足够；from==to 或空输入原样返回，
 * 不复制）。采样率须为正数。
 */
export function resampleLinear(input: Int16Array, fromRate: number, toRate: number): Int16Array {
  if (!(fromRate > 0) || !(toRate > 0)) {
    throw new RangeError('resampleLinear: sample rates must be positive');
  }
  if (fromRate === toRate || input.length === 0) {
    return input;
  }
  const ratio = fromRate / toRate;
  const outLen = Math.floor(input.length / ratio);
  const out = new Int16Array(outLen);
  for (let i = 0; i < outLen; i++) {
    const pos = i * ratio;
    const i0 = Math.floor(pos);
    const i1 = Math.min(i0 + 1, input.length - 1);
    const frac = pos - i0;
    out[i] = Math.round(input[i0] * (1 - frac) + input[i1] * frac);
  }
  return out;
}

/** pcm16 → 小端字节序线上格式（DataView 显式 LE，平台序无关）。 */
export function pcm16ToLeBytes(input: Int16Array): Uint8Array {
  const out = new Uint8Array(input.length * 2);
  const view = new DataView(out.buffer);
  for (let i = 0; i < input.length; i++) {
    view.setInt16(i * 2, input[i], true);
  }
  return out;
}

/** 宿主注入点：非安全上下文/非 DOM 宿主缺省实现不可用，测试注入替身。 */
export interface MicCapturePorts {
  getUserMedia?: (constraints: MediaStreamConstraints) => Promise<MediaStream>;
  audioContextFactory?: () => AudioContext;
}

export interface MicCaptureOptions {
  /** 目标采样率；缺省 {@link VOICE_SAMPLE_RATE}。 */
  targetSampleRate?: number;
  /** 采集注入点；缺省运行时探测 globalThis（见 start）。 */
  ports?: MicCapturePorts;
  debug?: boolean;
}

/**
 * 一段麦克风采集会话。生命周期：`start(onChunk)` 建图起流 → onChunk 持续
 * 收到小端 pcm16 分片 → `stop()` 收线（停轨、断图、关 AudioContext）。
 * 不可复用错误以 ServifyError 透出：权限拒绝 capture_denied、设备缺失/无
 * getUserMedia capture_unavailable、重复 start capture_already_active。
 */
export class MicCapture {
  private readonly options: MicCaptureOptions;
  private active = false;
  private stream: MediaStream | null = null;
  private ctx: AudioContext | null = null;
  private source: MediaStreamAudioSourceNode | null = null;
  private processor: ScriptProcessorNode | null = null;
  private mute: GainNode | null = null;

  constructor(options: MicCaptureOptions = {}) {
    this.options = options;
  }

  isActive(): boolean {
    return this.active;
  }

  async start(onChunk: (pcmLe: Uint8Array) => void): Promise<void> {
    if (this.active) {
      throw new ServifyError('Mic capture already active', {
        code: 'capture_already_active',
        retryable: false,
      });
    }
    const getUserMedia = this.resolveGetUserMedia();
    const createCtx = this.resolveAudioContextFactory();

    let stream: MediaStream;
    try {
      stream = await getUserMedia({ audio: true });
    } catch (error) {
      throw mapCaptureError(error);
    }

    const ctx = createCtx();
    const source = ctx.createMediaStreamSource(stream);
    const processor = ctx.createScriptProcessor(4096, 1, 1);
    const mute = ctx.createGain();
    mute.gain.value = 0;

    processor.onaudioprocess = (event: AudioProcessingEvent) => {
      const input = event.inputBuffer.getChannelData(0);
      const pcm = resampleLinear(floatToPcm16(input), ctx.sampleRate, this.targetRate());
      if (pcm.length > 0) {
        onChunk(pcm16ToLeBytes(pcm));
      }
    };
    source.connect(processor);
    processor.connect(mute);
    mute.connect(ctx.destination);

    this.stream = stream;
    this.ctx = ctx;
    this.source = source;
    this.processor = processor;
    this.mute = mute;
    this.active = true;
    this.log('麦克风采集已启动', ctx.sampleRate, '→', this.targetRate(), 'Hz');
  }

  /** 收线：停轨 → 断图 → 关上下文。幂等；close 失败不阻断收尾。 */
  async stop(): Promise<void> {
    this.active = false;
    if (this.processor) {
      this.processor.onaudioprocess = null;
      this.processor.disconnect();
      this.processor = null;
    }
    if (this.source) {
      this.source.disconnect();
      this.source = null;
    }
    if (this.mute) {
      this.mute.disconnect();
      this.mute = null;
    }
    if (this.stream) {
      this.stream.getTracks().forEach((track) => track.stop());
      this.stream = null;
    }
    if (this.ctx) {
      const ctx = this.ctx;
      this.ctx = null;
      try {
        await ctx.close();
      } catch (error) {
        this.log('AudioContext 收尾异常（忽略）:', error);
      }
    }
    this.log('麦克风采集已停止');
  }

  private targetRate(): number {
    return this.options.targetSampleRate ?? VOICE_SAMPLE_RATE;
  }

  private resolveGetUserMedia(): (constraints: MediaStreamConstraints) => Promise<MediaStream> {
    if (this.options.ports?.getUserMedia) {
      return this.options.ports.getUserMedia;
    }
    const g = (globalThis as {
      navigator?: { mediaDevices?: { getUserMedia?: (c: MediaStreamConstraints) => Promise<MediaStream> } };
    }).navigator?.mediaDevices?.getUserMedia;
    if (!g) {
      throw new ServifyError(
        'getUserMedia is not available in this environment; provide options.ports.getUserMedia.',
        { code: 'capture_unavailable', retryable: false },
      );
    }
    return g.bind(globalThis.navigator.mediaDevices);
  }

  private resolveAudioContextFactory(): () => AudioContext {
    if (this.options.ports?.audioContextFactory) {
      return this.options.ports.audioContextFactory;
    }
    const AC = (globalThis as { AudioContext?: new () => AudioContext }).AudioContext;
    if (!AC) {
      throw new ServifyError(
        'AudioContext is not available in this environment; provide options.ports.audioContextFactory.',
        { code: 'capture_unavailable', retryable: false },
      );
    }
    return () => new AC();
  }

  private log(...args: unknown[]): void {
    if (this.options.debug) {
      console.warn('[ServifyVoiceCapture]', ...args);
    }
  }
}

/**
 * getUserMedia 失败归一：权限拒绝（NotAllowedError/SecurityError）→
 * capture_denied（不可重试，须用户在浏览器授权面板放行）；其余（设备缺失、
 * 硬件占用、未知）→ capture_unavailable（可重试）。
 */
function mapCaptureError(error: unknown): ServifyError {
  const name = (error as { name?: string } | null)?.name;
  if (name === 'NotAllowedError' || name === 'SecurityError') {
    return new ServifyError('Microphone permission denied', {
      code: 'capture_denied',
      retryable: false,
      details: { cause: name ?? String(error) },
    });
  }
  return new ServifyError('Microphone unavailable', {
    code: 'capture_unavailable',
    retryable: true,
    details: { cause: name ?? String(error) },
  });
}
