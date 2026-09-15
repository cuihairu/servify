package delivery

import (
	"time"

	"servify/apps/server/pkg/weknora"
)

// AIResponse AI 查询响应契约。legacy internal/services 通过类型别名引用同一份定义，
// 保证 modules 与 legacy 两侧是同一个类型而不是两份平行 DTO。
type AIResponse struct {
	Content    string  `json:"content"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source"`
}

// AIMetrics AI 服务指标契约。
type AIMetrics struct {
	QueryCount                  int64         `json:"query_count"`
	SuccessCount                int64         `json:"success_count"`
	KnowledgeProviderUsageCount int64         `json:"knowledge_provider_usage_count"`
	DifyUsageCount              int64         `json:"dify_usage_count"`
	WeKnoraUsageCount           int64         `json:"weknora_usage_count"`
	FallbackUsageCount          int64         `json:"fallback_usage_count"`
	AverageLatency              time.Duration `json:"average_latency"`
	KnowledgeProviderLatency    time.Duration `json:"knowledge_provider_latency"`
	WeKnoraLatency              time.Duration `json:"weknora_latency"`
	OpenAILatency               time.Duration `json:"openai_latency"`
	ActiveKnowledgeProvider     string        `json:"active_knowledge_provider,omitempty"`
}

// EnhancedAIResponse 增强的 AI 响应契约。Strategy 取值 "weknora"、"dify"、"fallback"、"hybrid"、"transfer"。
type EnhancedAIResponse struct {
	*AIResponse
	Sources    []weknora.SearchResult `json:"sources,omitempty"`
	Strategy   string                 `json:"strategy"`
	Duration   time.Duration          `json:"duration"`
	TokensUsed int                    `json:"tokens_used,omitempty"`
}
