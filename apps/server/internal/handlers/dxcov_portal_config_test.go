package handlers

import (
	"net/http"
	"testing"

	"servify/apps/server/internal/config"

	"github.com/stretchr/testify/assert"
)

func TestDxcPortalConfigHandlerGetWithoutResolver(t *testing.T) {
	t.Run("nil resolver with config", func(t *testing.T) {
		cfg := &config.Config{
			Portal: config.PortalConfig{
				BrandName:     "Dxc Brand",
				DefaultLocale: "zh-CN",
			},
		}
		h := NewPortalConfigHandlerWithResolver(cfg, nil)
		r := dxcRouter()
		r.GET("/portal/config", h.Get)
		w := dxcDo(r, http.MethodGet, "/portal/config", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Dxc Brand")
	})

	t.Run("nil resolver and nil config", func(t *testing.T) {
		h := NewPortalConfigHandlerWithResolver(nil, nil)
		r := dxcRouter()
		r.GET("/portal/config", h.Get)
		w := dxcDo(r, http.MethodGet, "/portal/config", "")
		assert.Equal(t, http.StatusOK, w.Code)
		var got PortalConfigResponse
		dxcDecode(t, w, &got)
		assert.Equal(t, "", got.BrandName)
		assert.Equal(t, "", got.DefaultLocale)
	})
}
