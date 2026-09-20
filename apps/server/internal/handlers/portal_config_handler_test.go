package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/configscope"
)

func TestPortalConfigHandler_Get_WithDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Portal: config.PortalConfig{
			BrandName: "",
		},
	}
	handler := NewPortalConfigHandlerWithResolver(cfg, configscope.NewResolver(cfg))

	router := gin.New()
	router.GET("/portal/config", handler.Get)

	req := httptest.NewRequest("GET", "/portal/config", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Check default values
	assert.Contains(t, w.Body.String(), "Servify")
	assert.Contains(t, w.Body.String(), "#4299e1")
	assert.Contains(t, w.Body.String(), "zh-CN")
}

func TestPortalConfigHandler_Get_WithCustomConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Portal: config.PortalConfig{
			BrandName:      "Test Brand",
			LogoURL:        "https://example.com/logo.png",
			PrimaryColor:   "#FF0000",
			SecondaryColor: "#00FF00",
			DefaultLocale:  "en-US",
			Locales:        []string{"en-US", "fr-FR"},
			SupportEmail:   "support@example.com",
		},
	}
	handler := NewPortalConfigHandlerWithResolver(cfg, configscope.NewResolver(cfg))

	router := gin.New()
	router.GET("/portal/config", handler.Get)

	req := httptest.NewRequest("GET", "/portal/config", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Check custom values
	assert.Contains(t, w.Body.String(), "Test Brand")
	assert.Contains(t, w.Body.String(), "https://example.com/logo.png")
	assert.Contains(t, w.Body.String(), "#FF0000")
	assert.Contains(t, w.Body.String(), "#00FF00")
	assert.Contains(t, w.Body.String(), "en-US")
	assert.Contains(t, w.Body.String(), "fr-FR")
	assert.Contains(t, w.Body.String(), "support@example.com")
}

func TestPortalConfigHandler_Get_WithNilConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewPortalConfigHandlerWithResolver(nil, configscope.NewResolver(nil))

	router := gin.New()
	router.GET("/portal/config", handler.Get)

	req := httptest.NewRequest("GET", "/portal/config", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Check defaults are applied
	assert.Contains(t, w.Body.String(), "Servify")
}

func TestPortalConfigHandler_Get_WithPartialConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Portal: config.PortalConfig{
			BrandName: "Partial Brand",
			// Leave other fields empty to test defaults
		},
	}
	handler := NewPortalConfigHandlerWithResolver(cfg, configscope.NewResolver(cfg))

	router := gin.New()
	router.GET("/portal/config", handler.Get)

	req := httptest.NewRequest("GET", "/portal/config", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Custom brand name
	assert.Contains(t, w.Body.String(), "Partial Brand")
	// Defaults for other fields
	assert.Contains(t, w.Body.String(), "#4299e1")
	assert.Contains(t, w.Body.String(), "zh-CN")
}

func TestNewPortalConfigHandler(t *testing.T) {
	cfg := &config.Config{
		Portal: config.PortalConfig{
			BrandName: "Test",
		},
	}
	handler := NewPortalConfigHandlerWithResolver(cfg, configscope.NewResolver(cfg))

	assert.NotNil(t, handler)
	assert.Equal(t, cfg, handler.cfg)
}

func TestPortalConfigHandler_Get_WithResolverOverrides(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Portal: config.PortalConfig{
			BrandName:      "System Brand",
			PrimaryColor:   "#111111",
			SecondaryColor: "#222222",
			DefaultLocale:  "zh-CN",
		},
	}
	resolver := configscope.NewResolver(
		cfg,
		configscope.WithTenantPortalProvider(stubPortalProvider{
			ok: true,
			value: config.PortalConfig{
				BrandName:    "Tenant Brand",
				PrimaryColor: "#333333",
			},
		}),
		configscope.WithWorkspacePortalProvider(stubPortalProvider{
			ok: true,
			value: config.PortalConfig{
				BrandName:     "Workspace Brand",
				DefaultLocale: "en-US",
			},
		}),
	)
	handler := NewPortalConfigHandlerWithResolver(cfg, resolver)

	router := gin.New()
	router.GET("/portal/config", handler.Get)

	req := httptest.NewRequest("GET", "/portal/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Workspace Brand")
	assert.Contains(t, w.Body.String(), "#333333")
	assert.Contains(t, w.Body.String(), "en-US")
}

type stubPortalProvider struct {
	value config.PortalConfig
	ok    bool
	err   error
}

func (s stubPortalProvider) LoadPortalConfig(ctx context.Context) (config.PortalConfig, bool, error) {
	return s.value, s.ok, s.err
}

func TestPortalConfigResponse_Structure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Portal: config.PortalConfig{
			BrandName:      "Test Brand",
			LogoURL:        "logo.png",
			PrimaryColor:   "#123456",
			SecondaryColor: "#654321",
			DefaultLocale:  "de-DE",
			Locales:        []string{"de-DE", "en-GB"},
			SupportEmail:   "test@test.com",
		},
	}
	handler := NewPortalConfigHandlerWithResolver(cfg, configscope.NewResolver(cfg))

	router := gin.New()
	router.GET("/portal/config", handler.Get)

	req := httptest.NewRequest("GET", "/portal/config", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "brand_name")
	assert.Contains(t, w.Body.String(), "primary_color")
	assert.Contains(t, w.Body.String(), "secondary_color")
	assert.Contains(t, w.Body.String(), "default_locale")
	assert.Contains(t, w.Body.String(), "locales")
}
