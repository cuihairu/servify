package handlers

import (
	"errors"
	"net/http"
	"testing"

	"servify/apps/server/internal/models"

	"github.com/stretchr/testify/assert"
)

func dxcMacroService() *unitMacroService {
	return &unitMacroService{
		macros:  []models.Macro{{ID: 1, Name: "m1"}},
		macro:   &models.Macro{ID: 1, Name: "m1"},
		comment: &models.TicketComment{ID: 3, Content: "hi"},
	}
}

func TestDxcNewMacroHandler(t *testing.T) {
	svc := dxcMacroService()
	h := NewMacroHandler(svc)
	assert.NotNil(t, h)
	assert.Equal(t, svc, h.service)
}

func TestDxcMacroHandlerList(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/macros", NewMacroHandler(dxcMacroService()).List)
		w := dxcDo(r, http.MethodGet, "/macros", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "m1")
	})

	t.Run("error", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/macros", NewMacroHandler(&unitMacroService{listErr: errors.New("boom")}).List)
		w := dxcDo(r, http.MethodGet, "/macros", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to list macros")
	})
}

func TestDxcMacroHandlerCreate(t *testing.T) {
	valid := `{"name":"m","content":"c"}`
	cases := []struct {
		name string
		svc  *unitMacroService
		body string
		want int
	}{
		{"success", dxcMacroService(), valid, http.StatusCreated},
		{"invalid json", dxcMacroService(), `{`, http.StatusBadRequest},
		{"missing required", dxcMacroService(), `{"description":"d"}`, http.StatusBadRequest},
		{"service error", &unitMacroService{createErr: errors.New("boom")}, valid, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.POST("/macros", NewMacroHandler(tc.svc).Create)
			w := dxcDo(r, http.MethodPost, "/macros", tc.body)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcMacroHandlerUpdate(t *testing.T) {
	body := `{"content":"new"}`
	cases := []struct {
		name string
		svc  *unitMacroService
		id   string
		body string
		want int
	}{
		{"success", dxcMacroService(), "1", body, http.StatusOK},
		{"invalid id", dxcMacroService(), "abc", body, http.StatusBadRequest},
		{"invalid json", dxcMacroService(), "1", `{`, http.StatusBadRequest},
		{"not found", &unitMacroService{updateErr: errors.New("macro not found")}, "9", body, http.StatusNotFound},
		{"generic error", &unitMacroService{updateErr: errors.New("boom")}, "9", body, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.PUT("/macros/:id", NewMacroHandler(tc.svc).Update)
			w := dxcDo(r, http.MethodPut, "/macros/"+tc.id, tc.body)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcMacroHandlerDelete(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitMacroService
		id   string
		want int
	}{
		{"success", dxcMacroService(), "1", http.StatusOK},
		{"invalid id", dxcMacroService(), "abc", http.StatusBadRequest},
		{"not found", &unitMacroService{deleteErr: errors.New("macro not found")}, "9", http.StatusNotFound},
		{"generic error", &unitMacroService{deleteErr: errors.New("boom")}, "9", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.DELETE("/macros/:id", NewMacroHandler(tc.svc).Delete)
			w := dxcDo(r, http.MethodDelete, "/macros/"+tc.id, "")
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcMacroHandlerApply(t *testing.T) {
	valid := `{"ticket_id":5,"user_id":2}`
	cases := []struct {
		name string
		svc  *unitMacroService
		id   string
		body string
		want int
	}{
		{"success", dxcMacroService(), "1", valid, http.StatusOK},
		{"invalid id", dxcMacroService(), "x", valid, http.StatusBadRequest},
		{"invalid json", dxcMacroService(), "1", `{`, http.StatusBadRequest},
		{"missing ticket id", dxcMacroService(), "1", `{"user_id":2}`, http.StatusBadRequest},
		{"not found", &unitMacroService{applyErr: errors.New("macro not found")}, "9", valid, http.StatusNotFound},
		{"generic error", &unitMacroService{applyErr: errors.New("boom")}, "9", valid, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.POST("/macros/:id/apply", NewMacroHandler(tc.svc).Apply)
			w := dxcDo(r, http.MethodPost, "/macros/"+tc.id+"/apply", tc.body)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcRegisterMacroRoutes(t *testing.T) {
	r := dxcRouter()
	RegisterMacroRoutes(&r.RouterGroup, NewMacroHandler(dxcMacroService()))
	w := dxcDo(r, http.MethodGet, "/macros", "")
	assert.Equal(t, http.StatusOK, w.Code)
}
