package llm

import "context"

// ChatChunk is the streaming unit returned by ChatStream. ContentDelta 是
// 模型输出增量——provider 流式失败时只发终末空增量（Done=true，无内容），
// 错误文本绝不进 ContentDelta（下游把它逐字推给访客）。
type ChatChunk struct {
	ContentDelta string    `json:"content_delta"`
	ToolCall     *ToolCall `json:"tool_call,omitempty"`
	Done         bool      `json:"done,omitempty"`
}

// LLMProvider defines the contract for model providers.
type LLMProvider interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error)
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	HealthCheck(ctx context.Context) error
}
