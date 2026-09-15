package delivery

import (
	"context"
	"fmt"

	"servify/apps/server/internal/models"
)

// RuntimeService is the AI contract used by websocket/router/runtime glue.
type RuntimeService interface {
	ProcessQuery(ctx context.Context, query string, sessionID string) (*AIResponse, error)
	ShouldTransferToHuman(query string, sessionHistory []models.Message) bool
	GetSessionSummary(messages []models.Message) (string, error)
	GetStatus(ctx context.Context) map[string]interface{}
}

type EnhancedRuntimeService interface {
	RuntimeService
	ProcessQueryEnhanced(ctx context.Context, query string, sessionID string) (*EnhancedAIResponse, error)
	UploadKnowledgeDocument(ctx context.Context, title, content string, tags []string) error
	GetMetrics() *AIMetrics
	SetKnowledgeProviderEnabled(enabled bool)
	ResetCircuitBreaker()
	SyncKnowledgeBase(ctx context.Context) error
}

// HandlerService is the only AI contract that HTTP handlers should depend on.
type HandlerService interface {
	ProcessQuery(ctx context.Context, query string, sessionID string) (interface{}, error)
	GetStatus(ctx context.Context) map[string]interface{}
	GetMetrics() (*AIMetrics, bool)
	UploadKnowledgeDocument(ctx context.Context, title, content string, tags []string) error
	SyncKnowledgeBase(ctx context.Context) error
	SetKnowledgeProviderEnabled(enabled bool) bool
	ResetCircuitBreaker() bool
}

// HandlerServiceAdapter bridges legacy AI services to the handler-facing AI contract.
type HandlerServiceAdapter struct {
	service RuntimeService
}

func NewHandlerServiceAdapter(service RuntimeService) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{service: service}
}

func (a *HandlerServiceAdapter) ProcessQuery(ctx context.Context, query string, sessionID string) (interface{}, error) {
	if a == nil || a.service == nil {
		return nil, fmt.Errorf("ai service not configured")
	}
	if enhanced, ok := a.service.(EnhancedRuntimeService); ok {
		return enhanced.ProcessQueryEnhanced(ctx, query, sessionID)
	}
	return a.service.ProcessQuery(ctx, query, sessionID)
}

func (a *HandlerServiceAdapter) GetStatus(ctx context.Context) map[string]interface{} {
	if a == nil || a.service == nil {
		return nil
	}
	return a.service.GetStatus(ctx)
}

func (a *HandlerServiceAdapter) GetMetrics() (*AIMetrics, bool) {
	if a == nil || a.service == nil {
		return nil, false
	}
	enhanced, ok := a.service.(EnhancedRuntimeService)
	if !ok {
		return nil, false
	}
	return enhanced.GetMetrics(), true
}

func (a *HandlerServiceAdapter) UploadKnowledgeDocument(ctx context.Context, title, content string, tags []string) error {
	if a == nil || a.service == nil {
		return fmt.Errorf("ai service not configured")
	}
	enhanced, ok := a.service.(EnhancedRuntimeService)
	if !ok {
		return errUnsupportedEnhancedFeature("document upload")
	}
	return enhanced.UploadKnowledgeDocument(ctx, title, content, tags)
}

func (a *HandlerServiceAdapter) SyncKnowledgeBase(ctx context.Context) error {
	if a == nil || a.service == nil {
		return fmt.Errorf("ai service not configured")
	}
	enhanced, ok := a.service.(EnhancedRuntimeService)
	if !ok {
		return errUnsupportedEnhancedFeature("knowledge base sync")
	}
	return enhanced.SyncKnowledgeBase(ctx)
}

func (a *HandlerServiceAdapter) SetKnowledgeProviderEnabled(enabled bool) bool {
	if a == nil || a.service == nil {
		return false
	}
	enhanced, ok := a.service.(EnhancedRuntimeService)
	if !ok {
		return false
	}
	enhanced.SetKnowledgeProviderEnabled(enabled)
	return true
}

func (a *HandlerServiceAdapter) ResetCircuitBreaker() bool {
	if a == nil || a.service == nil {
		return false
	}
	enhanced, ok := a.service.(EnhancedRuntimeService)
	if !ok {
		return false
	}
	enhanced.ResetCircuitBreaker()
	return true
}

type unsupportedEnhancedFeatureError string

func (e unsupportedEnhancedFeatureError) Error() string {
	return string(e)
}

func errUnsupportedEnhancedFeature(feature string) error {
	return unsupportedEnhancedFeatureError(feature + " not available for standard AI service")
}

func IsUnsupportedEnhancedFeature(err error) bool {
	_, ok := err.(unsupportedEnhancedFeatureError)
	return ok
}
