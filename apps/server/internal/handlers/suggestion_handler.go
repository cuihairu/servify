package handlers

import (
	"net/http"
	"strconv"
	"strings"

	suggestioncontract "servify/apps/server/internal/modules/suggestion/contract"
	suggestiondelivery "servify/apps/server/internal/modules/suggestion/delivery"

	"github.com/gin-gonic/gin"
)

type SuggestionHandler struct {
	service suggestiondelivery.HandlerService
}

func NewSuggestionHandler(service suggestiondelivery.HandlerService) *SuggestionHandler {
	return &SuggestionHandler{service: service}
}

func (h *SuggestionHandler) Suggest(c *gin.Context) {
	query := c.Query("query")
	limit := parseIntDefault(c.Query("limit"), 5)
	docLimit := parseIntDefault(c.Query("doc_limit"), 5)

	resp, err := h.service.Suggest(c.Request.Context(), &suggestioncontract.SuggestionRequest{
		Query:             query,
		TicketLimit:       limit,
		KnowledgeDocLimit: docLimit,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to suggest", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

func (h *SuggestionHandler) SuggestPost(c *gin.Context) {
	var req suggestioncontract.SuggestionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	resp, err := h.service.Suggest(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to suggest", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

func RegisterSuggestionRoutes(r *gin.RouterGroup, handler *SuggestionHandler) {
	assist := r.Group("/assist")
	{
		assist.GET("/suggest", handler.Suggest)
		assist.POST("/suggest", handler.SuggestPost)
		assist.GET("/suggestions/exposure-summary", handler.ExposureSummary)
	}
}

// ExposureSummary 曝光/转化聚合（P2-0 RQ-5 管理面出口）：assist 权限组
// 消费，返回全量曝光/转化计数与按 kind 分组，供运营侧核对推荐效果。
func (h *SuggestionHandler) ExposureSummary(c *gin.Context) {
	resp, err := h.service.ExposureSummary(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to summarize suggestion exposures", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

// InitialQuestions 客户侧首屏推荐问题（P2-0 RQ-1）：客户未输入前可点击
// 的热门问题，公开路由（无鉴权），数据面仅公开知识文档。
func (h *SuggestionHandler) InitialQuestions(c *gin.Context) {
	limit := parseIntDefault(c.Query("limit"), 8)
	resp, err := h.service.InitialQuestions(c.Request.Context(), &suggestioncontract.InitialQuestionsRequest{
		Limit:     limit,
		SessionID: c.Query("session_id"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to suggest initial questions", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

// NextQuestions 客户侧上下文联想（P2-0 RQ-2）：以客户最近一条消息为
// query 联想下一问，公开路由（无鉴权），数据面仅公开知识文档。
func (h *SuggestionHandler) NextQuestions(c *gin.Context) {
	query := strings.TrimSpace(c.Query("query"))
	if query == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: "query is required"})
		return
	}
	resp, err := h.service.NextQuestions(c.Request.Context(), &suggestioncontract.NextQuestionsRequest{
		Query:     query,
		SessionID: c.Query("session_id"),
		Limit:     parseIntDefault(c.Query("limit"), 8),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to suggest next questions", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

// NextQuestionsPost 是 NextQuestions 的 POST 版（query 放 body，适配
// 客户侧 SDK 在长 query 场景下的调用习惯）。
func (h *SuggestionHandler) NextQuestionsPost(c *gin.Context) {
	var req suggestioncontract.NextQuestionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: "query is required"})
		return
	}
	resp, err := h.service.NextQuestions(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to suggest next questions", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

// RegisterPublicSuggestionRoutes 挂载客户侧推荐问题公开路由（P2-0）：
// 与 CSAT / 知识库公开路由同模式，无鉴权；服务端只吐公开知识文档。
func RegisterPublicSuggestionRoutes(r *gin.RouterGroup, handler *SuggestionHandler) {
	suggestions := r.Group("/suggestions")
	{
		suggestions.GET("/initial", handler.InitialQuestions)
		suggestions.GET("/next", handler.NextQuestions)
		suggestions.POST("/next", handler.NextQuestionsPost)
	}
}

func parseIntDefault(v string, def int) int {
	if v == "" {
		return def
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return def
}
