// history_adapter.go 历史消息批量标注契约（Phase 1 收尾，
// docs/realtime-translation-design.md §1.5）：读向绑定与
// RealtimeTranslateService 同构——工作台历史面是坐席读向（读访客消息的
// 译文），由装配层构造期固定。
package delivery

import (
	"context"
	"strings"
)

// HistoryTranslation 一页历史的批量译文：Texts 与输入同序等长、失败段为
// 空串（调用方按段跳过标注）；TargetLang 供调用方盖章 §4.4 的
// translation_lang 保留键。
type HistoryTranslation struct {
	Texts      []string
	TargetLang string
}

// HistoryTranslateService 历史消息批量翻译契约：把工作台一页历史里的待译
// 文本批量译成该会话绑定读向的偏好语言。会话无偏好返回 (nil, nil)（静默
// 跳过）；provider 未配置返回 ErrTranslationUnavailable。
type HistoryTranslateService interface {
	TranslateHistory(ctx context.Context, sessionID string, texts []string) (*HistoryTranslation, error)
}

// historyTranslateServiceAdapter 组合偏好读取与翻译门面（批量出站），
// nil 安全面与实时适配器同口径。
type historyTranslateServiceAdapter struct {
	translator TranslateInvoker
	prefs      SessionPreferenceReader
	viewer     string
}

// NewHistoryTranslateService 创建历史批量翻译服务（viewer 绑定该实例消费
// 的偏好读向；工作台历史面用 agent 读向）。
func NewHistoryTranslateService(translator TranslateInvoker, prefs SessionPreferenceReader, viewer string) HistoryTranslateService {
	return &historyTranslateServiceAdapter{translator: translator, prefs: prefs, viewer: viewer}
}

// TranslateHistory 见 HistoryTranslateService 契约。
func (a *historyTranslateServiceAdapter) TranslateHistory(ctx context.Context, sessionID string, texts []string) (*HistoryTranslation, error) {
	if a == nil || a.translator == nil || a.prefs == nil {
		return nil, ErrTranslationUnavailable
	}
	targetLang, err := a.prefs.GetSessionLanguage(ctx, sessionID, a.viewer)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(targetLang) == "" {
		return nil, nil
	}
	result, err := a.translator.BatchTranslate(ctx, BatchTranslateCommand{Texts: texts, TargetLang: targetLang})
	if err != nil {
		return nil, err
	}
	return &HistoryTranslation{Texts: result.Texts, TargetLang: result.TargetLang}, nil
}
