package server

import (
	"servify/apps/server/internal/handlers"
	"servify/apps/server/internal/middleware"
	auditplatform "servify/apps/server/internal/platform/audit"
	"servify/apps/server/internal/platform/configscope"
	"servify/apps/server/internal/platform/usersecurity"

	"github.com/gin-gonic/gin"
)

func registerManagementRoutes(r *gin.Engine, deps Dependencies) {
	api := r.Group("/api")
	api.Use(middleware.AuthMiddleware(deps.Config, deps.DB, authPolicies(deps.DB)...))
	api.Use(middleware.EnforceRequestScope())
	api.Use(middleware.RequirePrincipalKinds("agent", "admin", "service"))
	api.Use(middleware.AuditMiddleware(deps.DB))

	customersAPI := api.Group("/")
	customersAPI.Use(middleware.RequireResourcePermission("customers"))
	handlers.RegisterCustomerRoutes(customersAPI, handlers.NewCustomerHandler(deps.CustomerHandlerService, deps.Logger))

	agentsAPI := api.Group("/")
	agentsAPI.Use(middleware.RequireResourcePermission("agents"))
	handlers.RegisterAgentRoutes(agentsAPI, handlers.NewAgentHandler(deps.AgentHandlerService, deps.Logger))
	handlers.RegisterAgentGroupRoutes(agentsAPI, handlers.NewAgentGroupHandler(deps.AgentGroupService))

	remoteAssistAPI := api.Group("/")
	remoteAssistAPI.Use(middleware.RequireResourcePermission("assist"))
	handlers.RegisterAssistRoutes(remoteAssistAPI, handlers.NewAssistHandler(deps.AssistHandlerService))

	ticketsAPI := api.Group("/")
	ticketsAPI.Use(middleware.RequireResourcePermission("tickets"))
	handlers.RegisterTicketRoutes(ticketsAPI, handlers.NewTicketHandler(deps.TicketHandlerService, deps.Logger))

	sessionTransferAPI := api.Group("/")
	sessionTransferAPI.Use(middleware.RequireResourcePermission("session_transfer"))
	handlers.RegisterSessionTransferRoutes(sessionTransferAPI, handlers.NewSessionTransferHandler(deps.TransferHandlerService, deps.Logger))

	satisfactionAPI := api.Group("/")
	satisfactionAPI.Use(middleware.RequireResourcePermission("satisfaction"))
	handlers.RegisterSatisfactionRoutes(satisfactionAPI, handlers.NewSatisfactionHandler(deps.SatisfactionService, deps.Logger))

	workspaceAPI := api.Group("/")
	workspaceAPI.Use(middleware.RequireResourcePermission("workspace"))
	handlers.RegisterWorkspaceRoutes(workspaceAPI, handlers.NewWorkspaceHandler(deps.WorkspaceService))
	handlers.RegisterConversationWorkspaceRoutes(workspaceAPI, handlers.NewConversationWorkspaceHandler(deps.ConversationHandler, deps.RealtimeGateway))

	macrosAPI := api.Group("/")
	macrosAPI.Use(middleware.RequireResourcePermission("macros"))
	handlers.RegisterMacroRoutes(macrosAPI, handlers.NewMacroHandler(deps.MacroService))

	integrationsAPI := api.Group("/")
	integrationsAPI.Use(middleware.RequireResourcePermission("integrations"))
	handlers.RegisterAppIntegrationRoutes(integrationsAPI, handlers.NewAppMarketHandler(deps.AppIntegrationService))

	customFieldsAPI := api.Group("/")
	customFieldsAPI.Use(middleware.RequireResourcePermission("custom_fields"))
	handlers.RegisterCustomFieldRoutes(customFieldsAPI, handlers.NewCustomFieldHandler(deps.CustomFieldService))

	statisticsAPI := api.Group("/")
	statisticsAPI.Use(middleware.RequireResourcePermission("statistics"))
	handlers.RegisterStatisticsRoutes(statisticsAPI,
		handlers.NewStatisticsHandler(deps.StatisticsHandlerService, deps.Logger),
		handlers.NewStatisticsExportHandler(deps.StatisticsHandlerService, deps.SatisfactionService, deps.Logger))

	slaAPI := api.Group("/")
	slaAPI.Use(middleware.RequireResourcePermission("sla"))
	handlers.RegisterSLARoutes(slaAPI, handlers.NewSLAHandler(deps.SLAService, deps.TicketReaderService))

	shiftAPI := api.Group("/")
	shiftAPI.Use(middleware.RequireResourcePermission("shift"))
	handlers.RegisterShiftRoutes(shiftAPI, handlers.NewShiftHandler(deps.ShiftService))

	automationAPI := api.Group("/")
	automationAPI.Use(middleware.RequireResourcePermission("automation"))
	handlers.RegisterAutomationRoutes(automationAPI, handlers.NewAutomationHandler(deps.AutomationHandlerService))

	knowledgeAPI := api.Group("/")
	knowledgeAPI.Use(middleware.RequireResourcePermission("knowledge"))
	handlers.RegisterKnowledgeDocRoutes(knowledgeAPI, handlers.NewKnowledgeDocHandler(deps.KnowledgeDocHandler))

	assistAPI := api.Group("/")
	assistAPI.Use(middleware.RequireResourcePermission("assist"))
	handlers.RegisterSuggestionRoutes(assistAPI, handlers.NewSuggestionHandler(deps.SuggestionService))

	gamificationAPI := api.Group("/")
	gamificationAPI.Use(middleware.RequireResourcePermission("gamification"))
	handlers.RegisterGamificationRoutes(gamificationAPI, handlers.NewGamificationHandler(deps.GamificationService))

	voiceAPI := api.Group("/")
	voiceAPI.Use(middleware.RequireResourcePermission("voice"))
	handlers.RegisterVoiceRoutes(voiceAPI, handlers.NewVoiceHandler(deps.VoiceCoordinator, deps.VoiceProtocolRegistry))

	webhooksAPI := api.Group("/")
	webhooksAPI.Use(middleware.RequireResourcePermission("webhooks"))
	handlers.RegisterWebhookRoutes(webhooksAPI, handlers.NewWebhookHandler(deps.WebhookHandlerService))

	qualityAPI := api.Group("/")
	qualityAPI.Use(middleware.RequireResourcePermission("quality"))
	handlers.RegisterQualityRoutes(qualityAPI, handlers.NewQualityHandler(deps.QualityHandlerService))

	// 开放平台：API Key 管理 + X-API-Key 只读会话面（service principal）。
	apiKeysAPI := api.Group("/")
	apiKeysAPI.Use(middleware.RequireResourcePermission("api_keys"))
	handlers.RegisterAPIKeyRoutes(apiKeysAPI, handlers.NewAPIKeyHandler(deps.APIKeyService))

	conversationsAPI := api.Group("/")
	conversationsAPI.Use(middleware.RequireResourcePermission("conversations"))
	handlers.RegisterOpenConversationRoutes(conversationsAPI, handlers.NewOpenConversationHandler(deps.OpenConversationReader))

	auditAPI := api.Group("/")
	auditAPI.Use(middleware.RequireResourcePermission("audit"))
	handlers.RegisterAuditRoutes(auditAPI, handlers.NewAuditHandler(auditplatform.NewGormQueryService(deps.DB)))

	securityAPI := api.Group("/")
	securityAPI.Use(middleware.RequireResourcePermission("security"))
	securitySessionRiskResolver := configscope.NewResolver(
		deps.Config,
		configscope.WithTenantSessionRiskProvider(configscope.NewGormTenantConfigProvider(deps.DB)),
		configscope.WithWorkspaceSessionRiskProvider(configscope.NewGormWorkspaceConfigProvider(deps.DB)),
	)
	userSecurityHandler := handlers.NewUserSecurityHandler(usersecurity.NewService(deps.DB, deps.Logger), deps.Logger).
		WithJWTSecret(deps.Config.JWT.Secret).
		WithSessionRiskResolver(securitySessionRiskResolver)
	if provider := sessionIPIntelligenceFromConfig(deps.Config); provider != nil {
		userSecurityHandler = userSecurityHandler.WithSessionIPIntelligence(provider)
	}
	handlers.RegisterUserSecurityRoutes(securityAPI, userSecurityHandler)
	handlers.RegisterScopedConfigRoutes(securityAPI, handlers.NewScopedConfigHandler(configscope.NewGormConfigStore(deps.DB), auditplatform.NewGormQueryService(deps.DB)))
}
