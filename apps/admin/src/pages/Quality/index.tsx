import React, { useRef, useState } from 'react';
import {
  ActionType,
  ProColumns,
  ProTable,
} from '@ant-design/pro-components';
import {
  Button,
  Descriptions,
  Drawer,
  Form,
  Input,
  InputNumber,
  message,
  Modal,
  Popconfirm,
  Progress,
  Select,
  Space,
  Tag,
  Typography,
} from 'antd';
import {
  confirmQualityReview,
  getQualityReview,
  listQualityReviews,
  rescoreQualityReview,
} from '@/services/quality';

const STATUS_META: Record<string, { color: string; text: string }> = {
  pending: { color: 'default', text: '待打分' },
  skipped: { color: 'default', text: '已跳过' },
  scored: { color: 'processing', text: '已打分' },
  failed: { color: 'error', text: '打分失败' },
  confirmed: { color: 'success', text: '已确认' },
};

const SEVERITY_META: Record<string, { color: string; text: string }> = {
  low: { color: 'gold', text: '低' },
  medium: { color: 'orange', text: '中' },
  high: { color: 'red', text: '高' },
};

const TRIGGER_TEXT: Record<string, string> = {
  worker: '自动扫描',
  manual: '人工',
  rescore: '重新打分',
};

function formatDuration(seconds: number): string {
  if (!seconds) return '-';
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return m > 0 ? `${m}分${s}秒` : `${s}秒`;
}

function parseJSONArray<T>(raw: string): T[] {
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? (parsed as T[]) : [];
  } catch {
    return [];
  }
}

function parseJSONObject<T>(raw: string): Record<string, T> {
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? (parsed as Record<string, T>)
      : {};
  } catch {
    return {};
  }
}

const QualityPage: React.FC = () => {
  const actionRef = useRef<ActionType>();
  const [detailOpen, setDetailOpen] = useState(false);
  const [detail, setDetail] = useState<API.QualityReview | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [confirmForm] = Form.useForm();
  const [submitting, setSubmitting] = useState(false);

  const openDetail = async (sessionId: string) => {
    setDetailOpen(true);
    setDetailLoading(true);
    try {
      const review = await getQualityReview(sessionId);
      setDetail(review);
    } catch (error) {
      console.error('获取质检详情失败:', error);
      message.error('获取质检详情失败');
      setDetailOpen(false);
    } finally {
      setDetailLoading(false);
    }
  };

  const handleConfirm = async () => {
    if (!detail) return;
    const values = await confirmForm.validateFields();
    setSubmitting(true);
    try {
      await confirmQualityReview(detail.session_id, {
        manual_score: values.manual_score ?? undefined,
        manual_result: values.manual_result,
        review_note: values.review_note,
      });
      message.success('已确认');
      setConfirmOpen(false);
      confirmForm.resetFields();
      await openDetail(detail.session_id);
      actionRef.current?.reload();
    } catch (error) {
      console.error('确认质检失败:', error);
      message.error('确认质检失败');
    } finally {
      setSubmitting(false);
    }
  };

  const handleRescore = async (record: API.QualityReview) => {
    try {
      await rescoreQualityReview(record.session_id, record.status === 'confirmed');
      message.success('已加入重新打分队列');
      if (detail?.session_id === record.session_id) {
        await openDetail(record.session_id);
      }
      actionRef.current?.reload();
    } catch (error) {
      console.error('重新打分失败:', error);
      message.error('重新打分失败');
    }
  };

  const columns: ProColumns<API.QualityReview>[] = [
    {
      title: '会话 ID',
      dataIndex: 'session_id',
      width: 260,
      copyable: true,
      ellipsis: true,
    },
    {
      title: '坐席 ID',
      dataIndex: 'agent_id',
      width: 90,
      render: (_, record) => record.agent_id ?? '-',
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      valueType: 'select',
      valueEnum: Object.fromEntries(
        Object.entries(STATUS_META).map(([k, v]) => [k, { text: v.text }]),
      ),
      render: (_, record) => {
        const meta = STATUS_META[record.status] || { color: 'default', text: record.status };
        return <Tag color={meta.color}>{meta.text}</Tag>;
      },
    },
    {
      title: '违规',
      dataIndex: 'violation_count',
      width: 120,
      render: (_, record) => {
        if (!record.violation_count) return <Tag>无</Tag>;
        const sev = SEVERITY_META[record.max_severity] || { color: 'default', text: record.max_severity || '-' };
        return (
          <Space size={4}>
            <Tag color="red">{record.violation_count} 项</Tag>
            <Tag color={sev.color}>{sev.text}</Tag>
          </Space>
        );
      },
    },
    {
      title: '严重度',
      dataIndex: 'has_violations',
      hideInTable: true,
      valueType: 'select',
      valueEnum: {
        true: { text: '有违规' },
        false: { text: '无违规' },
      },
    },
    {
      title: 'LLM 总分',
      dataIndex: 'llm_total_score',
      width: 100,
      hideInSearch: true,
      render: (_, record) =>
        record.llm_total_score != null ? record.llm_total_score.toFixed(1) : '-',
    },
    {
      title: '分数下限',
      dataIndex: 'min_score',
      hideInTable: true,
      valueType: 'digit',
    },
    {
      title: '分数上限',
      dataIndex: 'max_score',
      hideInTable: true,
      valueType: 'digit',
    },
    {
      title: '人工分',
      dataIndex: 'manual_score',
      width: 90,
      hideInSearch: true,
      render: (_, record) => (record.manual_score != null ? record.manual_score.toFixed(1) : '-'),
    },
    {
      title: '消息数',
      dataIndex: 'message_count',
      width: 80,
      hideInSearch: true,
    },
    {
      title: '时长',
      dataIndex: 'duration_seconds',
      width: 110,
      hideInSearch: true,
      render: (_, record) => formatDuration(record.duration_seconds),
    },
    {
      title: '触发方式',
      dataIndex: 'trigger',
      width: 100,
      hideInSearch: true,
      render: (_, record) => TRIGGER_TEXT[record.trigger] || record.trigger,
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      valueType: 'dateTime',
      width: 170,
      hideInSearch: true,
    },
    {
      title: '操作',
      valueType: 'option',
      width: 160,
      fixed: 'right',
      render: (_, record) => [
        <a key="detail" onClick={() => openDetail(record.session_id)}>
          详情
        </a>,
        <Popconfirm
          key="rescore"
          title="重新打分"
          description={
            record.status === 'confirmed'
              ? '该记录已人工确认，重打将覆盖人工结论，确定继续？'
              : '重置为待打分并重新质检？'
          }
          onConfirm={() => handleRescore(record)}
        >
          <a>重打</a>
        </Popconfirm>,
      ],
    },
  ];

  const violations = detail ? parseJSONArray<API.QualityViolation>(detail.violations_json) : [];
  const dimensions = detail ? parseJSONObject<API.QualityDimensionScore>(detail.dimensions_json) : {};

  return (
    <>
      <ProTable<API.QualityReview>
        headerTitle="质检抽检"
        rowKey="session_id"
        actionRef={actionRef}
        columns={columns}
        request={async (params) => {
          try {
            const result = await listQualityReviews({
              page: params.current,
              page_size: params.pageSize,
              status: params.status,
              has_violations: params.has_violations
                ? params.has_violations === 'true'
                : undefined,
              min_score: params.min_score,
              max_score: params.max_score,
            });
            return {
              data: result?.items || [],
              total: result?.total || 0,
              success: true,
            };
          } catch (error) {
            console.error('获取质检记录失败:', error);
            return { data: [], total: 0, success: true };
          }
        }}
        search={{ filterType: 'light' }}
        pagination={{ defaultPageSize: 20 }}
        scroll={{ x: 1400 }}
      />

      <Drawer
        title={detail ? `质检详情 · ${detail.session_id}` : '质检详情'}
        width={640}
        open={detailOpen}
        loading={detailLoading}
        onClose={() => setDetailOpen(false)}
        extra={
          detail && (
            <Space>
              {detail.status === 'scored' && (
                <Button
                  type="primary"
                  onClick={() => {
                    confirmForm.resetFields();
                    setConfirmOpen(true);
                  }}
                >
                  确认
                </Button>
              )}
              <Popconfirm
                title="重新打分"
                description={
                  detail.status === 'confirmed'
                    ? '该记录已人工确认，重打将覆盖人工结论，确定继续？'
                    : '重置为待打分并重新质检？'
                }
                onConfirm={() => handleRescore(detail)}
              >
                <Button>重新打分</Button>
              </Popconfirm>
            </Space>
          )
        }
      >
        {detail && (
          <Space direction="vertical" size="large" style={{ width: '100%' }}>
            <Descriptions column={2} size="small" bordered>
              <Descriptions.Item label="状态">
                <Tag color={(STATUS_META[detail.status] || { color: 'default' }).color}>
                  {(STATUS_META[detail.status] || { text: detail.status }).text}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="触发方式">
                {TRIGGER_TEXT[detail.trigger] || detail.trigger}
              </Descriptions.Item>
              <Descriptions.Item label="坐席 ID">{detail.agent_id ?? '-'}</Descriptions.Item>
              <Descriptions.Item label="客户 ID">{detail.customer_id ?? '-'}</Descriptions.Item>
              <Descriptions.Item label="消息数">{detail.message_count}</Descriptions.Item>
              <Descriptions.Item label="会话时长">{formatDuration(detail.duration_seconds)}</Descriptions.Item>
              <Descriptions.Item label="LLM 打分">
                {detail.llm_total_score != null
                  ? `${detail.llm_total_score.toFixed(1)}（${detail.llm_provider}/${detail.llm_model}）`
                  : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="打分时间">
                {detail.scored_at ? new Date(detail.scored_at).toLocaleString() : '-'}
              </Descriptions.Item>
              {detail.manual_score != null && (
                <>
                  <Descriptions.Item label="人工覆盖分">{detail.manual_score.toFixed(1)}</Descriptions.Item>
                  <Descriptions.Item label="人工结论">
                    {detail.manual_result === 'pass'
                      ? '通过'
                      : detail.manual_result === 'violation'
                        ? '违规'
                        : '-'}
                  </Descriptions.Item>
                </>
              )}
              {detail.review_note && (
                <Descriptions.Item label="复核备注" span={2}>
                  {detail.review_note}
                </Descriptions.Item>
              )}
              {detail.reviewed_by != null && (
                <Descriptions.Item label="复核人 ID" span={2}>
                  {detail.reviewed_by}
                  {detail.reviewed_at ? `（${new Date(detail.reviewed_at).toLocaleString()}）` : ''}
                </Descriptions.Item>
              )}
            </Descriptions>

            {detail.status === 'failed' && (
              <Descriptions
                title="失败诊断"
                column={1}
                size="small"
                bordered
                style={{ marginTop: -12 }}
              >
                <Descriptions.Item label="错误">
                  <Typography.Text type="danger">{detail.last_error || '-'}</Typography.Text>
                </Descriptions.Item>
                <Descriptions.Item label="已尝试次数">{detail.attempt_count}</Descriptions.Item>
                <Descriptions.Item label="下次重试">
                  {detail.next_retry_at ? new Date(detail.next_retry_at).toLocaleString() : '-'}
                </Descriptions.Item>
              </Descriptions>
            )}

            {detail.llm_summary && (
              <div>
                <Typography.Title level={5}>LLM 评语</Typography.Title>
                <Typography.Paragraph>{detail.llm_summary}</Typography.Paragraph>
              </div>
            )}

            {Object.keys(dimensions).length > 0 && (
              <div>
                <Typography.Title level={5}>维度得分</Typography.Title>
                <Space direction="vertical" size="middle" style={{ width: '100%' }}>
                  {Object.entries(dimensions).map(([key, dim]) => (
                    <div key={key}>
                      <Space style={{ justifyContent: 'space-between', width: '100%' }}>
                        <Typography.Text>{key}</Typography.Text>
                        <Typography.Text type="secondary">
                          {dim.score} / 10（权重 {(dim.weight * 100).toFixed(0)}%）
                        </Typography.Text>
                      </Space>
                      <Progress
                        percent={dim.score * 10}
                        strokeColor={dim.score >= 8 ? '#52c41a' : dim.score >= 6 ? '#faad14' : '#ff4d4f'}
                      />
                      {dim.reason && (
                        <Typography.Text type="secondary">{dim.reason}</Typography.Text>
                      )}
                    </div>
                  ))}
                </Space>
              </div>
            )}

            <div>
              <Typography.Title level={5}>
                规则违规{detail.violation_count ? `（${detail.violation_count}）` : '（无）'}
              </Typography.Title>
              {violations.length === 0 ? (
                <Typography.Text type="secondary">未命中规则</Typography.Text>
              ) : (
                <Space direction="vertical" size="small" style={{ width: '100%' }}>
                  {violations.map((v, idx) => {
                    const sev = SEVERITY_META[v.severity] || {
                      color: 'default',
                      text: v.severity,
                    };
                    return (
                      <div key={idx}>
                        <Space size={8}>
                          <Tag color={sev.color}>{sev.text}</Tag>
                          <Typography.Text strong>{v.rule}</Typography.Text>
                        </Space>
                        {v.detail && (
                          <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
                            {v.detail}
                          </Typography.Paragraph>
                        )}
                        {v.snippet && (
                          <Typography.Paragraph code style={{ marginBottom: 0 }}>
                            {v.snippet}
                          </Typography.Paragraph>
                        )}
                      </div>
                    );
                  })}
                </Space>
              )}
            </div>
          </Space>
        )}
      </Drawer>

      <Modal
        title="人工确认"
        open={confirmOpen}
        confirmLoading={submitting}
        onOk={handleConfirm}
        onCancel={() => setConfirmOpen(false)}
        destroyOnClose
      >
        <Form form={confirmForm} layout="vertical" initialValues={{ manual_result: 'pass' }}>
          <Form.Item
            name="manual_result"
            label="结论"
            rules={[{ required: true, message: '请选择结论' }]}
          >
            <Select
              options={[
                { value: 'pass', label: '通过' },
                { value: 'violation', label: '违规' },
              ]}
            />
          </Form.Item>
          <Form.Item name="manual_score" label="覆盖分（0-10，可选）">
            <InputNumber min={0} max={10} step={0.1} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="review_note" label="复核备注">
            <Input.TextArea rows={3} placeholder="抽检说明" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
};

export default QualityPage;
