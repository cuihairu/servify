package delivery

import (
	"context"
	assistdomain "servify/apps/server/internal/modules/assist/domain"

	assistapp "servify/apps/server/internal/modules/assist/application"
)

// HandlerServiceAdapter 把 assist application 包成 HandlerService（management 与访客面共用）。
type HandlerServiceAdapter struct {
	service *assistapp.Service
}

func NewHandlerService(service *assistapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{service: service}
}

func (a *HandlerServiceAdapter) StartSession(ctx context.Context, cmd StartCommand) (*assistdomain.RemoteAssistSession, error) {
	return a.service.StartSession(ctx, cmd)
}

func (a *HandlerServiceAdapter) EndSession(ctx context.Context, id uint, cmd EndCommand) (*assistdomain.RemoteAssistSession, error) {
	return a.service.EndSession(ctx, id, cmd)
}

func (a *HandlerServiceAdapter) GetSession(ctx context.Context, id uint) (*assistdomain.RemoteAssistSession, error) {
	return a.service.GetSession(ctx, id)
}

func (a *HandlerServiceAdapter) ListSessions(ctx context.Context, conversationSessionID string, limit int) ([]assistdomain.RemoteAssistSession, error) {
	return a.service.ListSessions(ctx, conversationSessionID, limit)
}

func (a *HandlerServiceAdapter) AddAnnotation(ctx context.Context, assistSessionID uint, cmd AnnotationCommand) (*assistdomain.RemoteAssistAnnotation, error) {
	return a.service.AddAnnotation(ctx, assistSessionID, cmd)
}

func (a *HandlerServiceAdapter) ListAnnotations(ctx context.Context, assistSessionID uint) ([]assistdomain.RemoteAssistAnnotation, error) {
	return a.service.ListAnnotations(ctx, assistSessionID)
}

func (a *HandlerServiceAdapter) DeleteAnnotation(ctx context.Context, id uint) error {
	return a.service.DeleteAnnotation(ctx, id)
}

func (a *HandlerServiceAdapter) AttachRecording(ctx context.Context, id uint, customerUserID uint, meta RecordingMeta) (*assistdomain.RemoteAssistSession, error) {
	return a.service.AttachRecording(ctx, id, customerUserID, meta)
}

func (a *HandlerServiceAdapter) RespondConsent(ctx context.Context, id uint, customerUserID uint, accept bool) (*assistdomain.RemoteAssistSession, error) {
	return a.service.RespondConsent(ctx, id, customerUserID, accept)
}
