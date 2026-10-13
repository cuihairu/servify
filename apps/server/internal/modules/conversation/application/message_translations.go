// message_translations.go 历史译文持久化的 metadata 保留键契约
// （docs/realtime-translation-design.md §1.5「存储列与跨页缓存留后续刀」的
// 收口面）：messages.metadata 为 JSON 字符串键值列，保留键 translations
// 存「按语言键 → 译文」的 JSON 串。偏好随时可改不构成语言陈旧——每个
// 语言键写入后即不可变（源内容发后不可改），切语言只是查不到对应键、
// 现译现写，旧键不动。infra 落库与 handlers 历史标注共用本编解码。
package application

import "encoding/json"

// MessageTranslationsKey messages.metadata 中的保留键：值为 JSON 串，
// 形如 {"zh":"你好","en":"hello"}（语言键 → 译文）。
const MessageTranslationsKey = "translations"

// DecodeMessageTranslations 解析保留键的按语言译文表；空串/坏 JSON/
// 非 JSON 对象一律返回 nil（译文是增强能力，静默降级）。
func DecodeMessageTranslations(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	decoded := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}
	if len(decoded) == 0 {
		return nil
	}
	return decoded
}

// MergeMessageTranslations 把 (lang, text) 合入既有 metadata 键值表：
// 同语言键已存在时保留旧值（写入方只上送未命中缓存的段，同键即并发
// 标注竞态，先到先得）；返回新表不改入参。
func MergeMessageTranslations(base map[string]string, lang, text string) map[string]string {
	merged := map[string]string{}
	if raw := base[MessageTranslationsKey]; raw != "" {
		_ = json.Unmarshal([]byte(raw), &merged)
	}
	if _, ok := merged[lang]; !ok {
		merged[lang] = text
	}
	out := make(map[string]string, len(base)+1)
	for k, v := range base {
		if k != MessageTranslationsKey {
			out[k] = v
		}
	}
	if encoded, err := json.Marshal(merged); err == nil {
		out[MessageTranslationsKey] = string(encoded)
	}
	return out
}
