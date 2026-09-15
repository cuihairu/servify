package customerapi

import (
	"servify/apps/server/internal/models"
)

// 以下类型是 customer 模块对 HTTP 层暴露的契约。legacy internal/services 通过
// 类型别名引用同一份定义；绑定 tag 保持不变，wire contract 不受迁移影响。

// CustomerCreateRequest 创建客户请求契约。
type CustomerCreateRequest struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Company  string `json:"company"`
	Industry string `json:"industry"`
	Source   string `json:"source"`
	Tags     string `json:"tags"`
	Notes    string `json:"notes"`
	Priority string `json:"priority"`
}

// CustomerUpdateRequest 更新客户请求契约。
type CustomerUpdateRequest struct {
	Name     *string `json:"name"`
	Phone    *string `json:"phone"`
	Company  *string `json:"company"`
	Industry *string `json:"industry"`
	Source   *string `json:"source"`
	Tags     *string `json:"tags"`
	Notes    *string `json:"notes"`
	Priority *string `json:"priority"`
	Status   *string `json:"status"`
}

// CustomerListRequest 客户列表请求契约。
type CustomerListRequest struct {
	Page      int      `form:"page,default=1"`
	PageSize  int      `form:"page_size,default=20"`
	Search    string   `form:"search"`
	Industry  []string `form:"industry"`
	Source    []string `form:"source"`
	Priority  []string `form:"priority"`
	Status    []string `form:"status"`
	Tags      string   `form:"tags"`
	SortBy    string   `form:"sort_by,default=created_at"`
	SortOrder string   `form:"sort_order,default=desc"`
}

// CustomerInfo 客户信息契约。
type CustomerInfo struct {
	models.User
	Company  string `json:"company"`
	Industry string `json:"industry"`
	Source   string `json:"source"`
	Tags     string `json:"tags"`
	Notes    string `json:"notes"`
	Priority string `json:"priority"`
}

// CustomerActivity 客户活动记录契约。
type CustomerActivity struct {
	CustomerID     uint             `json:"customer_id"`
	RecentSessions []models.Session `json:"recent_sessions"`
	RecentTickets  []models.Ticket  `json:"recent_tickets"`
	RecentMessages []models.Message `json:"recent_messages"`
}

// CustomerStats 客户统计信息契约。
type CustomerStats struct {
	Total       int64                   `json:"total"`
	Active      int64                   `json:"active"`
	NewThisWeek int64                   `json:"new_this_week"`
	BySource    []CustomerSourceCount   `json:"by_source"`
	ByIndustry  []CustomerIndustryCount `json:"by_industry"`
	ByPriority  []CustomerPriorityCount `json:"by_priority"`
}

// CustomerSourceCount 按来源统计契约。
type CustomerSourceCount struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

// CustomerIndustryCount 按行业统计契约。
type CustomerIndustryCount struct {
	Industry string `json:"industry"`
	Count    int64  `json:"count"`
}

// CustomerPriorityCount 按优先级统计契约。
type CustomerPriorityCount struct {
	Priority string `json:"priority"`
	Count    int64  `json:"count"`
}
