import { request } from '@/lib/request';

const API = '/api/remote-assist';

export interface StartAssistParams {
  conversation_session_id: string;
  agent_user_id?: number;
}

export interface AnnotationPayload {
  timestamp_ms: number;
  shape: 'rect' | 'freehand' | 'arrow';
  payload: Record<string, unknown>;
}

/** 发起协助：先建记录再走 WS/RTC 信令，返回的 id 经 DataChannel 下发给访客 */
export async function startAssistSession(body: StartAssistParams) {
  return request<API.RemoteAssistSession>(`${API}/sessions`, { method: 'POST', data: body });
}

/** 结束协助（访客侧录制经 /api/v1/upload 上传后自动回写录制元数据） */
export async function endAssistSession(id: number, outcome: 'ended' | 'failed' = 'ended') {
  return request<API.RemoteAssistSession>(`${API}/sessions/${id}/end`, {
    method: 'POST',
    data: { outcome },
  });
}

/** 按会话查协助记录（新的在前） */
export async function listAssistSessions(conversationSessionId: string, limit = 10) {
  return request<API.RemoteAssistSession[]>(`${API}/sessions`, {
    params: { conversation_session_id: conversationSessionId, limit },
  });
}

export async function getAssistSession(id: number) {
  return request<API.RemoteAssistSession>(`${API}/sessions/${id}`);
}

export async function listAnnotations(assistSessionId: number) {
  return request<API.RemoteAssistAnnotation[]>(
    `${API}/sessions/${assistSessionId}/annotations`,
  );
}

export async function addAnnotation(assistSessionId: number, body: AnnotationPayload) {
  return request<API.RemoteAssistAnnotation>(`${API}/sessions/${assistSessionId}/annotations`, {
    method: 'POST',
    data: body,
  });
}

export async function deleteAnnotation(id: number) {
  return request<{ message: string }>(`${API}/annotations/${id}`, { method: 'DELETE' });
}
