package server

import (
	"context"
	"fmt"
	"time"

	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	agentinfra "servify/apps/server/internal/modules/agent/infra"
	analyticsapp "servify/apps/server/internal/modules/analytics/application"
	analyticsdelivery "servify/apps/server/internal/modules/analytics/delivery"
	analyticsinfra "servify/apps/server/internal/modules/analytics/infra"
	apikeyapp "servify/apps/server/internal/modules/api_key/application"
	apikeydelivery "servify/apps/server/internal/modules/api_key/delivery"
	apikeyinfra "servify/apps/server/internal/modules/api_key/infra"
	appintegrationapp "servify/apps/server/internal/modules/app_integration/application"
	appintegrationdelivery "servify/apps/server/internal/modules/app_integration/delivery"
	appintegrationinfra "servify/apps/server/internal/modules/app_integration/infra"
	assistapp "servify/apps/server/internal/modules/assist/application"
	assistdelivery "servify/apps/server/internal/modules/assist/delivery"
	assistinfra "servify/apps/server/internal/modules/assist/infra"
	automationapp "servify/apps/server/internal/modules/automation/application"
	automationdelivery "servify/apps/server/internal/modules/automation/delivery"
	automationinfra "servify/apps/server/internal/modules/automation/infra"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	conversationinfra "servify/apps/server/internal/modules/conversation/infra"
	customfieldapp "servify/apps/server/internal/modules/custom_field/application"
	customfielddelivery "servify/apps/server/internal/modules/custom_field/delivery"
	customfieldinfra "servify/apps/server/internal/modules/custom_field/infra"
	customerdelivery "servify/apps/server/internal/modules/customer/delivery"
	emaildelivery "servify/apps/server/internal/modules/email/delivery"
	emailinfra "servify/apps/server/internal/modules/email/infra"
	gamificationdelivery "servify/apps/server/internal/modules/gamification/delivery"
	knowledgedelivery "servify/apps/server/internal/modules/knowledge/delivery"
	macroapp "servify/apps/server/internal/modules/macro/application"
	macrodelivery "servify/apps/server/internal/modules/macro/delivery"
	macroinfra "servify/apps/server/internal/modules/macro/infra"
	qualitydelivery "servify/apps/server/internal/modules/quality/delivery"
	routingapp "servify/apps/server/internal/modules/routing/application"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	routinginfra "servify/apps/server/internal/modules/routing/infra"
	satisfapp "servify/apps/server/internal/modules/satisfaction/application"
	shiftapp "servify/apps/server/internal/modules/shift/application"
	shiftdelivery "servify/apps/server/internal/modules/shift/delivery"
	shiftinfra "servify/apps/server/internal/modules/shift/infra"
	slapp "servify/apps/server/internal/modules/sla/application"
	suggestionapp "servify/apps/server/internal/modules/suggestion/application"
	suggestiondelivery "servify/apps/server/internal/modules/suggestion/delivery"
	suggestioninfra "servify/apps/server/internal/modules/suggestion/infra"
	ticketdelivery "servify/apps/server/internal/modules/ticket/delivery"
	voiceapp "servify/apps/server/internal/modules/voice/application"
	voicedelivery "servify/apps/server/internal/modules/voice/delivery"
	voiceinfra "servify/apps/server/internal/modules/voice/infra"
	webhookapp "servify/apps/server/internal/modules/webhook/application"
	webhookdelivery "servify/apps/server/internal/modules/webhook/delivery"
	webhookinfra "servify/apps/server/internal/modules/webhook/infra"
	workspaceapp "servify/apps/server/internal/modules/workspace/application"
	workspacedelivery "servify/apps/server/internal/modules/workspace/delivery"
	workspaceinfra "servify/apps/server/internal/modules/workspace/infra"
	svcerrors "servify/apps/server/internal/observability/errors"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	iceturn "servify/apps/server/internal/platform/iceturn"
	"servify/apps/server/internal/platform/pstnprovider"
	realtimeplatform "servify/apps/server/internal/platform/realtime"
	"servify/apps/server/internal/platform/sip"
	"servify/apps/server/internal/platform/sipws"
	twiliovoice "servify/apps/server/internal/platform/twiliovoice"
	"servify/apps/server/internal/platform/voiceprotocol"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type runtimeAssemblyState struct {
	aiAssembly          *AIAssembly
	wsHub               *realtimeplatform.WebSocketHub
	routingService      *routingapp.Service
	agentAdapter        *agentdelivery.HandlerServiceAdapter
	satisfactionService *satisfapp.SatisfactionService
}

func initializeObservability(rt *Runtime) {
	if !rt.Config.Monitoring.Enabled {
		return
	}
	svcmetrics.DefaultRegistry.RegisterGoCollector()
	svcmetrics.DefaultRegistry.RegisterProcessCollector()
	// errors_total 统一出口：先注册计数器，registerBaseMiddleware 才能挂
	// StatusMiddleware 对全部路由的 5xx 打点。
	svcerrors.RegisterErrorMetrics(svcmetrics.DefaultRegistry)
	rt.HTTPMetrics = svcmetrics.NewHTTPMetrics(svcmetrics.DefaultRegistry)
	rt.BusinessMetrics = svcmetrics.NewBusinessMetrics(svcmetrics.DefaultRegistry)
}

func wireAIRuntime(rt *Runtime) (*AIAssembly, error) {
	aiAssembly, err := BuildAIAssembly(rt.Config, rt.Logger, AIAssemblyOptions{DB: rt.DB})
	if err != nil {
		return nil, err
	}
	rt.AIService = NewScopedAIRuntimeService(rt.Config, rt.Logger, rt.DB, aiAssembly.RuntimeService, rt.BusinessMetrics)
	rt.AIHandlerService = NewScopedAIHandlerService(rt.Config, rt.Logger, rt.DB, aiAssembly.Service, aiAssembly.RuntimeService, rt.BusinessMetrics)
	return aiAssembly, nil
}

func wireRealtimeRuntime(rt *Runtime) *realtimeplatform.WebSocketHub {
	wsHub := realtimeplatform.NewWebSocketHub()
	rt.wsRuntime = wsHub
	rt.RealtimeGateway = realtimeplatform.NewWebSocketAdapter(wsHub)
	return wsHub
}

func wireConversationRuntime(rt *Runtime, wsHub *realtimeplatform.WebSocketHub) {
	conversationRepo := conversationinfra.NewGormRepository(rt.DB)
	conversationService := conversationapp.NewService(conversationRepo, rt.Bus).AttachBusinessMetrics(rt.BusinessMetrics)
	rt.ConversationHandler = conversationdelivery.NewHandlerService(conversationService)
	wsHub.SetConversationMessageWriter(conversationdelivery.NewWebSocketMessageAdapter(conversationService))
	// 开放平台：X-API-Key 只读会话面复用同一 conversation service。
	rt.OpenConversationReader = conversationdelivery.NewOpenConversationAdapter(conversationService)
	wireEmailRuntime(rt, conversationService)
}

// wireEmailRuntime 按需构建单邮箱 IMAP 渠道（enabled=false 时不构建任何组件）。
func wireEmailRuntime(rt *Runtime, conversationService *conversationapp.Service) {
	cfg := rt.Config.Email
	if !cfg.Enabled {
		return
	}
	imapClient := emailinfra.NewGoIMAPClient(cfg, rt.Logger)
	smtpFrom := cfg.SMTP.From
	if smtpFrom == "" {
		smtpFrom = cfg.Username
	}
	smtpSender := emailinfra.NewGoSMTPSender(emailinfra.SMTPConfig{
		Host:        cfg.SMTP.Host,
		Port:        cfg.SMTP.Port,
		Username:    cfg.SMTP.Username,
		Password:    cfg.SMTP.Password,
		From:        smtpFrom,
		UseSTARTTLS: cfg.SMTP.UseSTARTTLS,
		SkipVerify:  cfg.SMTP.SkipVerify,
	})
	rt.smtpSender = smtpSender
	rt.emailAdapter = emaildelivery.NewAdapter(emaildelivery.AdapterDeps{
		IMAP:     imapClient,
		SMTP:     smtpSender,
		Ingestor: conversationService,
		From:     smtpFrom,
		Interval: time.Duration(cfg.PollIntervalSeconds) * time.Second,
		Logger:   rt.Logger,
	})

	// 坐席回复按会话渠道出站：email 渠道启用时把 agent 消息回发访客邮箱
	NewChannelOutboundDispatcher(rt.DB, rt.emailAdapter, rt.Logger).Register(rt.Bus)
}

func wireRoutingRuntime(rt *Runtime) *routingapp.Service {
	routingRepo := routinginfra.NewGormRepository(rt.DB)
	return routingapp.NewService(routingRepo, rt.Bus).AttachBusinessMetrics(rt.BusinessMetrics)
}

func wireRealtimeGateways(rt *Runtime, wsHub *realtimeplatform.WebSocketHub) (*realtimeplatform.WebRTCService, error) {
	turn := iceturn.Config{
		URL:              rt.Config.WebRTC.TURN.URL,
		Realm:            rt.Config.WebRTC.TURN.Realm,
		StaticAuthSecret: rt.Config.WebRTC.TURN.StaticAuthSecret,
		TTL:              rt.Config.WebRTC.TURN.TTL,
	}
	// 装配层兜底 gate：与 config.InsecureDefaults 的零容忍告警成对（双层 gate）。
	if err := turn.Validate(); err != nil {
		return nil, fmt.Errorf("webrtc turn: %w", err)
	}
	ice := iceturn.Assemble(rt.Config.WebRTC.STUNServers, rt.Config.WebRTC.STUNServer, turn.WithDefaults(), time.Now())
	rt.Logger.Infof("WebRTC ICE assembled: %s", ice.Describe())
	webrtcService := realtimeplatform.NewWebRTCService(ice, wsHub)
	wsHub.SetWebRTCService(webrtcService)
	rt.RTCGateway = realtimeplatform.NewWebRTCAdapter(webrtcService)
	// REST 下发口与 WS 推送消费同一份装配结果（ICEConfigSource 由 *WebRTCService 实现）。
	rt.RTCIceSource = webrtcService
	rt.MessageRouter = realtimeplatform.NewMessageRouter(rt.AIService, wsHub, rt.DB)
	wsHub.SetAIService(rt.AIService)
	return webrtcService, nil
}

func wireVoiceRuntime(rt *Runtime, webrtcService *realtimeplatform.WebRTCService) error {
	voiceService := voiceapp.NewService(voiceinfra.NewGormRepository(rt.DB), rt.Bus)
	recordingProvider, err := buildVoiceRecordingProvider(rt.Config, rt.Logger)
	if err != nil {
		return err
	}
	transcriptProvider, err := buildVoiceTranscriptProvider(rt.Config, rt.Logger)
	if err != nil {
		return err
	}

	recordingService := voiceapp.NewRecordingService(
		recordingProvider,
		voiceinfra.NewGormRecordingRepository(rt.DB),
		rt.Bus,
	)
	transcriptService := voiceapp.NewTranscriptService(
		transcriptProvider,
		voiceinfra.NewGormTranscriptRepository(rt.DB),
		rt.Bus,
	)

	rt.VoiceCoordinator = voicedelivery.NewCoordinator(voiceService, recordingService, transcriptService)
	webrtcService.SetVoiceLifecycle(rt.VoiceCoordinator)
	rt.VoiceProtocolRegistry = voiceprotocol.NewRegistry()
	_ = rt.VoiceProtocolRegistry.RegisterSignaling(sip.NewVoiceProtocolAdapter())
	_ = rt.VoiceProtocolRegistry.RegisterSignaling(sipws.NewAdapter())
	_ = rt.VoiceProtocolRegistry.RegisterSignaling(pstnprovider.NewAdapter())
	// Hosted PSTN ingress: only mounted when explicitly configured; a missing
	// auth token is a startup error rather than a silently unsigned webhook.
	if rt.Config.Voice.PSTN.Provider == "twilio" {
		if rt.Config.Voice.Twilio.AuthToken == "" {
			return fmt.Errorf("voice.pstn.provider is twilio but voice.twilio.auth_token is empty")
		}
		// RegisterSignaling 仅在 registry/adapter 为 nil 时报错；registry
		// 刚创建、NewAdapter 恒返回非 nil，与上方三处注册同样忽略返回值。
		_ = rt.VoiceProtocolRegistry.RegisterSignaling(twiliovoice.NewAdapter(rt.Config.Voice.Twilio.AuthToken))
	}
	_ = rt.VoiceProtocolRegistry.RegisterMedia(voicedelivery.NewWebRTCAdapter(voiceService))
	_ = rt.VoiceProtocolRegistry.RegisterMedia(voicedelivery.NewRTPAdapter())
	_ = rt.VoiceProtocolRegistry.RegisterMedia(voicedelivery.NewSRTPAdapter())
	return nil
}

// newAgentRegistry 按 Redis 可用性选择坐席运行态 registry：
// 有 Redis 走多实例共享，否则退化为单机内存态。
func newAgentRegistry(db *gorm.DB, redisClient *redis.Client, logger *logrus.Logger) agentapp.RuntimeRegistry {
	if redisClient != nil {
		logger.Info("using redis-backed agent registry for multi-instance support")
		return agentinfra.NewRedisRegistry(redisClient, db, logger)
	}
	logger.Warn("using in-memory agent registry - not suitable for multi-instance deployment")
	return agentinfra.NewInMemoryRegistry()
}

func wireOperationalServices(rt *Runtime, state *runtimeAssemblyState) {
	slaService := slapp.NewService(rt.DB, rt.Logger)
	rt.SLAService = slaService
	rt.slaService = slaService

	// automation：单一 module 实例贯穿 HTTP/eventbus/SLA/timer worker，
	// 避免 facade 与 delivery 各持一份实例的 split-brain。
	autoModule := automationapp.NewService(automationinfra.NewGormRepository(rt.DB))
	rt.AutomationHandlerService = automationdelivery.NewHandlerServiceAdapter(autoModule)
	automationdelivery.NewEventBusSubscriber(autoModule).Register(rt.Bus)
	slaService.SetAutomationModule(autoModule)
	rt.automationModule = autoModule
	autoModule.SetTimerBatchSize(rt.Config.Automation.TimerBatchSize)

	rt.CustomerHandlerService = customerdelivery.NewHandlerService(rt.DB)

	// 远程协助：会话/录制/标注（媒体面走既有 WS+RTC，不在此装配）
	rt.AssistHandlerService = assistdelivery.NewHandlerService(
		assistapp.NewAssistService(assistinfra.NewGormRepository(rt.DB)))

	// agent：单一 module 实例贯穿 HTTP/组管理/routing 转接/workspace。
	agentRepo := agentinfra.NewGormRepository(rt.DB)
	agentRegistry := newAgentRegistry(rt.DB, rt.Redis, rt.Logger)
	agentModule := agentapp.NewService(agentRepo, agentRegistry)
	agentAdapter := agentdelivery.NewHandlerServiceAdapter(agentModule, rt.Logger)
	rt.AgentHandlerService = agentAdapter
	rt.AgentGroupService = agentAdapter
	go agentdelivery.NewRuntimeMaintenance(rt.Logger, agentModule).Start(context.Background())
	state.agentAdapter = agentAdapter

	// statistics：单一 module 实例贯穿 HTTP/eventbus/每日统计 worker。
	analyticsModule := analyticsapp.NewService(analyticsinfra.NewGormRepository(rt.DB))
	rt.StatisticsHandlerService = analyticsdelivery.NewHandlerServiceAdapter(analyticsModule)
	analyticsdelivery.NewEventBusSubscriber(analyticsModule).Register(rt.Bus)
	rt.dailyStatsRunner = analyticsdelivery.NewDailyStatsRunner(analyticsModule, rt.Logger)

	satisfactionService := satisfapp.NewService(rt.DB, rt.Logger)
	rt.SatisfactionService = satisfactionService
	rt.satisfactionSvc = satisfactionService
	state.satisfactionService = satisfactionService
	// CSAT 邮件：评分链接基地址 + email 渠道启用时注入 SMTP 投递器
	//（mailer 未注入时 ScheduleSurvey 维持旧行为：直接置 sent）。
	satisfactionService.SetSurveyLinkBaseURL(rt.Config.Server.PublicBaseURL)
	if rt.smtpSender != nil {
		smtpFrom := rt.Config.Email.SMTP.From
		if smtpFrom == "" {
			smtpFrom = rt.Config.Email.Username
		}
		satisfactionService.SetSurveyMailer(&surveyEmailMailer{sender: rt.smtpSender, from: smtpFrom})
	}

	shiftModule := shiftapp.NewService(shiftinfra.NewGormRepository(rt.DB))
	rt.ShiftService = shiftdelivery.NewHandlerServiceAdapter(shiftModule)
	workspaceModule := workspaceapp.NewService(workspaceinfra.NewGormRepository(rt.DB), agentAdapter)
	rt.WorkspaceService = workspacedelivery.NewHandlerServiceAdapter(workspaceModule)
	macroModule := macroapp.NewService(macroinfra.NewGormRepository(rt.DB))
	rt.MacroService = macrodelivery.NewHandlerServiceAdapter(macroModule)
	appIntegrationModule := appintegrationapp.NewService(appintegrationinfra.NewGormRepository(rt.DB))
	rt.AppIntegrationService = appintegrationdelivery.NewHandlerServiceAdapter(appIntegrationModule)
	customFieldModule := customfieldapp.NewService(customfieldinfra.NewGormRepository(rt.DB))
	rt.CustomFieldService = customfielddelivery.NewHandlerServiceAdapter(customFieldModule)
	rt.KnowledgeDocHandler = knowledgedelivery.NewHandlerServiceWithProvider(rt.DB, state.aiAssembly.KnowledgeProvider(rt.Config))
	// 客户侧推荐（P2-0 RQ-5）：module 单实例贯穿 REST 管理面与 WS 转化归因，
	// 曝光落库与转化匹配读同一仓储；未注入 hub 时归因自动跳过。
	suggestionModule := suggestionapp.NewService(suggestioninfra.NewGormRepository(rt.DB))
	rt.SuggestionService = suggestiondelivery.NewHandlerServiceAdapter(suggestionModule)
	if state.wsHub != nil {
		state.wsHub.SetSuggestionConversionService(suggestionModule)
	}
	rt.GamificationService = gamificationdelivery.NewHandlerService(rt.DB)
	rt.TicketHandlerService = ticketdelivery.NewHandlerServiceWithDependencies(ticketdelivery.HandlerAssemblyDependencies{
		DB:           rt.DB,
		Logger:       rt.Logger,
		Bus:          rt.Bus,
		SLA:          slaService,
		Satisfaction: satisfactionService,
	}).AttachBusinessMetrics(rt.BusinessMetrics)
	rt.TicketReaderService = ticketdelivery.NewReaderServiceAdapter(rt.DB)

	// 出站 Webhook：订阅 bus 事件入队（同步、只落库），投递与重试全部走后台 worker。
	webhookService := webhookapp.NewService(webhookinfra.NewGormRepository(rt.DB), rt.Logger)
	webhookService.SetDeliverer(webhookinfra.NewHTTPDeliverer())
	webhookdelivery.NewEventBusSubscriber(webhookService).Register(rt.Bus)
	// automation call_webhook 动作复用同一投递器（单次投递 + 审计行）。
	autoModule.SetWebhookDispatcher(webhookdelivery.NewAutomationWebhookDispatcher(webhookService))
	rt.webhookService = webhookService
	rt.WebhookHandlerService = webhookdelivery.NewHandlerServiceAdapter(webhookService)

	// 开放平台：API Key 签发/吊销管理（X-API-Key 认证分支在 AuthMiddleware 内）。
	apiKeyModule := apikeyapp.NewService(apikeyinfra.NewGormRepository(rt.DB))
	rt.APIKeyService = apikeydelivery.NewHandlerServiceAdapter(apiKeyModule)

	// 质检：规则 + 可选 LLM 打分，扫描走后台 worker（quality.enabled 才装配）。
	rt.qualityService = rt.buildQualityService()
	if rt.qualityService != nil {
		rt.QualityHandlerService = qualitydelivery.NewHandlerServiceAdapter(rt.qualityService)
	}
}
func wireTransferRuntime(rt *Runtime, state *runtimeAssemblyState) {
	transferService := routingdelivery.NewHandlerService(routingdelivery.HandlerDependencies{
		DB:                rt.DB,
		Logger:            rt.Logger,
		AI:                rt.AIService,
		Agents:            state.agentAdapter,
		Notifier:          newRoutingTransferNotifier(rt.RealtimeGateway),
		Routing:           routingdelivery.NewSessionTransferAdapter(state.routingService, rt.Bus),
		Tickets:           ticketdelivery.NewRuntimeAdapter(rt.Bus),
		Conversation:      conversationdelivery.NewRuntimeAdapter(rt.DB, rt.Bus),
		AgentLoad:         agentdelivery.NewTransferRuntimeAdapter(),
		DispatchBatchSize: rt.Config.Routing.DispatchBatchSize,
		ClaimLeaseSeconds: rt.Config.Routing.ClaimLeaseSeconds,
	})
	rt.TransferHandlerService = transferService
	rt.transferHandler = transferService
	state.wsHub.SetSessionTransferService(transferService)
}
