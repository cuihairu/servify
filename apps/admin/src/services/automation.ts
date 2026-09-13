import { request } from '@/lib/request';
import { normalizePaginatedResponse } from './_response';

const API = '/api/automations';

export async function listAutomations(params?: { page?: number; page_size?: number }) {
  const payload = await request<unknown>(API, { params });
  return normalizePaginatedResponse<API.Automation>(payload);
}

export async function createAutomation(data: Partial<API.Automation>) {
  return request<API.Automation>(API, { method: 'POST', data });
}

export async function deleteAutomation(id: number) {
  return request<API.MessageResponse>(`${API}/${id}`, { method: 'DELETE' });
}

export async function getAutomationRuns(params?: { page?: number; page_size?: number }) {
  return request<API.PaginatedResponse<API.AutomationRun>>(`${API}/runs`, { params });
}

export async function runAutomation(id: number, ticketIds: number[]) {
  // 手动运行：trigger_id 定位单个触发器，跳过事件白名单
  // （后端按触发器自身定义的事件评估条件）
  return request<{ event: string; tickets_processed: number; matches: number }>(`${API}/run`, {
    method: 'POST',
    data: { trigger_id: id, ticket_ids: ticketIds, dry_run: false },
  });
}

export async function dryRunAutomation(id: number, ticketIds: number[]) {
  return request<{ event: string; tickets_processed: number; matches: number }>(`${API}/run`, {
    method: 'POST',
    data: { trigger_id: id, ticket_ids: ticketIds, dry_run: true },
  });
}
