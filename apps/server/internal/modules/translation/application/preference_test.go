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
	deleted   [][2]string
}

func (f *fakePreferenceStore) UpsertPreference(_ context.Context, pref *domain.TranslationLanguagePreference) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upserted = pref
	return nil
}

func (f *fakePreferenceStore) GetPreference(_ context.Context, sessionID, viewerRole string) (*domain.TranslationLanguagePreference, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.get != nil && f.get.ViewerRole != viewerRole {
		return nil, nil
	}
	return f.get, nil
}

func (f *fakePreferenceStore) DeletePreference(_ context.Context, sessionID, viewerRole string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, [2]string{sessionID, viewerRole})
	return nil
}

func scopedContext() context.Context {
	return platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")
}

func TestSetSessionLanguageValidation(t *testing.T) {
	store := &fakePreferenceStore{}
	svc := translationapp.NewPreferenceService(store)

	t.Run("empty session id rejected", func(t *testing.T) {
		if _, err := svc.SetSessionLanguage(scopedContext(), "  ", translationapp.ViewerRoleAgent, "en"); !errors.Is(err, translationapp.ErrTranslationSessionRequired) {
			t.Fatalf("want ErrTranslationSessionRequired, got %v", err)
		}
	})

	t.Run("invalid viewer role rejected", func(t *testing.T) {
		if _, err := svc.SetSessionLanguage(scopedContext(), "conv-1", "admin", "en"); !errors.Is(err, translationapp.ErrTranslationViewerInvalid) {
			t.Fatalf("want ErrTranslationViewerInvalid, got %v", err)
		}
	})

	t.Run("empty viewer role rejected", func(t *testing.T) {
		if _, err := svc.SetSessionLanguage(scopedContext(), "conv-1", "", "en"); !errors.Is(err, translationapp.ErrTranslationViewerInvalid) {
			t.Fatalf("want ErrTranslationViewerInvalid, got %v", err)
		}
	})

	t.Run("empty target lang rejected", func(t *testing.T) {
		if _, err := svc.SetSessionLanguage(scopedContext(), "conv-1", translationapp.ViewerRoleVisitor, "   "); !errors.Is(err, translationapp.ErrTranslationTargetRequired) {
			t.Fatalf("want ErrTranslationTargetRequired, got %v", err)
		}
	})

	t.Run("invalid lang tag rejected", func(t *testing.T) {
		if _, err := svc.SetSessionLanguage(scopedContext(), "conv-1", translationapp.ViewerRoleVisitor, "not a lang!"); !errors.Is(err, translationapp.ErrTranslationLangInvalid) {
			t.Fatalf("want ErrTranslationLangInvalid, got %v", err)
		}
	})

	t.Run("store error passthrough", func(t *testing.T) {
		boom := errors.New("boom")
		store.upsertErr = boom
		if _, err := svc.SetSessionLanguage(scopedContext(), "conv-1", translationapp.ViewerRoleAgent, "en"); !errors.Is(err, boom) {
			t.Fatalf("want raw store error, got %v", err)
		}
		store.upsertErr = nil
	})
}

func TestSetSessionLanguageUpsertsWithScope(t *testing.T) {
	store := &fakePreferenceStore{}
	svc := translationapp.NewPreferenceService(store)

	got, err := svc.SetSessionLanguage(scopedContext(), "conv-1", translationapp.ViewerRoleAgent, "ZH-CN")
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
	if store.upserted.ViewerRole != translationapp.ViewerRoleAgent {
		t.Fatalf("pref must carry viewer role agent, got %q", store.upserted.ViewerRole)
	}
	// scope 取自认证 ctx，不接受请求方自报。
	if store.upserted.TenantID != "tenant-a" || store.upserted.WorkspaceID != "ws-1" {
		t.Fatalf("scope must come from ctx: %+v", store.upserted)
	}

	// viewer role 大小写不敏感，规范化后落库。
	if _, err := svc.SetSessionLanguage(scopedContext(), "conv-1", "Visitor", "en"); err != nil {
		t.Fatalf("case-insensitive viewer role: %v", err)
	}
	if store.upserted.ViewerRole != translationapp.ViewerRoleVisitor {
		t.Fatalf("viewer role must normalize to visitor, got %q", store.upserted.ViewerRole)
	}
}

func TestGetSessionLanguage(t *testing.T) {
	store := &fakePreferenceStore{}
	svc := translationapp.NewPreferenceService(store)

	t.Run("unset returns empty string not error", func(t *testing.T) {
		lang, err := svc.GetSessionLanguage(scopedContext(), "conv-404", translationapp.ViewerRoleAgent)
		if err != nil || lang != "" {
			t.Fatalf("want empty lang no error, got %q %v", lang, err)
		}
	})

	t.Run("set returns stored lang", func(t *testing.T) {
		store.get = &domain.TranslationLanguagePreference{ConversationSessionID: "conv-1", ViewerRole: translationapp.ViewerRoleAgent, TargetLang: "en"}
		lang, err := svc.GetSessionLanguage(scopedContext(), "conv-1", translationapp.ViewerRoleAgent)
		if err != nil || lang != "en" {
			t.Fatalf("want en, got %q %v", lang, err)
		}
	})

	t.Run("role mismatch reads nothing", func(t *testing.T) {
		lang, err := svc.GetSessionLanguage(scopedContext(), "conv-1", translationapp.ViewerRoleVisitor)
		if err != nil || lang != "" {
			t.Fatalf("visitor must not read agent row, got %q %v", lang, err)
		}
	})

	t.Run("store error passthrough", func(t *testing.T) {
		boom := errors.New("db down")
		store.get = nil
		store.getErr = boom
		if _, err := svc.GetSessionLanguage(scopedContext(), "conv-1", translationapp.ViewerRoleAgent); !errors.Is(err, boom) {
			t.Fatalf("want raw store error, got %v", err)
		}
		store.getErr = nil
	})

	t.Run("empty session id rejected", func(t *testing.T) {
		if _, err := svc.GetSessionLanguage(scopedContext(), "", translationapp.ViewerRoleAgent); !errors.Is(err, translationapp.ErrTranslationSessionRequired) {
			t.Fatalf("want ErrTranslationSessionRequired, got %v", err)
		}
	})

	t.Run("invalid viewer role rejected", func(t *testing.T) {
		if _, err := svc.GetSessionLanguage(scopedContext(), "conv-1", "bot"); !errors.Is(err, translationapp.ErrTranslationViewerInvalid) {
			t.Fatalf("want ErrTranslationViewerInvalid, got %v", err)
		}
	})
}

func TestClearSessionLanguage(t *testing.T) {
	store := &fakePreferenceStore{}
	svc := translationapp.NewPreferenceService(store)

	if err := svc.ClearSessionLanguage(scopedContext(), "conv-1", translationapp.ViewerRoleVisitor); err != nil {
		t.Fatalf("ClearSessionLanguage() error = %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != [2]string{"conv-1", translationapp.ViewerRoleVisitor} {
		t.Fatalf("unexpected deletions: %v", store.deleted)
	}

	if err := svc.ClearSessionLanguage(scopedContext(), "", translationapp.ViewerRoleVisitor); !errors.Is(err, translationapp.ErrTranslationSessionRequired) {
		t.Fatalf("want ErrTranslationSessionRequired, got %v", err)
	}

	if err := svc.ClearSessionLanguage(scopedContext(), "conv-1", "root"); !errors.Is(err, translationapp.ErrTranslationViewerInvalid) {
		t.Fatalf("want ErrTranslationViewerInvalid, got %v", err)
	}

	boom := errors.New("boom")
	store.deleteErr = boom
	if err := svc.ClearSessionLanguage(scopedContext(), "conv-1", translationapp.ViewerRoleVisitor); !errors.Is(err, boom) {
		t.Fatalf("want raw store error, got %v", err)
	}
}

func TestPreferenceServiceNilSafety(t *testing.T) {
	var svc *translationapp.PreferenceService
	ctx := context.Background()
	if _, err := svc.SetSessionLanguage(ctx, "conv-1", translationapp.ViewerRoleAgent, "en"); !errors.Is(err, translationapp.ErrTranslationUnavailable) {
		t.Fatalf("want ErrTranslationUnavailable, got %v", err)
	}
	if _, err := svc.GetSessionLanguage(ctx, "conv-1", translationapp.ViewerRoleAgent); !errors.Is(err, translationapp.ErrTranslationUnavailable) {
		t.Fatalf("want ErrTranslationUnavailable, got %v", err)
	}
	if err := svc.ClearSessionLanguage(ctx, "conv-1", translationapp.ViewerRoleAgent); !errors.Is(err, translationapp.ErrTranslationUnavailable) {
		t.Fatalf("want ErrTranslationUnavailable, got %v", err)
	}
	// store 为 nil 同样降级（服务构造了但未注入存储的防御面）。
	bare := translationapp.NewPreferenceService(nil)
	if _, err := bare.SetSessionLanguage(ctx, "conv-1", translationapp.ViewerRoleAgent, "en"); !errors.Is(err, translationapp.ErrTranslationUnavailable) {
		t.Fatalf("want ErrTranslationUnavailable, got %v", err)
	}
}
