import React, { useRef, useState } from 'react';
import { ProCard, ProTable } from '@ant-design/pro-components';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import {
  Alert,
  Button,
  Collapse,
  Descriptions,
  Drawer,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Radio,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import {
  approveScopedConfig,
  getScopedConfig,
  getScopedConfigHistoryEntry,
  listScopedConfigHistory,
  putScopedConfig,
  rollbackScopedConfig,
  verifyScopedConfig,
} from '@/services/scopedConfig';
import type { ScopedConfigScope } from '@/services/scopedConfig';
import { getErrorMessage, isFormValidationError } from '@/utils/error';

const governanceColorMap: Record<string, string> = {
  awaiting_approval: 'orange',
  awaiting_verification: 'blue',
  verification_failed: 'red',
  verified: 'green',
  approved: 'green',
  not_required: 'default',
};

const governanceLabelMap: Record<string, string> = {
  awaiting_approval: '待审批',
  awaiting_verification: '待核验',
  verification_failed: '核验未过',
  verified: '已核验',
  approved: '已审批',
  not_required: '无需流程',
};

const riskColorMap: Record<string, string> = {
  low: 'blue',
  medium: 'orange',
  high: 'red',
};

const riskLabelMap: Record<string, string> = {
  low: '低风险',
  medium: '中风险',
  high: '高风险',
};

const approvalStatusColorMap: Record<string, string> = {
  not_required: 'default',
  missing: 'red',
  provided: 'blue',
  approved: 'green',
};

const approvalStatusLabelMap: Record<string, string> = {
  not_required: '无需审批',
  missing: '缺审批',
  provided: '已附审批引用',
  approved: '已审批',
};

const sectionLabelMap: Record<string, string> = {
  portal: 'Portal 外观',
  openai: 'OpenAI Provider',
  dify: 'Dify Provider',
  weknora: 'WeKnora Provider',
  ragflow: 'RAGFlow Provider',
  session_risk: '会话风控',
  translation_quota: '翻译配额覆盖',
};

const operationLabelMap: Record<string, string> = {
  update: '更新',
  rollback: '回滚',
};

const verificationStatusColorMap: Record<string, string> = {
  not_applicable: 'default',
  pending: 'blue',
  passed: 'green',
  failed: 'red',
};

const verificationStatusLabelMap: Record<string, string> = {
  not_applicable: '不适用',
  pending: '待核验',
  passed: '通过',
  failed: '未通过',
};

const formatJSON = (value: unknown): string => {
  if (value === undefined || value === null) {
    return '-';
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
};

const splitEvidence = (raw?: string): string[] =>
  (raw || '')
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean);

interface QuotaFormValues {
  limit: number;
  change_ref: string;
  reason: string;
  approval_ref?: string;
}

interface RollbackFormValues {
  change_ref: string;
  reason: string;
  approval_ref?: string;
}

interface ApproveFormValues {
  change_ref: string;
  reason: string;
  approval_ref: string;
  notes: string;
  evidence_text?: string;
}

interface VerifyFormValues {
  status: 'passed' | 'failed';
  notes?: string;
  evidence_text?: string;
  change_ref: string;
  reason: string;
  checks?: Record<string, string>;
  check_notes?: Record<string, string>;
}

const ScopedConfigCard: React.FC = () => {
  const [scope, setScope] = useState<ScopedConfigScope>('tenant');
  const [workspaceIdInput, setWorkspaceIdInput] = useState('');
  const [configLoading, setConfigLoading] = useState(false);
  const [configLoaded, setConfigLoaded] = useState(false);
  const [doc, setDoc] = useState<API.ScopedConfigDocument | null>(null);
  const [docScope, setDocScope] = useState<{ tenant_id?: string; workspace_id?: string }>({});
  const [saving, setSaving] = useState(false);
  const [quotaForm] = Form.useForm<QuotaFormValues>();

  const historyActionRef = useRef<ActionType>();
  const scopeRef = useRef<ScopedConfigScope>('tenant');
  const workspaceRef = useRef<string>('');

  const [drawerOpen, setDrawerOpen] = useState(false);
  const [entryLoading, setEntryLoading] = useState(false);
  const [entry, setEntry] = useState<API.ScopedConfigHistoryEntry | null>(null);
  const [rollbackForm] = Form.useForm<RollbackFormValues>();
  const [approveForm] = Form.useForm<ApproveFormValues>();
  const [verifyForm] = Form.useForm<VerifyFormValues>();
  const [acting, setActing] = useState(false);

  const currentScopeArgs = (): [ScopedConfigScope, string | undefined] => {
    if (scopeRef.current === 'workspace') {
      return ['workspace', workspaceRef.current];
    }
    return ['tenant', undefined];
  };

  const loadConfig = async () => {
    const activeScope = scope;
    if (activeScope === 'workspace' && !workspaceIdInput.trim()) {
      message.warning('请输入 workspace ID');
      return;
    }
    scopeRef.current = activeScope;
    workspaceRef.current = workspaceIdInput.trim();
    setConfigLoading(true);
    try {
      const result = await getScopedConfig(activeScope, workspaceIdInput.trim() || undefined);
      const nextDoc = result?.config || null;
      setDoc(nextDoc);
      setDocScope({
        tenant_id: result?.tenant_id,
        workspace_id: result?.workspace_id,
      });
      setConfigLoaded(true);
      quotaForm.setFieldsValue({
        limit: nextDoc?.translation_quota?.limit ?? 0,
        change_ref: undefined,
        reason: undefined,
        approval_ref: undefined,
      });
      historyActionRef.current?.reload();
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '加载作用域配置失败'));
    } finally {
      setConfigLoading(false);
    }
  };

  const handleScopeChange = (next: ScopedConfigScope) => {
    setScope(next);
    setConfigLoaded(false);
    setDoc(null);
    setDocScope({});
  };

  const handleSaveQuota = async () => {
    let values: QuotaFormValues;
    try {
      values = await quotaForm.validateFields();
    } catch (error: unknown) {
      if (isFormValidationError(error)) {
        return;
      }
      throw error;
    }
    const [activeScope, workspaceId] = currentScopeArgs();
    setSaving(true);
    try {
      await putScopedConfig(
        activeScope,
        {
          translation_quota: { limit: values.limit },
          change_ref: values.change_ref.trim(),
          reason: values.reason.trim(),
          ...(values.approval_ref?.trim() ? { approval_ref: values.approval_ref.trim() } : {}),
        },
        workspaceId,
      );
      message.success('翻译配额覆盖已保存');
      await loadConfig();
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '保存翻译配额覆盖失败'));
    } finally {
      setSaving(false);
    }
  };

  const openEntry = async (auditId: number) => {
    setDrawerOpen(true);
    setEntryLoading(true);
    setEntry(null);
    try {
      const [activeScope, workspaceId] = currentScopeArgs();
      const result = await getScopedConfigHistoryEntry(activeScope, auditId, workspaceId);
      setEntry(result || null);
      const changeControl = result?.change_control;
      rollbackForm.setFieldsValue({
        change_ref: changeControl?.change_ref ? `${changeControl.change_ref}-rollback` : '',
        reason: '',
        approval_ref: changeControl?.approval_ref || '',
      });
      approveForm.setFieldsValue({
        change_ref: changeControl?.change_ref || '',
        reason: changeControl?.reason || '',
        approval_ref: '',
        notes: '',
        evidence_text: '',
      });
      verifyForm.setFieldsValue({
        status: undefined,
        notes: '',
        evidence_text: '',
        change_ref: changeControl?.change_ref ? `${changeControl.change_ref}-verify` : '',
        reason: '',
        checks: {},
        check_notes: {},
      });
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '加载配置变更详情失败'));
      setDrawerOpen(false);
    } finally {
      setEntryLoading(false);
    }
  };

  const reloadAfterAction = async (successText: string) => {
    message.success(successText);
    historyActionRef.current?.reload();
    if (entry) {
      await openEntry(entry.audit.id);
    }
    await loadConfig();
  };

  const submitRollback = async () => {
    if (!entry) {
      return;
    }
    let values: RollbackFormValues;
    try {
      values = await rollbackForm.validateFields();
    } catch (error: unknown) {
      if (isFormValidationError(error)) {
        return;
      }
      throw error;
    }
    const [activeScope, workspaceId] = currentScopeArgs();
    setActing(true);
    try {
      await rollbackScopedConfig(
        activeScope,
        entry.audit.id,
        {
          confirm: true,
          change_ref: values.change_ref.trim(),
          reason: values.reason.trim(),
          ...(values.approval_ref?.trim() ? { approval_ref: values.approval_ref.trim() } : {}),
        },
        workspaceId,
      );
      await reloadAfterAction('已回滚到所选版本');
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '回滚失败'));
    } finally {
      setActing(false);
    }
  };

  const submitApprove = async () => {
    if (!entry) {
      return;
    }
    let values: ApproveFormValues;
    try {
      values = await approveForm.validateFields();
    } catch (error: unknown) {
      if (isFormValidationError(error)) {
        return;
      }
      throw error;
    }
    const [activeScope, workspaceId] = currentScopeArgs();
    setActing(true);
    try {
      await approveScopedConfig(
        activeScope,
        {
          change_ref: values.change_ref.trim(),
          reason: values.reason.trim(),
          approval_ref: values.approval_ref.trim(),
          notes: values.notes.trim(),
          evidence: splitEvidence(values.evidence_text),
        },
        workspaceId,
      );
      await reloadAfterAction('审批已记录');
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '记录审批失败'));
    } finally {
      setActing(false);
    }
  };

  const submitVerify = async () => {
    if (!entry) {
      return;
    }
    let values: VerifyFormValues;
    try {
      values = await verifyForm.validateFields();
    } catch (error: unknown) {
      if (isFormValidationError(error)) {
        return;
      }
      throw error;
    }
    const checkResults: API.ScopedConfigVerificationCheckResult[] = [];
    for (const definition of entry.verification_template?.checks || []) {
      const status = values.checks?.[definition.id];
      if (!status) {
        continue;
      }
      checkResults.push({
        id: definition.id,
        status,
        notes: values.check_notes?.[definition.id]?.trim(),
      });
    }
    const [activeScope, workspaceId] = currentScopeArgs();
    setActing(true);
    try {
      await verifyScopedConfig(
        activeScope,
        entry.audit.id,
        {
          status: values.status,
          notes: values.notes?.trim() || undefined,
          evidence: splitEvidence(values.evidence_text),
          ...(checkResults.length ? { checks: checkResults } : {}),
          change_ref: values.change_ref.trim(),
          reason: values.reason.trim(),
        },
        workspaceId,
      );
      await reloadAfterAction(values.status === 'passed' ? '核验已提交：通过' : '核验已提交：未通过');
    } catch (error: unknown) {
      message.error(getErrorMessage(error, '提交核验失败'));
    } finally {
      setActing(false);
    }
  };

  const docSections = Object.entries(sectionLabelMap).filter(([key]) => {
    const value = doc?.[key as keyof API.ScopedConfigDocument];
    return value !== undefined && value !== null;
  });

  const historyColumns: ProColumns<API.ScopedConfigHistoryItem>[] = [
    {
      title: '审计 ID',
      dataIndex: ['audit', 'id'],
      width: 90,
    },
    {
      title: '操作',
      dataIndex: 'operation',
      width: 90,
      valueType: 'select',
      valueEnum: {
        update: { text: '更新' },
        rollback: { text: '回滚' },
      },
      render: (_, record) => (
        <Tag>{operationLabelMap[record.operation] || record.operation}</Tag>
      ),
    },
    {
      title: '变更引用',
      key: 'change_ref',
      search: false,
      width: 160,
      ellipsis: true,
      render: (_, record) => record.change_control?.change_ref || '-',
    },
    {
      title: '风险',
      key: 'risk_level',
      search: false,
      width: 90,
      render: (_, record) => (
        <Tag color={riskColorMap[record.change_risk?.risk_level || ''] || 'default'}>
          {riskLabelMap[record.change_risk?.risk_level || ''] || record.change_risk?.risk_level || '-'}
        </Tag>
      ),
    },
    {
      title: '审批',
      key: 'approval_status',
      search: false,
      width: 110,
      render: (_, record) => {
        const status = record.approval_policy?.approval_status || '';
        return (
          <Tag color={approvalStatusColorMap[status] || 'default'}>
            {approvalStatusLabelMap[status] || status || '-'}
          </Tag>
        );
      },
    },
    {
      title: '治理状态',
      dataIndex: 'governance_status',
      search: false,
      width: 110,
      render: (_, record) => (
        <Tag color={governanceColorMap[record.governance_status] || 'default'}>
          {governanceLabelMap[record.governance_status] || record.governance_status}
        </Tag>
      ),
    },
    {
      title: '核验',
      dataIndex: 'verification_status',
      search: false,
      width: 90,
      render: (_, record) => (
        <Tag color={verificationStatusColorMap[record.verification_status] || 'default'}>
          {verificationStatusLabelMap[record.verification_status] || record.verification_status}
        </Tag>
      ),
    },
    {
      title: '操作人',
      dataIndex: ['audit', 'actor_user_id'],
      search: false,
      width: 90,
      render: (_, record) => record.audit.actor_user_id ?? '-',
    },
    {
      title: '时间',
      dataIndex: ['audit', 'created_at'],
      search: false,
      valueType: 'dateTime',
      width: 170,
    },
    {
      title: '操作',
      valueType: 'option',
      width: 90,
      fixed: 'right',
      render: (_, record) => [
        <a key="detail" onClick={() => void openEntry(record.audit.id)}>
          详情
        </a>,
      ],
    },
  ];

  const diffColumns = [
    {
      title: '路径',
      dataIndex: 'path',
      width: 240,
      render: (value: string) => <Typography.Text code>{value}</Typography.Text>,
    },
    {
      title: '类型',
      dataIndex: 'type',
      width: 90,
      render: (value: string) => {
        const colorMap: Record<string, string> = {
          added: 'green',
          removed: 'red',
          updated: 'orange',
        };
        const labelMap: Record<string, string> = {
          added: '新增',
          removed: '移除',
          updated: '修改',
        };
        return <Tag color={colorMap[value] || 'default'}>{labelMap[value] || value}</Tag>;
      },
    },
    {
      title: '当前值',
      dataIndex: 'current',
      render: (value: unknown) => (
        <pre style={{ margin: 0, maxHeight: 160, overflow: 'auto', fontSize: 12 }}>
          {formatJSON(value)}
        </pre>
      ),
    },
    {
      title: '快照值',
      dataIndex: 'snapshot',
      render: (value: unknown) => (
        <pre style={{ margin: 0, maxHeight: 160, overflow: 'auto', fontSize: 12 }}>
          {formatJSON(value)}
        </pre>
      ),
    },
  ];

  const templateChecks = entry?.verification_template?.checks || [];

  return (
    <div>
      <ProCard
        title="作用域配置覆盖（租户 / 工作区）"
        style={{ marginTop: 16 }}
        extra={
          <Space>
            <Input
              style={{ width: 200 }}
              placeholder="workspace ID（工作区作用域）"
              value={workspaceIdInput}
              disabled={scope === 'tenant'}
              onChange={(event) => setWorkspaceIdInput(event.target.value)}
            />
            <Button type="primary" loading={configLoading} onClick={() => void loadConfig()}>
              加载配置
            </Button>
          </Space>
        }
      >
        <Space style={{ marginBottom: 16 }}>
          <Radio.Group
            value={scope}
            onChange={(event) => handleScopeChange(event.target.value as ScopedConfigScope)}
          >
            <Radio.Button value="tenant">租户作用域</Radio.Button>
            <Radio.Button value="workspace">工作区作用域</Radio.Button>
          </Radio.Group>
          {docScope.tenant_id ? <Tag>tenant: {docScope.tenant_id}</Tag> : null}
          {docScope.workspace_id ? <Tag>workspace: {docScope.workspace_id}</Tag> : null}
        </Space>

        {configLoaded && doc ? (
          <>
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 16 }}
              message="覆盖写入按分节合并：只提交的非空分节会被覆盖，其余分节保持现值。翻译配额只覆盖 limit，limit<=0 表示回落系统值（enabled/unit/window 恒为系统级口径）。"
            />
            {docSections.length > 0 ? (
              <Collapse
                style={{ marginBottom: 16 }}
                items={docSections.map(([key, label]) => ({
                  key,
                  label: `${label}（已覆盖）`,
                  children: (
                    <pre style={{ margin: 0, maxHeight: 260, overflow: 'auto', fontSize: 12 }}>
                      {formatJSON(doc[key as keyof API.ScopedConfigDocument])}
                    </pre>
                  ),
                }))}
              />
            ) : (
              <Alert
                type="warning"
                showIcon
                style={{ marginBottom: 16 }}
                message="当前作用域尚无任何覆盖分节，全部取系统级配置。"
              />
            )}

            <Form form={quotaForm} layout="vertical" style={{ maxWidth: 720 }}>
              <Descriptions title="翻译配额覆盖（translation_quota）" column={1} size="small" />
              <Form.Item
                name="limit"
                label="覆盖 limit"
                initialValue={0}
                extra="0 表示不覆盖（回落系统值）；enabled/unit/window 由系统级配置决定"
                rules={[
                  { required: true, message: '请输入 limit' },
                  {
                    type: 'integer',
                    min: 0,
                    message: 'limit 必须为不小于 0 的整数',
                  },
                ]}
              >
                <InputNumber min={0} precision={0} style={{ width: 240 }} />
              </Form.Item>
              <Form.Item
                name="change_ref"
                label="变更引用（change_ref）"
                rules={[{ required: true, whitespace: true, message: '变更引用必填' }]}
              >
                <Input placeholder="例如：CHG-20261019-001" />
              </Form.Item>
              <Form.Item
                name="reason"
                label="变更原因（reason）"
                rules={[{ required: true, whitespace: true, message: '变更原因必填' }]}
              >
                <Input.TextArea rows={2} placeholder="说明本次覆盖调整的业务原因" />
              </Form.Item>
              <Form.Item
                name="approval_ref"
                label="审批引用（approval_ref，仅高危变更需要）"
                extra="翻译配额覆盖为中风险，通常无需审批；回滚等高危操作必须携带已记录的他人审批引用"
              >
                <Input placeholder="留空即可（中风险）" />
              </Form.Item>
              <Button type="primary" loading={saving} onClick={() => void handleSaveQuota()}>
                保存覆盖
              </Button>
            </Form>
          </>
        ) : (
          <div style={{ textAlign: 'center', padding: 32 }}>
            {configLoading ? (
              <Spin tip="加载作用域配置中..." />
            ) : (
              <Typography.Text type="secondary">
                选择作用域后加载当前覆盖配置；配置历史见下方。
              </Typography.Text>
            )}
          </div>
        )}
      </ProCard>

      <ProCard title="配置变更历史与治理" style={{ marginTop: 16 }}>
        <ProTable<API.ScopedConfigHistoryItem>
          actionRef={historyActionRef}
          rowKey={(record) => record.audit.id}
          columns={historyColumns}
          search={false}
          options={false}
          pagination={{ defaultPageSize: 20 }}
          request={async (params) => {
            const [activeScope, workspaceId] = currentScopeArgs();
            try {
              const result = await listScopedConfigHistory(
                activeScope,
                {
                  page: params.current,
                  page_size: params.pageSize,
                  action: typeof params.operation === 'string' ? params.operation : undefined,
                },
                workspaceId,
              );
              return {
                data: result?.data || [],
                total: result?.total || 0,
                success: true,
              };
            } catch (error) {
              console.error('获取配置变更历史失败:', error);
              return {
                data: [],
                total: 0,
                success: true,
              };
            }
          }}
          locale={{ emptyText: '请先在上方加载配置；当前作用域暂无变更记录' }}
          scroll={{ x: 1080 }}
        />
      </ProCard>

      <Drawer
        title={entry ? `变更详情 #${entry.audit.id} · ${operationLabelMap[entry.audit.action.split('.').pop() || ''] || entry.audit.action}` : '变更详情'}
        width={1040}
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        destroyOnClose
      >
        {entryLoading || !entry ? (
          <div style={{ textAlign: 'center', padding: 80 }}>
            <Spin size="large" tip="加载变更详情中..." />
          </div>
        ) : (
          <>
            <Descriptions
              title="治理状态"
              column={2}
              size="small"
              style={{ marginBottom: 16 }}
              items={[
                {
                  key: 'status',
                  label: '治理状态',
                  children: (
                    <Tag color={governanceColorMap[entry.governance_status] || 'default'}>
                      {governanceLabelMap[entry.governance_status] || entry.governance_status}
                    </Tag>
                  ),
                },
                {
                  key: 'risk',
                  label: '风险等级',
                  children: (
                    <Tag color={riskColorMap[entry.change_risk?.risk_level || ''] || 'default'}>
                      {riskLabelMap[entry.change_risk?.risk_level || ''] || '-'}
                    </Tag>
                  ),
                },
                {
                  key: 'approval',
                  label: '审批',
                  children: approvalStatusLabelMap[entry.approval_policy?.approval_status || ''] || '-',
                },
                {
                  key: 'verification',
                  label: '核验',
                  children: verificationStatusLabelMap[entry.verification_status] || '-',
                },
                {
                  key: 'change_ref',
                  label: '变更引用',
                  children: entry.change_control?.change_ref || '-',
                },
                {
                  key: 'reason',
                  label: '变更原因',
                  children: entry.change_control?.reason || '-',
                },
              ].map((item) => ({ ...item, span: 1 }))}
            />

            {entry.change_risk?.risk_reasons?.length ? (
              <Alert
                type={entry.change_risk.risk_level === 'high' ? 'error' : 'warning'}
                showIcon
                style={{ marginBottom: 16 }}
                message={`风险原因：${entry.change_risk.risk_reasons.join('、')}`}
                description={
                  entry.change_risk.changed_paths?.length
                    ? `变更路径：${entry.change_risk.changed_paths.join('、')}`
                    : undefined
                }
              />
            ) : null}

            <Collapse
              style={{ marginBottom: 16 }}
              defaultActiveKey={['diff']}
              items={[
                {
                  key: 'diff',
                  label: '变更对比（当前 vs 快照）',
                  children: (
                    <Table
                      size="small"
                      rowKey="path"
                      columns={diffColumns}
                      dataSource={entry.diff?.changes || []}
                      pagination={false}
                      locale={{ emptyText: '无字段级差异' }}
                    />
                  ),
                },
                {
                  key: 'verifications',
                  label: `核验记录（${entry.verifications?.length || 0}）`,
                  children: (
                    <Table
                      size="small"
                      rowKey="audit_id"
                      pagination={false}
                      locale={{ emptyText: '暂无核验记录' }}
                      dataSource={entry.verifications || []}
                      columns={[
                        {
                          title: '结论',
                          dataIndex: 'status',
                          width: 90,
                          render: (value: string) => (
                            <Tag color={verificationStatusColorMap[value] || 'default'}>
                              {verificationStatusLabelMap[value] || value}
                            </Tag>
                          ),
                        },
                        { title: '核验人', dataIndex: 'actor_user_id', width: 90 },
                        {
                          title: '备注',
                          dataIndex: 'notes',
                          ellipsis: true,
                          render: (value?: string) => value || '-',
                        },
                        {
                          title: '证据',
                          dataIndex: 'evidence',
                          render: (value?: string[]) =>
                            value && value.length ? value.join('；') : '-',
                        },
                        {
                          title: '时间',
                          dataIndex: 'created_at',
                          width: 170,
                          render: (value: string) => (value ? value.replace('T', ' ').slice(0, 19) : '-'),
                        },
                      ]}
                    />
                  ),
                },
              ]}
            />

            {templateChecks.length > 0 ? (
              <Collapse
                style={{ marginBottom: 16 }}
                items={[
                  {
                    key: 'verify',
                    label: '提交核验（须由操作者之外的用户执行）',
                    children: (
                      <Form form={verifyForm} layout="vertical">
                        <Alert
                          type="info"
                          showIcon
                          style={{ marginBottom: 12 }}
                          message="核验通过要求：全部必填检查项通过且至少附一条证据；核验未过须填写备注。"
                        />
                        <Space style={{ marginBottom: 12 }}>
                          <Form.Item
                            name="status"
                            label="核验结论"
                            style={{ marginBottom: 0 }}
                            rules={[{ required: true, message: '请选择核验结论' }]}
                          >
                            <Select
                              style={{ width: 160 }}
                              placeholder="选择结论"
                              options={[
                                { value: 'passed', label: '通过' },
                                { value: 'failed', label: '未通过' },
                              ]}
                            />
                          </Form.Item>
                        </Space>
                        {templateChecks.map((check) => (
                          <Space
                            key={check.id}
                            align="start"
                            style={{ display: 'flex', marginBottom: 8 }}
                          >
                            <Form.Item
                              name={['checks', check.id]}
                              initialValue={undefined}
                              style={{ marginBottom: 0, minWidth: 140 }}
                              rules={
                                check.required
                                  ? [{ required: true, message: '必填检查项' }]
                                  : undefined
                              }
                            >
                              <Select
                                style={{ width: 140 }}
                                placeholder={check.required ? '必填检查项' : '选填检查项'}
                                options={[
                                  { value: 'passed', label: '通过' },
                                  { value: 'failed', label: '未通过' },
                                  { value: 'skipped', label: '跳过' },
                                ]}
                              />
                            </Form.Item>
                            <Form.Item
                              name={['check_notes', check.id]}
                              style={{ marginBottom: 0, flex: 1, minWidth: 420 }}
                            >
                              <Input placeholder={`${check.title}${check.evidence_hint ? `（证据提示：${check.evidence_hint}）` : ''}`} />
                            </Form.Item>
                          </Space>
                        ))}
                        <Form.Item
                          name="evidence_text"
                          label="证据（每行一条，核验通过必填）"
                          rules={[
                            ({ getFieldValue }) => ({
                              validator(_, value) {
                                if (getFieldValue('status') !== 'passed' || splitEvidence(value).length > 0) {
                                  return Promise.resolve();
                                }
                                return Promise.reject(new Error('核验通过须至少提供一条证据'));
                              },
                            }),
                          ]}
                        >
                          <Input.TextArea rows={3} placeholder={'例如：\nQuotaGate 429 行为验证通过\n配额解析结果符合覆盖值'} />
                        </Form.Item>
                        <Form.Item
                          name="notes"
                          label="备注（核验未过必填）"
                          rules={[
                            ({ getFieldValue }) => ({
                              validator(_, value) {
                                if (getFieldValue('status') !== 'failed' || (value || '').trim()) {
                                  return Promise.resolve();
                                }
                                return Promise.reject(new Error('核验未过须填写备注'));
                              },
                            }),
                          ]}
                        >
                          <Input.TextArea rows={2} />
                        </Form.Item>
                        <Form.Item
                          name="change_ref"
                          label="变更引用"
                          rules={[{ required: true, whitespace: true, message: '变更引用必填' }]}
                        >
                          <Input />
                        </Form.Item>
                        <Form.Item
                          name="reason"
                          label="核验原因"
                          rules={[{ required: true, whitespace: true, message: '核验原因必填' }]}
                        >
                          <Input />
                        </Form.Item>
                        <Button type="primary" loading={acting} onClick={() => void submitVerify()}>
                          提交核验
                        </Button>
                      </Form>
                    ),
                  },
                ]}
              />
            ) : null}

            <Collapse
              items={[
                {
                  key: 'approve',
                  label: '记录审批（高危变更执行前，由他人审批）',
                  children: (
                    <Form form={approveForm} layout="vertical">
                      <Form.Item
                        name="change_ref"
                        label="变更引用"
                        rules={[{ required: true, whitespace: true, message: '变更引用必填' }]}
                      >
                        <Input />
                      </Form.Item>
                      <Form.Item
                        name="reason"
                        label="变更原因"
                        rules={[{ required: true, whitespace: true, message: '变更原因必填' }]}
                      >
                        <Input.TextArea rows={2} />
                      </Form.Item>
                      <Form.Item
                        name="approval_ref"
                        label="审批引用（approval_ref）"
                        rules={[{ required: true, whitespace: true, message: '审批引用必填' }]}
                      >
                        <Input placeholder="例如：APP-20261019-001" />
                      </Form.Item>
                      <Form.Item
                        name="notes"
                        label="审批意见"
                        rules={[{ required: true, whitespace: true, message: '审批意见必填' }]}
                      >
                        <Input.TextArea rows={2} />
                      </Form.Item>
                      <Form.Item name="evidence_text" label="审批证据（每行一条）">
                        <Input.TextArea rows={2} />
                      </Form.Item>
                      <Button type="primary" loading={acting} onClick={() => void submitApprove()}>
                        记录审批
                      </Button>
                    </Form>
                  ),
                },
                {
                  key: 'rollback',
                  label: '回滚到此版本（高危：需审批引用 + 已记录的他人审批）',
                  children: entry.audit.after_json ? (
                    <Form form={rollbackForm} layout="vertical">
                      <Alert
                        type="warning"
                        showIcon
                        style={{ marginBottom: 12 }}
                        message="回滚属高危变更：必须携带 approval_ref，且该审批需由与操作者不同的用户先记录（变更引用与审批引用需匹配）。"
                      />
                      <Form.Item
                        name="change_ref"
                        label="变更引用"
                        rules={[{ required: true, whitespace: true, message: '变更引用必填' }]}
                      >
                        <Input />
                      </Form.Item>
                      <Form.Item
                        name="reason"
                        label="回滚原因"
                        rules={[{ required: true, whitespace: true, message: '回滚原因必填' }]}
                      >
                        <Input.TextArea rows={2} placeholder="说明回滚触发的问题症状" />
                      </Form.Item>
                      <Form.Item
                        name="approval_ref"
                        label="审批引用（approval_ref）"
                        rules={[{ required: true, whitespace: true, message: '回滚必须携带审批引用' }]}
                      >
                        <Input />
                      </Form.Item>
                      <Popconfirm
                        title="确认回滚到该版本？"
                        description="回滚会用快照覆盖当前作用域配置，并记入变更历史。"
                        okText="确认回滚"
                        cancelText="取消"
                        okButtonProps={{ danger: true }}
                        onConfirm={() => void submitRollback()}
                      >
                        <Button danger loading={acting}>
                          回滚到此版本
                        </Button>
                      </Popconfirm>
                    </Form>
                  ) : (
                    <Typography.Text type="secondary">
                      该记录不含快照（无 after_json），无法回滚。
                    </Typography.Text>
                  ),
                },
              ]}
            />
          </>
        )}
      </Drawer>
    </div>
  );
};

export default ScopedConfigCard;
