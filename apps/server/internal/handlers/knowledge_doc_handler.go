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

func RegisterKnowledgeDocRoutes(r *gin.RouterGroup, handler *KnowledgeDocHandler) {
	docs := r.Group("/knowledge-docs")
	{
		docs.GET("", handler.List)
		docs.GET("/:id", handler.Get)
		docs.POST("", handler.Create)
		docs.PUT("/:id", handler.Update)
		docs.DELETE("/:id", handler.Delete)
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
