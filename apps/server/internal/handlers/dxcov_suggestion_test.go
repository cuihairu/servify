package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"

	suggestioncontract "servify/apps/server/internal/modules/suggestion/contract"

	"github.com/stretchr/testify/assert"
)

type dxcSuggestionRecorder struct {
	resp    *suggestioncontract.SuggestionResponse
	err     error
	lastReq *suggestioncontract.SuggestionRequest
}

func (s *dxcSuggestionRecorder) Suggest(ctx context.Context, req *suggestioncontract.SuggestionRequest) (*suggestioncontract.SuggestionResponse, error) {
	s.lastReq = req
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func TestDxcNewSuggestionHandler(t *testing.T) {
	svc := &dxcSuggestionRecorder{}
	h := NewSuggestionHandler(svc)
	assert.NotNil(t, h)
	assert.Equal(t, svc, h.service)
}

func TestDxcSuggestionHandlerSuggest(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		svc := &dxcSuggestionRecorder{resp: &suggestioncontract.SuggestionResponse{Query: "q"}}
		r := dxcRouter()
		r.GET("/assist/suggest", NewSuggestionHandler(svc).Suggest)
		w := dxcDo(r, http.MethodGet, "/assist/suggest?query=q", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"success":true`)
		assert.NotNil(t, svc.lastReq)
		assert.Equal(t, "q", svc.lastReq.Query)
		assert.Equal(t, 5, svc.lastReq.TicketLimit)
		assert.Equal(t, 5, svc.lastReq.KnowledgeDocLimit)
	})

	t.Run("explicit limits", func(t *testing.T) {
		svc := &dxcSuggestionRecorder{}
		r := dxcRouter()
		r.GET("/assist/suggest", NewSuggestionHandler(svc).Suggest)
		w := dxcDo(r, http.MethodGet, "/assist/suggest?query=q&limit=7&doc_limit=9", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 7, svc.lastReq.TicketLimit)
		assert.Equal(t, 9, svc.lastReq.KnowledgeDocLimit)
	})

	t.Run("invalid limits fall back", func(t *testing.T) {
		svc := &dxcSuggestionRecorder{}
		r := dxcRouter()
		r.GET("/assist/suggest", NewSuggestionHandler(svc).Suggest)
		w := dxcDo(r, http.MethodGet, "/assist/suggest?limit=abc&doc_limit=xyz", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 5, svc.lastReq.TicketLimit)
		assert.Equal(t, 5, svc.lastReq.KnowledgeDocLimit)
	})

	t.Run("service error", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/assist/suggest", NewSuggestionHandler(&dxcSuggestionRecorder{err: errors.New("boom")}).Suggest)
		w := dxcDo(r, http.MethodGet, "/assist/suggest", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to suggest")
	})
}

func TestDxcSuggestionHandlerSuggestPost(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &dxcSuggestionRecorder{resp: &suggestioncontract.SuggestionResponse{Query: "q"}}
		r := dxcRouter()
		r.POST("/assist/suggest", NewSuggestionHandler(svc).SuggestPost)
		w := dxcDo(r, http.MethodPost, "/assist/suggest", `{"query":"q","ticket_limit":3}`)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "q", svc.lastReq.Query)
		assert.Equal(t, 3, svc.lastReq.TicketLimit)
	})

	t.Run("invalid json", func(t *testing.T) {
		r := dxcRouter()
		r.POST("/assist/suggest", NewSuggestionHandler(&dxcSuggestionRecorder{}).SuggestPost)
		w := dxcDo(r, http.MethodPost, "/assist/suggest", `{`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing required query", func(t *testing.T) {
		r := dxcRouter()
		r.POST("/assist/suggest", NewSuggestionHandler(&dxcSuggestionRecorder{}).SuggestPost)
		w := dxcDo(r, http.MethodPost, "/assist/suggest", `{"ticket_limit":3}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error", func(t *testing.T) {
		r := dxcRouter()
		r.POST("/assist/suggest", NewSuggestionHandler(&dxcSuggestionRecorder{err: errors.New("boom")}).SuggestPost)
		w := dxcDo(r, http.MethodPost, "/assist/suggest", `{"query":"q"}`)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestDxcRegisterSuggestionRoutes(t *testing.T) {
	r := dxcRouter()
	RegisterSuggestionRoutes(&r.RouterGroup, NewSuggestionHandler(&dxcSuggestionRecorder{}))
	w := dxcDo(r, http.MethodGet, "/assist/suggest", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDxcParseIntDefault(t *testing.T) {
	assert.Equal(t, 5, parseIntDefault("", 5))
	assert.Equal(t, 7, parseIntDefault("7", 5))
	assert.Equal(t, 5, parseIntDefault("abc", 5))
}
