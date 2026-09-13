import React, { useRef, useState } from 'react';
import { ProTable } from '@ant-design/pro-components';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { PlusOutlined } from '@ant-design/icons';
import {
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  message,
} from 'antd';
import {
  createAutomation,
  deleteAutomation,
  dryRunAutomation,
  listAutomations,
  runAutomation,
} from '@/services/automation';
import { getErrorMessage, isFormValidationError } from '@/utils/error';

const EVENT_OPTIONS = [
  { value: 'ticket_created', label: '工单创建' },
  { value: 'ticket_updated', label: '工单更新' },
  { value: 'ticket_closed', label: '工单关闭' },
  { value: 'ticket_assigned', label: '工单分配' },
  { value: 'sla_violation', label: 'SLA 违约' },
  { value: 'conversation.created', label: '会话创建' },
  { value: 'conversation.message_received', label: '收到会话消息' },
  { value: 'routing.agent_assigned', label: '路由分配坐席' },
  { value: 'routing.transfer_completed', label: '路由转接完成' },
];

const CONDITION_FIELD_OPTIONS = [
  { value: 'ticket.priority', label: '工单优先级' },
  { value: 'ticket.status', label: '工单状态' },
  { value: 'ticket.tags', label: '工单标签' },
  { value: 'violation.type', label: 'SLA 违约类型' },
];

const CONDITION_OP_OPTIONS = [
  { value: 'eq', label: '等于' },
  { value: 'neq', label: '不等于' },
  { value: 'contains', label: '包含' },
];

const ACTION_OPTIONS = [
  { value: 'set_priority', label: '设置优先级' },
  { value: 'escalate_priority', label: '升级优先级' },
  { value: 'add_tag', label: '添加标签' },
  { value: 'remove_tag', label: '移除标签' },
  { value: 'add_comment', label: '添加评论' },
  { value: 'call_webhook', label: '调用 Webhook' },
  { value: 'delay', label: '延迟执行' },
  { value: 'notify_log', label: '仅记录日志' },
];

const PRIORITY_OPTIONS = ['low', 'normal', 'high', 'urgent'].map((p) => ({ value: p, label: p }));

type ActionFormValue = { type?: string; params?: Record<string, any> };

function prettyJson(value?: string | Record<string, unknown>) {
  if (!value) {
    return '[]';
  }
  if (typeof value === 'string') {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
  return JSON.stringify(value, null, 2);
}

// 把结构化表单值组装成后端 TriggerAction；非法参数直接抛错由调用方提示
function buildActions(rawActions: ActionFormValue[] | undefined) {
  return (rawActions || []).map((action) => {
    const base: { type: string; params: Record<string, any> } = { type: action.type || '', params: {} };
    const p = action.params || {};
    switch (action.type) {
      case 'set_priority':
        base.params = { priority: p.priority };
        break;
      case 'escalate_priority':
        base.params = { wrap: Boolean(p.wrap) };
        break;
      case 'add_tag':
      case 'remove_tag':
        base.params = { tag: (p.tag || '').trim() };
        if (!base.params.tag) {
          throw new Error('标签不能为空');
        }
        break;
      case 'add_comment':
        base.params = { content: p.content };
        if (!base.params.content) {
          throw new Error('评论内容不能为空');
        }
        break;
      case 'call_webhook':
        base.params = { url: (p.url || '').trim() };
        if (!base.params.url) {
          throw new Error('Webhook URL 不能为空');
        }
        if (p.secret) {
          base.params.secret = p.secret;
        }
        if (p.payload) {
          base.params.payload = JSON.parse(p.payload);
        }
        break;
      case 'delay': {
        const nested = p.actions ? JSON.parse(p.actions) : [];
        if (!Array.isArray(nested)) {
          throw new Error('延迟动作必须是 JSON 数组');
        }
        base.params = { minutes: p.minutes, actions: nested };
        break;
      }
      default:
        break;
    }
    return base;
  });
}

const AutomationPage: React.FC = () => {
  const actionRef = useRef<ActionType>();
  const [form] = Form.useForm();
  const [modalOpen, setModalOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [runTarget, setRunTarget] = useState<API.Automation | null>(null);
  const [runTicketIds, setRunTicketIds] = useState('');
  const [runDry, setRunDry] = useState(false);
  const [running, setRunning] = useState(false);

  const openView = (record: API.Automation) => {
    Modal.info({
      title: record.name,
      width: 760,
      content: (
        <div style={{ marginTop: 16, display: 'grid', gap: 12 }}>
          <div>事件：{record.event || record.trigger_type || '-'}</div>
          <div>状态：{record.active ?? record.enabled ? '启用' : '停用'}</div>
          <div>
            条件：
            <pre style={{ whiteSpace: 'pre-wrap', marginTop: 8 }}>{prettyJson(record.conditions)}</pre>
          </div>
          <div>
            动作：
            <pre style={{ whiteSpace: 'pre-wrap', marginTop: 8 }}>{prettyJson(record.actions)}</pre>
          </div>
        </div>
      ),
    });
  };

  const handleCreate = async () => {
    try {
      const values = await form.validateFields();
      const actions = buildActions(values.actions);
      const conditions = (values.conditions || []).map((c: any) => ({
        field: c.field,
        op: c.op,
        value: c.value,
      }));
      setSubmitting(true);
      await createAutomation({
        name: values.name?.trim(),
        event: values.event,
        conditions,
        actions,
        active: values.active ?? true,
      });
      message.success('规则已创建');
      setModalOpen(false);
      form.resetFields();
      actionRef.current?.reload();
    } catch (error: unknown) {
      if (isFormValidationError(error)) {
        return;
      }
      if (error instanceof SyntaxError) {
        message.error('Webhook 载荷或延迟动作不是合法 JSON');
        return;
      }
      message.error(getErrorMessage(error, '创建失败'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleRun = async () => {
    if (!runTarget) {
      return;
    }
    const ids = runTicketIds
      .split(/[,，\s]+/)
      .map((s) => Number.parseInt(s, 10))
      .filter((n) => Number.isInteger(n) && n > 0);
    if (ids.length === 0) {
      message.error('请输入要对其运行的工单 ID（逗号分隔）');
      return;
    }
    try {
      setRunning(true);
      const resp = runDry
        ? await dryRunAutomation(runTarget.id, ids)
        : await runAutomation(runTarget.id, ids);
      message.success(
        runDry
          ? `试运行完成：处理 ${resp.tickets_processed} 张工单，命中 ${resp.matches} 次（未执行动作）`
          : `执行完成：处理 ${resp.tickets_processed} 张工单，命中 ${resp.matches} 次`,
      );
      setRunTarget(null);
      setRunTicketIds('');
      setRunDry(false);
      actionRef.current?.reload();
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '执行失败'));
    } finally {
      setRunning(false);
    }
  };

  const renderActionParams = (action: ActionFormValue | undefined, field: any) => {
    switch (action?.type) {
      case 'set_priority':
        return (
          <Form.Item
            name={[field.name, 'params', 'priority']}
            label="目标优先级"
            rules={[{ required: true, message: '请选择优先级' }]}
          >
            <Select options={PRIORITY_OPTIONS} placeholder="low / normal / high / urgent" />
          </Form.Item>
        );
      case 'escalate_priority':
        return (
          <Form.Item
            name={[field.name, 'params', 'wrap']}
            label="已是最高级时回绕到 low"
            valuePropName="checked"
          >
            <Switch checkedChildren="回绕" unCheckedChildren="保持" />
          </Form.Item>
        );
      case 'add_tag':
      case 'remove_tag':
        return (
          <Form.Item
            name={[field.name, 'params', 'tag']}
            label="标签"
            rules={[{ required: true, message: '请输入标签' }]}
          >
            <Input placeholder="例如：vip" />
          </Form.Item>
        );
      case 'add_comment':
        return (
          <Form.Item
            name={[field.name, 'params', 'content']}
            label="评论内容"
            rules={[{ required: true, message: '请输入评论内容' }]}
          >
            <Input.TextArea rows={3} placeholder="追加到工单的系统评论" />
          </Form.Item>
        );
      case 'call_webhook':
        return (
          <>
            <Form.Item
              name={[field.name, 'params', 'url']}
              label="Webhook URL"
              rules={[{ required: true, message: '请输入 URL' }]}
            >
              <Input placeholder="https://example.com/hook" />
            </Form.Item>
            <Form.Item name={[field.name, 'params', 'secret']} label="签名密钥（可选）">
              <Input placeholder="留空则不签名" />
            </Form.Item>
            <Form.Item
              name={[field.name, 'params', 'payload']}
              label="请求体 JSON（可选，留空用工单快照）"
              rules={[
                {
                  validator: (_, value) => {
                    if (!value) {
                      return Promise.resolve();
                    }
                    try {
                      JSON.parse(value);
                      return Promise.resolve();
                    } catch {
                      return Promise.reject(new Error('不是合法 JSON'));
                    }
                  },
                },
              ]}
            >
              <Input.TextArea rows={3} placeholder='{"custom": "payload"}' />
            </Form.Item>
          </>
        );
      case 'delay':
        return (
          <>
            <Form.Item
              name={[field.name, 'params', 'minutes']}
              label="延迟分钟数"
              rules={[{ required: true, message: '请输入延迟分钟数' }]}
            >
              <InputNumber min={1} precision={0} style={{ width: '100%' }} placeholder="例如：30" />
            </Form.Item>
            <Form.Item
              name={[field.name, 'params', 'actions']}
              label="到期后执行的动作 JSON"
              rules={[
                { required: true, message: '请输入到期后执行的动作' },
                {
                  validator: (_, value) => {
                    if (!value) {
                      return Promise.resolve();
                    }
                    try {
                      const parsed = JSON.parse(value);
                      if (!Array.isArray(parsed)) {
                        return Promise.reject(new Error('必须是 JSON 数组'));
                      }
                      if (JSON.stringify(parsed).includes('"delay"')) {
                        return Promise.reject(new Error('不允许嵌套 delay 动作'));
                      }
                      return Promise.resolve();
                    } catch (error) {
                      if (error instanceof Error && error.message !== 'Unexpected end of JSON input') {
                        return Promise.reject(error);
                      }
                      return Promise.reject(new Error('不是合法 JSON'));
                    }
                  },
                },
              ]}
            >
              <Input.TextArea
                rows={3}
                placeholder='[{"type":"add_comment","params":{"content":"超时提醒"}}]'
              />
            </Form.Item>
          </>
        );
      default:
        return null;
    }
  };

  const columns: ProColumns<API.Automation>[] = [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 80,
    },
    {
      title: '规则名称',
      dataIndex: 'name',
      search: true,
    },
    {
      title: '触发事件',
      dataIndex: 'event',
      width: 180,
      search: false,
    },
    {
      title: '执行动作',
      dataIndex: 'actions',
      width: 240,
      search: false,
      render: (_, record) => {
        if (typeof record.actions === 'string') {
          return record.actions;
        }
        if (record.actions) {
          return JSON.stringify(record.actions);
        }
        return '-';
      },
    },
    {
      title: '状态',
      dataIndex: 'active',
      width: 100,
      search: false,
      render: (_, record) => (
        <Switch
          checked={Boolean(record.active ?? record.enabled)}
          checkedChildren="启用"
          unCheckedChildren="停用"
          disabled
          onChange={() => {}}
        />
      ),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      valueType: 'dateTime',
      width: 180,
      search: false,
    },
    {
      title: '操作',
      valueType: 'option',
      width: 180,
      render: (_, record) => (
        <Space>
          <a onClick={() => openView(record)}>查看</a>
          <a
            onClick={() => {
              setRunTicketIds('');
              setRunDry(false);
              setRunTarget(record);
            }}
          >
            执行
          </a>
          <a
            onClick={async () => {
              try {
                await deleteAutomation(record.id);
                message.success('规则已删除');
                actionRef.current?.reload();
              } catch (error: unknown) {
                message.error(getErrorMessage(error, '删除失败'));
              }
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
      <ProTable<API.Automation>
        headerTitle="自动化规则"
        rowKey="id"
        actionRef={actionRef}
        columns={columns}
        toolBarRender={() => [
          <Button
            key="create"
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => {
              form.resetFields();
              form.setFieldsValue({ active: true, conditions: [], actions: [] });
              setModalOpen(true);
            }}
          >
            新建规则
          </Button>,
        ]}
        request={async (params) => {
          try {
            const result = await listAutomations();
            const keyword = typeof params.name === 'string' ? params.name.trim().toLowerCase() : '';
            let data = result.data;
            if (keyword) {
              data = data.filter((item) => item.name.toLowerCase().includes(keyword));
            }
            const total = data.length;
            const current = params.current || 1;
            const pageSize = params.pageSize || 20;
            return {
              data: data.slice((current - 1) * pageSize, current * pageSize),
              total,
              success: true,
            };
          } catch (error: unknown) {
            console.error('获取自动化规则失败:', error);
            return { data: [], total: 0, success: true };
          }
        }}
        pagination={{ defaultPageSize: 20 }}
      />

      <Modal
        title="新建规则"
        open={modalOpen}
        onCancel={() => {
          setModalOpen(false);
          form.resetFields();
        }}
        onOk={handleCreate}
        confirmLoading={submitting}
        okText="创建"
        width={760}
      >
        <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item name="name" label="规则名称" rules={[{ required: true, message: '请输入规则名称' }]}>
            <Input placeholder="例如：高优先级自动加急" />
          </Form.Item>
          <Form.Item name="event" label="触发事件" rules={[{ required: true, message: '请选择触发事件' }]}>
            <Select options={EVENT_OPTIONS} placeholder="选择触发事件" />
          </Form.Item>

          <Form.Item label="条件（全部满足才触发）">
            <Form.List name="conditions">
              {(fields, { add, remove }) => (
                <div style={{ display: 'grid', gap: 8 }}>
                  {fields.map((field) => (
                    <Space key={field.key} align="baseline" wrap>
                      <Form.Item
                        name={[field.name, 'field']}
                        noStyle
                        rules={[{ required: true, message: '选择字段' }]}
                      >
                        <Select options={CONDITION_FIELD_OPTIONS} placeholder="字段" style={{ width: 160 }} />
                      </Form.Item>
                      <Form.Item
                        name={[field.name, 'op']}
                        noStyle
                        rules={[{ required: true, message: '选择操作' }]}
                      >
                        <Select options={CONDITION_OP_OPTIONS} placeholder="操作" style={{ width: 100 }} />
                      </Form.Item>
                      <Form.Item
                        name={[field.name, 'value']}
                        noStyle
                        rules={[{ required: true, message: '输入值' }]}
                      >
                        <Input placeholder="值" style={{ width: 180 }} />
                      </Form.Item>
                      <a onClick={() => remove(field.name)}>移除</a>
                    </Space>
                  ))}
                  <Button type="dashed" block onClick={() => add()} icon={<PlusOutlined />}>
                    添加条件
                  </Button>
                </div>
              )}
            </Form.List>
          </Form.Item>

          <Form.Item label="动作（自上而下执行；delay 必须是最后一个动作）">
            <Form.List name="actions">
              {(fields, { add, remove }) => (
                <div style={{ display: 'grid', gap: 12 }}>
                  {fields.map((field) => (
                    <div
                      key={field.key}
                      style={{ border: '1px solid #f0f0f0', borderRadius: 6, padding: '12px 12px 0' }}
                    >
                      <Space align="baseline" wrap>
                        <span>动作 {field.name + 1}</span>
                        <Form.Item
                          name={[field.name, 'type']}
                          noStyle
                          rules={[{ required: true, message: '选择动作类型' }]}
                        >
                          <Select options={ACTION_OPTIONS} placeholder="动作类型" style={{ width: 180 }} />
                        </Form.Item>
                        <a onClick={() => remove(field.name)}>移除</a>
                      </Space>
                      <Form.Item noStyle shouldUpdate={(prev, cur) => prev !== cur}>
                        {() => {
                          const actions = form.getFieldValue('actions') as ActionFormValue[] | undefined;
                          return renderActionParams(actions?.[field.name], field);
                        }}
                      </Form.Item>
                    </div>
                  ))}
                  <Button type="dashed" block onClick={() => add()} icon={<PlusOutlined />}>
                    添加动作
                  </Button>
                </div>
              )}
            </Form.List>
          </Form.Item>

          <Form.Item name="active" label="启用" valuePropName="checked">
            <Switch checkedChildren="启用" unCheckedChildren="停用" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`执行规则：${runTarget?.name ?? ''}`}
        open={Boolean(runTarget)}
        onCancel={() => setRunTarget(null)}
        onOk={handleRun}
        confirmLoading={running}
        okText={runDry ? '试运行' : '执行'}
        width={520}
      >
        <div style={{ display: 'grid', gap: 12, marginTop: 16 }}>
          <div>输入要对其运行的工单 ID（逗号或空格分隔）：</div>
          <Input
            value={runTicketIds}
            onChange={(e) => setRunTicketIds(e.target.value)}
            placeholder="例如：1, 2, 3"
          />
          <Space>
            <Switch checked={runDry} onChange={setRunDry} />
            <span>仅试运行（评估条件命中，不执行动作）</span>
          </Space>
        </div>
      </Modal>
    </>
  );
};

export default AutomationPage;
