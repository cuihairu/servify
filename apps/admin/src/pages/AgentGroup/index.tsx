import React, { useRef, useState } from 'react';
import { ActionType, ProColumns, ProTable } from '@ant-design/pro-components';
import {
  Button,
  Drawer,
  Form,
  Input,
  InputNumber,
  message,
  Popconfirm,
  Select,
  Space,
  Switch,
  Tag,
  Transfer,
  Typography,
} from 'antd';
import {
  createAgentGroup,
  deleteAgentGroup,
  listAgentGroups,
  listGroupMembers,
  replaceGroupMembers,
  updateAgentGroup,
  AgentGroupParams,
} from '@/services/agentGroup';
import { listAgents } from '@/services/agent';

const OVERFLOW_META: Record<string, { color: string; text: string }> = {
  global: { color: 'processing', text: '溢出→全局池' },
  none: { color: 'warning', text: '组满即失败' },
};

/** 可选坐席（Transfer 数据源；rowKey 用 users.id——组成员与亲和路由统一语义） */
interface AgentOption {
  key: string;
  user_id: number;
  name: string;
  email: string;
  status: string;
}

export default function AgentGroupPage() {
  const actionRef = useRef<ActionType>();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editing, setEditing] = useState<API.AgentGroup | null>(null);
  const [saving, setSaving] = useState(false);
  const [agentOptions, setAgentOptions] = useState<AgentOption[]>([]);
  const [memberIds, setMemberIds] = useState<string[]>([]);
  const [form] = Form.useForm();

  const loadAgentOptions = async () => {
    if (agentOptions.length > 0) return;
    try {
      const res = await listAgents({ page: 1, page_size: 200 });
      const options: AgentOption[] = (res.data || []).map((a) => ({
        key: String(a.user_id ?? a.id),
        user_id: a.user_id ?? a.id,
        name: a.name,
        email: a.email,
        status: a.status,
      }));
      setAgentOptions(options);
    } catch (e) {
      message.error('加载坐席列表失败');
    }
  };

  const openCreate = () => {
    setEditing(null);
    setMemberIds([]);
    form.resetFields();
    form.setFieldsValue({ priority: 0, overflow_policy: 'global', enabled: true });
    loadAgentOptions();
    setDrawerOpen(true);
  };

  const openEdit = async (group: API.AgentGroup) => {
    setEditing(group);
    form.resetFields();
    form.setFieldsValue({
      name: group.name,
      description: group.description,
      priority: group.priority,
      overflow_policy: group.overflow_policy,
      enabled: group.enabled,
    });
    loadAgentOptions();
    try {
      const res = await listGroupMembers(group.id);
      setMemberIds(res.member_ids.map(String));
    } catch (e) {
      setMemberIds([]);
    }
    setDrawerOpen(true);
  };

  const handleSave = async () => {
    const values = await form.validateFields();
    setSaving(true);
    try {
      const body: AgentGroupParams = {
        name: values.name,
        description: values.description || '',
        priority: values.priority ?? 0,
        overflow_policy: values.overflow_policy,
        enabled: values.enabled,
      };
      let groupId: number;
      if (editing) {
        await updateAgentGroup(editing.id, body);
        groupId = editing.id;
      } else {
        const created = await createAgentGroup(body);
        groupId = created.id;
      }
      await replaceGroupMembers(groupId, memberIds.map(Number));
      message.success(editing ? '坐席组已更新' : '坐席组已创建');
      setDrawerOpen(false);
      actionRef.current?.reload();
    } catch (e: any) {
      const detail = e?.error?.message || e?.message;
      if (detail) message.error(detail);
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (group: API.AgentGroup) => {
    try {
      await deleteAgentGroup(group.id);
      message.success('坐席组已删除');
      actionRef.current?.reload();
    } catch (e) {
      message.error('删除失败');
    }
  };

  const columns: ProColumns<API.AgentGroup>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      copyable: true,
      render: (_, entity) => (
        <Space>
          <Typography.Text strong>{entity.name}</Typography.Text>
          {!entity.enabled && <Tag color="default">已停用</Tag>}
        </Space>
      ),
    },
    { title: '描述', dataIndex: 'description', ellipsis: true, search: false },
    { title: '优先级', dataIndex: 'priority', width: 80, align: 'center', search: false },
    {
      title: '溢出策略',
      dataIndex: 'overflow_policy',
      width: 130,
      valueType: 'select',
      fieldProps: {
        options: [
          { label: '溢出→全局池', value: 'global' },
          { label: '组满即失败', value: 'none' },
        ],
      },
      render: (_, entity) => {
        const meta = OVERFLOW_META[entity.overflow_policy] || {
          color: 'default',
          text: entity.overflow_policy,
        };
        return <Tag color={meta.color}>{meta.text}</Tag>;
      },
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      width: 80,
      align: 'center',
      valueType: 'select',
      fieldProps: {
        options: [
          { label: '启用', value: 'true' },
          { label: '停用', value: 'false' },
        ],
      },
      render: (_, entity) =>
        entity.enabled ? <Tag color="success">启用</Tag> : <Tag color="default">停用</Tag>,
    },
    { title: '创建时间', dataIndex: 'created_at', width: 160, search: false, valueType: 'dateTime' },
    {
      title: '操作',
      valueType: 'option',
      width: 160,
      render: (_, entity) => [
        <a key="edit" onClick={() => openEdit(entity)}>
          编辑
        </a>,
        <Popconfirm
          key="delete"
          title="确认删除该坐席组？"
          description="软删除：组不再参与分配，排队中的目标组将走全局池兜底。"
          okText="删除"
          cancelText="取消"
          okButtonProps={{ danger: true }}
          onConfirm={() => handleDelete(entity)}
        >
          <a style={{ color: '#cf1322' }}>删除</a>
        </Popconfirm>,
      ],
    },
  ];

  return (
    <>
      <ProTable<API.AgentGroup>
        headerTitle="坐席组"
        rowKey="id"
        actionRef={actionRef}
        columns={columns}
        search={{ labelWidth: 'auto' }}
        pagination={{ pageSize: 20 }}
        request={async (params) => {
          const res = await listAgentGroups();
          let groups = res.groups || [];
          const keyword = params.name?.trim();
          if (keyword) groups = groups.filter((g) => g.name.includes(keyword));
          if (params.enabled === 'true') groups = groups.filter((g) => g.enabled);
          if (params.enabled === 'false') groups = groups.filter((g) => !g.enabled);
          if (params.overflow_policy) {
            groups = groups.filter((g) => g.overflow_policy === params.overflow_policy);
          }
          return { data: groups, total: groups.length, success: true };
        }}
        toolBarRender={() => [
          <Button key="create" type="primary" onClick={openCreate}>
            新建坐席组
          </Button>,
        ]}
      />
      <Drawer
        title={editing ? `编辑坐席组：${editing.name}` : '新建坐席组'}
        width={680}
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        destroyOnClose
        extra={
          <Space>
            <Button onClick={() => setDrawerOpen(false)}>取消</Button>
            <Button type="primary" loading={saving} onClick={handleSave}>
              保存
            </Button>
          </Space>
        }
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="组名" rules={[{ required: true, message: '请输入组名' }]}>
            <Input placeholder="如：售前组" maxLength={64} />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} placeholder="组的用途说明（可选）" maxLength={256} />
          </Form.Item>
          <Space size="large" wrap>
            <Form.Item name="priority" label="优先级" tooltip="数值越大越优先被选为指定目标">
              <InputNumber min={0} max={9999} style={{ width: 140 }} />
            </Form.Item>
            <Form.Item
              name="overflow_policy"
              label="溢出策略"
              tooltip="组内无人可派时：global 落全局池兜底；none 直接报组不可用"
            >
              <Select
                style={{ width: 180 }}
                options={[
                  { label: '溢出→全局池', value: 'global' },
                  { label: '组满即失败', value: 'none' },
                ]}
              />
            </Form.Item>
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Space>
          <Form.Item label="组成员" tooltip="全量保存：右侧列表即最终成员（坐席用户 ID）">
            <Transfer
              dataSource={agentOptions}
              targetKeys={memberIds}
              onChange={(keys) => setMemberIds(keys as string[])}
              render={(item) => `${item.name}（${item.email}）`}
              showSearch
              listStyle={{ width: 290, height: 320 }}
              locale={{ itemUnit: '项', itemsUnit: '项', searchPlaceholder: '搜索坐席' }}
            />
          </Form.Item>
        </Form>
      </Drawer>
    </>
  );
}
