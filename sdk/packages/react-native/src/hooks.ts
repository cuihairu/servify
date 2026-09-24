import { useCallback, useEffect, useState } from 'react';
import type { ChatSession, Message, Ticket, AiStreamDeltaUpdate, AiStreamEndUpdate } from '@servify/core';
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
  isLoading: boolean;
  error: Error | null;
  startChat: (options?: { priority?: 'low' | 'normal' | 'high' | 'urgent'; message?: string }) => Promise<void>;
  sendMessage: (content: string, options?: { type?: 'text' | 'image' | 'file'; attachments?: string[] }) => Promise<void>;
  endChat: () => Promise<void>;
}

export function useChat(): UseChatReturn {
  const { sdk } = useServify();
  const [session, setSession] = useState<ChatSession | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

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

    return () => {
      sdk.off('message', handleMessage);
      sdk.off('session_created', handleSessionCreated);
      sdk.off('session_ended', handleSessionEnded);
      sdk.off('error', handleError);
      sdk.off('ai-stream:delta', handleStreamDelta);
      sdk.off('ai-stream:end', handleStreamEnd);
    };
  }, [sdk]);

  return {
    session,
    messages,
    isLoading,
    error,
    startChat,
    sendMessage,
    endChat,
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
