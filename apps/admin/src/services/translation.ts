import { request } from '@/lib/request';

// 会话翻译语言偏好（Phase 1 刀一存储面 + 刀三双面，docs/realtime-translation-design.md）。
// 读向（viewer 角色）由服务端按认证主体推导：管理端主体落 agent 读向，因此
// 请求体不携带角色，避免越权改写访客读向。与 translate 端点同款单一注册点。
// 未设置时服务端返回 200 且 target_lang 为空串（不是 404）。

export async function getTranslationPreference(sessionId: string) {
  return request<API.DataResponse<API.TranslationPreference>>(
    `/api/v1/translation/preferences/${encodeURIComponent(sessionId)}`,
  );
}

export async function setTranslationPreference(sessionId: string, targetLang: string) {
  return request<API.DataResponse<API.TranslationPreference>>(
    `/api/v1/translation/preferences/${encodeURIComponent(sessionId)}`,
    { method: 'PUT', data: { target_lang: targetLang } },
  );
}

export async function clearTranslationPreference(sessionId: string) {
  return request<API.MessageResponse>(
    `/api/v1/translation/preferences/${encodeURIComponent(sessionId)}`,
    { method: 'DELETE' },
  );
}
