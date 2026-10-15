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
 * WebRTC 音轨面（PROTOCOL §9.3）：enableAudioTrack() 上行 webrtc-request，
 * 服务端发起 offer（服务端是媒体发送方），本类自动应答并经 voice:audio-track
 * 透出 PCMU 音轨；失败回 voice:webrtc-error（非致命，通道不收线）。
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
  /** RTCPeerConnection 工厂注入点（音轨面 §9.3）；缺省用 globalThis（非 DOM 宿主需自行注入）。 */
  peerConnectionFactory?: () => RTCPeerConnection;
  debug?: boolean;
}

export class VoiceChannel extends EventEmitter<ServifyVoiceEventMap> {
  private ws: WebSocket | null = null;
  private peer: RTCPeerConnection | null = null;
  private trackRequested = false;
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
    this.closePeer();
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  /**
   * 请求 WebRTC 音轨面（PROTOCOL §9.3）：上行 webrtc-request，服务端回
   * webrtc-offer 后自动应答并经 voice:audio-track 透出音轨。幂等：请求在途
   * 或音轨面已建时忽略。未连接时 throw transport_disconnected（同 sendAudio）。
   */
  enableAudioTrack(): void {
    if (this.trackRequested || this.peer) {
      return;
    }
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new ServifyError('Voice channel not connected', {
        code: 'transport_disconnected',
        retryable: true,
      });
    }
    this.trackRequested = true;
    this.sendFrame('webrtc-request', {});
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
      case 'webrtc-offer': {
        this.handleAudioTrackOffer(data);
        return;
      }
      case 'webrtc-candidate': {
        const candidate = data['candidate'];
        if (!candidate || typeof candidate !== 'object' || Array.isArray(candidate)) {
          this.log('忽略畸形 webrtc-candidate 载荷');
          return;
        }
        // 候选早于应答到达属正常（trickle）：注入失败不致命。
        void this.peer
          ?.addIceCandidate(candidate as RTCIceCandidateInit)
          .catch((err) => this.log('音轨候选注入失败:', err));
        return;
      }
      case 'webrtc-error': {
        const { code, message } = data;
        if (typeof code !== 'string' || typeof message !== 'string') {
          return;
        }
        this.emit('voice:webrtc-error', { code, message } satisfies VoiceErrorUpdate);
        return;
      }
      default:
        this.log('未知语音帧类型:', frame.type);
    }
  }

  private sendFrame(type: string, data: unknown): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      return;
    }
    this.ws.send(JSON.stringify({ type, data }));
  }

  /**
   * 音轨协商（§9.3）：服务端是媒体发送方，offer 由服务端发起——置远端描述、
   * 造应答、置本地描述后回 webrtc-answer；候选走 trickle 双向。任一步失败
   * 只收本地音轨面（通道不收线，译文音频回落 voice:audio）。
   */
  private handleAudioTrackOffer(data: Record<string, unknown>): void {
    const { type, sdp } = data;
    if (type !== 'offer' || typeof sdp !== 'string') {
      this.log('忽略畸形 webrtc-offer 载荷');
      return;
    }
    this.closePeer(); // 重协商先收旧面（正常流里每连接至多一次）
    const peer = this.createPeer();
    if (!peer) {
      return;
    }
    this.peer = peer;

    peer.ontrack = (event) => {
      this.emit('voice:audio-track', event.track);
    };
    peer.onicecandidate = (event) => {
      if (!event.candidate) {
        return; // 收尾空候选不上行
      }
      this.sendFrame('webrtc-candidate', event.candidate.toJSON());
    };

    void (async () => {
      await peer.setRemoteDescription({ type: 'offer', sdp });
      const answer = await peer.createAnswer();
      await peer.setLocalDescription(answer);
      const local = peer.localDescription ?? answer;
      this.sendFrame('webrtc-answer', local);
    })().catch((err) => {
      this.log('音轨协商失败:', err);
      this.closePeer();
      this.trackRequested = false; // 面已回收，允许重新 enableAudioTrack
    });
  }

  private createPeer(): RTCPeerConnection | null {
    if (this.options.peerConnectionFactory) {
      return this.options.peerConnectionFactory();
    }
    const PC = (globalThis as { RTCPeerConnection?: new () => RTCPeerConnection })
      .RTCPeerConnection;
    if (!PC) {
      this.log('RTCPeerConnection 不可用；请注入 peerConnectionFactory 以启用音轨面');
      return null;
    }
    return new PC();
  }

  private closePeer(): void {
    const peer = this.peer;
    this.peer = null;
    this.trackRequested = false;
    peer?.close();
  }

  private log(...args: unknown[]): void {
    if (this.options.debug) {
      console.warn('[ServifyVoice]', ...args);
    }
  }
}
