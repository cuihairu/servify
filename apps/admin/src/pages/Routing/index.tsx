import React, { useRef, useState } from 'react';
import { ProTable } from '@ant-design/pro-components';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { Button, Input, Tag, Tooltip, Typography, message } from 'antd';
import {
  getRoutingScoring,
  getTransferHistory,
  getWaitingQueue,
  processQueue,
} from '@/services/sessionTransfer';

const RoutingPage: React.FC = () => {
  const queueActionRef = useRef<ActionType>();
  const historyActionRef = useRef<ActionType>();
  const scoringActionRef = useRef<ActionType>();
  const [sessionId, setSessionId] = useState('');

  const queueColumns: ProColumns<API.TransferQueueRecord>[] = [
    { title: '会话ID', dataIndex: 'session_id', width: 180 },
    { title: '原因', dataIndex: 'reason', ellipsis: true },
    {
      title: '目标技能',
      dataIndex: 'target_skills',
      ellipsis: true,
      render: (_, record) => record.target_skills || '-',
    },
    {
      title: '优先级',
      dataIndex: 'priority',
      width: 120,
      render: (_, record) => <Tag>{record.priority || 'normal'}</Tag>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 120,
      render: (_, record) => <Tag>{record.status || 'waiting'}</Tag>,
    },
    {
      title: '入队时间',
      dataIndex: 'queued_at',
      valueType: 'dateTime',
      width: 180,
    },
    {
      title: '分配时间',
      dataIndex: 'assigned_at',
      valueType: 'dateTime',
      width: 180,
    },
  ];

  const transferColumns: ProColumns<API.SessionTransferRecord>[] = [
    { title: 'ID', dataIndex: 'id', width: 80 },
    { title: '会话ID', dataIndex: 'session_id', width: 180 },
    { title: '原客服ID', dataIndex: 'from_agent_id', width: 120 },
    { title: '目标客服ID', dataIndex: 'to_agent_id', width: 120 },
    { title: '原因', dataIndex: 'reason', ellipsis: true },
    { title: '备注', dataIndex: 'notes', ellipsis: true },
    {
      title: '转接时间',
      dataIndex: 'transferred_at',
      valueType: 'dateTime',
      width: 180,
    },
  ];

  // 分配评分审计（routing_assignments，§6.3-3 分数与因子可见）：
  // 每次经打分引擎的分配落 total_score + factors + reasons + strategy。
  const formatFactors = (factors?: Record<string, number>) => {
    if (!factors || Object.keys(factors).length === 0) return '-';
    return Object.entries(factors)
      .map(([key, value]) => `${key} ${(value * 100).toFixed(0)}`)
      .join(' / ');
  };

  const scoringColumns: ProColumns<API.RoutingAssignmentScore>[] = [
    { title: '会话ID', dataIndex: 'session_id', width: 180 },
    {
      title: '方向',
      width: 160,
      render: (_, record) =>
        `${record.from_agent_id ?? '-'} → ${record.to_agent_id ?? '-'}`,
    },
    {
      title: '总分',
      dataIndex: 'total_score',
      width: 100,
      render: (_, record) => (
        <Tag color={record.total_score >= 0.7 ? 'green' : record.total_score >= 0.4 ? 'blue' : 'default'}>
          {record.total_score.toFixed(2)}
        </Tag>
      ),
    },
    { title: '策略', dataIndex: 'strategy', width: 170, render: (_, record) => record.strategy || '-' },
    {
      title: '因子',
      ellipsis: true,
      render: (_, record) => (
        <Tooltip title={formatFactors(record.factors)}>
          <Typography.Text style={{ maxWidth: 260 }} ellipsis>
            {formatFactors(record.factors)}
          </Typography.Text>
        </Tooltip>
      ),
    },
    {
      title: '理由',
      dataIndex: 'reasons',
      ellipsis: true,
      render: (_, record) =>
        record.reasons && record.reasons.length > 0 ? record.reasons.join('；') : '-',
    },
    {
      title: '分配时间',
      dataIndex: 'assigned_at',
      valueType: 'dateTime',
      width: 180,
    },
  ];

  return (
    <div>
      <ProTable<API.TransferQueueRecord>
        headerTitle="等待队列"
        rowKey="session_id"
        columns={queueColumns}
        actionRef={queueActionRef}
        toolBarRender={() => [
          <Button
            key="process"
            type="primary"
            onClick={async () => {
              try {
                await processQueue();
                message.success('队列处理完成');
                queueActionRef.current?.reload();
                historyActionRef.current?.reload();
              } catch (error) {
                message.error('处理队列失败');
              }
            }}
          >
            处理队列
          </Button>,
        ]}
        request={async () => {
          try {
            const result = await getWaitingQueue();
            const data = result.data || [];
            return { data, total: result.count || data.length, success: true };
          } catch (error) {
            console.error('获取等待队列失败:', error);
            return { data: [], total: 0, success: true };
          }
        }}
        search={false}
        pagination={{ defaultPageSize: 10 }}
      />

      <ProTable<API.SessionTransferRecord>
        headerTitle="转接历史"
        rowKey="id"
        columns={transferColumns}
        actionRef={historyActionRef}
        style={{ marginTop: 16 }}
        toolBarRender={() => [
          <Input
            key="session-id"
            allowClear
            placeholder="按会话ID筛选"
            style={{ width: 240 }}
            value={sessionId}
            onChange={(event) => {
              setSessionId(event.target.value);
            }}
            onPressEnter={() => historyActionRef.current?.reload()}
          />,
          <Button key="search" type="primary" onClick={() => historyActionRef.current?.reload()}>
            查询
          </Button>,
        ]}
        request={async () => {
          try {
            const result = await getTransferHistory(sessionId, 50);
            return { data: result.items, total: result.count, success: true };
          } catch (error) {
            console.error('获取转接历史失败:', error);
            return { data: [], total: 0, success: true };
          }
        }}
        search={false}
        pagination={{ defaultPageSize: 10 }}
      />

      <ProTable<API.RoutingAssignmentScore>
        headerTitle="分配评分审计"
        rowKey={(record) =>
          `${record.session_id}-${record.to_agent_id ?? '-'}-${record.assigned_at ?? ''}`
        }
        columns={scoringColumns}
        actionRef={scoringActionRef}
        style={{ marginTop: 16 }}
        toolBarRender={() => [
          <Input
            key="session-id"
            allowClear
            placeholder="按会话ID查询评分"
            style={{ width: 240 }}
            value={sessionId}
            onChange={(event) => {
              setSessionId(event.target.value);
            }}
            onPressEnter={() => scoringActionRef.current?.reload()}
          />,
          <Button
            key="search"
            type="primary"
            onClick={() => scoringActionRef.current?.reload()}
          >
            查询
          </Button>,
        ]}
        request={async () => {
          if (!sessionId.trim()) {
            return { data: [], total: 0, success: true };
          }
          try {
            const result = await getRoutingScoring(sessionId, 50);
            return { data: result.data, total: result.count, success: true };
          } catch (error) {
            console.error('获取分配评分失败:', error);
            return { data: [], total: 0, success: true };
          }
        }}
        search={false}
        pagination={{ defaultPageSize: 10 }}
      />
    </div>
  );
};

export default RoutingPage;
