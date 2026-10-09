import React, { useState, useRef } from 'react';
import { ProTable } from '@ant-design/pro-components';
import type { ProColumns, ActionType } from '@ant-design/pro-components';
import { Tag, Button, Space, Progress, Modal, Form, Input, InputNumber, message } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { navigateTo } from '@/lib/navigation';
import { AGENT_STATUS_MAP } from '@/utils/constants';
import { listAgents, createAgent } from '@/services/agent';
import { getErrorMessage, isFormValidationError } from '@/utils/error';

const AgentListPage: React.FC = () => {
  const actionRef = useRef<ActionType>();
  const [createOpen, setCreateOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [form] = Form.useForm();

  const handleCreate = async () => {
    try {
      const values = await form.validateFields();
      setCreating(true);
      const skills = values.skills
        ? values.skills.split(',').map((s: string) => s.trim()).filter(Boolean)
        : [];
      await createAgent({
        name: values.name,
        email: values.email,
        skills,
        max_concurrent: values.max_concurrent || 5,
      });
      message.success('客服创建成功');
      setCreateOpen(false);
      form.resetFields();
      actionRef.current?.reload();
    } catch (error: unknown) {
      if (isFormValidationError(error)) return;
      message.error(getErrorMessage(error, '创建失败'));
    } finally {
      setCreating(false);
    }
  };

  const columns: ProColumns<API.Agent>[] = [
    {
      title: '姓名',
      dataIndex: 'name',
      search: true,
    },
    {
      title: '邮箱',
      dataIndex: 'email',
      copyable: true,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      valueType: 'select',
      valueEnum: Object.fromEntries(
        Object.entries(AGENT_STATUS_MAP).map(([k, v]) => [k, { text: v.text }]),
      ),
      render: (_, record) => {
        const item = AGENT_STATUS_MAP[record.status];
        return item ? <Tag color={item.color}>{item.text}</Tag> : record.status;
      },
    },
    {
      title: '技能',
      dataIndex: 'skills',
      search: false,
      render: (_, record) =>
        record.skills?.map((skill) => <Tag key={skill}>{skill}</Tag>),
    },
    {
      title: '会话数',
      dataIndex: 'current_sessions',
      width: 160,
      search: false,
      render: (_, record) => {
        const percent = record.max_sessions
          ? Math.round(((record.current_sessions || 0) / record.max_sessions) * 100)
          : 0;
        return (
          <Space>
            <span>
              {record.current_sessions || 0}/{record.max_sessions}
            </span>
            <Progress percent={percent} size="small" style={{ width: 60 }} />
          </Space>
        );
      },
    },
    {
      title: '操作',
      valueType: 'option',
      width: 120,
      render: (_, record) => (
        <Space>
          <a onClick={() => navigateTo(`/agent/detail/${record.id}`)}>查看</a>
          <a onClick={() => navigateTo(`/agent/detail/${record.id}`)}>编辑</a>
        </Space>
      ),
    },
  ];

  return (
    <>
      <ProTable<API.Agent>
        headerTitle="客服列表"
        rowKey="id"
        actionRef={actionRef}
        columns={columns}
        toolBarRender={() => [
          <Button key="create" type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
            添加客服
          </Button>,
        ]}
        request={async (params) => {
          try {
            const result = await listAgents({
              page: params.current,
              page_size: params.pageSize,
              status: params.status,
              search: params.name,
            });
            return {
              data: result.data,
              total: result.total,
              success: true,
            };
          } catch (error) {
            console.error('获取客服列表失败:', error);
            return { data: [], total: 0, success: true };
          }
        }}
        pagination={{ defaultPageSize: 20 }}
      />

      <Modal
        title="添加客服"
        open={createOpen}
        onCancel={() => { setCreateOpen(false); form.resetFields(); }}
        onOk={handleCreate}
        confirmLoading={creating}
        okText="创建"
      >
        <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item name="name" label="姓名" rules={[{ required: true, message: '请输入姓名' }]}>
            <Input placeholder="请输入客服姓名" />
          </Form.Item>
          <Form.Item name="email" label="邮箱" rules={[
            { required: true, message: '请输入邮箱' },
            { type: 'email', message: '请输入有效邮箱' },
          ]}>
            <Input placeholder="请输入邮箱" />
          </Form.Item>
          <Form.Item name="skills" label="技能标签（逗号分隔）">
            <Input placeholder="如: 售前, 售后, 技术" />
          </Form.Item>
          <Form.Item name="max_concurrent" label="最大并发会话数" initialValue={5}>
            <InputNumber min={1} max={50} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
};

export default AgentListPage;
