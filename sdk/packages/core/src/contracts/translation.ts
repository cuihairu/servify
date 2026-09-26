// 聊天文本翻译契约（Phase 0.5，docs/realtime-translation-design.md）：
// REST 端点 POST /api/v1/translation/translate 的便捷调用与消息 metadata
// 译文保留键约定。请求/响应类型见 types.ts（TranslationResult）。

// 消息 metadata 保留键：translation=译文文本、translation_lang=译文语言
// 标签（BCP-47 子集，如 en/zh-CN）。Phase 1 起由服务端在自动翻译后盖章，
// 客户端只读不写（避免伪造译文）；未知 metadata 键一律忽略的既有边界
// 语义不受影响。
export const TRANSLATION_METADATA_KEYS = {
  text: 'translation',
  lang: 'translation_lang',
} as const;

// 从消息 metadata 读取译文：无译文键返回 null（渲染方回退原文），
// 只有译文键没有语言键时 lang 为空串（语言未知仍可展示）。
export function readMessageTranslation(
  metadata?: Record<string, string> | null,
): { text: string; lang: string } | null {
  if (!metadata) {
    return null;
  }
  const text = metadata[TRANSLATION_METADATA_KEYS.text];
  if (!text) {
    return null;
  }
  return { text, lang: metadata[TRANSLATION_METADATA_KEYS.lang] ?? '' };
}
