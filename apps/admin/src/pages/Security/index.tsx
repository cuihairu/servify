import React, { useRef, useState } from 'react';
import { ProCard, ProDescriptions, ProTable } from '@ant-design/pro-components';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import {
  Button,
  Drawer,
  Form,
  Input,
  Modal,
  Popconfirm,
  Space,
  Spin,
  Tag,
  message,
} from 'antd';
import {
  batchRevokeUserTokens,
  getUserSecurity,
  listRevokedTokens,
  listUserSessions,
  queryUsersSecurity,
  revokeAllUserSessions,
  revokeSecurityToken,
  revokeUserSession,
  revokeUserTokens,
} from '@/services/security';
import { getErrorMessage, isFormValidationError } from '@/utils/error';
import ScopedConfigCard from './ScopedConfigCard';

const parseUserIds = (raw: string): number[] => {
  const seen = new Set<number>();
  return raw
    .split(/[\s,]+/)
    .map((value) => Number(value.trim()))
    .filter((value) => Number.isInteger(value) && value > 0)
    .filter((value) => {
      if (seen.has(value)) {
        return false;
      }
      seen.add(value);
      return true;
    });
};

const statusColorMap: Record<string, string> = {
  active: 'green',
  revoked: 'red',
  inactive: 'default',
  banned: 'red',
};

const riskColorMap: Record<string, string> = {
  low: 'blue',
  medium: 'orange',
  high: 'red',
};

const networkColorMap: Record<string, string> = {
  public: 'orange',
  private: 'green',
  loopback: 'blue',
  unknown: 'default',
};

const SecurityPage: React.FC = () => {
  const revokedActionRef = useRef<ActionType>();
  const [lookupInput, setLookupInput] = useState('');
  const [lookupLoading, setLookupLoading] = useState(false);
  const [lookupUserIds, setLookupUserIds] = useState<number[]>([]);
  const [previewItems, setPreviewItems] = useState<API.UserSecurityPreview[]>([]);
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerLoading, setDrawerLoading] = useState(false);
  const [selectedUser, setSelectedUser] = useState<API.UserSecurityPreview | null>(null);
  const [selectedUserDetail, setSelectedUserDetail] = useState<API.UserSecurityDetail | null>(null);
  const [selectedUserSessions, setSelectedUserSessions] = useState<API.UserSecuritySession[]>([]);
  const [tokenModalOpen, setTokenModalOpen] = useState(false);
  const [tokenSubmitting, setTokenSubmitting] = useState(false);
  const [tokenForm] = Form.useForm<{ token: string; reason?: string }>();

  const loadUserPreview = async (userIds = lookupUserIds) => {
    if (!userIds.length) {
      setPreviewItems([]);
      setLookupUserIds([]);
      setSelectedRowKeys([]);
      return;
    }

    setLookupLoading(true);
    try {
      const result = await queryUsersSecurity(userIds);
      setPreviewItems(result?.items || []);
      setLookupUserIds(userIds);
      setSelectedRowKeys((keys) => keys.filter((key) => userIds.includes(Number(key))));
    } catch (error: unknown) {
      setPreviewItems([]);
      setSelectedRowKeys([]);
      message.error(getErrorMessage(error, '查询用户安全态失败'));
    } finally {
      setLookupLoading(false);
    }
  };

  const openUserDrawer = async (record: API.UserSecurityPreview) => {
    setSelectedUser(record);
    setDrawerOpen(true);
    setDrawerLoading(true);
    try {
      const [detailResult, sessionsResult] = await Promise.all([
        getUserSecurity(record.user_id),
        listUserSessions(record.user_id),
      ]);
      setSelectedUserDetail(detailResult || null);
      setSelectedUserSessions(sessionsResult?.items || []);
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '加载用户安全详情失败'));
      setSelectedUserDetail(null);
      setSelectedUserSessions([]);
    } finally {
      setDrawerLoading(false);
    }
  };

  const reloadSelectedUser = async () => {
    if (selectedUser) {
      await openUserDrawer(selectedUser);
    }
  };

  const handleLookup = async () => {
    const userIds = parseUserIds(lookupInput);
    if (!userIds.length) {
      message.warning('请输入至少一个有效的用户 ID');
      return;
    }
    await loadUserPreview(userIds);
  };

  const handleRevokeUserTokens = async (userId: number) => {
    try {
      await revokeUserTokens(userId);
      message.success(`已吊销用户 #${userId} 的全部 Token`);
      await loadUserPreview();
      if (selectedUser?.user_id === userId) {
        await reloadSelectedUser();
      }
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '吊销用户 Token 失败'));
    }
  };

  const handleBatchRevoke = async () => {
    const userIds = selectedRowKeys.map((key) => Number(key)).filter((id) => Number.isInteger(id) && id > 0);
    if (!userIds.length) {
      message.warning('请先选择要吊销的用户');
      return;
    }

    Modal.confirm({
      title: '批量吊销用户 Token',
      content: `确认吊销 ${userIds.length} 个用户的全部 Token 吗？`,
      okText: '确认',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await batchRevokeUserTokens(userIds);
          message.success(`已吊销 ${userIds.length} 个用户的全部 Token`);
          await loadUserPreview();
          if (selectedUser && userIds.includes(selectedUser.user_id)) {
            await reloadSelectedUser();
          }
        } catch (error: unknown) {
          message.error(getErrorMessage(error, '批量吊销用户 Token 失败'));
        }
      },
    });
  };

  const handleRevokeSession = async (sessionId: string) => {
    if (!selectedUser) {
      return;
    }

    try {
      await revokeUserSession(selectedUser.user_id, sessionId);
      message.success(`已吊销会话 ${sessionId}`);
      await reloadSelectedUser();
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '吊销会话失败'));
    }
  };

  const handleRevokeAllSessions = async () => {
    if (!selectedUser) {
      return;
    }

    try {
      await revokeAllUserSessions(selectedUser.user_id);
      message.success(`已吊销用户 #${selectedUser.user_id} 的全部活跃会话`);
      await reloadSelectedUser();
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '批量吊销会话失败'));
    }
  };

  const handleSubmitRawToken = async () => {
    try {
      const values = await tokenForm.validateFields();
      setTokenSubmitting(true);
      const result = await revokeSecurityToken(values.token, values.reason);
      message.success(`已加入 denylist：${result.jti}`);
      setTokenModalOpen(false);
      tokenForm.resetFields();
      revokedActionRef.current?.reload();
    } catch (error: unknown) {
      if (isFormValidationError(error)) {
        return;
      }
      message.error(getErrorMessage(error, '吊销 JWT 失败'));
    } finally {
      setTokenSubmitting(false);
    }
  };

  const userColumns: ProColumns<API.UserSecurityPreview>[] = [
    {
      title: '用户 ID',
      dataIndex: 'user_id',
      width: 96,
    },
    {
      title: '用户名',
      dataIndex: 'username',
      copyable: true,
      width: 160,
    },
    {
      title: '姓名',
      dataIndex: 'name',
      width: 160,
      render: (_, record) => record.name || '-',
    },
    {
      title: '角色',
      dataIndex: 'role',
      width: 100,
      render: (_, record) => <Tag color="blue">{record.role || '-'}</Tag>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (_, record) => (
        <Tag color={statusColorMap[record.status] || 'default'}>
          {record.status || '-'}
        </Tag>
      ),
    },
    {
      title: '当前版本',
      dataIndex: 'token_version',
      width: 96,
    },
    {
      title: '下次吊销后版本',
      dataIndex: 'next_token_version',
      width: 128,
    },
    {
      title: '最后登录',
      dataIndex: 'last_login',
      valueType: 'dateTime',
      width: 180,
    },
    {
      title: 'Token 生效下界',
      dataIndex: 'token_valid_after',
      valueType: 'dateTime',
      width: 180,
    },
    {
      title: '操作',
      valueType: 'option',
      width: 180,
      fixed: 'right',
      render: (_, record) => [
        <a key="sessions" onClick={() => void openUserDrawer(record)}>
          查看会话
        </a>,
        <Popconfirm
          key="revoke"
          title="确认吊销该用户全部 Token？"
          description="会提升 token_version 并刷新 token_valid_after。"
          okText="确认"
          cancelText="取消"
          onConfirm={() => handleRevokeUserTokens(record.user_id)}
        >
          <a>吊销 Token</a>
        </Popconfirm>,
      ],
    },
  ];

  const sessionColumns: ProColumns<API.UserSecuritySession>[] = [
    {
      title: '会话 ID',
      dataIndex: 'session_id',
      width: 220,
      copyable: true,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 90,
      render: (_, record) => (
        <Tag color={statusColorMap[record.status || ''] || 'default'}>
          {record.status || '-'}
        </Tag>
      ),
    },
    {
      title: '版本',
      dataIndex: 'token_version',
      width: 72,
    },
    {
      title: '风险等级',
      dataIndex: 'risk_level',
      width: 110,
      render: (_, record) => (
        <Space size={[4, 4]} wrap>
          <Tag color={riskColorMap[record.risk_level || ''] || 'default'}>
            {record.risk_level || 'unknown'}
          </Tag>
          {typeof record.risk_score === 'number' ? <Tag>{`分数 ${record.risk_score}`}</Tag> : null}
        </Space>
      ),
    },
    {
      title: '网络',
      dataIndex: 'network_label',
      width: 120,
      render: (_, record) => (
        <Space size={[4, 4]} wrap>
          <Tag color={networkColorMap[record.network_label || ''] || 'default'}>
            {record.network_label || 'unknown'}
          </Tag>
          {record.location_label ? <Tag>{record.location_label}</Tag> : null}
        </Space>
      ),
    },
    {
      title: '风险原因',
      dataIndex: 'risk_reasons',
      width: 220,
      render: (_, record) =>
        record.risk_reasons && record.risk_reasons.length > 0 ? (
          <Space size={[4, 4]} wrap>
            {record.risk_reasons.slice(0, 3).map((reason) => (
              <Tag key={reason}>{reason}</Tag>
            ))}
          </Space>
        ) : (
          '-'
        ),
    },
    {
      title: '漂移信号',
      key: 'signals',
      width: 220,
      render: (_, record) => {
        const tags = [];
        if (record.ip_drift) tags.push(<Tag key="ip_drift" color="orange">IP 漂移</Tag>);
        if (record.device_drift) tags.push(<Tag key="device_drift" color="orange">设备漂移</Tag>);
        if (record.rapid_ip_change) tags.push(<Tag key="rapid_ip_change" color="red">快速换 IP</Tag>);
        if (record.rapid_device_change) tags.push(<Tag key="rapid_device_change" color="red">快速换设备</Tag>);
        if (record.rapid_refresh_activity) tags.push(<Tag key="rapid_refresh" color="red">高频刷新</Tag>);
        if (record.refresh_recency && record.refresh_recency !== 'unknown') {
          tags.push(<Tag key="refresh_recency">{record.refresh_recency}</Tag>);
        }
        return tags.length ? <Space size={[4, 4]} wrap>{tags}</Space> : '-';
      },
    },
    {
      title: '设备指纹',
      dataIndex: 'device_fingerprint',
      width: 160,
      ellipsis: true,
      render: (_, record) => record.device_fingerprint || '-',
    },
    {
      title: '客户端',
      dataIndex: 'user_agent',
      width: 240,
      ellipsis: true,
      render: (_, record) => record.user_agent || '-',
    },
    {
      title: 'IP',
      dataIndex: 'client_ip',
      width: 130,
      render: (_, record) => record.client_ip || '-',
    },
    {
      title: '最后活跃',
      dataIndex: 'last_seen_at',
      valueType: 'dateTime',
      width: 170,
    },
    {
      title: '最近刷新',
      dataIndex: 'last_refreshed_at',
      valueType: 'dateTime',
      width: 170,
    },
    {
      title: '失效时间',
      dataIndex: 'revoked_at',
      valueType: 'dateTime',
      width: 170,
    },
    {
      title: '操作',
      valueType: 'option',
      width: 120,
      fixed: 'right',
      render: (_, record) =>
        record.status === 'active' ? [
          <Popconfirm
            key="revoke-session"
            title="确认吊销该会话？"
            okText="确认"
            cancelText="取消"
            onConfirm={() => handleRevokeSession(record.session_id)}
          >
            <a>吊销会话</a>
          </Popconfirm>,
        ] : [
          <span key="revoked" style={{ color: '#999' }}>
            已失效
          </span>,
        ],
    },
  ];

  const revokedTokenColumns: ProColumns<API.RevokedToken>[] = [
    {
      title: 'JTI',
      dataIndex: 'jti',
      copyable: true,
      width: 260,
    },
    {
      title: '用户 ID',
      dataIndex: 'user_id',
      width: 96,
    },
    {
      title: '会话 ID',
      dataIndex: 'session_id',
      copyable: true,
      width: 220,
      render: (_, record) => record.session_id || '-',
    },
    {
      title: '用途',
      dataIndex: 'token_use',
      width: 100,
      valueType: 'select',
      valueEnum: {
        access: { text: 'access' },
        refresh: { text: 'refresh' },
      },
      render: (_, record) => <Tag>{record.token_use || '-'}</Tag>,
    },
    {
      title: '原因',
      dataIndex: 'reason',
      search: false,
      ellipsis: true,
      render: (_, record) => record.reason || '-',
    },
    {
      title: '过期时间',
      dataIndex: 'expires_at',
      valueType: 'dateTime',
      search: false,
      width: 180,
    },
    {
      title: '吊销时间',
      dataIndex: 'revoked_at',
      valueType: 'dateTime',
      search: false,
      width: 180,
    },
    {
      title: '仅未过期',
      dataIndex: 'active_only',
      hideInTable: true,
      valueType: 'select',
      initialValue: 'true',
      valueEnum: {
        true: { text: '是' },
        false: { text: '否' },
      },
    },
  ];

  const detailData: Partial<API.UserSecurityPreview & API.UserSecurityDetail> | undefined = selectedUser
    ? {
        ...selectedUser,
        ...selectedUserDetail,
      }
    : selectedUserDetail || undefined;

  return (
    <div>
      <ProCard
        title="用户安全态查询"
        extra={
          <Space>
            <Button onClick={() => void loadUserPreview()} disabled={!lookupUserIds.length}>
              刷新结果
            </Button>
            <Button danger disabled={!selectedRowKeys.length} onClick={handleBatchRevoke}>
              批量吊销 Token
            </Button>
          </Space>
        }
      >
        <Input.TextArea
          rows={3}
          value={lookupInput}
          onChange={(event) => setLookupInput(event.target.value)}
          placeholder="输入一个或多个用户 ID，支持逗号、空格或换行分隔，例如：101, 102, 205"
        />
        <Space style={{ marginTop: 12, marginBottom: 16 }}>
          <Button type="primary" onClick={() => void handleLookup()} loading={lookupLoading}>
            查询用户安全态
          </Button>
          <Button
            onClick={() => {
              setLookupInput('');
              setPreviewItems([]);
              setLookupUserIds([]);
              setSelectedRowKeys([]);
            }}
          >
            清空结果
          </Button>
        </Space>

        <ProTable<API.UserSecurityPreview>
          headerTitle="查询结果"
          rowKey="user_id"
          search={false}
          options={false}
          loading={lookupLoading}
          columns={userColumns}
          dataSource={previewItems}
          rowSelection={{
            selectedRowKeys,
            onChange: (keys) => setSelectedRowKeys(keys),
          }}
          pagination={false}
          scroll={{ x: 1320 }}
          locale={{
            emptyText: lookupUserIds.length ? '未查询到匹配用户' : '请输入用户 ID 后开始查询',
          }}
        />
      </ProCard>

      <ProCard
        title="JWT 吊销与 Denylist"
        style={{ marginTop: 16 }}
        extra={
          <Button type="primary" onClick={() => setTokenModalOpen(true)}>
            吊销原始 JWT
          </Button>
        }
      >
        <ProTable<API.RevokedToken>
          actionRef={revokedActionRef}
          rowKey="jti"
          columns={revokedTokenColumns}
          search={{ filterType: 'light' }}
          pagination={{ defaultPageSize: 20 }}
          request={async (params) => {
            try {
              const result = await listRevokedTokens({
                page: params.current,
                page_size: params.pageSize,
                jti: typeof params.jti === 'string' ? params.jti : undefined,
                user_id:
                  typeof params.user_id === 'string' || typeof params.user_id === 'number'
                    ? params.user_id
                    : undefined,
                session_id: typeof params.session_id === 'string' ? params.session_id : undefined,
                token_use: typeof params.token_use === 'string' ? params.token_use : undefined,
                active_only:
                  typeof params.active_only === 'string' || typeof params.active_only === 'boolean'
                    ? params.active_only
                    : undefined,
              });
              return {
                data: result?.items || [],
                total: result?.count || 0,
                success: true,
              };
            } catch (error) {
              console.error('获取 denylist 失败:', error);
              return {
                data: [],
                total: 0,
                success: true,
              };
            }
          }}
          scroll={{ x: 1180 }}
        />
      </ProCard>

      <ScopedConfigCard />

      <Drawer
        title={
          selectedUser
            ? `安全详情 #${selectedUser.user_id} ${selectedUser.username ? `· ${selectedUser.username}` : ''}`
            : '安全详情'
        }
        width={1200}
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        extra={
          selectedUser ? (
            <Space>
              <Button onClick={() => void reloadSelectedUser()}>刷新</Button>
              <Button danger onClick={() => void handleRevokeUserTokens(selectedUser.user_id)}>
                吊销全部 Token
              </Button>
              <Button danger onClick={() => void handleRevokeAllSessions()}>
                吊销全部会话
              </Button>
            </Space>
          ) : null
        }
      >
        {drawerLoading ? (
          <div style={{ textAlign: 'center', padding: 80 }}>
            <Spin size="large" tip="加载安全详情中..." />
          </div>
        ) : (
          <div>
            <ProDescriptions
              title="用户安全态"
              column={2}
              dataSource={detailData || {}}
              columns={[
                { title: '用户 ID', dataIndex: 'user_id' },
                { title: '用户名', dataIndex: 'username', copyable: true },
                { title: '姓名', dataIndex: 'name' },
                {
                  title: '角色',
                  dataIndex: 'role',
                  render: (_, record: Partial<API.UserSecurityPreview & API.UserSecurityDetail>) => (
                    <Tag color="blue">{String(record.role || '-')}</Tag>
                  ),
                },
                {
                  title: '状态',
                  dataIndex: 'status',
                  render: (_, record: Partial<API.UserSecurityPreview & API.UserSecurityDetail>) => (
                    <Tag color={statusColorMap[String(record.status || '')] || 'default'}>
                      {String(record.status || '-')}
                    </Tag>
                  ),
                },
                { title: 'Token 版本', dataIndex: 'token_version' },
                { title: '下次吊销后版本', dataIndex: 'next_token_version' },
                { title: '最后登录', dataIndex: 'last_login', valueType: 'dateTime' },
                {
                  title: 'Token 生效下界',
                  dataIndex: 'token_valid_after',
                  valueType: 'dateTime',
                },
              ]}
            />

            <ProTable<API.UserSecuritySession>
              headerTitle="认证会话"
              rowKey="session_id"
              search={false}
              options={false}
              style={{ marginTop: 16 }}
              columns={sessionColumns}
              dataSource={selectedUserSessions}
              pagination={false}
              scroll={{ x: 2200 }}
              locale={{ emptyText: '暂无认证会话' }}
            />
          </div>
        )}
      </Drawer>

      <Modal
        title="吊销原始 JWT"
        open={tokenModalOpen}
        okText="加入 denylist"
        okButtonProps={{ danger: true }}
        confirmLoading={tokenSubmitting}
        onOk={() => void handleSubmitRawToken()}
        onCancel={() => {
          setTokenModalOpen(false);
          tokenForm.resetFields();
        }}
      >
        <Form form={tokenForm} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item
            name="token"
            label="JWT"
            rules={[{ required: true, message: '请输入要吊销的 JWT' }]}
          >
            <Input.TextArea
              rows={6}
              placeholder="粘贴 access token 或 refresh token，提交后会按 jti 加入 denylist"
            />
          </Form.Item>
          <Form.Item name="reason" label="吊销原因">
            <Input.TextArea rows={3} placeholder="例如：密钥泄露、异常设备登录、人工审计要求" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default SecurityPage;
