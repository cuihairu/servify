package mock

import (
	"context"
	"sync"

	"servify/apps/server/internal/platform/tts"
)

// Provider 是可编程的 tts.Synthesizer 测试替身：回放预设音频/错误，录制
// 请求供断言。以指针使用（&mock.Provider{}），mu 保证并发录制安全。
type Provider struct {
	// Audio/Format/SampleRate 成功回放面；Audio 为空时缺省 "mock-audio"。
	Audio      []byte
	Format     string
	SampleRate int
	// Error 全局错误面：非 nil 时每次 Synthesize 都返回它。
	Error error
	// ErrorQueue 按 Synthesize 调用次序逐次消费的预设错误；耗尽或为空时
	// 回落 Error。nil 元素表示该次调用成功。
	ErrorQueue  []error
	HealthError error

	mu       sync.Mutex
	requests []tts.SynthesizeRequest
}

// Synthesize 转发预设响应，同时录制请求（供测试断言文本/音色/格式透传）。
func (p *Provider) Synthesize(ctx context.Context, req tts.SynthesizeRequest) (tts.SynthesizeResponse, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	err := p.nextErrorLocked()
	p.mu.Unlock()
	if err != nil {
		return tts.SynthesizeResponse{}, err
	}
	audio := p.Audio
	if audio == nil {
		audio = []byte("mock-audio")
	}
	return tts.SynthesizeResponse{Audio: audio, Format: p.Format, SampleRate: p.SampleRate}, nil
}

// nextErrorLocked 弹出下一次调用的预设错误（调用方持有 mu）：队列优先
// （nil 元素放行成功），回落全局 Error。
func (p *Provider) nextErrorLocked() error {
	if len(p.ErrorQueue) > 0 {
		err := p.ErrorQueue[0]
		p.ErrorQueue = p.ErrorQueue[1:]
		if err != nil {
			return err
		}
		return nil
	}
	return p.Error
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	return p.HealthError
}

// RecordedRequests 返回已录制的合成请求快照。
func (p *Provider) RecordedRequests() []tts.SynthesizeRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]tts.SynthesizeRequest, len(p.requests))
	copy(out, p.requests)
	return out
}
