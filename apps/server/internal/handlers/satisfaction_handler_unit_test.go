package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	satisfactiondelivery "servify/apps/server/internal/modules/satisfaction/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func newSatisfactionUnitRouter(svc *unitSatisfactionService) (*gin.Engine, *SatisfactionHandler, *CSATSurveyHandler) {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	h := NewSatisfactionHandler(svc, logger)
	csat := NewCSATSurveyHandler(svc)
	r := gin.New()
	RegisterSatisfactionRoutes(&r.RouterGroup, h)
	RegisterCSATSurveyRoutes(&r.RouterGroup, csat)
	return r, h, csat
}

func satisfactionUnitRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSatisfactionHandlerUnitCreate(t *testing.T) {
	body := `{"ticket_id":1,"customer_id":2,"rating":5}`
	sat := &models.CustomerSatisfaction{ID: 1}
	cases := []struct {
		name string
		svc  *unitSatisfactionService
		body string
		want int
	}{
		{"success", &unitSatisfactionService{sat: sat}, body, http.StatusCreated},
		{"invalid body", &unitSatisfactionService{}, `{`, http.StatusBadRequest},
		{"conflict", &unitSatisfactionService{createErr: errors.New("satisfaction already exists")}, body, http.StatusConflict},
		{"not owner", &unitSatisfactionService{createErr: errors.New("user is not the owner")}, body, http.StatusForbidden},
		{"internal", &unitSatisfactionService{createErr: errors.New("boom")}, body, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := newSatisfactionUnitRouter(tc.svc)
			w := satisfactionUnitRequest(r, http.MethodPost, "/satisfactions", tc.body)
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestSatisfactionHandlerUnitGet(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitSatisfactionService
		path string
		want int
	}{
		{"success", &unitSatisfactionService{sat: &models.CustomerSatisfaction{ID: 2}}, "/satisfactions/2", http.StatusOK},
		{"bad id", &unitSatisfactionService{}, "/satisfactions/x", http.StatusBadRequest},
		{"not found", &unitSatisfactionService{getErr: errors.New("satisfaction not found")}, "/satisfactions/2", http.StatusNotFound},
		{"internal", &unitSatisfactionService{getErr: errors.New("boom")}, "/satisfactions/2", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := newSatisfactionUnitRouter(tc.svc)
			w := satisfactionUnitRequest(r, http.MethodGet, tc.path, "")
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestSatisfactionHandlerUnitList(t *testing.T) {
	t.Run("success without dates", func(t *testing.T) {
		svc := &unitSatisfactionService{sats: []models.CustomerSatisfaction{{ID: 1}}, total: 1}
		r, _, _ := newSatisfactionUnitRouter(svc)
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	// gin binds *time.Time query params as RFC3339; values in that format reach the
	// handler's manual "2006-01-02" parse and are rejected with 400.
	t.Run("rfc3339 date_from rejected", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions?date_from=2026-01-01T00:00:00Z", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("rfc3339 date_to rejected", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions?date_to=2026-01-01T00:00:00Z", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("bad date_from", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions?date_from=zzz", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("bad date_to", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions?date_to=zzz", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("bad query", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions?page=zz", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("error", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{listErr: errors.New("boom")})
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestSatisfactionHandlerUnitSurveys(t *testing.T) {
	t.Run("success with pages", func(t *testing.T) {
		svc := &unitSatisfactionService{surveys: []models.SatisfactionSurvey{{ID: 1}, {ID: 2}, {ID: 3}}, total: 3}
		r, _, _ := newSatisfactionUnitRouter(svc)
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions/surveys?page_size=2", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("bad query", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions/surveys?page=zz", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("error", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{surveyErr: errors.New("boom")})
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions/surveys", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestSatisfactionHandlerUnitResend(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitSatisfactionService
		want int
	}{
		{"success", &unitSatisfactionService{survey: &models.SatisfactionSurvey{ID: 5}}, http.StatusOK},
		{"not found", &unitSatisfactionService{resendErr: satisfactiondelivery.ErrSurveyNotFound}, http.StatusNotFound},
		{"completed", &unitSatisfactionService{resendErr: satisfactiondelivery.ErrSurveyCompleted}, http.StatusBadRequest},
		{"internal", &unitSatisfactionService{resendErr: errors.New("boom")}, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := newSatisfactionUnitRouter(tc.svc)
			w := satisfactionUnitRequest(r, http.MethodPost, "/satisfactions/surveys/5/resend", "")
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	t.Run("bad id", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodPost, "/satisfactions/surveys/zz/resend", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestSatisfactionHandlerUnitByTicket(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitSatisfactionService
		want int
	}{
		{"success", &unitSatisfactionService{sat: &models.CustomerSatisfaction{ID: 1}}, http.StatusOK},
		{"no content", &unitSatisfactionService{}, http.StatusNoContent},
		{"error", &unitSatisfactionService{byTicketErr: errors.New("boom")}, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := newSatisfactionUnitRouter(tc.svc)
			w := satisfactionUnitRequest(r, http.MethodGet, "/tickets/7/satisfaction", "")
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	t.Run("bad ticket id", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodGet, "/tickets/zz/satisfaction", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestSatisfactionHandlerUnitStats(t *testing.T) {
	t.Run("success with dates", func(t *testing.T) {
		svc := &unitSatisfactionService{stats: &satisfactiondelivery.SatisfactionStatsResponse{TotalRatings: 10}}
		r, _, _ := newSatisfactionUnitRouter(svc)
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions/stats?date_from=2026-01-01&date_to=2026-03-01", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("bad date_from", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions/stats?date_from=zz", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("bad date_to", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions/stats?date_to=zz", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("error", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{statsErr: errors.New("boom")})
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions/stats", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestSatisfactionHandlerUnitUpdateDelete(t *testing.T) {
	t.Run("update success", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{sat: &models.CustomerSatisfaction{ID: 3}})
		w := satisfactionUnitRequest(r, http.MethodPut, "/satisfactions/3", `{"comment":"c"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("update bad id", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodPut, "/satisfactions/zz", `{"comment":"c"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("update invalid body", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodPut, "/satisfactions/3", `{`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("update not found", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{updateErr: errors.New("satisfaction not found")})
		w := satisfactionUnitRequest(r, http.MethodPut, "/satisfactions/3", `{"comment":"c"}`)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("update internal", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{updateErr: errors.New("boom")})
		w := satisfactionUnitRequest(r, http.MethodPut, "/satisfactions/3", `{"comment":"c"}`)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("delete success", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodDelete, "/satisfactions/3", "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("delete bad id", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
		w := satisfactionUnitRequest(r, http.MethodDelete, "/satisfactions/zz", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("delete not found", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{deleteErr: errors.New("satisfaction not found")})
		w := satisfactionUnitRequest(r, http.MethodDelete, "/satisfactions/3", "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("delete internal", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{deleteErr: errors.New("boom")})
		w := satisfactionUnitRequest(r, http.MethodDelete, "/satisfactions/3", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestCSATSurveyHandlerUnit(t *testing.T) {
	t.Run("get survey success", func(t *testing.T) {
		svc := &unitSatisfactionService{preview: &satisfactiondelivery.SatisfactionSurveyPreview{TicketID: 1}}
		r, _, _ := newSatisfactionUnitRouter(svc)
		w := satisfactionUnitRequest(r, http.MethodGet, "/csat/token-1", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("get survey not found", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{previewErr: satisfactiondelivery.ErrSurveyNotFound})
		w := satisfactionUnitRequest(r, http.MethodGet, "/csat/token-1", "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("get survey internal", func(t *testing.T) {
		r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{previewErr: errors.New("boom")})
		w := satisfactionUnitRequest(r, http.MethodGet, "/csat/token-1", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})

	respondCases := []struct {
		name string
		svc  *unitSatisfactionService
		body string
		want int
	}{
		{"success", &unitSatisfactionService{sat: &models.CustomerSatisfaction{ID: 1}}, `{"rating":5,"comment":"great"}`, http.StatusOK},
		{"invalid body", &unitSatisfactionService{}, `{`, http.StatusBadRequest},
		{"rating out of range", &unitSatisfactionService{}, `{"rating":9}`, http.StatusBadRequest},
		{"rating missing", &unitSatisfactionService{}, `{"comment":"x"}`, http.StatusBadRequest},
		{"not found", &unitSatisfactionService{respondErr: satisfactiondelivery.ErrSurveyNotFound}, `{"rating":4}`, http.StatusNotFound},
		{"expired", &unitSatisfactionService{respondErr: satisfactiondelivery.ErrSurveyExpired}, `{"rating":4}`, http.StatusGone},
		{"completed", &unitSatisfactionService{respondErr: satisfactiondelivery.ErrSurveyCompleted}, `{"rating":4}`, http.StatusConflict},
		{"internal", &unitSatisfactionService{respondErr: errors.New("boom")}, `{"rating":4}`, http.StatusInternalServerError},
	}
	for _, tc := range respondCases {
		t.Run("respond "+tc.name, func(t *testing.T) {
			r, _, _ := newSatisfactionUnitRouter(tc.svc)
			w := satisfactionUnitRequest(r, http.MethodPost, "/csat/token-1/respond", tc.body)
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}

	t.Run("register guards", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		RegisterCSATSurveyRoutes(nil, NewCSATSurveyHandler(&unitSatisfactionService{}))
		r := gin.New()
		RegisterCSATSurveyRoutes(&r.RouterGroup, nil)
	})
}

var _ = time.Now
