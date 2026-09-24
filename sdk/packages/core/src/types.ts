import type { AuthProvider } from './contracts/auth-provider';
import type { CapabilitySet } from './contracts/capability';
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
  reconnectPolicy?: ReconnectPolicy;
  authProvider?: AuthProvider;
  onTokenRefreshRequired?: () => Promise<void>;
  remoteAssist?: RemoteAssistConfig;
  /**
   * WebSocket 工厂注入点：非 DOM 宿主（React Native 测试、自定义传输）替换
   * 全局 WebSocket 构造。缺省使用 globalThis.WebSocket。
   */
  webSocketFactory?: WebSocketFactory;
  /**
   * 能力集覆盖：平台绑定（React Native headless 等）注入宿主能力面，
   * 缺省 createWebCapabilitySet()。
   */
  capabilities?: CapabilitySet;
}

/** WebSocket 传输工厂：返回宿主平台的 WebSocket 实例（浏览器/RN 同形 API）。 */
export type WebSocketFactory = (url: string, protocols?: string | string[]) => WebSocket;

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
    | 'webrtc-state-change'
    | 'webrtc-ice-config';
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
  /** 宿主覆盖口（最高优先级）；缺省消费服务端下发（WS webrtc-ice-config / REST ice-servers）。 */
  iceServers?: ServifyRTCIceServer[];
  /** RTCPeerConnection 工厂注入点：非 DOM 宿主替换全局构造（缺省 globalThis）。 */
  peerConnectionFactory?: (config: { iceServers?: ServifyRTCIceServer[] }) => RTCPeerConnection;
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

// --- DOM 解耦的 RTC 结构化类型 ---
// 与 DOM 库同名类型字段兼容（{type,sdp}/{candidate,...}），但由 core 自持，
// 非 DOM 宿主（React Native）无需 DOM lib 即可消费共享契约。

export type ServifyRTCSdpType = 'offer' | 'pranswer' | 'answer' | 'rollback';

export interface ServifyRTCSessionDescriptionInit {
  type: ServifyRTCSdpType;
  sdp?: string;
}

export interface ServifyRTCIceCandidateInit {
  candidate?: string;
  sdpMid?: string | null;
  sdpMLineIndex?: number | null;
  usernameFragment?: string | null;
}

export interface ServifyRTCIceServer {
  urls: string | string[];
  username?: string;
  credential?: string;
  credentialType?: string;
  /** TURN 时间限凭据的秒数（服务端 webrtc-ice-config / ice-servers 下发，供刷新规划）。 */
  ttl?: number;
}

/** 远端媒体轨道的最小结构面（DOM MediaStreamTrack / RN 注入轨道同形）。 */
export interface ServifyMediaStreamTrack {
  readonly id: string;
  readonly kind: string;
  readonly label: string;
  readonly enabled: boolean;
  readonly muted: boolean;
  stop(): void;
}

/** 远端媒体流的最小结构面（DOM MediaStream / RN 注入流同形）。 */
export interface ServifyMediaStream {
  readonly id: string;
  readonly active: boolean;
  getTracks(): ServifyMediaStreamTrack[];
}

/** ontrack 事件的最小结构面（peer.ontrack 负载鸭子类型）。 */
export interface ServifyRTCTrackEvent {
  readonly track: ServifyMediaStreamTrack;
  readonly streams: readonly ServifyMediaStream[];
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
  'webrtc:offer': [offer: ServifyRTCSessionDescriptionInit];
  'webrtc:answer': [answer: ServifyRTCSessionDescriptionInit];
  'webrtc:candidate': [candidate: ServifyRTCIceCandidateInit];
  'webrtc:track': [event: ServifyRTCTrackEvent];
  'webrtc:state': [state: RemoteAssistState];
  'webrtc:ice-config': [iceServers: ServifyRTCIceServer[]];
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
  iceServers?: ServifyRTCIceServer[];
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

// 访客补拉端点（§10 #1，GET /api/v1/sessions/:id/messages）单条消息 DTO：
// id 为服务端单调数字游标（字符串承载），sender ∈ customer/agent/system/ai。
export interface VisitorMessage {
  id: string;
  conversation_id: string;
  sender: string;
  kind: string;
  content: string;
  metadata?: Record<string, string>;
  created_at: string;
}

// 客户侧推荐问题（P2-0）：首屏热门 / 会话内上下文联想。
// question 即可直接作为 query 发起提问的可点击文案。
export interface RecommendedQuestion {
  question: string;
  source: 'knowledge_doc' | 'intent' | string;
  source_id?: string;
  category?: string;
  score: number;
}

// 首屏推荐问题响应
export interface InitialQuestionsResult {
  questions: RecommendedQuestion[];
  meta?: Record<string, unknown>;
}

// 上下文联想问题响应
export interface NextQuestionsResult {
  query: string;
  questions: RecommendedQuestion[];
  meta?: Record<string, unknown>;
}
