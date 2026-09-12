package handlers

import (
	"errors"
	"net/http"

	"servify/apps/server/internal/models"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"

	"github.com/gin-gonic/gin"
)

// AgentGroupHandler 坐席组管理：组 CRUD、成员维护与启停
type AgentGroupHandler struct {
	service agentdelivery.AgentGroupService
}

func NewAgentGroupHandler(service agentdelivery.AgentGroupService) *AgentGroupHandler {
	return &AgentGroupHandler{service: service}
}

// ListGroups 获取坐席组列表
// @Summary 获取坐席组列表
// @Description 返回当前租户/工作区下全部坐席组（软删的不返回）
// @Tags 坐席组
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} ErrorResponse
// @Router /api/agents/groups [get]
func (h *AgentGroupHandler) ListGroups(c *gin.Context) {
	groups, err := h.service.ListAgentGroups(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list agent groups", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"groups": groups, "total": len(groups)})
}

// CreateGroupRequest 创建坐席组请求体（swag 文档用；enabled 缺省 true）。
type CreateGroupRequest struct {
	Name           string `json:"name" binding:"required" example:"售前组"`
	Description    string `json:"description" example:"售前咨询坐席"`
	Priority       int    `json:"priority" example:"10"`
	OverflowPolicy string `json:"overflow_policy" example:"global"` // global|none
	Enabled        *bool  `json:"enabled" example:"true"`
	MemberIDs      []uint `json:"member_ids"` // 坐席用户 ID 列表
	ParentID       *uint  `json:"parent_id"`  // 预留：父组
}

// CreateGroup 创建坐席组
// @Summary 创建坐席组
// @Description 创建坐席组并可同时设置成员；同名组在租户+工作区内唯一
// @Tags 坐席组
// @Accept json
// @Produce json
// @Param body body CreateGroupRequest true "坐席组内容"
// @Success 201 {object} models.AgentGroup
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/agents/groups [post]
func (h *AgentGroupHandler) CreateGroup(c *gin.Context) {
	var req CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	policy := req.OverflowPolicy
	if policy == "" {
		policy = "global"
	}
	group := &models.AgentGroup{
		Name:           req.Name,
		Description:    req.Description,
		Priority:       req.Priority,
		OverflowPolicy: policy,
		Enabled:        enabled,
		ParentID:       req.ParentID,
	}
	if err := h.service.CreateAgentGroup(c.Request.Context(), group); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, agentdelivery.ErrGroupNameRequired) {
			status = http.StatusBadRequest
		} else if errors.Is(err, agentdelivery.ErrGroupDuplicate) {
			status = http.StatusConflict
		}
		c.JSON(status, ErrorResponse{Error: "Failed to create agent group", Message: err.Error()})
		return
	}
	if len(req.MemberIDs) > 0 {
		if err := h.service.ReplaceGroupMembers(c.Request.Context(), group.ID, req.MemberIDs); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Group created but failed to set members", Message: err.Error()})
			return
		}
	}
	c.JSON(http.StatusCreated, group)
}

// GetGroup 获取单个坐席组
// @Summary 获取单个坐席组
// @Description 按 ID 返回坐席组详情
// @Tags 坐席组
// @Produce json
// @Param id path int true "坐席组 ID"
// @Success 200 {object} models.AgentGroup
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/agents/groups/{id} [get]
func (h *AgentGroupHandler) GetGroup(c *gin.Context) {
	id64, err := parseUintParam(c)
	id := uint(id64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid group id", Message: err.Error()})
		return
	}
	group, err := h.service.GetAgentGroup(c.Request.Context(), id)
	if err != nil {
		c.JSON(groupErrorStatus(err), ErrorResponse{Error: "Failed to get agent group", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, group)
}

// UpdateGroupRequest 更新坐席组请求体（字段缺省保留原值——零值语义以显式传入为准）。
type UpdateGroupRequest struct {
	Name           *string `json:"name" example:"售前组"`
	Description    *string `json:"description" example:"售前咨询坐席"`
	Priority       *int    `json:"priority" example:"10"`
	OverflowPolicy *string `json:"overflow_policy" example:"global"` // global|none
	Enabled        *bool   `json:"enabled" example:"true"`
	ParentID       *uint   `json:"parent_id"`
}

// UpdateGroup 更新坐席组
// @Summary 更新坐席组
// @Description 部分更新坐席组（指针字段缺省不覆盖；enabled=false 可停用组）
// @Tags 坐席组
// @Accept json
// @Produce json
// @Param id path int true "坐席组 ID"
// @Param body body UpdateGroupRequest true "更新内容"
// @Success 200 {object} models.AgentGroup
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/agents/groups/{id} [put]
func (h *AgentGroupHandler) UpdateGroup(c *gin.Context) {
	id64, err := parseUintParam(c)
	id := uint(id64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid group id", Message: err.Error()})
		return
	}
	group, err := h.service.GetAgentGroup(c.Request.Context(), id)
	if err != nil {
		c.JSON(groupErrorStatus(err), ErrorResponse{Error: "Failed to get agent group", Message: err.Error()})
		return
	}
	var req UpdateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	if req.Name != nil {
		group.Name = *req.Name
	}
	if req.Description != nil {
		group.Description = *req.Description
	}
	if req.Priority != nil {
		group.Priority = *req.Priority
	}
	if req.OverflowPolicy != nil {
		group.OverflowPolicy = *req.OverflowPolicy
	}
	if req.Enabled != nil {
		group.Enabled = *req.Enabled
	}
	if req.ParentID != nil {
		group.ParentID = req.ParentID
	}
	if err := h.service.UpdateAgentGroup(c.Request.Context(), group); err != nil {
		c.JSON(groupErrorStatus(err), ErrorResponse{Error: "Failed to update agent group", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, group)
}

// DeleteGroup 删除坐席组
// @Summary 删除坐席组
// @Description 软删坐席组；成员关系随之失效，排队中的目标组改为全局池兜底
// @Tags 坐席组
// @Produce json
// @Param id path int true "坐席组 ID"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/agents/groups/{id} [delete]
func (h *AgentGroupHandler) DeleteGroup(c *gin.Context) {
	id64, err := parseUintParam(c)
	id := uint(id64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid group id", Message: err.Error()})
		return
	}
	if err := h.service.DeleteAgentGroup(c.Request.Context(), id); err != nil {
		c.JSON(groupErrorStatus(err), ErrorResponse{Error: "Failed to delete agent group", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "deleted"})
}

// ReplaceGroupMembersRequest 全量替换组成员请求体。
type ReplaceGroupMembersRequest struct {
	MemberIDs []uint `json:"member_ids"` // 坐席用户 ID 全量列表
}

// ReplaceGroupMembers 全量替换坐席组成员
// @Summary 全量替换坐席组成员
// @Description 以请求体列表为准全量替换组成员（增量请先 GET 现有成员）
// @Tags 坐席组
// @Accept json
// @Produce json
// @Param id path int true "坐席组 ID"
// @Param body body ReplaceGroupMembersRequest true "成员用户 ID 全量列表"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/agents/groups/{id}/members [put]
func (h *AgentGroupHandler) ReplaceGroupMembers(c *gin.Context) {
	id64, err := parseUintParam(c)
	id := uint(id64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid group id", Message: err.Error()})
		return
	}
	var req ReplaceGroupMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	if err := h.service.ReplaceGroupMembers(c.Request.Context(), id, req.MemberIDs); err != nil {
		c.JSON(groupErrorStatus(err), ErrorResponse{Error: "Failed to replace group members", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "members updated"})
}

// ListGroupMembers 获取坐席组成员
// @Summary 获取坐席组成员
// @Description 返回组内坐席用户 ID 列表
// @Tags 坐席组
// @Produce json
// @Param id path int true "坐席组 ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/agents/groups/{id}/members [get]
func (h *AgentGroupHandler) ListGroupMembers(c *gin.Context) {
	id64, err := parseUintParam(c)
	id := uint(id64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid group id", Message: err.Error()})
		return
	}
	members, err := h.service.ListGroupMembers(c.Request.Context(), id)
	if err != nil {
		c.JSON(groupErrorStatus(err), ErrorResponse{Error: "Failed to list group members", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"member_ids": members, "total": len(members)})
}

// groupErrorStatus 把组服务错误映射到 HTTP 状态码。
func groupErrorStatus(err error) int {
	switch {
	case errors.Is(err, agentdelivery.ErrGroupNotFound):
		return http.StatusNotFound
	case errors.Is(err, agentdelivery.ErrGroupNameRequired):
		return http.StatusBadRequest
	case errors.Is(err, agentdelivery.ErrGroupDuplicate):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// RegisterAgentGroupRoutes 注册坐席组管理路由（挂在 /api/agents 组下，复用 agents 资源权限）
func RegisterAgentGroupRoutes(r *gin.RouterGroup, handler *AgentGroupHandler) {
	groups := r.Group("/agents/groups")
	{
		groups.GET("", handler.ListGroups)
		groups.POST("", handler.CreateGroup)
		groups.GET("/:id", handler.GetGroup)
		groups.PUT("/:id", handler.UpdateGroup)
		groups.DELETE("/:id", handler.DeleteGroup)
		groups.GET("/:id/members", handler.ListGroupMembers)
		groups.PUT("/:id/members", handler.ReplaceGroupMembers)
	}
}
