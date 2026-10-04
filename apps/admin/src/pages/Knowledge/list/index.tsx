import React, { useEffect, useRef, useState } from 'react';
import { ProTable } from '@ant-design/pro-components';
import type { ProColumns, ActionType } from '@ant-design/pro-components';
import { Tag, Button, Space, message, Modal, Form, Input, Select, Switch, Drawer, Timeline, Typography } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { navigateTo } from '@/lib/navigation';
import {
  listDocs, deleteDoc, createDoc, updateDoc,
  listSources, indexDocument, listIndexJobs, retryIndexJob,
} from '@/services/knowledge';
import { getErrorMessage, isFormValidationError } from '@/utils/error';

function normalizeTags(tags?: string | string[]) {
  if (Array.isArray(tags)) {
    return tags.map((tag) => tag.trim()).filter(Boolean);
  }
  if (typeof tags === 'string') {
    return tags
      .split(',')
      .map((tag) => tag.trim())
      .filter(Boolean);
  }
  return [];
}

const CATEGORIES = ['产品文档', '常见问题', '操作指南', 'API文档', '其他'];

const JOB_STATUS_COLOR: Record<string, string> = {
  done: 'green',
  failed: 'red',
  pending: 'default',
  processing: 'blue',
};

const KnowledgeListPage: React.FC = () => {
  const actionRef = useRef<ActionType>();
  const [modalOpen, setModalOpen] = useState(false);
  const [modalType, setModalType] = useState<'create' | 'edit'>('create');
  const [editingDoc, setEditingDoc] = useState<API.KnowledgeDoc | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [form] = Form.useForm();
  // 来源登记下拉（挂源，V1.0 B3-2）
  const [sources, setSources] = useState<API.KnowledgeSource[]>([]);
  // 索引任务抽屉（V1.0 B3-2）
  const [jobsDoc, setJobsDoc] = useState<API.KnowledgeDoc | null>(null);
  const [jobs, setJobs] = useState<API.KnowledgeIndexJob[]>([]);
  const [jobsLoading, setJobsLoading] = useState(false);
  const [indexingId, setIndexingId] = useState<number | null>(null);

  useEffect(() => {
    listSources()
      .then((rows) => setSources(rows || []))
      .catch(() => setSources([]));
  }, []);

  const openCreate = () => {
    setModalType('create');
    setEditingDoc(null);
    form.resetFields();
    setModalOpen(true);
  };

  const openEdit = (record: API.KnowledgeDoc) => {
    setModalType('edit');
    setEditingDoc(record);
    form.setFieldsValue({
      title: record.title,
      category: record.category,
      content: record.content,
      tags: normalizeTags(record.tags).join(', '),
      is_public: record.is_public ?? false,
      source_id: record.source_id || undefined,
    });
    setModalOpen(true);
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setSubmitting(true);
      const tags = values.tags
        ? values.tags.split(',').map((s: string) => s.trim()).filter(Boolean)
        : [];
      // 未选择来源时透传 0（后端口径：0=未归属来源）
      const sourceId = values.source_id ?? 0;

      if (modalType === 'create') {
        await createDoc({
          title: values.title,
          content: values.content || '',
          category: values.category,
          tags,
          is_public: values.is_public ?? false,
          source_id: sourceId,
        });
        message.success('文档创建成功');
      } else if (editingDoc) {
        await updateDoc(editingDoc.id, {
          title: values.title,
          content: values.content,
          category: values.category,
          tags,
          is_public: values.is_public ?? false,
          source_id: sourceId,
        });
        message.success('文档已更新');
      }

      setModalOpen(false);
      form.resetFields();
      actionRef.current?.reload();
    } catch (error: unknown) {
      if (isFormValidationError(error)) return;
      message.error(getErrorMessage(error, '操作失败'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (id: number) => {
    Modal.confirm({
      title: '确认删除',
      content: '删除后不可恢复，确定要删除此文档吗？',
      okText: '确认删除',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await deleteDoc(id);
          message.success('文档已删除');
          actionRef.current?.reload();
        } catch (error: unknown) {
          message.error(getErrorMessage(error, '删除失败'));
        }
      },
    });
  };

  // 重建索引：排队并同步执行一次（V1.0 B3-2）
  const handleIndex = async (record: API.KnowledgeDoc) => {
    try {
      setIndexingId(record.id);
      const result = await indexDocument(record.id);
      if (result?.Status === 'done') {
        message.success(`索引完成（版本 ${result.DocumentVersion}）`);
      } else {
        message.warning(`索引未完成：${result?.Status}${result?.Error ? `（${result.Error}）` : ''}`);
      }
      actionRef.current?.reload();
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '索引失败'));
    } finally {
      setIndexingId(null);
    }
  };

  const openJobs = async (record: API.KnowledgeDoc) => {
    setJobsDoc(record);
    setJobsLoading(true);
    try {
      setJobs((await listIndexJobs(record.id)) || []);
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '获取索引任务失败'));
      setJobs([]);
    } finally {
      setJobsLoading(false);
    }
  };

  const handleRetryJob = async (jobId: string) => {
    try {
      const result = await retryIndexJob(jobId);
      message.success(`任务 ${result?.JobID} 状态：${result?.Status}（版本 ${result?.DocumentVersion}）`);
      if (jobsDoc) await openJobs(jobsDoc);
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '重试失败'));
    }
  };

  const sourceName = (sourceId?: number) =>
    sources.find((s) => s.id === sourceId)?.name || (sourceId ? `来源 #${sourceId}` : '');

  const columns: ProColumns<API.KnowledgeDoc>[] = [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 80,
    },
    {
      title: '标题',
      dataIndex: 'title',
      search: true,
      ellipsis: true,
    },
    {
      title: '分类',
      dataIndex: 'category',
      width: 120,
      valueType: 'select',
      valueEnum: Object.fromEntries(CATEGORIES.map((c) => [c, { text: c }])),
    },
    {
      title: '来源',
      dataIndex: 'source_id',
      width: 140,
      search: false,
      render: (_, record) =>
        record.source_id ? <Tag color="geekblue">{sourceName(record.source_id)}</Tag> : <Tag>未挂源</Tag>,
    },
    {
      title: '版本',
      dataIndex: 'version',
      width: 80,
      search: false,
      render: (_, record) => <Tag>v{record.version ?? 1}</Tag>,
    },
    {
      title: '公开',
      dataIndex: 'is_public',
      width: 100,
      search: false,
      render: (_, record) => (
        <Tag color={record.is_public ? 'green' : 'default'}>
          {record.is_public ? '公开' : '内部'}
        </Tag>
      ),
    },
    {
      title: '标签',
      dataIndex: 'tags',
      search: false,
      render: (_, record) => normalizeTags(record.tags).map((tag) => <Tag key={tag}>{tag}</Tag>),
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      valueType: 'dateTime',
      width: 180,
      sorter: true,
    },
    {
      title: '操作',
      valueType: 'option',
      width: 260,
      render: (_, record) => (
        <Space>
          <a onClick={() => navigateTo(`/knowledge/detail/${record.id}`)}>查看</a>
          <a onClick={() => openEdit(record)}>编辑</a>
          <a onClick={() => void handleIndex(record)}>
            {indexingId === record.id ? '索引中...' : '重建索引'}
          </a>
          <a onClick={() => void openJobs(record)}>任务</a>
          <a onClick={() => handleDelete(record.id)} style={{ color: '#ff4d4f' }}>删除</a>
        </Space>
      ),
    },
  ];

  return (
    <>
      <ProTable<API.KnowledgeDoc>
        headerTitle="知识库文档"
        rowKey="id"
        actionRef={actionRef}
        columns={columns}
        toolBarRender={() => [
          <Button key="sources" onClick={() => navigateTo('/knowledge/sources')}>
            来源与索引
          </Button>,
          <Button key="analytics" onClick={() => navigateTo('/knowledge/analytics')}>
            检索分析
          </Button>,
          <Button key="create" type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新建文档
          </Button>,
        ]}
        request={async (params) => {
          try {
            const result = await listDocs({
              page: params.current,
              page_size: params.pageSize,
              search: params.title,
              category: params.category,
            });
            return {
              data: result.data,
              total: result.total,
              success: true,
            };
          } catch (error) {
            console.error('获取知识库列表失败:', error);
            return { data: [], total: 0, success: true };
          }
        }}
        pagination={{ defaultPageSize: 20 }}
      />

      <Modal
        title={modalType === 'create' ? '新建文档' : '编辑文档'}
        open={modalOpen}
        onCancel={() => { setModalOpen(false); form.resetFields(); }}
        onOk={handleSubmit}
        confirmLoading={submitting}
        okText={modalType === 'create' ? '创建' : '保存'}
        width={640}
      >
        <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item name="title" label="标题" rules={[{ required: true, message: '请输入标题' }]}>
            <Input placeholder="文档标题" />
          </Form.Item>
          <Form.Item name="category" label="分类">
            <Select placeholder="选择分类" allowClear options={CATEGORIES.map((c) => ({ label: c, value: c }))} />
          </Form.Item>
          <Form.Item name="source_id" label="知识来源">
            <Select
              placeholder="选择来源登记（可选）"
              allowClear
              options={sources.map((s) => ({ label: `${s.name}（${s.type}）`, value: s.id }))}
            />
          </Form.Item>
          <Form.Item name="tags" label="标签（逗号分隔）">
            <Input placeholder="如: 入门, API, 常见问题" />
          </Form.Item>
          <Form.Item
            name="is_public"
            label="公开到公共知识库"
            valuePropName="checked"
            initialValue={false}
          >
            <Switch checkedChildren="公开" unCheckedChildren="内部" />
          </Form.Item>
          <Form.Item name="content" label="内容" rules={[{ required: true, message: '请输入内容' }]}>
            <Input.TextArea rows={8} placeholder="文档内容（支持 Markdown）" />
          </Form.Item>
        </Form>
      </Modal>

      {/* 索引任务抽屉：状态/版本/错误可见，失败可重试（V1.0 B3-2，§8.2） */}
      <Drawer
        title={jobsDoc ? `索引任务 — ${jobsDoc.title}` : '索引任务'}
        width={480}
        open={!!jobsDoc}
        onClose={() => { setJobsDoc(null); setJobs([]); }}
      >
        <Button
          type="primary"
          size="small"
          loading={indexingId === jobsDoc?.id}
          onClick={() => jobsDoc && void handleIndex(jobsDoc)}
          style={{ marginBottom: 16 }}
        >
          重建索引
        </Button>
        {jobsLoading ? (
          <Typography.Text type="secondary">加载中...</Typography.Text>
        ) : jobs.length === 0 ? (
          <Typography.Text type="secondary">暂无索引任务</Typography.Text>
        ) : (
          <Timeline
            items={jobs.map((job) => ({
              color: JOB_STATUS_COLOR[job.Status] || 'gray',
              children: (
                <div>
                  <Space size={8} wrap>
                    <Typography.Text strong>{job.Status}</Typography.Text>
                    <Tag>v{job.DocumentVersion}</Tag>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {job.ID}
                    </Typography.Text>
                    {job.Status === 'failed' && (
                      <a onClick={() => void handleRetryJob(job.ID)}>重试</a>
                    )}
                  </Space>
                  <div style={{ fontSize: 12, color: '#999' }}>
                    {new Date(job.CreatedAt).toLocaleString()}
                    {job.CompletedAt ? ` → ${new Date(job.CompletedAt).toLocaleString()}` : ''}
                  </div>
                  {job.Error && (
                    <Typography.Text type="danger" style={{ fontSize: 12 }}>{job.Error}</Typography.Text>
                  )}
                </div>
              ),
            }))}
          />
        )}
      </Drawer>
    </>
  );
};

export default KnowledgeListPage;
