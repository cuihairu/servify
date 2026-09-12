package server

import (
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/handlers"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	analyticsdelivery "servify/apps/server/internal/modules/analytics/delivery"
	automationdelivery "servify/apps/server/internal/modules/automation/delivery"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	customerdelivery "servify/apps/server/internal/modules/customer/delivery"
	gamificationdelivery "servify/apps/server/internal/modules/gamification/delivery"
	knowledgedelivery "servify/apps/server/internal/modules/knowledge/delivery"
	qualitydelivery "servify/apps/server/internal/modules/quality/delivery"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	suggestiondelivery "servify/apps/server/internal/modules/suggestion/delivery"
	ticketdelivery "servify/apps/server/internal/modules/ticket/delivery"
	voicedelivery "servify/apps/server/internal/modules/voice/delivery"
	webhookdelivery "servify/apps/server/internal/modules/webhook/delivery"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	oidcplatform "servify/apps/server/internal/platform/auth/oidc"
	realtimeplatform "servify/apps/server/internal/platform/realtime"
	"servify/apps/server/internal/platform/voiceprotocol"
	"servify/apps/server/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
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
	AgentGroupService        agentdelivery.AgentGroupService
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
	SuggestionService        suggestiondelivery.HandlerService
	GamificationService      gamificationdelivery.HandlerService
	WebhookHandlerService    webhookdelivery.HandlerService
	QualityHandlerService    qualitydelivery.HandlerService
	APIKeyService            *services.APIKeyService
	OpenConversationReader   conversationdelivery.OpenConversationReader
	OIDCProvider             *oidcplatform.Provider
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
