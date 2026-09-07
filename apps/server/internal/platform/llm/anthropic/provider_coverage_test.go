package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/platform/llm"
)

// --- test helpers ---

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// pipeBody is an http response body backed by an io.Pipe. Its Close channel
// lets tests wait until the provider closed the body, i.e. streamOnce returned.
type pipeBody struct {
	*io.PipeReader
	closeOnce sync.Once
	closed    chan struct{}
}

func (b *pipeBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return b.PipeReader.Close()
}

// newPipeStreamProvider returns a provider whose stream responses are fed
// through an in-memory pipe, giving the test exact control over SSE framing.
func newPipeStreamProvider() (*Provider, *io.PipeWriter, <-chan struct{}) {
	pr, pw := io.Pipe()
	body := &pipeBody{PipeReader: pr, closed: make(chan struct{})}
	p := NewProvider("key", "http://stream.test")
	p.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       body,
		}, nil
	})}
	return p, pw, body.closed
}

func sse(data string) string { return "data: " + data + "\n\n" }

func drainChunks(ch <-chan llm.ChatChunk) (content string, toolCalls []llm.ToolCall, done bool) {
	var sb strings.Builder
	for chunk := range ch {
		if chunk.ContentDelta != "" {
			sb.WriteString(chunk.ContentDelta)
		}
		if chunk.ToolCall != nil {
			toolCalls = append(toolCalls, *chunk.ToolCall)
		}
		if chunk.Done {
			done = true
		}
	}
	return sb.String(), toolCalls, done
}

type capturedBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text"`
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Input     map[string]interface{} `json:"input"`
	ToolUseID string                 `json:"tool_use_id"`
	Content   string                 `json:"content"`
}

type capturedMessage struct {
	Role    string          `json:"role"`
	Content []capturedBlock `json:"content"`
}

type capturedTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type capturedRequest struct {
	Model       string            `json:"model"`
	System      string            `json:"system"`
	MaxTokens   int               `json:"max_tokens"`
	Stream      bool              `json:"stream"`
	Temperature float64           `json:"temperature"`
	Messages    []capturedMessage `json:"messages"`
	Tools       []capturedTool    `json:"tools"`
}

func chatMessages() []llm.ChatMessage {
	return []llm.ChatMessage{{Role: "user", Content: "hi"}}
}

// --- constructor / embed ---

func TestNewProviderDefaults(t *testing.T) {
	p := NewProvider("key", "")
	if p.baseURL != "https://api.anthropic.com/v1" {
		t.Fatalf("default baseURL = %q", p.baseURL)
	}
	p = NewProvider("key", "http://example.com/v1/")
	if p.baseURL != "http://example.com/v1" {
		t.Fatalf("trimmed baseURL = %q", p.baseURL)
	}
	if p.version != "2023-06-01" {
		t.Fatalf("version = %q", p.version)
	}
}

func TestProviderEmbedNotSupported(t *testing.T) {
	provider := NewProvider("key", "http://localhost")
	_, err := provider.Embed(context.Background(), []string{"text"})
	if err == nil {
		t.Fatal("Embed() expected not-supported error")
	}
	providerErr, ok := err.(*llm.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != llm.ProviderErrorNotSupported || providerErr.Retryable {
		t.Fatalf("unexpected provider error: %+v", providerErr)
	}
}

// --- Chat ---

func TestProviderChatRequestPayload(t *testing.T) {
	captured := make(chan capturedRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var payload capturedRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		captured <- payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_1",
			"model":"claude-test",
			"stop_reason":"tool_use",
			"content":[
				{"type":"thinking","thinking":"internal"},
				{"type":"text","text":"partial "},
				{"type":"text","text":"answer"},
				{"type":"tool_use","id":"toolu_9","name":"lookup","input":{"query":"ripples"}}
			],
			"usage":{"input_tokens":10,"output_tokens":5}
		}`))
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	resp, err := provider.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: "  be helpful  "},
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "let me check", ToolCalls: []llm.ToolCall{
				{ID: "t1", Name: "lookup", Arguments: map[string]interface{}{"q": "x"}},
			}},
			{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "t2", Name: "other"}}},
			{Role: "tool", ToolCallID: "t1", Content: "result-data"},
		},
		Tools:       []llm.ToolDefinition{{Name: "lookup", Description: "look up", InputSchema: map[string]interface{}{"type": "object"}}},
		Temperature: 0.5,
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if resp.Provider != "anthropic" || resp.Model != "claude-test" || resp.FinishReason != "tool_use" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Content != "partial answer" {
		t.Fatalf("content = %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "toolu_9" || resp.ToolCalls[0].Name != "lookup" {
		t.Fatalf("tool calls = %+v", resp.ToolCalls)
	}
	if q, _ := resp.ToolCalls[0].Arguments["query"].(string); q != "ripples" {
		t.Fatalf("tool arguments = %+v", resp.ToolCalls[0].Arguments)
	}
	if resp.TokenUsage == nil || resp.TokenUsage.TotalTokens != 15 {
		t.Fatalf("token usage = %+v", resp.TokenUsage)
	}

	payload := <-captured
	if payload.Model != "claude-3-haiku-20240307" {
		t.Fatalf("default model = %q", payload.Model)
	}
	if payload.MaxTokens != 1024 {
		t.Fatalf("default max_tokens = %d", payload.MaxTokens)
	}
	if payload.Stream {
		t.Fatal("non-stream request must not set stream=true")
	}
	if payload.Temperature != 0.5 {
		t.Fatalf("temperature = %v", payload.Temperature)
	}
	if payload.System != "be helpful" {
		t.Fatalf("system = %q", payload.System)
	}
	if len(payload.Messages) != 4 {
		t.Fatalf("messages = %+v", payload.Messages)
	}
	user := payload.Messages[0]
	if user.Role != "user" || len(user.Content) != 1 || user.Content[0].Type != "text" || user.Content[0].Text != "hi" {
		t.Fatalf("user message = %+v", user)
	}
	assistant := payload.Messages[1]
	if assistant.Role != "assistant" || len(assistant.Content) != 2 {
		t.Fatalf("assistant message = %+v", assistant)
	}
	if assistant.Content[0].Type != "text" || assistant.Content[0].Text != "let me check" {
		t.Fatalf("assistant text block = %+v", assistant.Content[0])
	}
	if assistant.Content[1].Type != "tool_use" || assistant.Content[1].ID != "t1" || assistant.Content[1].Name != "lookup" {
		t.Fatalf("assistant tool block = %+v", assistant.Content[1])
	}
	if q, _ := assistant.Content[1].Input["q"].(string); q != "x" {
		t.Fatalf("assistant tool input = %+v", assistant.Content[1].Input)
	}
	assistant2 := payload.Messages[2]
	if assistant2.Role != "assistant" || len(assistant2.Content) != 1 || assistant2.Content[0].Type != "tool_use" || assistant2.Content[0].ID != "t2" {
		t.Fatalf("assistant message without text = %+v", assistant2)
	}
	toolResult := payload.Messages[3]
	if toolResult.Role != "user" || len(toolResult.Content) != 1 {
		t.Fatalf("tool result message = %+v", toolResult)
	}
	block := toolResult.Content[0]
	if block.Type != "tool_result" || block.ToolUseID != "t1" || block.Content != "result-data" {
		t.Fatalf("tool result block = %+v", block)
	}
	if len(payload.Tools) != 1 || payload.Tools[0].Name != "lookup" || payload.Tools[0].InputSchema["type"] != "object" {
		t.Fatalf("tools = %+v", payload.Tools)
	}
}

func TestProviderChatHTTPErrorStatuses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		code   llm.ProviderErrorCode
		retry  bool
	}{
		{"bad_request", http.StatusBadRequest, llm.ProviderErrorInvalid, false},
		{"unauthorized", http.StatusUnauthorized, llm.ProviderErrorAuthFailed, false},
		{"rate_limited", http.StatusTooManyRequests, llm.ProviderErrorRateLimited, true},
		{"server_error", http.StatusInternalServerError, llm.ProviderErrorUnavailable, true},
		{"gateway_timeout", http.StatusGatewayTimeout, llm.ProviderErrorTimeout, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "boom", tc.status)
			}))
			defer srv.Close()

			provider := NewProvider("key", srv.URL)
			_, err := provider.Chat(context.Background(), llm.ChatRequest{Messages: chatMessages()})
			providerErr, ok := err.(*llm.ProviderError)
			if !ok {
				t.Fatalf("expected ProviderError, got %T", err)
			}
			if providerErr.Code != tc.code || providerErr.Retryable != tc.retry {
				t.Fatalf("provider error = %+v, want code=%s retryable=%v", providerErr, tc.code, tc.retry)
			}
			if providerErr.StatusCode != tc.status {
				t.Fatalf("status code = %d", providerErr.StatusCode)
			}
		})
	}
}

func TestProviderChatRetriesRateLimitedRequest(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"model":"claude-test","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	resp, err := provider.Chat(context.Background(), llm.ChatRequest{
		Messages: chatMessages(),
		Options: llm.RequestOptions{RetryPolicy: llm.RetryPolicy{
			MaxAttempts: 2,
			BaseDelayMs: 1,
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q", resp.Content)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

func TestProviderChatMarshalError(t *testing.T) {
	provider := NewProvider("key", "http://example.com")
	_, err := provider.Chat(context.Background(), llm.ChatRequest{
		Messages:    chatMessages(),
		Temperature: math.NaN(),
	})
	if err == nil || !strings.Contains(err.Error(), "marshal anthropic request") {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestProviderChatInvalidBaseURL(t *testing.T) {
	provider := NewProvider("key", "http://example.com/v1\x7f")
	_, err := provider.Chat(context.Background(), llm.ChatRequest{Messages: chatMessages()})
	if err == nil || !strings.Contains(err.Error(), "create anthropic request") {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestProviderChatRequestFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	provider := NewProvider("key", url)
	_, err := provider.Chat(context.Background(), llm.ChatRequest{Messages: chatMessages()})
	providerErr, ok := err.(*llm.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != llm.ProviderErrorUnavailable || !providerErr.Retryable {
		t.Fatalf("unexpected provider error: %+v", providerErr)
	}
}

func TestProviderChatResponseReadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4096")
		_, _ = w.Write([]byte(`{"model":"claude-test"}`))
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	_, err := provider.Chat(context.Background(), llm.ChatRequest{Messages: chatMessages()})
	if err == nil || !strings.Contains(err.Error(), "read anthropic response") {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestProviderChatInvalidJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Errorf("x-api-key = %q, want empty", got)
		}
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	provider := NewProvider("", srv.URL)
	_, err := provider.Chat(context.Background(), llm.ChatRequest{Messages: chatMessages()})
	if err == nil || !strings.Contains(err.Error(), "decode anthropic response") {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestProviderChatUpstreamErrorPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":{"type":"overloaded_error","message":"Overloaded"}}`))
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	_, err := provider.Chat(context.Background(), llm.ChatRequest{Messages: chatMessages()})
	providerErr, ok := err.(*llm.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != llm.ProviderErrorUpstream || providerErr.Message != "Overloaded" || providerErr.Retryable {
		t.Fatalf("unexpected provider error: %+v", providerErr)
	}
}

func TestProviderChatRequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	_, err := provider.Chat(context.Background(), llm.ChatRequest{
		Messages: chatMessages(),
		Options:  llm.RequestOptions{TimeoutMs: 40},
	})
	providerErr, ok := err.(*llm.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != llm.ProviderErrorUnavailable || !providerErr.Retryable {
		t.Fatalf("unexpected provider error: %+v", providerErr)
	}
	if providerErr.Cause == nil || !strings.Contains(providerErr.Cause.Error(), "context deadline exceeded") {
		t.Fatalf("cause = %v", providerErr.Cause)
	}
}

// --- HealthCheck ---

func TestProviderHealthCheckHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	err := provider.HealthCheck(context.Background())
	providerErr, ok := err.(*llm.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != llm.ProviderErrorUnavailable || providerErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unexpected provider error: %+v", providerErr)
	}
}

func TestProviderHealthCheckRequestFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	provider := NewProvider("", url)
	if err := provider.HealthCheck(context.Background()); err == nil {
		t.Fatal("HealthCheck() expected request failure")
	}
}

func TestProviderHealthCheckInvalidBaseURL(t *testing.T) {
	provider := NewProvider("key", "http://example.com/v1\x7f")
	err := provider.HealthCheck(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid control character") {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}

// --- ChatStream ---

func TestProviderChatStreamHappyPath(t *testing.T) {
	captured := make(chan capturedRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("anthropic-version = %q", got)
		}
		var payload capturedRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		captured <- payload

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		script := strings.Join([]string{
			"event: message_start\n",
			sse(`{"type":"message_start","message":{"id":"msg_1","model":"claude-test","type":"message"}}`),
			"event: content_block_start\n",
			sse(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			"event: content_block_delta\n",
			sse(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello "}}`),
			sse(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"brave"}}`),
			sse(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`),
			"event: content_block_stop\n",
			sse(`{"type":"content_block_stop","index":0}`),
			"event: content_block_start\n",
			sse(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}}`),
			sse(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"query\":"}}`),
			sse(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"ripples\"}"}}`),
			"event: content_block_stop\n",
			sse(`{"type":"content_block_stop","index":1}`),
			"event: message_delta\n",
			sse(`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":12}}`),
			"event: message_stop\n",
			sse(`{"type":"message_stop"}`),
		}, "")
		_, _ = io.WriteString(w, script)
		flusher.Flush()
	}))
	defer srv.Close()

	provider := NewProvider("test-key", srv.URL)
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: "A"},
			{Role: "system", Content: "B"},
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "let me check", ToolCalls: []llm.ToolCall{{ID: "t1", Name: "lookup", Arguments: map[string]interface{}{"query": "x"}}}},
			{Role: "tool", ToolCallID: "t1", Content: "result"},
		},
		Tools: []llm.ToolDefinition{{Name: "lookup", Description: "look up", InputSchema: map[string]interface{}{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	content, toolCalls, done := drainChunks(ch)
	if content != "hello brave world" {
		t.Fatalf("streamed content = %q", content)
	}
	if len(toolCalls) != 1 || toolCalls[0].ID != "toolu_1" || toolCalls[0].Name != "lookup" {
		t.Fatalf("tool calls = %+v", toolCalls)
	}
	if q, _ := toolCalls[0].Arguments["query"].(string); q != "ripples" {
		t.Fatalf("tool arguments = %+v", toolCalls[0].Arguments)
	}
	if !done {
		t.Fatal("expected done chunk after message_stop")
	}

	payload := <-captured
	if payload.Model != "claude-3-haiku-20240307" || payload.MaxTokens != 1024 || !payload.Stream {
		t.Fatalf("payload defaults: model=%q max_tokens=%d stream=%v", payload.Model, payload.MaxTokens, payload.Stream)
	}
	if payload.System != "A\nB" {
		t.Fatalf("system = %q", payload.System)
	}
	if len(payload.Messages) != 3 {
		t.Fatalf("messages = %+v", payload.Messages)
	}
	if payload.Messages[1].Role != "assistant" || len(payload.Messages[1].Content) != 2 {
		t.Fatalf("assistant message = %+v", payload.Messages[1])
	}
	if payload.Messages[1].Content[0].Type != "text" || payload.Messages[1].Content[0].Text != "let me check" {
		t.Fatalf("assistant text block = %+v", payload.Messages[1].Content[0])
	}
	if payload.Messages[1].Content[1].Type != "tool_use" || payload.Messages[1].Content[1].ID != "t1" {
		t.Fatalf("assistant tool block = %+v", payload.Messages[1].Content[1])
	}
	toolResult := payload.Messages[2]
	if toolResult.Role != "user" || toolResult.Content[0].Type != "tool_result" || toolResult.Content[0].ToolUseID != "t1" {
		t.Fatalf("tool result message = %+v", toolResult)
	}
	if len(payload.Tools) != 1 || payload.Tools[0].Name != "lookup" {
		t.Fatalf("tools = %+v", payload.Tools)
	}
}

func TestProviderChatStreamIgnoresMalformedEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Errorf("x-api-key = %q, want empty", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		script := strings.Join([]string{
			": keep-alive comment\n\n",
			"event: ping\n",
			sse(`{"type":"ping"}`),
			"data: \n\n",
			"data:\n\n",
			"ping\n\n",
			sse(`{invalid json}`),
			sse(`{"type":"content_block_start"}`),
			sse(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"ignored"}}`),
			sse(`{"type":"content_block_delta","index":0}`),
			sse(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}`),
			sse(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"stray\":true}"}}`),
			sse(`{"type":"content_block_stop","index":0}`),
			sse(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_2","name":"broken","input":{}}}`),
			sse(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":""}}`),
			sse(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"not-json"}}`),
			sse(`{"type":"content_block_stop","index":1}`),
			sse(`{"type":"unknown_event"}`),
		}, "")
		_, _ = io.WriteString(w, script)
		flusher.Flush()
	}))
	defer srv.Close()

	provider := NewProvider("", srv.URL)
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{Messages: chatMessages()})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	content, toolCalls, done := drainChunks(ch)
	if content != "" {
		t.Fatalf("streamed content = %q, want empty", content)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("tool calls = %+v", toolCalls)
	}
	tc := toolCalls[0]
	if tc.ID != "toolu_2" || tc.Name != "broken" {
		t.Fatalf("tool call = %+v", tc)
	}
	if tc.Arguments == nil || len(tc.Arguments) != 0 {
		t.Fatalf("invalid arguments should decode to empty map, got %#v", tc.Arguments)
	}
	if done {
		t.Fatal("expected no done chunk when stream ends without message_stop")
	}
}

func TestProviderChatStreamHTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "overloaded", http.StatusInternalServerError)
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{Messages: chatMessages()})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	content, _, done := drainChunks(ch)
	if !done || !strings.Contains(content, "overloaded") {
		t.Fatalf("stream = %q done=%v, want error chunk with response body", content, done)
	}
}

func TestProviderChatStreamRequestFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	provider := NewProvider("key", url)
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{Messages: chatMessages()})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	content, _, done := drainChunks(ch)
	if !done || !strings.HasPrefix(content, "stream error:") {
		t.Fatalf("stream = %q done=%v, want stream error done chunk", content, done)
	}
}

func TestProviderChatStreamMarshalError(t *testing.T) {
	provider := NewProvider("key", "http://example.com")
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{
		Messages:    chatMessages(),
		Temperature: math.NaN(),
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	content, _, done := drainChunks(ch)
	if !done || !strings.Contains(content, "marshal anthropic stream request") {
		t.Fatalf("stream = %q done=%v", content, done)
	}
}

func TestProviderChatStreamInvalidBaseURL(t *testing.T) {
	provider := NewProvider("key", "http://example.com/v1\x7f")
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{Messages: chatMessages()})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	content, _, done := drainChunks(ch)
	if !done || !strings.Contains(content, "create anthropic stream request") {
		t.Fatalf("stream = %q done=%v", content, done)
	}
}

func TestProviderChatStreamLineTooLong(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + strings.Repeat("a", 300000) + "\n\n"))
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{Messages: chatMessages()})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	content, _, done := drainChunks(ch)
	if !done || !strings.Contains(content, "read anthropic stream") {
		t.Fatalf("stream = %q done=%v", content, done)
	}
}

func TestProviderChatStreamContextCancelledDuringTextDelta(t *testing.T) {
	provider, pw, bodyClosed := newPipeStreamProvider()
	defer pw.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := provider.ChatStream(ctx, llm.ChatRequest{Messages: chatMessages()})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	if _, err := pw.Write([]byte(sse(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"a"}}`))); err != nil {
		t.Fatalf("write first delta: %v", err)
	}
	if first := <-ch; first.ContentDelta != "a" {
		t.Fatalf("first chunk = %+v", first)
	}

	cancel()
	if _, err := pw.Write([]byte(sse(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"b"}}`))); err != nil {
		t.Fatalf("write second delta: %v", err)
	}

	<-bodyClosed

	content, _, _ := drainChunks(ch)
	if strings.Contains(content, "b") {
		t.Fatalf("stream emitted delta after cancel: %q", content)
	}
}

func TestProviderChatStreamContextCancelledDuringToolCallStop(t *testing.T) {
	provider, pw, bodyClosed := newPipeStreamProvider()
	defer pw.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := provider.ChatStream(ctx, llm.ChatRequest{Messages: chatMessages()})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	if _, err := pw.Write([]byte(sse(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}}`))); err != nil {
		t.Fatalf("write block start: %v", err)
	}
	if _, err := pw.Write([]byte(sse(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"query\":"}}`))); err != nil {
		t.Fatalf("write json delta: %v", err)
	}

	cancel()
	if _, err := pw.Write([]byte(sse(`{"type":"content_block_stop","index":1}`))); err != nil {
		t.Fatalf("write block stop: %v", err)
	}

	<-bodyClosed

	_, toolCalls, _ := drainChunks(ch)
	if len(toolCalls) != 0 {
		t.Fatalf("stream emitted tool call after cancel: %+v", toolCalls)
	}
}

func TestProviderChatStreamContextCancelledDuringMessageStop(t *testing.T) {
	provider, pw, bodyClosed := newPipeStreamProvider()
	defer pw.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := provider.ChatStream(ctx, llm.ChatRequest{Messages: chatMessages()})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	if _, err := pw.Write([]byte(sse(`{"type":"message_start","message":{"id":"msg_1","model":"claude-test","type":"message"}}`))); err != nil {
		t.Fatalf("write message_start: %v", err)
	}

	cancel()
	if _, err := pw.Write([]byte(sse(`{"type":"message_stop"}`))); err != nil {
		t.Fatalf("write message_stop: %v", err)
	}

	<-bodyClosed

	for chunk := range ch {
		if chunk.Done && chunk.ContentDelta == "" && chunk.ToolCall == nil {
			t.Fatal("message_stop emitted done chunk after cancel")
		}
	}
}
