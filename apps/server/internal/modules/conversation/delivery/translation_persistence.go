// translation_persistence.go 历史译文持久化窄契约（docs/realtime-translation-
// design.md §1.5「存储列与跨页缓存留后续刀」的收口面）：工作台历史标注把
// 批量译文按语言键写入 messages.metadata，重翻页命中缓存即免 LLM 调用。
// handlers 以窄接口消费，conversation app service 结构化满足（零适配）。
package delivery

import (
	"context"

	"servify/apps/server/internal/modules/conversation/application"
)

// TranslationEntry = application.MessageTranslationEntry 的 delivery 面别名
// （delivery 契约包不定义同形结构，handlers 只引 delivery 面类型）。
type TranslationEntry = application.MessageTranslationEntry

// TranslationCacheKey = application.MessageTranslationsKey 的 delivery 面别名
// （messages.metadata 里的保留键，值为按语言键的 JSON 串）。
const TranslationCacheKey = application.MessageTranslationsKey

// DecodeTranslationCache 解析 messages.metadata 保留键 translations 的按语言
// 译文表；空串/坏 JSON 一律返回 nil（译文是增强能力，静默降级）。
func DecodeTranslationCache(raw string) map[string]string {
	return application.DecodeMessageTranslations(raw)
}

// TranslationPersistence 会话消息译文持久化窄契约：入参为零条时实现
// 须返回 nil（空批不触库）。
type TranslationPersistence interface {
	SaveTranslations(ctx context.Context, entries []TranslationEntry) error
}
