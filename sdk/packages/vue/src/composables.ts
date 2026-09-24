import { ref, onMounted, onUnmounted } from 'vue';
import { useServify } from './plugin';
import {
  ChatSession,
  Message,
  Agent,
  Ticket,
  RemoteAssistStartOptions,
  RemoteAssistState,
  ServifyRTCTrackEvent,
  AiStreamDeltaUpdate,
  AiStreamEndUpdate,
} from '@servify/core';

// 流式气泡的伪 Message：ai-stream:delta 按 id upsert（内容为累计全量），
// end(interrupted=false) 移除让位终帧、true 定格内容并追加重试提示行。
function upsertMessage(list: Message[], next: Message): void {
  const index = list.findIndex((m) => m.id === next.id);
  if (index === -1) {
    list.push(next);
  } else {
    list[index] = next;
  }
}

function streamBubble(id: string, content: string): Message {
  return {
    id,
    session_id: '',
    sender_type: 'system',
    content,
    message_type: 'text',
    is_ai_response: true,
    metadata: {},
    created_at: new Date().toISOString(),
  };
}

export function useChat() {
  const sdk = useServify();

  // 响应式状态
  const session = ref<ChatSession | null>(null);
  const messages = ref<Message[]>([]);
  const agent = ref<Agent | null>(null);
  const isLoading = ref(false);
  const error = ref<Error | null>(null);

  // 开始聊天
  const startChat = async (options?: {
    priority?: 'low' | 'normal' | 'high' | 'urgent';
    message?: string;
  }) => {
    try {
      isLoading.value = true;
      error.value = null;
      const newSession = await sdk.startChat(options);
      session.value = newSession;
    } catch (err) {
      error.value = err as Error;
      throw err;
    } finally {
      isLoading.value = false;
    }
  };

  // 发送消息
  const sendMessage = async (content: string, options?: {
    type?: 'text' | 'image' | 'file';
    attachments?: string[];
  }) => {
    try {
      error.value = null;
      await sdk.sendMessage(content, options);
    } catch (err) {
      error.value = err as Error;
      throw err;
    }
  };

  // 结束聊天
  const endChat = async () => {
    try {
      error.value = null;
      await sdk.endSession();
      session.value = null;
      messages.value = [];
      agent.value = null;
    } catch (err) {
      error.value = err as Error;
      throw err;
    }
  };

  // 加载历史消息
  const loadMessages = async (page: number = 1, limit: number = 50) => {
    try {
      error.value = null;
      const result = await sdk.getMessages({ page, limit });
      if (page === 1) {
        messages.value = result.messages;
      } else {
        messages.value.push(...result.messages);
      }
    } catch (err) {
      error.value = err as Error;
      throw err;
    }
  };

  // 上传文件
  const uploadFile = async (file: File) => {
    try {
      error.value = null;
      const result = await sdk.uploadFile(file);
      return {
        fileUrl: result.file_url,
        fileName: result.file_name,
        fileSize: result.file_size,
      };
    } catch (err) {
      error.value = err as Error;
      throw err;
    }
  };

  // 事件处理函数
  const handleMessage = (message: Message) => {
    messages.value.push(message);
  };

  const handleSessionCreated = (newSession: ChatSession) => {
    session.value = newSession;
  };

  const handleSessionEnded = () => {
    session.value = null;
    messages.value = [];
    agent.value = null;
  };

  const handleError = (errorEvent: Error) => {
    error.value = errorEvent;
  };

  const handleStreamDelta = (update: AiStreamDeltaUpdate) => {
    upsertMessage(messages.value, streamBubble(update.id, update.content));
  };

  const handleStreamEnd = (update: AiStreamEndUpdate) => {
    if (!update.interrupted) {
      // ai-response 终帧 message 已先入列，流式气泡让位
      messages.value = messages.value.filter((m) => m.id !== update.id);
      return;
    }
    // 流中断：气泡定格部分内容 + 重试提示行（对齐 Android D8；协议无此帧，SDK 自造 UI 行）
    upsertMessage(messages.value, streamBubble(update.id, update.content ?? ''));
    upsertMessage(messages.value, {
      id: `${update.id}-hint`,
      session_id: '',
      sender_type: 'system',
      content: '回答中断，请重试',
      message_type: 'text',
      is_ai_response: false,
      metadata: {},
      created_at: new Date().toISOString(),
    });
  };

  // 生命周期
  onMounted(() => {
    // 注册事件监听器
    sdk.on('message', handleMessage);
    sdk.on('session_created', handleSessionCreated);
    sdk.on('session_ended', handleSessionEnded);
    sdk.on('error', handleError);
    sdk.on('ai-stream:delta', handleStreamDelta);
    sdk.on('ai-stream:end', handleStreamEnd);

    // 获取当前状态
    session.value = sdk.getSession();
    agent.value = sdk.getAgent();
  });

  onUnmounted(() => {
    // 移除事件监听器
    sdk.off('message', handleMessage);
    sdk.off('session_created', handleSessionCreated);
    sdk.off('session_ended', handleSessionEnded);
    sdk.off('error', handleError);
    sdk.off('ai-stream:delta', handleStreamDelta);
    sdk.off('ai-stream:end', handleStreamEnd);
  });

  return {
    // 状态
    session,
    messages,
    agent,
    isLoading,
    error,

    // 方法
    startChat,
    sendMessage,
    endChat,
    loadMessages,
    uploadFile,
  };
}

// AI 相关的组合式 API
export function useAI() {
  const sdk = useServify();
  const isLoading = ref(false);
  const error = ref<Error | null>(null);

  const askAI = async (question: string) => {
    try {
      isLoading.value = true;
      error.value = null;
      const result = await sdk.askAI(question);
      return result;
    } catch (err) {
      error.value = err as Error;
      throw err;
    } finally {
      isLoading.value = false;
    }
  };

  return {
    isLoading,
    error,
    askAI,
  };
}

// 工单相关的组合式 API
export function useTickets() {
  const sdk = useServify();
  const tickets = ref<Ticket[]>([]);
  const isLoading = ref(false);
  const error = ref<Error | null>(null);

  const createTicket = async (data: {
    title: string;
    description: string;
    priority?: 'low' | 'normal' | 'high' | 'urgent';
    category: string;
  }) => {
    try {
      error.value = null;
      const ticket = await sdk.createTicket(data);
      tickets.value.unshift(ticket);
      return ticket;
    } catch (err) {
      error.value = err as Error;
      throw err;
    }
  };

  return {
    tickets,
    isLoading,
    error,
    createTicket,
  };
}

// 满意度评价相关的组合式 API
export function useSatisfaction() {
  const sdk = useServify();
  const isLoading = ref(false);
  const error = ref<Error | null>(null);

  const submitRating = async (data: {
    ticketId?: number;
    rating: number;
    comment?: string;
    category?: string;
  }) => {
    try {
      isLoading.value = true;
      error.value = null;
      const result = await sdk.submitSatisfaction({
        ticket_id: data.ticketId,
        rating: data.rating,
        comment: data.comment,
        category: data.category,
      });
      return result;
    } catch (err) {
      error.value = err as Error;
      throw err;
    } finally {
      isLoading.value = false;
    }
  };

  return {
    isLoading,
    error,
    submitRating,
  };
}

export function useRemoteAssist() {
  const sdk = useServify();
  const state = ref<RemoteAssistState>('idle');
  const isActive = ref(false);
  const error = ref<Error | null>(null);
  const remoteStream = ref<MediaStream | null>(null);

  const syncActive = (nextState: RemoteAssistState) => {
    isActive.value =
      nextState === 'starting' ||
      nextState === 'offered' ||
      nextState === 'connecting' ||
      nextState === 'connected';
  };

  const startRemoteAssist = async (options?: RemoteAssistStartOptions) => {
    try {
      error.value = null;
      await sdk.startRemoteAssist(options);
    } catch (err) {
      error.value = err as Error;
      throw err;
    }
  };

  const acceptRemoteAnswer = async (answer: RTCSessionDescriptionInit) => {
    try {
      error.value = null;
      await sdk.acceptRemoteAnswer(answer);
    } catch (err) {
      error.value = err as Error;
      throw err;
    }
  };

  const addRemoteIce = async (candidate: RTCIceCandidateInit) => {
    try {
      error.value = null;
      await sdk.addRemoteIce(candidate);
    } catch (err) {
      error.value = err as Error;
      throw err;
    }
  };

  const endRemoteAssist = async () => {
    try {
      error.value = null;
      await sdk.endRemoteAssist();
      remoteStream.value = null;
    } catch (err) {
      error.value = err as Error;
      throw err;
    }
  };

  const handleState = (nextState: RemoteAssistState) => {
    state.value = nextState;
    syncActive(nextState);
    if (nextState === 'idle' || nextState === 'ended' || nextState === 'failed') {
      remoteStream.value = null;
    }
  };

  const handleAnswer = (answer: RTCSessionDescriptionInit) => {
    void acceptRemoteAnswer(answer).catch(() => undefined);
  };

  const handleCandidate = (candidate: RTCIceCandidateInit) => {
    void addRemoteIce(candidate).catch(() => undefined);
  };

  const handleTrack = (event: ServifyRTCTrackEvent) => {
    const [stream] = event.streams;
    if (stream) {
      // DOM 宿主的 streams 元素即 MediaStream；core 契约持最小结构面
      remoteStream.value = stream as MediaStream;
    }
  };

  const handleError = (err: Error) => {
    error.value = err;
  };

  onMounted(() => {
    sdk.on('webrtc:state', handleState);
    sdk.on('webrtc:answer', handleAnswer);
    sdk.on('webrtc:candidate', handleCandidate);
    sdk.on('webrtc:track', handleTrack);
    sdk.on('error', handleError);
  });

  onUnmounted(() => {
    sdk.off('webrtc:state', handleState);
    sdk.off('webrtc:answer', handleAnswer);
    sdk.off('webrtc:candidate', handleCandidate);
    sdk.off('webrtc:track', handleTrack);
    sdk.off('error', handleError);
  });

  return {
    state,
    isActive,
    error,
    remoteStream,
    startRemoteAssist,
    acceptRemoteAnswer,
    addRemoteIce,
    endRemoteAssist,
  };
}
