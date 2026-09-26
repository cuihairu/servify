package mock

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/platform/tts"
)

// 编译期契约断言：替身必须满足 Synthesizer。
var _ tts.Synthesizer = (*Provider)(nil)

func TestSynthesizeReplaysPresetAudio(t *testing.T) {
	p := &Provider{Audio: []byte("ogg-bytes"), Format: "mp3", SampleRate: 24000}
	resp, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{
		Text: "Hello", Lang: "en", Voice: "alloy", Format: "mp3",
	})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if string(resp.Audio) != "ogg-bytes" || resp.Format != "mp3" || resp.SampleRate != 24000 {
		t.Fatalf("response degraded: %+v", resp)
	}
	reqs := p.RecordedRequests()
	if len(reqs) != 1 || reqs[0].Text != "Hello" || reqs[0].Lang != "en" || reqs[0].Voice != "alloy" {
		t.Fatalf("request not recorded verbatim: %+v", reqs)
	}
}

func TestSynthesizeDefaultAudioWhenUnset(t *testing.T) {
	p := &Provider{}
	resp, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{Text: "你好"})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if string(resp.Audio) != "mock-audio" {
		t.Fatalf("default audio = %q, want mock-audio", resp.Audio)
	}
}

func TestSynthesizeErrorQueueThenGlobalFallback(t *testing.T) {
	first := errors.New("first call fails")
	second := errors.New("second call fails")
	p := &Provider{Error: errors.New("global failure"), ErrorQueue: []error{first, second, nil}}

	if _, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{Text: "1"}); !errors.Is(err, first) {
		t.Fatalf("call#1 err = %v, want first", err)
	}
	if _, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{Text: "2"}); !errors.Is(err, second) {
		t.Fatalf("call#2 err = %v, want second", err)
	}
	if _, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{Text: "3"}); err != nil {
		t.Fatalf("call#3 nil queue element must pass, got %v", err)
	}
	if _, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{Text: "4"}); !errors.Is(err, p.Error) {
		t.Fatalf("call#4 err = %v, want global fallback", err)
	}
	// 失败调用同样录制（管线测试断言"失败句不再重试"依赖它）。
	if got := len(p.RecordedRequests()); got != 4 {
		t.Fatalf("recorded requests = %d, want 4", got)
	}
}

func TestHealthCheck(t *testing.T) {
	p := &Provider{}
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("healthy by default, got %v", err)
	}
	healthErr := errors.New("probe failed")
	p2 := &Provider{HealthError: healthErr}
	if err := p2.HealthCheck(context.Background()); !errors.Is(err, healthErr) {
		t.Fatalf("HealthCheck err = %v, want healthErr", err)
	}
}
