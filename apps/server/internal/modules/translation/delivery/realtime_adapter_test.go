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

// rtPrefsStub 偏好读取替身：记录 (sessionID, viewer) 查询对。
type rtPrefsStub struct {
	lang       string
	err        error
	lastViewer string
}

func (s *rtPrefsStub) GetSessionLanguage(_ context.Context, _ string, viewer string) (string, error) {
	s.lastViewer = viewer
	return s.lang, s.err
}

var _ RealtimeTranslateService = (*realtimeTranslateServiceAdapter)(nil)

// TestRealtimeTranslateAdapter 自动翻译适配器（刀二 hub 读向 + 刀三坐席
// 发送口读向共型）：无偏好静默跳过、有偏好走门面并映射帧载荷、viewer 贯穿
// 偏好查询、未装配降级不可用、错误原样上抛。
func TestRealtimeTranslateAdapter(t *testing.T) {
	ctx := context.Background()

	t.Run("no preference skips silently", func(t *testing.T) {
		translator := &rtTranslateStub{}
		adapter := NewRealtimeTranslateService(translator, &rtPrefsStub{lang: ""}, ViewerRoleAgent)
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
		adapter := NewRealtimeTranslateService(translator, &rtPrefsStub{lang: "en"}, ViewerRoleAgent)
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

	t.Run("viewer is pinned to constructor binding", func(t *testing.T) {
		// hub 实例查 agent 读向、发送口实例查 visitor 读向——同一会话同一
		// 文本走不同偏好行，靠构造期绑定区分，不随请求变。
		prefs := &rtPrefsStub{lang: "en"}
		adapter := NewRealtimeTranslateService(&rtTranslateStub{}, prefs, ViewerRoleVisitor)
		if _, err := adapter.TranslateSessionMessage(ctx, "conv-1", "hi"); err != nil {
			t.Fatalf("visitor adapter: %v", err)
		}
		if prefs.lastViewer != ViewerRoleVisitor {
			t.Fatalf("prefs must see visitor role, got %q", prefs.lastViewer)
		}
	})

	t.Run("preference reader error passes through", func(t *testing.T) {
		boom := errors.New("pref store down")
		adapter := NewRealtimeTranslateService(&rtTranslateStub{}, &rtPrefsStub{err: boom}, ViewerRoleAgent)
		if _, err := adapter.TranslateSessionMessage(ctx, "conv-1", "hi"); !errors.Is(err, boom) {
			t.Fatalf("want raw pref error, got %v", err)
		}
	})

	t.Run("translate unavailable and raw errors pass through", func(t *testing.T) {
		adapter := NewRealtimeTranslateService(&rtTranslateStub{err: ErrTranslationUnavailable}, &rtPrefsStub{lang: "en"}, ViewerRoleAgent)
		if _, err := adapter.TranslateSessionMessage(ctx, "conv-1", "hi"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("want unavailable, got %v", err)
		}
		boom := errors.New("provider 502")
		adapter = NewRealtimeTranslateService(&rtTranslateStub{err: boom}, &rtPrefsStub{lang: "en"}, ViewerRoleAgent)
		if _, err := adapter.TranslateSessionMessage(ctx, "conv-1", "hi"); !errors.Is(err, boom) {
			t.Fatalf("want raw provider error, got %v", err)
		}
	})

	t.Run("nil surfaces degrade to unavailable", func(t *testing.T) {
		if _, err := NewRealtimeTranslateService(nil, &rtPrefsStub{lang: "en"}, ViewerRoleAgent).TranslateSessionMessage(ctx, "c", "hi"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("nil translator: %v", err)
		}
		if _, err := NewRealtimeTranslateService(&rtTranslateStub{}, nil, ViewerRoleAgent).TranslateSessionMessage(ctx, "c", "hi"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("nil prefs: %v", err)
		}
		var nilAdapter *realtimeTranslateServiceAdapter
		if _, err := nilAdapter.TranslateSessionMessage(ctx, "c", "hi"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("nil adapter: %v", err)
		}
	})
}
