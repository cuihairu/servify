package contract

import "time"

type CreateTicketRequest struct {
	Title        string                 `json:"title" binding:"required"`
	Description  string                 `json:"description"`
	CustomerID   uint                   `json:"customer_id" binding:"required"`
	Category     string                 `json:"category"`
	Priority     string                 `json:"priority"`
	Source       string                 `json:"source"`
	Tags         string                 `json:"tags"`
	SessionID    string                 `json:"session_id"`
	CustomFields map[string]interface{} `json:"custom_fields"`
}

// CreateVisitorTicketRequest 是访客工单创建的最小面（M3 移动 SDK 配套 §10 #4）：
// 免认证通道按 session 归属租户 scope，分类/优先级/来源由服务端固定默认值
// （general/normal/chat），自定义字段与标签不开放给访客。
type CreateVisitorTicketRequest struct {
	SessionID   string `json:"session_id" binding:"required"`
	Title       string `json:"title" binding:"required"`
	Description string `json:"description"`
	AISummary   string `json:"ai_summary"`
}

type UpdateTicketRequest struct {
	Title        *string                `json:"title"`
	Description  *string                `json:"description"`
	AgentID      *uint                  `json:"agent_id"`
	Category     *string                `json:"category"`
	Priority     *string                `json:"priority"`
	Status       *string                `json:"status"`
	Tags         *string                `json:"tags"`
	DueDate      *time.Time             `json:"due_date"`
	CustomFields map[string]interface{} `json:"custom_fields"`
}

type ListTicketRequest struct {
	Page               int               `form:"page,default=1"`
	PageSize           int               `form:"page_size,default=20"`
	Status             []string          `form:"status"`
	Priority           []string          `form:"priority"`
	Category           []string          `form:"category"`
	Source             []string          `form:"source"`
	Tag                string            `form:"tag"`
	AgentID            *uint             `form:"agent_id"`
	CustomerID         *uint             `form:"customer_id"`
	Search             string            `form:"search"`
	SortBy             string            `form:"sort_by,default=created_at"`
	SortOrder          string            `form:"sort_order,default=desc"`
	CustomFieldFilters map[string]string `form:"-" json:"-"`
}

type BulkUpdateTicketRequest struct {
	TicketIDs     []uint   `json:"ticket_ids" binding:"required,min=1"`
	Status        *string  `json:"status"`
	SetTags       *string  `json:"set_tags"`
	AddTags       []string `json:"add_tags"`
	RemoveTags    []string `json:"remove_tags"`
	AgentID       *uint    `json:"agent_id"`
	UnassignAgent bool     `json:"unassign_agent"`
}

type BulkUpdateFailure struct {
	TicketID uint   `json:"ticket_id"`
	Error    string `json:"error"`
}

type BulkUpdateResult struct {
	Updated []uint              `json:"updated"`
	Failed  []BulkUpdateFailure `json:"failed"`
}

type TicketStats struct {
	Total        int64           `json:"total"`
	TodayCreated int64           `json:"today_created"`
	Pending      int64           `json:"pending"`
	Resolved     int64           `json:"resolved"`
	ByStatus     []StatusCount   `json:"by_status"`
	ByPriority   []PriorityCount `json:"by_priority"`
}

type StatusCount struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type PriorityCount struct {
	Priority string `json:"priority"`
	Count    int64  `json:"count"`
}
