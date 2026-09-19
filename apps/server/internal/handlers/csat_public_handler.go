package handlers

import (
	"errors"
	"net/http"

	satisfactiondelivery "servify/apps/server/internal/modules/satisfaction/delivery"

	"github.com/gin-gonic/gin"
)

// CSATSurveyHandler 处理无需登录的 CSAT 调查答复
type CSATSurveyHandler struct {
	service satisfactiondelivery.SatisfactionService
}

// NewCSATSurveyHandler 创建公共调查处理器
func NewCSATSurveyHandler(service satisfactiondelivery.SatisfactionService) *CSATSurveyHandler {
	return &CSATSurveyHandler{service: service}
}

// GetSurvey 返回调查预览信息
func (h *CSATSurveyHandler) GetSurvey(c *gin.Context) {
	token := c.Param("token")
	survey, err := h.service.GetSurveyPreviewByToken(c.Request.Context(), token)
	if err != nil {
		if errors.Is(err, satisfactiondelivery.ErrSurveyNotFound) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "Survey not found", Message: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load survey", Message: err.Error()})
		return
	}

	c.JSON(http.StatusOK, survey)
}

// SubmitResponse 提交问卷结果
func (h *CSATSurveyHandler) SubmitResponse(c *gin.Context) {
	token := c.Param("token")
	var req struct {
		Rating  int    `json:"rating" binding:"required,min=1,max=5"`
		Comment string `json:"comment"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid payload",
			Message: err.Error(),
		})
		return
	}

	satisfaction, err := h.service.RespondSurvey(c.Request.Context(), token, req.Rating, req.Comment)
	if err != nil {
		switch {
		case errors.Is(err, satisfactiondelivery.ErrSurveyNotFound):
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "Survey not found", Message: err.Error()})
		case errors.Is(err, satisfactiondelivery.ErrSurveyExpired):
			c.JSON(http.StatusGone, ErrorResponse{Error: "Survey expired", Message: err.Error()})
		case errors.Is(err, satisfactiondelivery.ErrSurveyCompleted):
			c.JSON(http.StatusConflict, ErrorResponse{Error: "Survey already completed", Message: err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to submit survey", Message: err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Message: "感谢您的反馈！",
		Data:    satisfaction,
	})
}

// RegisterCSATSurveyRoutes 注册公共 CSAT 路由
func RegisterCSATSurveyRoutes(r *gin.RouterGroup, handler *CSATSurveyHandler) {
	if r == nil || handler == nil {
		return
	}
	r.GET("/csat/:token", handler.GetSurvey)
	r.POST("/csat/:token/respond", handler.SubmitResponse)
}
