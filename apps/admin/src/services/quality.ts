import { request } from '@/lib/request';

const API = '/api/quality';

export interface ListQualityReviewsParams {
  page?: number;
  page_size?: number;
  status?: string;
  agent_id?: number;
  customer_id?: number;
  has_violations?: boolean;
  severity?: string;
  min_score?: number;
  max_score?: number;
  from?: string;
  to?: string;
}

/** 质检记录列表（后端返回 {items,total,page,page_size} 风格） */
export async function listQualityReviews(params?: ListQualityReviewsParams) {
  return request<API.QualityReviewListResponse>(`${API}/reviews`, { params });
}

export async function getQualityReview(sessionId: string) {
  return request<API.QualityReview>(`${API}/reviews/${sessionId}`);
}

export interface ConfirmQualityReviewBody {
  manual_score?: number;
  manual_result?: 'pass' | 'violation';
  review_note?: string;
}

export async function confirmQualityReview(sessionId: string, body: ConfirmQualityReviewBody) {
  return request<{ message: string }>(`${API}/reviews/${sessionId}/confirm`, {
    method: 'POST',
    data: body,
  });
}

export async function rescoreQualityReview(sessionId: string, force = false) {
  return request<{ message: string }>(`${API}/reviews/${sessionId}/rescore`, {
    method: 'POST',
    params: force ? { force: 'true' } : undefined,
  });
}

export async function getScorerStatus() {
  return request<{ llm_enabled: boolean }>(`${API}/scorer`);
}
