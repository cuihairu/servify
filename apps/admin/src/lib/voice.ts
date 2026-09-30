/**
 * 管理端坐席语音实时翻译通道（PROTOCOL.md §9）：麦克风采集 → pcm16 24kHz
 * 单声道二进制上行 + 下行四帧（translation-delta/final/audio、voice-error）
 * 消费。纯前端自包含实现，无新增协议面——帧型、载荷校验、端点与会话 WS
 * 的关系全部按 PROTOCOL §9 既有契约（数学函数与事件校验语义同
 * sdk/packages/core 的 voice-capture.ts / voice.ts 同口径，管理端不依赖
 * sdk 工作区故按同口径自带；两处漂移由仓内 scripts/admin_voice_protocol_
 * surface_test.go 守卫钉住）。
 *
 * 无自动重连：语音流是活体采集会话，voice-error 帧后必随 close（close 是
 * 流终止单一信号），重启说话是用户显式动作——由页面重新 connect。
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

/** 语音面错误（code 与 core ServifyError 同词表，管理端不带 sdk 错误类型）。 */
export class VoiceEntryError extends Error {
  readonly code: string;
  readonly retryable: boolean;

  constructor(message: string, code: string, retryable: boolean) {
    super(message);
    this.name = 'VoiceEntryError';
    this.code = code;
    this.retryable = retryable;
  }
}

/** getUserMedia 失败归一：权限拒绝 → capture_denied（不可重试）；其余 → capture_unavailable（可重试）。 */
function mapCaptureError(error: unknown): VoiceEntryError {
  const name = (error as { name?: string } | null)?.name;
  if (name === 'NotAllowedError' || name === 'SecurityError') {
    return new VoiceEntryError('麦克风权限被拒绝', 'capture_denied', false);
  }
  return new VoiceEntryError('麦克风不可用', 'capture_unavailable', true);
}

/** 一段麦克风采集会话：start(onChunk) 持续产出小端 pcm16 分片，stop() 收线。 */
export class MicCapture {
  private active = false;
  private starting = false;
  private generation = 0;
  private stream: MediaStream | null = null;
  private ctx: AudioContext | null = null;
  private source: MediaStreamAudioSourceNode | null = null;
  private processor: ScriptProcessorNode | null = null;
  private mute: GainNode | null = null;

  isActive(): boolean {
    return this.active;
  }

  async start(onChunk: (pcmLe: Uint8Array) => void): Promise<void> {
    if (this.active || this.starting) {
      throw new VoiceEntryError('麦克风采集中已激活', 'capture_already_active', false);
    }
    const g = (globalThis as {
      navigator?: { mediaDevices?: { getUserMedia?: (c: MediaStreamConstraints) => Promise<MediaStream> } };
    }).navigator?.mediaDevices?.getUserMedia;
    const AC = (globalThis as { AudioContext?: new () => AudioContext }).AudioContext;
    if (!g || !AC) {
      throw new VoiceEntryError('当前环境不支持麦克风采集', 'capture_unavailable', false);
    }
    const gen = ++this.generation;
    this.starting = true;
    try {
      let stream: MediaStream;
      try {
        stream = await g.call(globalThis.navigator.mediaDevices, { audio: true });
      } catch (error) {
        throw mapCaptureError(error);
      }
      if (gen !== this.generation) {
        // start 途中被 stop：只停刚拿到的轨，绝不接线。
        stream.getTracks().forEach((track) => track.stop());
        return;
      }

      // 采集图：ScriptProcessor 必须接入渲染图才有回调，零增益 GainNode 避免
      // 回声；不选 AudioWorklet——processor 模块经 Blob URL 加载会被服务端
      // default-src 'self' CSP 拦截（与 core 同款选型理由，PROTOCOL §9）。
      const ctx = new AC();
      const source = ctx.createMediaStreamSource(stream);
      const processor = ctx.createScriptProcessor(4096, 1, 1);
      const mute = ctx.createGain();
      mute.gain.value = 0;

      processor.onaudioprocess = (event: AudioProcessingEvent) => {
        const input = event.inputBuffer.getChannelData(0);
        const pcm = resampleLinear(floatToPcm16(input), ctx.sampleRate, VOICE_SAMPLE_RATE);
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
    } finally {
      this.starting = false;
    }
  }

  /** 收线：作废在途 start → 停轨 → 断图 → 关上下文。幂等；close 失败不阻断收尾。 */
  async stop(): Promise<void> {
    this.generation++;
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
      } catch {
        // AudioContext 收尾异常忽略（会话已废弃）。
      }
    }
  }
}

/** 下行帧载荷（线序 snake_case 直透，PROTOCOL §9 载荷表）。 */
export interface VoiceDeltaUpdate {
  speaker: string;
  turn_seq: number;
  text: string;
}

export interface VoiceFinalUpdate {
  speaker: string;
  seq: number;
  original: string;
  content: string;
  source_lang: string;
  target_lang: string;
  degraded: boolean;
}

export interface VoiceAudioUpdate {
  speaker: string;
  seq: number;
  format: string;
  audio: string;
}

export interface VoiceErrorUpdate {
  code: string;
  message: string;
}

export type VoiceSpeaker = 'visitor' | 'agent';

export type VoiceEntryEvent =
  | 'connected'
  | 'disconnected'
  | 'voice:delta'
  | 'voice:final'
  | 'voice:audio'
  | 'voice:error';

type VoiceEntryHandlers = {
  connected?: () => void;
  disconnected?: (reason: string) => void;
  'voice:delta'?: (u: VoiceDeltaUpdate) => void;
  'voice:final'?: (u: VoiceFinalUpdate) => void;
  'voice:audio'?: (u: VoiceAudioUpdate) => void;
  'voice:error'?: (u: VoiceErrorUpdate) => void;
};

/** 管理端语音 WS 端点（与会话通道同源推导 ws/wss，PROTOCOL §9）。 */
export function buildVoiceWebSocketURL(sessionId: string): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${window.location.host}/api/v1/ws/voice?session_id=${encodeURIComponent(sessionId)}&speaker=agent`;
}

/**
 * 语音翻译通道客户端（PROTOCOL.md §9）：独立 WS 通道下行消费 + 二进制上行。
 * 下行必需字段校验失败静默忽略（字幕是流式语义，畸形帧不值得报错打断渲染）；
 * voice-error 事件后服务端必随 close。
 */
export class VoiceChannel {
  private ws: WebSocket | null = null;
  private readonly sessionId: string;
  private readonly accessToken?: string;
  private readonly handlers: VoiceEntryHandlers = {};

  constructor(options: { sessionId: string; accessToken?: string }) {
    this.sessionId = options.sessionId;
    this.accessToken = options.accessToken;
  }

  on<K extends VoiceEntryEvent>(event: K, handler: VoiceEntryHandlers[K]): void {
    this.handlers[event] = handler;
  }

  isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  connect(): Promise<void> {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      return Promise.resolve();
    }
    return new Promise((resolve, reject) => {
      let url = buildVoiceWebSocketURL(this.sessionId);
      if (this.accessToken) {
        // guest_token.required 部署的握手令牌口（PROTOCOL §9）；管理端默认
        // 无 guest token，与远程协助信令 WS 同口径不带（既有部署边界）。
        url += `&access_token=${encodeURIComponent(this.accessToken)}`;
      }
      let ws: WebSocket;
      try {
        ws = new WebSocket(url);
      } catch (error) {
        reject(error);
        return;
      }
      this.ws = ws;
      ws.onopen = () => {
        this.handlers.connected?.();
        resolve();
      };
      ws.onmessage = (event) => {
        this.handleFrame(event.data);
      };
      ws.onclose = (event) => {
        this.ws = null;
        this.handlers.disconnected?.(event.reason || '连接关闭');
      };
      ws.onerror = () => {
        // 握手被 401/503 拒绝时浏览器只给 onerror，无状态码细节——文案由
        // 页面层按"服务端未装配语音翻译或令牌无效"口径提示。
        reject(new VoiceEntryError('语音通道连接失败', 'transport_unavailable', true));
      };
    });
  }

  disconnect(): void {
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  /**
   * 二进制上行：pcm16 24kHz 单声道原始音频分片（20–100ms 级，单帧上限 64KB，
   * PROTOCOL §9）。未连接时 throw——采集侧据此收线，由用户重新发起。
   */
  sendAudio(chunk: Uint8Array): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new VoiceEntryError('语音通道未连接', 'transport_disconnected', true);
    }
    this.ws.send(chunk.buffer.slice(chunk.byteOffset, chunk.byteOffset + chunk.byteLength));
  }

  /**
   * 下行分发（PROTOCOL.md §9.1）：必需字段校验失败静默忽略；未知类型忽略
   * （服务端 additive 加帧时旧客户端不受影响）。
   */
  private handleFrame(raw: unknown): void {
    let frame: { type?: unknown; data?: unknown };
    try {
      frame = JSON.parse(String(raw)) as { type?: unknown; data?: unknown };
    } catch {
      return;
    }
    if (typeof frame.type !== 'string' || typeof frame.data !== 'object' || frame.data === null) {
      return;
    }
    const data = frame.data as Record<string, unknown>;

    switch (frame.type) {
      case 'translation-delta': {
        const { speaker, turn_seq, text } = data;
        if (typeof speaker !== 'string' || typeof turn_seq !== 'number' || typeof text !== 'string') {
          return;
        }
        this.handlers['voice:delta']?.({ speaker, turn_seq, text });
        return;
      }
      case 'translation-final': {
        const { speaker, seq, original, content, source_lang, target_lang, degraded } = data;
        if (
          typeof speaker !== 'string' ||
          typeof seq !== 'number' ||
          typeof original !== 'string' ||
          typeof content !== 'string' ||
          typeof source_lang !== 'string' ||
          typeof target_lang !== 'string' ||
          typeof degraded !== 'boolean'
        ) {
          return;
        }
        this.handlers['voice:final']?.({ speaker, seq, original, content, source_lang, target_lang, degraded });
        return;
      }
      case 'translation-audio': {
        const { speaker, seq, format, audio } = data;
        if (
          typeof speaker !== 'string' ||
          typeof seq !== 'number' ||
          typeof format !== 'string' ||
          typeof audio !== 'string'
        ) {
          return;
        }
        this.handlers['voice:audio']?.({ speaker, seq, format, audio });
        return;
      }
      case 'voice-error': {
        const { code, message } = data;
        if (typeof code !== 'string' || typeof message !== 'string') {
          return;
        }
        this.handlers['voice:error']?.({ code, message });
        return;
      }
      default:
        return;
    }
  }
}
