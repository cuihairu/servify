package delivery

import (
	"context"
)

// SessionTranslation 单条会话消息的译文载荷（message-translated 帧的
// Data，docs/realtime-translation-design.md §1.4）：译文与原文并存，
// 客户端按 original 关联（落库旁路没有消息 ID 可挂）。
type SessionTranslation struct {
	Original   string `json:"original"`
	Content    string `json:"content"`
	SourceLang string `json:"source_lang"`
	TargetLang string `json:"target_lang"`
}

// SessionPreferenceReader 会话语言偏好读取面（PreferenceService 同形，
// 结构化满足零适配）；viewer 由装配期绑定的 RealtimeTranslateService 实现
// 固定（hub 消费 agent 读向、坐席发送口消费 visitor 读向）。
type SessionPreferenceReader interface {
	GetSessionLanguage(ctx context.Context, sessionID, viewer string) (string, error)
}

// TranslateInvoker 翻译出站面（HandlerService 同形：单条 + 批量，模块门面
// 直接满足，装配层零适配注入）。
type TranslateInvoker interface {
	Translate(ctx context.Context, cmd TranslateCommand) (TranslateResult, error)
	BatchTranslate(ctx context.Context, cmd BatchTranslateCommand) (BatchTranslateResult, error)
}

// RealtimeTranslateService 会话消息自动翻译契约（Phase 1 刀二，hub 消费）：
// 按会话偏好翻译单条已落库文本。会话无偏好返回 (nil, nil)（静默跳过，
// 不是错误）；provider 未配置返回 ErrTranslationUnavailable（同样静默，
// 避免未部署 AI 的部署每条消息刷警告）；其余错误原样上抛由调用方记 Warn。
type RealtimeTranslateService interface {
	TranslateSessionMessage(ctx context.Context, sessionID, text string) (*SessionTranslation, error)
}
