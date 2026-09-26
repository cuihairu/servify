package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	translationapp "servify/apps/server/internal/modules/translation/application"
	"servify/apps/server/internal/platform/asr"
	"servify/apps/server/internal/platform/llm"
	mockllm "servify/apps/server/internal/platform/llm/mock"
	"servify/apps/server/internal/platform/tts"
	mocktts "servify/apps/server/internal/platform/tts/mock"
)

// SentenceTranslatorStub 可编程翻译替身（Satisfies translationapp.SentenceTranslator）。
type SentenceTranslatorStub struct {
	Text  string
	Err   error
	Delay time.Duration // >0 时阻塞至延迟到达或 ctx 释放（预算熔断/收线守卫用例）
}

func (s *SentenceTranslatorStub) Translate(ctx context.Context, cmd translationapp.TranslateCommand) (translationapp.TranslateResult, error) {
	if s.Delay > 0 {
		select {
		case <-time.After(s.Delay):
		case <-ctx.Done():
			return translationapp.TranslateResult{}, ctx.Err()
		}
	}
	if s.Err != nil {
		return translationapp.TranslateResult{}, s.Err
	}
	return translationapp.TranslateResult{Text: s.Text}, nil
}

// recordedAudio 一段产出音频（句序 + 字节 + 格式）。
type recordedAudio struct {
	Seq    int64
	Audio  string
	Format string
}

// recordingSink 录制管线全部产出供断言（管线串行调用，锁仅防未来并发化）。
type recordingSink struct {
	mu       sync.Mutex
	speeches []int64
	partials []string
	captions []translationapp.VoiceCaption
	audios   []recordedAudio
	errors   []error
}

func (r *recordingSink) OnSpeechStart(turnSeq int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.speeches = append(r.speeches, turnSeq)
}

func (r *recordingSink) OnPartial(turnSeq int64, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.partials = append(r.partials, text)
}

func (r *recordingSink) OnCaption(caption translationapp.VoiceCaption) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.captions = append(r.captions, caption)
}

func (r *recordingSink) OnAudio(capSeq int64, audio []byte, format string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.audios = append(r.audios, recordedAudio{Seq: capSeq, Audio: string(audio), Format: format})
}

func (r *recordingSink) OnError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, err)
}

// snapshot 返回全部录制的浅拷贝。
func (r *recordingSink) snapshot() (speeches []int64, partials []string, captions []translationapp.VoiceCaption, audios []recordedAudio, errs []error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int64{}, r.speeches...),
		append([]string{}, r.partials...),
		append([]translationapp.VoiceCaption{}, r.captions...),
		append([]recordedAudio{}, r.audios...),
		append([]error{}, r.errors...)
}

// runPipeline 以固定 sink 构造管线（zh→en、testParams），驱动其消费全部
// 事件直至通道关闭，返回录制结果。
func runPipeline(t *testing.T, provider *mockllm.Provider, synth tts.Synthesizer, events []asr.Event) *recordingSink {
	t.Helper()
	sink := &recordingSink{}
	p := translationapp.NewVoicePipeline(
		translationapp.NewService(provider, testParams), synth, sink, "zh", "en", nil)
	ch := make(chan asr.Event, len(events)+1)
	for _, ev := range events {
		ch <- ev
	}
	close(ch)
	done := make(chan struct{})
	go func() {
		p.Run(context.Background(), ch)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pipeline Run did not return after channel close")
	}
	return sink
}

func TestVoicePipelineCaptionFlow(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "translated line"}}
	synth := &mocktts.Provider{Audio: []byte("mp3-bytes"), Format: "mp3"}

	sink := runPipeline(t, provider, synth, []asr.Event{
		{Kind: asr.EventSpeechStart, Seq: 1},
		{Kind: asr.EventPartial, Seq: 1, Text: "你好"},
		{Kind: asr.EventFinal, Seq: 1, Text: "你好。"},
		{Kind: asr.EventFinal, Seq: 2, Text: "再见"}, // 无标点未满长：留在 buffer，通道关闭时 Flush
	})
	speeches, partials, captions, audios, errs := sink.snapshot()
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(speeches) != 1 || speeches[0] != 1 {
		t.Fatalf("speeches = %+v, want [1]", speeches)
	}
	if len(partials) != 1 || partials[0] != "你好" {
		t.Fatalf("partials = %+v, want single pass-through", partials)
	}
	if len(captions) != 2 {
		t.Fatalf("captions = %+v, want 2", captions)
	}
	if captions[0].Seq != 1 || captions[0].Source != "你好。" || captions[0].Translated != "translated line" || captions[0].Degraded {
		t.Fatalf("caption[0] degraded: %+v", captions[0])
	}
	if captions[1].Seq != 2 || captions[1].Source != "再见" || captions[1].Translated != "translated line" {
		t.Fatalf("caption[1] degraded: %+v", captions[1])
	}
	if len(audios) != 2 || audios[0].Seq != 1 || audios[0].Audio != "mp3-bytes" || audios[0].Format != "mp3" || audios[1].Seq != 2 {
		t.Fatalf("audios degraded: %+v", audios)
	}

	// 上下文尾窗：第一句无上文；第二句携带第一句原文+译文。
	requests := provider.RecordedRequests()
	if len(requests) != 2 {
		t.Fatalf("want 2 llm calls, got %d", len(requests))
	}
	if strings.Contains(requests[0].Messages[1].Content, "对话上文") {
		t.Fatalf("first sentence must not carry context: %+v", requests[0].Messages[1])
	}
	second := requests[1].Messages[1].Content
	if !strings.Contains(second, "对话上文") || !strings.Contains(second, "你好。") || !strings.Contains(second, "translated line") {
		t.Fatalf("second sentence must carry prior source+translation tail: %+v", second)
	}
}

func TestVoicePipelineDegradesOnTranslateFailure(t *testing.T) {
	synth := &mocktts.Provider{Audio: []byte("x")}
	sink := runPipeline(t, &mockllm.Provider{ChatError: errors.New("llm boom")}, synth, []asr.Event{
		{Kind: asr.EventFinal, Seq: 1, Text: "你好。"},
	})
	_, _, captions, audios, errs := sink.snapshot()
	if len(captions) != 1 || !captions[0].Degraded || captions[0].Translated != "你好。" {
		t.Fatalf("caption must degrade to source text: %+v", captions)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "llm boom") {
		t.Fatalf("errors = %+v, want translate failure surfaced", errs)
	}
	if len(audios) != 0 {
		t.Fatalf("degraded sentence must not be synthesized: %+v", audios)
	}
}

func TestVoicePipelineNilTranslateDegradesAll(t *testing.T) {
	sink := &recordingSink{}
	p := translationapp.NewVoicePipeline(nil, &mocktts.Provider{}, sink, "zh", "en", nil)
	ch := make(chan asr.Event, 1)
	ch <- asr.Event{Kind: asr.EventFinal, Seq: 1, Text: "你好。"}
	close(ch)
	done := make(chan struct{})
	go func() {
		p.Run(context.Background(), ch)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}

	_, _, captions, _, errs := sink.snapshot()
	if len(captions) != 1 || !captions[0].Degraded {
		t.Fatalf("caption must degrade: %+v", captions)
	}
	if len(errs) != 1 || !errors.Is(errs[0], translationapp.ErrTranslationUnavailable) {
		t.Fatalf("errors = %+v, want ErrTranslationUnavailable", errs)
	}
}

func TestVoicePipelineTTSErrorSkipsAudioOnly(t *testing.T) {
	sink := runPipeline(t,
		&mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "ok"}},
		&mocktts.Provider{Error: errors.New("tts down")},
		[]asr.Event{{Kind: asr.EventFinal, Seq: 1, Text: "你好。"}})
	_, _, captions, audios, errs := sink.snapshot()
	if len(captions) != 1 || captions[0].Degraded {
		t.Fatalf("caption must stay translated: %+v", captions)
	}
	if len(audios) != 0 {
		t.Fatalf("synthesis failure must skip audio: %+v", audios)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "tts down") {
		t.Fatalf("errors = %+v, want tts failure surfaced", errs)
	}
}

func TestVoicePipelineASRErrorEventSurfacedAndNonFatal(t *testing.T) {
	sink := runPipeline(t,
		&mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "ok"}},
		nil,
		[]asr.Event{
			{Kind: asr.EventError, Err: errors.New("stream broken")},
			{Kind: asr.EventFinal, Seq: 1, Text: "你好。"}, // 后续句继续处理
		})
	_, _, captions, _, errs := sink.snapshot()
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "stream broken") {
		t.Fatalf("errors = %+v, want ASR error surfaced", errs)
	}
	if len(captions) != 1 {
		t.Fatalf("pipeline must keep processing after error: %+v", captions)
	}
}

func TestVoicePipelineLengthSplitFeedsMultipleCaptions(t *testing.T) {
	sink := runPipeline(t,
		&mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "t"}},
		nil,
		[]asr.Event{{Kind: asr.EventFinal, Seq: 1, Text: strings.Repeat("a", 130)}}) // 60+60+10
	_, _, captions, _, _ := sink.snapshot()
	if len(captions) != 3 {
		t.Fatalf("captions = %d, want 3 (60/60/flush10)", len(captions))
	}
	if len(captions[0].Source) != 60 || len(captions[1].Source) != 60 || len(captions[2].Source) != 10 {
		t.Fatalf("split sizes degraded: %d/%d/%d",
			len(captions[0].Source), len(captions[1].Source), len(captions[2].Source))
	}
}

func TestVoicePipelineSpeechEndFlushesResidue(t *testing.T) {
	sink := runPipeline(t,
		&mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "ok"}},
		nil,
		[]asr.Event{
			{Kind: asr.EventSpeechEnd, Seq: 1}, // VAD 尾点：残余 buffer 切句
			{Kind: asr.EventFinal, Seq: 1, Text: "还没说完的半句"},
			{Kind: asr.EventSpeechEnd, Seq: 1},
		})
	_, _, captions, _, errs := sink.snapshot()
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// 第一个 speech_end 时 buffer 为空：无产出；final 落 buffer，第二个
	// speech_end 切句产出。
	if len(captions) != 1 || captions[0].Source != "还没说完的半句" {
		t.Fatalf("captions = %+v, want single flushed sentence", captions)
	}
}

func TestVoicePipelineContextTailClamped(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{
		Content: strings.Repeat("译", 250), // 长译文使原文+译文拼接必超 200 尾窗
	}}
	sink := runPipeline(t, provider, nil, []asr.Event{
		{Kind: asr.EventFinal, Seq: 1, Text: strings.Repeat("原", 50)}, // 无标点：留 buffer
		{Kind: asr.EventFinal, Seq: 2, Text: "第二句。"},                  // 触发句 1 产出
	})
	if _, _, captions, _, _ := sink.snapshot(); len(captions) != 2 {
		t.Fatalf("captions = %d, want 2", len(captions))
	}
	second := provider.RecordedRequests()[1].Messages[1].Content
	if !strings.Contains(second, "对话上文") {
		t.Fatalf("second sentence must carry context: %s", second)
	}
	// 服务端截尾后上下文不超过 MaxContextRunes。
	const marker = "对话上文（仅帮助理解衔接，不要翻译或输出它）：\n"
	start := strings.Index(second, marker) + len(marker)
	end := strings.Index(second[start:], "\n待译内容：")
	window := second[start : start+end]
	if len([]rune(window)) > translationapp.MaxContextRunes {
		t.Fatalf("context window = %d runes, want ≤ %d", len([]rune(window)), translationapp.MaxContextRunes)
	}
}

func TestVoicePipelineRunReturnsOnContextCancel(t *testing.T) {
	p := translationapp.NewVoicePipeline(nil, nil, &recordingSink{}, "zh", "en", nil)
	ch := make(chan asr.Event) // 永不入事件：Run 只能经 ctx 退出
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx, ch)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run must return promptly on context cancel")
	}
}

// recordingObserver 录制逐句产出计量（outcome 词表 + token 消费）。
type recordingObserver struct {
	mu       sync.Mutex
	outcomes []string
	tokens   []string // "provider:input:output" 形态快照
}

func (o *recordingObserver) OnSentenceOutcome(outcome string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.outcomes = append(o.outcomes, outcome)
}

func (o *recordingObserver) OnTranslateTokens(provider string, usage llm.TokenUsage) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.tokens = append(o.tokens, fmt.Sprintf("%s:%d:%d", provider, usage.InputTokens, usage.OutputTokens))
}

func (o *recordingObserver) snapshot() (outcomes, tokens []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string{}, o.outcomes...), append([]string{}, o.tokens...)
}

// drivePipeline 直接构造管线并驱动单句事件（obs 注入路径；runPipeline 固定
// nil obs 不动）。
func drivePipeline(t *testing.T, translate translationapp.SentenceTranslator, synth tts.Synthesizer, obs translationapp.VoiceObserver) *recordingSink {
	t.Helper()
	sink := &recordingSink{}
	p := translationapp.NewVoicePipeline(translate, synth, sink, "zh", "en", obs)
	ch := make(chan asr.Event, 1)
	ch <- asr.Event{Kind: asr.EventFinal, Seq: 1, Text: "你好。"}
	close(ch)
	done := make(chan struct{})
	go func() {
		p.Run(context.Background(), ch)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
	return sink
}

func TestVoicePipelineObserverTranslatedOutcomeAndTokens(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{
		Content:    "hello",
		Provider:   "mock",
		TokenUsage: &llm.TokenUsage{InputTokens: 11, OutputTokens: 7},
	}}
	obs := &recordingObserver{}
	sink := drivePipeline(t, translationapp.NewService(provider, testParams), &mocktts.Provider{Audio: []byte("mp3")}, obs)

	outcomes, tokens := obs.snapshot()
	if len(outcomes) != 1 || outcomes[0] != translationapp.VoiceOutcomeTranslated {
		t.Fatalf("outcomes = %+v, want [translated]", outcomes)
	}
	if _, _, _, audios, _ := sink.snapshot(); len(audios) != 1 {
		t.Fatalf("translated sentence must emit audio: %+v", audios)
	}
	if len(tokens) != 1 || tokens[0] != "mock:11:7" {
		t.Fatalf("tokens = %+v, want [mock:11:7]", tokens)
	}
}

func TestVoicePipelineObserverCaptionOnlyWithoutSynth(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{
		Content:    "hello",
		Provider:   "mock",
		TokenUsage: &llm.TokenUsage{InputTokens: 5, OutputTokens: 3},
	}}
	obs := &recordingObserver{}
	drivePipeline(t, translationapp.NewService(provider, testParams), nil, obs)

	outcomes, tokens := obs.snapshot()
	if len(outcomes) != 1 || outcomes[0] != translationapp.VoiceOutcomeCaptionOnly {
		t.Fatalf("outcomes = %+v, want [caption_only]", outcomes)
	}
	if len(tokens) != 1 || tokens[0] != "mock:5:3" {
		t.Fatalf("tokens = %+v, want [mock:5:3]", tokens)
	}
}

func TestVoicePipelineObserverDegradedOnTranslateFailure(t *testing.T) {
	obs := &recordingObserver{}
	sink := drivePipeline(t, &SentenceTranslatorStub{Err: errors.New("llm boom")}, &mocktts.Provider{}, obs)

	outcomes, _ := obs.snapshot()
	if len(outcomes) != 1 || outcomes[0] != translationapp.VoiceOutcomeDegraded {
		t.Fatalf("outcomes = %+v, want [degraded]", outcomes)
	}
	if _, _, captions, _, _ := sink.snapshot(); len(captions) != 1 || !captions[0].Degraded {
		t.Fatalf("captions = %+v, want degraded original", captions)
	}
}

func TestVoicePipelineObserverCaptionOnlyOnSynthFailure(t *testing.T) {
	provider := &mockllm.Provider{ChatResponse: llm.ChatResponse{Content: "hello"}}
	obs := &recordingObserver{}
	drivePipeline(t, translationapp.NewService(provider, testParams), &mocktts.Provider{Error: errors.New("tts down")}, obs)

	outcomes, _ := obs.snapshot()
	if len(outcomes) != 1 || outcomes[0] != translationapp.VoiceOutcomeCaptionOnly {
		t.Fatalf("outcomes = %+v, want [caption_only]", outcomes)
	}
}
