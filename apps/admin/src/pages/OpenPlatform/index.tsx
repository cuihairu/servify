import React, { useEffect, useRef, useState } from 'react';
import { PageContainer } from '@ant-design/pro-components';
import { ProTable } from '@ant-design/pro-components';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { PlusOutlined } from '@ant-design/icons';
import {
  Alert,
  Button,
  Checkbox,
  DatePicker,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Switch,
  Tabs,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd';
import {
  createAPIKey,
  createWebhook,
  deleteAPIKey,
  deleteWebhook,
  listAPIKeys,
  listWebhookDeliveries,
  listWebhookEvents,
  listWebhooks,
  redeliverWebhookDelivery,
  revokeAPIKey,
  rotateWebhookSecret,
  testWebhook,
  updateWebhook,
} from '@/services/openPlatform';
import { getErrorMessage, isFormValidationError } from '@/utils/error';
import type { Dayjs } from 'dayjs';

const deliveryStatusMeta: Record<string, { color: string; label: string }> = {
  pending: { color: 'processing', label: '待投递' },
  success: { color: 'success', label: '成功' },
  failed: { color: 'warning', label: '失败待重试' },
  dead: { color: 'error', label: '死信' },
};

function prettyJson(value?: string) {
  if (!value) {
    return '';
  }
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
}

/** 一次性明文结果弹窗：关闭后无法再看（secret/明文不在任何列表响应中回显） */
function showOneTimeSecretModal(title: string, secret: string) {
  Modal.success({
    title,
    width: 640,
    okText: '我已保存',
    content: (
      <div style={{ marginTop: 16 }}>
        <Alert
          type="warning"
          showIcon
          message="请立即复制保存，关闭后无法再次查看"
          style={{ marginBottom: 12 }}
        />
        <Typography.Paragraph copyable code style={{ marginBottom: 0, wordBreak: 'break-all' }}>
          {secret}
        </Typography.Paragraph>
      </div>
    ),
  });
}

// ---------- Webhook 订阅页签 ----------

const WebhooksTab: React.FC = () => {
  const actionRef = useRef<ActionType>();
  const [form] = Form.useForm();
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<API.WebhookEndpoint | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [supportedEvents, setSupportedEvents] = useState<string[]>([]);

  useEffect(() => {
    listWebhookEvents()
      .then(setSupportedEvents)
      .catch(() => setSupportedEvents([]));
  }, []);

  const openCreate = () => {
    setEditing(null);
    form.resetFields();
    form.setFieldsValue({ active: true, events: [] });
    setModalOpen(true);
  };

  const openEdit = (record: API.WebhookEndpoint) => {
    setEditing(record);
    form.resetFields();
    form.setFieldsValue({
      name: record.name,
      url: record.url,
      description: record.description,
      events: record.events ? record.events.split(',').filter(Boolean) : [],
      active: record.active ?? true,
    });
    setModalOpen(true);
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setSubmitting(true);
      const payload = {
        name: values.name?.trim(),
        url: values.url?.trim(),
        description: values.description?.trim(),
        // 空数组 = 订阅全部事件（后端 events 空串语义）
        events: (values.events ?? []).join(','),
        active: values.active ?? true,
      };
      if (editing) {
        await updateWebhook(editing.id, payload);
        message.success('订阅已更新');
      } else {
        const result = await createWebhook(payload);
        showOneTimeSecretModal('订阅已创建，签名密钥如下', result.secret);
      }
      setModalOpen(false);
      actionRef.current?.reload();
    } catch (error: unknown) {
      if (isFormValidationError(error)) {
        return;
      }
      message.error(getErrorMessage(error, editing ? '更新失败' : '创建失败'));
    } finally {
      setSubmitting(false);
    }
  };

  const columns: ProColumns<API.WebhookEndpoint>[] = [
    { title: 'ID', dataIndex: 'id', width: 64 },
    { title: '名称', dataIndex: 'name', search: false },
    {
      title: '回调地址',
      dataIndex: 'url',
      search: false,
      ellipsis: true,
      render: (_, record) => (
        <Typography.Text code copyable={{ text: record.url }}>
          {record.url}
        </Typography.Text>
      ),
    },
    {
      title: '订阅事件',
      dataIndex: 'events',
      search: false,
      render: (_, record) => {
        const events = record.events ? record.events.split(',').filter(Boolean) : [];
        if (events.length === 0) {
          return <Tag color="blue">全部事件</Tag>;
        }
        return (
          <Tooltip title={events.join('、')}>
            <span>{events.slice(0, 2).join('、')}{events.length > 2 ? ` 等 ${events.length} 项` : ''}</span>
          </Tooltip>
        );
      },
    },
    {
      title: '状态',
      dataIndex: 'active',
      width: 90,
      search: false,
      render: (_, record) => (
        <Switch checked={Boolean(record.active)} checkedChildren="启用" unCheckedChildren="停用" disabled />
      ),
    },
    { title: '创建时间', dataIndex: 'created_at', valueType: 'dateTime', width: 170, search: false },
    {
      title: '操作',
      valueType: 'option',
      width: 200,
      render: (_, record) => (
        <Space>
          <a onClick={() => openEdit(record)}>编辑</a>
          <a
            onClick={async () => {
              try {
                const result = await rotateWebhookSecret(record.id);
                showOneTimeSecretModal('签名密钥已轮换', result.secret);
              } catch (error: unknown) {
                message.error(getErrorMessage(error, '轮换失败'));
              }
            }}
          >
            轮换密钥
          </a>
          <a
            onClick={async () => {
              try {
                const delivery = await testWebhook(record.id);
                if (delivery.status === 'success') {
                  message.success(`测试事件投递成功（HTTP ${delivery.http_status ?? 200}）`);
                } else {
                  message.warning(`测试事件已发送，投递状态：${delivery.status}`);
                }
                actionRef.current?.reload();
              } catch (error: unknown) {
                message.error(getErrorMessage(error, '测试失败'));
              }
            }}
          >
            测试
          </a>
          <a
            onClick={() => {
              Modal.confirm({
                title: '删除该 Webhook 订阅？',
                content: `删除后「${record.name}」将不再收到任何事件推送。`,
                okText: '删除',
                okButtonProps: { danger: true },
                cancelText: '取消',
                onOk: async () => {
                  try {
                    await deleteWebhook(record.id);
                    message.success('订阅已删除');
                    actionRef.current?.reload();
                  } catch (error: unknown) {
                    message.error(getErrorMessage(error, '删除失败'));
                  }
                },
              });
            }}
          >
            删除
          </a>
        </Space>
      ),
    },
  ];

  return (
    <>
      <ProTable<API.WebhookEndpoint>
        headerTitle="Webhook 订阅"
        rowKey="id"
        actionRef={actionRef}
        search={false}
        columns={columns}
        toolBarRender={() => [
          <Button key="create" type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新建订阅
          </Button>,
        ]}
        request={async () => {
          try {
            const data = await listWebhooks();
            return { data, total: data.length, success: true };
          } catch (error: unknown) {
            console.error('获取 Webhook 订阅失败:', error);
            return { data: [], total: 0, success: true };
          }
        }}
        pagination={false}
      />

      <Modal
        title={editing ? '编辑订阅' : '新建订阅'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={handleSubmit}
        confirmLoading={submitting}
        okText={editing ? '保存' : '创建'}
        width={680}
      >
        <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="例如：内部工单同步" />
          </Form.Item>
          <Form.Item
            name="url"
            label="回调地址"
            rules={[
              { required: true, message: '请输入 https 回调地址' },
              { type: 'url', message: '必须是合法 URL' },
            ]}
          >
            <Input placeholder="https://example.com/hooks/servify" />
          </Form.Item>
          <Form.Item name="events" label="订阅事件（不选 = 订阅全部）">
            <Checkbox.Group
              style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', rowGap: 8 }}
              options={supportedEvents}
            />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} placeholder="用途说明（可选）" />
          </Form.Item>
          <Form.Item name="active" label="启用" valuePropName="checked">
            <Switch checkedChildren="启用" unCheckedChildren="停用" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
};

// ---------- 投递日志页签 ----------

const DeliveriesTab: React.FC = () => {
  const actionRef = useRef<ActionType>();

  const columns: ProColumns<API.WebhookDelivery>[] = [
    { title: 'ID', dataIndex: 'id', width: 72 },
    { title: '端点', dataIndex: 'endpoint_id', width: 72 },
    { title: '事件', dataIndex: 'event_name', width: 200, search: false },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      valueType: 'select',
      fieldProps: {
        options: Object.entries(deliveryStatusMeta).map(([value, meta]) => ({
          value,
          label: meta.label,
        })),
      },
      render: (_, record) => {
        const meta = deliveryStatusMeta[record.status] ?? { color: 'default', label: record.status };
        return <Tag color={meta.color}>{meta.label}</Tag>;
      },
    },
    {
      title: 'HTTP',
      dataIndex: 'http_status',
      width: 70,
      search: false,
      render: (_, record) => (record.http_status ? record.http_status : '-'),
    },
    {
      title: '尝试',
      dataIndex: 'attempt',
      width: 64,
      search: false,
      render: (_, record) => (record.attempt ?? 0) + 1,
    },
    {
      title: '耗时',
      dataIndex: 'duration_ms',
      width: 80,
      search: false,
      render: (_, record) => (record.duration_ms != null ? `${record.duration_ms}ms` : '-'),
    },
    {
      title: '下次重试',
      dataIndex: 'next_retry_at',
      valueType: 'dateTime',
      width: 170,
      search: false,
      render: (_, record) => record.next_retry_at ?? '-',
    },
    {
      title: '错误',
      dataIndex: 'last_error',
      search: false,
      ellipsis: true,
      render: (_, record) => record.last_error || '-',
    },
    { title: '时间', dataIndex: 'created_at', valueType: 'dateTime', width: 170, search: false },
    {
      title: '操作',
      valueType: 'option',
      width: 90,
      render: (_, record) => (
        <a
          onClick={async () => {
            try {
              await redeliverWebhookDelivery(record.id);
              message.success('已重置为待投递，worker 将立即重试');
              actionRef.current?.reload();
            } catch (error: unknown) {
              message.error(getErrorMessage(error, '重投失败'));
            }
          }}
        >
          重投
        </a>
      ),
    },
  ];

  return (
    <ProTable<API.WebhookDelivery>
      headerTitle="投递日志"
      rowKey="id"
      actionRef={actionRef}
      search={{ labelWidth: 'auto' }}
      columns={columns}
      request={async (params) => {
        try {
          const result = await listWebhookDeliveries({
            endpoint_id:
              typeof params.endpoint_id === 'string' && params.endpoint_id !== ''
                ? Number(params.endpoint_id)
                : undefined,
            status: typeof params.status === 'string' ? params.status : undefined,
            page: params.current,
            page_size: params.pageSize,
          });
          return { data: result.data, total: result.total, success: true };
        } catch (error: unknown) {
          console.error('获取投递日志失败:', error);
          return { data: [], total: 0, success: true };
        }
      }}
      pagination={{ defaultPageSize: 20 }}
      expandable={{
        expandedRowRender: (record) => (
          <div style={{ display: 'grid', gap: 8 }}>
            {record.event_id ? <div>事件 ID：{record.event_id}</div> : null}
            {record.aggregate_id ? <div>聚合对象：{record.aggregate_id}</div> : null}
            <pre style={{ whiteSpace: 'pre-wrap', margin: 0 }}>{prettyJson(record.payload) || '（无载荷）'}</pre>
          </div>
        ),
      }}
    />
  );
};

// ---------- API Keys 页签 ----------

const APIKeysTab: React.FC = () => {
  const actionRef = useRef<ActionType>();
  const [form] = Form.useForm();
  const [modalOpen, setModalOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const handleCreate = async () => {
    try {
      const values = await form.validateFields();
      setSubmitting(true);
      const expiresAt: Dayjs | undefined = values.expires_at;
      const result = await createAPIKey({
        name: values.name?.trim(),
        workspace_id: values.workspace_id?.trim() || undefined,
        scopes: values.scopes?.trim() || undefined,
        expires_at: expiresAt ? expiresAt.toISOString() : undefined,
      });
      setModalOpen(false);
      form.resetFields();
      actionRef.current?.reload();
      showOneTimeSecretModal('API Key 已签发', result.plaintext);
    } catch (error: unknown) {
      if (isFormValidationError(error)) {
        return;
      }
      message.error(getErrorMessage(error, '签发失败'));
    } finally {
      setSubmitting(false);
    }
  };

  const columns: ProColumns<API.APIKey>[] = [
    { title: 'ID', dataIndex: 'id', width: 64 },
    { title: '名称', dataIndex: 'name', search: false },
    {
      title: '密钥前缀',
      dataIndex: 'prefix',
      search: false,
      render: (_, record) => <Typography.Text code>{record.prefix}…</Typography.Text>,
    },
    {
      title: '权限',
      dataIndex: 'scopes',
      search: false,
      render: (_, record) => (record.scopes ? record.scopes : '默认（只读）'),
    },
    {
      title: '最近使用',
      dataIndex: 'last_used_at',
      valueType: 'dateTime',
      width: 170,
      search: false,
      render: (_, record) => record.last_used_at ?? '从未使用',
    },
    {
      title: '过期时间',
      dataIndex: 'expires_at',
      valueType: 'dateTime',
      width: 170,
      search: false,
      render: (_, record) => record.expires_at ?? '永不过期',
    },
    {
      title: '状态',
      dataIndex: 'revoked_at',
      width: 90,
      search: false,
      render: (_, record) =>
        record.revoked_at ? <Tag color="error">已吊销</Tag> : <Tag color="success">有效</Tag>,
    },
    { title: '创建时间', dataIndex: 'created_at', valueType: 'dateTime', width: 170, search: false },
    {
      title: '操作',
      valueType: 'option',
      width: 130,
      render: (_, record) => (
        <Space>
          {!record.revoked_at ? (
            <a
              onClick={() => {
                Modal.confirm({
                  title: '吊销该 API Key？',
                  content: `吊销后使用「${record.prefix}…」发起的请求将立即被拒绝（401）。`,
                  okText: '吊销',
                  okButtonProps: { danger: true },
                  cancelText: '取消',
                  onOk: async () => {
                    try {
                      await revokeAPIKey(record.id);
                      message.success('API Key 已吊销');
                      actionRef.current?.reload();
                    } catch (error: unknown) {
                      message.error(getErrorMessage(error, '吊销失败'));
                    }
                  },
                });
              }}
            >
              吊销
            </a>
          ) : null}
          <a
            onClick={() => {
              Modal.confirm({
                title: '删除该 API Key 记录？',
                content: '仅清除审计记录；已吊销的密钥本来就无法通过认证。',
                okText: '删除',
                okButtonProps: { danger: true },
                cancelText: '取消',
                onOk: async () => {
                  try {
                    await deleteAPIKey(record.id);
                    message.success('记录已删除');
                    actionRef.current?.reload();
                  } catch (error: unknown) {
                    message.error(getErrorMessage(error, '删除失败'));
                  }
                },
              });
            }}
          >
            删除
          </a>
        </Space>
      ),
    },
  ];

  return (
    <>
      <ProTable<API.APIKey>
        headerTitle="API Keys"
        rowKey="id"
        actionRef={actionRef}
        search={false}
        columns={columns}
        toolBarRender={() => [
          <Button
            key="create"
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => {
              form.resetFields();
              setModalOpen(true);
            }}
          >
            签发密钥
          </Button>,
        ]}
        request={async () => {
          try {
            const data = await listAPIKeys();
            return { data, total: data.length, success: true };
          } catch (error: unknown) {
            console.error('获取 API Keys 失败:', error);
            return { data: [], total: 0, success: true };
          }
        }}
        pagination={false}
      />

      <Modal
        title="签发 API Key"
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={handleCreate}
        confirmLoading={submitting}
        okText="签发"
      >
        <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="例如：监控系统集成" />
          </Form.Item>
          <Form.Item name="workspace_id" label="工作区 ID（可选，留空为全局）">
            <Input placeholder="workspace uuid" />
          </Form.Item>
          <Form.Item
            name="scopes"
            label="权限范围（可选，逗号分隔；留空为默认只读 tickets.read + conversations.read）"
          >
            <Input placeholder="tickets.read,conversations.read" />
          </Form.Item>
          <Form.Item name="expires_at" label="过期时间（可选）">
            <DatePicker showTime style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
};

const items = [
  { key: 'webhooks', label: 'Webhook 订阅', children: <WebhooksTab /> },
  { key: 'deliveries', label: '投递日志', children: <DeliveriesTab /> },
  { key: 'api-keys', label: 'API Keys', children: <APIKeysTab /> },
];

const OpenPlatformPage: React.FC = () => (
  <PageContainer title="开放平台" subTitle="对外集成：出站 Webhook 订阅与 API Key 管理">
    <Tabs defaultActiveKey="webhooks" items={items} />
    <Alert
      type="info"
      showIcon
      message="Email 渠道说明"
      description="Email 渠道无需在页面配置：在服务端 config.yml 的 email 段填入 IMAP/SMTP 信息并启用即可，客户来信会自动归并为会话。"
      style={{ marginTop: 16 }}
    />
  </PageContainer>
);

export default OpenPlatformPage;
