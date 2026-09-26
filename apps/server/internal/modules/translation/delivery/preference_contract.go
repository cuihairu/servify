// Package delivery 翻译模块的交付层契约：handlers 只 import 本包，
// 不得触及 application/infra（模块边界检查口径与 assist/translation
// 既有刀一致）。
package delivery

import (
	"context"

	translationapp "servify/apps/server/internal/modules/translation/application"
)

// viewer 角色常量再导出（handler 侧按认证主体推导后传入，不接受请求方
// 自报；取值口径见 domain/application）。
const (
	ViewerRoleAgent   = translationapp.ViewerRoleAgent
	ViewerRoleVisitor = translationapp.ViewerRoleVisitor
)

// PreferenceHandlerService 会话语言偏好服务契约（Phase 1 刀一存储面 +
// 刀三角色维度）：按 (sessionID, viewer) 读向设置/查询/清除翻译目标语言。
// 未设置以空串表达，不是错误。viewer 由服务端按认证主体推导（agent 主体
// → agent 读向；访客主体 → visitor 读向 + 会话绑定校验），不接受请求方
// 自报。
type PreferenceHandlerService interface {
	SetSessionLanguage(ctx context.Context, sessionID, viewer, targetLang string) (string, error)
	GetSessionLanguage(ctx context.Context, sessionID, viewer string) (string, error)
	ClearSessionLanguage(ctx context.Context, sessionID, viewer string) error
}

// 偏好面错误再导出（handlers 经 delivery 引用，不 import application）。
var (
	ErrTranslationSessionRequired = translationapp.ErrTranslationSessionRequired
	ErrTranslationPrefConflict    = translationapp.ErrTranslationPrefConflict
	ErrTranslationViewerInvalid   = translationapp.ErrTranslationViewerInvalid
)
