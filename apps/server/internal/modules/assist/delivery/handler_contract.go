package delivery

import (
	"context"
	assistdomain "servify/apps/server/internal/modules/assist/domain"

	assistapp "servify/apps/server/internal/modules/assist/application"
)

// 远程协助 handler 契约：handlers 包经此别名/接口使用 assist application，
// 不直接 import application（见 module-boundaries.rules）。

type (
	StartCommand      = assistapp.StartCommand
	EndCommand        = assistapp.EndCommand
	AnnotationCommand = assistapp.AnnotationCommand
	RecordingMeta     = assistapp.RecordingMeta
)

// 错误转发（handler 侧映射 HTTP 状态用）。
var (
	ErrAssistNotFound           = assistapp.ErrAssistNotFound
	ErrAssistAnnotationNotFound = assistapp.ErrAssistAnnotationNotFound
	ErrAssistSessionRequired    = assistapp.ErrAssistSessionRequired
	ErrAssistShapeInvalid       = assistapp.ErrAssistShapeInvalid
	ErrAssistPayloadInvalid     = assistapp.ErrAssistPayloadInvalid
	ErrAssistForbidden          = assistapp.ErrAssistForbidden
	ErrAssistAlreadyEnded       = assistapp.ErrAssistAlreadyEnded
	ErrAssistSessionActive      = assistapp.ErrAssistSessionActive
	ErrAssistConsentDeclined    = assistapp.ErrAssistConsentDeclined
	ErrAssistConsentDecided     = assistapp.ErrAssistConsentDecided
)

// HandlerService 远程协助管理/访客两面的服务契约。
type HandlerService interface {
	StartSession(ctx context.Context, cmd StartCommand) (*assistdomain.RemoteAssistSession, error)
	EndSession(ctx context.Context, id uint, cmd EndCommand) (*assistdomain.RemoteAssistSession, error)
	GetSession(ctx context.Context, id uint) (*assistdomain.RemoteAssistSession, error)
	ListSessions(ctx context.Context, conversationSessionID string, limit int) ([]assistdomain.RemoteAssistSession, error)
	AddAnnotation(ctx context.Context, assistSessionID uint, cmd AnnotationCommand) (*assistdomain.RemoteAssistAnnotation, error)
	ListAnnotations(ctx context.Context, assistSessionID uint) ([]assistdomain.RemoteAssistAnnotation, error)
	DeleteAnnotation(ctx context.Context, id uint) error
	AttachRecording(ctx context.Context, id uint, customerUserID uint, meta RecordingMeta) (*assistdomain.RemoteAssistSession, error)
	RespondConsent(ctx context.Context, id uint, customerUserID uint, accept bool) (*assistdomain.RemoteAssistSession, error)
}
