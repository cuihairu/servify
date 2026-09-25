import { useCallback, useEffect, useState } from 'react';
import type { ChatSession, Message, Ticket, AiStreamDeltaUpdate, AiStreamEndUpdate, TransferAssignmentUpdate, TransferWaitingUpdate } from '@servify/core';
import { useServify } from './RNProvider';

// 流式气泡的伪 Message：ai-stream:delta 按 id upsert（内容为累计全量），
// end(interrupted=false) 移除让位终帧、true 定格内容并追加重试提示行。
function upsertMessage(prev: Message[], next: Message): Message[] {
  const index = prev.findIndex((m) => m.id === next.id);
  if (index === -1) return [...prev, next];
  const copy = [...prev];
  copy[index] = next;
  return copy;
}

function removeMessage(prev: Message[], id: string): Message[] {
  return prev.filter((m) => m.id !== id);
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

export interface UseChatReturn {
  session: ChatSession | null;
  messages: Message[];
  /** 最近一次转人工通知（transfer_notification：坐席已接入），null = 尚未转人工。 */
  agentAssigned: TransferAssignmentUpdate | null;
  /** 最近一次排队通知（waiting_notification：已入等待队列），null = 未排队。 */
  waitingInQueue: TransferWaitingUpdate | null;
  /** 未读数（§4.3；面板不可见时到达的坐席/AI 内容，可见即清零）。 */
  unreadCount: number;
  isLoading: boolean;
  error: Error | null;
  startChat: (options?: { priority?: 'low' | 'normal' | 'high' | 'urgent'; message?: string }) => Promise<void>;
  sendMessage: (content: string, options?: { type?: 'text' | 'image' | 'file'; attachments?: string[] }) => Promise<void>;
  endChat: () => Promise<void>;
  /** 会话页可见性接线（§4.3 unreadCount；对齐移动端 onSessionVisible/Hidden）。 */
  markSessionVisible: () => void;
  markSessionHidden: () => void;
}

export function useChat(): UseChatReturn {
  const { sdk } = useServify();
  const [session, setSession] = useState<ChatSession | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  // 转人工通知（对齐移动端 agentAssigned/waitingInQueue），不渲染消息行
  const [agentAssigned, setAgentAssigned] = useState<TransferAssignmentUpdate | null>(null);
  const [waitingInQueue, setWaitingInQueue] = useState<TransferWaitingUpdate | null>(null);
  const [unreadCount, setUnreadCount] = useState(0);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  // 会话页可见性接线（§4.3 unreadCount；对齐移动端 onSessionVisible/Hidden）
  const markSessionVisible = useCallback(() => sdk?.markSessionVisible(), [sdk]);
  const markSessionHidden = useCallback(() => sdk?.markSessionHidden(), [sdk]);

  const startChat = useCallback(async (options?: {
    priority?: 'low' | 'normal' | 'high' | 'urgent';
    message?: string;
  }) => {
    if (!sdk) {
      throw new Error('SDK not initialized');
    }

    try {
      setIsLoading(true);
      setError(null);
      const nextSession = await sdk.startChat(options);
      setSession(nextSession);
    } catch (err) {
      setError(err as Error);
      throw err;
    } finally {
      setIsLoading(false);
    }
  }, [sdk]);

  const sendMessage = useCallback(async (
    content: string,
    options?: { type?: 'text' | 'image' | 'file'; attachments?: string[] },
  ) => {
    if (!sdk) {
      throw new Error('SDK not initialized');
    }

    try {
      setError(null);
      await sdk.sendMessage(content, options);
    } catch (err) {
      setError(err as Error);
      throw err;
    }
  }, [sdk]);

  const endChat = useCallback(async () => {
    if (!sdk) {
      throw new Error('SDK not initialized');
    }

    try {
      setError(null);
      await sdk.endSession();
      setSession(null);
      setMessages([]);
      setAgentAssigned(null);
      setWaitingInQueue(null);
    } catch (err) {
      setError(err as Error);
      throw err;
    }
  }, [sdk]);

  useEffect(() => {
    if (!sdk) {
      return;
    }

    const handleMessage = (message: Message) => {
      setMessages((prev) => [...prev, message]);
    };
    const handleSessionCreated = (nextSession: ChatSession) => {
      setSession(nextSession);
    };
    const handleSessionEnded = () => {
      setSession(null);
      setMessages([]);
      setAgentAssigned(null);
      setWaitingInQueue(null);
    };

    // 转人工通知：坐席接入（含等待队列派发）时清排队态（waiting_human → agent_chatting）
    const handleTransferAssigned = (update: TransferAssignmentUpdate) => {
      setAgentAssigned(update);
      setWaitingInQueue(null);
    };

    const handleTransferWaiting = (update: TransferWaitingUpdate) => {
      setWaitingInQueue(update);
    };

    const handleUnreadChange = (count: number) => {
      setUnreadCount(count);
    };
    const handleError = (nextError: Error) => {
      setError(nextError);
    };

    const handleStreamDelta = (update: AiStreamDeltaUpdate) => {
      setMessages((prev) => upsertMessage(prev, streamBubble(update.id, update.content)));
    };

    const handleStreamEnd = (update: AiStreamEndUpdate) => {
      setMessages((prev) => {
        if (!update.interrupted) {
          // ai-response 终帧 message 已先入列，流式气泡让位
          return removeMessage(prev, update.id);
        }
        // 流中断：气泡定格部分内容 + 重试提示行（对齐 Android D8；协议无此帧，SDK 自造 UI 行）
        let next = upsertMessage(prev, streamBubble(update.id, update.content ?? ''));
        next = upsertMessage(next, {
          id: `${update.id}-hint`,
          session_id: '',
          sender_type: 'system',
          content: '回答中断，请重试',
          message_type: 'text',
          is_ai_response: false,
          metadata: {},
          created_at: new Date().toISOString(),
        });
        return next;
      });
    };

    sdk.on('message', handleMessage);
    sdk.on('session_created', handleSessionCreated);
    sdk.on('session_ended', handleSessionEnded);
    sdk.on('error', handleError);
    sdk.on('ai-stream:delta', handleStreamDelta);
    sdk.on('ai-stream:end', handleStreamEnd);
    sdk.on('transfer:assigned', handleTransferAssigned);
    sdk.on('transfer:waiting', handleTransferWaiting);
    sdk.on('unread-change', handleUnreadChange);

    return () => {
      sdk.off('message', handleMessage);
      sdk.off('session_created', handleSessionCreated);
      sdk.off('session_ended', handleSessionEnded);
      sdk.off('error', handleError);
      sdk.off('ai-stream:delta', handleStreamDelta);
      sdk.off('ai-stream:end', handleStreamEnd);
      sdk.off('transfer:assigned', handleTransferAssigned);
      sdk.off('transfer:waiting', handleTransferWaiting);
      sdk.off('unread-change', handleUnreadChange);
    };
  }, [sdk]);

  return {
    session,
    messages,
    agentAssigned,
    waitingInQueue,
    unreadCount,
    isLoading,
    error,
    startChat,
    sendMessage,
    endChat,
    markSessionVisible,
    markSessionHidden,
  };
}

export interface UseTicketsReturn {
  tickets: Ticket[];
  isLoading: boolean;
  error: Error | null;
  createTicket: (data: {
    title: string;
    description: string;
    priority?: 'low' | 'normal' | 'high' | 'urgent';
    category: string;
  }) => Promise<Ticket>;
}

export function useTickets(): UseTicketsReturn {
  const { sdk } = useServify();
  const [tickets, setTickets] = useState<Ticket[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const createTicket = useCallback(async (data: {
    title: string;
    description: string;
    priority?: 'low' | 'normal' | 'high' | 'urgent';
    category: string;
  }) => {
    if (!sdk) {
      throw new Error('SDK not initialized');
    }

    try {
      setIsLoading(true);
      setError(null);
      const ticket = await sdk.createTicket(data);
      setTickets((prev) => [ticket, ...prev]);
      return ticket;
    } catch (err) {
      setError(err as Error);
      throw err;
    } finally {
      setIsLoading(false);
    }
  }, [sdk]);

  return { tickets, isLoading, error, createTicket };
}

export interface UseAIReturn {
  isLoading: boolean;
  error: Error | null;
  askAI: (question: string) => Promise<{ answer: string; confidence: number }>;
}

export function useAI(): UseAIReturn {
  const { sdk } = useServify();
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const askAI = useCallback(async (question: string) => {
    if (!sdk) {
      throw new Error('SDK not initialized');
    }

    try {
      setIsLoading(true);
      setError(null);
      return await sdk.askAI(question);
    } catch (err) {
      setError(err as Error);
      throw err;
    } finally {
      setIsLoading(false);
    }
  }, [sdk]);

  return { isLoading, error, askAI };
}

export interface UseSatisfactionReturn {
  isLoading: boolean;
  error: Error | null;
  submitRating: (data: {
    ticketId?: number;
    rating: number;
    comment?: string;
    category?: string;
  }) => Promise<unknown>;
}

export function useSatisfaction(): UseSatisfactionReturn {
  const { sdk } = useServify();
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const submitRating = useCallback(async (data: {
    ticketId?: number;
    rating: number;
    comment?: string;
    category?: string;
  }) => {
    if (!sdk) {
      throw new Error('SDK not initialized');
    }

    try {
      setIsLoading(true);
      setError(null);
      return await sdk.submitSatisfaction({
        ticket_id: data.ticketId,
        rating: data.rating,
        comment: data.comment,
        category: data.category,
      });
    } catch (err) {
      setError(err as Error);
      throw err;
    } finally {
      setIsLoading(false);
    }
  }, [sdk]);

  return { isLoading, error, submitRating };
}
