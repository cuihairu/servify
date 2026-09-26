package server

import (
	"context"
	"fmt"
	"net/http"

	"servify/apps/server/internal/config"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	analyticsdelivery "servify/apps/server/internal/modules/analytics/delivery"
	apikeydelivery "servify/apps/server/internal/modules/api_key/delivery"
	appintegrationdelivery "servify/apps/server/internal/modules/app_integration/delivery"
	assistdelivery "servify/apps/server/internal/modules/assist/delivery"
	automationapp "servify/apps/server/internal/modules/automation/application"
	automationdelivery "servify/apps/server/internal/modules/automation/delivery"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	customfielddelivery "servify/apps/server/internal/modules/custom_field/delivery"
	customerdelivery "servify/apps/server/internal/modules/customer/delivery"
	emaildelivery "servify/apps/server/internal/modules/email/delivery"
	emailinfra "servify/apps/server/internal/modules/email/infra"
	gamificationdelivery "servify/apps/server/internal/modules/gamification/delivery"
	knowledgedelivery "servify/apps/server/internal/modules/knowledge/delivery"
	macrodelivery "servify/apps/server/internal/modules/macro/delivery"
	pushdelivery "servify/apps/server/internal/modules/push/delivery"
	qualityapp "servify/apps/server/internal/modules/quality/application"
	qualitydelivery "servify/apps/server/internal/modules/quality/delivery"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	satisfapp "servify/apps/server/internal/modules/satisfaction/application"
	satisfactiondelivery "servify/apps/server/internal/modules/satisfaction/delivery"
	shiftdelivery "servify/apps/server/internal/modules/shift/delivery"
	slapp "servify/apps/server/internal/modules/sla/application"
	sladelivery "servify/apps/server/internal/modules/sla/delivery"
	suggestiondelivery "servify/apps/server/internal/modules/suggestion/delivery"
	ticketdelivery "servify/apps/server/internal/modules/ticket/delivery"
	translationdelivery "servify/apps/server/internal/modules/translation/delivery"
	voicedelivery "servify/apps/server/internal/modules/voice/delivery"
	webhookapp "servify/apps/server/internal/modules/webhook/application"
	webhookdelivery "servify/apps/server/internal/modules/webhook/delivery"
	workspacedelivery "servify/apps/server/internal/modules/workspace/delivery"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	oidcplatform "servify/apps/server/internal/platform/auth/oidc"
	"servify/apps/server/internal/platform/eventbus"
	realtimeplatform "servify/apps/server/internal/platform/realtime"
	"servify/apps/server/internal/platform/voiceprotocol"

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

	AIService                 aidelivery.RuntimeService
	AIHandlerService          aidelivery.HandlerService
	AICopilot                 *aidelivery.AgentCopilotService
	TranslationHandlerService translationdelivery.HandlerService
	// TranslationPreferenceHandlerService 会话翻译语言偏好（Phase 1 刀一）。
	TranslationPreferenceHandlerService translationdelivery.PreferenceHandlerService
	// RealtimeTranslateService 会话消息自动翻译（Phase 1 刀二，hub 消费，
	// agent 读向）。
	RealtimeTranslateService translationdelivery.RealtimeTranslateService
	// RealtimeVisitorTranslateService 坐席消息自动翻译（Phase 1 刀三，坐席
	// 发送口消费，visitor 读向）。
	RealtimeVisitorTranslateService translationdelivery.RealtimeTranslateService
	// HistoryTranslateService 历史消息批量标注（Phase 1 收尾，工作台历史
	// 面消费，agent 读向）。
	HistoryTranslateService translationdelivery.HistoryTranslateService
	// VoiceTranslationRuntime 语音翻译通道（Phase 2 刀二b-2；ai.asr 未配置
	// 为 nil = 通道与路由都不装配）。
	VoiceTranslationRuntime  translationdelivery.VoiceStreamStarter
	wsRuntime                websocketRunner
	voiceHub                 *realtimeplatform.VoiceHub
	RealtimeGateway          realtimeplatform.RealtimeGateway
	RTCGateway               realtimeplatform.RTCGateway
	RTCIceSource             realtimeplatform.ICEConfigSource
	MessageRouter            realtimeplatform.MessageRouterRuntime
	ConversationHandler      conversationdelivery.HandlerService
	VoiceCoordinator         *voicedelivery.Coordinator
	VoiceProtocolRegistry    *voiceprotocol.Registry
	CustomerHandlerService   customerdelivery.HandlerService
	AgentHandlerService      agentdelivery.HandlerService
	AgentGroupService        agentdelivery.AgentGroupService
	TicketHandlerService     ticketdelivery.HandlerService
	VisitorTicketService     ticketdelivery.VisitorTicketService
	PushRegistrationService  pushdelivery.PushRegistrationService
	VisitorMessagesService   conversationdelivery.VisitorMessagesService
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
	GuestTokenIssuer         conversationdelivery.GuestTokenIssuer
	VisitorReadService       conversationdelivery.VisitorReadService
	OIDCProvider             *oidcplatform.Provider
	HTTPMetrics              *svcmetrics.HTTPMetrics
	BusinessMetrics          *svcmetrics.BusinessMetrics

	// Private fields for worker access only
	dailyStatsRunner *analyticsdelivery.DailyStatsRunner
	slaService       *slapp.SLAService
	webhookService   *webhookapp.Service
	qualityService   *qualityapp.QualityService
	emailAdapter     *emaildelivery.Adapter
	smtpSender       *emailinfra.GoSMTPSender
	satisfactionSvc  *satisfapp.SatisfactionService
	transferHandler  *routingdelivery.HandlerServiceAdapter
	automationModule *automationapp.Service
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

	wsHub, voiceHub := wireRealtimeRuntime(rt)
	state.wsHub = wsHub
	historyAdapter, err := wireConversationRuntime(rt, wsHub, voiceHub)
	if err != nil {
		return nil, err
	}
	attachSessionHistory(rt, aiAssembly, historyAdapter)
	if err := wirePushRuntime(rt, wsHub); err != nil {
		return nil, err
	}
	state.routingService = wireRoutingRuntime(rt)

	webrtcService, err := wireRealtimeGateways(rt, wsHub)
	if err != nil {
		return nil, err
	}
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
	if rt.voiceHub != nil {
		go rt.voiceHub.Run()
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

// StatisticsServiceForWorker returns the daily-stats runner for worker use.
func (rt *Runtime) StatisticsServiceForWorker() *analyticsdelivery.DailyStatsRunner {
	return rt.dailyStatsRunner
}

// SLAServiceForWorker returns the SLA monitor for worker use.
func (rt *Runtime) SLAServiceForWorker() slapp.SLAMonitor {
	if rt.slaService == nil {
		return nil
	}
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
func (rt *Runtime) SurveysForWorker() satisfapp.SurveyEmailProcessor {
	return rt.satisfactionSvc
}

// AutomationTimersForWorker returns the delay-timer processor for worker use.
// 返回单一 module 实例（其自身即 TimerProcessor）——事件订阅与 webhook
// dispatcher 都装配在它上面，delay 到期后的 call_webhook 动作依赖它。
func (rt *Runtime) AutomationTimersForWorker() automationapp.TimerProcessor {
	if rt.automationModule == nil {
		return nil
	}
	return rt.automationModule
}

func (rt *Runtime) RouterDependencies() Dependencies {
	return Dependencies{
		Config:                    rt.Config,
		Logger:                    rt.Logger,
		DB:                        rt.DB,
		Redis:                     rt.Redis,
		AIService:                 rt.AIService,
		AIHandlerService:          rt.AIHandlerService,
		AICopilot:                 rt.AICopilot,
		TranslationHandlerService: rt.TranslationHandlerService,

		TranslationPreferenceHandlerService: rt.TranslationPreferenceHandlerService,
		RealtimeTranslateService:            rt.RealtimeTranslateService,
		RealtimeVisitorTranslateService:     rt.RealtimeVisitorTranslateService,
		HistoryTranslateService:             rt.HistoryTranslateService,
		RealtimeGateway:                     rt.RealtimeGateway,
		VoiceTranslationRuntime:             rt.VoiceTranslationRuntime,
		VoiceHub:                            rt.voiceHub,
		RTCGateway:                          rt.RTCGateway,
		RTCIceSource:                        rt.RTCIceSource,
		MessageRouter:                       rt.MessageRouter,
		ConversationHandler:                 rt.ConversationHandler,
		VoiceCoordinator:                    rt.VoiceCoordinator,
		VoiceProtocolRegistry:               rt.VoiceProtocolRegistry,
		CustomerHandlerService:              rt.CustomerHandlerService,
		AgentHandlerService:                 rt.AgentHandlerService,
		AgentGroupService:                   rt.AgentGroupService,
		TicketHandlerService:                rt.TicketHandlerService,
		VisitorTicketService:                rt.VisitorTicketService,
		PushRegistrationService:             rt.PushRegistrationService,
		VisitorMessagesService:              rt.VisitorMessagesService,
		TicketReaderService:                 rt.TicketReaderService,
		TransferHandlerService:              rt.TransferHandlerService,
		SatisfactionService:                 rt.SatisfactionService,
		WorkspaceService:                    rt.WorkspaceService,
		MacroService:                        rt.MacroService,
		AppIntegrationService:               rt.AppIntegrationService,
		CustomFieldService:                  rt.CustomFieldService,
		StatisticsHandlerService:            rt.StatisticsHandlerService,
		SLAService:                          rt.SLAService,
		ShiftService:                        rt.ShiftService,
		AutomationHandlerService:            rt.AutomationHandlerService,
		KnowledgeDocHandler:                 rt.KnowledgeDocHandler,
		SuggestionService:                   rt.SuggestionService,
		GamificationService:                 rt.GamificationService,
		WebhookHandlerService:               rt.WebhookHandlerService,
		QualityHandlerService:               rt.QualityHandlerService,
		AssistHandlerService:                rt.AssistHandlerService,
		APIKeyService:                       rt.APIKeyService,
		OpenConversationReader:              rt.OpenConversationReader,
		GuestTokenIssuer:                    rt.GuestTokenIssuer,
		VisitorReadService:                  rt.VisitorReadService,
		OIDCProvider:                        rt.OIDCProvider,
		HTTPMetrics:                         rt.HTTPMetrics,
	}
}
