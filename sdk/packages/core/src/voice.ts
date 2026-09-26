import EventEmitter from 'eventemitter3';

import { ServifyError } from './contracts/errors';
import type { WebSocketFactory } from './types';
import type { VoiceAudioUpdate, VoiceDeltaUpdate, VoiceErrorUpdate, VoiceFinalUpdate } from './types';
import type { ServifyVoiceEventMap } from './types';

/**
 * 语音翻译通道客户端（PROTOCOL.md §9）：独立 WS 通道 `/api/v1/ws/voice` 的
 * 下行消费 + 二进制上行传输。与会话通道（WebSocketManager）零共享——端点、
 * 帧族、事件面都不同；麦克风采集（getUserMedia → pcm16 24kHz 单声道分片）
 * 是宿主/demo 层职责，本类只负责把分片按二进制帧送上去（sendAudio）。
 *
 * 无自动重连：语音流是活体采集会话，服务端无续传语义，`voice-error` 帧后
 * 必随 close（close 是流终止单一信号）——重启说话是用户显式动作，由宿主
 * 重新 connect。
 */
export interface VoiceChannelOptions {
  /** 语音通道端点，如 `wss://host/api/v1/ws/voice`（query 参数由本类追加）。 */
  url: string;
  sessionId: string;
  /** 说话方；缺省 `visitor`（服务端大小写不敏感）。 */
  speaker?: 'visitor' | 'agent';
  /** 握手附带访问令牌（`guest_token.required` 开启时服务端校验，PROTOCOL §9）。 */
  accessToken?: string;
  /** WebSocket 工厂注入点；缺省用 globalThis.WebSocket（非 DOM 宿主需自行注入）。 */
  webSocketFactory?: WebSocketFactory;
  debug?: boolean;
}

export class VoiceChannel extends EventEmitter<ServifyVoiceEventMap> {
  private ws: WebSocket | null = null;
  private readonly options: VoiceChannelOptions;

  constructor(options: VoiceChannelOptions) {
    super();
    this.options = options;
  }

  isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  async connect(): Promise<void> {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      return;
    }

    return new Promise((resolve, reject) => {
      const url = this.buildUrl();
      this.log('正在连接语音通道...', url);
      try {
        this.ws = this.createSocket(url);
      } catch (error) {
        reject(error);
        return;
      }

      this.ws.onopen = () => {
        this.log('语音通道连接成功');
        this.emit('connected');
        resolve();
      };

      this.ws.onmessage = (event) => {
        this.handleFrame(event.data);
      };

      this.ws.onclose = (event) => {
        this.log('语音通道连接关闭:', event.code, event.reason);
        this.ws = null;
        this.emit('disconnected', event.reason || '连接关闭');
      };

      this.ws.onerror = () => {
        const err = new ServifyError('Voice channel connection error', {
          code: 'transport_unavailable',
          retryable: true,
          details: { url: this.options.url },
        });
        this.emit('error', err);
        reject(err);
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
   * PROTOCOL §9）。未连接时 throw retryable 的 transport_disconnected——
   * 采集侧据此暂停上行，连接恢复后由宿主重新发起。
   */
  sendAudio(chunk: ArrayBuffer | Uint8Array): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new ServifyError('Voice channel not connected', {
        code: 'transport_disconnected',
        retryable: true,
      });
    }
    const payload = chunk instanceof Uint8Array ? chunk.buffer.slice(chunk.byteOffset, chunk.byteOffset + chunk.byteLength) : chunk;
    this.ws.send(payload);
  }

  private buildUrl(): string {
    const url = new URL(this.options.url);
    url.searchParams.set('session_id', this.options.sessionId);
    if (this.options.speaker) {
      url.searchParams.set('speaker', this.options.speaker);
    }
    if (this.options.accessToken) {
      url.searchParams.set('access_token', this.options.accessToken);
    }
    return url.toString();
  }

  /** 构造通道 socket：注入工厂优先（非 DOM 宿主），缺省运行时探测 globalThis。 */
  private createSocket(url: string): WebSocket {
    if (this.options.webSocketFactory) {
      return this.options.webSocketFactory(url);
    }
    const WS = (globalThis as { WebSocket?: new (url: string, protocols?: string | string[]) => WebSocket })
      .WebSocket;
    if (!WS) {
      throw new Error(
        'WebSocket is not available in this environment; provide options.webSocketFactory to supply one.',
      );
    }
    return new WS(url);
  }

  /**
   * 下行分发（PROTOCOL.md §9.1）：必需字段校验失败静默忽略（字幕是流式
   * 语义，畸形帧不值得报错打断渲染）；voice-error 事件后服务端必随 close。
   */
  private handleFrame(raw: unknown): void {
    let frame: { type?: unknown; data?: unknown };
    try {
      frame = JSON.parse(String(raw)) as { type?: unknown; data?: unknown };
    } catch {
      this.log('解析语音帧失败');
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
        this.emit('voice:delta', { speaker, turn_seq, text } satisfies VoiceDeltaUpdate);
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
        this.emit('voice:final', {
          speaker,
          seq,
          original,
          content,
          source_lang,
          target_lang,
          degraded,
        } satisfies VoiceFinalUpdate);
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
        this.emit('voice:audio', { speaker, seq, format, audio } satisfies VoiceAudioUpdate);
        return;
      }
      case 'voice-error': {
        const { code, message } = data;
        if (typeof code !== 'string' || typeof message !== 'string') {
          return;
        }
        this.emit('voice:error', { code, message } satisfies VoiceErrorUpdate);
        return;
      }
      default:
        this.log('未知语音帧类型:', frame.type);
    }
  }

  private log(...args: unknown[]): void {
    if (this.options.debug) {
      console.warn('[ServifyVoice]', ...args);
    }
  }
}
