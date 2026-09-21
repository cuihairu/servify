// Package ragflow 提供 RAGFlow（InfiniFlow 开源 RAG 引擎）dataset HTTP API 的
// 薄客户端。API 面锚定 RAGFlow v0.27.x：Bearer 认证、`{base}/api/v1` 前缀、
// 统一包裹 `{"code":<int>,"data":...,"message":...}`（成功 code==0）。
package ragflow

import (
	"context"
	"time"
)

// Config 是 RAGFlow HTTP 客户端配置。
type Config struct {
	BaseURL string        // 如 http://localhost:9380（不含 /api/v1 前缀）
	APIKey  string        // 形如 ragflow-xxxx，空则不发 Authorization 头
	Timeout time.Duration // 零值取默认 30s
}

// ClientInterface 是 knowledgeprovider driver 依赖的客户端 seam。
type ClientInterface interface {
	GetDataset(ctx context.Context, datasetID string) (*Dataset, error)
	Retrieve(ctx context.Context, req *RetrieveRequest) (*RetrieveResponse, error)
	ListDocuments(ctx context.Context, datasetID, name string) ([]Document, error)
	UploadDocument(ctx context.Context, datasetID, name, content string) (*Document, error)
	ParseDocuments(ctx context.Context, datasetID string, documentIDs []string) error
	DeleteDocuments(ctx context.Context, datasetID string, documentIDs []string) error
	HealthCheck(ctx context.Context, datasetID string) error
}

// Dataset 是 RAGFlow 知识库（dataset）对象的最小视图。
type Dataset struct {
	ID   string
	Name string
}

// Document 是 RAGFlow 文档对象的最小视图。Run 为解析状态
// （UNSTART/RUNNING/CANCEL/DONE/FAIL 等），上传后不自动解析。
type Document struct {
	ID   string
	Name string
	Run  string
}

// RetrieveRequest 是纯检索请求（不带 LLM 生成）。TopK 映射 RAGFlow 的
// knn_top_k（旧字段 top_k 已废弃）。
type RetrieveRequest struct {
	Question            string   `json:"question"`
	DatasetIDs          []string `json:"dataset_ids"`
	PageSize            int      `json:"page_size,omitempty"`
	SimilarityThreshold float64  `json:"similarity_threshold,omitempty"`
	TopK                int      `json:"knn_top_k,omitempty"`
}

// RetrieveResponse 是检索结果。
type RetrieveResponse struct {
	Total  int     `json:"total"`
	Chunks []Chunk `json:"chunks"`
}

// Chunk 是检索单元。DocumentKeyword 为来源文件名。
type Chunk struct {
	Content         string  `json:"content"`
	DocumentID      string  `json:"document_id"`
	DocumentKeyword string  `json:"document_keyword"`
	DatasetID       string  `json:"dataset_id"`
	Similarity      float64 `json:"similarity"`
}
