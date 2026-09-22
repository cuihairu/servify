package application

import (
	"time"

	"servify/apps/server/internal/platform/knowledgeprovider"
	"servify/apps/server/internal/platform/llm"
)

// TaskType identifies the business intent of an AI request.
type TaskType string

const (
	TaskTypeQA      TaskType = "qa"
	TaskTypeSummary TaskType = "summary"
	TaskTypeSuggest TaskType = "suggest"
)

// RetrievalPolicy controls optional knowledge retrieval behavior.
type RetrievalPolicy struct {
	Enabled   bool
	TopK      int
	Threshold float64
	Strategy  string
}

// ToolPolicy controls whether tools may be used by the orchestrator.
type ToolPolicy struct {
	Enabled      bool
	AllowedTools []string
	MaxSteps     int
}

// AIRequest is the vendor-neutral input model for AI orchestration.
type AIRequest struct {
	TenantID        string
	TaskType        TaskType
	ConversationID  string
	UserID          string
	Query           string
	SystemPrompt    string
	Messages        []llm.ChatMessage
	RetrievalPolicy RetrievalPolicy
	ToolPolicy      ToolPolicy
	// Model 指定聊天模型；空值走 provider 侧默认（不序列化到请求体）。
	Model string
	// Temperature 采样温度透传；零值不写入请求体，同样落回 provider 默认。
	Temperature float64
	// MaxTokens 单次补全上限；零值由 provider 侧默认（openai 不下发、
	// anthropic 兜底 1024）。
	MaxTokens int
	// TimeoutMs 出站 LLM 调用超时（毫秒）；零值走 provider 侧 30s 兜底。
	// 经 ChatRequest.Options.TimeoutMs 生效，调用方更紧的外层 ctx 超时
	// （WS 20s / REST 30s）依然先到先断。
	TimeoutMs int
}

// AIResponse is the vendor-neutral output model for AI orchestration.
type AIResponse struct {
	Content      string                           `json:"content"`
	Model        string                           `json:"model,omitempty"`
	Provider     string                           `json:"provider,omitempty"`
	Sources      []knowledgeprovider.KnowledgeHit `json:"sources,omitempty"`
	TokenUsage   *llm.TokenUsage                  `json:"token_usage,omitempty"`
	FinishReason string                           `json:"finish_reason,omitempty"`
	Latency      time.Duration                    `json:"latency"`
	Truncated    bool                             `json:"truncated,omitempty"`
}
