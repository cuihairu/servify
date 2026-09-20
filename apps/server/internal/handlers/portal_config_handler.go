package handlers

import (
	"net/http"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/configscope"

	"github.com/gin-gonic/gin"
)

type PortalConfigHandler struct {
	cfg      *config.Config
	resolver *configscope.Resolver
}

func NewPortalConfigHandlerWithResolver(cfg *config.Config, resolver *configscope.Resolver) *PortalConfigHandler {
	return &PortalConfigHandler{cfg: cfg, resolver: resolver}
}

type PortalConfigResponse struct {
	BrandName      string   `json:"brand_name"`
	LogoURL        string   `json:"logo_url,omitempty"`
	PrimaryColor   string   `json:"primary_color,omitempty"`
	SecondaryColor string   `json:"secondary_color,omitempty"`
	DefaultLocale  string   `json:"default_locale"`
	Locales        []string `json:"locales"`
	SupportEmail   string   `json:"support_email,omitempty"`
}

func (h *PortalConfigHandler) Get(c *gin.Context) {
	var p config.PortalConfig
	if h.resolver != nil {
		p = h.resolver.ResolvePortal(c.Request.Context(), nil)
	} else if h.cfg != nil {
		p = configscope.NewResolver(h.cfg).ResolvePortal(c.Request.Context(), nil)
	}
	c.JSON(http.StatusOK, PortalConfigResponse{
		BrandName:      p.BrandName,
		LogoURL:        p.LogoURL,
		PrimaryColor:   p.PrimaryColor,
		SecondaryColor: p.SecondaryColor,
		DefaultLocale:  p.DefaultLocale,
		Locales:        p.Locales,
		SupportEmail:   p.SupportEmail,
	})
}
