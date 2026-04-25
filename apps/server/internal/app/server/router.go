package server

import (
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/handlers"
	"servify/apps/server/internal/middleware"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	analyticsdelivery "servify/apps/server/internal/modules/analytics/delivery"
	automationdelivery "servify/apps/server/internal/modules/automation/delivery"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	customerdelivery "servify/apps/server/internal/modules/customer/delivery"
	knowledgedelivery "servify/apps/server/internal/modules/knowledge/delivery"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	ticketdelivery "servify/apps/server/internal/modules/ticket/delivery"
	voicedelivery "servify/apps/server/internal/modules/voice/delivery"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	auditplatform "servify/apps/server/internal/platform/audit"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/configscope"
	realtimeplatform "servify/apps/server/internal/platform/realtime"
	"servify/apps/server/internal/platform/storage"
	"servify/apps/server/internal/platform/usersecurity"
	"servify/apps/server/internal/platform/voiceprotocol"
	"servify/apps/server/internal/services"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"time"
)

// Dependencies contains the runtime services required to assemble the HTTP router.
type Dependencies struct {
	Config                   *config.Config
	Logger                   *logrus.Logger
	DB                       *gorm.DB
	Redis                    *redis.Client
	AIService                aidelivery.RuntimeService
	AIHandlerService         aidelivery.HandlerService
	RealtimeGateway          realtimeplatform.RealtimeGateway
	RTCGateway               realtimeplatform.RTCGateway
	MessageRouter            services.MessageRouterRuntime
	VoiceCoordinator         *voicedelivery.Coordinator
	VoiceProtocolRegistry    *voiceprotocol.Registry
	CustomerHandlerService   customerdelivery.HandlerService
	ConversationHandler      conversationdelivery.HandlerService
	AgentHandlerService      agentdelivery.HandlerService
	TicketHandlerService     ticketdelivery.HandlerService
	TicketReaderService      *ticketdelivery.ReaderServiceAdapter
	TransferHandlerService   routingdelivery.HandlerService
	SatisfactionService      handlers.SatisfactionService
	WorkspaceService         services.WorkspaceOverviewReader
	MacroService             handlers.MacroService
	AppIntegrationService    handlers.AppMarketService
	CustomFieldService       handlers.CustomFieldService
	StatisticsHandlerService analyticsdelivery.HandlerService
	SLAService               handlers.SLAService
	ShiftService             handlers.ShiftService
	AutomationHandlerService automationdelivery.HandlerService
	KnowledgeDocHandler      knowledgedelivery.HandlerService
	SuggestionService        handlers.SuggestionService
	GamificationService      handlers.GamificationService
	HTTPMetrics              *svcmetrics.HTTPMetrics
}

// BuildRouter assembles the HTTP routes and middleware around already-wired services.
func BuildRouter(deps Dependencies) *gin.Engine {
	r := gin.New()
	registerBaseMiddleware(r, deps.Config, deps.HTTPMetrics)
	registerHealthRoutes(r, deps)
	registerAuthRoutes(r, deps)
	registerManagementRoutes(r, deps)
	registerPublicRoutes(r, deps)
	registerRealtimeRoutes(r, deps)
	registerStatic(r)
	if deps.Logger != nil {
		for _, warning := range RouteSecurityWarnings(r.Routes(), deps.Config) {
			deps.Logger.Warnf("security surface warning: %s", warning)
		}
	}
	return r
}

func authPolicies(db *gorm.DB) []platformauth.TokenPolicy {
	return []platformauth.TokenPolicy{
		platformauth.NewRevokedTokenPolicy(db),
		platformauth.NewUserStateTokenPolicy(db),
	}
}

func registerAuthRoutes(r *gin.Engine, deps Dependencies) {
	auth := r.Group("/api/v1/auth")
	sessionRiskResolver := configscope.NewResolver(
		deps.Config,
		configscope.WithTenantSessionRiskProvider(configscope.NewGormTenantConfigProvider(deps.DB)),
		configscope.WithWorkspaceSessionRiskProvider(configscope.NewGormWorkspaceConfigProvider(deps.DB)),
	)
	authHandler := handlers.NewAuthHandler(services.NewAuthService(deps.DB, deps.Config)).WithSessionRiskResolver(sessionRiskResolver)
	if provider := sessionIPIntelligenceFromConfig(deps.Config); provider != nil {
		authHandler = authHandler.WithSessionIPIntelligence(provider)
	}

	auth.POST("/register", authHandler.Register)
	auth.POST("/login", authHandler.Login)
	auth.POST("/refresh", authHandler.RefreshToken)

	// Authenticated auth routes
	authMe := auth.Group("")
	authMe.Use(middleware.AuthMiddleware(deps.Config, authPolicies(deps.DB)...))
	authMe.GET("/me", authHandler.GetCurrentUser)
	authMe.GET("/sessions", authHandler.ListSessions)
	authMe.POST("/sessions/logout-current", authHandler.LogoutCurrentSession)
	authMe.POST("/sessions/logout-others", authHandler.LogoutOtherSessions)

	// File upload (authenticated)
	localStorage := storage.NewLocalProvider("./uploads", "/uploads")
	uploadHandler := handlers.NewFileUploadHandler(localStorage, 32<<20) // 32MB max
	r.POST("/api/v1/upload", middleware.AuthMiddleware(deps.Config, authPolicies(deps.DB)...), uploadHandler.Upload)
	r.Static("/uploads", "./uploads")
}

func registerManagementRoutes(r *gin.Engine, deps Dependencies) {
	api := r.Group("/api")
	api.Use(middleware.AuthMiddleware(deps.Config, authPolicies(deps.DB)...))
	api.Use(middleware.EnforceRequestScope())
	api.Use(middleware.RequirePrincipalKinds("agent", "admin", "service"))
	api.Use(middleware.AuditMiddleware(deps.DB))

	customersAPI := api.Group("/")
	customersAPI.Use(middleware.RequireResourcePermission("customers"))
	handlers.RegisterCustomerRoutes(customersAPI, handlers.NewCustomerHandler(deps.CustomerHandlerService, deps.Logger))

	agentsAPI := api.Group("/")
	agentsAPI.Use(middleware.RequireResourcePermission("agents"))
	handlers.RegisterAgentRoutes(agentsAPI, handlers.NewAgentHandler(deps.AgentHandlerService, deps.Logger))

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
	handlers.RegisterStatisticsRoutes(statisticsAPI, handlers.NewStatisticsHandler(deps.StatisticsHandlerService, deps.Logger))

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
	userSecurityHandler := handlers.NewUserSecurityHandler(usersecurity.NewService(deps.DB, deps.Logger), deps.Logger).WithJWTSecret(deps.Config.JWT.Secret).WithSessionRiskResolver(securitySessionRiskResolver)
	if provider := sessionIPIntelligenceFromConfig(deps.Config); provider != nil {
		userSecurityHandler = userSecurityHandler.WithSessionIPIntelligence(provider)
	}
	handlers.RegisterUserSecurityRoutes(securityAPI, userSecurityHandler)
	handlers.RegisterScopedConfigRoutes(securityAPI, handlers.NewScopedConfigHandler(configscope.NewGormConfigStore(deps.DB), auditplatform.NewGormQueryService(deps.DB)))
}

func sessionIPIntelligenceFromConfig(cfg *config.Config) *handlers.HTTPSessionIPIntelligence {
	if cfg == nil {
		return nil
	}
	providerCfg := cfg.Security.SessionIPIntelligence
	if !providerCfg.Enabled || strings.TrimSpace(providerCfg.BaseURL) == "" {
		return nil
	}
	timeout := time.Duration(providerCfg.TimeoutMs) * time.Millisecond
	return handlers.NewHTTPSessionIPIntelligence(providerCfg.BaseURL, providerCfg.APIKey, providerCfg.AuthHeader, timeout)
}

func registerPublicRoutes(r *gin.Engine, deps Dependencies) {
	public := r.Group("/public")
	handlers.RegisterCSATSurveyRoutes(public, handlers.NewCSATSurveyHandler(deps.SatisfactionService))
	handlers.RegisterPublicKnowledgeBaseRoutes(public, handlers.NewKnowledgeDocHandler(deps.KnowledgeDocHandler))
	portalResolver := configscope.NewResolver(
		deps.Config,
		configscope.WithTenantPortalProvider(configscope.NewGormTenantConfigProvider(deps.DB)),
		configscope.WithWorkspacePortalProvider(configscope.NewGormWorkspaceConfigProvider(deps.DB)),
	)
	public.GET("/portal/config", handlers.NewPortalConfigHandlerWithResolver(deps.Config, portalResolver).Get)
}

func registerRealtimeRoutes(r *gin.Engine, deps Dependencies) {
	wsHandler := handlers.NewWebSocketHandler(deps.RealtimeGateway)
	publicV1 := r.Group("/api/v1")
	publicV1.GET("/ws", wsHandler.HandleWebSocket)

	webrtcHandler := handlers.NewWebRTCHandler(deps.RTCGateway)
	messageHandler := handlers.NewMessageHandler(deps.MessageRouter)
	aiHandler := handlers.NewAIHandler(deps.AIHandlerService)

	managementV1 := r.Group("/api/v1")
	managementV1.Use(middleware.AuthMiddleware(deps.Config, authPolicies(deps.DB)...))
	managementV1.Use(middleware.EnforceRequestScope())
	managementV1.Use(middleware.RequirePrincipalKinds("agent", "admin", "service"))
	managementV1.GET("/ws/stats", wsHandler.GetStats)
	managementV1.GET("/webrtc/stats", webrtcHandler.GetStats)
	managementV1.GET("/webrtc/connections", webrtcHandler.GetConnections)
	managementV1.GET("/messages/platforms", messageHandler.GetPlatformStats)
	aiAPI := managementV1.Group("/ai")
	aiAPI.POST("/query", aiHandler.ProcessQuery)
	aiAPI.GET("/status", aiHandler.GetStatus)
	aiAPI.GET("/metrics", aiHandler.GetMetrics)
	// 知识管理路由始终注册，handler内部处理未配置状态
	aiAPI.POST("/knowledge/upload", aiHandler.UploadDocument)
	aiAPI.POST("/knowledge/sync", aiHandler.SyncKnowledgeBase)
	aiAPI.PUT("/knowledge-provider/enable", aiHandler.EnableKnowledgeProvider)
	aiAPI.PUT("/knowledge-provider/disable", aiHandler.DisableKnowledgeProvider)
	aiAPI.POST("/circuit-breaker/reset", aiHandler.ResetCircuitBreaker)

	ingest := handlers.NewMetricsIngestHandler(handlers.NewMetricsAggregator())
	serviceV1 := r.Group("/api/v1")
	serviceV1.Use(middleware.AuthMiddleware(deps.Config, authPolicies(deps.DB)...))
	serviceV1.Use(middleware.EnforceRequestScope())
	serviceV1.Use(middleware.RequirePrincipalKinds("service"))
	serviceV1.POST("/metrics/ingest", ingest.Ingest)
}
