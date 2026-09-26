package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	translationdelivery "servify/apps/server/internal/modules/translation/delivery"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

var _ translationdelivery.PreferenceHandlerService = (*cvhTranslationPreferenceService)(nil)

// cvhTranslationPreferenceService 偏好服务 inline mock：捕获参数 + 注入结果/错误。
type cvhTranslationPreferenceService struct {
	setSessionID string
	setViewer    string
	setLang      string
	setResult    string
	getSessionID string
	getViewer    string
	getLang      string
	clearID      string
	clearViewer  string
	err          error
}

func (s *cvhTranslationPreferenceService) SetSessionLanguage(_ context.Context, sessionID, viewer, targetLang string) (string, error) {
	s.setSessionID = sessionID
	s.setViewer = viewer
	s.setLang = targetLang
	if s.err != nil {
		return "", s.err
	}
	return s.setResult, nil
}

func (s *cvhTranslationPreferenceService) GetSessionLanguage(_ context.Context, sessionID, viewer string) (string, error) {
	s.getSessionID = sessionID
	s.getViewer = viewer
	if s.err != nil {
		return "", s.err
	}
	return s.getLang, nil
}

func (s *cvhTranslationPreferenceService) ClearSessionLanguage(_ context.Context, sessionID, viewer string) error {
	s.clearID = sessionID
	s.clearViewer = viewer
	return s.err
}

// cvhPrincipal 模拟认证中间件注入的主体上下文（principal_kind / session_id）。
type cvhPrincipal struct {
	kind      string
	sessionID string
}

func cvhTranslationPreferenceRouter(svc *cvhTranslationPreferenceService, principals ...cvhPrincipal) *gin.Engine {
	r := dxcRouter()
	if len(principals) > 0 {
		p := principals[0]
		r.Use(func(c *gin.Context) {
			c.Set("principal_kind", p.kind)
			if p.sessionID != "" {
				c.Set("session_id", p.sessionID)
			}
			c.Next()
		})
	}
	h := NewTranslationPreferenceHandler(svc)
	r.GET("/api/v1/translation/preferences/:session_id", h.GetPreference)
	r.PUT("/api/v1/translation/preferences/:session_id", h.PutPreference)
	r.DELETE("/api/v1/translation/preferences/:session_id", h.DeletePreference)
	return r
}

func TestCvhNewTranslationPreferenceHandler(t *testing.T) {
	svc := &cvhTranslationPreferenceService{}
	h := NewTranslationPreferenceHandler(svc)
	assert.NotNil(t, h)
	assert.Equal(t, svc, h.service)
}

func TestCvhTranslationPreferenceGet(t *testing.T) {
	path := "/api/v1/translation/preferences/conv-1"

	t.Run("set lang echoed", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{getLang: "en"}
		w := dxcDo(cvhTranslationPreferenceRouter(svc), http.MethodGet, path, "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "conv-1", svc.getSessionID)
		assert.Contains(t, w.Body.String(), `"target_lang":"en"`)
		assert.Contains(t, w.Body.String(), `"session_id":"conv-1"`)
	})

	t.Run("unset lang is empty string not 404", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{}
		w := dxcDo(cvhTranslationPreferenceRouter(svc), http.MethodGet, path, "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"target_lang":""`)
	})

	t.Run("agent principal reads agent viewer role", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{}
		w := dxcDo(cvhTranslationPreferenceRouter(svc, cvhPrincipal{kind: "agent"}), http.MethodGet, path, "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, translationdelivery.ViewerRoleAgent, svc.getViewer)
	})

	t.Run("bound visitor reads visitor viewer role", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{getLang: "ja"}
		router := cvhTranslationPreferenceRouter(svc, cvhPrincipal{kind: "end_user", sessionID: "conv-1"})
		w := dxcDo(router, http.MethodGet, path, "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, translationdelivery.ViewerRoleVisitor, svc.getViewer)
		assert.Contains(t, w.Body.String(), `"target_lang":"ja"`)
	})

	t.Run("cross session visitor is forbidden before service call", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{}
		router := cvhTranslationPreferenceRouter(svc, cvhPrincipal{kind: "end_user", sessionID: "conv-other"})
		w := dxcDo(router, http.MethodGet, path, "")
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Empty(t, svc.getSessionID, "service must not be reached on session mismatch")
	})

	t.Run("visitor without session claim is forbidden", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{}
		router := cvhTranslationPreferenceRouter(svc, cvhPrincipal{kind: "end_user"})
		w := dxcDo(router, http.MethodGet, path, "")
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Empty(t, svc.getSessionID)
	})

	t.Run("service errors map to status codes", func(t *testing.T) {
		cases := []struct {
			name string
			err  error
			want int
		}{
			{"unavailable 503", translationdelivery.ErrTranslationUnavailable, http.StatusServiceUnavailable},
			{"session required 400", translationdelivery.ErrTranslationSessionRequired, http.StatusBadRequest},
			{"viewer invalid 400", translationdelivery.ErrTranslationViewerInvalid, http.StatusBadRequest},
			{"conflict 409", translationdelivery.ErrTranslationPrefConflict, http.StatusConflict},
			{"unknown 500", errors.New("db exploded"), http.StatusInternalServerError},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := &cvhTranslationPreferenceService{err: tc.err}
				w := dxcDo(cvhTranslationPreferenceRouter(svc), http.MethodGet, path, "")
				assert.Equal(t, tc.want, w.Code)
				assert.Contains(t, w.Body.String(), `"success":false`)
			})
		}
	})
}

func TestCvhTranslationPreferencePut(t *testing.T) {
	path := "/api/v1/translation/preferences/conv-9"

	t.Run("success upserts normalized lang", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{setResult: "zh-cn"}
		w := dxcDo(cvhTranslationPreferenceRouter(svc), http.MethodPut, path, `{"target_lang":"ZH-CN"}`)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "conv-9", svc.setSessionID)
		assert.Equal(t, "ZH-CN", svc.setLang, "原始输入透传，规范化在服务层")
		assert.Contains(t, w.Body.String(), `"target_lang":"zh-cn"`)
	})

	t.Run("bound visitor upserts visitor role", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{setResult: "ja"}
		router := cvhTranslationPreferenceRouter(svc, cvhPrincipal{kind: "end_user", sessionID: "conv-9"})
		w := dxcDo(router, http.MethodPut, path, `{"target_lang":"ja"}`)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, translationdelivery.ViewerRoleVisitor, svc.setViewer)
	})

	t.Run("cross session visitor cannot upsert", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{}
		router := cvhTranslationPreferenceRouter(svc, cvhPrincipal{kind: "end_user", sessionID: "conv-other"})
		w := dxcDo(router, http.MethodPut, path, `{"target_lang":"ja"}`)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Empty(t, svc.setSessionID)
	})

	t.Run("invalid json rejected", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{}
		w := dxcDo(cvhTranslationPreferenceRouter(svc), http.MethodPut, path, `{`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Empty(t, svc.setSessionID)
	})

	t.Run("missing field rejected", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{}
		w := dxcDo(cvhTranslationPreferenceRouter(svc), http.MethodPut, path, `{}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Empty(t, svc.setSessionID)
	})

	t.Run("put error mapping", func(t *testing.T) {
		cases := []struct {
			name string
			err  error
			want int
		}{
			{"lang invalid 400", translationdelivery.ErrTranslationLangInvalid, http.StatusBadRequest},
			{"target required 400", translationdelivery.ErrTranslationTargetRequired, http.StatusBadRequest},
			{"viewer invalid 400", translationdelivery.ErrTranslationViewerInvalid, http.StatusBadRequest},
			{"conflict 409", translationdelivery.ErrTranslationPrefConflict, http.StatusConflict},
			{"unavailable 503", translationdelivery.ErrTranslationUnavailable, http.StatusServiceUnavailable},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := &cvhTranslationPreferenceService{err: tc.err}
				w := dxcDo(cvhTranslationPreferenceRouter(svc), http.MethodPut, path, `{"target_lang":"en"}`)
				assert.Equal(t, tc.want, w.Code)
			})
		}
	})
}

func TestCvhTranslationPreferenceDelete(t *testing.T) {
	path := "/api/v1/translation/preferences/conv-2"

	t.Run("success idempotent", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{}
		w := dxcDo(cvhTranslationPreferenceRouter(svc), http.MethodDelete, path, "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "conv-2", svc.clearID)
	})

	t.Run("bound visitor deletes own visitor role", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{}
		router := cvhTranslationPreferenceRouter(svc, cvhPrincipal{kind: "end_user", sessionID: "conv-2"})
		w := dxcDo(router, http.MethodDelete, path, "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, translationdelivery.ViewerRoleVisitor, svc.clearViewer)
	})

	t.Run("cross session visitor cannot delete", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{}
		router := cvhTranslationPreferenceRouter(svc, cvhPrincipal{kind: "end_user", sessionID: "conv-other"})
		w := dxcDo(router, http.MethodDelete, path, "")
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Empty(t, svc.clearID)
	})

	t.Run("error mapped", func(t *testing.T) {
		svc := &cvhTranslationPreferenceService{err: translationdelivery.ErrTranslationSessionRequired}
		w := dxcDo(cvhTranslationPreferenceRouter(svc), http.MethodDelete, path, "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestCvhTranslationPreferenceRespondErrorNilGuard respondError 的 nil 哨兵：
// 防御性入口（正常链路已在调用方判空），nil 错误不写任何响应。
func TestCvhTranslationPreferenceRespondErrorNilGuard(t *testing.T) {
	h := NewTranslationPreferenceHandler(&cvhTranslationPreferenceService{})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	h.respondError(c, nil)
	assert.Equal(t, http.StatusOK, w.Code, "nil error must not write an error response")
	assert.Empty(t, w.Body.String())
}
