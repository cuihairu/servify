import EventEmitter from 'eventemitter3';
import type { AuthProvider } from './contracts/auth-provider';
import { ServifyError } from './contracts/errors';
import {
  computeReconnectDelay,
  normalizeReconnectPolicy,
  shouldReconnect,
} from './contracts/reconnect';
import type { Transport, TransportConnectOptions, TransportSendOptions, ReconnectPolicy, TransportState } from './contracts/transport';
import { WSMessage, ServifyEventMap, Message, MessageTranslation, RemoteAssistRuntimeState, RemoteAssistState, WebSocketFactory, ServifyRTCIceServer, AiStreamDeltaUpdate, AiStreamEndUpdate, TransferAssignmentUpdate, TransferWaitingUpdate } from './types';
import { StreamingAssembler } from './streaming';

export interface WebSocketManagerOptions {
  url: string;
  protocols?: string | string[];
  debug?: boolean;
  reconnectPolicy?: ReconnectPolicy;
  authProvider?: AuthProvider;
  onTokenRefreshRequired?: () => Promise<void>;
  /** WebSocket 工厂注入点；缺省用 globalThis.WebSocket（非 DOM 宿主需自行注入）。 */
  webSocketFactory?: WebSocketFactory;
  /** 回显判据超时（PROTOCOL §6.3）：发送后等待自己回显帧的上限，生产默认 10s。 */
  echoTimeoutMs?: number;
}

// 默认构造：运行时探测全局 WebSocket，缺失时给出可操作的错误信息。
function createDefaultWebSocket(url: string, protocols?: string | string[]): WebSocket {
  const WS = (globalThis as { WebSocket?: new (url: string, protocols?: string | string[]) => WebSocket })
    .WebSocket;
  if (!WS) {
    throw new Error(
      'WebSocket is not available in this environment; provide options.webSocketFactory to supply one.',
    );
  }
  return new WS(url, protocols);
}

type NormalizedWebSocketManagerOptions = Omit<
  Required<WebSocketManagerOptions>,
  'authProvider'
> & {
  authProvider?: AuthProvider;
};

/** 回显判据默认超时（PROTOCOL §6.3 成功判据的本地兜底；对齐移动端 EchoGate 10s）。 */
const DEFAULT_ECHO_TIMEOUT_MS = 10_000;

export class WebSocketManager extends EventEmitter<ServifyEventMap> implements Transport<WSMessage, WSMessage> {
  private ws: WebSocket | null = null;
  private options: NormalizedWebSocketManagerOptions;
  private reconnectAttemptCount = 0;
  private reconnectTimer: NodeJS.Timeout | null = null;
  private isManualClose = false;
  private subscribers = new Set<(message: WSMessage) => void>();
  /** ai-response-delta 三段流式契约的拼接状态（断连时在 onclose 单点收口）。 */
  private assembler = new StreamingAssembler();
  /**
   * 回显确认闸（PROTOCOL §6.3 对齐移动端 EchoGate）：text-message 发送后等待自己的
   * 回显帧，按内容匹配——迟到的旧回显不会误完成下一次发送。JS 事件循环单线程，
   * 无需移动端的 mutex/atomic；并发发送由 sdk.ts 的 promise 链串行化保证
   * 「同一时刻至多一个未决」。
   */
  private pendingEcho: { expectedContent: string; resolve: () => void } | null = null;
  readonly kind = 'websocket';
  state: TransportState = 'idle';

  constructor(options: WebSocketManagerOptions) {
    super();

    const reconnectPolicy = normalizeReconnectPolicy(options.reconnectPolicy);

    this.options = {
      protocols: [],
      debug: false,
      reconnectPolicy,
      authProvider: options.authProvider,
      onTokenRefreshRequired: options.onTokenRefreshRequired ?? (async () => undefined),
      ...options,
      // 展开后再兜底：调用方显式传 undefined 也不会覆盖掉默认工厂
      webSocketFactory: options.webSocketFactory ?? createDefaultWebSocket,
      echoTimeoutMs: options.echoTimeoutMs ?? DEFAULT_ECHO_TIMEOUT_MS,
    };
  }

  async connect(_options?: TransportConnectOptions): Promise<void> {
    const connectionUrl = await this.resolveConnectionUrl();

    return new Promise((resolve, reject) => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.state = 'connected';
        resolve();
        return;
      }

      this.isManualClose = false;
      this.state = 'connecting';
      this.log('正在连接 WebSocket...', connectionUrl);

      try {
        this.ws = this.options.webSocketFactory(connectionUrl, this.options.protocols);
      } catch (error) {
        this.state = 'error';
        reject(error);
        return;
      }

      this.ws.onopen = () => {
        this.log('WebSocket 连接成功');
        this.reconnectAttemptCount = 0;
        this.state = 'connected';
        this.emit('connected');
        resolve();
      };

      this.ws.onmessage = (event) => {
        try {
          const message: WSMessage = JSON.parse(event.data);
          this.handleMessage(message);
        } catch (error) {
          this.log('解析消息失败:', error);
          this.emit('error', new Error('Invalid message format'));
        }
      };

      this.ws.onclose = (event) => {
        this.log('WebSocket 连接关闭:', event.code, event.reason);
        this.closeStreamOnDisconnect();
        this.state = this.isManualClose ? 'closed' : 'idle';
        this.emit('disconnected', event.reason || '连接关闭');

        if (shouldReconnect(
          {
            attempt: this.reconnectAttemptCount,
            isManualClose: this.isManualClose,
          },
          this.options.reconnectPolicy,
        )) {
          this.state = 'reconnecting';
          this.scheduleReconnect();
        }
      };

      this.ws.onerror = (event) => {
        this.log('WebSocket 错误:', event);
        this.state = 'error';
        const err = new ServifyError('WebSocket connection error', {
          code: 'transport_unavailable',
          retryable: true,
          details: { url: this.options.url },
        });
        this.emit('error', err);
        reject(err);
      };
    });
  }

  async disconnect(): Promise<void> {
    this.isManualClose = true;
    this.state = 'closed';

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }

    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  async send(message: WSMessage, _options?: TransportSendOptions): Promise<void> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      this.log('WebSocket 未连接，无法发送消息');
      const err = new ServifyError('WebSocket not connected', {
        code: 'transport_disconnected',
        retryable: true,
      });
      this.emit('error', err);
      throw err;
    }

    try {
      this.ws.send(JSON.stringify(message));
      this.log('发送消息:', message);
    } catch (error) {
      this.log('发送消息失败:', error);
      const err = new ServifyError('Failed to send message', {
        code: 'transport_unavailable',
        cause: error,
        retryable: true,
      });
      this.emit('error', err);
      throw err;
    }
  }

  isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  /**
   * 发送并等待回显确认（PROTOCOL §6.3：成功判据=收到自己的回显帧；超时 throw
   * retryable 的 transport_timeout——不本地伪造成功）。对齐移动端 EchoGate 口径。
   * 断线不提前清闸：回显永远到不了，等超时路径自然收口（同移动端）。
   */
  async sendWithEchoConfirmation(message: WSMessage, expectedContent: string): Promise<void> {
    const timeoutMs = this.options.echoTimeoutMs;
    const gate = new Promise<void>((resolve) => {
      this.pendingEcho = { expectedContent, resolve };
    });
    try {
      await this.send(message);
    } catch (error) {
      if (this.pendingEcho?.expectedContent === expectedContent) this.pendingEcho = null;
      throw error;
    }
    let timer: NodeJS.Timeout | null = null;
    try {
      await Promise.race([
        gate,
        new Promise<never>((_, reject) => {
          timer = setTimeout(() => {
            if (this.pendingEcho?.expectedContent === expectedContent) this.pendingEcho = null;
            reject(
              new ServifyError(`No echo within ${timeoutMs}ms`, {
                code: 'transport_timeout',
                retryable: true,
                details: { expectedContent },
              }),
            );
          }, timeoutMs);
        }),
      ]);
    } finally {
      if (timer) clearTimeout(timer);
    }
  }

  /** 回显帧（text-message）按内容匹配闸——内容不符不动闸（旧回显不误完成新发送）。 */
  private resolvePendingEcho(data: unknown): void {
    if (!this.pendingEcho || typeof data !== 'object' || data === null) return;
    const { content } = data as Record<string, unknown>;
    if (typeof content === 'string' && content === this.pendingEcho.expectedContent) {
      const gate = this.pendingEcho;
      this.pendingEcho = null;
      gate.resolve();
    }
  }

  subscribe(handler: (message: WSMessage) => void): () => void {
    this.subscribers.add(handler);
    return () => {
      this.subscribers.delete(handler);
    };
  }

  private handleMessage(message: WSMessage): void {
    this.log('收到消息:', message);
    for (const subscriber of this.subscribers) {
      subscriber(message);
    }

      switch (message.type) {
      case 'text-message':
        this.emit('message', this.normalizeMessage(message, 'customer'));
        this.resolvePendingEcho(message.data);
        break;
      case 'agent-message':
        this.emit('message', this.normalizeMessage(message, 'agent'));
        break;
      case 'message-translated':
        // 译文注解帧（PROTOCOL §4.5）：独立事件、不并入 messages、
        // payload 异常时按原样透传由消费方自行判定（绝不是错误）。
        this.emit('message-translated', message.data as MessageTranslation);
        break;
      case 'ai-response':
        this.emit('message', this.normalizeMessage(message, 'system', true));
        this.closeStreamOnFinal();
        break;
      case 'ai-response-delta':
        this.handleStreamDelta(message.data);
        break;
      case 'transfer_notification':
        this.handleTransferNotification(message.data);
        break;
      case 'waiting_notification':
        this.handleWaitingNotification(message.data);
        break;
      case 'webrtc-offer':
        this.emit('webrtc:offer', message.data as RTCSessionDescriptionInit);
        break;
      case 'webrtc-answer':
        this.emit('webrtc:answer', message.data as RTCSessionDescriptionInit);
        break;
      case 'webrtc-candidate':
        this.emit('webrtc:candidate', this.extractICECandidate(message.data));
        break;
      case 'webrtc-state-change':
        this.emit('webrtc:state', this.extractRemoteAssistState(message.data));
        break;
      case 'webrtc-ice-config':
        this.emit('webrtc:ice-config', this.extractIceServers(message.data));
        break;
      default:
        this.log('未知消息类型:', message.type);
    }
  }

  private extractRemoteAssistState(data: unknown): RemoteAssistState {
    const runtimeState = this.extractRemoteAssistRuntimeState(data);

    switch (runtimeState.state) {
      case 'new':
      case 'checking':
      case 'connecting':
        return 'connecting';
      case 'connected':
        return 'connected';
      case 'failed':
        return 'failed';
      case 'closed':
      case 'disconnected':
        return 'ended';
      default:
        return 'connecting';
    }
  }

  private extractRemoteAssistRuntimeState(data: unknown): RemoteAssistRuntimeState {
    if (typeof data === 'object' && data !== null) {
      return {
        connectionId:
          'connection_id' in data && typeof data.connection_id === 'string'
            ? data.connection_id
            : undefined,
        state:
          'state' in data && typeof data.state === 'string'
            ? data.state
            : 'connecting',
      };
    }

    return { state: 'connecting' };
  }

  private scheduleReconnect(): void {
    this.reconnectAttemptCount++;
    this.emit('reconnecting', this.reconnectAttemptCount);

    const delay = computeReconnectDelay(this.options.reconnectPolicy, this.reconnectAttemptCount);
    this.log(`${delay}ms 后重连 (第 ${this.reconnectAttemptCount}/${this.options.reconnectPolicy.maxAttempts} 次)`);

    this.reconnectTimer = setTimeout(() => {
      this.connect().catch(() => {
        // 重连失败，继续尝试或放弃
      });
    }, delay);
  }

  private log(...args: unknown[]): void {
    if (this.options.debug) {
      console.warn('[ServifyWS]', ...args);
    }
  }

  private async resolveConnectionUrl(): Promise<string> {
    const token = await this.resolveAuthToken(this.options.authProvider);
    if (!token) {
      return this.options.url;
    }

    const url = new URL(this.options.url);
    url.searchParams.set('access_token', token);
    return url.toString();
  }

  private async resolveAuthToken(authProvider?: AuthProvider): Promise<string | null> {
    if (!authProvider) {
      return null;
    }

    const currentToken = await authProvider.getToken();
    if (currentToken?.accessToken) {
      return currentToken.accessToken;
    }

    if (!authProvider.refreshToken) {
      return null;
    }

    await this.options.onTokenRefreshRequired();

    const refreshedToken = await authProvider.refreshToken();
    if (refreshedToken?.accessToken) {
      return refreshedToken.accessToken;
    }

    throw new ServifyError('Authentication refresh required', {
      code: 'auth_refresh_required',
      retryable: false,
      details: { url: this.options.url },
    });
  }

  /**
   * ai-response-delta 增量帧：畸形负载静默忽略；内容增量发 ai-stream:delta
   * （content 为累计全量，UI 按 id upsert）；终末增量只做标记不发事件。
   */
  private handleStreamDelta(data: unknown): void {
    if (typeof data !== 'object' || data === null) {
      return;
    }
    const { content_delta, done } = data as Record<string, unknown>;
    if (typeof content_delta !== 'string' || typeof done !== 'boolean') {
      return;
    }
    const outcome = this.assembler.onDelta(content_delta, done);
    if (outcome === 'appended') {
      const update: AiStreamDeltaUpdate = {
        id: this.assembler.currentId ?? '',
        content: this.assembler.content,
      };
      this.emit('ai-stream:delta', update);
    }
  }

  /** 终帧收口：先发终帧 message（调用方已完成）再发 ai-stream:end 让 UI 移除流式气泡。 */
  private closeStreamOnFinal(): void {
    const ended = this.assembler.closeOnFinal();
    if (ended) {
      this.emit('ai-stream:end', { id: ended.id, interrupted: false });
    }
  }

  /** 断连收口（onclose 单点，主动 close 与异常断连都经此）：中断流通知 UI 保留部分内容。 */
  private closeStreamOnDisconnect(): void {
    const ended = this.assembler.closeOnDisconnect();
    if (ended) {
      const update: AiStreamEndUpdate = { id: ended.id, interrupted: true, content: ended.content };
      this.emit('ai-stream:end', update);
    }
  }

  /** 转人工通知（PROTOCOL §4.2）：坐席已接入（含等待队列派发）。纯事件流，不渲染消息、不动会话状态——对齐移动端 agentAssigned 语义。 */
  private handleTransferNotification(data: unknown): void {
    if (typeof data !== 'object' || data === null) return;
    const { message, agent_id } = data as Record<string, unknown>;
    if (typeof message !== 'string' || typeof agent_id !== 'number') return;
    const update: TransferAssignmentUpdate = { agentId: agent_id, message };
    this.emit('transfer:assigned', update);
  }

  /** 排队通知（PROTOCOL §4.2）：已入等待队列。同上，纯事件流——对齐移动端 waitingInQueue 语义。 */
  private handleWaitingNotification(data: unknown): void {
    if (typeof data !== 'object' || data === null) return;
    const { message } = data as Record<string, unknown>;
    if (typeof message !== 'string') return;
    const update: TransferWaitingUpdate = { message };
    this.emit('transfer:waiting', update);
  }

  private normalizeMessage(message: WSMessage, senderType: Message['sender_type'], isAIResponse = false): Message {
    const data = typeof message.data === 'object' && message.data !== null
      ? message.data as Record<string, unknown>
      : { content: String(message.data ?? '') };

    const content =
      typeof data.content === 'string'
        ? data.content
        : typeof data.message === 'string'
          ? data.message
          : String(message.data ?? '');

    return {
      id: typeof data.id === 'string' || typeof data.id === 'number'
        ? data.id
        : `ws-${Date.now()}`,
      session_id: typeof data.session_id === 'string' || typeof data.session_id === 'number'
        ? data.session_id
        : message.session_id ?? '',
      sender_type: senderType,
      content,
      message_type: 'text',
      is_ai_response: isAIResponse,
      metadata: data,
      created_at: new Date().toISOString(),
    };
  }

  /** 归一化服务端下发的 webrtc-ice-config 负载；条目非法即丢弃，整体非法返回空。 */
  private extractIceServers(data: unknown): ServifyRTCIceServer[] {
    if (typeof data !== 'object' || data === null) {
      return [];
    }
    const rawServers = (data as { ice_servers?: unknown }).ice_servers;
    if (!Array.isArray(rawServers)) {
      return [];
    }
    const servers: ServifyRTCIceServer[] = [];
    for (const entry of rawServers) {
      if (typeof entry !== 'object' || entry === null) {
        continue;
      }
      const { urls, username, credential, ttl } = entry as Record<string, unknown>;
      if (typeof urls !== 'string' && !Array.isArray(urls)) {
        continue;
      }
      const server: ServifyRTCIceServer = { urls };
      if (typeof username === 'string') {
        server.username = username;
      }
      if (typeof credential === 'string') {
        server.credential = credential;
      }
      if (typeof ttl === 'number' && Number.isFinite(ttl)) {
        server.ttl = ttl;
      }
      servers.push(server);
    }
    return servers;
  }

  private extractICECandidate(data: unknown): RTCIceCandidateInit {
    if (
      typeof data === 'object' &&
      data !== null &&
      'candidate' in data &&
      typeof data.candidate === 'object' &&
      data.candidate !== null
    ) {
      return data.candidate as RTCIceCandidateInit;
    }

    return data as RTCIceCandidateInit;
  }
}
