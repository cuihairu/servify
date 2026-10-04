import { request } from '@/lib/request';

export async function queryAI(data: { query: string; conversation_id?: string; customer_id?: number }) {
  return request<API.AIQueryResponse>('/api/v1/ai/query', { method: 'POST', data });
}

export interface AICopilotResult {
  action: string;
  session_id?: string;
  text: string;
}

/** 坐席 AI 辅助（V1.0 B1-3b，W3）：suggest_reply / rewrite / session_summary。 */
export async function aiCopilot(data: {
  action: 'suggest_reply' | 'rewrite' | 'session_summary';
  session_id?: string;
  draft?: string;
  tone?: string;
}) {
  return request<{ success: boolean; data?: AICopilotResult; error?: string }>(
    '/api/v1/ai/copilot',
    { method: 'POST', data },
  );
}

/** 知识检索建议（V1.0 B1-3b，W4）：答案带 sources 引用（title/score）。 */
export async function aiKnowledgeQuery(data: { query: string; session_id?: string }) {
  return request<{
    success: boolean;
    data?: {
      content?: string;
      confidence?: number;
      source?: string;
      strategy?: string;
      sources?: API.AIKnowledgeSource[];
    };
    error?: string;
  }>('/api/v1/ai/query', { method: 'POST', data });
}

export async function getAIStatus() {
  return request<API.AIStatus>('/api/v1/ai/status');
}

export async function getAIMetrics() {
  return request<API.AIMetrics>('/api/v1/ai/metrics');
}
