package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/llm"
)

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

func TestNewProviderDefaults(t *testing.T) {
	p := NewProvider("key", "")
	if p.baseURL != "https://api.openai.com/v1" {
		t.Fatalf("default baseURL = %q", p.baseURL)
	}

	p = NewProvider("key", "http://example.com/v1/")
	if p.baseURL != "http://example.com/v1" {
		t.Fatalf("trimmed baseURL = %q", p.baseURL)
	}
}

func TestProviderChatDefaultsModelAndOmitsAuthWithoutKey(t *testing.T) {
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var payload struct {
			Model string `json:"model"`
		}
		_ = decodeJSONBody(r, &payload)
		gotModel = payload.Model
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	provider := NewProvider("", srv.URL)
	resp, err := provider.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization header = %q, want empty", gotAuth)
	}
	if gotModel != config.DefaultOpenAIModel {
		t.Fatalf("default model = %q, want %q", gotModel, config.DefaultOpenAIModel)
	}
	if resp.Model != config.DefaultOpenAIModel {
		t.Fatalf("response model = %q", resp.Model)
	}
	if resp.TokenUsage != nil {
		t.Fatalf("expected nil token usage, got %+v", resp.TokenUsage)
	}
}

func TestProviderChatRoundTripsToolMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Role       string `json:"role"`
				ToolCallID string `json:"tool_call_id"`
				ToolCalls  []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"messages"`
		}
		if err := decodeJSONBody(r, &payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(payload.Messages) != 2 {
			t.Fatalf("messages = %d", len(payload.Messages))
		}
		assistant := payload.Messages[0]
		if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Function.Name != "lookup" {
			t.Fatalf("assistant tool calls = %+v", assistant.ToolCalls)
		}
		if !strings.Contains(assistant.ToolCalls[0].Function.Arguments, "query") {
			t.Fatalf("tool arguments = %q", assistant.ToolCalls[0].Function.Arguments)
		}
		tool := payload.Messages[1]
		if tool.Role != "tool" || tool.ToolCallID != "call-1" {
			t.Fatalf("tool message = %+v", tool)
		}
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"done"}}]}`))
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	resp, err := provider.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "lookup", Arguments: map[string]interface{}{"query": "hi"}}}},
			{Role: "tool", ToolCallID: "call-1", Content: "result"},
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "done" {
		t.Fatalf("content = %q", resp.Content)
	}
}

func TestProviderChatInvalidJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	if _, err := provider.Chat(context.Background(), llm.ChatRequest{Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}}}); err == nil {
		t.Fatal("Chat() expected decode error")
	}
}

func TestProviderChatUpstreamErrorPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":{"message":"insufficient quota"}}`))
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	_, err := provider.Chat(context.Background(), llm.ChatRequest{Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("Chat() expected upstream error")
	}
	providerErr, ok := err.(*llm.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != llm.ProviderErrorUpstream || providerErr.Message != "insufficient quota" {
		t.Fatalf("unexpected provider error: %+v", providerErr)
	}
}

func TestProviderChatEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	_, err := provider.Chat(context.Background(), llm.ChatRequest{Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("Chat() expected empty-choices error")
	}
	if !strings.Contains(err.Error(), "empty choices") {
		t.Fatalf("error = %v", err)
	}
}

func TestProviderChatRequestFailureIsRetryableProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	provider := NewProvider("key", url)
	_, err := provider.Chat(context.Background(), llm.ChatRequest{Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("Chat() expected request failure")
	}
	providerErr, ok := err.(*llm.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != llm.ProviderErrorUnavailable || !providerErr.Retryable {
		t.Fatalf("unexpected provider error: %+v", providerErr)
	}
}

func TestProviderHealthCheckHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	if err := provider.HealthCheck(context.Background()); err == nil {
		t.Fatal("HealthCheck() expected error for 401")
	}
}

func TestProviderHealthCheckRequestFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	provider := NewProvider("key", url)
	if err := provider.HealthCheck(context.Background()); err == nil {
		t.Fatal("HealthCheck() expected request failure")
	}
}

func TestProviderChatStreamIgnoresMalformedLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(": keep-alive comment\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {invalid json}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"choices\":[]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		flusher.Flush()
		// Stream ends without [DONE]: provider must still emit a done chunk.
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	var content strings.Builder
	done := false
	for chunk := range ch {
		if chunk.ContentDelta != "" {
			content.WriteString(chunk.ContentDelta)
		}
		if chunk.Done {
			done = true
		}
	}
	if content.String() != "ok" {
		t.Fatalf("streamed content = %q, want %q", content.String(), "ok")
	}
	if !done {
		t.Fatal("expected done chunk when stream ends without [DONE]")
	}
}

func TestProviderChatStreamRequestFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	provider := NewProvider("key", url)
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	var last llm.ChatChunk
	for chunk := range ch {
		last = chunk
	}
	if !last.Done || !strings.HasPrefix(last.ContentDelta, "stream error:") {
		t.Fatalf("last chunk = %+v, want stream error done chunk", last)
	}
}

func TestDecodeToolCallsInvalidArguments(t *testing.T) {
	calls := []struct {
		ID       string "json:\"id\""
		Type     string "json:\"type\""
		Function struct {
			Name      string "json:\"name\""
			Arguments string "json:\"arguments\""
		} "json:\"function\""
	}{
		{ID: "call-1", Function: struct {
			Name      string "json:\"name\""
			Arguments string "json:\"arguments\""
		}{Name: "broken", Arguments: "not-json"}},
	}
	out := decodeToolCalls(calls)
	if len(out) != 1 {
		t.Fatalf("tool calls = %d", len(out))
	}
	if out[0].ID != "call-1" || out[0].Name != "broken" {
		t.Fatalf("tool call = %+v", out[0])
	}
	if len(out[0].Arguments) != 0 {
		t.Fatalf("invalid arguments should decode to empty map, got %+v", out[0].Arguments)
	}
	if decodeToolCalls(nil) != nil {
		t.Fatal("decodeToolCalls(nil) should return nil")
	}
}

func decodeJSONBody(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func TestProviderHealthCheckInvalidURL(t *testing.T) {
	p := NewProvider("key", "http://bad\x7furl:%%")
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("expected create request error")
	}
}

func TestProviderChatInvalidURL(t *testing.T) {
	p := NewProvider("key", "http://bad\x7furl:%%")
	if _, err := p.Chat(context.Background(), llm.ChatRequest{Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}}}); err == nil {
		t.Fatal("expected create request error")
	}
}

type erroringReadCloser struct{}

func (erroringReadCloser) Read(_ []byte) (int, error) { return 0, errors.New("read failed") }
func (erroringReadCloser) Close() error               { return nil }

func TestProviderChatReadResponseFailure(t *testing.T) {
	p := &Provider{
		apiKey:  "key",
		baseURL: "http://openai-stub.local",
		client: &http.Client{Transport: &stubTransport{
			resp: &http.Response{StatusCode: http.StatusOK, Body: erroringReadCloser{}, Header: http.Header{}},
		}},
	}
	if _, err := p.Chat(context.Background(), llm.ChatRequest{Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}}}); err == nil {
		t.Fatal("expected read response error")
	}
}

type stubTransport struct {
	resp *http.Response
}

func (s *stubTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return s.resp, nil
}

func TestProviderChatStreamToolCallAccumulation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"handoff","arguments":"{\"conversation_"}}]}}]}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"id\":\"c-9\",\"reason\":\"rage quit\"}"}}]}}]}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "help"}},
		Tools: []llm.ToolDefinition{{
			Name:        "handoff",
			Description: "handoff to human",
			InputSchema: map[string]interface{}{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	var toolCalls []*llm.ToolCall
	done := false
	for chunk := range ch {
		if chunk.ToolCall != nil {
			toolCalls = append(toolCalls, chunk.ToolCall)
		}
		if chunk.Done {
			done = true
		}
	}
	if !done {
		t.Fatal("expected done chunk")
	}
	if len(toolCalls) != 1 {
		t.Fatalf("tool calls = %+v", toolCalls)
	}
	call := toolCalls[0]
	if call.ID != "call-1" || call.Name != "handoff" || call.Arguments["conversation_id"] != "c-9" {
		t.Fatalf("accumulated tool call = %+v", call)
	}
}

func TestProviderChatStreamFinishReasonWithoutDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"final"},"finish_reason":"stop"}]}` + "\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	content := ""
	done := false
	for chunk := range ch {
		content += chunk.ContentDelta
		if chunk.Done {
			done = true
		}
	}
	if content != "final" || !done {
		t.Fatalf("content=%q done=%v", content, done)
	}
}

func TestProviderChatStreamCanceledContextDuringStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"partial"}}]}` + "\n\n"))
		flusher.Flush()
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"-more"}}]}` + "\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	provider := NewProvider("key", srv.URL)
	ch, err := provider.ChatStream(ctx, llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	seen := 0
	for range ch {
		seen++
		if seen == 1 {
			cancel()
		}
	}
}

func TestProviderChatStreamDoneMarkerEmitsToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-2","type":"function","function":{"name":"ticket_lookup","arguments":"{\"ticket_id\":7}"}}]}}]}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "user", Content: "status?"},
			{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "handoff", Arguments: map[string]interface{}{"conversation_id": "c1"}}}},
			{Role: "tool", ToolCallID: "call-1", Content: "{}"},
		},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	var toolCalls []*llm.ToolCall
	done := false
	for chunk := range ch {
		if chunk.ToolCall != nil {
			toolCalls = append(toolCalls, chunk.ToolCall)
		}
		if chunk.Done {
			done = true
		}
	}
	if !done || len(toolCalls) != 1 || toolCalls[0].Name != "ticket_lookup" {
		t.Fatalf("done=%v toolCalls=%+v", done, toolCalls)
	}
}

func TestProviderChatStreamInvalidURL(t *testing.T) {
	p := NewProvider("key", "http://bad\x7furl:%%")
	ch, err := p.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	for range ch {
	}
}

func TestProviderChatStreamContextCanceledDuringSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for i := 0; i < 10; i++ {
			_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"chunk"}}]}` + "\n\n"))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	provider := NewProvider("key", srv.URL)
	ch, err := provider.ChatStream(ctx, llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	<-ch
	cancel()
	for range ch {
	}
}

func TestProviderChatStreamContextCanceledAtStreamEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"only"}}]}` + "\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	provider := NewProvider("key", srv.URL)
	ch, err := provider.ChatStream(ctx, llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	<-ch
	cancel()
	for range ch {
	}
}

func TestProviderChatStreamErrorWithCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := NewProvider("key", srv.URL)
	ch, err := provider.ChatStream(ctx, llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	for range ch {
	}
}
