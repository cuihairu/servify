package server

import (
	"servify/apps/server/internal/handlers"
	"servify/apps/server/internal/platform/configscope"
	"servify/apps/server/internal/platform/voiceprotocol"

	"github.com/gin-gonic/gin"
)

func registerPublicRoutes(r *gin.Engine, deps Dependencies) {
	public := r.Group("/public")
	handlers.RegisterCSATSurveyRoutes(public, handlers.NewCSATSurveyHandler(deps.SatisfactionService))
	handlers.RegisterPublicKnowledgeBaseRoutes(public, handlers.NewKnowledgeDocHandler(deps.KnowledgeDocHandler))
	handlers.RegisterPublicSuggestionRoutes(public, handlers.NewSuggestionHandler(deps.SuggestionService))
	portalResolver := configscope.NewResolver(
		deps.Config,
		configscope.WithTenantPortalProvider(configscope.NewGormTenantConfigProvider(deps.DB)),
		configscope.WithWorkspacePortalProvider(configscope.NewGormWorkspaceConfigProvider(deps.DB)),
	)
	public.GET("/portal/config", handlers.NewPortalConfigHandlerWithResolver(deps.Config, portalResolver).Get)
	registerPSTNWebhookRoutes(public, deps)
}

// registerPSTNWebhookRoutes mounts the hosted-vendor PSTN webhook only when
// an adapter is registered under the hosted-vendor protocol; the route does
// not exist otherwise (disabled provider serves 404).
func registerPSTNWebhookRoutes(public *gin.RouterGroup, deps Dependencies) {
	if deps.VoiceProtocolRegistry == nil {
		return
	}
	adapter, ok := deps.VoiceProtocolRegistry.Signaling(voiceprotocol.ProtocolHostedVendorWebhook)
	if !ok {
		return
	}
	hostedAdapter, ok := adapter.(voiceprotocol.HostedVendorWebhookAdapter)
	if !ok {
		return
	}
	handler := handlers.NewPSTNWebhookHandler(
		deps.VoiceCoordinator,
		hostedAdapter,
		deps.Config.Voice.PSTN.ValidateSignature,
		deps.Config.Server.PublicBaseURL,
	)
	handlers.RegisterPSTNWebhookRoutes(public, handler)
}
