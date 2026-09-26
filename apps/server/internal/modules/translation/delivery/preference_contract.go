// Package delivery 翻译模块的交付层契约：handlers 只 import 本包，
// 不得触及 application/infra（模块边界检查口径与 assist/translation
// 既有刀一致）。
package delivery

import (
	"context"

	translationapp "servify/apps/server/internal/modules/translation/application"
)

// 会话语言偏好服务契约（Phase 1 刀一）：坐席为客服会话设置/查询/清除
// 翻译目标语言。未设置以空串表达，不是错误。
type PreferenceHandlerService interface {
	SetSessionLanguage(ctx context.Context, sessionID, targetLang string) (string, error)
	GetSessionLanguage(ctx context.Context, sessionID string) (string, error)
	ClearSessionLanguage(ctx context.Context, sessionID string) error
}

// 偏好面错误再导出（handlers 经 delivery 引用，不 import application）。
var (
	ErrTranslationSessionRequired = translationapp.ErrTranslationSessionRequired
	ErrTranslationPrefConflict    = translationapp.ErrTranslationPrefConflict
)
