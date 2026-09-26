// 内部测试面（package application）：逐句预算熔断与收线守卫需要覆盖
// 未导出的预算字段（构造默认 5s，测试缩短到毫秒级以免拖慢套件）。
package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"servify/apps/server/internal/platform/asr"
	"servify/apps/server/internal/platform/tts"
)

// budgetSink 极简录制 sink（本文件专用）。
type budgetSink struct {
	mu       sync.Mutex
	captions []VoiceCaption
	audios   int
	errors   []error
}

func (s *budgetSink) OnSpeechStart(int64)     {}
func (s *budgetSink) OnPartial(int64, string) {}
func (s *budgetSink) OnCaption(c VoiceCaption) {
	s.mu.Lock()
	s.captions = append(s.captions, c)
	s.mu.Unlock()
}
func (s *budgetSink) OnAudio(int64, []byte, string) { s.mu.Lock(); s.audios++; s.mu.Unlock() }
func (s *budgetSink) OnError(err error)             { s.mu.Lock(); s.errors = append(s.errors, err); s.mu.Unlock() }

// budgetTranslator 可编程延迟/错误的翻译替身（本文件专用）。
type budgetTranslator struct {
	text  string
	delay time.Duration // >0 时阻塞至延迟到达或 ctx 释放
}

func (tr *budgetTranslator) Translate(ctx context.Context, cmd TranslateCommand) (TranslateResult, error) {
	if tr.delay > 0 {
		select {
		case <-time.After(tr.delay):
		case <-ctx.Done():
			return TranslateResult{}, ctx.Err()
		}
	}
	return TranslateResult{Text: tr.text}, nil
}

// budgetSynth 可编程延迟的合成替身（预算熔断用例）。
type budgetSynth struct {
	delay time.Duration
}

func (s *budgetSynth) Synthesize(ctx context.Context, req tts.SynthesizeRequest) (tts.SynthesizeResponse, error) {
	select {
	case <-time.After(s.delay):
		return tts.SynthesizeResponse{Audio: []byte("a"), Format: "mp3"}, nil
	case <-ctx.Done():
		return tts.SynthesizeResponse{}, ctx.Err()
	}
}

func (s *budgetSynth) HealthCheck(context.Context) error { return nil }

// shortBudgetPipeline 毫秒级预算的管线（其余同构造默认）。
func shortBudgetPipeline(translate SentenceTranslator, synth tts.Synthesizer, sink VoiceSink) *VoicePipeline {
	p := NewVoicePipeline(translate, synth, sink, "zh", "en", nil)
	p.translateBudget = 30 * time.Millisecond
	p.synthBudget = 30 * time.Millisecond
	return p
}

// runOneFinal 驱动单句 final 事件直至 Run 返回。
func runOneFinal(t *testing.T, p *VoicePipeline, ctx context.Context) {
	t.Helper()
	ch := make(chan asr.Event, 1)
	ch <- asr.Event{Kind: asr.EventFinal, Seq: 1, Text: "你好。"}
	close(ch)
	done := make(chan struct{})
	go func() {
		p.Run(ctx, ch)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
}

func TestVoicePipelineTranslateBudgetMeltDegradesOriginal(t *testing.T) {
	sink := &budgetSink{}
	p := shortBudgetPipeline(&budgetTranslator{delay: 300 * time.Millisecond}, &budgetSynth{}, sink)

	runOneFinal(t, p, context.Background())

	if len(sink.captions) != 1 || !sink.captions[0].Degraded || sink.captions[0].Translated != "你好。" {
		t.Fatalf("captions = %+v, want degraded original", sink.captions)
	}
	if sink.audios != 0 {
		t.Fatal("budget-melted sentence must not be synthesized")
	}
	if len(sink.errors) != 1 || !errors.Is(sink.errors[0], context.DeadlineExceeded) {
		t.Fatalf("errors = %+v, want DeadlineExceeded", sink.errors)
	}
}

func TestVoicePipelineSynthBudgetMeltKeepsCaptionOnly(t *testing.T) {
	sink := &budgetSink{}
	p := shortBudgetPipeline(&budgetTranslator{text: "fast"}, &budgetSynth{delay: 300 * time.Millisecond}, sink)

	runOneFinal(t, p, context.Background())

	if len(sink.captions) != 1 || sink.captions[0].Degraded {
		t.Fatalf("captions = %+v, want translated caption", sink.captions)
	}
	if sink.audios != 0 {
		t.Fatal("synth budget melt must skip audio")
	}
	if len(sink.errors) != 1 || !errors.Is(sink.errors[0], context.DeadlineExceeded) {
		t.Fatalf("errors = %+v, want DeadlineExceeded", sink.errors)
	}
}

func TestVoicePipelineNoCaptionDuringTeardown(t *testing.T) {
	sink := &budgetSink{}
	entered := make(chan struct{})
	p := NewVoicePipeline(&teardownTranslator{entered: entered}, &budgetSynth{}, sink, "zh", "en", nil)
	ch := make(chan asr.Event, 1)
	ch <- asr.Event{Kind: asr.EventFinal, Seq: 1, Text: "你好。"}
	close(ch)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx, ch)
		close(done)
	}()

	<-entered // 翻译已在途
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run must return after teardown")
	}

	if len(sink.captions) != 0 || sink.audios != 0 || len(sink.errors) != 0 {
		t.Fatalf("teardown must not emit caption/audio/error: %+v", sink)
	}
}

// teardownTranslator 在途阻塞至 ctx 释放（收线守卫用例）。
type teardownTranslator struct {
	entered chan struct{}
}

func (tr *teardownTranslator) Translate(ctx context.Context, cmd TranslateCommand) (TranslateResult, error) {
	close(tr.entered)
	<-ctx.Done()
	return TranslateResult{}, ctx.Err()
}
