import React, { useState, useEffect, useCallback } from 'react';
import { PageContainer } from '@ant-design/pro-components';
import { ProCard, StatisticCard } from '@ant-design/pro-components';
import { Col, DatePicker, Button, Result, Row, Segmented, Select, Space, Spin, Typography, message } from 'antd';
import { Line } from '@ant-design/charts';
import {
  DownloadOutlined,
  TeamOutlined,
  UserOutlined,
  MessageOutlined,
  FileTextOutlined,
  ClockCircleOutlined,
  SmileOutlined,
  RobotOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import dayjs, { Dayjs } from 'dayjs';
import {
  exportStatistics,
  getDashboardStats,
  getTimeRangeStats,
  getTicketCategoryStats,
  getTicketPriorityStats,
  getCustomerSourceStats,
  type StatisticsExportType,
} from '@/services/statistics';
import { getErrorMessage } from '@/utils/error';

const { RangePicker } = DatePicker;
const { Text } = Typography;

const EXPORT_TYPE_OPTIONS: { label: string; value: StatisticsExportType }[] = [
  { label: '时间范围统计', value: 'time_range' },
  { label: '客服绩效', value: 'agent_performance' },
  { label: '工单分类', value: 'ticket_category' },
  { label: '工单优先级', value: 'ticket_priority' },
  { label: '客户来源', value: 'customer_source' },
  { label: '满意度趋势', value: 'satisfaction' },
];

const Dashboard: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [stats, setStats] = useState<API.DashboardStats | null>(null);
  const [timeRangeData, setTimeRangeData] = useState<API.DashboardTrendPoint[]>([]);
  const [timeRangeLoading, setTimeRangeLoading] = useState(false);
  const [dateRange, setDateRange] = useState<[Dayjs, Dayjs]>([
    dayjs().subtract(7, 'day'),
    dayjs(),
  ]);
  const [preset, setPreset] = useState<string>('7天');
  const [exportType, setExportType] = useState<StatisticsExportType>('time_range');
  const [exportFormat, setExportFormat] = useState<'csv' | 'xlsx'>('csv');
  const [exporting, setExporting] = useState(false);

  const fetchDashboard = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await getDashboardStats();
      if (result) {
        setStats(result);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '获取仪表板数据失败');
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchTimeRange = useCallback(async (start: Dayjs, end: Dayjs) => {
    setTimeRangeLoading(true);
    try {
      const result = await getTimeRangeStats({
        start_date: start.format('YYYY-MM-DD'),
        end_date: end.format('YYYY-MM-DD'),
      });
      if (result) {
        const chartData: API.DashboardTrendPoint[] = [];
        result.forEach((item) => {
          chartData.push(
            { date: item.date, value: item.tickets || 0, type: '工单' },
            { date: item.date, value: item.sessions || 0, type: '会话' },
            { date: item.date, value: item.messages || 0, type: '消息' },
          );
        });
        setTimeRangeData(chartData);
      }
    } catch {
      message.error('获取趋势数据失败');
    } finally {
      setTimeRangeLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchDashboard();
  }, [fetchDashboard]);

  useEffect(() => {
    fetchTimeRange(dateRange[0], dateRange[1]);
  }, [dateRange, fetchTimeRange]);

  const handlePresetChange = (value: string) => {
    setPreset(value);
    const end = dayjs();
    let start: Dayjs;
    switch (value) {
      case '7天':
        start = end.subtract(7, 'day');
        break;
      case '30天':
        start = end.subtract(30, 'day');
        break;
      case '90天':
        start = end.subtract(90, 'day');
        break;
      default:
        start = end.subtract(7, 'day');
    }
    setDateRange([start, end]);
  };

  const handleExport = async () => {
    setExporting(true);
    try {
      const blob = await exportStatistics({
        type: exportType,
        format: exportFormat,
        from: dateRange[0].format('YYYY-MM-DD'),
        to: dateRange[1].format('YYYY-MM-DD'),
      });
      const filename = `servify-${exportType}-${dayjs().format('YYYYMMDD_HHmmss')}.${exportFormat}`;
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = filename;
      link.click();
      URL.revokeObjectURL(url);
      message.success('报表已导出');
    } catch (err) {
      message.error(getErrorMessage(err, '导出失败，请稍后重试'));
    } finally {
      setExporting(false);
    }
  };

  if (error) {
    return (
      <PageContainer>
        <Result
          status="error"
          title="加载失败"
          subTitle={error}
          extra={
            <Button type="primary" icon={<ReloadOutlined />} onClick={fetchDashboard}>
              重试
            </Button>
          }
        />
      </PageContainer>
    );
  }

  if (loading || !stats) {
    return (
      <PageContainer>
        <div style={{ textAlign: 'center', padding: 80 }}>
          <Spin size="large" tip="加载中..." />
        </div>
      </PageContainer>
    );
  }

  const formatSeconds = (seconds: number) => {
    if (!seconds || seconds <= 0) return '-';
    const mins = Math.floor(seconds / 60);
    const secs = Math.floor(seconds % 60);
    return mins > 0 ? `${mins}分${secs}秒` : `${secs}秒`;
  };

  const lineConfig = {
    data: timeRangeData,
    xField: 'date',
    yField: 'value',
    colorField: 'type',
    point: { shapeField: 'square', sizeField: 4 },
    interaction: { tooltip: { marker: false } },
    style: { lineWidth: 2 },
  };

  return (
    <PageContainer>
      {/* 今日概览 */}
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <StatisticCard
            statistic={{
              title: '今日工单',
              value: stats.today_tickets,
              icon: <FileTextOutlined style={{ fontSize: 24, color: '#1890ff' }} />,
              description: (
                <span>
                  总计 {stats.total_tickets} / 开放 {stats.open_tickets}
                </span>
              ),
            }}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatisticCard
            statistic={{
              title: '今日会话',
              value: stats.today_sessions,
              icon: <MessageOutlined style={{ fontSize: 24, color: '#52c41a' }} />,
              description: (
                <span>
                  活跃 {stats.active_sessions} / 总计 {stats.total_sessions}
                </span>
              ),
            }}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatisticCard
            statistic={{
              title: '今日消息',
              value: stats.today_messages,
              icon: <MessageOutlined style={{ fontSize: 24, color: '#722ed1' }} />,
            }}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatisticCard
            statistic={{
              title: '客户满意度',
              value: stats.customer_satisfaction?.toFixed(1) || '0.0',
              suffix: '/ 5.0',
              icon: <SmileOutlined style={{ fontSize: 24, color: '#faad14' }} />,
            }}
          />
        </Col>
      </Row>

      {/* 资源概览 */}
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} sm={12} lg={6}>
          <StatisticCard
            statistic={{
              title: '在线客服',
              value: stats.online_agents,
              icon: <UserOutlined style={{ fontSize: 24, color: '#13c2c2' }} />,
              description: (
                <span>
                  在线 {stats.online_agents} / 忙碌 {stats.busy_agents} / 总计{' '}
                  {stats.total_agents}
                </span>
              ),
            }}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatisticCard
            statistic={{
              title: '总客户数',
              value: stats.total_customers,
              icon: <TeamOutlined style={{ fontSize: 24, color: '#eb2f96' }} />,
            }}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatisticCard
            statistic={{
              title: '平均响应时间',
              value: formatSeconds(stats.avg_response_time),
              icon: <ClockCircleOutlined style={{ fontSize: 24, color: '#fa8c16' }} />,
            }}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatisticCard
            statistic={{
              title: 'AI 使用次数(今日)',
              value: stats.ai_usage_today || 0,
              icon: <RobotOutlined style={{ fontSize: 24, color: '#2f54eb' }} />,
            }}
          />
        </Col>
      </Row>

      {/* 工单状态分布 */}
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} sm={12} lg={6}>
          <StatisticCard statistic={{ title: '开放工单', value: stats.open_tickets }} />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatisticCard statistic={{ title: '已分配工单', value: stats.assigned_tickets }} />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatisticCard statistic={{ title: '已解决工单', value: stats.resolved_tickets }} />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatisticCard statistic={{ title: '已关闭工单', value: stats.closed_tickets }} />
        </Col>
      </Row>

      {/* 报表导出（时间范围与趋势图共用选择器） */}
      <ProCard title="报表导出" style={{ marginTop: 16 }}>
        <Space wrap>
          <Select
            value={exportType}
            options={EXPORT_TYPE_OPTIONS}
            style={{ width: 168 }}
            onChange={(val) => setExportType(val as StatisticsExportType)}
          />
          <Segmented
            options={[
              { label: 'CSV', value: 'csv' },
              { label: 'Excel', value: 'xlsx' },
            ]}
            value={exportFormat}
            onChange={(val) => setExportFormat(val as 'csv' | 'xlsx')}
          />
          <Text type="secondary">
            时间范围 {dateRange[0].format('YYYY-MM-DD')} ~ {dateRange[1].format('YYYY-MM-DD')}
            （与趋势图一致）
          </Text>
          <Button
            type="primary"
            icon={<DownloadOutlined />}
            loading={exporting}
            onClick={handleExport}
          >
            导出
          </Button>
        </Space>
      </ProCard>

      {/* 趋势图 */}
      <ProCard
        title="趋势分析"
        style={{ marginTop: 16 }}
        extra={
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <Segmented
              options={['7天', '30天', '90天']}
              value={preset}
              onChange={(val) => handlePresetChange(val as string)}
            />
            <RangePicker
              value={dateRange}
              onChange={(dates) => {
                if (dates && dates[0] && dates[1]) {
                  setPreset('');
                  setDateRange([dates[0], dates[1]]);
                }
              }}
            />
          </div>
        }
      >
        <Spin spinning={timeRangeLoading}>
          {timeRangeData.length > 0 ? (
            <Line {...lineConfig} />
          ) : (
            <div style={{ textAlign: 'center', padding: 40, color: '#999' }}>
              暂无趋势数据
            </div>
          )}
        </Spin>
      </ProCard>
    </PageContainer>
  );
};

export default Dashboard;
