package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	translationdelivery "servify/apps/server/internal/modules/translation/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// TranslationHandler 聊天文本实时翻译端点：POST /api/v1/translation/translate。
// Phase 0（见 docs/realtime-translation-design.md）：单条消息粒度、无状态、
// 坐席/访客两面共用同一契约。
type TranslationHandler struct {
	service translationdelivery.HandlerService
	logger  *logrus.Logger
}

// NewTranslationHandler 创建翻译处理器；service 为 nil 时端点按
// ErrTranslationUnavailable 降级为 503（AI 未装配的部署形态）。
func NewTranslationHandler(service translationdelivery.HandlerService) *TranslationHandler {
	return &TranslationHandler{service: service, logger: logrus.StandardLogger()}
}

// TranslateRequest 翻译请求体。target_lang 必填（BCP-47 风格，如 zh-CN/en），
// source_lang 缺省 auto（模型自动检测）。
type TranslateRequest struct {
	Text       string `json:"text" binding:"required" example:"请问订单什么时候发货？"`
	TargetLang string `json:"target_lang" binding:"required" example:"en"`
	SourceLang string `json:"source_lang,omitempty" example:"zh-CN"`
}

// Translate 翻译单条聊天文本
// @Summary 聊天文本实时翻译（Phase 0）
// @Description 单条消息粒度的实时翻译：坐席/访客客户端对到达的消息即时调用，返回可直接展示的译文。参数问题 400；AI 未装配 503；模型上游故障 502；超时 504
// @Tags 翻译
// @Accept json
// @Produce json
// @Param body body TranslateRequest true "翻译请求"
// @Success 200 {object} QueryResponse
// @Failure 400 {object} QueryResponse
// @Failure 502 {object} QueryResponse
// @Failure 503 {object} QueryResponse
// @Failure 504 {object} QueryResponse
// @Router /api/v1/translation/translate [post]
func (h *TranslationHandler) Translate(c *gin.Context) {
	start := time.Now()

	var req TranslateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, QueryResponse{
			Success:   false,
			Error:     "Invalid request format: " + err.Error(),
			Timestamp: time.Now(),
			Duration:  time.Since(start).String(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	result, err := h.service.Translate(ctx, translationdelivery.TranslateCommand{
		Text:       req.Text,
		SourceLang: req.SourceLang,
		TargetLang: req.TargetLang,
	})
	if err != nil {
		h.respondError(c, err, start)
		return
	}
	c.JSON(http.StatusOK, QueryResponse{
		Success:   true,
		Data:      result,
		Timestamp: time.Now(),
		Duration:  time.Since(start).String(),
	})
}

// respondError 哨兵错误→400、未配置→503、超时→504、其余（provider 故障）→502。
func (h *TranslationHandler) respondError(c *gin.Context, err error, start time.Time) {
	body := QueryResponse{
		Success:   false,
		Error:     err.Error(),
		Timestamp: time.Now(),
		Duration:  time.Since(start).String(),
	}
	switch {
	case errors.Is(err, translationdelivery.ErrTranslationUnavailable):
		c.JSON(http.StatusServiceUnavailable, body)
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, body)
	case errors.Is(err, translationdelivery.ErrTranslationTextRequired),
		errors.Is(err, translationdelivery.ErrTranslationTextTooLong),
		errors.Is(err, translationdelivery.ErrTranslationTargetRequired),
		errors.Is(err, translationdelivery.ErrTranslationLangInvalid),
		errors.Is(err, translationdelivery.ErrTranslationEmptyOutput):
		c.JSON(http.StatusBadRequest, body)
	default:
		h.logger.Warnf("translation failed: %v", err)
		c.JSON(http.StatusBadGateway, body)
	}
}
