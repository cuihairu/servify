package application

// 以下类型是 knowledge 模块对 HTTP 层暴露的契约。legacy internal/services 通过
// 类型别名引用同一份定义；绑定 tag 保持不变，wire contract 不受迁移影响。

// KnowledgeDocCreateRequest 创建知识文档请求契约。
type KnowledgeDocCreateRequest struct {
	Title    string   `json:"title" binding:"required"`
	Content  string   `json:"content" binding:"required"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
	IsPublic bool     `json:"is_public"`
}

// KnowledgeDocUpdateRequest 更新知识文档请求契约。
type KnowledgeDocUpdateRequest struct {
	Title    *string   `json:"title"`
	Content  *string   `json:"content"`
	Category *string   `json:"category"`
	Tags     *[]string `json:"tags"`
	IsPublic *bool     `json:"is_public"`
}

// KnowledgeDocListRequest 知识文档列表请求契约。
type KnowledgeDocListRequest struct {
	Page       int    `form:"page"`
	PageSize   int    `form:"page_size"`
	Category   string `form:"category"`
	Search     string `form:"search"`
	PublicOnly bool   `form:"public_only"`
}
