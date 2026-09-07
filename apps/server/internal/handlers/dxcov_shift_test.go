package handlers

import (
	"errors"
	"net/http"
	"testing"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/services"

	"github.com/stretchr/testify/assert"
)

func dxcShiftService() *unitShiftService {
	return &unitShiftService{
		shift:  &models.ShiftSchedule{ID: 1, AgentID: 2, ShiftType: "morning"},
		shifts: []models.ShiftSchedule{{ID: 1}, {ID: 2}},
		total:  2,
		stats:  &services.ShiftStatsResponse{Total: 2},
	}
}

func TestDxcNewShiftHandler(t *testing.T) {
	svc := dxcShiftService()
	h := NewShiftHandler(svc)
	assert.NotNil(t, h)
	assert.Equal(t, svc, h.shiftService)
}

func TestDxcShiftHandlerCreate(t *testing.T) {
	valid := `{"agent_id":2,"shift_type":"morning","start_time":"2026-01-01T08:00:00Z","end_time":"2026-01-01T16:00:00Z"}`
	cases := []struct {
		name string
		svc  *unitShiftService
		body string
		want int
	}{
		{"success", dxcShiftService(), valid, http.StatusCreated},
		{"invalid json", dxcShiftService(), `{`, http.StatusBadRequest},
		{"missing required", dxcShiftService(), `{"notes":"x"}`, http.StatusBadRequest},
		{"service error", &unitShiftService{createErr: errors.New("boom")}, valid, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.POST("/shifts", NewShiftHandler(tc.svc).CreateShift)
			w := dxcDo(r, http.MethodPost, "/shifts", tc.body)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcShiftHandlerList(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/shifts", NewShiftHandler(dxcShiftService()).ListShifts)
		w := dxcDo(r, http.MethodGet, "/shifts", "")
		assert.Equal(t, http.StatusOK, w.Code)
		var got PaginatedResponse
		dxcDecode(t, w, &got)
		assert.Equal(t, int64(2), got.Total)
		assert.Equal(t, 1, got.Page)
		assert.Equal(t, 20, got.PageSize)
	})

	t.Run("valid rfc3339 dates bind", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/shifts", NewShiftHandler(dxcShiftService()).ListShifts)
		w := dxcDo(r, http.MethodGet, "/shifts?date_from=2026-01-01T00:00:00Z&date_to=2026-02-01T00:00:00Z", "")
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("invalid query", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/shifts", NewShiftHandler(dxcShiftService()).ListShifts)
		w := dxcDo(r, http.MethodGet, "/shifts?date_from=notadate", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "INVALID_QUERY")
	})

	t.Run("service error", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/shifts", NewShiftHandler(&unitShiftService{listErr: errors.New("boom")}).ListShifts)
		w := dxcDo(r, http.MethodGet, "/shifts", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "LIST_FAILED")
	})
}

func TestDxcShiftHandlerUpdate(t *testing.T) {
	body := `{"status":"active"}`
	cases := []struct {
		name   string
		svc    *unitShiftService
		id     string
		body   string
		want   int
		wantEr string
	}{
		{"success", dxcShiftService(), "1", body, http.StatusOK, ""},
		{"invalid id", dxcShiftService(), "abc", body, http.StatusBadRequest, "INVALID_ID"},
		{"invalid json", dxcShiftService(), "1", `{`, http.StatusBadRequest, "INVALID_REQUEST"},
		{"not found", &unitShiftService{updateErr: errors.New("shift not found")}, "9", body, http.StatusNotFound, "UPDATE_FAILED"},
		{"bad time range", &unitShiftService{updateErr: errors.New("end_time must be after start_time")}, "9", body, http.StatusBadRequest, "UPDATE_FAILED"},
		{"generic error", &unitShiftService{updateErr: errors.New("boom")}, "9", body, http.StatusInternalServerError, "UPDATE_FAILED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.PUT("/shifts/:id", NewShiftHandler(tc.svc).UpdateShift)
			w := dxcDo(r, http.MethodPut, "/shifts/"+tc.id, tc.body)
			assert.Equal(t, tc.want, w.Code)
			if tc.wantEr != "" {
				assert.Contains(t, w.Body.String(), tc.wantEr)
			}
		})
	}
}

func TestDxcShiftHandlerDelete(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitShiftService
		id   string
		want int
	}{
		{"success", dxcShiftService(), "1", http.StatusOK},
		{"invalid id", dxcShiftService(), "x", http.StatusBadRequest},
		{"not found", &unitShiftService{deleteErr: errors.New("shift not found")}, "9", http.StatusNotFound},
		{"generic error", &unitShiftService{deleteErr: errors.New("boom")}, "9", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.DELETE("/shifts/:id", NewShiftHandler(tc.svc).DeleteShift)
			w := dxcDo(r, http.MethodDelete, "/shifts/"+tc.id, "")
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcShiftHandlerStats(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/shifts/stats", NewShiftHandler(dxcShiftService()).GetShiftStats)
		w := dxcDo(r, http.MethodGet, "/shifts/stats", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"total":2`)
	})

	t.Run("error", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/shifts/stats", NewShiftHandler(&unitShiftService{statsErr: errors.New("boom")}).GetShiftStats)
		w := dxcDo(r, http.MethodGet, "/shifts/stats", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "STATS_FAILED")
	})
}

func TestDxcRegisterShiftRoutes(t *testing.T) {
	r := dxcRouter()
	RegisterShiftRoutes(&r.RouterGroup, NewShiftHandler(dxcShiftService()))
	w := dxcDo(r, http.MethodGet, "/shifts", "")
	assert.Equal(t, http.StatusOK, w.Code)
}
