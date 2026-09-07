package llm

// ChatMessage is the vendor-neutral message format used by LLM providers.
//
// For multi-turn tool-calling it carries the optional pieces providers need to
// round-trip a tool exchange:
//   - ToolCalls: populated on an assistant message that requested tool execution.
//   - ToolCallID: populated on a "tool" role message to correlate it with the
//     originating assistant tool call (OpenAI) or as a tool_result block (Anthropic).
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolDefinition describes a callable tool/function exposed to the model.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

// ToolCall describes a tool request returned by a model.
type ToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// TokenUsage tracks provider token consumption.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ChatRequest is the shared input model for chat-style model requests.
type ChatRequest struct {
	Model       string           `json:"model,omitempty"`
	Messages    []ChatMessage    `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
	Options     RequestOptions   `json:"options,omitempty"`
}

// ChatResponse is the shared output model for chat-style model responses.
type ChatResponse struct {
	Content      string      `json:"content"`
	Provider     string      `json:"provider,omitempty"`
	Model        string      `json:"model,omitempty"`
	FinishReason string      `json:"finish_reason,omitempty"`
	ToolCalls    []ToolCall  `json:"tool_calls,omitempty"`
	TokenUsage   *TokenUsage `json:"token_usage,omitempty"`
}

type RequestOptions struct {
	TimeoutMs   int         `json:"timeout_ms,omitempty"`
	RetryPolicy RetryPolicy `json:"retry_policy,omitempty"`
}

type RetryPolicy struct {
	MaxAttempts   int `json:"max_attempts,omitempty"`
	BaseDelayMs   int `json:"base_delay_ms,omitempty"`
	BackoffFactor int `json:"backoff_factor,omitempty"`
}
