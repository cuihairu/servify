package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	qualitydelivery "servify/apps/server/internal/modules/quality/delivery"

	"github.com/gin-gonic/gin"
)

// QualityHandler 质检抽检工作台：记录查询、人工确认与重新打分
type QualityHandler struct {
	service qualitydelivery.HandlerService
}

func NewQualityHandler(service qualitydelivery.HandlerService) *QualityHandler {
	return &QualityHandler{service: service}
}

// ListReviews 获取质检记录列表
// @Summary 获取质检记录列表
// @Description 分页查询质检记录，支持按状态、坐席、有无违规、严重度、分数区间与时间过滤
// @Tags 质检
// @Produce json
// @Param status query string false "状态 pending|skipped|scored|failed|confirmed"
// @Param agent_id query int false "坐席用户 ID"
// @Param has_violations query bool false "是否含规则违规"
// @Param severity query string false "最高严重度 low|medium|high"
// @Param min_score query number false "LLM 总分下界"
// @Param max_score query number false "LLM 总分上界"
// @Param from query string false "创建时间起始 (RFC3339)"
// @Param to query string false "创建时间截止 (RFC3339)"
// @Param page query int false "页码（默认 1）"
// @Param page_size query int false "每页条数（默认 20，上限 200）"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} ErrorResponse
// @Router /api/quality/reviews [get]
func (h *QualityHandler) ListReviews(c *gin.Context) {
	query := qualitydelivery.ReviewListQuery{
		Status:   c.Query("status"),
		Severity: c.Query("severity"),
	}
	if v := c.Query("agent_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			id64 := uint(id)
			query.AgentID = &id64
		}
	}
	if v := c.Query("customer_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			id64 := uint(id)
			query.CustomerID = &id64
		}
	}
	if v := c.Query("has_violations"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			query.HasViolations = &b
		}
	}
	if v := c.Query("min_score"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			query.MinScore = &f
		}
	}
	if v := c.Query("max_score"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			query.MaxScore = &f
		}
	}
	if v := c.Query("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			query.From = &t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			query.To = &t
		}
	}
	if v := c.Query("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			query.Page = p
		}
	}
	if v := c.Query("page_size"); v != "" {
		if ps, err := strconv.Atoi(v); err == nil {
			query.PageSize = ps
		}
	}
	reviews, total, err := h.service.ListReviews(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list quality reviews", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": reviews, "total": total, "page": query.Page, "page_size": query.PageSize})
}

// GetReview 获取单条质检记录
// @Summary 获取单条质检记录
// @Description 按会话 ID 返回质检记录（含违规明细与维度得分 JSON）
// @Tags 质检
// @Produce json
// @Param sessionId path string true "会话 ID"
// @Success 200 {object} models.QualityReview
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/quality/reviews/{sessionId} [get]
func (h *QualityHandler) GetReview(c *gin.Context) {
	sessionID := c.Param("sessionId")
	review, err := h.service.GetReview(c.Request.Context(), sessionID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, qualitydelivery.ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{Error: "Failed to get quality review", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, review)
}

// ConfirmReviewRequest 人工确认请求体（swag 文档用，字段与 ConfirmCommand 一致）。
type ConfirmReviewRequest struct {
	ManualScore  *float64 `json:"manual_score" example:"8.5"`   // 人工覆盖分，缺省不覆盖
	ManualResult string   `json:"manual_result" example:"pass"` // pass|violation
	ReviewNote   string   `json:"review_note" example:"抽检通过"`
}

// ConfirmReview 人工确认质检记录
// @Summary 人工确认质检记录
// @Description 将 scored 记录置为 confirmed，可带人工覆盖分、结论与备注
// @Tags 质检
// @Accept json
// @Produce json
// @Param sessionId path string true "会话 ID"
// @Param body body ConfirmReviewRequest true "人工确认内容"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Router /api/quality/reviews/{sessionId}/confirm [post]
func (h *QualityHandler) ConfirmReview(c *gin.Context) {
	sessionID := c.Param("sessionId")
	var req ConfirmReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	cmd := qualitydelivery.ConfirmCommand{
		ManualScore:  req.ManualScore,
		ManualResult: req.ManualResult,
		ReviewNote:   req.ReviewNote,
	}
	if id := principalUserID(c); id != 0 {
		cmd.ReviewedBy = id
	}
	if err := h.service.ConfirmReview(c.Request.Context(), sessionID, cmd); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, qualitydelivery.ErrNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, qualitydelivery.ErrNotScoreable) {
			status = http.StatusConflict
		}
		c.JSON(status, ErrorResponse{Error: "Failed to confirm review", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "confirmed"})
}

// RescoreReview 重新打分
// @Summary 重新打分
// @Description 将记录重置为 pending（attempt 清零），下一轮扫描重新质检打分；confirmed 需 force=true
// @Tags 质检
// @Produce json
// @Param sessionId path string true "会话 ID"
// @Param force query bool false "强制重打已确认记录"
// @Success 202 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Router /api/quality/reviews/{sessionId}/rescore [post]
func (h *QualityHandler) RescoreReview(c *gin.Context) {
	sessionID := c.Param("sessionId")
	force, _ := strconv.ParseBool(c.Query("force"))
	if err := h.service.RescoreReview(c.Request.Context(), sessionID, force); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, qualitydelivery.ErrNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, qualitydelivery.ErrConfirmedNeedsForce) {
			status = http.StatusConflict
		}
		c.JSON(status, ErrorResponse{Error: "Failed to rescore review", Message: err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, SuccessResponse{Message: "rescore scheduled"})
}

// GetScorerStatus 返回 LLM 打分开关状态
// @Summary 返回 LLM 打分开关状态
// @Description enabled 表示 LLM 打分生效；false 表示 rules-only 模式
// @Tags 质检
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/quality/scorer [get]
func (h *QualityHandler) GetScorerStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"llm_enabled": h.service.ScorerEnabled()})
}

// principalUserID 从认证中间件注入的 claims 里取用户 ID（取不到返回 0）。
func principalUserID(c *gin.Context) uint {
	if v, ok := c.Get("user_id"); ok {
		switch id := v.(type) {
		case uint:
			return id
		case float64:
			return uint(id)
		}
	}
	return 0
}

// RegisterQualityRoutes 注册质检管理路由
func RegisterQualityRoutes(r *gin.RouterGroup, handler *QualityHandler) {
	quality := r.Group("/quality")
	{
		quality.GET("/reviews", handler.ListReviews)
		quality.GET("/reviews/:sessionId", handler.GetReview)
		quality.POST("/reviews/:sessionId/confirm", handler.ConfirmReview)
		quality.POST("/reviews/:sessionId/rescore", handler.RescoreReview)
		quality.GET("/scorer", handler.GetScorerStatus)
	}
}
