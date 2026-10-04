import { useEffect, useState } from 'react';
import { Button, Empty, Spin, Tag, Timeline, Tooltip } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { getConversationTimeline } from '@/services/conversation';

const ACTOR_MAP: Record<string, { label: string; color: string }> = {
  system: { label: '系统', color: 'default' },
  customer: { label: '客户', color: 'blue' },
  agent: { label: '坐席', color: 'green' },
  ai: { label: 'AI', color: 'purple' },
  routing: { label: '路由', color: 'orange' },
};

const EVENT_COLOR: Record<string, string> = {
  'conversation.created': 'blue',
  'routing.agent_assigned': 'green',
  'routing.transfer_completed': 'orange',
  'ticket.created': 'red',
  'ticket.assigned': 'gold',
  'ticket.closed': 'gray',
};

/**
 * 服务过程时间线（V1.0 收敛 B1-2，W6）：展示 conversation_events 投影流水
 * （旧→新）。数据全部来自服务端投影，本组件只读不写；sessionId 为空时不加载。
 */
export default function ServiceTimeline({ sessionId }: { sessionId: string | null }) {
  const [events, setEvents] = useState<API.ConversationTimelineEvent[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = async (id: string) => {
    setLoading(true);
    setError(null);
    try {
      const resp = await getConversationTimeline(id);
      setEvents(resp.items || []);
    } catch (e) {
      setError(e instanceof Error ? e.message : '时间线加载失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!sessionId) {
      setEvents([]);
      setError(null);
      return;
    }
    void load(sessionId);
  }, [sessionId]);

  if (!sessionId) {
    return <Empty description="选择会话后查看服务过程。" />;
  }
  if (loading) {
    return <div style={{ textAlign: 'center', padding: 32 }}><Spin /></div>;
  }
  if (error) {
    return (
      <div style={{ textAlign: 'center', padding: 16 }}>
        <Button size="small" icon={<ReloadOutlined />} onClick={() => void load(sessionId)}>重试</Button>
        <div style={{ marginTop: 8, color: '#999', fontSize: 12 }}>{error}</div>
      </div>
    );
  }
  if (events.length === 0) {
    return <Empty description="暂无服务过程事件。" />;
  }
  return (
    <div style={{ padding: '4px 4px 0' }}>
      <div style={{ textAlign: 'right', marginBottom: 8 }}>
        <Tooltip title="重新加载时间线">
          <Button size="small" type="text" icon={<ReloadOutlined />} onClick={() => void load(sessionId)} />
        </Tooltip>
      </div>
      <Timeline
        items={events.map((event) => {
          const actor = ACTOR_MAP[event.actor_type || ''] || null;
          return {
            color: EVENT_COLOR[event.event_type] || 'blue',
            children: (
              <div>
                <span style={{ fontWeight: 500 }}>{event.summary || event.event_type}</span>
                {actor && <Tag style={{ marginLeft: 8 }} color={actor.color}>{actor.label}</Tag>}
                <div style={{ fontSize: 12, color: '#999', marginTop: 2 }}>
                  {new Date(event.occurred_at).toLocaleString()}
                </div>
              </div>
            ),
          };
        })}
      />
    </div>
  );
}
