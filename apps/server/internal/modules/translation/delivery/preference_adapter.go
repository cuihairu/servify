package delivery

import (
	"context"

	translationapp "servify/apps/server/internal/modules/translation/application"
)

// preferenceHandlerServiceAdapter 把偏好应用服务适配为 handler 契约
// （与 Translate 面的 HandlerServiceAdapter 同构：nil 安全——未装配时
// 返回 ErrTranslationUnavailable，端点不注册由装配层保证）。
type preferenceHandlerServiceAdapter struct {
	inner *translationapp.PreferenceService
}

// NewPreferenceHandlerService 构造偏好交付适配器。
func NewPreferenceHandlerService(inner *translationapp.PreferenceService) PreferenceHandlerService {
	return &preferenceHandlerServiceAdapter{inner: inner}
}

func (a *preferenceHandlerServiceAdapter) SetSessionLanguage(ctx context.Context, sessionID, targetLang string) (string, error) {
	if a == nil || a.inner == nil {
		return "", translationapp.ErrTranslationUnavailable
	}
	return a.inner.SetSessionLanguage(ctx, sessionID, targetLang)
}

func (a *preferenceHandlerServiceAdapter) GetSessionLanguage(ctx context.Context, sessionID string) (string, error) {
	if a == nil || a.inner == nil {
		return "", translationapp.ErrTranslationUnavailable
	}
	return a.inner.GetSessionLanguage(ctx, sessionID)
}

func (a *preferenceHandlerServiceAdapter) ClearSessionLanguage(ctx context.Context, sessionID string) error {
	if a == nil || a.inner == nil {
		return translationapp.ErrTranslationUnavailable
	}
	return a.inner.ClearSessionLanguage(ctx, sessionID)
}
