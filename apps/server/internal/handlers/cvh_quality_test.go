package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"servify/apps/server/internal/models"
	qualitydelivery "servify/apps/server/internal/modules/quality/delivery"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

var _ qualitydelivery.HandlerService = (*cvhQualityService)(nil)

// cvhQualityService 质检服务 inline mock。
type cvhQualityService struct {
	reviews     []models.QualityReview
	total       int64
	listErr     error
	review      *models.QualityReview
	getErr      error
	confirmErr  error
	rescoreErr  error
	scorerOn    bool

	gotQuery          qualitydelivery.ReviewListQuery
	gotGetSession     string
	gotConfirmSession string
	gotConfirmCmd     qualitydelivery.ConfirmCommand
	gotRescoreSession string
	gotRescoreForce   bool
}

func (s *cvhQualityService) ListReviews(_ context.Context, query qualitydelivery.ReviewListQuery) ([]models.QualityReview, int64, error) {
	s.gotQuery = query
	return s.reviews, s.total, s.listErr
}

func (s *cvhQualityService) GetReview(_ context.Context, sessionID string) (*models.QualityReview, error) {
	s.gotGetSession = sessionID
	return s.review, s.getErr
}

func (s *cvhQualityService) ConfirmReview(_ context.Context, sessionID string, cmd qualitydelivery.ConfirmCommand) error {
	s.gotConfirmSession = sessionID
	s.gotConfirmCmd = cmd
	return s.confirmErr
}

func (s *cvhQualityService) RescoreReview(_ context.Context, sessionID string, force bool) error {
	s.gotRescoreSession = sessionID
	s.gotRescoreForce = force
	return s.rescoreErr
}

func (s *cvhQualityService) ScorerEnabled() bool { return s.scorerOn }

func cvhQualityRouter(svc *cvhQualityService, middleware ...gin.HandlerFunc) *gin.Engine {
	r := dxcRouter()
	for _, mw := range middleware {
		r.Use(mw)
	}
	RegisterQualityRoutes(r.Group("/api"), NewQualityHandler(svc))
	return r
}

func TestCvhNewQualityHandler(t *testing.T) {
	svc := &cvhQualityService{}
	h := NewQualityHandler(svc)
	assert.NotNil(t, h)
	assert.Equal(t, svc, h.service)
}

func TestCvhRegisterQualityRoutes(t *testing.T) {
	r := cvhQualityRouter(&cvhQualityService{})
	routes := r.Routes()
	assert.Len(t, routes, 5)
	want := map[string]bool{
		"GET /api/quality/reviews":                   false,
		"GET /api/quality/reviews/:sessionId":        false,
		"POST /api/quality/reviews/:sessionId/confirm": false,
		"POST /api/quality/reviews/:sessionId/rescore": false,
		"GET /api/quality/scorer":                    false,
	}
	for _, rt := range routes {
		key := rt.Method + " " + rt.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, seen := range want {
		assert.True(t, seen, "route %s not registered", key)
	}
}

func TestCvhQualityListReviews(t *testing.T) {
	t.Run("all filters parsed", func(t *testing.T) {
		svc := &cvhQualityService{
			reviews: []models.QualityReview{{ID: 1, SessionID: "sess-1"}, {ID: 2, SessionID: "sess-2"}},
			total:   2,
		}
		q := "/api/quality/reviews?status=pending&severity=high&agent_id=9&customer_id=12&has_violations=true" +
			"&min_score=1.5&max_score=9.5&from=2026-01-01T00:00:00Z&to=2026-02-01T00:00:00Z&page=2&page_size=50"
		w := dxcDo(cvhQualityRouter(svc), http.MethodGet, q, "")
		assert.Equal(t, http.StatusOK, w.Code)

		got := svc.gotQuery
		assert.Equal(t, "pending", got.Status)
		assert.Equal(t, "high", got.Severity)
		assert.NotNil(t, got.AgentID)
		assert.Equal(t, uint(9), *got.AgentID)
		assert.NotNil(t, got.CustomerID)
		assert.Equal(t, uint(12), *got.CustomerID)
		assert.NotNil(t, got.HasViolations)
		assert.True(t, *got.HasViolations)
		assert.NotNil(t, got.MinScore)
		assert.Equal(t, 1.5, *got.MinScore)
		assert.NotNil(t, got.MaxScore)
		assert.Equal(t, 9.5, *got.MaxScore)
		assert.NotNil(t, got.From)
		assert.NotNil(t, got.To)
		assert.Equal(t, 2, got.Page)
		assert.Equal(t, 50, got.PageSize)

		var resp struct {
			Total    int64 `json:"total"`
			Page     int   `json:"page"`
			PageSize int   `json:"page_size"`
		}
		dxcDecode(t, w, &resp)
		assert.Equal(t, int64(2), resp.Total)
		assert.Equal(t, 2, resp.Page)
		assert.Equal(t, 50, resp.PageSize)
	})

	t.Run("garbage params fall back to defaults", func(t *testing.T) {
		svc := &cvhQualityService{}
		q := "/api/quality/reviews?agent_id=abc&customer_id=xyz&has_violations=zz&min_score=q&max_score=w&from=bad&to=bad&page=x&page_size=y"
		w := dxcDo(cvhQualityRouter(svc), http.MethodGet, q, "")
		assert.Equal(t, http.StatusOK, w.Code)
		got := svc.gotQuery
		assert.Nil(t, got.AgentID)
		assert.Nil(t, got.CustomerID)
		assert.Nil(t, got.HasViolations)
		assert.Nil(t, got.MinScore)
		assert.Nil(t, got.MaxScore)
		assert.Nil(t, got.From)
		assert.Nil(t, got.To)
		assert.Equal(t, 0, got.Page)
		assert.Equal(t, 0, got.PageSize)
	})

	t.Run("service error", func(t *testing.T) {
		svc := &cvhQualityService{listErr: errors.New("boom")}
		w := dxcDo(cvhQualityRouter(svc), http.MethodGet, "/api/quality/reviews", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to list quality reviews")
	})
}

func TestCvhQualityGetReview(t *testing.T) {
	cases := []struct {
		name string
		svc  *cvhQualityService
		want int
	}{
		{"success", &cvhQualityService{review: &models.QualityReview{ID: 1, SessionID: "sess-1", Status: "scored"}}, http.StatusOK},
		{"not found", &cvhQualityService{getErr: qualitydelivery.ErrNotFound}, http.StatusNotFound},
		{"generic error", &cvhQualityService{getErr: errors.New("boom")}, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := dxcDo(cvhQualityRouter(tc.svc), http.MethodGet, "/api/quality/reviews/sess-1", "")
			assert.Equal(t, tc.want, w.Code)
			assert.Equal(t, "sess-1", tc.svc.gotGetSession)
			if tc.want == http.StatusOK {
				var got models.QualityReview
				dxcDecode(t, w, &got)
				assert.Equal(t, "sess-1", got.SessionID)
			} else {
				assert.Contains(t, w.Body.String(), "Failed to get quality review")
			}
		})
	}
}

func TestCvhQualityConfirmReview(t *testing.T) {
	path := "/api/quality/reviews/sess-1/confirm"
	asAgent := func(c *gin.Context) { c.Set("user_id", uint(7)); c.Next() }

	t.Run("success with reviewer", func(t *testing.T) {
		svc := &cvhQualityService{}
		body := `{"manual_score":8.5,"manual_result":"pass","review_note":"抽检通过"}`
		w := dxcDo(cvhQualityRouter(svc, asAgent), http.MethodPost, path, body)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "sess-1", svc.gotConfirmSession)
		assert.NotNil(t, svc.gotConfirmCmd.ManualScore)
		assert.Equal(t, 8.5, *svc.gotConfirmCmd.ManualScore)
		assert.Equal(t, "pass", svc.gotConfirmCmd.ManualResult)
		assert.Equal(t, "抽检通过", svc.gotConfirmCmd.ReviewNote)
		assert.Equal(t, uint(7), svc.gotConfirmCmd.ReviewedBy)
		var resp SuccessResponse
		dxcDecode(t, w, &resp)
		assert.Equal(t, "confirmed", resp.Message)
	})

	t.Run("success without user keeps reviewedBy zero", func(t *testing.T) {
		svc := &cvhQualityService{}
		w := dxcDo(cvhQualityRouter(svc), http.MethodPost, path, `{}`)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Zero(t, svc.gotConfirmCmd.ReviewedBy)
	})

	t.Run("invalid json", func(t *testing.T) {
		w := dxcDo(cvhQualityRouter(&cvhQualityService{}, asAgent), http.MethodPost, path, `{`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid request")
	})

	errorCases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", qualitydelivery.ErrNotFound, http.StatusNotFound},
		{"not scoreable", qualitydelivery.ErrNotScoreable, http.StatusConflict},
		{"generic", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range errorCases {
		t.Run("error "+tc.name, func(t *testing.T) {
			svc := &cvhQualityService{confirmErr: tc.err}
			w := dxcDo(cvhQualityRouter(svc, asAgent), http.MethodPost, path, `{}`)
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), "Failed to confirm review")
		})
	}
}

func TestCvhQualityRescoreReview(t *testing.T) {
	t.Run("success defaults to no force", func(t *testing.T) {
		svc := &cvhQualityService{}
		w := dxcDo(cvhQualityRouter(svc), http.MethodPost, "/api/quality/reviews/sess-1/rescore", "")
		assert.Equal(t, http.StatusAccepted, w.Code)
		assert.Equal(t, "sess-1", svc.gotRescoreSession)
		assert.False(t, svc.gotRescoreForce)
		var resp SuccessResponse
		dxcDecode(t, w, &resp)
		assert.Equal(t, "rescore scheduled", resp.Message)
	})

	t.Run("force=true", func(t *testing.T) {
		svc := &cvhQualityService{}
		w := dxcDo(cvhQualityRouter(svc), http.MethodPost, "/api/quality/reviews/sess-1/rescore?force=true", "")
		assert.Equal(t, http.StatusAccepted, w.Code)
		assert.True(t, svc.gotRescoreForce)
	})

	errorCases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", qualitydelivery.ErrNotFound, http.StatusNotFound},
		{"needs force", qualitydelivery.ErrConfirmedNeedsForce, http.StatusConflict},
		{"generic", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range errorCases {
		t.Run("error "+tc.name, func(t *testing.T) {
			svc := &cvhQualityService{rescoreErr: tc.err}
			w := dxcDo(cvhQualityRouter(svc), http.MethodPost, "/api/quality/reviews/sess-1/rescore", "")
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), "Failed to rescore review")
		})
	}
}

func TestCvhQualityGetScorerStatus(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		w := dxcDo(cvhQualityRouter(&cvhQualityService{scorerOn: true}), http.MethodGet, "/api/quality/scorer", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"llm_enabled":true`)
	})

	t.Run("disabled means rules-only", func(t *testing.T) {
		w := dxcDo(cvhQualityRouter(&cvhQualityService{}), http.MethodGet, "/api/quality/scorer", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"llm_enabled":false`)
	})
}
