package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"servify/apps/server/internal/models"

	"github.com/stretchr/testify/assert"
)

type dxcCustomFieldRecorder struct {
	*unitCustomFieldService
	resource   string
	activeOnly bool
}

func (r *dxcCustomFieldRecorder) List(ctx context.Context, resource string, activeOnly bool) ([]models.CustomField, error) {
	r.resource = resource
	r.activeOnly = activeOnly
	return r.unitCustomFieldService.List(ctx, resource, activeOnly)
}

func dxcCustomFieldSvc() *unitCustomFieldService {
	return &unitCustomFieldService{
		fields: []models.CustomField{{ID: 1, Key: "env", Name: "Env"}},
		field:  &models.CustomField{ID: 1, Key: "env", Name: "Env"},
	}
}

func TestDxcNewCustomFieldHandler(t *testing.T) {
	svc := dxcCustomFieldSvc()
	h := NewCustomFieldHandler(svc)
	assert.NotNil(t, h)
	assert.Equal(t, svc, h.service)
}

func TestDxcCustomFieldHandlerList(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		svc := &dxcCustomFieldRecorder{unitCustomFieldService: dxcCustomFieldSvc()}
		r := dxcRouter()
		r.GET("/custom-fields", NewCustomFieldHandler(svc).List)
		w := dxcDo(r, http.MethodGet, "/custom-fields", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "ticket", svc.resource)
		assert.True(t, svc.activeOnly)
	})

	t.Run("explicit params", func(t *testing.T) {
		svc := &dxcCustomFieldRecorder{unitCustomFieldService: dxcCustomFieldSvc()}
		r := dxcRouter()
		r.GET("/custom-fields", NewCustomFieldHandler(svc).List)
		w := dxcDo(r, http.MethodGet, "/custom-fields?resource=customer&active=false", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "customer", svc.resource)
		assert.False(t, svc.activeOnly)
	})

	t.Run("service error", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/custom-fields", NewCustomFieldHandler(&unitCustomFieldService{listErr: errors.New("boom")}).List)
		w := dxcDo(r, http.MethodGet, "/custom-fields", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to list custom fields")
	})
}

func TestDxcCustomFieldHandlerGet(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/custom-fields/:id", NewCustomFieldHandler(dxcCustomFieldSvc()).Get)
		w := dxcDo(r, http.MethodGet, "/custom-fields/1", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "env")
	})

	t.Run("invalid id", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/custom-fields/:id", NewCustomFieldHandler(dxcCustomFieldSvc()).Get)
		w := dxcDo(r, http.MethodGet, "/custom-fields/abc", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error maps to 404", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/custom-fields/:id", NewCustomFieldHandler(&unitCustomFieldService{getErr: errors.New("boom")}).Get)
		w := dxcDo(r, http.MethodGet, "/custom-fields/9", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Custom field not found")
	})
}

func TestDxcCustomFieldHandlerCreate(t *testing.T) {
	valid := `{"key":"env","name":"Env","type":"text"}`
	cases := []struct {
		name string
		svc  *unitCustomFieldService
		body string
		want int
	}{
		{"success", dxcCustomFieldSvc(), valid, http.StatusCreated},
		{"invalid json", dxcCustomFieldSvc(), `{`, http.StatusBadRequest},
		{"missing required", dxcCustomFieldSvc(), `{"key":"env"}`, http.StatusBadRequest},
		{"service error", &unitCustomFieldService{createErr: errors.New("boom")}, valid, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.POST("/custom-fields", NewCustomFieldHandler(tc.svc).Create)
			w := dxcDo(r, http.MethodPost, "/custom-fields", tc.body)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcCustomFieldHandlerUpdate(t *testing.T) {
	body := `{"name":"Env2"}`
	cases := []struct {
		name string
		svc  *unitCustomFieldService
		id   string
		body string
		want int
	}{
		{"success", dxcCustomFieldSvc(), "1", body, http.StatusOK},
		{"invalid id", dxcCustomFieldSvc(), "x", body, http.StatusBadRequest},
		{"invalid json", dxcCustomFieldSvc(), "1", `{`, http.StatusBadRequest},
		{"not found", &unitCustomFieldService{updateErr: errors.New("custom field not found")}, "9", body, http.StatusNotFound},
		{"generic error", &unitCustomFieldService{updateErr: errors.New("boom")}, "9", body, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.PUT("/custom-fields/:id", NewCustomFieldHandler(tc.svc).Update)
			w := dxcDo(r, http.MethodPut, "/custom-fields/"+tc.id, tc.body)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcCustomFieldHandlerDelete(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitCustomFieldService
		id   string
		want int
	}{
		{"success", dxcCustomFieldSvc(), "1", http.StatusOK},
		{"invalid id", dxcCustomFieldSvc(), "x", http.StatusBadRequest},
		{"not found", &unitCustomFieldService{deleteErr: errors.New("custom field not found")}, "9", http.StatusNotFound},
		{"generic error", &unitCustomFieldService{deleteErr: errors.New("boom")}, "9", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.DELETE("/custom-fields/:id", NewCustomFieldHandler(tc.svc).Delete)
			w := dxcDo(r, http.MethodDelete, "/custom-fields/"+tc.id, "")
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcRegisterCustomFieldRoutes(t *testing.T) {
	r := dxcRouter()
	RegisterCustomFieldRoutes(&r.RouterGroup, NewCustomFieldHandler(dxcCustomFieldSvc()))
	w := dxcDo(r, http.MethodGet, "/custom-fields", "")
	assert.Equal(t, http.StatusOK, w.Code)
}
