import React, { useEffect, useRef, useState } from 'react';
import { ProTable } from '@ant-design/pro-components';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { PlusOutlined } from '@ant-design/icons';
import { Button, DatePicker, Form, Modal, Select, Space, Tag, message } from 'antd';
import dayjs from 'dayjs';
import { listAgents } from '@/services/agent';
import { createShift, deleteShift, listShifts, updateShift } from '@/services/shift';
import { getErrorMessage, isFormValidationError } from '@/utils/error';

const SHIFT_TYPE_OPTIONS = [
  { label: '早班', value: 'morning' },
  { label: '午班', value: 'afternoon' },
  { label: '晚班', value: 'evening' },
  { label: '夜班', value: 'night' },
];

const SHIFT_STATUS_OPTIONS = [
  { label: '已排班', value: 'scheduled' },
  { label: '进行中', value: 'active' },
  { label: '已完成', value: 'completed' },
  { label: '已取消', value: 'cancelled' },
];

const ShiftPage: React.FC = () => {
  const actionRef = useRef<ActionType>();
  const [form] = Form.useForm();
  const [modalOpen, setModalOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [editingShift, setEditingShift] = useState<API.Shift | null>(null);
  const [agentOptions, setAgentOptions] = useState<Array<{ label: string; value: number }>>([]);

  useEffect(() => {
    const fetchAgents = async () => {
      try {
        const result = await listAgents({ page: 1, page_size: 200 });
        setAgentOptions(
          result.data.map((agent) => ({
            label: agent.name || agent.email || `#${agent.id}`,
            value: agent.id,
          })),
        );
      } catch (error) {
        console.error('获取客服列表失败:', error);
      }
    };

    void fetchAgents();
  }, []);

  const closeModal = () => {
    setModalOpen(false);
    setEditingShift(null);
    form.resetFields();
  };

  const openCreate = () => {
    setEditingShift(null);
    form.resetFields();
    form.setFieldsValue({
      shift_type: 'morning',
      status: 'scheduled',
    });
    setModalOpen(true);
  };

  const openEdit = (record: API.Shift) => {
    setEditingShift(record);
    form.setFieldsValue({
      agent_id: record.agent_id,
      shift_type: record.shift_type,
      status: record.status,
      start_time: record.start_time ? dayjs(record.start_time) : undefined,
      end_time: record.end_time ? dayjs(record.end_time) : undefined,
    });
    setModalOpen(true);
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      if (!values.start_time || !values.end_time) {
        message.error('请选择完整班次时间');
        return;
      }

      setSubmitting(true);
      const payload = {
        shift_type: values.shift_type,
        status: values.status,
        start_time: values.start_time.toISOString(),
        end_time: values.end_time.toISOString(),
      };

      if (editingShift) {
        await updateShift(editingShift.id, payload);
        message.success('班次已更新');
      } else {
        await createShift({
          ...payload,
          agent_id: values.agent_id,
        });
        message.success('班次已创建');
      }

      closeModal();
      actionRef.current?.reload();
    } catch (error: unknown) {
      if (isFormValidationError(error)) {
        return;
      }
      message.error(getErrorMessage(error, editingShift ? '更新失败' : '创建失败'));
    } finally {
      setSubmitting(false);
    }
  };

  const columns: ProColumns<API.Shift>[] = [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 80,
    },
    {
      title: '客服',
      dataIndex: 'agent_name',
      width: 160,
      search: false,
      render: (_, record) =>
        record.agent?.name || record.agent?.username || record.agent_name || `#${record.agent_id}`,
    },
    {
      title: '班次类型',
      dataIndex: 'shift_type',
      width: 120,
      valueType: 'select',
      valueEnum: Object.fromEntries(SHIFT_TYPE_OPTIONS.map((item) => [item.value, { text: item.label }])),
    },
    {
      title: '开始时间',
      dataIndex: 'start_time',
      width: 180,
      valueType: 'dateTime',
      search: false,
    },
    {
      title: '结束时间',
      dataIndex: 'end_time',
      width: 180,
      valueType: 'dateTime',
      search: false,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      valueType: 'select',
      valueEnum: Object.fromEntries(SHIFT_STATUS_OPTIONS.map((item) => [item.value, { text: item.label }])),
      render: (_, record) => (
        <Tag color={record.status === 'active' ? 'green' : 'default'}>
          {SHIFT_STATUS_OPTIONS.find((item) => item.value === record.status)?.label || record.status}
        </Tag>
      ),
    },
    {
      title: '操作',
      valueType: 'option',
      width: 140,
      render: (_, record) => (
        <Space>
          <a onClick={() => openEdit(record)}>编辑</a>
          <a
            onClick={async () => {
              try {
                await deleteShift(record.id);
                message.success('班次已删除');
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
      <ProTable<API.Shift>
        headerTitle="班次管理"
        rowKey="id"
        actionRef={actionRef}
        columns={columns}
        toolBarRender={() => [
          <Button key="create" type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新建班次
          </Button>,
        ]}
        request={async (params) => {
          try {
            const result = await listShifts({
              page: params.current,
              page_size: params.pageSize,
              shift_type: typeof params.shift_type === 'string' ? [params.shift_type] : undefined,
              status: typeof params.status === 'string' ? [params.status] : undefined,
            });
            return {
              data: result.data,
              total: result.total,
              success: true,
            };
          } catch (error: unknown) {
            console.error('获取班次列表失败:', error);
            return { data: [], total: 0, success: true };
          }
        }}
        search={{ filterType: 'light' }}
        pagination={{ defaultPageSize: 20 }}
      />

      <Modal
        title={editingShift ? '编辑班次' : '新建班次'}
        open={modalOpen}
        onCancel={closeModal}
        onOk={handleSubmit}
        confirmLoading={submitting}
        okText={editingShift ? '保存' : '创建'}
        width={640}
      >
        <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item
            name="agent_id"
            label="客服"
            rules={[{ required: true, message: '请选择客服' }]}
          >
            <Select
              disabled={Boolean(editingShift)}
              options={agentOptions}
              placeholder="选择客服"
              showSearch
              optionFilterProp="label"
            />
          </Form.Item>
          <Form.Item
            name="shift_type"
            label="班次类型"
            rules={[{ required: true, message: '请选择班次类型' }]}
          >
            <Select options={SHIFT_TYPE_OPTIONS} />
          </Form.Item>
          <Form.Item
            name="status"
            label="状态"
            rules={[{ required: true, message: '请选择状态' }]}
          >
            <Select options={SHIFT_STATUS_OPTIONS} />
          </Form.Item>
          <Form.Item
            name="start_time"
            label="开始时间"
            rules={[{ required: true, message: '请选择开始时间' }]}
          >
            <DatePicker showTime style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            name="end_time"
            label="结束时间"
            rules={[{ required: true, message: '请选择结束时间' }]}
          >
            <DatePicker showTime style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
};

export default ShiftPage;
