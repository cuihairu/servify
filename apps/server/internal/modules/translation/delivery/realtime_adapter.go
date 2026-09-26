package delivery

import (
	"context"
	"strings"

	translationapp "servify/apps/server/internal/modules/translation/application"
)

// realtimeTranslateServiceAdapter 组合偏好读取与翻译门面：先查会话在装配
// 期绑定读向（viewer）的偏好（无偏好静默跳过），再走与 Phase 0 translate
// 端点同一翻译服务/同出站参数。inner 未装配（nil 门面或 nil 偏好）时统一
// 降级 ErrTranslationUnavailable，调用方静默跳过。
type realtimeTranslateServiceAdapter struct {
	translator TranslateInvoker
	prefs      SessionPreferenceReader
	viewer     string
}

// NewRealtimeTranslateService 创建自动翻译服务（viewer 绑定该实例消费的
// 偏好读向：hub 用 agent 读向、坐席发送口用 visitor 读向）；nil 安全面与
// 偏好适配器同口径。
func NewRealtimeTranslateService(translator TranslateInvoker, prefs SessionPreferenceReader, viewer string) RealtimeTranslateService {
	return &realtimeTranslateServiceAdapter{translator: translator, prefs: prefs, viewer: viewer}
}

// TranslateSessionMessage 见 RealtimeTranslateService 契约。
func (a *realtimeTranslateServiceAdapter) TranslateSessionMessage(ctx context.Context, sessionID, text string) (*SessionTranslation, error) {
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
