import EventEmitter from 'eventemitter3';
import type { AuthProvider } from './contracts/auth-provider';
import { ServifyError } from './contracts/errors';
import {
  computeReconnectDelay,
  normalizeReconnectPolicy,
  shouldReconnect,
} from './contracts/reconnect';
import type { Transport, TransportConnectOptions, TransportSendOptions, ReconnectPolicy, TransportState } from './contracts/transport';
import { WSMessage, ServifyEventMap, Message, ChatSession, RemoteAssistRuntimeState, RemoteAssistState, WebSocketFactory } from './types';

export interface WebSocketManagerOptions {
  url: string;
  protocols?: string | string[];
  heartbeatInterval?: number;
  debug?: boolean;
  reconnectPolicy?: ReconnectPolicy;
  authProvider?: AuthProvider;
  onTokenRefreshRequired?: () => Promise<void>;
  /** WebSocket 工厂注入点；缺省用 globalThis.WebSocket（非 DOM 宿主需自行注入）。 */
  webSocketFactory?: WebSocketFactory;
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

export class WebSocketManager extends EventEmitter<ServifyEventMap> implements Transport<WSMessage, WSMessage> {
  private ws: WebSocket | null = null;
  private options: NormalizedWebSocketManagerOptions;
  private reconnectAttemptCount = 0;
  private reconnectTimer: NodeJS.Timeout | null = null;
  private heartbeatTimer: NodeJS.Timeout | null = null;
  private isManualClose = false;
  private subscribers = new Set<(message: WSMessage) => void>();
  readonly kind = 'websocket';
  state: TransportState = 'idle';

  constructor(options: WebSocketManagerOptions) {
    super();

    const reconnectPolicy = normalizeReconnectPolicy(options.reconnectPolicy);

    this.options = {
      protocols: [],
      heartbeatInterval: 30000,
      debug: false,
      reconnectPolicy,
      authProvider: options.authProvider,
      onTokenRefreshRequired: options.onTokenRefreshRequired ?? (async () => undefined),
      ...options,
      // 展开后再兜底：调用方显式传 undefined 也不会覆盖掉默认工厂
      webSocketFactory: options.webSocketFactory ?? createDefaultWebSocket,
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
        this.startHeartbeat();
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
        this.stopHeartbeat();
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
    this.stopHeartbeat();

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
      case 'message':
      case 'text-message':
        this.emit('message', this.normalizeMessage(message, 'customer'));
        break;
      case 'agent-message':
        this.emit('message', this.normalizeMessage(message, 'agent'));
        break;
      case 'ai-response':
        this.emit('message', this.normalizeMessage(message, 'system', true));
        break;
      case 'session_update':
        this.emit('session_updated', message.data as ChatSession);
        break;
      case 'agent_status':
        if (typeof message.data === 'object' && message.data !== null) {
          const agentStatus = message.data as {
            type?: 'assigned' | 'typing';
            agent?: ServifyEventMap['agent_assigned'][0];
            typing?: boolean;
          };

          if (agentStatus.type === 'assigned' && agentStatus.agent) {
            this.emit('agent_assigned', agentStatus.agent);
          } else if (agentStatus.type === 'typing' && typeof agentStatus.typing === 'boolean') {
            this.emit('agent_typing', agentStatus.typing);
          }
        }
        break;
      case 'error':
        this.emit('error', new Error(this.extractMessageText(message.data) || 'Unknown error'));
        break;
      case 'system':
        // 处理系统消息，如心跳响应
        if (typeof message.data === 'object' && message.data !== null && 'type' in message.data && message.data.type === 'pong') {
          // 心跳响应处理
          this.log('收到心跳响应');
        }
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

  private startHeartbeat(): void {
    this.stopHeartbeat();

    this.heartbeatTimer = setInterval(() => {
      if (this.isConnected()) {
        void this.send({
          type: 'system',
          data: { type: 'ping', timestamp: new Date().toISOString() }
        }).catch(() => undefined);
      }
    }, this.options.heartbeatInterval);
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
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

  private extractMessageText(data: unknown): string | null {
    if (typeof data === 'object' && data !== null && 'message' in data && typeof data.message === 'string') {
      return data.message;
    }

    return null;
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
