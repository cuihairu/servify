package handlers

import (
	"errors"
	"net/http"
	"testing"

	"servify/apps/server/internal/models"
	automationdelivery "servify/apps/server/internal/modules/automation/delivery"

	"github.com/stretchr/testify/assert"
)

func dxcAutomationService() *unitAutomationService {
	return &unitAutomationService{
		triggers: []models.AutomationTrigger{{ID: 1, Name: "t1"}},
		trigger:  &models.AutomationTrigger{ID: 1, Name: "t1"},
		runs:     []models.AutomationRun{{ID: 1}},
		runTotal: 1,
		batch:    &automationdelivery.BatchRunResponse{Event: "ticket.created", Matches: 1},
	}
}

func TestDxcNewAutomationHandler(t *testing.T) {
	svc := dxcAutomationService()
	h := NewAutomationHandler(svc)
	assert.NotNil(t, h)
	assert.Equal(t, svc, h.service)
}

func TestDxcAutomationHandlerListTriggers(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/automations", NewAutomationHandler(dxcAutomationService()).ListTriggers)
		w := dxcDo(r, http.MethodGet, "/automations", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "t1")
	})

	t.Run("error", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/automations", NewAutomationHandler(&unitAutomationService{listErr: errors.New("boom")}).ListTriggers)
		w := dxcDo(r, http.MethodGet, "/automations", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to list triggers")
	})
}

func TestDxcAutomationHandlerCreateTrigger(t *testing.T) {
	valid := `{"name":"t","event":"ticket.created"}`
	cases := []struct {
		name string
		svc  *unitAutomationService
		body string
		want int
	}{
		{"success", dxcAutomationService(), valid, http.StatusCreated},
		{"invalid json", dxcAutomationService(), `{`, http.StatusBadRequest},
		{"service error", &unitAutomationService{createErr: errors.New("boom")}, valid, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.POST("/automations", NewAutomationHandler(tc.svc).CreateTrigger)
			w := dxcDo(r, http.MethodPost, "/automations", tc.body)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcAutomationHandlerDeleteTrigger(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitAutomationService
		id   string
		want int
	}{
		{"success", dxcAutomationService(), "1", http.StatusOK},
		{"invalid id", dxcAutomationService(), "abc", http.StatusBadRequest},
		{"not found", &unitAutomationService{deleteErr: errors.New("trigger not found")}, "9", http.StatusNotFound},
		{"generic error", &unitAutomationService{deleteErr: errors.New("boom")}, "9", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.DELETE("/automations/:id", NewAutomationHandler(tc.svc).DeleteTrigger)
			w := dxcDo(r, http.MethodDelete, "/automations/"+tc.id, "")
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcAutomationHandlerListRuns(t *testing.T) {
	t.Run("success with defaults", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/automations/runs", NewAutomationHandler(dxcAutomationService()).ListRuns)
		w := dxcDo(r, http.MethodGet, "/automations/runs", "")
		assert.Equal(t, http.StatusOK, w.Code)
		var got PaginatedResponse
		dxcDecode(t, w, &got)
		assert.Equal(t, int64(1), got.Total)
		assert.Equal(t, 1, got.Page)
		assert.Equal(t, 20, got.PageSize)
	})

	t.Run("service error", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/automations/runs", NewAutomationHandler(&unitAutomationService{runsErr: errors.New("boom")}).ListRuns)
		w := dxcDo(r, http.MethodGet, "/automations/runs", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to list runs")
	})

	t.Run("invalid query", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/automations/runs", NewAutomationHandler(dxcAutomationService()).ListRuns)
		w := dxcDo(r, http.MethodGet, "/automations/runs?Page=abc", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid query parameters")
	})
}

func TestDxcAutomationHandlerRunBatch(t *testing.T) {
	valid := `{"event":"ticket.created","ticket_ids":[1,2],"dry_run":true}`
	cases := []struct {
		name string
		svc  *unitAutomationService
		body string
		want int
	}{
		{"success", dxcAutomationService(), valid, http.StatusOK},
		{"invalid json", dxcAutomationService(), `{`, http.StatusBadRequest},
		{"service error", &unitAutomationService{batchErr: errors.New("boom")}, valid, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.POST("/automations/run", NewAutomationHandler(tc.svc).RunBatch)
			w := dxcDo(r, http.MethodPost, "/automations/run", tc.body)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcRegisterAutomationRoutes(t *testing.T) {
	r := dxcRouter()
	RegisterAutomationRoutes(&r.RouterGroup, NewAutomationHandler(dxcAutomationService()))
	w := dxcDo(r, http.MethodGet, "/automations", "")
	assert.Equal(t, http.StatusOK, w.Code)
}
