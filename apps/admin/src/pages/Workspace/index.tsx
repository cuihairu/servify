import { useCallback, useEffect, useState } from 'react';
import { ProCard } from '@ant-design/pro-components';
import {
  Alert,
  Badge,
  Button,
  Collapse,
  Empty,
  Input,
  Modal,
  Select,
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd';
import {
  getConversation,
  getConversationMessages,
  sendConversationMessage,
  assignAgent,
  transferConversation,
  closeConversation,
} from '@/services/conversation';
import { getWorkspaceOverview } from '@/services/workspace';
import { getCustomer } from '@/services/customer';
import { listTickets, createTicket } from '@/services/ticket';
import { listAgents } from '@/services/agent';
// 坐席 AI 辅助与知识检索（V1.0 收敛 B1-3b，W3/W4）。
import { aiCopilot, aiKnowledgeQuery } from '@/services/ai';
// 服务过程时间线（V1.0 收敛 B1-2，W6）：conversation_events 投影只读展示。
import ServiceTimeline from '@/pages/Conversation/components/ServiceTimeline';

const STATUS_MAP: Record<string, { label: string; color: string }> = {
  waiting: { label: '排队中', color: 'red' },
  active: { label: '接待中', color: 'green' },
  transferred: { label: '已转接', color: 'orange' },
  closed: { label: '已结束', color: 'default' },
};

function statusCfg(status?: string) {
  return STATUS_MAP[status || ''] || { label: status || '未知', color: 'default' };
}

/**
 * Agent Workspace（V1.0 收敛 B1-3a，docs/v1-convergence-plan.md §4）：
 * 三栏接待工作台——左栏 Inbox/Queue 聚合（W1），中栏会话主流程
 * （W2/W6），右栏 Customer 档案与工单汇（W5）。copilot/知识建议
 * （W3/W4）在 B1-3b 批次接入。
 */
export default function Workspace() {
  // ---- 左栏：队列聚合（W1） ----
  const [overview, setOverview] = useState<API.WorkspaceOverview | null>(null);
  const [overviewLoading, setOverviewLoading] = useState(false);
  const [statusFilter, setStatusFilter] = useState<string | null>(null);

  // ---- 中栏：会话区（W2） ----
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [selectedSession, setSelectedSession] = useState<API.Conversation | null>(null);
  const [messages, setMessages] = useState<API.ConversationMessage[]>([]);
  const [messagesLoading, setMessagesLoading] = useState(false);
  const [draft, setDraft] = useState('');
  const [sending, setSending] = useState(false);
  const [operating, setOperating] = useState(false);
  const [transferOpen, setTransferOpen] = useState(false);
  const [transferTarget, setTransferTarget] = useState<number | null>(null);
  const [agents, setAgents] = useState<Array<{ id: number; name?: string }>>([]);
  const [ticketOpen, setTicketOpen] = useState(false);
  const [ticketForm, setTicketForm] = useState({ title: '', description: '', priority: 'normal' });

  // ---- AI 辅助（W3）与知识建议（W4） ----
  const [copilotBusy, setCopilotBusy] = useState<string | null>(null);
  const [summaryText, setSummaryText] = useState('');
  const [knowledge, setKnowledge] = useState<API.AIKnowledgeSource[] | null>(null);
  const [knowledgeBusy, setKnowledgeBusy] = useState(false);

  // ---- 右栏：Customer 面板（W5） ----
  const [customer, setCustomer] = useState<API.Customer | null>(null);
  const [customerTickets, setCustomerTickets] = useState<API.Ticket[]>([]);

  const loadOverview = useCallback(async () => {
    setOverviewLoading(true);
    try {
      const resp = await getWorkspaceOverview();
      setOverview(resp || null);
    } catch (e) {
      message.error(e instanceof Error ? e.message : '队列概览加载失败');
    } finally {
      setOverviewLoading(false);
    }
  }, []);

  const loadConversation = useCallback(async (sessionId: string) => {
    setMessagesLoading(true);
    try {
      const [convResp, msgResp] = await Promise.all([
        getConversation(sessionId),
        getConversationMessages(sessionId, { limit: 100 }),
      ]);
      setSelectedSession(convResp.data || null);
      setMessages(msgResp.data || []);
      const customerId = convResp.data?.customer_id;
      if (customerId) {
        void getCustomer(customerId).then((resp) => setCustomer(resp || null));
        void listTickets({ customer_id: customerId, page_size: 20 }).then((resp) => {
          setCustomerTickets(resp.data || []);
        });
      } else {
        setCustomer(null);
        setCustomerTickets([]);
      }
    } catch (e) {
      message.error(e instanceof Error ? e.message : '会话加载失败');
    } finally {
      setMessagesLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadOverview();
  }, [loadOverview]);

  useEffect(() => {
    if (selectedId) {
      void loadConversation(selectedId);
    }
  }, [selectedId, loadConversation]);

  // 切换会话即清空 AI 面产物（摘要与知识建议都是会话级视图）。
  useEffect(() => {
    setSummaryText('');
    setKnowledge(null);
  }, [selectedId]);

  const runCopilot = async (
    action: 'suggest_reply' | 'rewrite' | 'session_summary',
  ) => {
    if (!selectedId) return;
    if (action === 'rewrite' && !draft.trim()) {
      message.warning('先在输入框写草稿再改写');
      return;
    }
    setCopilotBusy(action);
    try {
      const resp = await aiCopilot({
        action,
        session_id: selectedId,
        draft: action === 'rewrite' ? draft.trim() : undefined,
      });
      if (!resp.success || !resp.data?.text) {
        message.warning(resp.error || 'AI 未返回结果');
        return;
      }
      if (action === 'session_summary') {
        setSummaryText(resp.data.text);
      } else {
        // 建议回复与改写都落到输入框：坐席可编辑后再发送（AI 起草，人发）。
        setDraft(resp.data.text);
      }
    } catch (e) {
      message.warning(e instanceof Error ? e.message : 'AI 辅助暂不可用');
    } finally {
      setCopilotBusy(null);
    }
  };

  const runKnowledgeSearch = async () => {
    if (!selectedSession) return;
    // 检索词：优先坐席草稿，否则取最后一条客户消息（会话上下文即查询）。
    const lastCustomer = [...messages].reverse().find((item) => item.sender === 'customer');
    const query = draft.trim() || lastCustomer?.content?.trim();
    if (!query) {
      message.warning('没有可检索的内容：先写草稿或等客户消息');
      return;
    }
    setKnowledgeBusy(true);
    try {
      const resp = await aiKnowledgeQuery({ query, session_id: selectedId || undefined });
      if (!resp.success || !resp.data) {
        message.warning(resp.error || '知识检索暂不可用');
        return;
      }
      setKnowledge(resp.data.sources || []);
      if ((resp.data.sources || []).length === 0) {
        message.info('知识库没有命中相关内容');
      }
    } catch (e) {
      message.warning(e instanceof Error ? e.message : '知识检索暂不可用');
    } finally {
      setKnowledgeBusy(false);
    }
  };

  const sessions = (overview?.recent_sessions || []).filter(
    (item) => !statusFilter || item.status === statusFilter,
  );

  const waitingCount = overview?.waiting_queue ?? 0;
  const activeCount = overview?.total_active_sessions ?? 0;
  const aiCount = Math.max(activeCount - waitingCount, 0);

  const handleSend = async () => {
    if (!selectedId || !draft.trim()) return;
    setSending(true);
    try {
      await sendConversationMessage(selectedId, { content: draft.trim() });
      setDraft('');
      await loadConversation(selectedId);
      void loadOverview();
    } catch (e) {
      message.error(e instanceof Error ? e.message : '发送失败');
    } finally {
      setSending(false);
    }
  };

  const handleAssign = async () => {
    if (!selectedId) return;
    // 与会话管理页同口径：从可用坐席里取第一个（服务端 required 校验
    // 不接受 0，当前登录坐席亲和由后续批次接入）。
    const available = overview?.agent_stats?.available_agents || [];
    if (available.length === 0) {
      message.warning('当前没有可用客服');
      return;
    }
    setOperating(true);
    try {
      await assignAgent(selectedId, { agent_id: available[0].id });
      message.success('已接管');
      await loadConversation(selectedId);
      void loadOverview();
    } catch (e) {
      message.error(e instanceof Error ? e.message : '接管失败');
    } finally {
      setOperating(false);
    }
  };

  const openTransfer = async () => {
    setTransferOpen(true);
    if (agents.length === 0) {
      try {
        const resp = await listAgents({ page: 1, page_size: 100 });
        setAgents((resp.data || []).map((item: API.Agent) => ({ id: item.id, name: item.name })));
      } catch (e) {
        message.error(e instanceof Error ? e.message : '坐席列表加载失败');
      }
    }
  };

  const handleTransfer = async () => {
    if (!selectedId || !transferTarget) return;
    setOperating(true);
    try {
      await transferConversation(selectedId, { to_agent_id: transferTarget });
      message.success('已转派');
      setTransferOpen(false);
      setTransferTarget(null);
      await loadConversation(selectedId);
      void loadOverview();
    } catch (e) {
      message.error(e instanceof Error ? e.message : '转派失败');
    } finally {
      setOperating(false);
    }
  };

  const handleClose = async () => {
    if (!selectedId) return;
    setOperating(true);
    try {
      await closeConversation(selectedId);
      message.success('会话已结束');
      await loadConversation(selectedId);
      void loadOverview();
    } catch (e) {
      message.error(e instanceof Error ? e.message : '结束失败');
    } finally {
      setOperating(false);
    }
  };

  const handleCreateTicket = async () => {
    if (!selectedId || !ticketForm.title.trim()) return;
    setOperating(true);
    try {
      await createTicket({
        title: ticketForm.title.trim(),
        description: ticketForm.description.trim(),
        priority: ticketForm.priority,
        customer_id: selectedSession?.customer_id,
        session_id: selectedId,
      } as Parameters<typeof createTicket>[0]);
      message.success('工单已创建');
      setTicketOpen(false);
      setTicketForm({ title: '', description: '', priority: 'normal' });
      if (selectedSession?.customer_id) {
        void listTickets({ customer_id: selectedSession.customer_id, page_size: 20 }).then(
          (resp) => setCustomerTickets(resp.data || []),
        );
      }
    } catch (e) {
      message.error(e instanceof Error ? e.message : '建单失败');
    } finally {
      setOperating(false);
    }
  };

  const renderQueueGroup = (key: string, label: string, color: string, count: number) => (
    <div
      key={key}
      onClick={() => setStatusFilter(statusFilter === key ? null : key)}
      style={{
        display: 'flex', justifyContent: 'space-between', alignItems: 'center',
        padding: '6px 10px', borderRadius: 8, cursor: 'pointer',
        background: statusFilter === key ? '#e6f4ff' : undefined,
      }}
    >
      <Space size={6}><Badge color={color} />{label}</Space>
      <Tag>{count}</Tag>
    </div>
  );

  return (
    <div style={{ height: 'calc(100vh - 112px)' }}>
      <ProCard gutter={16} ghost style={{ height: '100%' }}>
        {/* 左栏：Inbox/Queue（W1） */}
        <ProCard
          colSpan={{ xs: 24, sm: 260 }}
          title="接待队列"
          extra={<Button size="small" type="text" onClick={() => void loadOverview()} loading={overviewLoading}>刷新</Button>}
          style={{ height: '100%', overflowY: 'auto' }}
        >
          <Space direction="vertical" size={4} style={{ width: '100%' }}>
            {renderQueueGroup('waiting', '排队中', 'red', waitingCount)}
            {renderQueueGroup('active', '接待中', 'green', aiCount)}
            {renderQueueGroup('closed', '已结束', 'default', (overview?.recent_sessions || []).filter((s) => s.status === 'closed').length)}
          </Space>
          <Typography.Text type="secondary" style={{ display: 'block', margin: '12px 0 8px', fontSize: 12 }}>
            会话（{sessions.length}）
          </Typography.Text>
          {overviewLoading ? (
            <div style={{ textAlign: 'center', padding: 24 }}><Spin /></div>
          ) : sessions.length === 0 ? (
            <Empty description="暂无会话" style={{ marginTop: 24 }} />
          ) : (
            sessions.map((item) => {
              const cfg = statusCfg(item.status);
              return (
                <div
                  key={item.id}
                  onClick={() => setSelectedId(item.id)}
                  style={{
                    padding: '8px 10px', borderRadius: 8, cursor: 'pointer', marginBottom: 4,
                    background: selectedId === item.id ? '#e6f7ff' : '#fafafa',
                  }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                    <span style={{ fontWeight: 500 }}>{item.customer_name || item.id.slice(0, 8)}</span>
                    <Tag color={cfg.color} style={{ marginRight: 0 }}>{cfg.label}</Tag>
                  </div>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {item.platform || 'web'} · {item.agent_name || '未分配'}
                  </Typography.Text>
                </div>
              );
            })
          )}
        </ProCard>

        {/* 中栏：会话区（W2 + W6） */}
        <ProCard
          colSpan="auto"
          title={selectedId ? `会话 ${selectedId.slice(0, 8)}` : '会话'}
          extra={selectedSession ? (
            <Space size="middle">
              <Tag color={statusCfg(selectedSession.status).color}>
                {statusCfg(selectedSession.status).label}
              </Tag>
              {selectedSession.status !== 'closed' && (
                <>
                  {selectedSession.status === 'waiting' && (
                    <Button size="small" type="primary" onClick={handleAssign} loading={operating}>接管</Button>
                  )}
                  <Button size="small" onClick={openTransfer} loading={operating}>转派</Button>
                  <Button size="small" onClick={() => setTicketOpen(true)}>转工单</Button>
                  <Button size="small" danger onClick={handleClose} loading={operating}>结束</Button>
                </>
              )}
            </Space>
          ) : null}
          style={{ height: '100%' }}
        >
          <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
            {selectedId && (
              <Collapse
                size="small"
                style={{ marginBottom: 12 }}
                items={[{
                  key: 'timeline',
                  label: '服务过程（创建 → 分配 → 转接 → 工单）',
                  children: <ServiceTimeline sessionId={selectedId} />,
                }]}
              />
            )}
            <div style={{ flex: 1, overflowY: 'auto', padding: 12, background: '#fafafa', borderRadius: 8 }}>
              {!selectedId ? (
                <Empty description="从左侧选择一个会话开始接待。" style={{ marginTop: 96 }} />
              ) : messagesLoading ? (
                <div style={{ textAlign: 'center', padding: 80 }}><Spin /></div>
              ) : messages.length === 0 ? (
                <Empty description="暂无消息。" style={{ marginTop: 96 }} />
              ) : (
                messages.map((item) => {
                  const isAgent = item.sender === 'agent';
                  return (
                    <div key={item.id} style={{ display: 'flex', justifyContent: isAgent ? 'flex-end' : 'flex-start', marginBottom: 12 }}>
                      <div style={{
                        maxWidth: '72%',
                        background: isAgent ? '#1677ff' : '#fff',
                        color: isAgent ? '#fff' : '#000',
                        border: isAgent ? 'none' : '1px solid #f0f0f0',
                        borderRadius: 12, padding: '10px 12px',
                        boxShadow: '0 1px 2px rgba(0,0,0,0.04)',
                      }}>
                        <div style={{ fontSize: 12, opacity: 0.75, marginBottom: 4 }}>
                          {item.sender === 'agent' ? '坐席' : item.sender === 'ai' ? 'AI' : item.sender === 'system' ? '系统' : '客户'}
                          {' · '}
                          {new Date(item.created_at).toLocaleString()}
                        </div>
                        <div style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{item.content}</div>
                      </div>
                    </div>
                  );
                })
              )}
            </div>
            {selectedId && selectedSession?.status !== 'closed' && (
              <>
                {summaryText && (
                  <Alert
                    type="info"
                    closable
                    onClose={() => setSummaryText('')}
                    style={{ marginTop: 8, whiteSpace: 'pre-wrap' }}
                    message="AI 会话摘要"
                    description={summaryText}
                  />
                )}
                <Input.TextArea
                  rows={3}
                  placeholder="输入消息... (Enter 发送, Shift+Enter 换行)"
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                  style={{ marginTop: 8 }}
                  disabled={sending}
                  onPressEnter={(e) => { if (!e.shiftKey) { e.preventDefault(); void handleSend(); } }}
                />
                {/* AI 辅助工具条（W3）：AI 起草/改写，坐席编辑后发送。 */}
                <div style={{ marginTop: 8, display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
                  <Space size={4} wrap>
                    <Button size="small" onClick={() => void runCopilot('suggest_reply')} loading={copilotBusy === 'suggest_reply'}>
                      AI 建议回复
                    </Button>
                    <Button size="small" onClick={() => void runCopilot('rewrite')} loading={copilotBusy === 'rewrite'} disabled={!draft.trim()}>
                      AI 改写草稿
                    </Button>
                    <Button size="small" onClick={() => void runCopilot('session_summary')} loading={copilotBusy === 'session_summary'}>
                      AI 摘要
                    </Button>
                    <Button size="small" onClick={() => void runKnowledgeSearch()} loading={knowledgeBusy}>
                      知识检索
                    </Button>
                  </Space>
                  <Button type="primary" onClick={handleSend} loading={sending} disabled={!draft.trim()}>发送消息</Button>
                </div>
                {/* 知识建议（W4）：引用来源附 relevance（score 0-1 → 百分比）。 */}
                {knowledge && knowledge.length > 0 && (
                  <Collapse
                    size="small"
                    style={{ marginTop: 8 }}
                    items={[{
                      key: 'knowledge',
                      label: `知识库建议（${knowledge.length} 条引用）`,
                      children: (
                        <Space direction="vertical" size={8} style={{ width: '100%' }}>
                          {knowledge.map((item, index) => (
                            <div key={`${item.document_id || index}-${index}`} style={{ padding: 8, background: '#fafafa', borderRadius: 8 }}>
                              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, marginBottom: 4 }}>
                                <Typography.Text strong style={{ fontSize: 13 }}>
                                  {item.title || item.document_id || '未命名文档'}
                                </Typography.Text>
                                {typeof item.score === 'number' && (
                                  <Tag color="blue">相关度 {(item.score * 100).toFixed(0)}%</Tag>
                                )}
                              </div>
                              <Typography.Paragraph type="secondary" style={{ marginBottom: 0, fontSize: 12 }} ellipsis={{ rows: 3 }}>
                                {item.content}
                              </Typography.Paragraph>
                            </div>
                          ))}
                        </Space>
                      ),
                    }]}
                  />
                )}
              </>
            )}
          </div>
        </ProCard>

        {/* 右栏：Customer 面板（W5） */}
        <ProCard
          colSpan={{ xs: 24, sm: 300 }}
          title="客户 360"
          style={{ height: '100%', overflowY: 'auto' }}
        >
          {!customer ? (
            <Empty description="选择带客户身份的会话后查看档案。" style={{ marginTop: 48 }} />
          ) : (
            <Space direction="vertical" size={12} style={{ width: '100%' }}>
              <div>
                <Typography.Title level={5} style={{ marginBottom: 4 }}>{customer.name}</Typography.Title>
                <Space size={4} wrap>
                  {(customer.tags || []).map((tag) => <Tag key={tag} color="blue">{tag}</Tag>)}
                  {customer.priority && customer.priority !== 'normal' && (
                    <Tag color="red">{customer.priority}</Tag>
                  )}
                </Space>
              </div>
              <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block' }}>
                {customer.email || '无邮箱'} · {customer.phone || '无电话'}
              </Typography.Text>
              <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block' }}>
                {customer.company || '—'} · {customer.source || '未知来源'}
              </Typography.Text>
              <div>
                <Typography.Text strong>工单汇（{customerTickets.length}）</Typography.Text>
                {customerTickets.length === 0 ? (
                  <Typography.Text type="secondary" style={{ display: 'block', marginTop: 8, fontSize: 12 }}>
                    暂无工单。
                  </Typography.Text>
                ) : (
                  customerTickets.map((ticket) => (
                    <div key={ticket.id} style={{ padding: '6px 0', borderBottom: '1px solid #f0f0f0' }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                        <span style={{ fontSize: 13 }}>#{ticket.id} {ticket.title}</span>
                        <Tag style={{ marginRight: 0 }}>{ticket.status}</Tag>
                      </div>
                    </div>
                  ))
                )}
              </div>
            </Space>
          )}
        </ProCard>
      </ProCard>

      <Modal
        title="转派会话"
        open={transferOpen}
        onOk={handleTransfer}
        onCancel={() => setTransferOpen(false)}
        confirmLoading={operating}
        okButtonProps={{ disabled: !transferTarget }}
      >
        <Select
          style={{ width: '100%' }}
          placeholder="选择目标坐席"
          value={transferTarget}
          onChange={setTransferTarget}
          options={agents.map((item) => ({ value: item.id, label: item.name || `坐席 ${item.id}` }))}
        />
      </Modal>

      <Modal
        title="转工单"
        open={ticketOpen}
        onOk={handleCreateTicket}
        onCancel={() => setTicketOpen(false)}
        confirmLoading={operating}
        okButtonProps={{ disabled: !ticketForm.title.trim() }}
      >
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          <Input
            placeholder="工单标题（必填）"
            value={ticketForm.title}
            onChange={(e) => setTicketForm({ ...ticketForm, title: e.target.value })}
          />
          <Input.TextArea
            rows={4}
            placeholder="问题描述"
            value={ticketForm.description}
            onChange={(e) => setTicketForm({ ...ticketForm, description: e.target.value })}
          />
          <Select
            style={{ width: 160 }}
            value={ticketForm.priority}
            onChange={(v) => setTicketForm({ ...ticketForm, priority: v })}
            options={[
              { value: 'low', label: '低' },
              { value: 'normal', label: '普通' },
              { value: 'high', label: '高' },
              { value: 'urgent', label: '紧急' },
            ]}
          />
        </Space>
      </Modal>
    </div>
  );
}
