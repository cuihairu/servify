package delivery

import (
	"context"

	translationapp "servify/apps/server/internal/modules/translation/application"

	"servify/apps/server/internal/platform/llm"
)

// HandlerServiceAdapter 把 translation application 包成 HandlerService。
type HandlerServiceAdapter struct {
	service *translationapp.Service
}

// 编译期确认实现端口。
var _ HandlerService = (*HandlerServiceAdapter)(nil)

// NewHandlerService 从应用服务构造契约实现。
func NewHandlerService(service *translationapp.Service) *HandlerServiceAdapter {
	return &HandlerServiceAdapter{service: service}
}

// NewTranslationHandlerService 一步装配：LLM provider + 出站参数 → 契约实现
// （runtime 装配用；llm 为 nil 时端点降级 503 而非启动失败）。
func NewTranslationHandlerService(llm llm.LLMProvider, params RuntimeParams) *HandlerServiceAdapter {
	return NewHandlerService(translationapp.NewService(llm, params))
}

// Translate 透传应用服务；nil 适配器按未配置降级（503 语义）。
func (a *HandlerServiceAdapter) Translate(ctx context.Context, cmd TranslateCommand) (TranslateResult, error) {
	if a == nil || a.service == nil {
		return TranslateResult{}, ErrTranslationUnavailable
	}
	return a.service.Translate(ctx, cmd)
}
