package delivery

import (
	"net/http"
	"strconv"

	aiapp "servify/apps/server/internal/modules/ai/application"

	"github.com/gin-gonic/gin"
)

// V1.0 收敛 B3-1b（docs/v1-convergence-plan.md §5.3/§8.3）：反馈闭环与检索
// 分析的 HTTP 面。反馈端点走"认证即可"的单一双面注册点（坐席会话页与访客
// widget 共用；end_user 强制会话绑定，与翻译偏好读向同口径）；检索分析是
// 管理面读口（agent/admin）。

// AnswerFeedbackHandler 反馈与检索分析处理器。
type AnswerFeedbackHandler struct {
	service *aiapp.AnswerFeedbackService
}

func NewAnswerFeedbackHandler(service *aiapp.AnswerFeedbackService) *AnswerFeedbackHandler {
	return &AnswerFeedbackHandler{service: service}
}

// AnswerFeedbackRequest POST /api/v1/ai/feedback 请求契约。
type AnswerFeedbackRequest struct {
	AnswerID uint   `json:"answer_id" binding:"required"`
	Helpful  bool   `json:"helpful"`
	Comment  string `json:"comment"`
}

// SubmitFeedback POST /api/v1/ai/feedback：对一次 ai-answer 评价
// helpful/not_helpful + 可选意见，落 answer_feedback。
// @Summary AI 答案反馈
// @Description 对一次 AI 首答提交"是否有帮助"评价（访客经会话绑定校验）
// @Tags AI
// @Accept json
// @Produce json
// @Param request body AnswerFeedbackRequest true "反馈内容"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Router /api/v1/ai/feedback [post]
func (h *AnswerFeedbackHandler) SubmitFeedback(c *gin.Context) {
	var req AnswerFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	createdBy := c.GetString("username")
	if createdBy == "" {
		// 访客主体无用户名：以 token 会话标识留痕（审计回看口径）。
		createdBy = "visitor:" + c.GetString("session_id")
	}
	feedbackID, err := h.service.SubmitFeedback(c.Request.Context(), aiapp.SubmitFeedbackRequest{
		AnswerID:      req.AnswerID,
		Helpful:       req.Helpful,
		Comment:       req.Comment,
		CallerKind:    c.GetString("principal_kind"),
		CallerSession: c.GetString("session_id"),
		CreatedBy:     createdBy,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": feedbackID, "answer_id": req.AnswerID})
}

// RetrievalAnalytics GET /api/v1/ai/retrieval-analytics?days=&limit=：
// top 问答 / 无命中率问题 / 低置信率问题 + 反馈计数（§8.3）。
// @Summary 检索分析读口
// @Description Knowledge 管理页展示：窗口内 top 问答、零命中问题、低置信问题与反馈计数
// @Tags AI
// @Produce json
// @Param days query int false "统计窗口天数（默认 7，上限 90）"
// @Param limit query int false "榜单条数（默认 10，上限 50）"
// @Success 200 {object} aiapp.RetrievalAnalytics
// @Failure 400 {object} map[string]interface{}
// @Router /api/v1/ai/retrieval-analytics [get]
func (h *AnswerFeedbackHandler) RetrievalAnalytics(c *gin.Context) {
	days, err := strconv.Atoi(c.DefaultQuery("days", "7"))
	if err != nil || days <= 0 {
		days = 7
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if err != nil || limit <= 0 {
		limit = 10
	}
	analytics, err := h.service.RetrievalAnalytics(c.Request.Context(), days, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, analytics)
}

// RegisterAnswerFeedbackRoutes 注册反馈端点：调用方给到已挂 AuthMiddleware
// 的组（"认证即可"无主体种类限制——坐席与访客两面共用）。
func RegisterAnswerFeedbackRoutes(r gin.IRouter, handler *AnswerFeedbackHandler) {
	r.POST("/ai/feedback", handler.SubmitFeedback)
}

// RegisterAIRetrievalAnalyticsRoutes 注册检索分析读口：调用方给到管理面组
// （agent/admin 主体）。
func RegisterAIRetrievalAnalyticsRoutes(r gin.IRouter, handler *AnswerFeedbackHandler) {
	r.GET("/ai/retrieval-analytics", handler.RetrievalAnalytics)
}
