package mock

import (
	"context"
	"sync"

	"servify/apps/server/internal/platform/llm"
)

// Provider is a controllable mock implementation of llm.LLMProvider.
type Provider struct {
	ChatResponse      llm.ChatResponse
	ChatError         error
	StreamChunks      []llm.ChatChunk
	StreamError       error
	EmbeddingResponse [][]float32
	EmbeddingError    error
	HealthError       error

	mu       sync.Mutex
	Requests []llm.ChatRequest
}

// Chat 转发预设响应，同时录制收到的请求（供测试断言 prompt 组装、
// 模型/温度透传等是否劣化）。Provider 以指针使用（&mock.Provider{}），
// mu 保证并发录制安全。
func (p *Provider) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	p.mu.Lock()
	p.Requests = append(p.Requests, req)
	p.mu.Unlock()
	return p.ChatResponse, p.ChatError
}

func (p *Provider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatChunk, error) {
	p.mu.Lock()
	p.Requests = append(p.Requests, req)
	p.mu.Unlock()
	if p.StreamError != nil {
		return nil, p.StreamError
	}
	ch := make(chan llm.ChatChunk, len(p.StreamChunks))
	for _, chunk := range p.StreamChunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (p *Provider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return p.EmbeddingResponse, p.EmbeddingError
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	return p.HealthError
}

// RecordedRequests 返回已录制的 Chat/ChatStream 请求快照。
func (p *Provider) RecordedRequests() []llm.ChatRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]llm.ChatRequest, len(p.Requests))
	copy(out, p.Requests)
	return out
}
