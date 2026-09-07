package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/services"

	"github.com/gin-gonic/gin"
)

func newSLAUnitRouter(svc *unitSLAService, ticket *unitTicketService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	var ticketReader SLATicketReader
	if ticket != nil {
		ticketReader = ticket
	}
	h := NewSLAHandler(svc, ticketReader)
	r := gin.New()
	RegisterSLARoutes(&r.RouterGroup, h)
	return r
}

func slaUnitRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSLAHandlerUnitCreateConfig(t *testing.T) {
	body := `{"name":"n","priority":"high","first_response_time":10,"resolution_time":60,"escalation_time":30}`
	config := &models.SLAConfig{ID: 1, Priority: "high"}
	cases := []struct {
		name string
		svc  *unitSLAService
		body string
		want int
	}{
		{"success", &unitSLAService{config: config}, body, http.StatusCreated},
		{"invalid body", &unitSLAService{}, `{`, http.StatusBadRequest},
		{"invalid priority", &unitSLAService{createErr: errors.New("invalid priority value")}, body, http.StatusBadRequest},
		{"invalid config", &unitSLAService{createErr: errors.New("resolution_time must be less than escalation_time")}, body, http.StatusBadRequest},
		{"must be at least", &unitSLAService{createErr: errors.New("first_response_time must be at least 1")}, body, http.StatusBadRequest},
		{"invalid input generic", &unitSLAService{createErr: errors.New("field invalid")}, body, http.StatusBadRequest},
		{"conflict", &unitSLAService{createErr: errors.New("config already exists")}, body, http.StatusConflict},
		{"internal", &unitSLAService{createErr: errors.New("boom")}, body, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newSLAUnitRouter(tc.svc, nil)
			w := slaUnitRequest(r, http.MethodPost, "/sla/configs", tc.body)
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestSLAHandlerUnitGetConfig(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitSLAService
		path string
		want int
	}{
		{"success", &unitSLAService{config: &models.SLAConfig{ID: 2}}, "/sla/configs/2", http.StatusOK},
		{"bad id", &unitSLAService{}, "/sla/configs/zz", http.StatusBadRequest},
		{"not found", &unitSLAService{getErr: errors.New("config not found")}, "/sla/configs/2", http.StatusNotFound},
		{"internal", &unitSLAService{getErr: errors.New("boom")}, "/sla/configs/2", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newSLAUnitRouter(tc.svc, nil)
			w := slaUnitRequest(r, http.MethodGet, tc.path, "")
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestSLAHandlerUnitListConfigs(t *testing.T) {
	t.Run("success defaults applied", func(t *testing.T) {
		svc := &unitSLAService{configs: []models.SLAConfig{{ID: 1}}, total: 1}
		r := newSLAUnitRouter(svc, nil)
		w := slaUnitRequest(r, http.MethodGet, "/sla/configs?page=0&page_size=500", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("valid paging", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{}, nil)
		w := slaUnitRequest(r, http.MethodGet, "/sla/configs?page=2&page_size=10", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("bad query", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{}, nil)
		w := slaUnitRequest(r, http.MethodGet, "/sla/configs?page=zz", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("error", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{listErr: errors.New("boom")}, nil)
		w := slaUnitRequest(r, http.MethodGet, "/sla/configs", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestSLAHandlerUnitUpdateConfig(t *testing.T) {
	body := `{"name":"n2"}`
	cases := []struct {
		name string
		svc  *unitSLAService
		path string
		body string
		want int
	}{
		{"success", &unitSLAService{config: &models.SLAConfig{ID: 3}}, "/sla/configs/3", body, http.StatusOK},
		{"bad id", &unitSLAService{}, "/sla/configs/zz", body, http.StatusBadRequest},
		{"invalid body", &unitSLAService{}, "/sla/configs/3", `{`, http.StatusBadRequest},
		{"not found", &unitSLAService{updateErr: errors.New("config not found")}, "/sla/configs/3", body, http.StatusNotFound},
		{"invalid priority", &unitSLAService{updateErr: errors.New("invalid priority")}, "/sla/configs/3", body, http.StatusBadRequest},
		{"must be less than", &unitSLAService{updateErr: errors.New("value must be less than max")}, "/sla/configs/3", body, http.StatusBadRequest},
		{"invalid generic", &unitSLAService{updateErr: errors.New("bad invalid input")}, "/sla/configs/3", body, http.StatusBadRequest},
		{"conflict", &unitSLAService{updateErr: errors.New("duplicate config")}, "/sla/configs/3", body, http.StatusConflict},
		{"internal", &unitSLAService{updateErr: errors.New("boom")}, "/sla/configs/3", body, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newSLAUnitRouter(tc.svc, nil)
			w := slaUnitRequest(r, http.MethodPut, tc.path, tc.body)
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestSLAHandlerUnitDeleteConfig(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitSLAService
		want int
	}{
		{"success", &unitSLAService{}, http.StatusOK},
		{"not found", &unitSLAService{deleteErr: errors.New("config not found")}, http.StatusNotFound},
		{"has violations", &unitSLAService{deleteErr: errors.New("cannot delete SLA config with violations")}, http.StatusConflict},
		{"internal", &unitSLAService{deleteErr: errors.New("boom")}, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newSLAUnitRouter(tc.svc, nil)
			w := slaUnitRequest(r, http.MethodDelete, "/sla/configs/4", "")
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	t.Run("bad id", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{}, nil)
		w := slaUnitRequest(r, http.MethodDelete, "/sla/configs/zz", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestSLAHandlerUnitByPriority(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{config: &models.SLAConfig{ID: 5}}, nil)
		w := slaUnitRequest(r, http.MethodGet, "/sla/configs/priority/high?customer_tier=vip", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("nil config", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{}, nil)
		w := slaUnitRequest(r, http.MethodGet, "/sla/configs/priority/high", "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("error", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{priorityErr: errors.New("boom")}, nil)
		w := slaUnitRequest(r, http.MethodGet, "/sla/configs/priority/high", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestSLAHandlerUnitViolations(t *testing.T) {
	t.Run("success defaults", func(t *testing.T) {
		svc := &unitSLAService{violations: []models.SLAViolation{{ID: 1}}, total: 1}
		r := newSLAUnitRouter(svc, nil)
		w := slaUnitRequest(r, http.MethodGet, "/sla/violations?page=0&page_size=0", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("bad query", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{}, nil)
		w := slaUnitRequest(r, http.MethodGet, "/sla/violations?page=zz", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("error", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{violationErr: errors.New("boom")}, nil)
		w := slaUnitRequest(r, http.MethodGet, "/sla/violations", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("resolve success", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{}, nil)
		w := slaUnitRequest(r, http.MethodPost, "/sla/violations/6/resolve", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("resolve bad id", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{}, nil)
		w := slaUnitRequest(r, http.MethodPost, "/sla/violations/zz/resolve", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("resolve not found", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{resolveErr: errors.New("violation not found")}, nil)
		w := slaUnitRequest(r, http.MethodPost, "/sla/violations/6/resolve", "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("resolve internal", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{resolveErr: errors.New("boom")}, nil)
		w := slaUnitRequest(r, http.MethodPost, "/sla/violations/6/resolve", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestSLAHandlerUnitStats(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{stats: &services.SLAStatsResponse{TotalConfigs: 2}}, nil)
		w := slaUnitRequest(r, http.MethodGet, "/sla/stats", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("error", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{statsErr: errors.New("boom")}, nil)
		w := slaUnitRequest(r, http.MethodGet, "/sla/stats", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestSLAHandlerUnitCheckTicket(t *testing.T) {
	t.Run("bad id", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{}, &unitTicketService{})
		w := slaUnitRequest(r, http.MethodPost, "/sla/check/ticket/zz", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("ticket generic error", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{}, &unitTicketService{getErr: errors.New("boom")})
		w := slaUnitRequest(r, http.MethodPost, "/sla/check/ticket/7", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("check error", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{checkErr: errors.New("boom")}, &unitTicketService{ticket: &models.Ticket{ID: 7}})
		w := slaUnitRequest(r, http.MethodPost, "/sla/check/ticket/7", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("violation found", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{violation: &models.SLAViolation{ID: 9}}, &unitTicketService{ticket: &models.Ticket{ID: 7}})
		w := slaUnitRequest(r, http.MethodPost, "/sla/check/ticket/7", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("no violation", func(t *testing.T) {
		r := newSLAUnitRouter(&unitSLAService{}, &unitTicketService{ticket: &models.Ticket{ID: 7}})
		w := slaUnitRequest(r, http.MethodPost, "/sla/check/ticket/7", "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d", w.Code)
		}
	})
}
