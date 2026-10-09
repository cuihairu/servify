import React, { useState, useEffect } from 'react';
import { ProCard, StatisticCard } from '@ant-design/pro-components';
import { Button, Input, Space, Divider, Row, Col, Spin } from 'antd';
import { getAIStatus, getAIMetrics, queryAI } from '@/services/ai';

const AIManagementPage: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [status, setStatus] = useState<API.AIStatus>({
    provider: '-',
    model: '-',
    available: false,
  });
  const [metrics, setMetrics] = useState<any>(null);
  const [queryText, setQueryText] = useState('');
  const [queryResult, setQueryResult] = useState<string>('');
  const [querying, setQuerying] = useState(false);

  useEffect(() => {
    const fetchData = async () => {
      setLoading(true);
      try {
        const [statusResult, metricsResult] = await Promise.allSettled([
          getAIStatus(),
          getAIMetrics(),
        ]);
        if (statusResult.status === 'fulfilled' && statusResult.value) {
          setStatus(statusResult.value);
        }
        if (metricsResult.status === 'fulfilled' && metricsResult.value) {
          setMetrics(metricsResult.value);
        }
      } catch (error) {
        console.error('获取 AI 状态失败:', error);
      } finally {
        setLoading(false);
      }
    };
    fetchData();
  }, []);

  const handleTestQuery = async () => {
    if (!queryText.trim()) return;
    setQuerying(true);
    try {
      const result = await queryAI({ query: queryText });
      setQueryResult(
        typeof result === 'string'
          ? result
          : JSON.stringify(result, null, 2),
      );
    } catch (error) {
      setQueryResult('查询失败，请检查 AI 服务是否可用');
    } finally {
      setQuerying(false);
    }
  };

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 80 }}>
        <Spin size="large" tip="加载中..." />
      </div>
    );
  }

  return (
    <div>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={8}>
          <StatisticCard
            statistic={{
              title: 'AI 服务商',
              value: status.provider,
            }}
          />
        </Col>
        <Col xs={24} sm={8}>
          <StatisticCard
            statistic={{
              title: '模型',
              value: status.model,
            }}
          />
        </Col>
        <Col xs={24} sm={8}>
          <StatisticCard
            statistic={{
              title: '服务状态',
              value: status.available ? '可用' : '不可用',
            }}
          />
        </Col>
      </Row>

      <ProCard title="AI 指标" style={{ marginTop: 16 }} collapsible>
        {metrics ? (
          <Row gutter={[16, 16]}>
            {metrics.latency_ms !== undefined && (
              <Col span={8}>
                <StatisticCard
                  statistic={{
                    title: '响应延迟',
                    value: metrics.latency_ms,
                    suffix: 'ms',
                  }}
                />
              </Col>
            )}
            {metrics.token_usage !== undefined && (
              <Col span={8}>
                <StatisticCard
                  statistic={{
                    title: 'Token 用量',
                    value: metrics.token_usage,
                  }}
                />
              </Col>
            )}
            {metrics.accuracy !== undefined && (
              <Col span={8}>
                <StatisticCard
                  statistic={{
                    title: '准确率',
                    value: `${(metrics.accuracy * 100).toFixed(1)}%`,
                  }}
                />
              </Col>
            )}
          </Row>
        ) : (
          <div style={{ textAlign: 'center', padding: 40, color: '#999' }}>
            暂无指标数据
          </div>
        )}
      </ProCard>

      <ProCard title="查询测试" style={{ marginTop: 16 }}>
        <Space direction="vertical" style={{ width: '100%' }}>
          <Input.TextArea
            rows={4}
            placeholder="输入测试查询内容..."
            value={queryText}
            onChange={(e) => setQueryText(e.target.value)}
          />
          <div>
            <Button
              type="primary"
              onClick={handleTestQuery}
              loading={querying}
              disabled={!queryText.trim()}
            >
              发送测试
            </Button>
          </div>
          <Divider />
          {queryResult ? (
            <div style={{ whiteSpace: 'pre-wrap', padding: 16, background: '#f5f5f5', borderRadius: 4 }}>
              {queryResult}
            </div>
          ) : (
            <div style={{ textAlign: 'center', padding: 20, color: '#999' }}>
              AI 响应结果区域
            </div>
          )}
        </Space>
      </ProCard>
    </div>
  );
};

export default AIManagementPage;
