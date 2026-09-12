import { request } from '@/lib/request';

const API = '/api/agents/groups';

export interface AgentGroupParams {
  name?: string;
  description?: string;
  priority?: number;
  overflow_policy?: 'global' | 'none';
  enabled?: boolean;
  member_ids?: number[];
  parent_id?: number | null;
}

/** 坐席组列表（后端返回 {groups,total} 风格） */
export async function listAgentGroups() {
  return request<API.AgentGroupListResponse>(API);
}

export async function getAgentGroup(id: number) {
  return request<API.AgentGroup>(`${API}/${id}`);
}

export async function createAgentGroup(body: AgentGroupParams) {
  return request<API.AgentGroup>(API, { method: 'POST', data: body });
}

/** 部分更新：只传需要覆盖的字段（enabled=false 显式停用组） */
export async function updateAgentGroup(id: number, body: AgentGroupParams) {
  return request<API.AgentGroup>(`${API}/${id}`, { method: 'PUT', data: body });
}

export async function deleteAgentGroup(id: number) {
  return request<{ message: string }>(`${API}/${id}`, { method: 'DELETE' });
}

export async function listGroupMembers(id: number) {
  return request<{ member_ids: number[]; total: number }>(`${API}/${id}/members`);
}

/** 全量替换组成员（增量请先 listGroupMembers 合并） */
export async function replaceGroupMembers(id: number, memberIds: number[]) {
  return request<{ message: string }>(`${API}/${id}/members`, {
    method: 'PUT',
    data: { member_ids: memberIds },
  });
}
