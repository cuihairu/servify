import type { AuthProvider } from './contracts/auth-provider';
import type { ReconnectPolicy } from './contracts/transport';

// 基础类型定义
export interface ServifyConfig {
  apiUrl: string;
  wsUrl?: string;
  customerId?: string;
  customerName?: string;
  customerEmail?: string;
  sessionId?: string;
  debug?: boolean;
  autoConnect?: boolean;
  reconnectAttempts?: number;
  reconnectDelay?: number;
  reconnectPolicy?: ReconnectPolicy;
  authProvider?: AuthProvider;
  onTokenRefreshRequired?: () => Promise<void>;
  remoteAssist?: RemoteAssistConfig;
}

export interface Customer {
  id: number;
  name: string;
  email: string;
  phone?: string;
  address?: string;
  avatar?: string;
  status: 'active' | 'inactive';
  notes?: string;
  created_at: string;
  updated_at: string;
}

export interface Agent {
  id: number;
  name: string;
  email: string;
  avatar?: string;
  status: 'online' | 'offline' | 'busy' | 'away';
  is_ai?: boolean;
  created_at: string;
  updated_at: string;
}

export interface ChatSession {
  id: string | number;
  customer_id: number;
  agent_id?: number;
  status: 'active' | 'closed' | 'ended' | 'transferred' | 'waiting_human';
  channel: 'web' | 'mobile' | 'email' | 'phone';
  priority: 'low' | 'normal' | 'high' | 'urgent';
  queue_id?: number;
  started_at: string;
  ended_at?: string;
  created_at: string;
  updated_at: string;
}

export interface Message {
  id: string | number;
  session_id: string | number;
  sender_type: 'customer' | 'agent' | 'system';
  sender_id?: number;
  content: string;
  message_type: 'text' | 'image' | 'file' | 'system';
  attachments?: string[];
  is_ai_response?: boolean;
  metadata?: Record<string, unknown>;
  created_at: string;
}

export interface Ticket {
  id: number;
  customer_id: number;
  assigned_agent_id?: number;
  title: string;
  description: string;
  status: 'open' | 'in_progress' | 'resolved' | 'closed';
  priority: 'low' | 'normal' | 'high' | 'urgent';
  category: string;
  tags?: string[];
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  resolved_at?: string;
}

export interface CustomerSatisfaction {
  id: number;
  ticket_id: number;
  customer_id: number;
  agent_id?: number;
  rating: number; // 1-5
  comment?: string;
  category?: string;
  created_at: string;
}

// WebSocket 消息类型
export interface WSMessage {
  type:
    | 'message'
    | 'text-message'
    | 'agent-message'
    | 'ai-response'
    | 'session_update'
    | 'agent_status'
    | 'typing'
    | 'error'
    | 'system'
    | 'webrtc-offer'
    | 'webrtc-answer'
    | 'webrtc-candidate'
    | 'webrtc-state-change';
  data: unknown;
  session_id?: string;
  timestamp?: string;
}

export interface RemoteAssistConfig {
  enabled?: boolean;
  captureScreen?: boolean;
  audio?: boolean;
  /** 访客端本地录制屏幕共享，结束后自动上传并回写协助会话（默认 false） */
  record?: boolean;
  iceServers?: RTCIceServer[];
  dataChannelLabel?: string;
}

export type RemoteAssistStartOptions = RemoteAssistConfig;

/** 录制生命周期状态（'remote-assist:recording' 事件负载） */
export type RemoteAssistRecordingState =
  | 'idle'
  | 'recording'
  | 'saving'
  | 'saved'
  | 'failed'
  | 'unsupported';

export type RemoteAssistState =
  | 'idle'
  | 'starting'
  | 'offered'
  | 'connecting'
  | 'connected'
  | 'failed'
  | 'ended';

export interface RemoteAssistRuntimeState {
  connectionId?: string;
  state: string;
}

// 事件类型
export type ServifyEventMap = {
  'connected': [];
  'disconnected': [reason: string];
  'reconnecting': [attempt: number];
  'message': [message: Message];
  'session_created': [session: ChatSession];
  'session_updated': [session: ChatSession];
  'session_ended': [session: ChatSession];
  'agent_assigned': [agent: Agent];
  'agent_typing': [isTyping: boolean];
  'error': [error: Error];
  'ticket_created': [ticket: Ticket];
  'ticket_updated': [ticket: Ticket];
  'webrtc:offer': [offer: RTCSessionDescriptionInit];
  'webrtc:answer': [answer: RTCSessionDescriptionInit];
  'webrtc:candidate': [candidate: RTCIceCandidateInit];
  'webrtc:track': [event: RTCTrackEvent];
  'webrtc:state': [state: RemoteAssistState];
  'remote-assist:session': [assistId: string];
  'remote-assist:recording': [state: RemoteAssistRecordingState];
};

// API 响应类型
export interface ApiResponse<T = unknown> {
  success: boolean;
  data?: T;
  message?: string;
  error?: string;
}

// WebRTC 相关类型
export interface WebRTCConfig {
  iceServers?: RTCIceServer[];
  video?: boolean;
  audio?: boolean;
}

export interface WebRTCCall {
  id: number;
  session_id: number;
  caller_type: 'customer' | 'agent';
  status: 'ringing' | 'active' | 'ended';
  start_time?: string;
  end_time?: string;
  duration?: number;
}
