import { request } from '@/lib/request';

export async function getConversation(sessionId: string) {
  return request<API.DataResponse<API.Conversation>>(`/api/omni/sessions/${sessionId}`);
}

export async function getConversationMessages(
  sessionId: string,
  params?: { limit?: number; before?: string },
) {
  const query = new URLSearchParams();
  if (params?.limit) query.set('limit', String(params.limit));
  if (params?.before) query.set('before', params.before);
  const qs = query.toString();
  return request<API.ListResponse<API.ConversationMessage>>(
    `/api/omni/sessions/${sessionId}/messages${qs ? `?${qs}` : ''}`,
  );
}

export async function sendConversationMessage(sessionId: string, data: { content: string }) {
  return request<API.MessageResponse<API.ConversationMessage>>(`/api/omni/sessions/${sessionId}/messages`, {
    method: 'POST',
    data,
  });
}

/** 服务过程时间线（conversation_events 投影，旧→新时序，V1.0 B1-2）。 */
export async function getConversationTimeline(sessionId: string, limit = 50) {
  return request<API.ConversationTimelineResponse>(
    `/api/omni/sessions/${sessionId}/timeline?limit=${limit}`,
  );
}

export async function assignAgent(sessionId: string, data: { agent_id: number }) {
  return request<API.MessageResponse<API.Conversation>>(`/api/omni/sessions/${sessionId}/assign`, {
    method: 'POST',
    data,
  });
}

export async function transferConversation(sessionId: string, data: { to_agent_id: number }) {
  return request<API.MessageResponse<API.Conversation>>(`/api/omni/sessions/${sessionId}/transfer`, {
    method: 'POST',
    data,
  });
}

export async function closeConversation(sessionId: string) {
  return request<API.MessageResponse<API.Conversation>>(`/api/omni/sessions/${sessionId}/close`, {
    method: 'POST',
  });
}
