package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	assistdelivery "servify/apps/server/internal/modules/assist/delivery"

	"github.com/gin-gonic/gin"
)

// AssistHandler 远程协助管理面：协助会话生命周期、录制元数据与标注
type AssistHandler struct {
	service assistdelivery.HandlerService
}

func NewAssistHandler(service assistdelivery.HandlerService) *AssistHandler {
	return &AssistHandler{service: service}
}

// StartAssistRequest 发起协助请求体。租户/工作区不再接受客户端自报，
// 一律取鉴权主体的 token scope（RA-1）。
type StartAssistRequest struct {
	ConversationSessionID string `json:"conversation_session_id" binding:"required" example:"sess-123"`
	AgentUserID           uint   `json:"agent_user_id" example:"7"`
}

// StartSession 发起远程协助
// @Summary 发起远程协助会话
// @Description 在指定会话上创建 active 状态的协助记录（consent pending、同会话单活跃）；信令与媒体面走既有 WS/RTC 通道
// @Tags 远程协助
// @Accept json
// @Produce json
// @Param body body StartAssistRequest true "协助内容"
// @Success 201 {object} models.RemoteAssistSession
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/remote-assist/sessions [post]
func (h *AssistHandler) StartSession(c *gin.Context) {
	var req StartAssistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	session, err := h.service.StartSession(c.Request.Context(), assistdelivery.StartCommand{
		ConversationSessionID: req.ConversationSessionID,
		AgentUserID:           req.AgentUserID,
	})
	if err != nil {
		c.JSON(assistErrorStatus(err), ErrorResponse{Error: "Failed to start remote assist", Message: err.Error()})
		return
	}
	c.JSON(http.StatusCreated, session)
}

// EndAssistRequest 结束协助请求体（录制元数据可缺省）。
type EndAssistRequest struct {
	Outcome             string `json:"outcome" example:"ended"` // ended|failed
	RecordingKey        string `json:"recording_key,omitempty"`
	RecordingMime       string `json:"recording_mime,omitempty"`
	RecordingDurationMs int64  `json:"recording_duration_ms,omitempty"`
	RecordingSize       int64  `json:"recording_size,omitempty"`
}

// EndSession 结束远程协助
// @Summary 结束远程协助会话
// @Description 将协助置为 ended/failed 并落录制元数据（文件本体经 /api/v1/upload 上传）
// @Tags 远程协助
// @Accept json
// @Produce json
// @Param id path int true "协助会话 ID"
// @Param body body EndAssistRequest true "结束内容"
// @Success 200 {object} models.RemoteAssistSession
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/remote-assist/sessions/{id}/end [post]
func (h *AssistHandler) EndSession(c *gin.Context) {
	id64, err := parseUintParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid assist session id", Message: err.Error()})
		return
	}
	var req EndAssistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	session, err := h.service.EndSession(c.Request.Context(), uint(id64), assistdelivery.EndCommand{
		Outcome:             req.Outcome,
		RecordingKey:        req.RecordingKey,
		RecordingMime:       req.RecordingMime,
		RecordingDurationMs: req.RecordingDurationMs,
		RecordingSize:       req.RecordingSize,
	})
	if err != nil {
		c.JSON(assistErrorStatus(err), ErrorResponse{Error: "Failed to end remote assist", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, session)
}

// ListSessions 获取远程协助列表
// @Summary 获取远程协助列表
// @Description 按会话（可选）倒序列出协助记录
// @Tags 远程协助
// @Produce json
// @Param conversation_session_id query string false "按会话过滤"
// @Param limit query int false "上限（默认 100，最大 200）"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} ErrorResponse
// @Router /api/remote-assist/sessions [get]
func (h *AssistHandler) ListSessions(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	sessions, err := h.service.ListSessions(c.Request.Context(), c.Query("conversation_session_id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list remote assist sessions", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": sessions, "total": len(sessions)})
}

// GetSession 获取单次远程协助
// @Summary 获取单次远程协助
// @Description 按协助会话 ID 返回详情（含录制元数据）
// @Tags 远程协助
// @Produce json
// @Param id path int true "协助会话 ID"
// @Success 200 {object} models.RemoteAssistSession
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/remote-assist/sessions/{id} [get]
func (h *AssistHandler) GetSession(c *gin.Context) {
	id64, err := parseUintParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid assist session id", Message: err.Error()})
		return
	}
	session, err := h.service.GetSession(c.Request.Context(), uint(id64))
	if err != nil {
		c.JSON(assistErrorStatus(err), ErrorResponse{Error: "Failed to get remote assist session", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, session)
}

// AddAnnotationRequest 新增标注请求体。
type AddAnnotationRequest struct {
	TimestampMs int64          `json:"timestamp_ms" example:"12000"` // 相对录制/协助开始时间的毫秒偏移
	Shape       string         `json:"shape" example:"rect"`         // rect|freehand|arrow
	Payload     map[string]any `json:"payload" example:"{\"x\":0.1,\"y\":0.2,\"w\":0.3,\"h\":0.4,\"color\":\"#f5222d\"}"`
}

// AddAnnotation 追加标注
// @Summary 追加远程协助标注
// @Description 在协助会话上追加一条 Canvas 标注（坐标按归一化比例存储）
// @Tags 远程协助
// @Accept json
// @Produce json
// @Param id path int true "协助会话 ID"
// @Param body body AddAnnotationRequest true "标注内容"
// @Success 201 {object} models.RemoteAssistAnnotation
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/remote-assist/sessions/{id}/annotations [post]
func (h *AssistHandler) AddAnnotation(c *gin.Context) {
	id64, err := parseUintParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid assist session id", Message: err.Error()})
		return
	}
	var req AddAnnotationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	payload := "{}"
	if req.Payload != nil {
		if raw, err := json.Marshal(req.Payload); err == nil {
			payload = string(raw)
		}
	}
	annotation, err := h.service.AddAnnotation(c.Request.Context(), uint(id64), assistdelivery.AnnotationCommand{
		TimestampMs: req.TimestampMs,
		Shape:       req.Shape,
		Payload:     payload,
		CreatedBy:   principalUserID(c),
	})
	if err != nil {
		c.JSON(assistErrorStatus(err), ErrorResponse{Error: "Failed to add annotation", Message: err.Error()})
		return
	}
	c.JSON(http.StatusCreated, annotation)
}

// ListAnnotations 获取标注列表
// @Summary 获取远程协助标注
// @Description 按时间戳升序返回协助会话的全部标注
// @Tags 远程协助
// @Produce json
// @Param id path int true "协助会话 ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/remote-assist/sessions/{id}/annotations [get]
func (h *AssistHandler) ListAnnotations(c *gin.Context) {
	id64, err := parseUintParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid assist session id", Message: err.Error()})
		return
	}
	annotations, err := h.service.ListAnnotations(c.Request.Context(), uint(id64))
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list annotations", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": annotations, "total": len(annotations)})
}

// DeleteAnnotation 删除标注
// @Summary 删除远程协助标注
// @Tags 远程协助
// @Produce json
// @Param id path int true "标注 ID"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/remote-assist/annotations/{id} [delete]
func (h *AssistHandler) DeleteAnnotation(c *gin.Context) {
	id64, err := parseUintParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid annotation id", Message: err.Error()})
		return
	}
	if err := h.service.DeleteAnnotation(c.Request.Context(), uint(id64)); err != nil {
		c.JSON(assistErrorStatus(err), ErrorResponse{Error: "Failed to delete annotation", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "deleted"})
}

// assistErrorStatus 把 assist 服务错误映射到 HTTP 状态码。
func assistErrorStatus(err error) int {
	switch {
	case errors.Is(err, assistdelivery.ErrAssistNotFound),
		errors.Is(err, assistdelivery.ErrAssistAnnotationNotFound):
		return http.StatusNotFound
	case errors.Is(err, assistdelivery.ErrAssistSessionRequired),
		errors.Is(err, assistdelivery.ErrAssistShapeInvalid),
		errors.Is(err, assistdelivery.ErrAssistPayloadInvalid):
		return http.StatusBadRequest
	case errors.Is(err, assistdelivery.ErrAssistForbidden):
		return http.StatusForbidden
	case errors.Is(err, assistdelivery.ErrAssistAlreadyEnded),
		errors.Is(err, assistdelivery.ErrAssistSessionActive),
		errors.Is(err, assistdelivery.ErrAssistConsentDeclined),
		errors.Is(err, assistdelivery.ErrAssistConsentDecided):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// RegisterAssistRoutes 注册远程协助管理路由（复用 assist 资源权限）
func RegisterAssistRoutes(r *gin.RouterGroup, handler *AssistHandler) {
	assist := r.Group("/remote-assist")
	{
		assist.POST("/sessions", handler.StartSession)
		assist.GET("/sessions", handler.ListSessions)
		assist.GET("/sessions/:id", handler.GetSession)
		assist.POST("/sessions/:id/end", handler.EndSession)
		assist.GET("/sessions/:id/annotations", handler.ListAnnotations)
		assist.POST("/sessions/:id/annotations", handler.AddAnnotation)
		assist.DELETE("/annotations/:id", handler.DeleteAnnotation)
	}
}
