import { request } from '@/lib/request';

const API = '/api/knowledge-docs';

export async function listDocs(params: {
  page?: number;
  page_size?: number;
  search?: string;
  category?: string;
}) {
  return request<API.PaginatedResponse<API.KnowledgeDoc>>(API, { params });
}

export async function getDoc(id: number) {
  return request<API.KnowledgeDoc>(`${API}/${id}`);
}

export async function createDoc(data: {
  title: string;
  content: string;
  category?: string;
  tags?: string[];
  is_public?: boolean;
  source_id?: number;
}) {
  return request<API.KnowledgeDoc>(API, { method: 'POST', data });
}

export async function updateDoc(id: number, data: {
  title?: string;
  content?: string;
  category?: string;
  tags?: string[];
  is_public?: boolean;
  source_id?: number;
}) {
  return request<API.KnowledgeDoc>(`${API}/${id}`, { method: 'PUT', data });
}

export async function deleteDoc(id: number) {
  return request<API.MessageResponse>(`${API}/${id}`, { method: 'DELETE' });
}

// ---- 来源登记与索引任务（V1.0 收敛 B3-1a/B3-2，§8.1/§8.2）----

const SOURCE_TYPES = ['markdown', 'website', 'pdf', 'faq', 'api'];

export async function listSources(type?: string) {
  return request<API.KnowledgeSource[]>(`${API}/sources`, {
    params: type ? { type } : undefined,
  });
}

export async function createSource(data: {
  name: string;
  type: string;
  description?: string;
}) {
  return request<API.KnowledgeSource>(`${API}/sources`, { method: 'POST', data });
}

export async function deleteSource(id: number) {
  return request<API.MessageResponse>(`${API}/sources/${id}`, { method: 'DELETE' });
}

export async function listIndexJobs(docId: number, limit = 20) {
  return request<API.KnowledgeIndexJob[]>(`${API}/${docId}/index-jobs`, {
    params: { limit },
  });
}

/** 重建索引：排队并同步执行一次（任务落执行时文档版本） */
export async function indexDocument(docId: number) {
  return request<API.KnowledgeIndexJobResult>(`${API}/${docId}/index-jobs`, {
    method: 'POST',
  });
}

export async function retryIndexJob(jobId: string) {
  return request<API.KnowledgeIndexJobResult>(`${API}/index-jobs/${jobId}/retry`, {
    method: 'POST',
  });
}

// ---- 检索分析（V1.0 收敛 B3-1b/B3-2，§8.3）----

export async function getRetrievalAnalytics(params?: { days?: number; limit?: number }) {
  return request<API.KnowledgeRetrievalAnalytics>('/api/v1/ai/retrieval-analytics', {
    params,
  });
}

export { SOURCE_TYPES };
