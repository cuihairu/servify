import React, { useEffect, useState } from 'react';
import { ProCard } from '@ant-design/pro-components';
import { Button, Space, Spin, Statistic, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { goBack } from '@/lib/navigation';
import { getRetrievalAnalytics } from '@/services/knowledge';
import { getErrorMessage } from '@/utils/error';

// 检索分析（V1.0 收敛 B3-2，docs/v1-convergence-plan.md §8.3）：窗口内
// top 问答 / 零命中问题（知识缺口）/ 低置信问题 + 反馈计数，读
// GET /api/v1/ai/retrieval-analytics（ai_answers 聚合投影）。

const WINDOW_OPTIONS = [7, 30, 90];

function StatCard({
  title,
  analytics,
}: {
  title: string;
  analytics?: API.KnowledgeRetrievalAnalytics;
}) {
  if (!analytics) return null;
  const pct = analytics.total_answers
    ? Math.round((analytics.hit_answers / analytics.total_answers) * 100)
    : 0;
  return (
    <ProCard title={title} size="small" bordered>
      <Statistic title="AI 首答数" value={analytics.total_answers} />
      <Space size={24} style={{ marginTop: 12 }} wrap>
        <Statistic title="引用命中率" value={pct} suffix="%" />
        <Statistic title="低置信数" value={analytics.low_confidence_answers} />
        <Statistic title="平均置信" value={Number(analytics.avg_confidence.toFixed(2))} />
        <Statistic title="👍 有帮助" value={analytics.helpful_count} valueStyle={{ color: '#3f8600' }} />
        <Statistic title="👎 没帮助" value={analytics.not_helpful_count} valueStyle={{ color: '#cf1322' }} />
      </Space>
    </ProCard>
  );
}

function QuestionTable({
  title,
  rows,
  highlight,
}: {
  title: string;
  rows?: API.KnowledgeQuestionStat[];
  highlight?: 'hit' | 'miss';
}) {
  const columns: ColumnsType<API.KnowledgeQuestionStat> = [
    { title: '问题', dataIndex: 'query', ellipsis: true },
    { title: '次数', dataIndex: 'count', width: 80 },
    {
      title: '平均置信',
      dataIndex: 'avg_confidence',
      width: 100,
      render: (v: number) => (v > 0 ? v.toFixed(2) : '-'),
    },
  ];
  return (
    <ProCard title={title} size="small" bordered style={{ marginTop: 16 }}>
      {rows && rows.length > 0 ? (
        <Table<API.KnowledgeQuestionStat>
          rowKey={(r) => `${title}-${r.query}`}
          size="small"
          columns={columns}
          dataSource={rows}
          pagination={false}
        />
      ) : (
        <Typography.Text type="secondary">
          {highlight === 'miss' ? '窗口内无零命中提问（知识覆盖良好）' : '窗口内暂无数据'}
        </Typography.Text>
      )}
    </ProCard>
  );
}

const AnalyticsPage: React.FC = () => {
  const [days, setDays] = useState(7);
  const [analytics, setAnalytics] = useState<API.KnowledgeRetrievalAnalytics | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    getRetrievalAnalytics({ days, limit: 10 })
      .then((data) => {
        if (!cancelled) {
          setAnalytics(data);
          setError('');
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(getErrorMessage(err, '获取检索分析失败'));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [days]);

  return (
    <div>
      <ProCard
        title="检索分析"
        extra={
          <Space>
            <Space size={4}>
              {WINDOW_OPTIONS.map((d) => (
                <Tag
                  key={d}
                  color={d === days ? 'blue' : 'default'}
                  style={{ cursor: 'pointer' }}
                  onClick={() => setDays(d)}
                >
                  近 {d} 天
                </Tag>
              ))}
            </Space>
            <Button onClick={goBack}>返回</Button>
          </Space>
        }
      >
        {loading ? (
          <div style={{ textAlign: 'center', padding: 60 }}>
            <Spin tip="加载中..." />
          </div>
        ) : error ? (
          <Typography.Text type="danger">{error}</Typography.Text>
        ) : (
          <>
            <StatCard title={`窗口统计（近 ${analytics?.window_days ?? days} 天）`} analytics={analytics ?? undefined} />
            <QuestionTable title="Top 问答（带引用命中）" rows={analytics?.top_questions} highlight="hit" />
            <QuestionTable title="零命中问题（知识缺口信号）" rows={analytics?.no_hit_questions} highlight="miss" />
            <QuestionTable title="低置信问题（置信 < 0.65）" rows={analytics?.low_confidence_questions} />
          </>
        )}
      </ProCard>
    </div>
  );
};

export default AnalyticsPage;
