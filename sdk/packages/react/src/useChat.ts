import { useState, useEffect, useCallback } from 'react';
import { useServify } from './ServifyProvider';
import { ChatSession, Message, Agent } from '@servify/core';

export interface UseChatReturn {
  // 状态
  session: ChatSession | null;
  messages: Message[];
  agent: Agent | null;
  isLoading: boolean;
  error: Error | null;

  // 方法
  startChat: (options?: {
    priority?: 'low' | 'normal' | 'high' | 'urgent';
    message?: string;
  }) => Promise<void>;
  sendMessage: (content: string, options?: {
    type?: 'text' | 'image' | 'file';
    attachments?: string[];
  }) => Promise<void>;
  endChat: () => Promise<void>;
  loadMessages: (page?: number, limit?: number) => Promise<void>;
  uploadFile: (file: File) => Promise<{ fileUrl: string; fileName: string; fileSize: number }>;
}

export function useChat(): UseChatReturn {
  const { sdk } = useServify();
  const [session, setSession] = useState<ChatSession | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [agent, setAgent] = useState<Agent | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  // 开始聊天
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
      const newSession = await sdk.startChat(options);
      setSession(newSession);
    } catch (err) {
      setError(err as Error);
    } finally {
      setIsLoading(false);
    }
  }, [sdk]);

  // 发送消息
  const sendMessage = useCallback(async (content: string, options?: {
    type?: 'text' | 'image' | 'file';
    attachments?: string[];
  }) => {
    if (!sdk) {
      throw new Error('SDK not initialized');
    }

    try {
      setError(null);
      await sdk.sendMessage(content, options);
    } catch (err) {
      setError(err as Error);
    }
  }, [sdk]);

  // 结束聊天
  const endChat = useCallback(async () => {
    if (!sdk) {
      throw new Error('SDK not initialized');
    }

    try {
      setError(null);
      await sdk.endSession();
      setSession(null);
      setMessages([]);
      setAgent(null);
    } catch (err) {
      setError(err as Error);
    }
  }, [sdk]);

  // 加载历史消息
  const loadMessages = useCallback(async (page: number = 1, limit: number = 50) => {
    if (!sdk) {
      throw new Error('SDK not initialized');
    }

    try {
      setError(null);
      const result = await sdk.getMessages({ page, limit });
      if (page === 1) {
        setMessages(result.messages);
      } else {
        setMessages(prev => [...prev, ...result.messages]);
      }
    } catch (err) {
      setError(err as Error);
    }
  }, [sdk]);

  // 上传文件
  const uploadFile = useCallback(async (file: File) => {
    if (!sdk) {
      throw new Error('SDK not initialized');
    }

    try {
      setError(null);
      const result = await sdk.uploadFile(file);
      return {
        fileUrl: result.file_url,
        fileName: result.file_name,
        fileSize: result.file_size,
      };
    } catch (err) {
      setError(err as Error);
      throw err;
    }
  }, [sdk]);

  // 设置事件监听器
  useEffect(() => {
    if (!sdk) return;

    const handleMessage = (message: Message) => {
      setMessages(prev => [...prev, message]);
    };

    const handleSessionCreated = (session: ChatSession) => {
      setSession(session);
    };

    const handleSessionEnded = (_session: ChatSession) => {
      setSession(null);
      setMessages([]);
      setAgent(null);
    };

    const handleError = (error: Error) => {
      setError(error);
    };

    // 注册事件监听器
    sdk.on('message', handleMessage);
    sdk.on('session_created', handleSessionCreated);
    sdk.on('session_ended', handleSessionEnded);
    sdk.on('error', handleError);

    // 获取当前状态
    setSession(sdk.getSession());
    setAgent(sdk.getAgent());

    // 清理函数
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
    agent,
    isLoading,
    error,
    startChat,
    sendMessage,
    endChat,
    loadMessages,
    uploadFile,
  };
}
