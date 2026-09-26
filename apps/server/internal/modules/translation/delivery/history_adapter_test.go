package delivery

import (
	"context"
	"errors"
	"testing"
)

var _ HistoryTranslateService = (*historyTranslateServiceAdapter)(nil)

// TestHistoryTranslateAdapter 历史批量标注适配器（Phase 1 收尾）：无偏好
// 静默跳过、有偏好走批量门面并回传目标语言、viewer 构造期贯穿、nil 安全面
// 与错误原样上抛。
func TestHistoryTranslateAdapter(t *testing.T) {
	ctx := context.Background()

	t.Run("no preference skips silently", func(t *testing.T) {
		translator := &rtTranslateStub{}
		adapter := NewHistoryTranslateService(translator, &rtPrefsStub{lang: ""}, ViewerRoleAgent)
		got, err := adapter.TranslateHistory(ctx, "conv-1", []string{"你好"})
		if err != nil || got != nil {
			t.Fatalf("want nil/nil for unset preference, got %+v %v", got, err)
		}
		if translator.calls.Load() != 0 {
			t.Fatalf("facade must not be called without preference, calls=%d", translator.calls.Load())
		}
	})

	t.Run("preference set batches via facade", func(t *testing.T) {
		translator := &rtTranslateStub{batchResult: []string{"hello", "hi"}}
		adapter := NewHistoryTranslateService(translator, &rtPrefsStub{lang: "en"}, ViewerRoleAgent)
		got, err := adapter.TranslateHistory(ctx, "conv-1", []string{"你好", "你好啊"})
		if err != nil {
			t.Fatalf("batch: %v", err)
		}
		if got == nil || len(got.Texts) != 2 || got.Texts[0] != "hello" || got.TargetLang != "en" {
			t.Fatalf("result = %+v", got)
		}
		if len(translator.gotBatchCmd.Texts) != 2 || translator.gotBatchCmd.TargetLang != "en" {
			t.Fatalf("cmd = %+v", translator.gotBatchCmd)
		}
	})

	t.Run("viewer is pinned to constructor binding", func(t *testing.T) {
		prefs := &rtPrefsStub{lang: "en"}
		adapter := NewHistoryTranslateService(&rtTranslateStub{batchResult: []string{"x"}}, prefs, ViewerRoleVisitor)
		if _, err := adapter.TranslateHistory(ctx, "conv-1", []string{"hi"}); err != nil {
			t.Fatalf("visitor adapter: %v", err)
		}
		if prefs.lastViewer != ViewerRoleVisitor {
			t.Fatalf("prefs must see visitor role, got %q", prefs.lastViewer)
		}
	})

	t.Run("preference and provider errors pass through", func(t *testing.T) {
		boom := errors.New("pref store down")
		adapter := NewHistoryTranslateService(&rtTranslateStub{}, &rtPrefsStub{err: boom}, ViewerRoleAgent)
		if _, err := adapter.TranslateHistory(ctx, "conv-1", []string{"hi"}); !errors.Is(err, boom) {
			t.Fatalf("want raw pref error, got %v", err)
		}
		providerErr := errors.New("provider 502")
		adapter = NewHistoryTranslateService(&rtTranslateStub{err: providerErr}, &rtPrefsStub{lang: "en"}, ViewerRoleAgent)
		if _, err := adapter.TranslateHistory(ctx, "conv-1", []string{"hi"}); !errors.Is(err, providerErr) {
			t.Fatalf("want raw provider error, got %v", err)
		}
	})

	t.Run("nil surfaces degrade to unavailable", func(t *testing.T) {
		if _, err := NewHistoryTranslateService(nil, &rtPrefsStub{lang: "en"}, ViewerRoleAgent).TranslateHistory(ctx, "c", []string{"hi"}); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("nil translator: %v", err)
		}
		if _, err := NewHistoryTranslateService(&rtTranslateStub{}, nil, ViewerRoleAgent).TranslateHistory(ctx, "c", []string{"hi"}); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("nil prefs: %v", err)
		}
		var nilAdapter *historyTranslateServiceAdapter
		if _, err := nilAdapter.TranslateHistory(ctx, "c", []string{"hi"}); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("nil adapter: %v", err)
		}
	})
}
