import React, { useState, useEffect } from 'react';
import { ProDescriptions } from '@ant-design/pro-components';
import { ProCard } from '@ant-design/pro-components';
import { Button, Space, Spin, Tag, Modal, Form, Input, Select, Switch, message } from 'antd';
import { goBack, useDetailParams } from '@/lib/navigation';
import { getDoc, updateDoc, listSources } from '@/services/knowledge';
import { getErrorMessage, isFormValidationError } from '@/utils/error';

const CATEGORIES = ['产品文档', '常见问题', '操作指南', 'API文档', '其他'];

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

const KnowledgeDetailPage: React.FC = () => {
  const { id } = useDetailParams();
  const [loading, setLoading] = useState(true);
  const [doc, setDoc] = useState<API.KnowledgeDoc | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const [sources, setSources] = useState<API.KnowledgeSource[]>([]);
  const [form] = Form.useForm();

  useEffect(() => {
    const fetchDoc = async () => {
      if (!id) return;
      setLoading(true);
      try {
        const result = await getDoc(Number(id));
        if (result) {
          setDoc(result);
        }
      } catch (error) {
        console.error('获取文档详情失败:', error);
      } finally {
        setLoading(false);
      }
    };
    fetchDoc();
    listSources()
      .then((rows) => setSources(rows || []))
      .catch(() => setSources([]));
  }, [id]);

  const openEditModal = () => {
    if (!doc) return;
    form.setFieldsValue({
      title: doc.title,
      category: doc.category,
      content: doc.content,
      tags: normalizeTags(doc.tags).join(', '),
      is_public: doc.is_public ?? false,
      source_id: doc.source_id || undefined,
    });
    setModalOpen(true);
  };

  const handleSave = async () => {
    if (!id) return;
    try {
      const values = await form.validateFields();
      setSubmitting(true);
      const updated = await updateDoc(Number(id), {
        title: values.title,
        category: values.category,
        content: values.content,
        tags: values.tags
          ? values.tags.split(',').map((tag: string) => tag.trim()).filter(Boolean)
          : [],
        is_public: values.is_public ?? false,
        source_id: values.source_id ?? 0,
      });
      setDoc(updated);
      setModalOpen(false);
      message.success('文档已更新');
    } catch (error: unknown) {
      if (isFormValidationError(error)) return;
      message.error(getErrorMessage(error, '更新失败'));
    } finally {
      setSubmitting(false);
    }
  };

  const handlePublish = async () => {
    if (!id || !doc || doc.is_public) return;
    try {
      setPublishing(true);
      const updated = await updateDoc(Number(id), { is_public: true });
      setDoc(updated);
      message.success('文档已发布');
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '发布失败'));
    } finally {
      setPublishing(false);
    }
  };

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 80 }}>
        <Spin size="large" tip="加载中..." />
      </div>
    );
  }

  if (!doc) {
    return (
      <div style={{ textAlign: 'center', padding: 80, color: '#999' }}>
        文档不存在或加载失败
        <br />
        <Button style={{ marginTop: 16 }} onClick={goBack}>
          返回
        </Button>
      </div>
    );
  }

  return (
    <div>
      <ProCard
        title="文档详情"
        extra={
          <Space>
            <Button onClick={goBack}>返回</Button>
            <Button onClick={openEditModal}>编辑</Button>
            <Button type="primary" onClick={handlePublish} loading={publishing} disabled={doc.is_public}>
              {doc.is_public ? '已公开' : '发布'}
            </Button>
          </Space>
        }
      >
        <ProDescriptions
          column={2}
          dataSource={doc}
          columns={[
            { title: '文档ID', dataIndex: 'id' },
            { title: '标题', dataIndex: 'title' },
            { title: '分类', dataIndex: 'category' },
            {
              title: '公开',
              dataIndex: 'is_public',
              render: (_, record) => (
                <Tag color={record.is_public ? 'green' : 'default'}>
                  {record.is_public ? '公开' : '内部'}
                </Tag>
              ),
            },
            {
              title: '标签',
              dataIndex: 'tags',
              render: (_, record) => {
                const tags = normalizeTags(record.tags);
                return tags.length > 0 ? tags.map((tag) => <Tag key={tag}>{tag}</Tag>) : '-';
              },
            },
            {
              title: '知识来源',
              dataIndex: 'source_id',
              render: (_, record) => {
                if (!record.source_id) return <Tag>未挂源</Tag>;
                const source = sources.find((s) => s.id === record.source_id);
                return <Tag color="geekblue">{source?.name || `来源 #${record.source_id}`}</Tag>;
              },
            },
            {
              title: '版本',
              dataIndex: 'version',
              render: (_, record) => <Tag>v{record.version ?? 1}</Tag>,
            },
            { title: '创建时间', dataIndex: 'created_at' },
            { title: '更新时间', dataIndex: 'updated_at' },
          ]}
        />
      </ProCard>

      <ProCard title="文档内容" style={{ marginTop: 16 }}>
        {doc.content ? (
          <div style={{ whiteSpace: 'pre-wrap', lineHeight: 1.8 }}>
            {doc.content}
          </div>
        ) : (
          <div style={{ textAlign: 'center', padding: 40, color: '#999' }}>
            暂无文档内容
          </div>
        )}
      </ProCard>

      <Modal
        title="编辑文档"
        open={modalOpen}
        onCancel={() => {
          setModalOpen(false);
          form.resetFields();
        }}
        onOk={handleSave}
        confirmLoading={submitting}
        okText="保存"
        width={720}
      >
        <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item name="title" label="标题" rules={[{ required: true, message: '请输入标题' }]}>
            <Input placeholder="文档标题" />
          </Form.Item>
          <Form.Item name="category" label="分类">
            <Select allowClear options={CATEGORIES.map((item) => ({ label: item, value: item }))} />
          </Form.Item>
          <Form.Item name="source_id" label="知识来源">
            <Select
              placeholder="选择来源登记（可选）"
              allowClear
              options={sources.map((s) => ({ label: `${s.name}（${s.type}）`, value: s.id }))}
            />
          </Form.Item>
          <Form.Item name="tags" label="标签（逗号分隔）">
            <Input placeholder="例如：入门, API, 常见问题" />
          </Form.Item>
          <Form.Item name="is_public" label="公开" valuePropName="checked">
            <Switch checkedChildren="公开" unCheckedChildren="内部" />
          </Form.Item>
          <Form.Item name="content" label="内容" rules={[{ required: true, message: '请输入内容' }]}>
            <Input.TextArea rows={10} placeholder="文档内容" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default KnowledgeDetailPage;
