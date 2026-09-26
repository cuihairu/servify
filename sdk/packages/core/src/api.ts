import { ApiResponse, Customer, ChatSession, Message, Ticket, CustomerSatisfaction, InitialQuestionsResult, NextQuestionsResult, ServifyRTCIceServer, TranslationResult, VisitorMessage } from './types';

export interface ApiClientOptions {
  baseUrl: string;
  timeout?: number;
  headers?: Record<string, string>;
  debug?: boolean;
}

function isStructuredApiResponse<T>(value: unknown): value is ApiResponse<T> {
  return typeof value === 'object' && value !== null && 'success' in value;
}

export class ApiClient {
  private options: Required<ApiClientOptions>;

  constructor(options: ApiClientOptions) {
    this.options = {
      timeout: 10000,
      headers: {
        'Content-Type': 'application/json',
      },
      debug: false,
      ...options
    };
  }

  // 通用请求方法
  private async request<T>(
    method: 'GET' | 'POST' | 'PUT' | 'DELETE',
    endpoint: string,
    data?: unknown,
    options?: Partial<ApiClientOptions>
  ): Promise<ApiResponse<T>> {
    const url = `${this.options.baseUrl}${endpoint}`;
    const headers = { ...this.options.headers, ...options?.headers };

    this.log(`${method} ${url}`, data);

    try {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), this.options.timeout);

      const response = await fetch(url, {
        method,
        headers,
        body: data ? JSON.stringify(data) : undefined,
        signal: controller.signal,
      });

      clearTimeout(timeoutId);

      const result = await response.json() as unknown;
      const structuredResult = isStructuredApiResponse<T>(result) ? result : undefined;
      this.log(`响应:`, result);

      if (!response.ok) {
        return {
          success: false,
          error: structuredResult?.error || `HTTP ${response.status}: ${response.statusText}`,
        };
      }

      return {
        success: true,
        data: structuredResult?.data ?? (result as T),
        message: structuredResult?.message,
      };
    } catch (error) {
      this.log(`请求失败:`, error);

      if (error instanceof Error) {
        if (error.name === 'AbortError') {
          return { success: false, error: 'Request timeout' };
        }
        return { success: false, error: error.message };
      }

      return { success: false, error: 'Unknown error' };
    }
  }

  // 客户相关 API
  async getCustomer(customerId: number): Promise<ApiResponse<Customer>> {
    return this.request<Customer>('GET', `/api/customers/${customerId}`);
  }

  async createCustomer(customerData: Partial<Customer>): Promise<ApiResponse<Customer>> {
    return this.request<Customer>('POST', '/api/customers', customerData);
  }

  async updateCustomer(customerId: number, customerData: Partial<Customer>): Promise<ApiResponse<Customer>> {
    return this.request<Customer>('PUT', `/api/customers/${customerId}`, customerData);
  }

  // 会话相关 API
  async createSession(sessionData: Partial<ChatSession>): Promise<ApiResponse<ChatSession>> {
    return this.unsupported<ChatSession>(
      'REST session creation is not exposed by the current server contract. Use the WebSocket chat flow instead.',
      sessionData,
    );
  }

  async getSession(sessionId: string | number): Promise<ApiResponse<ChatSession>> {
    return this.request<ChatSession>('GET', `/api/omni/sessions/${encodeURIComponent(String(sessionId))}`);
  }

  async endSession(sessionId: string | number): Promise<ApiResponse<void>> {
    return this.request<void>('POST', `/api/omni/sessions/${encodeURIComponent(String(sessionId))}/close`);
  }

  async getCustomerSessions(customerId: number): Promise<ApiResponse<ChatSession[]>> {
    return this.request<ChatSession[]>('GET', `/api/customers/${customerId}/sessions`);
  }

  // 消息相关 API
  async sendMessage(messageData: Partial<Message>): Promise<ApiResponse<Message>> {
    const sessionId = messageData.session_id;
    if (sessionId === undefined || sessionId === null || String(sessionId).trim() === '') {
      return { success: false, error: 'session_id is required' };
    }

    return this.request<Message>(
      'POST',
      `/api/omni/sessions/${encodeURIComponent(String(sessionId))}/messages`,
      { content: messageData.content },
    );
  }

  async getSessionMessages(sessionId: string | number, options?: {
    page?: number;
    limit?: number;
  }): Promise<ApiResponse<{ messages: Message[]; total: number; page: number }>> {
    const params = new URLSearchParams();
    if (options?.page) params.append('page', options.page.toString());
    if (options?.limit) params.append('limit', options.limit.toString());

    const query = params.toString() ? `?${params.toString()}` : '';
    const response = await this.request<Message[]>(
      'GET',
      `/api/omni/sessions/${encodeURIComponent(String(sessionId))}/messages${query}`,
    );
    if (!response.success) {
      return {
        success: false,
        error: response.error,
        message: response.message,
      };
    }

    const messages = Array.isArray(response.data) ? response.data : [];
    return {
      success: true,
      data: {
        messages,
        total: messages.length,
        page: options?.page ?? 1,
      },
      message: response.message,
    };
  }

  // 访客消息增量拉取（§10 #1，服务端刀 7 端点）：免认证访客唯一消息读入口，
  // 与 /api/omni 管理面 helpers（访客不可达）分属两条通道。404=会话行未建过
  // （首连常态），非 2xx / IO / 畸形体均归一为 success:false，由调用方
  // （重连对账）按全失败面静默处理。
  async getVisitorMessages(sessionId: string | number, options?: {
    afterId?: number;
    limit?: number;
  }): Promise<ApiResponse<{ messages: VisitorMessage[]; has_more: boolean }>> {
    const params = new URLSearchParams();
    params.append('limit', String(options?.limit ?? 100));
    if (options?.afterId !== undefined) {
      params.append('after_id', String(options.afterId));
    }
    return this.request(
      'GET',
      `/api/v1/sessions/${encodeURIComponent(String(sessionId))}/messages?${params.toString()}`,
    );
  }

  // AI 相关 API
  async askAI(question: string, sessionId?: string | number): Promise<ApiResponse<{ answer: string; confidence: number }>> {
    const response = await this.request<{
      answer?: string;
      content?: string;
      confidence?: number;
    }>('POST', '/api/v1/ai/query', { query: question, session_id: sessionId ? String(sessionId) : '' });
    if (!response.success) {
      return response as ApiResponse<{ answer: string; confidence: number }>;
    }

    return {
      success: true,
      data: {
        answer: response.data?.answer ?? response.data?.content ?? '',
        confidence: response.data?.confidence ?? 0,
      },
      message: response.message,
    };
  }

  async getAIStatus(): Promise<ApiResponse<{ status: string; models: string[] }>> {
    return this.request('GET', '/api/v1/ai/status');
  }

  // 客户侧推荐问题（P2-0）：公开路由，无需登录态
  async getInitialQuestions(limit?: number, options?: { sessionId?: string | number }): Promise<ApiResponse<InitialQuestionsResult>> {
    const params = new URLSearchParams();
    if (limit && limit > 0) {
      params.set('limit', String(limit));
    }
    // 曝光归因（P2-0 RQ-5）：session_id 使服务端把曝光/转化挂到同一会话
    if (options?.sessionId) {
      params.set('session_id', String(options.sessionId));
    }
    const query = params.toString();
    return this.request<InitialQuestionsResult>('GET', `/public/suggestions/initial${query ? `?${query}` : ''}`);
  }

  async getNextQuestions(query: string, options?: { sessionId?: string | number; limit?: number }): Promise<ApiResponse<NextQuestionsResult>> {
    const params = new URLSearchParams();
    const q = query.trim();
    if (q) {
      params.set('query', q);
    }
    if (options?.sessionId) {
      params.set('session_id', String(options.sessionId));
    }
    if (options?.limit && options.limit > 0) {
      params.set('limit', String(options.limit));
    }
    return this.request<NextQuestionsResult>('GET', `/public/suggestions/next?${params.toString()}`);
  }

  // 工单相关 API
  async createTicket(ticketData: Partial<Ticket>): Promise<ApiResponse<Ticket>> {
    return this.request<Ticket>('POST', '/api/tickets', ticketData);
  }

  async getTicket(ticketId: number): Promise<ApiResponse<Ticket>> {
    return this.request<Ticket>('GET', `/api/tickets/${ticketId}`);
  }

  async updateTicket(ticketId: number, updates: Partial<Ticket>): Promise<ApiResponse<Ticket>> {
    return this.request<Ticket>('PUT', `/api/tickets/${ticketId}`, updates);
  }

  async getCustomerTickets(customerId: number): Promise<ApiResponse<Ticket[]>> {
    return this.request<Ticket[]>('GET', `/api/tickets?customer_id=${encodeURIComponent(String(customerId))}`);
  }

  // 满意度评价 API
  async submitSatisfaction(satisfactionData: Partial<CustomerSatisfaction>): Promise<ApiResponse<CustomerSatisfaction>> {
    return this.request<CustomerSatisfaction>('POST', '/api/satisfactions', satisfactionData);
  }

  async getSatisfactionByTicket(ticketId: number): Promise<ApiResponse<CustomerSatisfaction>> {
    return this.request<CustomerSatisfaction>('GET', `/api/tickets/${ticketId}/satisfaction`);
  }

  // 队列相关 API
  async joinQueue(queueData: {
    customer_id: number;
    priority?: string;
    estimated_wait?: number;
  }): Promise<ApiResponse<{ queue_position: number; estimated_wait: number }>> {
    return this.unsupported('Queue join is not exposed by the current server contract.', queueData);
  }

  async getQueueStatus(customerId: number): Promise<ApiResponse<{
    in_queue: boolean;
    position?: number;
    estimated_wait?: number;
  }>> {
    return this.unsupported('Queue status is not exposed by the current server contract.', customerId);
  }

  async leaveQueue(customerId: number): Promise<ApiResponse<void>> {
    return this.unsupported('Queue leave is not exposed by the current server contract.', customerId);
  }

  // 文件上传 API
  async uploadFile(file: File, sessionId: string | number): Promise<ApiResponse<{
    file_url: string;
    file_name: string;
    file_size: number;
  }>> {
    const formData = new FormData();
    formData.append('file', file);
    formData.append('session_id', sessionId.toString());

    const url = `${this.options.baseUrl}/api/v1/upload`;

    try {
      const response = await fetch(url, {
        method: 'POST',
        body: formData,
        headers: {
          // 不设置 Content-Type，让浏览器自动设置 multipart/form-data 边界
          ...Object.fromEntries(
            Object.entries(this.options.headers).filter(([key]) =>
              key.toLowerCase() !== 'content-type'
            )
          )
        }
      });

      const result = await response.json() as unknown;
      const structuredResult = isStructuredApiResponse<{
        file_url: string;
        file_name: string;
        file_size: number;
      }>(result) ? result : undefined;

      if (!response.ok) {
        return { success: false, error: structuredResult?.error || 'Upload failed' };
      }

      return {
        success: true,
        data: structuredResult?.data ?? result as {
          file_url: string;
          file_name: string;
          file_size: number;
        },
      };
    } catch (error) {
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Upload failed'
      };
    }
  }

  // 远程协助：访客回写录制元数据（recording_key 为 upload 返回的 URL）
  async attachRemoteAssistRecording(
    assistId: string | number,
    meta: {
      recording_key: string;
      recording_mime?: string;
      recording_duration_ms?: number;
      recording_size?: number;
    },
  ): Promise<ApiResponse<void>> {
    return this.request<void>(
      'POST',
      `/api/v1/remote-assist/${encodeURIComponent(String(assistId))}/recording`,
      meta,
    );
  }

  // 聊天文本翻译（Phase 0.5，docs/realtime-translation-design.md）：单条消息
  // 粒度翻译端点，坐席与访客两面共用（AuthMiddleware 认证即可）。访客面
  // token 由 guest session 签发、经 options.token 注入 Authorization 头；
  // 空 text/target_lang 客户端先行拒绝（与服务端 400 语义一致，省一次往返）。
  async translateText(text: string, targetLang: string, options?: {
    sourceLang?: string;
    token?: string;
  }): Promise<ApiResponse<TranslationResult>> {
    if (!text.trim()) {
      return { success: false, error: 'text is required' };
    }
    if (!targetLang.trim()) {
      return { success: false, error: 'target_lang is required' };
    }
    const headers: Record<string, string> | undefined = options?.token
      ? { Authorization: `Bearer ${options.token}` }
      : undefined;
    return this.request<TranslationResult>('POST', '/api/v1/translation/translate', {
      text,
      target_lang: targetLang,
      ...(options?.sourceLang ? { source_lang: options.sourceLang } : {}),
    }, headers ? { headers } : undefined);
  }

  // ICE 配置下发（docs/TURN_DEPLOYMENT.md 切片三）：取代旧 startCall/endCall/
  // getCallStatus 的 unsupported 桩——服务端契约只有 ICE 配置面，呼叫模型从未存在；
  // 信令本身走 WebSocket（webrtc-offer/webrtc-answer/webrtc-candidate）。
  async getIceServers(): Promise<ApiResponse<{ ice_servers: ServifyRTCIceServer[] }>> {
    return this.request<{ ice_servers: ServifyRTCIceServer[] }>('GET', '/api/v1/rtc/ice-servers');
  }

  // 设置认证头
  setAuthToken(token: string): void {
    this.options.headers['Authorization'] = `Bearer ${token}`;
  }

  // 设置客户 ID
  setCustomerId(customerId: number): void {
    this.options.headers['X-Customer-ID'] = customerId.toString();
  }

  private log(...args: unknown[]): void {
    if (this.options.debug) {
      console.warn('[ServifyAPI]', ...args);
    }
  }

  private unsupported<T>(error: string, context?: unknown): Promise<ApiResponse<T>> {
    this.log(error, context);
    return Promise.resolve({ success: false, error });
  }
}
