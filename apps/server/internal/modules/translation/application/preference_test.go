package application_test

import (
	"context"
	"errors"
	"testing"

	translationapp "servify/apps/server/internal/modules/translation/application"
	"servify/apps/server/internal/modules/translation/domain"
	platformauth "servify/apps/server/internal/platform/auth"
)

type fakePreferenceStore struct {
	upserted  *domain.TranslationLanguagePreference
	get       *domain.TranslationLanguagePreference
	getErr    error
	upsertErr error
	deleteErr error
	deleted   []string
}

func (f *fakePreferenceStore) UpsertPreference(_ context.Context, pref *domain.TranslationLanguagePreference) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upserted = pref
	return nil
}

func (f *fakePreferenceStore) GetPreference(_ context.Context, sessionID string) (*domain.TranslationLanguagePreference, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.get, nil
}

func (f *fakePreferenceStore) DeletePreference(_ context.Context, sessionID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, sessionID)
	return nil
}

func scopedContext() context.Context {
	return platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")
}

func TestSetSessionLanguageValidation(t *testing.T) {
	store := &fakePreferenceStore{}
	svc := translationapp.NewPreferenceService(store)

	t.Run("empty session id rejected", func(t *testing.T) {
		if _, err := svc.SetSessionLanguage(scopedContext(), "  ", "en"); !errors.Is(err, translationapp.ErrTranslationSessionRequired) {
			t.Fatalf("want ErrTranslationSessionRequired, got %v", err)
		}
	})

	t.Run("empty target lang rejected", func(t *testing.T) {
		if _, err := svc.SetSessionLanguage(scopedContext(), "conv-1", "   "); !errors.Is(err, translationapp.ErrTranslationTargetRequired) {
			t.Fatalf("want ErrTranslationTargetRequired, got %v", err)
		}
	})

	t.Run("invalid lang tag rejected", func(t *testing.T) {
		if _, err := svc.SetSessionLanguage(scopedContext(), "conv-1", "not a lang!"); !errors.Is(err, translationapp.ErrTranslationLangInvalid) {
			t.Fatalf("want ErrTranslationLangInvalid, got %v", err)
		}
	})

	t.Run("store error passthrough", func(t *testing.T) {
		boom := errors.New("boom")
		store.upsertErr = boom
		if _, err := svc.SetSessionLanguage(scopedContext(), "conv-1", "en"); !errors.Is(err, boom) {
			t.Fatalf("want raw store error, got %v", err)
		}
		store.upsertErr = nil
	})
}

func TestSetSessionLanguageUpsertsWithScope(t *testing.T) {
	store := &fakePreferenceStore{}
	svc := translationapp.NewPreferenceService(store)

	got, err := svc.SetSessionLanguage(scopedContext(), "conv-1", "ZH-CN")
	if err != nil {
		t.Fatalf("SetSessionLanguage() error = %v", err)
	}
	// 语言标签小写规范化（与 Translate 回显口径一致）。
	if got != "zh-cn" {
		t.Fatalf("want normalized zh-cn, got %q", got)
	}
	if store.upserted == nil {
		t.Fatalf("store must receive upsert")
	}
	if store.upserted.ConversationSessionID != "conv-1" || store.upserted.TargetLang != "zh-cn" {
		t.Fatalf("unexpected pref payload: %+v", store.upserted)
	}
	// scope 取自认证 ctx，不接受请求方自报。
	if store.upserted.TenantID != "tenant-a" || store.upserted.WorkspaceID != "ws-1" {
		t.Fatalf("scope must come from ctx: %+v", store.upserted)
	}
}

func TestGetSessionLanguage(t *testing.T) {
	store := &fakePreferenceStore{}
	svc := translationapp.NewPreferenceService(store)

	t.Run("unset returns empty string not error", func(t *testing.T) {
		lang, err := svc.GetSessionLanguage(scopedContext(), "conv-404")
		if err != nil || lang != "" {
			t.Fatalf("want empty lang no error, got %q %v", lang, err)
		}
	})

	t.Run("set returns stored lang", func(t *testing.T) {
		store.get = &domain.TranslationLanguagePreference{ConversationSessionID: "conv-1", TargetLang: "en"}
		lang, err := svc.GetSessionLanguage(scopedContext(), "conv-1")
		if err != nil || lang != "en" {
			t.Fatalf("want en, got %q %v", lang, err)
		}
	})

	t.Run("store error passthrough", func(t *testing.T) {
		boom := errors.New("db down")
		store.get = nil
		store.getErr = boom
		if _, err := svc.GetSessionLanguage(scopedContext(), "conv-1"); !errors.Is(err, boom) {
			t.Fatalf("want raw store error, got %v", err)
		}
		store.getErr = nil
	})

	t.Run("empty session id rejected", func(t *testing.T) {
		if _, err := svc.GetSessionLanguage(scopedContext(), ""); !errors.Is(err, translationapp.ErrTranslationSessionRequired) {
			t.Fatalf("want ErrTranslationSessionRequired, got %v", err)
		}
	})
}

func TestClearSessionLanguage(t *testing.T) {
	store := &fakePreferenceStore{}
	svc := translationapp.NewPreferenceService(store)

	if err := svc.ClearSessionLanguage(scopedContext(), "conv-1"); err != nil {
		t.Fatalf("ClearSessionLanguage() error = %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "conv-1" {
		t.Fatalf("unexpected deletions: %v", store.deleted)
	}

	if err := svc.ClearSessionLanguage(scopedContext(), ""); !errors.Is(err, translationapp.ErrTranslationSessionRequired) {
		t.Fatalf("want ErrTranslationSessionRequired, got %v", err)
	}

	boom := errors.New("boom")
	store.deleteErr = boom
	if err := svc.ClearSessionLanguage(scopedContext(), "conv-1"); !errors.Is(err, boom) {
		t.Fatalf("want raw store error, got %v", err)
	}
}

func TestPreferenceServiceNilSafety(t *testing.T) {
	var svc *translationapp.PreferenceService
	ctx := context.Background()
	if _, err := svc.SetSessionLanguage(ctx, "conv-1", "en"); !errors.Is(err, translationapp.ErrTranslationUnavailable) {
		t.Fatalf("want ErrTranslationUnavailable, got %v", err)
	}
	if _, err := svc.GetSessionLanguage(ctx, "conv-1"); !errors.Is(err, translationapp.ErrTranslationUnavailable) {
		t.Fatalf("want ErrTranslationUnavailable, got %v", err)
	}
	if err := svc.ClearSessionLanguage(ctx, "conv-1"); !errors.Is(err, translationapp.ErrTranslationUnavailable) {
		t.Fatalf("want ErrTranslationUnavailable, got %v", err)
	}
	// store 为 nil 同样降级（服务构造了但未注入存储的防御面）。
	bare := translationapp.NewPreferenceService(nil)
	if _, err := bare.SetSessionLanguage(ctx, "conv-1", "en"); !errors.Is(err, translationapp.ErrTranslationUnavailable) {
		t.Fatalf("want ErrTranslationUnavailable, got %v", err)
	}
}
