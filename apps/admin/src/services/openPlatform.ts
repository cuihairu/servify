import { request } from '@/lib/request';
import { normalizePaginatedResponse } from './_response';

const WEBHOOKS_API = '/api/webhooks';
const API_KEYS = '/api/api-keys';

// ---------- Webhook 订阅 ----------

export async function listWebhooks() {
  return request<API.WebhookEndpoint[]>(WEBHOOKS_API);
}

/** 创建后 secret 明文仅本次响应返回 */
export async function createWebhook(data: Partial<API.WebhookEndpoint>) {
  return request<{ endpoint: API.WebhookEndpoint; secret: string }>(WEBHOOKS_API, {
    method: 'POST',
    data,
  });
}

export async function updateWebhook(id: number | string, data: Partial<API.WebhookEndpoint>) {
  return request<API.WebhookEndpoint>(`${WEBHOOKS_API}/${id}`, { method: 'PUT', data });
}

export async function deleteWebhook(id: number | string) {
  return request<API.MessageResponse>(`${WEBHOOKS_API}/${id}`, { method: 'DELETE' });
}

/** 轮换签名密钥，明文仅本次响应返回 */
export async function rotateWebhookSecret(id: number | string) {
  return request<{ endpoint: API.WebhookEndpoint; secret: string }>(`${WEBHOOKS_API}/${id}/secret`, {
    method: 'POST',
  });
}

/** 同步发送一条 ping 测试事件（返回本次投递记录） */
export async function testWebhook(id: number | string) {
  return request<API.WebhookDelivery>(`${WEBHOOKS_API}/${id}/test`, { method: 'POST' });
}

export async function listWebhookEvents() {
  const payload = await request<{ events?: string[] }>(`${WEBHOOKS_API}/events`);
  return payload?.events ?? [];
}

export async function listWebhookDeliveries(params?: {
  endpoint_id?: number;
  status?: string;
  page?: number;
  page_size?: number;
}) {
  const payload = await request<unknown>(`${WEBHOOKS_API}/deliveries`, { params });
  return normalizePaginatedResponse<API.WebhookDelivery>(payload, ['items', 'data']);
}

export async function redeliverWebhookDelivery(id: number | string) {
  return request<API.WebhookDelivery>(`${WEBHOOKS_API}/deliveries/${id}/redeliver`, {
    method: 'POST',
  });
}

// ---------- API Keys ----------

export async function listAPIKeys() {
  return request<API.APIKey[]>(API_KEYS);
}

/** 签发后明文仅本次响应返回 */
export async function createAPIKey(data: {
  name: string;
  workspace_id?: string;
  scopes?: string;
  expires_at?: string;
}) {
  return request<{ api_key: API.APIKey; plaintext: string }>(API_KEYS, {
    method: 'POST',
    data,
  });
}

export async function revokeAPIKey(id: number | string) {
  return request<API.APIKey>(`${API_KEYS}/${id}/revoke`, { method: 'POST' });
}

export async function deleteAPIKey(id: number | string) {
  return request<API.MessageResponse>(`${API_KEYS}/${id}`, { method: 'DELETE' });
}
