import { request } from '@/lib/request';

type SeriesPoint = {
  date: string;
  tickets?: number;
  sessions?: number;
  messages?: number;
};

type NamedValue = {
  name: string;
  value: number;
};

type AgentPerformance = {
  agent_id: number;
  agent_name: string;
  resolved_tickets: number;
  avg_first_response_time: number;
  avg_resolution_time: number;
  satisfaction_score?: number;
};

/** 仪表板统计 */
export async function getDashboardStats() {
  return request<API.DashboardStats>('/api/statistics/dashboard');
}

/** 时间范围统计 */
export async function getTimeRangeStats(params: {
  start_date: string;
  end_date: string;
}) {
  return request<SeriesPoint[]>('/api/statistics/time-range', { params });
}

/** 工单分类统计 */
export async function getTicketCategoryStats(params?: {
  start_date?: string;
  end_date?: string;
}) {
  return request<NamedValue[]>('/api/statistics/ticket-category', { params });
}

/** 工单优先级统计 */
export async function getTicketPriorityStats(params?: {
  start_date?: string;
  end_date?: string;
}) {
  return request<NamedValue[]>('/api/statistics/ticket-priority', { params });
}

/** 客服绩效 */
export async function getAgentPerformanceStats(params: {
  start_date: string;
  end_date: string;
  limit?: number;
}) {
  return request<AgentPerformance[]>('/api/statistics/agent-performance', { params });
}

/** 客户来源分布 */
export async function getCustomerSourceStats() {
  return request<NamedValue[]>('/api/statistics/customer-source');
}

/** 远程协助工单统计 */
export async function getRemoteAssistTicketStats() {
  return request<API.RemoteAssistTicketStats>('/api/statistics/remote-assist-tickets');
}

/** 统计报表导出类型（与后端 /statistics/export 的 type 取值一致） */
export type StatisticsExportType =
  | 'time_range'
  | 'agent_performance'
  | 'ticket_category'
  | 'ticket_priority'
  | 'customer_source'
  | 'satisfaction';

/** 导出统计报表（CSV / Excel 二进制） */
export async function exportStatistics(params: {
  type: StatisticsExportType;
  format: 'csv' | 'xlsx';
  from: string;
  to: string;
}) {
  return request<Blob>('/api/statistics/export', {
    method: 'GET',
    params: { type: params.type, format: params.format, from: params.from, to: params.to },
    responseType: 'blob',
  });
}
