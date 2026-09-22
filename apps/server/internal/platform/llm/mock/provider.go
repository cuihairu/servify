package mock

import (
	"context"
	"sync"

	"servify/apps/server/internal/platform/llm"
)

// Provider is a controllable mock implementation of llm.LLMProvider.
type Provider struct {
	ChatResponse llm.ChatResponse
	ChatError    error
	StreamChunks []llm.ChatChunk
	// StreamQueue 按 ChatStream 调用次序逐次消费的预设分片；耗尽或为空
	// 时回落 StreamChunks（多步 agent 流式循环测试用）。
	StreamQueue [][]llm.ChatChunk
	StreamError error
	// StreamErrorQueue 按 ChatStream 调用次序逐次消费的预设错误；耗尽或
	// 为空时回落 StreamError。nil 元素表示该次调用成功（用分片）。
	StreamErrorQueue  []error
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
	chunks, err := p.nextStreamPresetLocked()
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	ch := make(chan llm.ChatChunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

// nextStreamPresetLocked 返回本次调用的流式预设（调用方持有 mu）：错误
// 队列优先（nil 元素表示该次放行成功），回落 StreamError；分片队列按次
// 消费，回落 StreamChunks。
func (p *Provider) nextStreamPresetLocked() ([]llm.ChatChunk, error) {
	if len(p.StreamErrorQueue) > 0 {
		err := p.StreamErrorQueue[0]
		p.StreamErrorQueue = p.StreamErrorQueue[1:]
		if err != nil {
			return nil, err
		}
	} else if p.StreamError != nil {
		return nil, p.StreamError
	}
	if len(p.StreamQueue) > 0 {
		chunks := p.StreamQueue[0]
		p.StreamQueue = p.StreamQueue[1:]
		return chunks, nil
	}
	return p.StreamChunks, nil
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
