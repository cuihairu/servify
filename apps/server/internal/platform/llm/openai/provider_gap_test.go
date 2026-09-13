package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"servify/apps/server/internal/platform/llm"
)

// TestProviderChatMarshalsToolDefinitions 覆盖 chatOnce 的工具定义序列化循环。
func TestProviderChatMarshalsToolDefinitions(t *testing.T) {
	var gotTools []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		} `json:"function"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []struct {
				Type     string `json:"type"`
				Function struct {
					Name        string                 `json:"name"`
					Description string                 `json:"description"`
					Parameters  map[string]interface{} `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		_ = decodeJSONBody(r, &payload)
		gotTools = payload.Tools
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	provider := NewProvider("key", srv.URL)
	_, err := provider.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
		Tools: []llm.ToolDefinition{{
			Name:        "handoff",
			Description: "handoff to human agent",
			InputSchema: map[string]interface{}{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if len(gotTools) != 1 || gotTools[0].Type != "function" || gotTools[0].Function.Name != "handoff" {
		t.Fatalf("tools not round-tripped: %+v", gotTools)
	}
	if gotTools[0].Function.Description != "handoff to human agent" {
		t.Fatalf("description = %q", gotTools[0].Function.Description)
	}
}

// TestProviderChatUnmarshalableToolSchema 覆盖 chatOnce 的请求序列化失败分支:
// InputSchema 携带不可序列化的值。
func TestProviderChatUnmarshalableToolSchema(t *testing.T) {
	provider := NewProvider("key", "http://openai-stub.local")
	_, err := provider.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
		Tools: []llm.ToolDefinition{{
			Name:        "bad",
			InputSchema: map[string]interface{}{"ch": make(chan int)},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "marshal openai request") {
		t.Fatalf("err = %v, want marshal failure", err)
	}
}

// TestProviderChatStreamUnmarshalableToolSchema 覆盖 streamOnce 的请求序列化失败分支。
func TestProviderChatStreamUnmarshalableToolSchema(t *testing.T) {
	provider := NewProvider("key", "http://openai-stub.local")
	ch, err := provider.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
		Tools: []llm.ToolDefinition{{
			Name:        "bad",
			InputSchema: map[string]interface{}{"ch": make(chan int)},
		}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	var streamErr string
	for chunk := range ch {
		if strings.Contains(chunk.ContentDelta, "marshal openai stream request") {
			streamErr = chunk.ContentDelta
		}
	}
	if streamErr == "" {
		t.Fatal("expected marshal error to be delivered on the stream")
	}
}

// stubSSETransport 返回一条完整的内存 SSE 响应,让 streamOnce 的行处理
// 不依赖 socket,从而可以在接收方停止接收后确定性地触发取消分支。
type stubSSETransport struct {
	body string
}

func (s *stubSSETransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     http.Header{},
	}, nil
}

// streamOnceCanceledAfterFirstChunk 直接驱动 streamOnce:接收方消费掉
// 第一个内容 chunk 后立即取消 ctx 且不再接收。此后所有发送 select 中
// 只有 ctx.Done 就绪,分支选择是确定的。
func streamOnceCanceledAfterFirstChunk(t *testing.T, body string) error {
	t.Helper()
	p := &Provider{
		apiKey:  "key",
		baseURL: "http://openai-stub.local",
		client:  &http.Client{Transport: &stubSSETransport{body: body}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan llm.ChatChunk)
	go func() {
		first := <-ch
		if first.ContentDelta != "hello" {
			t.Errorf("first chunk = %+v, want content %q", first, "hello")
		}
		cancel()
	}()
	return p.streamOnce(ctx, llm.ChatRequest{
		Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}},
	}, ch)
}

// TestProviderChatStreamCanceledAtDoneMarker 覆盖 [DONE] 处理中的 ctx.Done 分支。
func TestProviderChatStreamCanceledAtDoneMarker(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"hello"}}]}`,
		"data: [DONE]",
		"",
	}, "\n\n")
	if err := streamOnceCanceledAfterFirstChunk(t, body); !errors.Is(err, context.Canceled) {
		t.Fatalf("streamOnce() error = %v, want context.Canceled", err)
	}
}

// TestProviderChatStreamCanceledAtFinishReason 覆盖 finish_reason 终止路径中的 ctx.Done 分支。
func TestProviderChatStreamCanceledAtFinishReason(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"hello"}}]}`,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
	}, "\n\n")
	if err := streamOnceCanceledAfterFirstChunk(t, body); !errors.Is(err, context.Canceled) {
		t.Fatalf("streamOnce() error = %v, want context.Canceled", err)
	}
}

// TestProviderChatStreamCanceledBeforeToolEmit 覆盖 emitToolResults 阻塞发送中的 ctx.Done 分支。
func TestProviderChatStreamCanceledBeforeToolEmit(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"hello"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}}]}`,
		"data: [DONE]",
		"",
	}, "\n\n")
	if err := streamOnceCanceledAfterFirstChunk(t, body); !errors.Is(err, context.Canceled) {
		t.Fatalf("streamOnce() error = %v, want context.Canceled", err)
	}
}
