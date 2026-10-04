package domain

import "time"

type Document struct {
	ID         string
	ProviderID string
	ExternalID string
	Title      string
	Content    string
	Category   string
	Tags       []string
	IsPublic   bool
	// SourceID 来源登记归属（B3-1a §8.1），0=未挂来源。
	SourceID uint
	// Version 文档版本号（B3-1a §8.2），内容变更自增。
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type IndexJobStatus string

const (
	IndexJobQueued  IndexJobStatus = "queued"
	IndexJobRunning IndexJobStatus = "running"
	IndexJobDone    IndexJobStatus = "done"
	IndexJobFailed  IndexJobStatus = "failed"
)

type IndexJob struct {
	ID         string
	DocumentID string
	Status     IndexJobStatus
	Error      string
	// DocumentVersion 任务完成时索引的文档版本（B3-1a §8.2），0=未执行到回存。
	DocumentVersion int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CompletedAt     *time.Time
}

// Source 来源登记实体（B3-1a §8.1）。Type 取值见 SourceTypes。
type Source struct {
	ID          uint
	Name        string
	Type        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SourceTypes 来源类型收口（§8.1：markdown/website/PDF/FAQ/API）。
var SourceTypes = []string{"markdown", "website", "pdf", "faq", "api"}
