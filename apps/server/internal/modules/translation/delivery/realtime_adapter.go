package delivery

import (
	"context"
	"strings"

	translationapp "servify/apps/server/internal/modules/translation/application"
)

// realtimeTranslateServiceAdapter 组合偏好读取与翻译门面：先查会话偏好
// （无偏好静默跳过），再走与 Phase 0 translate 端点同一翻译服务/同出站
// 参数。inner 未装配（nil 门面或 nil 偏好）时统一降级
// ErrTranslationUnavailable，hub 侧静默跳过。
type realtimeTranslateServiceAdapter struct {
	translator TranslateInvoker
	prefs      SessionPreferenceReader
}

// NewRealtimeTranslateService 创建 hub 自动翻译服务；nil 安全面与偏好
// 适配器同口径。
func NewRealtimeTranslateService(translator TranslateInvoker, prefs SessionPreferenceReader) RealtimeTranslateService {
	return &realtimeTranslateServiceAdapter{translator: translator, prefs: prefs}
}

// TranslateSessionMessage 见 RealtimeTranslateService 契约。
func (a *realtimeTranslateServiceAdapter) TranslateSessionMessage(ctx context.Context, sessionID, text string) (*SessionTranslation, error) {
	if a == nil || a.translator == nil || a.prefs == nil {
		return nil, ErrTranslationUnavailable
	}
	targetLang, err := a.prefs.GetSessionLanguage(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(targetLang) == "" {
		return nil, nil
	}
	result, err := a.translator.Translate(ctx, translationapp.TranslateCommand{
		Text:       text,
		TargetLang: targetLang,
	})
	if err != nil {
		return nil, err
	}
	return &SessionTranslation{
		Original:   text,
		Content:    result.Text,
		SourceLang: result.SourceLang,
		TargetLang: result.TargetLang,
	}, nil
}
