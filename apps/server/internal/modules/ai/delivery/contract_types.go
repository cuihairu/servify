package delivery

import (
	"time"

	"servify/apps/server/pkg/weknora"
)

// AIResponse AI 查询响应契约。legacy internal/services 通过类型别名引用同一份定义，
// 保证 modules 与 legacy 两侧是同一个类型而不是两份平行 DTO。
//
// Sources / Strategy / NextAction / HandoffReason 是编排路径的附加输出：
// REST enhanced 响应与 WS ai-response 帧共用，全部 omitempty，legacy
// 消费方按零值省略向后兼容。
type AIResponse struct {
	Content    string  `json:"content"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source"`
	// Sources 知识库命中的引用来源（答案带引用）；零命中或非检索路径省略。
	Sources []weknora.SearchResult `json:"sources,omitempty"`
	// Strategy 本条答案的产生方式："weknora"/"dify"（检索增强）、"llm"
	// （零命中由 LLM 直接作答）、"fallback"（编排失败走 legacy 兜底）、
	// "transfer"（关键词触发的转人工）。
	Strategy string `json:"strategy,omitempty"`
	// NextAction 建议的下一步动作（建议，不代替执行方决策）：当前仅
	// "handoff"（置信不足，建议转人工）；其余场景省略 = 正常作答。
	NextAction string `json:"next_action,omitempty"`
	// HandoffReason NextAction=handoff 时的原因码，当前固定 "low_confidence"。
	HandoffReason string `json:"handoff_reason,omitempty"`
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

// EnhancedAIResponse 增强的 AI 响应契约。Strategy 取值见 AIResponse.Strategy。
type EnhancedAIResponse struct {
	*AIResponse
	Sources    []weknora.SearchResult `json:"sources,omitempty"`
	Strategy   string                 `json:"strategy"`
	Duration   time.Duration          `json:"duration"`
	TokensUsed int                    `json:"tokens_used,omitempty"`
}
