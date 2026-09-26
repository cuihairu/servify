package delivery

import (
	"context"
	"errors"
	"testing"

	translationapp "servify/apps/server/internal/modules/translation/application"
)

// TestPreferenceHandlerServiceAdapter 适配器 nil 安全面：inner 未装配
// （nil 服务或 nil 存储）时统一降级 ErrTranslationUnavailable。
func TestPreferenceHandlerServiceAdapter(t *testing.T) {
	ctx := context.Background()

	t.Run("nil inner store degrades to unavailable", func(t *testing.T) {
		adapter := NewPreferenceHandlerService(translationapp.NewPreferenceService(nil))
		if _, err := adapter.SetSessionLanguage(ctx, "conv-1", "en"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("SetSessionLanguage error = %v, want ErrTranslationUnavailable", err)
		}
		if _, err := adapter.GetSessionLanguage(ctx, "conv-1"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("GetSessionLanguage error = %v, want ErrTranslationUnavailable", err)
		}
		if err := adapter.ClearSessionLanguage(ctx, "conv-1"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("ClearSessionLanguage error = %v, want ErrTranslationUnavailable", err)
		}
	})

	t.Run("nil adapter is nil-safe", func(t *testing.T) {
		var nilAdapter *preferenceHandlerServiceAdapter
		if _, err := nilAdapter.SetSessionLanguage(ctx, "conv-1", "en"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("SetSessionLanguage nil adapter error = %v", err)
		}
		if _, err := nilAdapter.GetSessionLanguage(ctx, "conv-1"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("GetSessionLanguage nil adapter error = %v", err)
		}
		if err := nilAdapter.ClearSessionLanguage(ctx, "conv-1"); !errors.Is(err, ErrTranslationUnavailable) {
			t.Fatalf("ClearSessionLanguage nil adapter error = %v", err)
		}
	})

	t.Run("interface satisfied by adapter", func(t *testing.T) {
		var _ PreferenceHandlerService = NewPreferenceHandlerService(nil)
	})
}
