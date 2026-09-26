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

/** 服务端下发的单条 ICE 服务器条目（与 WS webrtc-ice-config 帧同形；
 * TURN 条目带 username/credential/ttl，ttl 为凭据剩余秒数） */
export interface RTCIceServerEntry {
  urls: string | string[];
  username?: string;
  credential?: string;
  ttl?: number;
}

/** 服务端装配的 ICE 配置（GET /api/v1/rtc/ice-servers，docs/TURN_DEPLOYMENT.md
 * 切片三）：坐席端建 PC 前取用（RA-6），替代硬编码公网 STUN——严格网络下由
 * 服务端配置的 TURN 兜住建连。网关未装配 503 / 空配置空列表，调用方自行兜底 */
export async function getIceServers() {
  return request<{ ice_servers: RTCIceServerEntry[] }>('/api/v1/rtc/ice-servers');
}
