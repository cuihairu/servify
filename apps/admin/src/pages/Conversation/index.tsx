import React, { useState, useEffect, useRef, useCallback } from 'react';
import { ProTable, ProCard } from '@ant-design/pro-components';
import type { ProColumns } from '@ant-design/pro-components';
import {
  Alert,
  Button,
  Empty,
  Input,
  Modal,
  Result,
  Select,
  Space,
  Spin,
  Tag,
  message,
  Tooltip,
} from 'antd';
import {
  DisconnectOutlined,
  LinkOutlined,
  ReloadOutlined,
  VideoCameraOutlined,
} from '@ant-design/icons';
import {
  getConversationMessages,
  sendConversationMessage,
  assignAgent,
  transferConversation,
  closeConversation,
} from '@/services/conversation';
import { createTicket } from '@/services/ticket';
import { getWorkspaceOverview } from '@/services/workspace';
import { endAssistSession, getIceServers, startAssistSession } from '@/services/remoteAssist';
import type { RTCIceServerEntry } from '@/services/remoteAssist';
import AssistReviewPanel from './components/AssistReviewPanel';
import { navigateTo, useQueryParam } from '@/lib/navigation';

const STATUS_MAP: Record<string, { color: string; label: string }> = {
  active: { color: 'green', label: '进行中' },
  waiting_human: { color: 'orange', label: '等待客服' },
  transferred: { color: 'blue', label: '已转派' },
  closed: { color: 'default', label: '已结束' },
};

const SENDER_MAP: Record<string, { label: string; color: string }> = {
  agent: { label: '客服', color: '#1677ff' },
  customer: { label: '客户', color: '#52c41a' },
  ai: { label: 'AI', color: '#722ed1' },
  system: { label: '系统', color: '#999' },
};

interface ConversationRecord {
  id: string;
  customer_name?: string;
  agent_name?: string;
  platform?: string;
  status: string;
  started_at: string;
}

type RemoteAssistState =
  | 'idle'
  | 'connecting'
  | 'offered'
  | 'connected'
  | 'failed'
  | 'ended';

const REMOTE_ASSIST_STATE_MAP: Record<RemoteAssistState, { color: string; label: string }> = {
  idle: { color: 'default', label: '未启动' },
  connecting: { color: 'processing', label: '连接中' },
  offered: { color: 'cyan', label: '已发起' },
  connected: { color: 'green', label: '已连接' },
  failed: { color: 'red', label: '失败' },
  ended: { color: 'default', label: '已结束' },
};

const REMOTE_ASSIST_WS_STATUS_MAP: Record<string, { color: string; label: string }> = {
  disconnected: { color: 'default', label: '未连接' },
  connecting: { color: 'processing', label: '信令连接中' },
  connected: { color: 'green', label: '信令已连接' },
  failed: { color: 'red', label: '信令异常' },
};

const ASSIST_RESULT_PRESETS = [
  {
    key: 'resolved',
    label: '已解决',
    priority: 'normal' as const,
    summary: '协助结果: 已解决\n后续动作: 无需继续跟进，建议客户观察并回访确认。',
  },
  {
    key: 'followup',
    label: '待回访',
    priority: 'normal' as const,
    summary: '协助结果: 已完成初步排查\n后续动作: 需要回访确认处理效果。',
  },
  {
    key: 'escalate',
    label: '转二线',
    priority: 'high' as const,
    summary: '协助结果: 一线未能闭环\n后续动作: 转交二线技术支持继续排查。',
  },
  {
    key: 'ticket',
    label: '转工单',
    priority: 'high' as const,
    summary: '协助结果: 需要异步跟进\n后续动作: 已转工单，等待后续处理。',
  },
];

function buildRemoteAssistWebSocketURL(sessionId: string) {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${window.location.host}/api/v1/ws?session_id=${encodeURIComponent(sessionId)}`;
}

const ConversationPage: React.FC = () => {
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const preselectedId = useQueryParam('id');
  const [overview, setOverview] = useState<API.WorkspaceOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const [overviewError, setOverviewError] = useState<string | null>(null);
  const [messageLoading, setMessageLoading] = useState(false);
  const [sending, setSending] = useState(false);
  const [messages, setMessages] = useState<API.ConversationMessage[]>([]);
  const [draft, setDraft] = useState('');
  const [hasMore, setHasMore] = useState(false);
  const [transferModalOpen, setTransferModalOpen] = useState(false);
  const [transferTarget, setTransferTarget] = useState<number | null>(null);
  const [operating, setOperating] = useState(false);
  const [remoteAssistOperating, setRemoteAssistOperating] = useState(false);
  const [remoteAssistState, setRemoteAssistState] = useState<RemoteAssistState>('idle');
  const [remoteAssistSignalState, setRemoteAssistSignalState] = useState<'disconnected' | 'connecting' | 'connected' | 'failed'>('disconnected');
  const [remoteAssistError, setRemoteAssistError] = useState<string | null>(null);
  const [remoteAssistConnectionId, setRemoteAssistConnectionId] = useState<string | null>(null);
  const [remoteAssistHasStream, setRemoteAssistHasStream] = useState(false);
  const [assistSessionId, setAssistSessionId] = useState<number | null>(null);
  const [assistReviewSession, setAssistReviewSession] = useState<API.RemoteAssistSession | null>(null);
  const [ticketModalOpen, setTicketModalOpen] = useState(false);
  const [ticketCreating, setTicketCreating] = useState(false);
  const [assistSummary, setAssistSummary] = useState('');
  const [assistResultPreset, setAssistResultPreset] = useState<string>('manual');
  const [ticketTitle, setTicketTitle] = useState('');
  const [ticketDescription, setTicketDescription] = useState('');
  const [ticketPriority, setTicketPriority] = useState<'low' | 'normal' | 'high' | 'urgent'>('normal');
  const chatEndRef = useRef<HTMLDivElement>(null);
  const remoteAssistPeerRef = useRef<RTCPeerConnection | null>(null);
  const remoteAssistSocketRef = useRef<WebSocket | null>(null);
  const remoteAssistStreamRef = useRef<MediaStream | null>(null);
  // 最近一次服务端下发的 ICE 配置（WS webrtc-ice-config 推送或 REST 回退拉取，
  // RA-6）：跨协助尝试缓存，TURN 凭据过期由下次 REST 回退自然刷新。
  const remoteAssistIceServersRef = useRef<RTCIceServerEntry[] | null>(null);
  const remoteAssistVideoRef = useRef<HTMLVideoElement | null>(null);
  const assistSessionIdRef = useRef<number | null>(null);

  const fetchOverview = useCallback(async () => {
    setLoading(true);
    setOverviewError(null);
    try {
      const result = await getWorkspaceOverview();
      if (result) {
        setOverview(result);
        // URL query param 预选会话
        if (!selectedId && preselectedId) {
          setSelectedId(preselectedId);
        }
      }
    } catch (error) {
      console.error('获取工作区概览失败:', error);
      setOverviewError('获取工作区概览失败，请重试');
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    fetchOverview();
  }, [fetchOverview]);

  const scrollToBottom = useCallback(() => {
    setTimeout(() => chatEndRef.current?.scrollIntoView({ behavior: 'smooth' }), 50);
  }, []);

  useEffect(() => {
    const fetchMessages = async () => {
      if (!selectedId) {
        setMessages([]);
        setHasMore(false);
        return;
      }
      setMessageLoading(true);
      try {
        const result = await getConversationMessages(selectedId, { limit: 50 });
        setMessages(result?.data || []);
        setHasMore((result?.data || []).length >= 50);
        scrollToBottom();
      } catch (error) {
        console.error('获取会话消息失败:', error);
        message.error('获取会话消息失败');
      } finally {
        setMessageLoading(false);
      }
    };
    fetchMessages();
  }, [selectedId, scrollToBottom]);

  const handleLoadMore = async () => {
    if (!selectedId || messages.length === 0) return;
    const oldestId = messages[0]?.id;
    if (!oldestId) return;
    try {
      const result = await getConversationMessages(selectedId, { before: oldestId, limit: 50 });
      const older = result?.data || [];
      setMessages((prev) => [...older, ...prev]);
      setHasMore(older.length >= 50);
    } catch {
      message.error('加载更多消息失败');
    }
  };

  const handleSend = async () => {
    if (!selectedId || !draft.trim()) return;
    const content = draft.trim();
    setSending(true);
    try {
      const payload = await sendConversationMessage(selectedId, { content });
      const item = payload.data;
      if (item) {
        setMessages((prev) => [...prev, item]);
      }
      setDraft('');
      message.success('消息已发送');
      scrollToBottom();
    } catch (error) {
      console.error('发送消息失败:', error);
      message.error('发送消息失败，请重试');
      // 发送失败保留草稿
    } finally {
      setSending(false);
    }
  };

  const refreshOverview = useCallback(async () => {
    try {
      const result = await getWorkspaceOverview();
      if (result) setOverview(result);
    } catch {
      // 静默失败，避免干扰操作
    }
  }, []);

  const handleAssign = async () => {
    if (!selectedId) return;
    setOperating(true);
    try {
      const agents = overview?.agent_stats?.available_agents || [];
      if (agents.length === 0) {
        message.warning('当前没有可用客服');
        return;
      }
      await assignAgent(selectedId, { agent_id: agents[0].id });
      message.success('已接管会话');
      const result = await getConversationMessages(selectedId, { limit: 50 });
      setMessages(result?.data || []);
      refreshOverview();
    } catch (error) {
      message.error('接管失败: ' + (error as Error).message);
    } finally {
      setOperating(false);
    }
  };

  const handleTransfer = async () => {
    if (!selectedId || !transferTarget) return;
    setOperating(true);
    try {
      await transferConversation(selectedId, { to_agent_id: transferTarget });
      message.success('已转派会话');
      setTransferModalOpen(false);
      setTransferTarget(null);
      const result = await getConversationMessages(selectedId, { limit: 50 });
      setMessages(result?.data || []);
      refreshOverview();
    } catch (error) {
      message.error('转派失败: ' + (error as Error).message);
    } finally {
      setOperating(false);
    }
  };

  const handleClose = () => {
    if (!selectedId) return;
    Modal.confirm({
      title: '确认结束会话',
      content: '结束后将无法继续发送消息，确定要结束吗？',
      okText: '确认结束',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        setOperating(true);
        try {
          await closeConversation(selectedId);
          message.success('会话已结束');
          const result = await getConversationMessages(selectedId, { limit: 50 });
          setMessages(result?.data || []);
          refreshOverview();
        } catch (error) {
          message.error('关闭失败: ' + (error as Error).message);
        } finally {
          setOperating(false);
        }
      },
    });
  };

  const teardownRemoteAssist = useCallback((nextState: RemoteAssistState = 'ended') => {
    const socket = remoteAssistSocketRef.current;
    if (socket) {
      socket.onopen = null;
      socket.onmessage = null;
      socket.onerror = null;
      socket.onclose = null;
      if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING) {
        socket.close();
      }
      remoteAssistSocketRef.current = null;
    }

    const peer = remoteAssistPeerRef.current;
    if (peer) {
      peer.onicecandidate = null;
      peer.onconnectionstatechange = null;
      peer.ontrack = null;
      peer.close();
      remoteAssistPeerRef.current = null;
    }

    const stream = remoteAssistStreamRef.current;
    if (stream) {
      stream.getTracks().forEach((track) => track.stop());
      remoteAssistStreamRef.current = null;
    }

    if (remoteAssistVideoRef.current) {
      remoteAssistVideoRef.current.srcObject = null;
    }

    setRemoteAssistSignalState('disconnected');
    setRemoteAssistConnectionId(null);
    setRemoteAssistHasStream(false);
    setRemoteAssistState(nextState);
  }, []);

  // 坐席端 ICE 解析（RA-6）：服务端下发优先（WS webrtc-ice-config 推送缓存，
  // 回退 REST /api/v1/rtc/ice-servers，与访客 SDK 同口径），两者皆空才退回
  // 公网 STUN 兜底——严格网络（禁公网 STUN 出口）下由服务端配置的 TURN 兜住。
  const resolveRemoteAssistIceServers = useCallback(async (): Promise<RTCIceServerEntry[]> => {
    const pushed = remoteAssistIceServersRef.current;
    if (pushed && pushed.length > 0) {
      return pushed;
    }
    try {
      const { ice_servers: fetched } = await getIceServers();
      if (Array.isArray(fetched) && fetched.length > 0) {
        remoteAssistIceServersRef.current = fetched;
        return fetched;
      }
    } catch {
      // REST 面失败（网关未装配 503 等）走公网 STUN 兜底
    }
    return [{ urls: 'stun:stun.l.google.com:19302' }];
  }, []);

  const handleStartRemoteAssist = useCallback(async () => {
    if (!selectedId) {
      message.warning('请先选择会话');
      return;
    }

    setRemoteAssistOperating(true);
    setRemoteAssistError(null);
    setRemoteAssistState('connecting');
    setRemoteAssistSignalState('connecting');

    try {
      teardownRemoteAssist('idle');
      setAssistReviewSession(null);

      // 先落协助记录，assist id 经 DataChannel 下发给访客（访客录制回写依赖它）
      const assistSession = await startAssistSession({ conversation_session_id: selectedId });
      assistSessionIdRef.current = assistSession.id;
      setAssistSessionId(assistSession.id);

      const socket = new WebSocket(buildRemoteAssistWebSocketURL(selectedId));
      remoteAssistSocketRef.current = socket;

      socket.onerror = () => {
        setRemoteAssistSignalState('failed');
        setRemoteAssistState('failed');
        setRemoteAssistError('远程协助信令连接失败');
        setRemoteAssistOperating(false);
        // 信令没打通，协助记录兜底置 failed 避免 active 悬挂
        const assistId = assistSessionIdRef.current;
        if (assistId) {
          void endAssistSession(assistId, 'failed').catch(() => undefined);
        }
      };

      socket.onclose = () => {
        setRemoteAssistSignalState('disconnected');
        setRemoteAssistState((prev) => (prev === 'failed' ? prev : 'ended'));
        setRemoteAssistOperating(false);
      };

      socket.onopen = async () => {
        setRemoteAssistSignalState('connected');

        try {
          const peer = new RTCPeerConnection({
            iceServers: await resolveRemoteAssistIceServers(),
          });
          remoteAssistPeerRef.current = peer;

          peer.createDataChannel('servify-admin-remote-assist');

          // 访客 SDK 在自己的 DataChannel 上监听 assist-session 消息；
          // 这里经 ondatachannel 拿到那条 channel，把 assist id 发过去
          peer.ondatachannel = (event) => {
            const inbound = event.channel;
            const sendAssistId = () => {
              const assistId = assistSessionIdRef.current;
              if (assistId && inbound.readyState === 'open') {
                inbound.send(JSON.stringify({ type: 'assist-session', assist_id: assistId }));
              }
            };
            inbound.onopen = sendAssistId;
            if (inbound.readyState === 'open') {
              sendAssistId();
            }
          };

          peer.onicecandidate = (event) => {
            if (!event.candidate || socket.readyState !== WebSocket.OPEN) {
              return;
            }

            socket.send(JSON.stringify({
              type: 'webrtc-candidate',
              data: event.candidate.toJSON(),
            }));
          };

          peer.onconnectionstatechange = () => {
            const state = peer.connectionState;
            if (state === 'connected') {
              setRemoteAssistState('connected');
            } else if (state === 'connecting') {
              setRemoteAssistState('connecting');
            } else if (state === 'failed') {
              setRemoteAssistState('failed');
              setRemoteAssistError('远程协助连接建立失败');
            } else if (state === 'closed' || state === 'disconnected') {
              setRemoteAssistState('ended');
            }
          };

          peer.ontrack = (event) => {
            const [stream] = event.streams;
            if (!stream) {
              return;
            }
            remoteAssistStreamRef.current = stream;
            setRemoteAssistHasStream(true);
            if (remoteAssistVideoRef.current) {
              remoteAssistVideoRef.current.srcObject = stream;
            }
          };

          socket.onmessage = async (event) => {
            try {
              const payload = JSON.parse(event.data);
              if (payload.type === 'webrtc-ice-config' && payload.data) {
                // hub 建联即推送（RA-6）：缓存供本次/下次建 PC 使用；
                // 本次的 PC 已按 REST 回退结果建好，推送仅刷新缓存。
                const servers = payload.data.ice_servers;
                if (Array.isArray(servers) && servers.length > 0) {
                  remoteAssistIceServersRef.current = servers;
                }
                return;
              }

              if (payload.type === 'webrtc-answer' && payload.data) {
                await peer.setRemoteDescription(payload.data);
                setRemoteAssistState('connecting');
                return;
              }

              if (payload.type === 'webrtc-candidate' && payload.data) {
                const candidate = payload.data?.candidate ?? payload.data;
                await peer.addIceCandidate(candidate);
                return;
              }

              if (payload.type === 'webrtc-state-change' && payload.data) {
                const state = String(payload.data.state || '');
                const connectionId = String(payload.data.connection_id || '');
                if (connectionId) {
                  setRemoteAssistConnectionId(connectionId);
                }
                if (state === 'connected') {
                  setRemoteAssistState('connected');
                } else if (state === 'connecting' || state === 'new') {
                  setRemoteAssistState('connecting');
                } else if (state === 'failed') {
                  setRemoteAssistState('failed');
                }
              }
            } catch (error) {
              setRemoteAssistError((error as Error).message);
              setRemoteAssistState('failed');
            }
          };

          const offer = await peer.createOffer({
            offerToReceiveAudio: true,
            offerToReceiveVideo: true,
          });
          await peer.setLocalDescription(offer);

          socket.send(JSON.stringify({
            type: 'webrtc-offer',
            data: offer,
          }));
          setRemoteAssistState('offered');
        } catch (error) {
          setRemoteAssistError((error as Error).message);
          setRemoteAssistState('failed');
          setRemoteAssistSignalState('failed');
        } finally {
          setRemoteAssistOperating(false);
        }
      };
    } catch (error) {
      setRemoteAssistError((error as Error).message);
      setRemoteAssistState('failed');
      setRemoteAssistSignalState('failed');
      setRemoteAssistOperating(false);
    }
  }, [selectedId, teardownRemoteAssist, resolveRemoteAssistIceServers]);

  const handleEndRemoteAssist = useCallback(async () => {
    teardownRemoteAssist('ended');

    const assistId = assistSessionIdRef.current;
    if (!assistId) {
      return;
    }
    assistSessionIdRef.current = null;
    try {
      // 访客录制经 /upload 上传后回写可能有延迟，end 后短暂等待再落回放
      const ended = await endAssistSession(assistId);
      setAssistReviewSession(ended);
      setAssistSessionId(null);
    } catch (error) {
      message.error('结束协助记录失败: ' + (error as Error).message);
    }
  }, [teardownRemoteAssist]);

  const sessions = overview?.recent_sessions || [];
  const selectedSession = sessions.find((s) => s.id === selectedId) || null;
  const sessionStatus = selectedSession?.status || '';
  const statusCfg = STATUS_MAP[sessionStatus] || {};
  const isClosed = sessionStatus === 'closed';
  const isWaiting = sessionStatus === 'waiting_human';
  const isActive = sessionStatus === 'active';
  const remoteAssistActive =
    remoteAssistState === 'connecting' ||
    remoteAssistState === 'offered' ||
    remoteAssistState === 'connected';
  const remoteAssistStatusCfg = REMOTE_ASSIST_STATE_MAP[remoteAssistState];
  const remoteAssistSignalCfg = REMOTE_ASSIST_WS_STATUS_MAP[remoteAssistSignalState];

  const openCreateTicketModal = useCallback(() => {
    if (!selectedSession) {
      message.warning('请先选择会话');
      return;
    }

    const defaultTitle = `远程协助跟进 - ${selectedSession.customer_name || selectedSession.id}`;
    const defaultDescription = [
      `会话ID: ${selectedSession.id}`,
      `客户: ${selectedSession.customer_name || '未识别客户'}`,
      `渠道: ${selectedSession.platform || 'unknown'}`,
      `当前会话状态: ${selectedSession.status}`,
      `远程协助状态: ${remoteAssistStatusCfg.label}`,
      assistSummary.trim() ? '' : '协助摘要: ',
      assistSummary.trim(),
    ]
      .filter(Boolean)
      .join('\n');

    setTicketTitle(defaultTitle);
    setTicketDescription(defaultDescription);
    setTicketPriority(remoteAssistState === 'failed' ? 'high' : 'normal');
    setTicketModalOpen(true);
  }, [assistSummary, remoteAssistState, remoteAssistStatusCfg.label, selectedSession]);

  const applyAssistPreset = useCallback((presetKey: string) => {
    const preset = ASSIST_RESULT_PRESETS.find((item) => item.key === presetKey);
    if (!preset) {
      setAssistResultPreset('manual');
      return;
    }

    setAssistResultPreset(preset.key);
    setAssistSummary(preset.summary);
    setTicketPriority(preset.priority);
  }, []);

  const handleCreateTicket = useCallback(async () => {
    if (!selectedSession) {
      return;
    }
    if (!ticketTitle.trim()) {
      message.warning('请输入工单标题');
      return;
    }

    setTicketCreating(true);
    try {
      const ticket = await createTicket({
        title: ticketTitle.trim(),
        description: ticketDescription.trim(),
        priority: ticketPriority,
        category: 'remote-assist',
        source: 'remote_assist',
        customer_id: selectedSession.customer_id,
        session_id: selectedSession.id,
        tags: [
          'remote_assist',
          ...(assistResultPreset !== 'manual' ? [assistResultPreset] : []),
        ],
        custom_fields: {
          source: 'remote_assist',
          session_id: selectedSession.id,
          remote_assist: {
            session_id: selectedSession.id,
            customer_name: selectedSession.customer_name || '',
            platform: selectedSession.platform || '',
            assist_state: remoteAssistState,
            assist_state_label: remoteAssistStatusCfg.label,
            result_preset: assistResultPreset === 'manual' ? '' : assistResultPreset,
            summary: assistSummary.trim(),
          },
        },
      });
      message.success(`工单 #${ticket.id} 已创建`);
      setTicketModalOpen(false);
      navigateTo(`/ticket/detail/${ticket.id}`);
    } catch (error) {
      message.error('创建工单失败: ' + (error as Error).message);
    } finally {
      setTicketCreating(false);
    }
  }, [
    assistResultPreset,
    assistSummary,
    remoteAssistState,
    remoteAssistStatusCfg.label,
    selectedSession,
    ticketDescription,
    ticketPriority,
    ticketTitle,
  ]);

  useEffect(() => () => {
    teardownRemoteAssist('ended');
  }, [teardownRemoteAssist]);

  useEffect(() => {
    setRemoteAssistError(null);
    teardownRemoteAssist('idle');
    setAssistResultPreset('manual');
    setAssistSummary('');
    assistSessionIdRef.current = null;
    setAssistSessionId(null);
    setAssistReviewSession(null);
  }, [selectedId, teardownRemoteAssist]);

  const columns: ProColumns<ConversationRecord>[] = [
    { title: 'ID', dataIndex: 'id', width: 80, render: (_, r) => <Tooltip title={r.id}><span style={{ fontFamily: 'monospace', fontSize: 12 }}>{r.id.length > 8 ? `${r.id.slice(0, 8)}...` : r.id}</span></Tooltip> },
    { title: '客户', dataIndex: 'customer_name', width: 120, search: true },
    { title: '客服', dataIndex: 'agent_name', width: 120 },
    { title: '渠道', dataIndex: 'platform', width: 80 },
    { title: '状态', dataIndex: 'status', width: 100, render: (_, r) => { const c = STATUS_MAP[r.status] || { color: 'default', label: r.status }; return <Tag color={c.color}>{c.label}</Tag>; } },
    { title: '开始时间', dataIndex: 'started_at', valueType: 'dateTime', width: 160 },
  ];

  const onlineAgents = (overview?.agent_stats?.available_agents || []).filter(
    (a) => a.id !== selectedSession?.agent_id,
  );

  return (
    <div style={{ display: 'flex', gap: 16, height: 'calc(100vh - 120px)' }}>
      <div style={{ width: 480, flexShrink: 0 }}>
        {overviewError ? (
          <Result
            status="error"
            title="加载失败"
            subTitle={overviewError}
            extra={<Button type="primary" icon={<ReloadOutlined />} onClick={fetchOverview}>重试</Button>}
          />
        ) : (
          <ProTable<ConversationRecord>
            headerTitle={
              <Space>
                <span>会话列表</span>
                <Button size="small" type="text" icon={<ReloadOutlined />} onClick={refreshOverview} loading={loading} title="刷新列表" />
              </Space>
            }
            rowKey="id"
            columns={columns}
            search={{ filterType: 'light' }}
            tableAlertRender={false}
            scroll={{ y: 'calc(100vh - 220px)' }}
            pagination={{ defaultPageSize: 20 }}
            loading={loading}
            onRow={(record) => ({
              onClick: () => setSelectedId(record.id),
              style: { cursor: 'pointer', background: selectedId === record.id ? '#e6f7ff' : undefined },
            })}
            request={async () => ({ data: sessions, total: sessions.length, success: true })}
          />
        )}
      </div>
      <div style={{ flex: 1 }}>
        <ProCard
          title={
            selectedId ? (
              <Space>
                <span>会话 {selectedId.slice(0, 8)}</span>
                {sessionStatus && <Tag color={statusCfg.color}>{statusCfg.label || sessionStatus}</Tag>}
                {selectedSession && <Tag color={remoteAssistStatusCfg.color}>协助: {remoteAssistStatusCfg.label}</Tag>}
              </Space>
            ) : '聊天区域'
          }
          extra={
            selectedSession ? (
              <Space size="middle">
                <Tag>{selectedSession.platform || 'unknown'}</Tag>
                <span>{selectedSession.customer_name || '未识别客户'}</span>
                <span>{selectedSession.agent_name || '待分配客服'}</span>
                {!isClosed && (
                  <>
                    {isWaiting && (
                      <Button size="small" type="primary" onClick={handleAssign} loading={operating}>
                        接管
                      </Button>
                    )}
                    {isActive && (
                      <Button size="small" onClick={() => setTransferModalOpen(true)} loading={operating}>
                        转派
                      </Button>
                    )}
                    <Button
                      size="small"
                      icon={remoteAssistActive ? <DisconnectOutlined /> : <VideoCameraOutlined />}
                      onClick={remoteAssistActive ? handleEndRemoteAssist : handleStartRemoteAssist}
                      loading={remoteAssistOperating}
                    >
                      {remoteAssistActive ? '结束协助' : '发起协助'}
                    </Button>
                    <Button size="small" onClick={openCreateTicketModal}>
                      转工单
                    </Button>
                    {!isClosed && (
                      <Button size="small" danger onClick={handleClose} loading={operating}>
                        结束
                      </Button>
                    )}
                  </>
                )}
              </Space>
            ) : null
          }
          style={{ height: '100%' }}
        >
          <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
            {selectedId && (
              <div style={{ marginBottom: 12 }}>
                <Alert
                  type={remoteAssistState === 'failed' ? 'error' : 'info'}
                  showIcon
                  message={(
                    <Space size="middle" wrap>
                      <span>远程协助</span>
                      <Tag color={remoteAssistStatusCfg.color}>{remoteAssistStatusCfg.label}</Tag>
                      <Tag color={remoteAssistSignalCfg.color}>{remoteAssistSignalCfg.label}</Tag>
                      {remoteAssistConnectionId && (
                        <span style={{ fontFamily: 'monospace' }}>连接 {remoteAssistConnectionId}</span>
                      )}
                      {assistSessionId && (
                        <span style={{ fontFamily: 'monospace' }}>协助 #{assistSessionId}</span>
                      )}
                    </Space>
                  )}
                  description={(
                    <div>
                      <div>当前按会话级 WebSocket/WebRTC 建立协助链路，先打通最小发起与状态观测。</div>
                      {remoteAssistError && <div style={{ marginTop: 4, color: '#ff4d4f' }}>{remoteAssistError}</div>}
                    </div>
                  )}
                  action={(
                    <Button
                      size="small"
                      type={remoteAssistActive ? 'default' : 'primary'}
                      icon={remoteAssistActive ? <DisconnectOutlined /> : <LinkOutlined />}
                      onClick={remoteAssistActive ? handleEndRemoteAssist : handleStartRemoteAssist}
                      loading={remoteAssistOperating}
                    >
                      {remoteAssistActive ? '结束协助' : '连接协助'}
                    </Button>
                  )}
                />
                <div style={{ marginTop: 8 }}>
                  <Space size={[8, 8]} wrap>
                    <span style={{ color: '#666' }}>协助结果模板</span>
                    {ASSIST_RESULT_PRESETS.map((preset) => (
                      <Tag
                        key={preset.key}
                        color={assistResultPreset === preset.key ? 'blue' : 'default'}
                        style={{ cursor: 'pointer', userSelect: 'none' }}
                        onClick={() => applyAssistPreset(preset.key)}
                      >
                        {preset.label}
                      </Tag>
                    ))}
                    <Tag
                      color={assistResultPreset === 'manual' ? 'blue' : 'default'}
                      style={{ cursor: 'pointer', userSelect: 'none' }}
                      onClick={() => setAssistResultPreset('manual')}
                    >
                      手动
                    </Tag>
                  </Space>
                </div>
                <Input.TextArea
                  rows={3}
                  style={{ marginTop: 8 }}
                  placeholder="记录协助摘要、排查结论或后续动作，转工单时会自动带入。"
                  value={assistSummary}
                  onChange={(e) => setAssistSummary(e.target.value)}
                />
                <div style={{
                  marginTop: 8,
                  minHeight: 180,
                  borderRadius: 8,
                  border: '1px dashed #d9d9d9',
                  background: '#fcfcfc',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  overflow: 'hidden',
                }}
                >
                  <video
                    ref={remoteAssistVideoRef}
                    autoPlay
                    playsInline
                    muted
                    style={{
                      width: '100%',
                      maxHeight: 240,
                      display: remoteAssistHasStream ? 'block' : 'none',
                      background: '#000',
                    }}
                  />
                  {!remoteAssistHasStream && (
                    <div style={{ color: '#999', padding: 24 }}>
                      尚未收到远端媒体流，当前面板用于发起协助和观察连接状态。
                    </div>
                  )}
                </div>
                {assistReviewSession && <AssistReviewPanel session={assistReviewSession} />}
              </div>
            )}
            <div style={{ flex: 1, overflowY: 'auto', padding: 12, background: '#fafafa', borderRadius: 8 }}>
              {!selectedId ? (
                <Empty description="请先从左侧选择一个会话。" style={{ marginTop: 96 }} />
              ) : messageLoading ? (
                <div style={{ textAlign: 'center', padding: 80 }}><Spin /></div>
              ) : messages.length === 0 ? (
                <Empty description="当前会话暂无消息。" style={{ marginTop: 96 }} />
              ) : (
                <>
                  {hasMore && (
                    <div style={{ textAlign: 'center', marginBottom: 12 }}>
                      <Button size="small" type="link" onClick={handleLoadMore}>加载更多消息</Button>
                    </div>
                  )}
                  {messages.map((item) => {
                    const isAgent = item.sender === 'agent';
                    const align = isAgent ? 'flex-end' : 'flex-start';
                    const background = isAgent ? '#1677ff' : '#fff';
                    const color = isAgent ? '#fff' : '#000';
                    const senderCfg = SENDER_MAP[item.sender] || { label: item.sender, color: '#999' };
                    return (
                      <div key={item.id} style={{ display: 'flex', justifyContent: align, marginBottom: 12 }}>
                        <div style={{
                          maxWidth: '72%', background, color,
                          border: isAgent ? 'none' : '1px solid #f0f0f0',
                          borderRadius: 12, padding: '10px 12px',
                          boxShadow: '0 1px 2px rgba(0,0,0,0.04)',
                        }}>
                          <div style={{ fontSize: 12, opacity: 0.75, marginBottom: 4, display: 'flex', gap: 8, alignItems: 'center' }}>
                            <span style={{ color: senderCfg.color, fontWeight: 500 }}>{senderCfg.label}</span>
                            <span>{new Date(item.created_at).toLocaleString()}</span>
                          </div>
                          <div style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{item.content}</div>
                        </div>
                      </div>
                    );
                  })}
                  <div ref={chatEndRef} />
                </>
              )}
            </div>
            {!isClosed && selectedId && (
              <>
                <Input.TextArea
                  rows={3}
                  placeholder="输入消息... (Enter 发送, Shift+Enter 换行)"
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                  style={{ marginTop: 8 }}
                  disabled={sending}
                  onPressEnter={(e) => { if (!e.shiftKey) { e.preventDefault(); handleSend(); } }}
                />
                <div style={{ marginTop: 8, display: 'flex', justifyContent: 'flex-end' }}>
                  <Button type="primary" onClick={handleSend} loading={sending} disabled={!draft.trim()}>
                    发送消息
                  </Button>
                </div>
              </>
            )}
          </div>
        </ProCard>
      </div>

      <Modal
        title="转派会话"
        open={transferModalOpen}
        onCancel={() => { setTransferModalOpen(false); setTransferTarget(null); }}
        onOk={handleTransfer}
        confirmLoading={operating}
        okButtonProps={{ disabled: !transferTarget }}
      >
        <p style={{ marginBottom: 12 }}>选择目标客服：</p>
        {onlineAgents.length === 0 ? (
          <Empty description="暂无可用客服" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        ) : (
          <Select
            style={{ width: '100%' }}
            placeholder="选择客服"
            value={transferTarget}
            onChange={(v) => setTransferTarget(v)}
            options={onlineAgents.map((a) => ({ label: a.name || `客服 #${a.id}`, value: a.id }))}
          />
        )}
      </Modal>
      <Modal
        title="创建跟进工单"
        open={ticketModalOpen}
        onCancel={() => setTicketModalOpen(false)}
        onOk={handleCreateTicket}
        confirmLoading={ticketCreating}
        okText="创建并查看"
      >
        <div style={{ display: 'grid', gap: 12 }}>
          <div>
            <div style={{ marginBottom: 6 }}>标题</div>
            <Input
              value={ticketTitle}
              onChange={(e) => setTicketTitle(e.target.value)}
              placeholder="输入工单标题"
            />
          </div>
          <div>
            <div style={{ marginBottom: 6 }}>优先级</div>
            <Select
              style={{ width: '100%' }}
              value={ticketPriority}
              onChange={(value) => setTicketPriority(value)}
              options={[
                { label: '低', value: 'low' },
                { label: '普通', value: 'normal' },
                { label: '高', value: 'high' },
                { label: '紧急', value: 'urgent' },
              ]}
            />
          </div>
          <div>
            <div style={{ marginBottom: 6 }}>描述</div>
            <Input.TextArea
              rows={8}
              value={ticketDescription}
              onChange={(e) => setTicketDescription(e.target.value)}
              placeholder="输入跟进说明"
            />
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default ConversationPage;
