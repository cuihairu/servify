package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type stubTranslationPreferenceService struct {
	mu        sync.Mutex
	lang      string
	viewerHit string
	err       error
}

func (s *stubTranslationPreferenceService) SetSessionLanguage(_ context.Context, _, viewer, targetLang string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viewerHit = viewer
	if s.err != nil {
		return "", s.err
	}
	return targetLang, nil
}

func (s *stubTranslationPreferenceService) GetSessionLanguage(_ context.Context, _, viewer string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viewerHit = viewer
	return s.lang, s.err
}

func (s *stubTranslationPreferenceService) ClearSessionLanguage(_ context.Context, _, viewer string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viewerHit = viewer
	return s.err
}

func (s *stubTranslationPreferenceService) hitViewer() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewerHit
}

// TestBuildRouter_TranslationPreferenceRoutes 会话语言偏好路由（Phase 1
// 刀一双面 + 刀三角色维度）：单一注册点认证即可（agent 主体读 agent 读向、
// end_user 主体读 visitor 读向并强制 token 会话绑定）；匿名 401、跨会话访客
// 403；未装配时不注册（404）。
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

	t.Run("agent principal reads agent viewer role", func(t *testing.T) {
		svc := &stubTranslationPreferenceService{lang: "en"}
		agentRouter := BuildRouter(Dependencies{Config: testRouterConfig(), TranslationPreferenceHandlerService: svc})
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        7,
			"principal_kind": "agent",
		}, "test-secret")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/translation/preferences/conv-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		agentRouter.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"target_lang":"en"`) {
			t.Fatalf("expected stored lang, body=%s", w.Body.String())
		}
		if svc.hitViewer() != "agent" {
			t.Fatalf("agent principal must hit agent viewer role, got %q", svc.hitViewer())
		}
	})

	t.Run("bound visitor reads visitor viewer role", func(t *testing.T) {
		svc := &stubTranslationPreferenceService{lang: "ja"}
		visitorRouter := BuildRouter(Dependencies{Config: testRouterConfig(), TranslationPreferenceHandlerService: svc})
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        12,
			"principal_kind": "end_user",
			"session_id":     "conv-1",
		}, "test-secret")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/translation/preferences/conv-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		visitorRouter.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}
		if svc.hitViewer() != "visitor" {
			t.Fatalf("end_user principal must hit visitor viewer role, got %q", svc.hitViewer())
		}
	})

	t.Run("cross session visitor rejected with 403", func(t *testing.T) {
		svc := &stubTranslationPreferenceService{}
		visitorRouter := BuildRouter(Dependencies{Config: testRouterConfig(), TranslationPreferenceHandlerService: svc})
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        12,
			"principal_kind": "end_user",
			"session_id":     "conv-other",
		}, "test-secret")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/translation/preferences/conv-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		visitorRouter.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d body=%s", w.Code, w.Body.String())
		}
		if svc.hitViewer() != "" {
			t.Fatalf("service must not be reached on session mismatch, hit=%q", svc.hitViewer())
		}
	})

	t.Run("visitor without session claim rejected with 403", func(t *testing.T) {
		visitorRouter := BuildRouter(Dependencies{
			Config:                              testRouterConfig(),
			TranslationPreferenceHandlerService: &stubTranslationPreferenceService{},
		})
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        12,
			"principal_kind": "end_user",
		}, "test-secret")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/translation/preferences/conv-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		visitorRouter.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("agent principal writes preference", func(t *testing.T) {
		svc := &stubTranslationPreferenceService{}
		agentRouter := BuildRouter(Dependencies{Config: testRouterConfig(), TranslationPreferenceHandlerService: svc})
		token := createTestHS256JWT(t, map[string]interface{}{
			"user_id":        7,
			"principal_kind": "agent",
		}, "test-secret")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/translation/preferences/conv-1", strings.NewReader(`{"target_lang":"fr"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		agentRouter.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"target_lang":"fr"`) {
			t.Fatalf("expected upserted lang, body=%s", w.Body.String())
		}
		if svc.hitViewer() != "agent" {
			t.Fatalf("write must land on agent viewer role, got %q", svc.hitViewer())
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
