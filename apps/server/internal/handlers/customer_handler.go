package handlers

import (
	"net/http"
	"strconv"

	customerapi "servify/apps/server/internal/modules/customer/api"
	customerdelivery "servify/apps/server/internal/modules/customer/delivery"
	auditplatform "servify/apps/server/internal/platform/audit"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// CustomerHandler 客户管理处理器
type CustomerHandler struct {
	customerService customerdelivery.HandlerService
	logger          *logrus.Logger
}

// NewCustomerHandler 创建客户处理器
func NewCustomerHandler(customerService customerdelivery.HandlerService, logger *logrus.Logger) *CustomerHandler {
	return &CustomerHandler{
		customerService: customerService,
		logger:          logger,
	}
}

// CreateCustomer 创建客户
// @Summary 创建客户
// @Description 创建新的客户账户
// @Tags 客户管理
// @Accept json
// @Produce json
// @Param customer body customerapi.CustomerCreateRequest true "客户信息"
// @Success 201 {object} models.User
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/customers [post]
func (h *CustomerHandler) CreateCustomer(c *gin.Context) {
	var req customerapi.CustomerCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	customer, err := h.customerService.CreateCustomer(c.Request.Context(), &req)
	if err != nil {
		if h.logger != nil {
			h.logger.Errorf("Failed to create customer: %v", err)
		}
		status := http.StatusInternalServerError
		if isInvalidInputError(err) {
			status = http.StatusBadRequest
		} else if isConflictError(err) {
			status = http.StatusConflict
		}
		c.JSON(status, ErrorResponse{
			Error:   "Failed to create customer",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, customer)
}

// GetCustomer 获取客户详情
// @Summary 获取客户详情
// @Description 根据ID获取客户的详细信息
// @Tags 客户管理
// @Accept json
// @Produce json
// @Param id path int true "客户ID"
// @Success 200 {object} models.User
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/customers/{id} [get]
func (h *CustomerHandler) GetCustomer(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid customer ID",
			Message: "ID must be a valid number",
		})
		return
	}

	customer, err := h.customerService.GetCustomerByID(c.Request.Context(), uint(id))
	if err != nil {
		if h.logger != nil {
			h.logger.Errorf("Failed to get customer %d: %v", id, err)
		}
		status := http.StatusInternalServerError
		label := "Failed to get customer"
		if isNotFoundError(err) {
			status = http.StatusNotFound
			label = "Customer not found"
		}
		c.JSON(status, ErrorResponse{
			Error:   label,
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, customer)
}

// UpdateCustomer 更新客户信息
// @Summary 更新客户信息
// @Description 更新客户的基本信息和扩展信息
// @Tags 客户管理
// @Accept json
// @Produce json
// @Param id path int true "客户ID"
// @Param customer body customerapi.CustomerUpdateRequest true "更新信息"
// @Success 200 {object} models.User
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/customers/{id} [put]
func (h *CustomerHandler) UpdateCustomer(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid customer ID",
			Message: "ID must be a valid number",
		})
		return
	}

	var req customerapi.CustomerUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	h.setCustomerAuditSnapshot(c, uint(id), true)

	customer, err := h.customerService.UpdateCustomer(c.Request.Context(), uint(id), &req)
	if err != nil {
		if h.logger != nil {
			h.logger.Errorf("Failed to update customer %d: %v", id, err)
		}
		status := http.StatusInternalServerError
		if isNotFoundError(err) {
			status = http.StatusNotFound
		} else if isInvalidInputError(err) {
			status = http.StatusBadRequest
		}
		c.JSON(status, ErrorResponse{
			Error:   "Failed to update customer",
			Message: err.Error(),
		})
		return
	}

	auditplatform.SetAfter(c, customer)
	c.JSON(http.StatusOK, customer)
}

// RevokeCustomerTokens 强制失效客户已有 token
// @Summary 强制失效客户 token
// @Description 提升 token_version 并刷新 token_valid_after，使该客户旧 token 失效
// @Tags 客户管理
// @Accept json
// @Produce json
// @Param id path int true "客户ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/customers/{id}/revoke-tokens [post]
func (h *CustomerHandler) RevokeCustomerTokens(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid customer ID",
			Message: "ID must be a valid number",
		})
		return
	}

	h.setCustomerAuditSnapshot(c, uint(id), true)

	version, err := h.customerService.RevokeCustomerTokens(c.Request.Context(), uint(id))
	if err != nil {
		if h.logger != nil {
			h.logger.Errorf("Failed to revoke customer %d tokens: %v", id, err)
		}
		status := http.StatusInternalServerError
		if isNotFoundError(err) {
			status = http.StatusNotFound
		} else if isInvalidInputError(err) {
			status = http.StatusBadRequest
		}
		c.JSON(status, ErrorResponse{
			Error:   "Failed to revoke customer tokens",
			Message: err.Error(),
		})
		return
	}

	h.setCustomerAuditSnapshot(c, uint(id), false)

	c.JSON(http.StatusOK, gin.H{
		"message":       "Customer tokens revoked successfully",
		"customer_id":   id,
		"token_version": version,
	})
}

// ListCustomers 获取客户列表
// @Summary 获取客户列表
// @Description 获取客户列表，支持分页和过滤
// @Tags 客户管理
// @Accept json
// @Produce json
// @Param page query int false "页码"
// @Param page_size query int false "每页大小"
// @Param search query string false "搜索关键词"
// @Param industry query []string false "行业过滤"
// @Param source query []string false "来源过滤"
// @Param priority query []string false "优先级过滤"
// @Param status query []string false "状态过滤"
// @Param tags query string false "标签过滤"
// @Param sort_by query string false "排序字段"
// @Param sort_order query string false "排序方向"
// @Success 200 {object} PaginatedResponse{data=[]customerapi.CustomerInfo}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/customers [get]
func (h *CustomerHandler) ListCustomers(c *gin.Context) {
	var req customerapi.CustomerListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid query parameters",
			Message: err.Error(),
		})
		return
	}

	customers, total, err := h.customerService.ListCustomers(c.Request.Context(), &req)
	if err != nil {
		if h.logger != nil {
			h.logger.Errorf("Failed to list customers: %v", err)
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to list customers",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, PaginatedResponse{
		Data:     customers,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
}

// GetCustomerActivity 获取客户活动记录
// @Summary 获取客户活动记录
// @Description 获取客户的最近活动记录，包括会话、工单等
// @Tags 客户管理
// @Accept json
// @Produce json
// @Param id path int true "客户ID"
// @Param limit query int false "记录数量限制"
// @Success 200 {object} customerapi.CustomerActivity
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/customers/{id}/activity [get]
func (h *CustomerHandler) GetCustomerActivity(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid customer ID",
			Message: "ID must be a valid number",
		})
		return
	}

	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		limit = 10
	}

	activity, err := h.customerService.GetCustomerActivity(c.Request.Context(), uint(id), limit)
	if err != nil {
		if h.logger != nil {
			h.logger.Errorf("Failed to get customer activity %d: %v", id, err)
		}
		status := http.StatusInternalServerError
		if isNotFoundError(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{
			Error:   "Failed to get customer activity",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, activity)
}

// AddCustomerNote 添加客户备注
// @Summary 添加客户备注
// @Description 为客户添加备注信息
// @Tags 客户管理
// @Accept json
// @Produce json
// @Param id path int true "客户ID"
// @Param note body map[string]string true "备注信息"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/customers/{id}/notes [post]
func (h *CustomerHandler) AddCustomerNote(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid customer ID",
			Message: "ID must be a valid number",
		})
		return
	}

	var req struct {
		Note string `json:"note" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	// 从上下文获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Error:   "Unauthorized",
			Message: "User not authenticated",
		})
		return
	}

	h.setCustomerAuditSnapshot(c, uint(id), true)

	if err := h.customerService.AddCustomerNote(c.Request.Context(), uint(id), req.Note, userID.(uint)); err != nil {
		if h.logger != nil {
			h.logger.Errorf("Failed to add note to customer %d: %v", id, err)
		}
		status := http.StatusInternalServerError
		if isNotFoundError(err) {
			status = http.StatusNotFound
		} else if isInvalidInputError(err) {
			status = http.StatusBadRequest
		}
		c.JSON(status, ErrorResponse{
			Error:   "Failed to add customer note",
			Message: err.Error(),
		})
		return
	}
	if customer, err := h.customerService.GetCustomerByID(c.Request.Context(), uint(id)); err == nil && customer != nil {
		auditplatform.SetAfter(c, gin.H{
			"customer":   customer,
			"added_note": req.Note,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "Note added successfully",
		"customer_id": id,
	})
}

// UpdateCustomerTags 更新客户标签
// @Summary 更新客户标签
// @Description 更新客户的标签信息
// @Tags 客户管理
// @Accept json
// @Produce json
// @Param id path int true "客户ID"
// @Param tags body map[string][]string true "标签信息"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/customers/{id}/tags [put]
func (h *CustomerHandler) UpdateCustomerTags(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid customer ID",
			Message: "ID must be a valid number",
		})
		return
	}

	var req struct {
		Tags []string `json:"tags" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request body",
			Message: err.Error(),
		})
		return
	}

	h.setCustomerAuditSnapshot(c, uint(id), true)

	if err := h.customerService.UpdateCustomerTags(c.Request.Context(), uint(id), req.Tags); err != nil {
		if h.logger != nil {
			h.logger.Errorf("Failed to update tags for customer %d: %v", id, err)
		}
		status := http.StatusInternalServerError
		if isNotFoundError(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, ErrorResponse{
			Error:   "Failed to update customer tags",
			Message: err.Error(),
		})
		return
	}
	if customer, err := h.customerService.GetCustomerByID(c.Request.Context(), uint(id)); err == nil && customer != nil {
		auditplatform.SetAfter(c, gin.H{
			"customer": customer,
			"tags":     req.Tags,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "Tags updated successfully",
		"customer_id": id,
		"tags":        req.Tags,
	})
}

func (h *CustomerHandler) setCustomerAuditSnapshot(c *gin.Context, customerID uint, before bool) {
	if h == nil || h.customerService == nil || c == nil || customerID == 0 {
		return
	}
	customer, err := h.customerService.GetCustomerByID(c.Request.Context(), customerID)
	if err != nil || customer == nil {
		return
	}
	if before {
		auditplatform.SetBefore(c, customer)
		return
	}
	auditplatform.SetAfter(c, customer)
}

// GetCustomerStats 获取客户统计
// @Summary 获取客户统计
// @Description 获取客户相关的统计数据
// @Tags 客户管理
// @Accept json
// @Produce json
// @Success 200 {object} customerapi.CustomerStats
// @Failure 500 {object} ErrorResponse
// @Router /api/customers/stats [get]
func (h *CustomerHandler) GetCustomerStats(c *gin.Context) {
	stats, err := h.customerService.GetCustomerStats(c.Request.Context())
	if err != nil {
		if h.logger != nil {
			h.logger.Errorf("Failed to get customer stats: %v", err)
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to get customer statistics",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// RegisterCustomerRoutes 注册客户管理相关路由
func RegisterCustomerRoutes(r *gin.RouterGroup, handler *CustomerHandler) {
	customers := r.Group("/customers")
	{
		customers.POST("", handler.CreateCustomer)
		customers.GET("", handler.ListCustomers)
		customers.GET("/stats", handler.GetCustomerStats)
		customers.GET("/:id", handler.GetCustomer)
		customers.PUT("/:id", handler.UpdateCustomer)
		customers.POST("/:id/revoke-tokens", handler.RevokeCustomerTokens)
		customers.GET("/:id/activity", handler.GetCustomerActivity)
		customers.POST("/:id/notes", handler.AddCustomerNote)
		customers.PUT("/:id/tags", handler.UpdateCustomerTags)
	}
}
