package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"servify/apps/server/internal/platform/llm"
)

func TestProviderChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{"finish_reason":"tool_calls","message":{"content":"hello from model","tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"query\":\"hello\"}"}}]}}],
			"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}
		}`))
	}))
	defer srv.Close()

	provider := NewProvider("test-key", srv.URL)
	resp, err := provider.Chat(context.Background(), llm.ChatRequest{
		Model: "gpt-test",
		Messages: []llm.ChatMessage{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Content != "hello from model" {
		t.Fatalf("unexpected content: %s", resp.Content)
	}
	if resp.Provider != "openai" {
		t.Fatalf("unexpected provider: %s", resp.Provider)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "lookup" {
		t.Fatalf("unexpected tool calls: %+v", resp.ToolCalls)
	}
	if resp.TokenUsage == nil || resp.TokenUsage.TotalTokens != 18 {
		t.Fatalf("unexpected token usage: %+v", resp.TokenUsage)
	}
}

func TestProviderChatReturnsClassifiedHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`rate limited`))
	}))
	defer srv.Close()

	provider := NewProvider("test-key", srv.URL)
	_, err := provider.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	providerErr, ok := err.(*llm.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != llm.ProviderErrorRateLimited || !providerErr.Retryable {
		t.Fatalf("unexpected provider error: %+v", providerErr)
	}
}

func TestProviderHealthCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	provider := NewProvider("test-key", srv.URL)
	if err := provider.HealthCheck(context.Background()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestProviderChatStreamEmitsContentAndToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("response writer does not support flushing")
		}
		// Content delta chunks.
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hel\"}}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"}}]}\n\n"))
		flusher.Flush()
		// Tool call deltas (split across two chunks).
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\"}}]}}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"hi\\\"}\"}}]}}]}\n\n"))
		flusher.Flush()
		// Terminal chunk.
		stop := "stop"
		_ = stop
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	provider := NewProvider("test-key", srv.URL)
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{
		Model: "gpt-test",
		Messages: []llm.ChatMessage{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var content strings.Builder
	var toolCalls []llm.ToolCall
	var doneCount int
	for chunk := range ch {
		if chunk.ContentDelta != "" {
			content.WriteString(chunk.ContentDelta)
		}
		if chunk.ToolCall != nil {
			toolCalls = append(toolCalls, *chunk.ToolCall)
		}
		if chunk.Done {
			doneCount++
		}
	}

	if content.String() != "Hello" {
		t.Fatalf("expected streamed content 'Hello', got %q", content.String())
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	tc := toolCalls[0]
	if tc.ID != "call-1" || tc.Name != "lookup" {
		t.Fatalf("unexpected tool call: %+v", tc)
	}
	if tc.Arguments["q"] != "hi" {
		t.Fatalf("unexpected tool call arguments: %+v", tc.Arguments)
	}
	if doneCount != 1 {
		t.Fatalf("expected exactly 1 done chunk, got %d", doneCount)
	}
}

func TestProviderChatStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`rate limited`))
	}))
	defer srv.Close()

	provider := NewProvider("test-key", srv.URL)
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected no immediate error, got %v", err)
	}
	var last llm.ChatChunk
	for chunk := range ch {
		last = chunk
	}
	if !last.Done {
		t.Fatal("expected a terminal done chunk on stream error")
	}
}
