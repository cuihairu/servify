package server

import (
	"servify/apps/server/internal/config"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	analyticsdelivery "servify/apps/server/internal/modules/analytics/delivery"
	apikeydelivery "servify/apps/server/internal/modules/api_key/delivery"
	appintegrationdelivery "servify/apps/server/internal/modules/app_integration/delivery"
	assistdelivery "servify/apps/server/internal/modules/assist/delivery"
	automationdelivery "servify/apps/server/internal/modules/automation/delivery"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	customfielddelivery "servify/apps/server/internal/modules/custom_field/delivery"
	customerdelivery "servify/apps/server/internal/modules/customer/delivery"
	gamificationdelivery "servify/apps/server/internal/modules/gamification/delivery"
	knowledgedelivery "servify/apps/server/internal/modules/knowledge/delivery"
	macrodelivery "servify/apps/server/internal/modules/macro/delivery"
	pushdelivery "servify/apps/server/internal/modules/push/delivery"
	qualitydelivery "servify/apps/server/internal/modules/quality/delivery"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	satisfactiondelivery "servify/apps/server/internal/modules/satisfaction/delivery"
	shiftdelivery "servify/apps/server/internal/modules/shift/delivery"
	sladelivery "servify/apps/server/internal/modules/sla/delivery"
	suggestiondelivery "servify/apps/server/internal/modules/suggestion/delivery"
	ticketdelivery "servify/apps/server/internal/modules/ticket/delivery"
	voicedelivery "servify/apps/server/internal/modules/voice/delivery"
	webhookdelivery "servify/apps/server/internal/modules/webhook/delivery"
	workspacedelivery "servify/apps/server/internal/modules/workspace/delivery"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	oidcplatform "servify/apps/server/internal/platform/auth/oidc"
	realtimeplatform "servify/apps/server/internal/platform/realtime"
	"servify/apps/server/internal/platform/voiceprotocol"

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
	AICopilot                *aidelivery.AgentCopilotService
	RealtimeGateway          realtimeplatform.RealtimeGateway
	RTCGateway               realtimeplatform.RTCGateway
	RTCIceSource             realtimeplatform.ICEConfigSource
	MessageRouter            realtimeplatform.MessageRouterRuntime
	VoiceCoordinator         *voicedelivery.Coordinator
	VoiceProtocolRegistry    *voiceprotocol.Registry
	CustomerHandlerService   customerdelivery.HandlerService
	ConversationHandler      conversationdelivery.HandlerService
	AgentHandlerService      agentdelivery.HandlerService
	AgentGroupService        agentdelivery.AgentGroupService
	TicketHandlerService     ticketdelivery.HandlerService
	VisitorTicketService     ticketdelivery.VisitorTicketService
	PushRegistrationService  pushdelivery.PushRegistrationService
	TicketReaderService      *ticketdelivery.ReaderServiceAdapter
	TransferHandlerService   routingdelivery.HandlerService
	SatisfactionService      satisfactiondelivery.SatisfactionService
	WorkspaceService         workspacedelivery.HandlerService
	MacroService             macrodelivery.HandlerService
	AppIntegrationService    appintegrationdelivery.HandlerService
	CustomFieldService       customfielddelivery.HandlerService
	StatisticsHandlerService analyticsdelivery.HandlerService
	SLAService               sladelivery.SLAService
	ShiftService             shiftdelivery.HandlerService
	AutomationHandlerService automationdelivery.HandlerService
	KnowledgeDocHandler      knowledgedelivery.HandlerService
	SuggestionService        suggestiondelivery.HandlerService
	GamificationService      gamificationdelivery.HandlerService
	WebhookHandlerService    webhookdelivery.HandlerService
	QualityHandlerService    qualitydelivery.HandlerService
	AssistHandlerService     assistdelivery.HandlerService
	APIKeyService            apikeydelivery.HandlerService
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
	registerStatic(r, deps.Config)
	if deps.Logger != nil {
		for _, warning := range routeSecurityWarnings(r.Routes(), deps.Config) {
			deps.Logger.Warnf("security surface warning: %s", warning)
		}
	}
	return r
}
