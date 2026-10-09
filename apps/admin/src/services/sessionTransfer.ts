import { request } from '@/lib/request';
import { normalizeCountedListResponse } from './_response';

export async function getWaitingQueue() {
  return request<{ count: number; data: API.TransferQueueRecord[] }>('/api/session-transfer/waiting');
}

export async function getTransferHistory(sessionId?: string, limit = 50) {
  const normalizedSessionId = sessionId?.trim();
  const payload = normalizedSessionId
    ? await request<unknown>(`/api/session-transfer/history/${normalizedSessionId}`)
    : await request<unknown>('/api/session-transfer/history', {
        params: { limit },
      });
  return normalizeCountedListResponse<API.SessionTransferRecord>(payload);
}

export async function transferToAgent(data: { session_id: string; agent_id: number; reason?: string }) {
  return request<API.SessionTransferResult>('/api/session-transfer/to-agent', {
    method: 'POST',
    data: {
      session_id: data.session_id,
      target_agent_id: data.agent_id,
      reason: data.reason,
    },
  });
}

export async function cancelTransfer(sessionId: string) {
  return request<API.MessageResponse<{ session_id: string }>>('/api/session-transfer/cancel', {
    method: 'POST',
    data: { session_id: sessionId },
  });
}

export async function processQueue() {
  return request<API.MessageResponse>('/api/session-transfer/process-queue', { method: 'POST' });
}

export async function getRoutingScoring(sessionId: string, limit = 50) {
  const normalizedSessionId = sessionId.trim();
  if (!normalizedSessionId) {
    return { count: 0, data: [] as API.RoutingAssignmentScore[] };
  }
  const payload = await request<{ count?: number; data?: API.RoutingAssignmentScore[] }>(
    `/api/session-transfer/scoring/${normalizedSessionId}`,
    { params: { limit } },
  );
  return { count: payload.count ?? 0, data: payload.data ?? [] };
}
