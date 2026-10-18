import { request } from '@/lib/request';

const API = '/api/security/config';

export type ScopedConfigScope = 'tenant' | 'workspace';

export interface ScopedConfigReadResponse {
  tenant_id: string;
  workspace_id?: string;
  config: API.ScopedConfigDocument;
}

export async function getScopedConfig(scope: ScopedConfigScope, workspaceId?: string) {
  if (scope === 'workspace') {
    return request<ScopedConfigReadResponse>(`${API}/workspace`, {
      params: { workspace_id: workspaceId },
    });
  }
  return request<ScopedConfigReadResponse>(`${API}/tenant`);
}

/** 写请求体：文档各分节顶层展开（只写非空分节，其余分节保持现值），另带变更控制三字段 */
export interface ScopedConfigWritePayload extends API.ScopedConfigDocument {
  change_ref: string;
  reason: string;
  approval_ref?: string;
}

export async function putScopedConfig(
  scope: ScopedConfigScope,
  payload: ScopedConfigWritePayload,
  workspaceId?: string,
) {
  const path = scope === 'workspace' ? `${API}/workspace` : `${API}/tenant`;
  return request<API.ScopedConfigGovernedWriteResponse>(path, {
    method: 'PUT',
    data: payload,
    params: scope === 'workspace' ? { workspace_id: workspaceId } : undefined,
  });
}

export async function listScopedConfigHistory(
  scope: ScopedConfigScope,
  params?: { page?: number; page_size?: number; action?: string },
  workspaceId?: string,
) {
  const path = scope === 'workspace' ? `${API}/workspace/history` : `${API}/tenant/history`;
  return request<API.ScopedConfigHistoryResponse>(path, {
    params: {
      ...(scope === 'workspace' ? { workspace_id: workspaceId } : {}),
      ...(params || {}),
    },
  });
}

export async function getScopedConfigHistoryEntry(
  scope: ScopedConfigScope,
  auditId: number,
  workspaceId?: string,
) {
  const path = scope === 'workspace' ? `${API}/workspace/history` : `${API}/tenant/history`;
  return request<API.ScopedConfigHistoryEntry>(`${path}/${auditId}`, {
    params: scope === 'workspace' ? { workspace_id: workspaceId } : undefined,
  });
}

export interface ScopedConfigRollbackPayload {
  confirm: boolean;
  change_ref: string;
  reason: string;
  approval_ref?: string;
}

export async function rollbackScopedConfig(
  scope: ScopedConfigScope,
  auditId: number,
  payload: ScopedConfigRollbackPayload,
  workspaceId?: string,
) {
  const path = scope === 'workspace' ? `${API}/workspace/rollback` : `${API}/tenant/rollback`;
  return request<API.ScopedConfigGovernedWriteResponse>(`${path}/${auditId}`, {
    method: 'POST',
    data: payload,
    params: {
      confirm: 'true',
      ...(scope === 'workspace' ? { workspace_id: workspaceId } : {}),
    },
  });
}

export interface ScopedConfigApprovePayload {
  change_ref: string;
  reason: string;
  approval_ref: string;
  notes: string;
  evidence?: string[];
}

export async function approveScopedConfig(
  scope: ScopedConfigScope,
  payload: ScopedConfigApprovePayload,
  workspaceId?: string,
) {
  const path = scope === 'workspace' ? `${API}/workspace/approve` : `${API}/tenant/approve`;
  return request<API.ScopedConfigApprovalResponse>(path, {
    method: 'POST',
    data: payload,
    params: scope === 'workspace' ? { workspace_id: workspaceId } : undefined,
  });
}

export interface ScopedConfigVerifyPayload {
  status: 'passed' | 'failed';
  notes?: string;
  evidence?: string[];
  checks?: API.ScopedConfigVerificationCheckResult[];
  change_ref: string;
  reason: string;
}

export async function verifyScopedConfig(
  scope: ScopedConfigScope,
  auditId: number,
  payload: ScopedConfigVerifyPayload,
  workspaceId?: string,
) {
  const path = scope === 'workspace' ? `${API}/workspace/verify` : `${API}/tenant/verify`;
  return request<Record<string, unknown>>(`${path}/${auditId}`, {
    method: 'POST',
    data: payload,
    params: scope === 'workspace' ? { workspace_id: workspaceId } : undefined,
  });
}
