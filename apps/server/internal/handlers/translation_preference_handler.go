package handlers

import (
	"errors"
	"net/http"
	"time"

	translationdelivery "servify/apps/server/internal/modules/translation/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// TranslationPreferenceHandler 会话翻译语言偏好端点（Phase 1 刀一存储面 +
// 刀三双面共用，docs/realtime-translation-design.md）：
// GET/PUT/DELETE /api/v1/translation/preferences/:session_id。与 translate
// 端点同款单一注册点（AuthMiddleware 认证即可，agent/visitor 主体两面共
// 用）；读向（viewer 角色）由认证主体推导：end_user → visitor 读向（并强
// 制会话绑定：token 的 session_id 必须与路径一致），其余主体 → agent 读
// 向。租户 scope 由中间件注入 ctx、服务端自取，不接受请求方自报。
type TranslationPreferenceHandler struct {
	service translationdelivery.PreferenceHandlerService
	logger  *logrus.Logger
}

// NewTranslationPreferenceHandler 创建偏好处理器；service 为 nil 时端点按
// ErrTranslationUnavailable 降级为 503（装配层保证未装配时不注册）。
func NewTranslationPreferenceHandler(service translationdelivery.PreferenceHandlerService) *TranslationPreferenceHandler {
	return &TranslationPreferenceHandler{service: service, logger: logrus.StandardLogger()}
}

// TranslationPreferenceResponse 偏好读写响应：target_lang 为空串表示未设置。
type TranslationPreferenceResponse struct {
	SessionID  string `json:"session_id" example:"conv-123"`
	TargetLang string `json:"target_lang" example:"en"`
}

// TranslationPreferenceRequest 偏好写入请求体。
type TranslationPreferenceRequest struct {
	TargetLang string `json:"target_lang" binding:"required" example:"en"`
}

// viewerFor 主体 → 读向推导：访客（end_user）读写 visitor 读向并强制会话
// 绑定；坐席/管理/服务主体读写 agent 读向。返回 (viewer, 绑定是否通过)。
func viewerFor(c *gin.Context) (string, bool) {
	if c.GetString("principal_kind") == "end_user" {
		if sid := c.GetString("session_id"); sid != "" && sid == c.Param("session_id") {
			return translationdelivery.ViewerRoleVisitor, true
		}
		return translationdelivery.ViewerRoleVisitor, false
	}
	return translationdelivery.ViewerRoleAgent, true
}

// GetPreference 查询会话翻译目标语言
// @Summary 查询会话翻译语言偏好
// @Description 返回会话当前的自动翻译目标语言（坐席主体读 agent 读向，访客主体读自己的 visitor 读向）；未设置时 target_lang 为空串（200，不是 404）
// @Tags 翻译
// @Produce json
// @Param session_id path string true "客服会话 ID"
// @Success 200 {object} QueryResponse
// @Failure 400 {object} QueryResponse
// @Failure 403 {object} QueryResponse
// @Failure 503 {object} QueryResponse
// @Router /api/v1/translation/preferences/{session_id} [get]
func (h *TranslationPreferenceHandler) GetPreference(c *gin.Context) {
	viewer, bound := viewerFor(c)
	if !bound {
		c.JSON(http.StatusForbidden, QueryResponse{Success: false, Error: "session mismatch", Timestamp: time.Now()})
		return
	}
	lang, err := h.service.GetSessionLanguage(c.Request.Context(), c.Param("session_id"), viewer)
	h.respond(c, lang, err)
}

// PutPreference 设置会话翻译目标语言（upsert）
// @Summary 设置会话翻译语言偏好
// @Description upsert 当前主体读向的自动翻译目标语言（BCP-47 风格，如 en/zh-CN，大小写不敏感）；重复设置覆盖。访客主体只能设置自己会话的 visitor 读向
// @Tags 翻译
// @Accept json
// @Produce json
// @Param session_id path string true "客服会话 ID"
// @Param body body TranslationPreferenceRequest true "目标语言"
// @Success 200 {object} QueryResponse
// @Failure 400 {object} QueryResponse
// @Failure 403 {object} QueryResponse
// @Failure 503 {object} QueryResponse
// @Router /api/v1/translation/preferences/{session_id} [put]
func (h *TranslationPreferenceHandler) PutPreference(c *gin.Context) {
	viewer, bound := viewerFor(c)
	if !bound {
		c.JSON(http.StatusForbidden, QueryResponse{Success: false, Error: "session mismatch", Timestamp: time.Now()})
		return
	}
	start := time.Now()
	var req TranslationPreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, QueryResponse{
			Success:   false,
			Error:     "Invalid request format: " + err.Error(),
			Timestamp: time.Now(),
			Duration:  time.Since(start).String(),
		})
		return
	}
	lang, err := h.service.SetSessionLanguage(c.Request.Context(), c.Param("session_id"), viewer, req.TargetLang)
	h.respond(c, lang, err)
}

// DeletePreference 清除会话翻译目标语言
// @Summary 清除会话翻译语言偏好
// @Description 清除当前主体读向的自动翻译目标语言；无偏好时同样返回 200（幂等）。访客主体只能清除自己会话的 visitor 读向
// @Tags 翻译
// @Produce json
// @Param session_id path string true "客服会话 ID"
// @Success 200 {object} QueryResponse
// @Failure 400 {object} QueryResponse
// @Failure 403 {object} QueryResponse
// @Failure 503 {object} QueryResponse
// @Router /api/v1/translation/preferences/{session_id} [delete]
func (h *TranslationPreferenceHandler) DeletePreference(c *gin.Context) {
	viewer, bound := viewerFor(c)
	if !bound {
		c.JSON(http.StatusForbidden, QueryResponse{Success: false, Error: "session mismatch", Timestamp: time.Now()})
		return
	}
	if err := h.service.ClearSessionLanguage(c.Request.Context(), c.Param("session_id"), viewer); err != nil {
		h.respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, QueryResponse{Success: true, Timestamp: time.Now()})
}

// respond 统一 GET/PUT 的成功与错误出口（lang 为空串时 Data 仍带 session 语义）。
func (h *TranslationPreferenceHandler) respond(c *gin.Context, lang string, err error) {
	if err != nil {
		h.respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, QueryResponse{
		Success:   true,
		Data:      TranslationPreferenceResponse{SessionID: c.Param("session_id"), TargetLang: lang},
		Timestamp: time.Now(),
	})
}

// respondError 哨兵错误→400、未配置→503、跨 scope 冲突→409、其余→500。
func (h *TranslationPreferenceHandler) respondError(c *gin.Context, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, translationdelivery.ErrTranslationUnavailable):
		c.JSON(http.StatusServiceUnavailable, QueryResponse{Success: false, Error: err.Error(), Timestamp: time.Now()})
	case errors.Is(err, translationdelivery.ErrTranslationPrefConflict):
		c.JSON(http.StatusConflict, QueryResponse{Success: false, Error: err.Error(), Timestamp: time.Now()})
	case errors.Is(err, translationdelivery.ErrTranslationSessionRequired),
		errors.Is(err, translationdelivery.ErrTranslationTargetRequired),
		errors.Is(err, translationdelivery.ErrTranslationLangInvalid),
		errors.Is(err, translationdelivery.ErrTranslationViewerInvalid):
		c.JSON(http.StatusBadRequest, QueryResponse{Success: false, Error: err.Error(), Timestamp: time.Now()})
	default:
		h.logger.Warnf("translation preference operation failed: %v", err)
		c.JSON(http.StatusInternalServerError, QueryResponse{Success: false, Error: err.Error(), Timestamp: time.Now()})
	}
}
