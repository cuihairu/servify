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

// TranslationPersistence 会话消息译文持久化窄契约：入参为零条时实现
// 须返回 nil（空批不触库）。
type TranslationPersistence interface {
	SaveTranslations(ctx context.Context, entries []TranslationEntry) error
}
