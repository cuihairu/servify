package handlers

import (
	"net/http"
	"strconv"

	macrodelivery "servify/apps/server/internal/modules/macro/delivery"

	"github.com/gin-gonic/gin"
)

// MacroHandler 管理宏/模板
//
//nolint:revive
type MacroHandler struct {
	service macrodelivery.HandlerService
}

func NewMacroHandler(service macrodelivery.HandlerService) *MacroHandler {
	return &MacroHandler{service: service}
}

func (h *MacroHandler) List(c *gin.Context) {
	macros, err := h.service.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list macros", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, macros)
}

func (h *MacroHandler) Create(c *gin.Context) {
	var req macrodelivery.MacroCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	macro, err := h.service.Create(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to create macro", Message: err.Error()})
		return
	}
	c.JSON(http.StatusCreated, macro)
}

func (h *MacroHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: err.Error()})
		return
	}
	var req macrodelivery.MacroUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	macro, err := h.service.Update(c.Request.Context(), uint(id), &req)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundError(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{Error: "Failed to update macro", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, macro)
}

func (h *MacroHandler) Delete(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: err.Error()})
		return
	}
	if err := h.service.Delete(c.Request.Context(), uint(id)); err != nil {
		status := http.StatusBadRequest
		if isNotFoundError(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{Error: "Failed to delete macro", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "deleted"})
}

func (h *MacroHandler) Apply(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: err.Error()})
		return
	}
	var req struct {
		TicketID uint `json:"ticket_id" binding:"required"`
		UserID   uint `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	comment, err := h.service.ApplyToTicket(c.Request.Context(), uint(id), req.TicketID, req.UserID)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundError(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{Error: "Failed to apply macro", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, comment)
}

// RegisterMacroRoutes 注册宏路由
func RegisterMacroRoutes(r *gin.RouterGroup, handler *MacroHandler) {
	macros := r.Group("/macros")
	{
		macros.GET("", handler.List)
		macros.POST("", handler.Create)
		macros.PUT("/:id", handler.Update)
		macros.DELETE("/:id", handler.Delete)
		macros.POST("/:id/apply", handler.Apply)
	}
}
