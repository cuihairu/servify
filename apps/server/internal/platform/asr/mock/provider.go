package mock

import (
	"context"
	"sync"

	"servify/apps/server/internal/platform/asr"
)

// Provider 是可编程的 asr.Recognizer 测试替身：每个会话各自从脚本头部
// 完整回放（按 FeedAudio 调用次序逐批注入事件），录制会话参数与音频分片
// 供断言。以指针使用（&mock.Provider{}），mu 保证并发录制安全。
type Provider struct {
	// SessionError 非 nil 时 NewSession 直接失败（provider 建联失败面）。
	SessionError error
	// HealthError 非 nil 时 HealthCheck 返回它。
	HealthError error
	// Script 按 FeedAudio 调用次序逐次消费：第 i 次调用注入 Script[i]
	//（nil 批次等同空批次）；脚本耗尽后继续喂音频不再产生事件。
	Script [][]asr.Event

	mu       sync.Mutex
	requests []asr.SessionOptions
	chunks   [][]byte
}

// NewSession 录制参数并返回会话与事件通道。通道容量按脚本全量 +1 预分配，
// FeedAudio 注入永不阻塞、事件语义完全确定。
func (p *Provider) NewSession(ctx context.Context, opts asr.SessionOptions) (asr.RecognizeSession, <-chan asr.Event, error) {
	p.mu.Lock()
	p.requests = append(p.requests, opts)
	total := 0
	for _, batch := range p.Script {
		total += len(batch)
	}
	p.mu.Unlock()
	if p.SessionError != nil {
		return nil, nil, p.SessionError
	}
	events := make(chan asr.Event, total+1)
	sess := &session{
		p:      p,
		script: append([][]asr.Event(nil), p.Script...),
		events: events,
	}
	return sess, events, nil
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	return p.HealthError
}

func (p *Provider) recordChunk(chunk []byte) {
	p.mu.Lock()
	p.chunks = append(p.chunks, chunk)
	p.mu.Unlock()
}

// RecordedRequests 返回已录制的会话参数快照。
func (p *Provider) RecordedRequests() []asr.SessionOptions {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]asr.SessionOptions, len(p.requests))
	copy(out, p.requests)
	return out
}

// RecordedChunks 返回已录制的音频分片快照。
func (p *Provider) RecordedChunks() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]byte, len(p.chunks))
	copy(out, p.chunks)
	return out
}

// session 单会话替身：持脚本私有快照与游标——多会话互不串扰，各自完整
// 回放同一份脚本。
type session struct {
	p *Provider

	script [][]asr.Event
	next   int

	mu     sync.Mutex
	closed bool
	events chan asr.Event
}

func (s *session) FeedAudio(chunk []byte) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return asr.ErrSessionClosed
	}
	var batch []asr.Event
	if s.next < len(s.script) {
		batch = s.script[s.next]
		s.next++
	}
	s.p.recordChunk(chunk)
	s.mu.Unlock()
	for _, ev := range batch {
		s.events <- ev
	}
	return nil
}

func (s *session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	close(s.events)
	return nil
}
