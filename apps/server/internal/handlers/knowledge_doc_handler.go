package handlers

import (
	"net/http"
	"strconv"

	knowledgedelivery "servify/apps/server/internal/modules/knowledge/delivery"

	"github.com/gin-gonic/gin"
)

type KnowledgeDocHandler struct {
	service    knowledgedelivery.HandlerService
	publicOnly bool
}

func NewKnowledgeDocHandler(service knowledgedelivery.HandlerService) *KnowledgeDocHandler {
	return &KnowledgeDocHandler{service: service}
}

func NewPublicKnowledgeDocHandler(service knowledgedelivery.HandlerService) *KnowledgeDocHandler {
	return &KnowledgeDocHandler{service: service, publicOnly: true}
}

func (h *KnowledgeDocHandler) List(c *gin.Context) {
	var req knowledgedelivery.KnowledgeDocListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid query parameters", Message: err.Error()})
		return
	}
	if h.publicOnly {
		req.PublicOnly = true
	}
	docs, total, err := h.service.List(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list knowledge docs", Message: err.Error()})
		return
	}
	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	c.JSON(http.StatusOK, PaginatedResponse{
		Data:     docs,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

func (h *KnowledgeDocHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: err.Error()})
		return
	}
	doc, err := h.service.Get(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Knowledge doc not found", Message: err.Error()})
		return
	}
	if h.publicOnly && !doc.IsPublic {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Knowledge doc not found", Message: "document is not public"})
		return
	}
	c.JSON(http.StatusOK, doc)
}

func (h *KnowledgeDocHandler) Create(c *gin.Context) {
	var req knowledgedelivery.KnowledgeDocCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	doc, err := h.service.Create(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to create knowledge doc", Message: err.Error()})
		return
	}
	c.JSON(http.StatusCreated, doc)
}

func (h *KnowledgeDocHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: err.Error()})
		return
	}
	var req knowledgedelivery.KnowledgeDocUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	doc, err := h.service.Update(c.Request.Context(), uint(id), &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to update knowledge doc", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, doc)
}

func (h *KnowledgeDocHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: err.Error()})
		return
	}
	if err := h.service.Delete(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to delete knowledge doc", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "deleted"})
}

// ListSources 列来源登记（可选 ?type= 过滤，B3-1a §8.1）。
func (h *KnowledgeDocHandler) ListSources(c *gin.Context) {
	sources, err := h.service.ListSources(c.Request.Context(), c.Query("type"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to list knowledge sources", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, sources)
}

// CreateSource 登记知识来源。
func (h *KnowledgeDocHandler) CreateSource(c *gin.Context) {
	var req knowledgedelivery.KnowledgeSourceCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	source, err := h.service.CreateSource(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to create knowledge source", Message: err.Error()})
		return
	}
	c.JSON(http.StatusCreated, source)
}

// DeleteSource 删除来源登记（仍被文档引用时拒绝）。
func (h *KnowledgeDocHandler) DeleteSource(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: err.Error()})
		return
	}
	if err := h.service.DeleteSource(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to delete knowledge source", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "deleted"})
}

// ListIndexJobs 按文档列索引任务（状态/版本/错误可见）。
func (h *KnowledgeDocHandler) ListIndexJobs(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit <= 0 {
		limit = 20
	}
	jobs, err := h.service.ListIndexJobs(c.Request.Context(), c.Param("id"), limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to list index jobs", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, jobs)
}

// IndexDocument 排队并执行一次文档索引（重建索引入口）。
func (h *KnowledgeDocHandler) IndexDocument(c *gin.Context) {
	result, err := h.service.IndexDocument(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to index knowledge doc", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// RetryIndexJob 重跑索引任务（失败重试/按当前版本重建）。
func (h *KnowledgeDocHandler) RetryIndexJob(c *gin.Context) {
	result, err := h.service.RetryIndexJob(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to retry index job", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func RegisterKnowledgeDocRoutes(r *gin.RouterGroup, handler *KnowledgeDocHandler) {
	docs := r.Group("/knowledge-docs")
	{
		docs.GET("", handler.List)
		docs.GET("/:id", handler.Get)
		docs.POST("", handler.Create)
		docs.PUT("/:id", handler.Update)
		docs.DELETE("/:id", handler.Delete)

		// 来源登记与索引任务（B3-1a，docs/v1-convergence-plan.md §8.1/§8.2）。
		docs.GET("/sources", handler.ListSources)
		docs.POST("/sources", handler.CreateSource)
		docs.DELETE("/sources/:id", handler.DeleteSource)
		docs.GET("/:id/index-jobs", handler.ListIndexJobs)
		docs.POST("/:id/index-jobs", handler.IndexDocument)
		docs.POST("/index-jobs/:job_id/retry", handler.RetryIndexJob)
	}
}

func RegisterPublicKnowledgeBaseRoutes(r *gin.RouterGroup, handler *KnowledgeDocHandler) {
	if !handler.publicOnly {
		handler = NewPublicKnowledgeDocHandler(handler.service)
	}
	kb := r.Group("/kb")
	{
		kb.GET("/docs", handler.List)
		kb.GET("/docs/:id", handler.Get)
	}
}
