package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	translationdelivery "servify/apps/server/internal/modules/translation/delivery"
)

type stubTranslationHandlerService struct{}

func (stubTranslationHandlerService) Translate(_ context.Context, cmd translationdelivery.TranslateCommand) (translationdelivery.TranslateResult, error) {
	return translationdelivery.TranslateResult{
		Text:       "translated:" + cmd.Text,
		SourceLang: cmd.SourceLang,
		TargetLang: cmd.TargetLang,
	}, nil
}

func (stubTranslationHandlerService) BatchTranslate(_ context.Context, cmd translationdelivery.BatchTranslateCommand) (translationdelivery.BatchTranslateResult, error) {
	return translationdelivery.BatchTranslateResult{Texts: []string{}, TargetLang: cmd.TargetLang}, nil
}

// TestBuildRouter_VisitorTranslationRoute 访客面翻译路由（Phase 0.5 服务端半边）：
// 匿名 401；访客（end_user）令牌放行——与坐席面同契约、同服务实例。
func TestBuildRouter_VisitorTranslationRoute(t *testing.T) {
	router := BuildRouter(Dependencies{
		Config:                    testRouterConfig(),
		TranslationHandlerService: stubTranslationHandlerService{},
	})

	t.Run("missing token rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/translation/translate", strings.NewReader(`{"text":"hi","target_lang":"en"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("end user token allowed", func(t *testing.T) {
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        12,
			"principal_kind": "end_user",
		}, "test-secret")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/translation/translate", strings.NewReader(`{"text":"你好","target_lang":"en"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"text":"translated:你好"`) {
			t.Fatalf("expected translated payload, body=%s", w.Body.String())
		}
	})

	t.Run("route absent when service not wired", func(t *testing.T) {
		anonymous := BuildRouter(Dependencies{Config: testRouterConfig()})
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        12,
			"principal_kind": "end_user",
		}, "test-secret")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/translation/translate", strings.NewReader(`{"text":"hi","target_lang":"en"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		anonymous.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 when translation service unwired, got %d", w.Code)
		}
	})
}
