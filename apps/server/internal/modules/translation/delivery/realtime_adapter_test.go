package delivery

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	translationapp "servify/apps/server/internal/modules/translation/application"
)

// rtTranslateStub 翻译门面替身：捕获入参并注入结果/错误。
type rtTranslateStub struct {
	calls  atomic.Int64
	gotCmd translationapp.TranslateCommand
	result translationapp.TranslateResult
	err    error
}

func (s *rtTranslateStub) Translate(_ context.Context, cmd translationapp.TranslateCommand) (translationapp.TranslateResult, error) {
	s.calls.Add(1)
	s.gotCmd = cmd
	if s.err != nil {
		return translationapp.TranslateResult{}, s.err
	}
	return s.result, nil
}

// rtPrefsStub 偏好读取替身。
type rtPrefsStub struct {
	lang string
	err  error
}

func (s *rtPrefsStub) GetSessionLanguage(_ context.Context, _ string) (string, error) {
	return s.lang, s.err
}

var _ RealtimeTranslateService = (*realtimeTranslateServiceAdapter)(nil)

// TestRealtimeTranslateAdapter 刀二 hub 自动翻译适配器：无偏好静默跳过、
// 有偏好走门面并映射帧载荷、未装配降级不可用、错误原样上抛。
func TestRealtimeTranslateAdapter(t *testing.T) {
	ctx := context.Background()

	t.Run("no preference skips silently", func(t *testing.T) {
		translator := &rtTranslateStub{}
		adapter := NewRealtimeTranslateService(translator, &rtPrefsStub{lang: ""})
		got, err := adapter.TranslateSessionMessage(ctx, "conv-1", "你好")
		if err != nil || got != nil {
			t.Fatalf("want nil/nil for unset preference, got %+v %v", got, err)
		}
		if translator.calls.Load() != 0 {
			t.Fatalf("translate must not be called without preference, calls=%d", translator.calls.Load())
		}
	})

	t.Run("preference set translates via facade and maps payload", func(t *testing.T) {
		translator := &rtTranslateStub{result: translationapp.TranslateResult{
			Text: "hello", SourceLang: "zh", TargetLang: "en",
		}}
		adapter := NewRealtimeTranslateService(translator, &rtPrefsStub{lang: "en"})
		got, err := adapter.TranslateSessionMessage(ctx, "conv-1", "你好")
		if err != nil {
			t.Fatalf("translate: %v", err)
		}
		if got == nil {
			t.Fatal("want translation payload")
		}
		if got.Original != "你好" || got.Content != "hello" || got.SourceLang != "zh" || got.TargetLang != "en" {
			t.Fatalf("payload = %+v", got)
		}
		if translator.gotCmd != (translationapp.TranslateCommand{Text: "你好", TargetLang: "en"}) {
			t.Fatalf("cmd = %+v", translator.gotCmd)
		}
	})

	t.Run("preference reader error passes through", func(t *testing.T) {
		boom := errors.New("pref store down")
		adapter := NewRealtimeTranslateService(&rtTranslateStub{}, &rtPrefsStub{err: boom})
		if _, err := adapter.TranslateSessionMessage(ctx, "conv-1", "hi"); !errors.Is(err, boom) {
			t.Fatalf("want raw pref error, got %v", err)
		}
	})

	t.Run("translate unavailable and raw errors pass through", func(t *testing.T) {
		adapter := NewRealtimeTranslateService(&rtTranslateStub{err: ErrTranslationUnavailable}, &rtPrefsStub{lang: "en"})
		if _, err := adapter.TranslateSessionMessage(ctx, "conv-1", "hi"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("want unavailable, got %v", err)
		}
		boom := errors.New("provider 502")
		adapter = NewRealtimeTranslateService(&rtTranslateStub{err: boom}, &rtPrefsStub{lang: "en"})
		if _, err := adapter.TranslateSessionMessage(ctx, "conv-1", "hi"); !errors.Is(err, boom) {
			t.Fatalf("want raw provider error, got %v", err)
		}
	})

	t.Run("nil surfaces degrade to unavailable", func(t *testing.T) {
		if _, err := NewRealtimeTranslateService(nil, &rtPrefsStub{lang: "en"}).TranslateSessionMessage(ctx, "c", "hi"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("nil translator: %v", err)
		}
		if _, err := NewRealtimeTranslateService(&rtTranslateStub{}, nil).TranslateSessionMessage(ctx, "c", "hi"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("nil prefs: %v", err)
		}
		var nilAdapter *realtimeTranslateServiceAdapter
		if _, err := nilAdapter.TranslateSessionMessage(ctx, "c", "hi"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("nil adapter: %v", err)
		}
	})
}
