import EventEmitter from 'eventemitter3';
import { ApiClient } from './api';
import { WebSocketManager } from './websocket';
import { createWebCapabilitySet } from './bindings/web';
import type { ClientSession, SessionIdentity } from './contracts/client-session';
import type { CapabilitySet } from './contracts/capability';
import {
  ServifyConfig,
  ServifyEventMap,
  Customer,
  Agent,
  ChatSession,
  Message,
  Ticket,
  CustomerSatisfaction,
  RemoteAssistStartOptions,
  RemoteAssistState,
  RemoteAssistRecordingState,
  ServifyRTCIceServer,
  ServifyRTCSessionDescriptionInit,
  ServifyRTCIceCandidateInit,
} from './types';

export class ServifySDK extends EventEmitter<ServifyEventMap> implements ClientSession<Record<string, unknown>, ServifyEventMap> {
  private config: ServifyConfig;
  private api: ApiClient;
  private ws: WebSocketManager | null = null;
  private currentCustomer: Customer | null = null;
  private currentSession: ChatSession | null = null;
  private currentAgent: Agent | null = null;
  private messageQueue: Message[] = [];
  private remoteAssistPeer: RTCPeerConnection | null = null;
  private remoteAssistStream: MediaStream | null = null;
  private remoteAssistSessionId: string | null = null;
  private remoteAssistRecorder: MediaRecorder | null = null;
  private remoteAssistChunks: Blob[] = [];
  private remoteAssistRecordingStartedAt = 0;
  private isInitialized = false;
  readonly id: string;
  readonly capabilities: CapabilitySet;
  readonly events = this;
  readonly authProvider = undefined;
  readonly transport = {
    get kind() { return 'session'; },
    get state() { return 'idle' as const; },
    connect: async () => undefined,
    disconnect: async () => undefined,
    send: async () => undefined,
    isConnected: () => false,
    subscribe: () => () => undefined,
  };

  constructor(config: ServifyConfig) {
    super();
    this.id = config.sessionId || `web-${Date.now()}`;
    this.capabilities = config.capabilities ?? createWebCapabilitySet();

    this.config = {
      autoConnect: true,
      reconnectAttempts: 5,
      reconnectDelay: 1000,
      debug: false,
      ...config
    };

    // 初始化 API 客户端
    this.api = new ApiClient({
      baseUrl: this.config.apiUrl,
      debug: this.config.debug,
    });

    // 如果提供了客户信息，设置到 API 客户端
    if (this.config.customerId) {
      this.api.setCustomerId(parseInt(this.config.customerId));
    }

    this.log('SDK 初始化完成', this.config);
  }

  // 初始化 SDK
  async initialize(): Promise<void> {
    if (this.isInitialized) {
      this.log('SDK 已初始化');
      return;
    }

    try {
      this.log('正在初始化 SDK...');

      // 获取或创建客户信息
      await this.initializeCustomer();

      // 初始化 WebSocket 连接
      if (this.config.autoConnect) {
        await this.connect();
      }

      this.isInitialized = true;
      this.log('SDK 初始化成功');

    } catch (error) {
      this.log('SDK 初始化失败:', error);
      throw error;
    }
  }

  // 连接到服务器
  async connect(): Promise<void> {
    if (!this.currentCustomer) {
      throw new Error('Customer not initialized. Call initialize() first.');
    }

    const wsUrl = this.config.wsUrl || this.config.apiUrl.replace(/^http/, 'ws') + '/api/v1/ws';
    const realtimeSessionID = this.resolveRealtimeSessionID();

    this.ws = new WebSocketManager({
      url: `${wsUrl}?session_id=${encodeURIComponent(realtimeSessionID)}`,
      reconnectAttempts: this.config.reconnectAttempts!,
      reconnectDelay: this.config.reconnectDelay!,
      reconnectPolicy: this.config.reconnectPolicy,
      authProvider: this.config.authProvider,
      onTokenRefreshRequired: this.config.onTokenRefreshRequired,
      webSocketFactory: this.config.webSocketFactory,
      debug: this.config.debug,
    });

    // 转发 WebSocket 事件
    this.ws.on('connected', () => this.emit('connected'));
    this.ws.on('disconnected', (reason) => this.emit('disconnected', reason));
    this.ws.on('reconnecting', (attempt) => this.emit('reconnecting', attempt));
    this.ws.on('message', (message) => this.handleIncomingMessage(message));
    this.ws.on('session_updated', (session) => {
      this.currentSession = session;
      this.emit('session_updated', session);
    });
    this.ws.on('agent_assigned', (agent) => {
      this.currentAgent = agent;
      this.emit('agent_assigned', agent);
    });
    this.ws.on('agent_typing', (isTyping) => this.emit('agent_typing', isTyping));
    this.ws.on('webrtc:offer', (offer) => this.emit('webrtc:offer', offer));
    this.ws.on('webrtc:answer', (answer) => this.emit('webrtc:answer', answer));
    this.ws.on('webrtc:candidate', (candidate) => this.emit('webrtc:candidate', candidate));
    this.ws.on('webrtc:state', (state) => this.updateRemoteAssistState(state));
    this.ws.on('error', (error) => this.emit('error', error));

    await this.ws.connect();
  }

  // 断开连接
  disconnect(): void {
    void this.endRemoteAssist();
    this.ws?.disconnect();
    this.ws = null;
  }

  // 开始聊天会话
  async startChat(options?: {
    priority?: 'low' | 'normal' | 'high' | 'urgent';
    message?: string;
    metadata?: Record<string, unknown>;
  }): Promise<ChatSession> {
    if (!this.currentCustomer) {
      throw new Error('Customer not initialized');
    }

    const sessionData: ChatSession = {
      id: this.currentSession?.id ?? this.id,
      customer_id: this.currentCustomer.id,
      status: 'active',
      channel: 'web',
      priority: options?.priority || 'normal',
      started_at: new Date().toISOString(),
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };

    this.currentSession = sessionData;

    if (!this.ws?.isConnected()) {
      await this.connect();
    }

    this.emit('session_created', this.currentSession);

    // 如果有初始消息，发送它
    if (options?.message) {
      await this.sendMessage(options.message);
    }

    return this.currentSession;
  }

  // 发送消息
  async sendMessage(content: string, options?: {
    type?: 'text' | 'image' | 'file';
    attachments?: string[];
    metadata?: Record<string, unknown>;
  }): Promise<Message> {
    if (!this.currentSession) {
      throw new Error('No active session. Start a chat first.');
    }

    const messageData: Message = {
      id: `local-${Date.now()}`,
      session_id: this.currentSession.id,
      sender_type: 'customer',
      sender_id: this.currentCustomer?.id,
      content,
      message_type: options?.type || 'text',
      attachments: options?.attachments,
      metadata: options?.metadata,
      created_at: new Date().toISOString(),
    };

    if (!this.ws?.isConnected()) {
      await this.connect();
    }

    await this.ws?.send({
      type: 'text-message',
      data: {
        content,
        message_type: options?.type || 'text',
        attachments: options?.attachments,
        metadata: options?.metadata,
      },
    });

    return messageData;
  }

  // 结束会话
  async endSession(): Promise<void> {
    if (!this.currentSession) {
      return;
    }

    await this.endRemoteAssist();

    const endedSession = { ...this.currentSession, status: 'closed' as const, ended_at: new Date().toISOString() };
    this.currentSession = null;
    this.currentAgent = null;
    this.emit('session_ended', endedSession);
  }

  // 创建工单
  async createTicket(ticketData: {
    title: string;
    description: string;
    priority?: 'low' | 'normal' | 'high' | 'urgent';
    category: string;
    metadata?: Record<string, unknown>;
  }): Promise<Ticket> {
    if (!this.currentCustomer) {
      throw new Error('Customer not initialized');
    }

    const data: Partial<Ticket> = {
      ...ticketData,
      customer_id: this.currentCustomer.id,
      status: 'open',
    };

    const response = await this.api.createTicket(data);
    if (!response.success || !response.data) {
      throw new Error(response.error || 'Failed to create ticket');
    }

    this.emit('ticket_created', response.data);
    return response.data;
  }

  async startRemoteAssist(options?: RemoteAssistStartOptions): Promise<RTCPeerConnection> {
    if (!this.ws?.isConnected()) {
      throw new Error('WebSocket not connected. Call connect() first.');
    }

    this.updateRemoteAssistState('starting');
    // 重置残留媒体资源但保留已绑定的 assist 会话（宿主页可能在 start 前注入）
    await this.cleanupRemoteAssistMedia();

    const iceServers = options?.iceServers || this.config.remoteAssist?.iceServers || [];
    const peerFactory =
      options?.peerConnectionFactory ?? this.config.remoteAssist?.peerConnectionFactory;
    const peer = peerFactory
      ? peerFactory({ iceServers })
      : this.createDefaultPeerConnection(iceServers);
    this.remoteAssistPeer = peer;

    const dataChannel = peer.createDataChannel(
      options?.dataChannelLabel ||
        this.config.remoteAssist?.dataChannelLabel ||
        'servify-remote-assist',
    );
    // 坐席侧经 DataChannel 下发 assist 会话 ID（type: 'assist-session'）
    dataChannel.onmessage = (event) => this.handleRemoteAssistDataChannelMessage(event.data);

    peer.onicecandidate = (event) => {
      if (!event.candidate || !this.ws?.isConnected()) {
        return;
      }

      void this.ws.send({
        type: 'webrtc-candidate',
        data: event.candidate.toJSON(),
      });
    };

    peer.onconnectionstatechange = () => {
      const state = peer.connectionState;
      if (state === 'connected') {
        this.updateRemoteAssistState('connected');
      } else if (state === 'connecting') {
        this.updateRemoteAssistState('connecting');
      } else if (state === 'failed') {
        this.updateRemoteAssistState('failed');
      } else if (state === 'closed' || state === 'disconnected') {
        this.updateRemoteAssistState('ended');
      }
    };

    peer.ontrack = (event) => {
      this.emit('webrtc:track', event);
    };

    const shouldCaptureScreen =
      options?.captureScreen ?? this.config.remoteAssist?.captureScreen ?? false;
    if (shouldCaptureScreen) {
      this.remoteAssistStream = await this.captureRemoteAssistStream(options);
      for (const track of this.remoteAssistStream.getTracks()) {
        peer.addTrack(track, this.remoteAssistStream);
        // 用户在浏览器弹层点「停止共享」时也要落盘录制
        track.addEventListener('ended', () => {
          void this.stopRemoteAssistRecording().catch(() => undefined);
        });
      }
      this.maybeStartRemoteAssistRecorder(options);
    }

    const offer = await peer.createOffer();
    await peer.setLocalDescription(offer);
    const localDescription = peer.localDescription;
    if (!localDescription) {
      throw new Error('Failed to create local WebRTC description');
    }

    await this.ws.send({
      type: 'webrtc-offer',
      data: localDescription.toJSON(),
    });
    this.emit('webrtc:offer', localDescription.toJSON());
    this.updateRemoteAssistState('offered');

    return peer;
  }

  async acceptRemoteAnswer(answer: ServifyRTCSessionDescriptionInit): Promise<void> {
    if (!this.remoteAssistPeer) {
      throw new Error('Remote assist has not started');
    }

    await this.remoteAssistPeer.setRemoteDescription(answer);
    this.updateRemoteAssistState('connecting');
  }

  async addRemoteIce(candidate: ServifyRTCIceCandidateInit): Promise<void> {
    if (!this.remoteAssistPeer) {
      throw new Error('Remote assist has not started');
    }

    await this.remoteAssistPeer.addIceCandidate(candidate);
  }

  async endRemoteAssist(): Promise<void> {
    // 先落盘录制（需要 stream 仍活着拿最后的数据），再停 track
    await this.cleanupRemoteAssistMedia();
    this.remoteAssistSessionId = null;
    this.updateRemoteAssistState('ended');
  }

  // 显式登记远程协助会话 ID（宿主页可从 URL/postMessage 获取后注入；
  // 也由坐席经 DataChannel 自动下发）。录制上传时回写到该会话。
  setRemoteAssistSession(assistId: string | number): void {
    const id = String(assistId).trim();
    if (!id) {
      return;
    }
    this.remoteAssistSessionId = id;
    this.emit('remote-assist:session', id);
  }

  getRemoteAssistSession(): string | null {
    return this.remoteAssistSessionId;
  }

  // 提交满意度评价
  async submitSatisfaction(satisfaction: {
    ticket_id?: number;
    rating: number;
    comment?: string;
    category?: string;
  }): Promise<CustomerSatisfaction> {
    if (!this.currentCustomer) {
      throw new Error('Customer not initialized');
    }

    const data: Partial<CustomerSatisfaction> = {
      ...satisfaction,
      customer_id: this.currentCustomer.id,
      agent_id: this.currentAgent?.id,
    };

    const response = await this.api.submitSatisfaction(data);
    if (!response.success || !response.data) {
      throw new Error(response.error || 'Failed to submit satisfaction');
    }

    return response.data;
  }

  // AI 问答
  async askAI(question: string): Promise<{ answer: string; confidence: number }> {
    const response = await this.api.askAI(question, this.currentSession?.id);
    if (!response.success || !response.data) {
      throw new Error(response.error || 'Failed to get AI response');
    }

    return response.data;
  }

  // 文件上传
  async uploadFile(file: File): Promise<{ file_url: string; file_name: string; file_size: number }> {
    if (!this.currentSession) {
      throw new Error('No active session');
    }

    const response = await this.api.uploadFile(file, this.currentSession.id);
    if (!response.success || !response.data) {
      throw new Error(response.error || 'Failed to upload file');
    }

    return response.data;
  }

  // 获取历史消息
  async getMessages(options?: {
    page?: number;
    limit?: number;
  }): Promise<{ messages: Message[]; total: number; page: number }> {
    if (!this.currentSession) {
      throw new Error('No active session');
    }

    const response = await this.api.getSessionMessages(this.currentSession.id, options);
    if (!response.success || !response.data) {
      throw new Error(response.error || 'Failed to get messages');
    }

    return response.data;
  }

  // 获取客户信息
  getCustomer(): Customer | null {
    return this.currentCustomer;
  }

  // 获取当前会话
  getSession(): ChatSession | null {
    return this.currentSession;
  }

  // 获取当前客服代理
  getAgent(): Agent | null {
    return this.currentAgent;
  }

  getIdentity(): SessionIdentity {
    return {
      customerId: this.currentCustomer?.id?.toString(),
      conversationId: this.currentSession?.id?.toString(),
      agentId: this.currentAgent?.id?.toString(),
    };
  }

  getState(): Record<string, unknown> {
    return {
      initialized: this.isInitialized,
      connected: this.isConnected(),
      customer: this.currentCustomer,
      session: this.currentSession,
      agent: this.currentAgent,
    };
  }

  updateState(patch: Partial<Record<string, unknown>>): Record<string, unknown> {
    return { ...this.getState(), ...patch };
  }

  // 检查连接状态
  isConnected(): boolean {
    return this.ws?.isConnected() ?? false;
  }

  // 私有方法：初始化客户信息
  private async initializeCustomer(): Promise<void> {
    const customerId = Number.parseInt(this.config.customerId || '0', 10);
    this.currentCustomer = {
      id: Number.isFinite(customerId) && customerId > 0 ? customerId : 0,
      name: this.config.customerName || 'Anonymous',
      email: this.config.customerEmail || '',
      status: 'active',
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };

    if (this.currentCustomer.id > 0) {
      this.api.setCustomerId(this.currentCustomer.id);
    }
  }

  // 私有方法：处理收到的消息
  private handleIncomingMessage(message: Message): void {
    this.messageQueue.push(message);
    this.emit('message', message);
  }

  private resolveRealtimeSessionID(): string {
    const sessionID = this.currentSession?.id;
    if (typeof sessionID === 'string' || typeof sessionID === 'number') {
      return String(sessionID);
    }

    return this.id;
  }

  private static readonly RECORDING_MIME_CANDIDATES = [
    'video/webm;codecs=vp9',
    'video/webm;codecs=vp8',
    'video/webm',
    'video/mp4',
  ];

  // 停录制（如在上传）→ 停共享 track → 关闭 peer；不动已绑定的 assist 会话 ID
  private async cleanupRemoteAssistMedia(): Promise<void> {
    await this.stopRemoteAssistRecording();

    if (this.remoteAssistStream) {
      for (const track of this.remoteAssistStream.getTracks()) {
        track.stop();
      }
      this.remoteAssistStream = null;
    }

    if (this.remoteAssistPeer) {
      this.remoteAssistPeer.onicecandidate = null;
      this.remoteAssistPeer.onconnectionstatechange = null;
      this.remoteAssistPeer.ontrack = null;
      this.remoteAssistPeer.close();
      this.remoteAssistPeer = null;
    }
  }

  // record 开关开启且浏览器支持 MediaRecorder 时启动本地录制；否则静默降级
  private maybeStartRemoteAssistRecorder(options?: RemoteAssistStartOptions): void {
    const shouldRecord = options?.record ?? this.config.remoteAssist?.record ?? false;
    if (!shouldRecord || !this.remoteAssistStream) {
      return;
    }

    const mimeType = this.resolveRecordingMimeType();
    if (!mimeType) {
      this.log('MediaRecorder unavailable, remote assist recording skipped');
      this.emitRemoteAssistRecording('unsupported');
      return;
    }

    try {
      const recorder = new MediaRecorder(this.remoteAssistStream, { mimeType });
      recorder.ondataavailable = (event) => {
        if (event.data && event.data.size > 0) {
          this.remoteAssistChunks.push(event.data);
        }
      };
      recorder.start(1000);
      this.remoteAssistRecorder = recorder;
      this.remoteAssistChunks = [];
      this.remoteAssistRecordingStartedAt = Date.now();
      this.emitRemoteAssistRecording('recording');
    } catch (error) {
      this.log('Failed to start remote assist recording:', error);
      this.emitRemoteAssistRecording('failed');
    }
  }

  // 停止录制并把 Blob 上传到既有 /upload，再回写协助会话录制元数据。
  // 上传失败只发 error 事件，不阻塞 endRemoteAssist 的清理。
  private async stopRemoteAssistRecording(): Promise<void> {
    const recorder = this.remoteAssistRecorder;
    if (!recorder) {
      return;
    }

    this.remoteAssistRecorder = null;
    // 不提前清空 chunks：stop() 内部会同步 flush 最后一个 chunk 到同一数组
    const chunks = this.remoteAssistChunks;
    const startedAt = this.remoteAssistRecordingStartedAt;

    const stopped = new Promise<void>((resolve) => {
      recorder.onstop = () => resolve();
    });
    try {
      recorder.stop();
    } catch (error) {
      this.log('Remote assist recorder stop failed:', error);
    }
    await stopped;

    this.remoteAssistChunks = [];
    this.remoteAssistRecordingStartedAt = 0;

    const blob = new Blob(chunks, { type: recorder.mimeType || 'video/webm' });
    if (blob.size === 0) {
      this.emitRemoteAssistRecording('idle');
      return;
    }

    const assistId = this.remoteAssistSessionId;
    if (!assistId) {
      this.log('No remote assist session bound, recording dropped');
      this.emitRemoteAssistRecording('failed');
      return;
    }

    this.emitRemoteAssistRecording('saving');
    try {
      const extension = blob.type.includes('mp4') ? 'mp4' : 'webm';
      const file = new File(
        [blob],
        `remote-assist-${assistId}-${Date.now()}.${extension}`,
        { type: blob.type },
      );
      const upload = await this.api.uploadFile(file, this.resolveRealtimeSessionID());
      if (!upload.success || !upload.data) {
        throw new Error(upload.error || 'Failed to upload remote assist recording');
      }
      // 服务端 upload 响应的 URL 字段是 url（file_url 为旧契约兜底）
      const payload = upload.data as { url?: string; file_url?: string };
      const recordingKey = payload.url || payload.file_url || '';
      if (!recordingKey) {
        throw new Error('Upload response missing recording URL');
      }

      const attach = await this.api.attachRemoteAssistRecording(assistId, {
        recording_key: recordingKey,
        recording_mime: blob.type,
        recording_duration_ms: Date.now() - startedAt,
        recording_size: blob.size,
      });
      if (!attach.success) {
        throw new Error(attach.error || 'Failed to attach remote assist recording');
      }
      this.emitRemoteAssistRecording('saved');
    } catch (error) {
      this.log('Failed to save remote assist recording:', error);
      this.emitRemoteAssistRecording('failed');
      this.emit(
        'error',
        error instanceof Error ? error : new Error('Failed to save remote assist recording'),
      );
    }
  }

  private resolveRecordingMimeType(): string | null {
    if (typeof MediaRecorder === 'undefined') {
      return null;
    }

    for (const mimeType of ServifySDK.RECORDING_MIME_CANDIDATES) {
      try {
        if (typeof MediaRecorder.isTypeSupported === 'function' && MediaRecorder.isTypeSupported(mimeType)) {
          return mimeType;
        }
      } catch {
        // 探测失败继续尝试下一个
      }
    }
    // isTypeSupported 缺失时退回 webm 默认值，让 MediaRecorder 构造自行报错
    return typeof MediaRecorder.isTypeSupported === 'function' ? null : 'video/webm';
  }

  private emitRemoteAssistRecording(state: RemoteAssistRecordingState): void {
    this.emit('remote-assist:recording', state);
  }

  private handleRemoteAssistDataChannelMessage(raw: unknown): void {
    if (typeof raw !== 'string' || !raw.startsWith('{')) {
      return;
    }

    try {
      const parsed = JSON.parse(raw) as { type?: string; assist_id?: unknown };
      if (parsed.type !== 'assist-session') {
        return;
      }
      if (typeof parsed.assist_id !== 'string' && typeof parsed.assist_id !== 'number') {
        return;
      }
      this.setRemoteAssistSession(parsed.assist_id);
    } catch {
      // 非 JSON 消息忽略（DataChannel 上可能跑其他协议）
    }
  }

  // 默认 RTCPeerConnection 构造：非 DOM 宿主必须注入 peerConnectionFactory。
  private createDefaultPeerConnection(iceServers: ServifyRTCIceServer[]): RTCPeerConnection {
    const RTC = (globalThis as {
      RTCPeerConnection?: new (config?: { iceServers?: unknown[] }) => RTCPeerConnection;
    }).RTCPeerConnection;
    if (!RTC) {
      throw new Error(
        'RTCPeerConnection is not available in this environment; inject remoteAssist.peerConnectionFactory to provide one.',
      );
    }
    return new RTC({ iceServers: iceServers as unknown[] });
  }

  private async captureRemoteAssistStream(options?: RemoteAssistStartOptions): Promise<MediaStream> {
    if (
      typeof navigator === 'undefined' ||
      !navigator.mediaDevices ||
      typeof navigator.mediaDevices.getDisplayMedia !== 'function'
    ) {
      throw new Error('Screen capture is not supported in this browser');
    }

    return navigator.mediaDevices.getDisplayMedia({
      video: true,
      audio: options?.audio ?? this.config.remoteAssist?.audio ?? false,
    });
  }

  private updateRemoteAssistState(state: RemoteAssistState): void {
    this.emit('webrtc:state', state);
  }

  // 私有方法：日志输出
  private log(...args: unknown[]): void {
    if (this.config.debug) {
      console.warn('[ServifySDK]', ...args);
    }
  }
}
