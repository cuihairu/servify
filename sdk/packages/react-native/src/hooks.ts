import { useCallback, useEffect, useState } from 'react';
import type { ChatSession, Message, Ticket } from '@servify/core';
import { useServify } from './RNProvider';

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

    sdk.on('message', handleMessage);
    sdk.on('session_created', handleSessionCreated);
    sdk.on('session_ended', handleSessionEnded);
    sdk.on('error', handleError);

    return () => {
      sdk.off('message', handleMessage);
      sdk.off('session_created', handleSessionCreated);
      sdk.off('session_ended', handleSessionEnded);
      sdk.off('error', handleError);
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
