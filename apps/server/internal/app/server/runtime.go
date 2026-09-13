package server

import (
	"context"
	"fmt"
	"net/http"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/handlers"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	analyticsdelivery "servify/apps/server/internal/modules/analytics/delivery"
	assistdelivery "servify/apps/server/internal/modules/assist/delivery"
	automationapp "servify/apps/server/internal/modules/automation/application"
	automationdelivery "servify/apps/server/internal/modules/automation/delivery"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	customerdelivery "servify/apps/server/internal/modules/customer/delivery"
	emaildelivery "servify/apps/server/internal/modules/email/delivery"
	emailinfra "servify/apps/server/internal/modules/email/infra"
	gamificationdelivery "servify/apps/server/internal/modules/gamification/delivery"
	knowledgedelivery "servify/apps/server/internal/modules/knowledge/delivery"
	qualityapp "servify/apps/server/internal/modules/quality/application"
	qualitydelivery "servify/apps/server/internal/modules/quality/delivery"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	suggestiondelivery "servify/apps/server/internal/modules/suggestion/delivery"
	ticketdelivery "servify/apps/server/internal/modules/ticket/delivery"
	voicedelivery "servify/apps/server/internal/modules/voice/delivery"
	webhookapp "servify/apps/server/internal/modules/webhook/application"
	webhookdelivery "servify/apps/server/internal/modules/webhook/delivery"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	oidcplatform "servify/apps/server/internal/platform/auth/oidc"
	"servify/apps/server/internal/platform/eventbus"
	realtimeplatform "servify/apps/server/internal/platform/realtime"
	"servify/apps/server/internal/platform/voiceprotocol"
	"servify/apps/server/internal/services"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// Runtime owns the service graph used by the HTTP server.
type Runtime struct {
	Config *config.Config
	Logger *logrus.Logger
	DB     *gorm.DB
	Redis  *redis.Client
	Bus    eventbus.Bus

	AIService                aidelivery.RuntimeService
	AIHandlerService         aidelivery.HandlerService
	wsRuntime                websocketRunner
	RealtimeGateway          realtimeplatform.RealtimeGateway
	RTCGateway               realtimeplatform.RTCGateway
	MessageRouter            services.MessageRouterRuntime
	ConversationHandler      conversationdelivery.HandlerService
	VoiceCoordinator         *voicedelivery.Coordinator
	VoiceProtocolRegistry    *voiceprotocol.Registry
	CustomerHandlerService   customerdelivery.HandlerService
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
	AssistHandlerService     assistdelivery.HandlerService
	APIKeyService            *services.APIKeyService
	OpenConversationReader   conversationdelivery.OpenConversationReader
	OIDCProvider             *oidcplatform.Provider
	HTTPMetrics              *svcmetrics.HTTPMetrics

	// Private fields for worker access only
	statisticsService *services.StatisticsService
	slaService        *services.SLAService
	webhookService    *webhookapp.Service
	qualityService    *qualityapp.QualityService
	emailAdapter      *emaildelivery.Adapter
	smtpSender        *emailinfra.GoSMTPSender
	satisfactionSvc   *services.SatisfactionService
	transferHandler   *routingdelivery.HandlerServiceAdapter
	automationSvc     *services.AutomationService
}

type websocketRunner interface {
	Run()
}

// BuildRuntime wires the current modular-monolith runtime behind an explicit assembly boundary.
func BuildRuntime(cfg *config.Config, logger *logrus.Logger, db *gorm.DB, redisClient *redis.Client, bus eventbus.Bus) (*Runtime, error) {
	rt := &Runtime{
		Config: cfg,
		Logger: logger,
		DB:     db,
		Redis:  redisClient,
		Bus:    bus,
	}

	initializeObservability(rt)

	state := &runtimeAssemblyState{}

	aiAssembly, err := wireAIRuntime(rt)
	if err != nil {
		return nil, err
	}
	state.aiAssembly = aiAssembly

	wsHub := wireRealtimeRuntime(rt)
	state.wsHub = wsHub
	wireConversationRuntime(rt, wsHub)
	state.routingService = wireRoutingRuntime(rt)

	webrtcService := wireRealtimeGateways(rt, wsHub)
	if err := wireVoiceRuntime(rt, webrtcService); err != nil {
		return nil, err
	}

	wireOperationalServices(rt, state)
	wireTransferRuntime(rt, state)

	// OIDC SSO: fail fast on a configured-but-broken provider so a server
	// never comes up advertising SSO it cannot serve.
	if cfg.OIDC.Enabled {
		provider, err := oidcplatform.NewFromConfig(context.Background(), cfg.OIDC, cfg.Server.Environment)
		if err != nil {
			return nil, fmt.Errorf("oidc provider: %w", err)
		}
		rt.OIDCProvider = provider
	}
	return rt, nil
}

func (rt *Runtime) Start() error {
	if rt.wsRuntime != nil {
		go rt.wsRuntime.Run()
	}
	return rt.MessageRouter.Start()
}

func (rt *Runtime) Stop(context.Context) error {
	if rt.MessageRouter == nil {
		return nil
	}
	return rt.MessageRouter.Stop()
}

// Router builds the HTTP router for this runtime.
func (rt *Runtime) Router() http.Handler {
	return BuildRouter(rt.RouterDependencies())
}

// StatisticsServiceForWorker returns the concrete statistics service for worker use.
func (rt *Runtime) StatisticsServiceForWorker() *services.StatisticsService {
	return rt.statisticsService
}

// SLAServiceForWorker returns the concrete SLA service for worker use.
func (rt *Runtime) SLAServiceForWorker() *services.SLAService {
	return rt.slaService
}

// WebhookDeliveryForWorker returns the webhook delivery processor for worker use.
func (rt *Runtime) WebhookDeliveryForWorker() webhookapp.Processor {
	return rt.webhookService
}

// EmailPollAdapterForWorker returns the email poll adapter when the email channel
// is enabled; nil keeps the poll worker unregistered.
func (rt *Runtime) EmailPollAdapterForWorker() emaildelivery.PollProcessor {
	if rt.emailAdapter == nil {
		return nil
	}
	return rt.emailAdapter
}

// QualityScanForWorker returns the quality review scanner when quality review is
// enabled; nil keeps the scan worker unregistered.
func (rt *Runtime) QualityScanForWorker() *qualityapp.QualityService {
	return rt.qualityService
}

// WaitingQueueForWorker returns the waiting-queue dispatcher when auto dispatch is
// enabled; nil keeps the dispatch worker unregistered (manual trigger still works).
func (rt *Runtime) WaitingQueueForWorker() *routingdelivery.HandlerServiceAdapter {
	if !rt.Config.Routing.Enabled {
		return nil
	}
	return rt.transferHandler
}

// SurveysForWorker returns the satisfaction service for the survey email worker.
// Worker 自身对 mailer 为 nil 的服务是无操作（直接返回），无需开关判断。
func (rt *Runtime) SurveysForWorker() *services.SatisfactionService {
	return rt.satisfactionSvc
}

// AutomationTimersForWorker returns the delay-timer processor for worker use.
// 返回门面持有的 module 实例——事件订阅与 webhook dispatcher 都装配在它上面，
// delay 到期后的 call_webhook 动作依赖它。
func (rt *Runtime) AutomationTimersForWorker() automationapp.TimerProcessor {
	if rt.automationSvc == nil {
		return nil
	}
	return rt.automationSvc.TimersForWorker()
}

func (rt *Runtime) RouterDependencies() Dependencies {
	return Dependencies{
		Config:                   rt.Config,
		Logger:                   rt.Logger,
		DB:                       rt.DB,
		Redis:                    rt.Redis,
		AIService:                rt.AIService,
		AIHandlerService:         rt.AIHandlerService,
		RealtimeGateway:          rt.RealtimeGateway,
		RTCGateway:               rt.RTCGateway,
		MessageRouter:            rt.MessageRouter,
		ConversationHandler:      rt.ConversationHandler,
		VoiceCoordinator:         rt.VoiceCoordinator,
		VoiceProtocolRegistry:    rt.VoiceProtocolRegistry,
		CustomerHandlerService:   rt.CustomerHandlerService,
		AgentHandlerService:      rt.AgentHandlerService,
		AgentGroupService:        rt.AgentGroupService,
		TicketHandlerService:     rt.TicketHandlerService,
		TicketReaderService:      rt.TicketReaderService,
		TransferHandlerService:   rt.TransferHandlerService,
		SatisfactionService:      rt.SatisfactionService,
		WorkspaceService:         rt.WorkspaceService,
		MacroService:             rt.MacroService,
		AppIntegrationService:    rt.AppIntegrationService,
		CustomFieldService:       rt.CustomFieldService,
		StatisticsHandlerService: rt.StatisticsHandlerService,
		SLAService:               rt.SLAService,
		ShiftService:             rt.ShiftService,
		AutomationHandlerService: rt.AutomationHandlerService,
		KnowledgeDocHandler:      rt.KnowledgeDocHandler,
		SuggestionService:        rt.SuggestionService,
		GamificationService:      rt.GamificationService,
		WebhookHandlerService:    rt.WebhookHandlerService,
		QualityHandlerService:    rt.QualityHandlerService,
		AssistHandlerService:     rt.AssistHandlerService,
		APIKeyService:            rt.APIKeyService,
		OpenConversationReader:   rt.OpenConversationReader,
		OIDCProvider:             rt.OIDCProvider,
		HTTPMetrics:              rt.HTTPMetrics,
	}
}
