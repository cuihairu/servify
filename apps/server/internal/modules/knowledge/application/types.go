package application

import "time"

type CreateDocumentRequest struct {
	ID       string
	Title    string
	Content  string
	Category string
	Tags     []string
	IsPublic bool
	// SourceID 来源登记归属（B3-1a §8.1），0=未挂来源。
	SourceID uint
}

type UpdateDocumentRequest struct {
	Title    *string
	Content  *string
	Category *string
	Tags     *[]string
	IsPublic *bool
	// SourceID 调整来源归属；nil=不改动。
	SourceID *uint
}

type ListDocumentsFilter struct {
	Page       int
	PageSize   int
	Category   string
	Search     string
	PublicOnly bool
}

type QueueIndexJobRequest struct {
	JobID      string
	DocumentID string
}

type RunIndexJobRequest struct {
	JobID string
}

type IndexJobResult struct {
	JobID      string
	DocumentID string
	Status     string
	Error      string
	// DocumentVersion 本次执行索引的文档版本（B3-1a §8.2）。
	DocumentVersion int
	CompletedAt     *time.Time
}

// CreateSourceRequest 来源登记入参（B3-1a §8.1）。Type 必须属于
// domain.SourceTypes（markdown/website/pdf/faq/api）。
type CreateSourceRequest struct {
	Name        string
	Type        string
	Description string
}

type ListSourcesFilter struct {
	Type string
}

// IndexJobDTO 索引任务读口条目（B3-1a §8.2：状态可见 + 版本关联）。
type IndexJobDTO struct {
	ID              string
	DocumentID      string
	Status          string
	Error           string
	DocumentVersion int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CompletedAt     *time.Time
}
