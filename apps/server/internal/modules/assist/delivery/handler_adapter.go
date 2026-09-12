package delivery

import (
	"context"

	"servify/apps/server/internal/models"
	assistapp "servify/apps/server/internal/modules/assist/application"
)

// HandlerServiceAdapter 把 assist application 包成 HandlerService（management 与访客面共用）。
type HandlerServiceAdapter struct {
	service *assistapp.Service
}

func NewHandlerService(service *assistapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{service: service}
}

func (a *HandlerServiceAdapter) StartSession(ctx context.Context, cmd StartCommand) (*models.RemoteAssistSession, error) {
	return a.service.StartSession(ctx, cmd)
}

func (a *HandlerServiceAdapter) EndSession(ctx context.Context, id uint, cmd EndCommand) (*models.RemoteAssistSession, error) {
	return a.service.EndSession(ctx, id, cmd)
}

func (a *HandlerServiceAdapter) GetSession(ctx context.Context, id uint) (*models.RemoteAssistSession, error) {
	return a.service.GetSession(ctx, id)
}

func (a *HandlerServiceAdapter) ListSessions(ctx context.Context, conversationSessionID string, limit int) ([]models.RemoteAssistSession, error) {
	return a.service.ListSessions(ctx, conversationSessionID, limit)
}

func (a *HandlerServiceAdapter) AddAnnotation(ctx context.Context, assistSessionID uint, cmd AnnotationCommand) (*models.RemoteAssistAnnotation, error) {
	return a.service.AddAnnotation(ctx, assistSessionID, cmd)
}

func (a *HandlerServiceAdapter) ListAnnotations(ctx context.Context, assistSessionID uint) ([]models.RemoteAssistAnnotation, error) {
	return a.service.ListAnnotations(ctx, assistSessionID)
}

func (a *HandlerServiceAdapter) DeleteAnnotation(ctx context.Context, id uint) error {
	return a.service.DeleteAnnotation(ctx, id)
}

func (a *HandlerServiceAdapter) AttachRecording(ctx context.Context, id uint, customerUserID uint, meta RecordingMeta) (*models.RemoteAssistSession, error) {
	return a.service.AttachRecording(ctx, id, customerUserID, meta)
}
