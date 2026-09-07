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
