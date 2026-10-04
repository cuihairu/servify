import React, { useRef, useState } from 'react';
import { ProTable, ProCard } from '@ant-design/pro-components';
import type { ProColumns, ActionType } from '@ant-design/pro-components';
import { Tag, Button, Space, message, Modal, Form, Input, Select } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { goBack } from '@/lib/navigation';
import { listSources, createSource, deleteSource, SOURCE_TYPES } from '@/services/knowledge';
import { getErrorMessage, isFormValidationError } from '@/utils/error';

// 来源登记管理（V1.0 收敛 B3-2，docs/v1-convergence-plan.md §8.1）：
// knowledge_sources 元数据 CRUD；删除受引用守卫（仍被文档引用时后端拒绝）。

const SOURCE_TYPE_LABEL: Record<string, string> = {
  markdown: 'Markdown',
  website: '网站',
  pdf: 'PDF',
  faq: 'FAQ',
  api: 'API',
};

const SourcesPage: React.FC = () => {
  const actionRef = useRef<ActionType>();
  const [modalOpen, setModalOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [form] = Form.useForm();

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setSubmitting(true);
      await createSource({
        name: values.name,
        type: values.type,
        description: values.description,
      });
      message.success('来源已登记');
      setModalOpen(false);
      form.resetFields();
      actionRef.current?.reload();
    } catch (error: unknown) {
      if (isFormValidationError(error)) return;
      message.error(getErrorMessage(error, '登记失败'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = (record: API.KnowledgeSource) => {
    Modal.confirm({
      title: '确认删除来源',
      content: `删除「${record.name}」后不可恢复；仍被文档引用时将被拒绝。`,
      okText: '确认删除',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await deleteSource(record.id);
          message.success('来源已删除');
          actionRef.current?.reload();
        } catch (error: unknown) {
          message.error(getErrorMessage(error, '删除失败'));
        }
      },
    });
  };

  const columns: ProColumns<API.KnowledgeSource>[] = [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 80,
    },
    {
      title: '名称',
      dataIndex: 'name',
      search: true,
      ellipsis: true,
    },
    {
      title: '类型',
      dataIndex: 'type',
      width: 120,
      valueType: 'select',
      valueEnum: Object.fromEntries(SOURCE_TYPES.map((t) => [t, { text: SOURCE_TYPE_LABEL[t] || t }])),
      render: (_, record) => <Tag color="geekblue">{SOURCE_TYPE_LABEL[record.type] || record.type}</Tag>,
    },
    {
      title: '描述',
      dataIndex: 'description',
      search: false,
      ellipsis: true,
    },
    {
      title: '登记时间',
      dataIndex: 'created_at',
      valueType: 'dateTime',
      width: 180,
      search: false,
    },
    {
      title: '操作',
      valueType: 'option',
      width: 100,
      render: (_, record) => (
        <a onClick={() => handleDelete(record)} style={{ color: '#ff4d4f' }}>删除</a>
      ),
    },
  ];

  return (
    <ProCard
      title="知识来源"
      extra={
        <Space>
          <Button onClick={goBack}>返回</Button>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => { form.resetFields(); setModalOpen(true); }}
          >
            登记来源
          </Button>
        </Space>
      }
    >
      <ProTable<API.KnowledgeSource>
        rowKey="id"
        actionRef={actionRef}
        columns={columns}
        search={false}
        request={async (params) => {
          try {
            const rows = await listSources(params.type);
            return { data: rows || [], total: rows?.length || 0, success: true };
          } catch (error) {
            console.error('获取来源列表失败:', error);
            return { data: [], total: 0, success: false };
          }
        }}
        pagination={{ defaultPageSize: 20 }}
      />

      <Modal
        title="登记知识来源"
        open={modalOpen}
        onCancel={() => { setModalOpen(false); form.resetFields(); }}
        onOk={handleSubmit}
        confirmLoading={submitting}
        okText="登记"
      >
        <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入来源名称' }]}>
            <Input placeholder="如: 官方帮助中心" />
          </Form.Item>
          <Form.Item name="type" label="类型" rules={[{ required: true, message: '请选择来源类型' }]}>
            <Select
              placeholder="选择类型"
              options={SOURCE_TYPES.map((t) => ({ label: SOURCE_TYPE_LABEL[t] || t, value: t }))}
            />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} placeholder="来源说明（可选）" />
          </Form.Item>
        </Form>
      </Modal>
    </ProCard>
  );
};

export default SourcesPage;
