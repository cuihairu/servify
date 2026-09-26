package delivery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	translationapp "servify/apps/server/internal/modules/translation/application"
	"servify/apps/server/internal/platform/asr"
	"servify/apps/server/internal/platform/tts"
)

// vaRecognizerStub ASR provider 替身：记录会话参数，事件通道交测试驱动。
type vaRecognizerStub struct {
	newSessErr error
	opts       asr.SessionOptions
	session    *vaSessionStub
}

func (r *vaRecognizerStub) NewSession(_ context.Context, opts asr.SessionOptions) (asr.RecognizeSession, <-chan asr.Event, error) {
	if r.newSessErr != nil {
		return nil, nil, r.newSessErr
	}
	r.opts = opts
	r.session = &vaSessionStub{events: make(chan asr.Event, 16)}
	return r.session, r.session.events, nil
}

func (r *vaRecognizerStub) HealthCheck(context.Context) error { return nil }

// vaSessionStub ASR 会话替身：记录喂入分片，Close 排干语义（关闭事件通道）。
type vaSessionStub struct {
	events chan asr.Event

	mu     sync.Mutex
	chunks [][]byte
	closed bool
}

func (s *vaSessionStub) FeedAudio(chunk []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return asr.ErrSessionClosed
	}
	s.chunks = append(s.chunks, append([]byte(nil), chunk...))
	return nil
}

func (s *vaSessionStub) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.events)
	return nil
}

func (s *vaSessionStub) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// vaSynthStub TTS 替身：记录请求并可注入失败。
type vaSynthStub struct {
	mu   sync.Mutex
	reqs []tts.SynthesizeRequest
	err  error
}

func (s *vaSynthStub) Synthesize(_ context.Context, req tts.SynthesizeRequest) (tts.SynthesizeResponse, error) {
	s.mu.Lock()
	s.reqs = append(s.reqs, req)
	err := s.err
	s.mu.Unlock()
	if err != nil {
		return tts.SynthesizeResponse{}, err
	}
	return tts.SynthesizeResponse{Audio: []byte("aud"), Format: "mp3"}, nil
}

func (s *vaSynthStub) HealthCheck(context.Context) error { return nil }

func (s *vaSynthStub) recordedRequests() []tts.SynthesizeRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tts.SynthesizeRequest(nil), s.reqs...)
}

// vaSink 语音管线产出录制替身。
type vaSink struct {
	mu         sync.Mutex
	partials   []string
	captions   []translationapp.VoiceCaption
	audios     [][]byte
	startTurns []int64
}

func (s *vaSink) OnSpeechStart(turnSeq int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startTurns = append(s.startTurns, turnSeq)
}

func (s *vaSink) OnPartial(_ int64, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partials = append(s.partials, text)
}

func (s *vaSink) OnCaption(caption translationapp.VoiceCaption) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.captions = append(s.captions, caption)
}

func (s *vaSink) OnAudio(_ int64, audio []byte, _ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audios = append(s.audios, audio)
}

func (s *vaSink) OnError(error) {}

func (s *vaSink) snapshot() (captions []translationapp.VoiceCaption, audios [][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]translationapp.VoiceCaption(nil), s.captions...), append([][]byte(nil), s.audios...)
}

// waitVoiceCaptions 轮询等管线产出 N 条字幕（管线在独立 goroutine 消费）。
func waitVoiceCaptions(t *testing.T, sink *vaSink, n int) []translationapp.VoiceCaption {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		captions, _ := sink.snapshot()
		if len(captions) >= n {
			return captions
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d/%d captions produced", len(captions), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

var _ VoiceStreamStarter = (*voiceChannelAdapter)(nil)

// TestVoiceChannelAdapterUnconfigured 未装配/无偏好形态：recognizer 或偏好
// 读取缺失 → 不可用；会话无偏好读向 → 通道不激活；ASR 建联失败原样上抛。
func TestVoiceChannelAdapterUnconfigured(t *testing.T) {
	ctx := context.Background()

	t.Run("nil recognizer", func(t *testing.T) {
		if _, err := NewVoiceChannelService(&rtTranslateStub{}, &rtPrefsStub{lang: "en"}, nil, nil).StartAudioStream(ctx, "s", VoiceSpeakerVisitor, &vaSink{}); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("err = %v, want ErrTranslationUnavailable", err)
		}
	})
	t.Run("nil prefs", func(t *testing.T) {
		if _, err := NewVoiceChannelService(&rtTranslateStub{}, nil, &vaRecognizerStub{}, nil).StartAudioStream(ctx, "s", VoiceSpeakerVisitor, &vaSink{}); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("err = %v, want ErrTranslationUnavailable", err)
		}
	})
	t.Run("no preference means inactive channel", func(t *testing.T) {
		if _, err := NewVoiceChannelService(&rtTranslateStub{}, &rtPrefsStub{lang: ""}, &vaRecognizerStub{}, nil).StartAudioStream(ctx, "s", VoiceSpeakerVisitor, &vaSink{}); !errors.Is(err, ErrVoiceChannelInactive) {
			t.Fatalf("err = %v, want ErrVoiceChannelInactive", err)
		}
	})
	t.Run("preference read error propagates", func(t *testing.T) {
		boom := errors.New("db down")
		if _, err := NewVoiceChannelService(&rtTranslateStub{}, &rtPrefsStub{err: boom}, &vaRecognizerStub{}, nil).StartAudioStream(ctx, "s", VoiceSpeakerVisitor, &vaSink{}); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want wrapped boom", err)
		}
	})
	t.Run("asr new session failure", func(t *testing.T) {
		boom := errors.New("dial refused")
		if _, err := NewVoiceChannelService(&rtTranslateStub{}, &rtPrefsStub{lang: "en"}, &vaRecognizerStub{newSessErr: boom}, nil).StartAudioStream(ctx, "s", VoiceSpeakerVisitor, &vaSink{}); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want boom", err)
		}
	})
}

// TestVoiceChannelAdapterReadDirection 说话方→读向对偶：visitor 说话查
// agent 读向（坐席读什么语言），agent 说话查 visitor 读向。
func TestVoiceChannelAdapterReadDirection(t *testing.T) {
	for _, tc := range []struct {
		speaker    string
		wantViewer string
	}{
		{VoiceSpeakerVisitor, ViewerRoleAgent},
		{VoiceSpeakerAgent, ViewerRoleVisitor},
	} {
		prefs := &rtPrefsStub{lang: "en"}
		stream, err := NewVoiceChannelService(&rtTranslateStub{}, prefs, &vaRecognizerStub{}, nil).StartAudioStream(context.Background(), "s", tc.speaker, &vaSink{})
		if err != nil {
			t.Fatalf("speaker=%s: %v", tc.speaker, err)
		}
		_ = stream.Close()
		if prefs.lastViewer != tc.wantViewer {
			t.Fatalf("speaker=%s viewer = %q, want %q", tc.speaker, prefs.lastViewer, tc.wantViewer)
		}
	}
}

// TestVoiceChannelAdapterSessionShape 会话参数：源语言留空（模型自动检测）、
// 固定 pcm16 24kHz 单声道上行格式。
func TestVoiceChannelAdapterSessionShape(t *testing.T) {
	recognizer := &vaRecognizerStub{}
	stream, err := NewVoiceChannelService(&rtTranslateStub{}, &rtPrefsStub{lang: "en"}, recognizer, nil).StartAudioStream(context.Background(), "s", VoiceSpeakerVisitor, &vaSink{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stream.Close()
	if recognizer.opts.SourceLang != "" {
		t.Fatalf("source lang = %q, want auto (empty)", recognizer.opts.SourceLang)
	}
	if recognizer.opts.Format != (asr.AudioFormat{SampleRate: 24000, Channels: 1, Encoding: "pcm16"}) {
		t.Fatalf("format = %+v", recognizer.opts.Format)
	}
}

// TestVoiceChannelAdapterEndToEnd 上行分片 → ASR final 事件 → 翻译 → 字幕
// + TTS 音频全链；Close 后 Feed 按契约返 ErrSessionClosed。
func TestVoiceChannelAdapterEndToEnd(t *testing.T) {
	translator := &rtTranslateStub{result: translationapp.TranslateResult{Text: "Hello", TargetLang: "en"}}
	synth := &vaSynthStub{}
	recognizer := &vaRecognizerStub{}
	sink := &vaSink{}

	stream, err := NewVoiceChannelService(translator, &rtPrefsStub{lang: "en"}, recognizer, synth).StartAudioStream(context.Background(), "s", VoiceSpeakerVisitor, sink)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// 上行分片直通会话。
	if err := stream.Feed([]byte("audio-chunk")); err != nil {
		t.Fatalf("feed: %v", err)
	}
	if got := recognizer.session.chunks; len(got) != 1 || string(got[0]) != "audio-chunk" {
		t.Fatalf("chunks = %v", got)
	}

	// final 事件驱动分句 → 翻译 → 字幕 + TTS。
	recognizer.session.events <- asr.Event{Kind: asr.EventFinal, Seq: 1, Text: "你好。"}
	captions := waitVoiceCaptions(t, sink, 1)
	if captions[0].Source != "你好。" || captions[0].Translated != "Hello" || captions[0].TargetLang != "en" {
		t.Fatalf("caption degraded: %+v", captions[0])
	}
	reqs := synth.recordedRequests()
	if len(reqs) != 1 || reqs[0].Text != "Hello" || reqs[0].Lang != "en" {
		t.Fatalf("tts requests degraded: %+v", reqs)
	}
	if _, audios := sink.snapshot(); len(audios) != 1 || string(audios[0]) != "aud" {
		t.Fatalf("audio downstream degraded")
	}

	// Close 收尾：会话关（事件通道关 → 管线返回），Feed 返 ErrSessionClosed。
	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !recognizer.session.isClosed() {
		t.Fatal("session must be closed by stream.Close")
	}
	if err := stream.Feed([]byte("late")); !errors.Is(err, asr.ErrSessionClosed) {
		t.Fatalf("feed after close = %v, want ErrSessionClosed", err)
	}
}

// TestVoiceChannelAdapterCaptionOnly TTS 未配置（nil synth）= 仅字幕合法
// 降级：字幕照常产出，无音频帧。
func TestVoiceChannelAdapterCaptionOnly(t *testing.T) {
	translator := &rtTranslateStub{result: translationapp.TranslateResult{Text: "Hi", TargetLang: "en"}}
	recognizer := &vaRecognizerStub{}
	sink := &vaSink{}

	stream, err := NewVoiceChannelService(translator, &rtPrefsStub{lang: "en"}, recognizer, nil).StartAudioStream(context.Background(), "s", VoiceSpeakerVisitor, sink)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stream.Close()

	recognizer.session.events <- asr.Event{Kind: asr.EventFinal, Seq: 1, Text: "嗨。"}
	captions := waitVoiceCaptions(t, sink, 1)
	if captions[0].Translated != "Hi" || captions[0].Degraded {
		t.Fatalf("caption degraded: %+v", captions[0])
	}
	if _, audios := sink.snapshot(); len(audios) != 0 {
		t.Fatalf("caption-only form must not emit audio, got %d", len(audios))
	}
}
