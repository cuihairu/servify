import {
  createWebServifySDK,
  type WebServifyClient,
  type WebServifyConfig,
  type Message,
  type Customer,
  type ChatSession,
  type Agent,
  type Ticket,
  type CustomerSatisfaction,
  type RemoteAssistStartOptions,
} from '@servify/core';

/**
 * 为原生 JavaScript 提供更简单的 API 接口
 */
export class VanillaServifySDK {
  private sdk: WebServifyClient;
  private eventCallbacks: Map<string, Array<(...args: unknown[]) => void>> = new Map();

  private normalizePriority(priority?: string): ChatSession['priority'] | undefined {
    if (priority === 'low' || priority === 'normal' || priority === 'high' || priority === 'urgent') {
      return priority;
    }

    return undefined;
  }

  private normalizeMessageType(type: string): 'text' | 'image' | 'file' {
    if (type === 'image' || type === 'file') {
      return type;
    }

    return 'text';
  }

  constructor(config: WebServifyConfig) {
    this.sdk = createWebServifySDK(config);

    // 转发所有事件到回调函数
    this.sdk.on('connected', () => this.triggerCallback('connected'));
    this.sdk.on('disconnected', (reason) => this.triggerCallback('disconnected', reason));
    this.sdk.on('message', (message) => this.triggerCallback('message', message));
    this.sdk.on('session_created', (session) => this.triggerCallback('sessionCreated', session));
    this.sdk.on('session_updated', (session) => this.triggerCallback('sessionUpdated', session));
    this.sdk.on('session_ended', (session) => this.triggerCallback('sessionEnded', session));
    this.sdk.on('agent_assigned', (agent) => this.triggerCallback('agentAssigned', agent));
    this.sdk.on('agent_typing', (isTyping) => this.triggerCallback('agentTyping', isTyping));
    this.sdk.on('error', (error) => this.triggerCallback('error', error));
    this.sdk.on('ticket_created', (ticket) => this.triggerCallback('ticketCreated', ticket));
    this.sdk.on('webrtc:offer', (offer) => this.triggerCallback('webrtc:offer', offer));
    this.sdk.on('webrtc:answer', (answer) => this.triggerCallback('webrtc:answer', answer));
    this.sdk.on('webrtc:candidate', (candidate) => this.triggerCallback('webrtc:candidate', candidate));
    this.sdk.on('webrtc:track', (event) => this.triggerCallback('webrtc:track', event));
    this.sdk.on('webrtc:state', (state) => this.triggerCallback('webrtc:state', state));
    this.sdk.on('remote-assist:session', (assistId) => this.triggerCallback('remote-assist:session', assistId));
    this.sdk.on('remote-assist:recording', (state) => this.triggerCallback('remote-assist:recording', state));
  }

  /**
   * 初始化 SDK
   */
  async init(): Promise<void> {
    return this.sdk.initialize();
  }

  /**
   * 连接到服务器
   */
  async connect(): Promise<void> {
    return this.sdk.connect();
  }

  /**
   * 断开连接
   */
  disconnect(): void {
    this.sdk.disconnect();
  }

  /**
   * 开始聊天
   */
  async startChat(options?: {
    priority?: string;
    message?: string;
  }): Promise<ChatSession> {
    return this.sdk.startChat({
      priority: this.normalizePriority(options?.priority),
      message: options?.message,
    });
  }

  /**
   * 发送消息
   */
  async sendMessage(content: string, type: string = 'text'): Promise<Message> {
    return this.sdk.sendMessage(content, { type: this.normalizeMessageType(type) });
  }

  /**
   * 结束会话
   */
  async endChat(): Promise<void> {
    return this.sdk.endSession();
  }

  /**
   * 发起远程协助基础链路
   */
  async startRemoteAssist(options?: RemoteAssistStartOptions): Promise<RTCPeerConnection> {
    return this.sdk.startRemoteAssist(options);
  }

  /**
   * 接收对端 WebRTC answer
   */
  async acceptRemoteAnswer(answer: RTCSessionDescriptionInit): Promise<void> {
    return this.sdk.acceptRemoteAnswer(answer);
  }

  /**
   * 注入对端 ICE candidate
   */
  async addRemoteIce(candidate: RTCIceCandidateInit): Promise<void> {
    return this.sdk.addRemoteIce(candidate);
  }

  /**
   * 结束远程协助
   */
  async endRemoteAssist(): Promise<void> {
    return this.sdk.endRemoteAssist();
  }

  /**
   * 显式登记远程协助会话 ID（录制上传时回写该会话）
   */
  setRemoteAssistSession(assistId: string | number): void {
    this.sdk.setRemoteAssistSession(assistId);
  }

  /**
   * 当前绑定的远程协助会话 ID
   */
  getRemoteAssistSession(): string | null {
    return this.sdk.getRemoteAssistSession();
  }

  /**
   * AI 问答
   */
  async askAI(question: string): Promise<{ answer: string; confidence: number }> {
    return this.sdk.askAI(question);
  }

  /**
   * 上传文件
   */
  async uploadFile(file: File): Promise<{ fileUrl: string; fileName: string; fileSize: number }> {
    const result = await this.sdk.uploadFile(file);
    return {
      fileUrl: result.file_url,
      fileName: result.file_name,
      fileSize: result.file_size,
    };
  }

  /**
   * 创建工单
   */
  async createTicket(data: {
    title: string;
    description: string;
    priority?: string;
    category: string;
  }): Promise<Ticket> {
    return this.sdk.createTicket({
      ...data,
      priority: this.normalizePriority(data.priority),
    });
  }

  /**
   * 提交满意度评价
   */
  async submitRating(rating: number, comment?: string): Promise<CustomerSatisfaction> {
    return this.sdk.submitSatisfaction({
      rating,
      comment,
    });
  }

  /**
   * 获取历史消息
   */
  async getMessages(page: number = 1, limit: number = 50): Promise<{
    messages: Message[];
    total: number;
    page: number;
  }> {
    return this.sdk.getMessages({ page, limit });
  }

  /**
   * 获取客户信息
   */
  getCustomer(): Customer | null {
    return this.sdk.getCustomer();
  }

  /**
   * 获取当前会话
   */
  getSession(): ChatSession | null {
    return this.sdk.getSession();
  }

  /**
   * 获取当前客服代理
   */
  getAgent(): Agent | null {
    return this.sdk.getAgent();
  }

  /**
   * 检查连接状态
   */
  isConnected(): boolean {
    return this.sdk.isConnected();
  }

  /**
   * 添加事件监听器（简化版）
   */
  on(event: string, callback: (...args: unknown[]) => void): void {
    if (!this.eventCallbacks.has(event)) {
      this.eventCallbacks.set(event, []);
    }
    this.eventCallbacks.get(event)!.push(callback);
  }

  /**
   * 移除事件监听器
   */
  off(event: string, callback?: (...args: unknown[]) => void): void {
    if (!callback) {
      this.eventCallbacks.delete(event);
      return;
    }

    const callbacks = this.eventCallbacks.get(event);
    if (callbacks) {
      const index = callbacks.indexOf(callback);
      if (index > -1) {
        callbacks.splice(index, 1);
      }
    }
  }

  /**
   * 触发回调函数
   */
  private triggerCallback(event: string, ...args: unknown[]): void {
    const callbacks = this.eventCallbacks.get(event);
    if (callbacks) {
      callbacks.forEach(callback => {
        try {
          callback(...args);
        } catch (error) {
          console.warn(`Error in ${event} callback:`, error);
        }
      });
    }
  }
}

// 全局变量注册（用于浏览器环境）
declare global {
  interface Window {
    Servify: typeof VanillaServifySDK;
    createServify: (config: WebServifyConfig) => VanillaServifySDK;
  }
}

// 浏览器环境下的全局注册
if (typeof window !== 'undefined') {
  window.Servify = VanillaServifySDK;
  window.createServify = (config: WebServifyConfig) => new VanillaServifySDK(config);
}

export type { WebServifyConfig as ServifyConfig, Message, Customer, ChatSession, Agent } from '@servify/core';
export default VanillaServifySDK;
