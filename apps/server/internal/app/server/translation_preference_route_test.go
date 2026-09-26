package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubTranslationPreferenceService struct {
	lang string
	err  error
}

func (s *stubTranslationPreferenceService) SetSessionLanguage(_ context.Context, _, targetLang string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return targetLang, nil
}

func (s *stubTranslationPreferenceService) GetSessionLanguage(_ context.Context, _ string) (string, error) {
	return s.lang, s.err
}

func (s *stubTranslationPreferenceService) ClearSessionLanguage(_ context.Context, _ string) error {
	return s.err
}

// TestBuildRouter_TranslationPreferenceRoutes 会话语言偏好路由（Phase 1 刀一）：
// 管理面（agent 主体放行、匿名 401）；未装配时不注册（404）。
func TestBuildRouter_TranslationPreferenceRoutes(t *testing.T) {
	router := BuildRouter(Dependencies{
		Config:                              testRouterConfig(),
		TranslationPreferenceHandlerService: &stubTranslationPreferenceService{lang: "en"},
	})

	t.Run("anonymous rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/translation/preferences/conv-1", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("agent principal reads preference", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        7,
			"principal_kind": "agent",
		}, "test-secret")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/translation/preferences/conv-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"target_lang":"en"`) {
			t.Fatalf("expected stored lang, body=%s", w.Body.String())
		}
	})

	t.Run("agent principal writes preference", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        7,
			"principal_kind": "agent",
		}, "test-secret")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/translation/preferences/conv-1", strings.NewReader(`{"target_lang":"fr"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"target_lang":"fr"`) {
			t.Fatalf("expected upserted lang, body=%s", w.Body.String())
		}
	})

	t.Run("service errors surface through the management chain", func(t *testing.T) {
		router := BuildRouter(Dependencies{
			Config:                              testRouterConfig(),
			TranslationPreferenceHandlerService: &stubTranslationPreferenceService{err: errors.New("storage down")},
		})
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        7,
			"principal_kind": "agent",
		}, "test-secret")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/translation/preferences/conv-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("route absent when service not wired", func(t *testing.T) {
		anonymous := BuildRouter(Dependencies{Config: testRouterConfig()})
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        7,
			"principal_kind": "agent",
		}, "test-secret")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/translation/preferences/conv-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		anonymous.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 when preference service unwired, got %d", w.Code)
		}
	})
}
