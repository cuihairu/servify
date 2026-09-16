package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	suggestioncontract "servify/apps/server/internal/modules/suggestion/contract"

	"github.com/gin-gonic/gin"
)

// suggestionPublicRecorder 客户侧推荐问题路由的测试替身：记录请求并
// 返回可断言的固定响应。
type suggestionPublicRecorder struct {
	initialReq *suggestioncontract.InitialQuestionsRequest
	nextReq    *suggestioncontract.NextQuestionsRequest
	initialErr error
	nextErr    error
}

func (s *suggestionPublicRecorder) Suggest(ctx context.Context, req *suggestioncontract.SuggestionRequest) (*suggestioncontract.SuggestionResponse, error) {
	return &suggestioncontract.SuggestionResponse{}, nil
}

func (s *suggestionPublicRecorder) InitialQuestions(ctx context.Context, req *suggestioncontract.InitialQuestionsRequest) (*suggestioncontract.InitialQuestionsResponse, error) {
	s.initialReq = req
	if s.initialErr != nil {
		return nil, s.initialErr
	}
	return &suggestioncontract.InitialQuestionsResponse{
		Questions: []suggestioncontract.RecommendedQuestion{{Question: "如何重置密码", Source: "knowledge_doc"}},
	}, nil
}

func (s *suggestionPublicRecorder) NextQuestions(ctx context.Context, req *suggestioncontract.NextQuestionsRequest) (*suggestioncontract.NextQuestionsResponse, error) {
	s.nextReq = req
	if s.nextErr != nil {
		return nil, s.nextErr
	}
	return &suggestioncontract.NextQuestionsResponse{
		Query:     req.Query,
		Questions: []suggestioncontract.RecommendedQuestion{{Question: "如何重置密码", Source: "knowledge_doc"}},
	}, nil
}

func newSuggestionPublicRouter(rec *suggestionPublicRecorder) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterPublicSuggestionRoutes(r.Group("/public"), NewSuggestionHandler(rec))
	return r
}

func TestPublicSuggestionInitialQuestions(t *testing.T) {
	rec := &suggestionPublicRecorder{}
	r := newSuggestionPublicRouter(rec)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public/suggestions/initial?limit=3", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"success":true`) || !strings.Contains(w.Body.String(), "如何重置密码") {
		t.Fatalf("body = %s", w.Body.String())
	}
	if rec.initialReq == nil || rec.initialReq.Limit != 3 {
		t.Fatalf("initialReq = %+v", rec.initialReq)
	}
}

func TestPublicSuggestionInitialQuestionsServiceError(t *testing.T) {
	rec := &suggestionPublicRecorder{initialErr: context.DeadlineExceeded}
	r := newSuggestionPublicRouter(rec)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public/suggestions/initial", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestPublicSuggestionNextQuestions(t *testing.T) {
	t.Run("get with query", func(t *testing.T) {
		rec := &suggestionPublicRecorder{}
		r := newSuggestionPublicRouter(rec)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/public/suggestions/next?query=%E5%AF%86%E7%A0%81&session_id=s-1&limit=5", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"success":true`) {
			t.Fatalf("body = %s", w.Body.String())
		}
		if rec.nextReq == nil || rec.nextReq.Query != "密码" || rec.nextReq.SessionID != "s-1" || rec.nextReq.Limit != 5 {
			t.Fatalf("nextReq = %+v", rec.nextReq)
		}
	})

	t.Run("get missing query rejected", func(t *testing.T) {
		rec := &suggestionPublicRecorder{}
		r := newSuggestionPublicRouter(rec)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/public/suggestions/next", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		if rec.nextReq != nil {
			t.Fatalf("service must not be called, got %+v", rec.nextReq)
		}
	})

	t.Run("post with body", func(t *testing.T) {
		rec := &suggestionPublicRecorder{}
		r := newSuggestionPublicRouter(rec)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/public/suggestions/next", strings.NewReader(`{"query":"密码","limit":4}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		if rec.nextReq == nil || rec.nextReq.Query != "密码" || rec.nextReq.Limit != 4 {
			t.Fatalf("nextReq = %+v", rec.nextReq)
		}
	})

	t.Run("post invalid json rejected", func(t *testing.T) {
		rec := &suggestionPublicRecorder{}
		r := newSuggestionPublicRouter(rec)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/public/suggestions/next", strings.NewReader(`not-json`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
	})

	t.Run("post blank query rejected", func(t *testing.T) {
		rec := &suggestionPublicRecorder{}
		r := newSuggestionPublicRouter(rec)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/public/suggestions/next", strings.NewReader(`{"query":"   "}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
	})

	t.Run("service error maps to 500", func(t *testing.T) {
		rec := &suggestionPublicRecorder{nextErr: context.DeadlineExceeded}
		r := newSuggestionPublicRouter(rec)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/public/suggestions/next?query=x", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
	})
}
