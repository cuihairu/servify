package handlers

import (
	"net/http"

	assistdelivery "servify/apps/server/internal/modules/assist/delivery"

	"github.com/gin-gonic/gin"
)

// AssistRecordingHandler 访客面远程协助回写：录制元数据与同意表态
// （文件本体配合既有 /api/v1/upload 上传）
type AssistRecordingHandler struct {
	service assistdelivery.HandlerService
}

func NewAssistRecordingHandler(service assistdelivery.HandlerService) *AssistRecordingHandler {
	return &AssistRecordingHandler{service: service}
}

// AttachRecordingRequest 录制元数据回写请求体。
type AttachRecordingRequest struct {
	RecordingKey        string `json:"recording_key" binding:"required" example:"uploads/2026/09/rec-abc.webm"`
	RecordingMime       string `json:"recording_mime" example:"video/webm"`
	RecordingDurationMs int64  `json:"recording_duration_ms" example:"95000"`
	RecordingSize       int64  `json:"recording_size" example:"1048576"`
}

// AttachRecording 回写远程协助录制元数据
// @Summary 回写远程协助录制元数据（访客面）
// @Description 访客经 /api/v1/upload 上传录制文件后，把 key/时长/大小挂到对应协助会话；校验该协助归属当前访客的会话
// @Tags 远程协助
// @Accept json
// @Produce json
// @Param id path int true "协助会话 ID"
// @Param body body AttachRecordingRequest true "录制元数据"
// @Success 200 {object} models.RemoteAssistSession
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/remote-assist/{id}/recording [post]
func (h *AssistRecordingHandler) AttachRecording(c *gin.Context) {
	id64, err := parseUintParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid assist session id", Message: err.Error()})
		return
	}
	var req AttachRecordingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	userID := principalUserID(c)
	if userID == 0 {
		c.JSON(http.StatusForbidden, ErrorResponse{Error: "Forbidden", Message: "missing authenticated user"})
		return
	}
	session, err := h.service.AttachRecording(c.Request.Context(), uint(id64), userID, assistdelivery.RecordingMeta{
		Key:        req.RecordingKey,
		Mime:       req.RecordingMime,
		DurationMs: req.RecordingDurationMs,
		Size:       req.RecordingSize,
	})
	if err != nil {
		c.JSON(assistErrorStatus(err), ErrorResponse{Error: "Failed to attach recording", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, session)
}

// RespondConsentRequest 协助同意表态请求体。
type RespondConsentRequest struct {
	Accept bool `json:"accept" example:"true"`
}

// RespondConsent 访客对远程协助邀请表态
// @Summary 远程协助同意表态（访客面）
// @Description 访客对协助邀请表示同意/拒绝：同意后协助继续；拒绝即结束协助且后续录制回写被拒绝（409）。同一表态幂等，相反表态 409
// @Tags 远程协助
// @Accept json
// @Produce json
// @Param id path int true "协助会话 ID"
// @Param body body RespondConsentRequest true "同意内容"
// @Success 200 {object} models.RemoteAssistSession
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/remote-assist/{id}/consent [post]
func (h *AssistRecordingHandler) RespondConsent(c *gin.Context) {
	id64, err := parseUintParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid assist session id", Message: err.Error()})
		return
	}
	var req RespondConsentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	userID := principalUserID(c)
	if userID == 0 {
		c.JSON(http.StatusForbidden, ErrorResponse{Error: "Forbidden", Message: "missing authenticated user"})
		return
	}
	session, err := h.service.RespondConsent(c.Request.Context(), uint(id64), userID, req.Accept)
	if err != nil {
		c.JSON(assistErrorStatus(err), ErrorResponse{Error: "Failed to respond consent", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, session)
}
