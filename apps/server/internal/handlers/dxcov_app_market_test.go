package handlers

import (
	"errors"
	"net/http"
	"testing"

	appintegrationdelivery "servify/apps/server/internal/modules/app_integration/delivery"

	"github.com/stretchr/testify/assert"
)

func dxcAppMarketService() *unitAppMarketService {
	return &unitAppMarketService{
		items: []*appintegrationdelivery.AppIntegration{{ID: 1, Name: "app1"}},
		item:  &appintegrationdelivery.AppIntegration{ID: 1, Name: "app1"},
		total: 5,
	}
}

func TestDxcNewAppMarketHandler(t *testing.T) {
	svc := dxcAppMarketService()
	h := NewAppMarketHandler(svc)
	assert.NotNil(t, h)
	assert.Equal(t, svc, h.service)
}

func TestDxcAppMarketHandlerList(t *testing.T) {
	t.Run("success default pages", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/apps/integrations", NewAppMarketHandler(dxcAppMarketService()).ListIntegrations)
		w := dxcDo(r, http.MethodGet, "/apps/integrations", "")
		assert.Equal(t, http.StatusOK, w.Code)
		var got PaginatedResponse
		dxcDecode(t, w, &got)
		assert.Equal(t, int64(5), got.Total)
		assert.Equal(t, 1, got.Page)
		assert.Equal(t, 20, got.PageSize)
		assert.Equal(t, 1, got.Pages)
	})

	t.Run("explicit page size computes pages", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/apps/integrations", NewAppMarketHandler(dxcAppMarketService()).ListIntegrations)
		w := dxcDo(r, http.MethodGet, "/apps/integrations?page_size=2", "")
		assert.Equal(t, http.StatusOK, w.Code)
		var got PaginatedResponse
		dxcDecode(t, w, &got)
		assert.Equal(t, 3, got.Pages)
	})

	t.Run("zero page size yields zero pages", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/apps/integrations", NewAppMarketHandler(dxcAppMarketService()).ListIntegrations)
		w := dxcDo(r, http.MethodGet, "/apps/integrations?page_size=0", "")
		assert.Equal(t, http.StatusOK, w.Code)
		var got PaginatedResponse
		dxcDecode(t, w, &got)
		assert.Equal(t, 0, got.Pages)
	})

	t.Run("invalid query", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/apps/integrations", NewAppMarketHandler(dxcAppMarketService()).ListIntegrations)
		w := dxcDo(r, http.MethodGet, "/apps/integrations?page=abc", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid query")
	})

	t.Run("service error", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/apps/integrations", NewAppMarketHandler(&unitAppMarketService{listErr: errors.New("boom")}).ListIntegrations)
		w := dxcDo(r, http.MethodGet, "/apps/integrations", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to load integrations")
	})
}

func TestDxcAppMarketHandlerCreate(t *testing.T) {
	valid := `{"name":"app","iframe_url":"https://x/y"}`
	cases := []struct {
		name string
		svc  *unitAppMarketService
		body string
		want int
	}{
		{"success", dxcAppMarketService(), valid, http.StatusCreated},
		{"invalid json", dxcAppMarketService(), `{`, http.StatusBadRequest},
		{"missing required", dxcAppMarketService(), `{"name":"app"}`, http.StatusBadRequest},
		{"conflict", &unitAppMarketService{createErr: errors.New("slug already exists")}, valid, http.StatusConflict},
		{"generic error", &unitAppMarketService{createErr: errors.New("boom")}, valid, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.POST("/apps/integrations", NewAppMarketHandler(tc.svc).CreateIntegration)
			w := dxcDo(r, http.MethodPost, "/apps/integrations", tc.body)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcAppMarketHandlerUpdate(t *testing.T) {
	body := `{"name":"app2"}`
	cases := []struct {
		name string
		svc  *unitAppMarketService
		id   string
		body string
		want int
	}{
		{"success", dxcAppMarketService(), "1", body, http.StatusOK},
		{"invalid id", dxcAppMarketService(), "x", body, http.StatusBadRequest},
		{"invalid json", dxcAppMarketService(), "1", `{`, http.StatusBadRequest},
		{"not found", &unitAppMarketService{updateErr: errors.New("integration not found")}, "9", body, http.StatusNotFound},
		{"generic error", &unitAppMarketService{updateErr: errors.New("boom")}, "9", body, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.PUT("/apps/integrations/:id", NewAppMarketHandler(tc.svc).UpdateIntegration)
			w := dxcDo(r, http.MethodPut, "/apps/integrations/"+tc.id, tc.body)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcAppMarketHandlerDelete(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitAppMarketService
		id   string
		want int
	}{
		{"success", dxcAppMarketService(), "1", http.StatusOK},
		{"invalid id", dxcAppMarketService(), "x", http.StatusBadRequest},
		{"not found", &unitAppMarketService{deleteErr: errors.New("integration not found")}, "9", http.StatusNotFound},
		{"generic error", &unitAppMarketService{deleteErr: errors.New("boom")}, "9", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dxcRouter()
			r.DELETE("/apps/integrations/:id", NewAppMarketHandler(tc.svc).DeleteIntegration)
			w := dxcDo(r, http.MethodDelete, "/apps/integrations/"+tc.id, "")
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestDxcRegisterAppIntegrationRoutes(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		r := dxcRouter()
		RegisterAppIntegrationRoutes(&r.RouterGroup, NewAppMarketHandler(dxcAppMarketService()))
		w := dxcDo(r, http.MethodGet, "/apps/integrations", "")
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("nil group", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RegisterAppIntegrationRoutes(nil, NewAppMarketHandler(dxcAppMarketService()))
		})
	})

	t.Run("nil handler", func(t *testing.T) {
		r := dxcRouter()
		assert.NotPanics(t, func() {
			RegisterAppIntegrationRoutes(&r.RouterGroup, nil)
		})
	})
}
