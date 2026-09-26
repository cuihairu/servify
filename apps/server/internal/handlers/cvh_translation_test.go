package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"

	translationdelivery "servify/apps/server/internal/modules/translation/delivery"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

var _ translationdelivery.HandlerService = (*cvhTranslationService)(nil)

// cvhTranslationService 翻译服务 inline mock：捕获指令 + 注入结果/错误。
type cvhTranslationService struct {
	cmd    translationdelivery.TranslateCommand
	result translationdelivery.TranslateResult
	err    error
}

func (s *cvhTranslationService) Translate(_ context.Context, cmd translationdelivery.TranslateCommand) (translationdelivery.TranslateResult, error) {
	s.cmd = cmd
	return s.result, s.err
}

func (s *cvhTranslationService) BatchTranslate(_ context.Context, cmd translationdelivery.BatchTranslateCommand) (translationdelivery.BatchTranslateResult, error) {
	return translationdelivery.BatchTranslateResult{Texts: []string{}, TargetLang: cmd.TargetLang}, s.err
}

func cvhTranslationRouter(svc *cvhTranslationService) *gin.Engine {
	r := dxcRouter()
	r.POST("/api/v1/translation/translate", NewTranslationHandler(svc).Translate)
	return r
}

func TestCvhNewTranslationHandler(t *testing.T) {
	svc := &cvhTranslationService{}
	h := NewTranslationHandler(svc)
	assert.NotNil(t, h)
	assert.Equal(t, svc, h.service)
}

func TestCvhTranslationTranslate(t *testing.T) {
	path := "/api/v1/translation/translate"

	t.Run("success echoes translated text", func(t *testing.T) {
		svc := &cvhTranslationService{result: translationdelivery.TranslateResult{
			Text: "When will my order ship?", SourceLang: "auto", TargetLang: "en",
		}}
		body := `{"text":"请问订单什么时候发货？","target_lang":"en"}`
		w := dxcDo(cvhTranslationRouter(svc), http.MethodPost, path, body)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "请问订单什么时候发货？", svc.cmd.Text)
		assert.Equal(t, "en", svc.cmd.TargetLang)
		assert.Empty(t, svc.cmd.SourceLang, "source_lang 缺省不传")
		assert.Contains(t, w.Body.String(), `"text":"When will my order ship?"`)
		assert.Contains(t, w.Body.String(), `"success":true`)
	})

	t.Run("forwards source lang", func(t *testing.T) {
		svc := &cvhTranslationService{}
		w := dxcDo(cvhTranslationRouter(svc), http.MethodPost, path, `{"text":"hi","target_lang":"en","source_lang":"zh-CN"}`)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "zh-CN", svc.cmd.SourceLang)
	})

	t.Run("invalid json", func(t *testing.T) {
		w := dxcDo(cvhTranslationRouter(&cvhTranslationService{}), http.MethodPost, path, `{`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid request format")
	})

	t.Run("missing required field", func(t *testing.T) {
		w := dxcDo(cvhTranslationRouter(&cvhTranslationService{}), http.MethodPost, path, `{"target_lang":"en"}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	errorCases := []struct {
		name string
		err  error
		want int
	}{
		{"text required", translationdelivery.ErrTranslationTextRequired, http.StatusBadRequest},
		{"text too long", translationdelivery.ErrTranslationTextTooLong, http.StatusBadRequest},
		{"target required", translationdelivery.ErrTranslationTargetRequired, http.StatusBadRequest},
		{"lang invalid", translationdelivery.ErrTranslationLangInvalid, http.StatusBadRequest},
		{"empty output", translationdelivery.ErrTranslationEmptyOutput, http.StatusBadRequest},
		{"unavailable", translationdelivery.ErrTranslationUnavailable, http.StatusServiceUnavailable},
		{"deadline", context.DeadlineExceeded, http.StatusGatewayTimeout},
		{"upstream failure", errors.New("upstream boom"), http.StatusBadGateway},
	}
	for _, tc := range errorCases {
		t.Run("error "+tc.name, func(t *testing.T) {
			svc := &cvhTranslationService{err: tc.err}
			w := dxcDo(cvhTranslationRouter(svc), http.MethodPost, path, `{"text":"hi","target_lang":"en"}`)
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), `"success":false`)
		})
	}
}
