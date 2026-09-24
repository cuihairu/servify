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
  InitialQuestionsResult,
  NextQuestionsResult,
} from './types';

// 断线补拉参数（与 Android/iOS M3 刀 10 同构镜像）：页大小 100、最多 10 轮
// （1000 条封顶，超过留给下次连接续拉）、指纹表容量 200。
const RECONCILE_PAGE_LIMIT = 100;
const RECONCILE_MAX_PAGES = 10;
const RECONCILE_FINGERPRINT_CAPACITY = 200;

export class ServifySDK extends EventEmitter<ServifyEventMap> implements ClientSession<Record<string, unknown>, ServifyEventMap> {
  private config: ServifyConfig;
  private api: ApiClient;
  private ws: WebSocketManager | null = null;
  private currentCustomer: Customer | null = null;
  private currentSession: ChatSession | null = null;
  private currentAgent: Agent | null = null;
  private messageQueue: Message[] = [];
  // 断线补拉状态（D7 流程 3；§10 #1 端点消费；Android/iOS M3 刀 10 同构）：
  // 游标仅由补拉结果推进（WS 帧无服务端消息 ID）；指纹表只由 WS 渲染积累，
  // 补拉渲染由游标保护不入表——入表会让补拉大页把 WS 渲染指纹挤出容量窗口、
  // 反破坏「游标确立前 WS 已渲染」的去重。
  private reconcileCursor: number | null = null;
  private reconcileFingerprints = new Set<string>();
  private reconcileInFlight = false;
  private reconcileQueued = false;
  private remoteAssistPeer: RTCPeerConnection | null = null;
  // 最近一次服务端下发的 ICE 配置（WS webrtc-ice-config 推送或 REST 回退拉取）。
  private serverIceServers: ServifyRTCIceServer[] | null = null;
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
      reconnectPolicy: this.config.reconnectPolicy,
      authProvider: this.config.authProvider,
      onTokenRefreshRequired: this.config.onTokenRefreshRequired,
      webSocketFactory: this.config.webSocketFactory,
      debug: this.config.debug,
    });

    // 转发 WebSocket 事件
    this.ws.on('connected', () => {
      this.emit('connected');
      // D7 流程 3：连接成功即对账断连窗口（首连也拉——固定 sessionId 接入时
      // 回放会话既有历史；新 session 404/空页静默）。异步不阻塞帧消费。
      void this.reconcileMissedMessages();
    });
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
    this.ws.on('webrtc:ice-config', (iceServers) => {
      this.serverIceServers = iceServers;
      this.emit('webrtc:ice-config', iceServers);
    });
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

    const iceServers =
      options?.iceServers || this.config.remoteAssist?.iceServers || (await this.resolveServerIceServers());
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

  // 服务端下发为默认（docs/TURN_DEPLOYMENT.md 切片三）：优先消费 WS 建联推送的
  // webrtc-ice-config 缓存，未拿到时回退 REST 面；两者皆空返回空数组（走 host 候选）。
  // 宿主经 options/config 传入的 iceServers 始终最高优先级。
  private async resolveServerIceServers(): Promise<ServifyRTCIceServer[]> {
    if (this.serverIceServers && this.serverIceServers.length > 0) {
      return this.serverIceServers;
    }
    const response = await this.api.getIceServers();
    const servers =
      response.success && response.data?.ice_servers ? response.data.ice_servers : [];
    this.serverIceServers = servers;
    return servers;
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

  // 客户侧推荐问题（P2-0）：公开路由，无需登录态；首屏在会话建立前后
  // 均可调用，上下文联想以客户最近一条消息为 query。sessionId 用于
  // 服务端曝光/转化归因（P2-0 RQ-5）。
  async getInitialQuestions(limit?: number, options?: { sessionId?: string | number }): Promise<InitialQuestionsResult> {
    const response = await this.api.getInitialQuestions(limit, options);
    if (!response.success || !response.data) {
      throw new Error(response.error || 'Failed to get initial questions');
    }
    return response.data;
  }

  async getNextQuestions(query: string, options?: { sessionId?: string | number; limit?: number }): Promise<NextQuestionsResult> {
    const response = await this.api.getNextQuestions(query, options);
    if (!response.success || !response.data) {
      throw new Error(response.error || 'Failed to get next questions');
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
    // WS 渲染点积累补拉指纹（键 = 服务端 sender 原词 + content，与补拉结果
    // 同键空间；sender_type/is_ai_response 反查与 WS 帧映射一一对应）。
    const sender = message.sender_type === 'system'
      ? (message.is_ai_response ? 'ai' : 'system')
      : message.sender_type;
    this.trackReconcileFingerprint(sender, message.content);
    this.emit('message', message);
  }

  /**
   * 增量补拉（D7 流程 3；§10 #1 端点）：连接成功后对账断连窗口内错过的消息。
   * 逐页 GET /api/v1/sessions/{id}/messages?after_id=<游标>（升序，has_more
   * 续拉），结果以 'message' 事件发出——embedder 渲染面零改动；sender 映射
   * 与 WS 帧口径同构（ai→system+is_ai_response，未知 sender 按 customer 回放）。
   *
   * 全失败面静默：会话行未建过（404，首连/未发过消息的常态）与 IO/HTTP 错误
   * 都不影响 WS 使用，游标不动、下次连接重新对账。游标与服务端 ID 仅在此链内
   * 自持；指纹命中只跳过渲染、游标照常推进（服务端落库事实已确认）。收尾不清
   * 指纹表：对账在途期间到达的 WS 帧会被清表抹掉，随后的重连补拉即重复渲染
   * （Android 侧实测可复现的竞态）；容量环形淘汰足够。
   */
  private async reconcileMissedMessages(): Promise<void> {
    if (this.reconcileInFlight) {
      // 重连风暴下的串行化：在途对账结束后补跑一次，游标保证不重复渲染。
      this.reconcileQueued = true;
      return;
    }
    this.reconcileInFlight = true;
    try {
      let rounds = 0;
      while (rounds++ < RECONCILE_MAX_PAGES) {
        const page = await this.api.getVisitorMessages(
          this.resolveRealtimeSessionID(),
          { afterId: this.reconcileCursor ?? undefined, limit: RECONCILE_PAGE_LIMIT },
        );
        if (!page.success || !page.data || !Array.isArray(page.data.messages)) {
          return; // 404/HTTP/IO/畸形体全静默：不动游标，下次连接重新对账
        }
        for (const entry of page.data.messages) {
          const parsed = this.parseVisitorMessage(entry);
          if (!parsed) continue;
          if (this.reconcileCursor === null || parsed.numericId > this.reconcileCursor) {
            this.reconcileCursor = parsed.numericId;
          }
          const key = `${parsed.sender}|${parsed.content}`;
          if (this.reconcileFingerprints.has(key)) continue;
          this.emit('message', parsed.message);
        }
        if (page.data.has_more !== true) return;
      }
    } finally {
      this.reconcileInFlight = false;
      if (this.reconcileQueued) {
        this.reconcileQueued = false;
        void this.reconcileMissedMessages();
      }
    }
  }

  /**
   * 单条补拉结果解析：id 非数字 / content、sender 形状非法即跳过（条目级防御，
   * 与 Android/iOS 刀 10 镜像——游标契约只认数字单调 ID）。id 前缀 srv- 与
   * WS 渲染的 ws- 本地 id 区分，embedder 可据此幂等去重。
   */
  private parseVisitorMessage(entry: unknown): {
    numericId: number;
    sender: string;
    content: string;
    message: Message;
  } | null {
    if (typeof entry !== 'object' || entry === null) {
      return null;
    }
    const dto = entry as Record<string, unknown>;
    const rawId = typeof dto.id === 'string' ? dto.id.trim() : '';
    const numericId = Number.parseInt(rawId, 10);
    if (rawId === '' || !Number.isSafeInteger(numericId)) {
      return null;
    }
    if (typeof dto.content !== 'string' || typeof dto.sender !== 'string') {
      return null;
    }

    const isAi = dto.sender === 'ai';
    const senderType: Message['sender_type'] = isAi
      ? 'system'
      : dto.sender === 'agent'
        ? 'agent'
        : dto.sender === 'system'
          ? 'system'
          : 'customer';
    return {
      numericId,
      sender: dto.sender,
      content: dto.content,
      message: {
        id: `srv-${numericId}`,
        session_id: typeof dto.conversation_id === 'string' || typeof dto.conversation_id === 'number'
          ? dto.conversation_id
          : this.resolveRealtimeSessionID(),
        sender_type: senderType,
        content: dto.content,
        message_type: 'text',
        is_ai_response: isAi,
        created_at: typeof dto.created_at === 'string' ? dto.created_at : new Date().toISOString(),
      },
    };
  }

  /** 补拉指纹入表（容量环形淘汰，最旧先出）。 */
  private trackReconcileFingerprint(sender: string, content: string): void {
    this.reconcileFingerprints.add(`${sender}|${content}`);
    while (this.reconcileFingerprints.size > RECONCILE_FINGERPRINT_CAPACITY) {
      const oldest = this.reconcileFingerprints.values().next().value;
      if (oldest === undefined) {
        break;
      }
      this.reconcileFingerprints.delete(oldest);
    }
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
