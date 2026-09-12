package server

import (
	"time"

	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	automationdelivery "servify/apps/server/internal/modules/automation/delivery"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	conversationinfra "servify/apps/server/internal/modules/conversation/infra"
	customerdelivery "servify/apps/server/internal/modules/customer/delivery"
	emaildelivery "servify/apps/server/internal/modules/email/delivery"
	emailinfra "servify/apps/server/internal/modules/email/infra"
	gamificationdelivery "servify/apps/server/internal/modules/gamification/delivery"
	knowledgedelivery "servify/apps/server/internal/modules/knowledge/delivery"
	qualitydelivery "servify/apps/server/internal/modules/quality/delivery"
	routingapp "servify/apps/server/internal/modules/routing/application"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	routinginfra "servify/apps/server/internal/modules/routing/infra"
	suggestiondelivery "servify/apps/server/internal/modules/suggestion/delivery"
	ticketdelivery "servify/apps/server/internal/modules/ticket/delivery"
	voiceapp "servify/apps/server/internal/modules/voice/application"
	voicedelivery "servify/apps/server/internal/modules/voice/delivery"
	voiceinfra "servify/apps/server/internal/modules/voice/infra"
	webhookapp "servify/apps/server/internal/modules/webhook/application"
	webhookdelivery "servify/apps/server/internal/modules/webhook/delivery"
	webhookinfra "servify/apps/server/internal/modules/webhook/infra"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	"servify/apps/server/internal/platform/pstnprovider"
	realtimeplatform "servify/apps/server/internal/platform/realtime"
	"servify/apps/server/internal/platform/sip"
	"servify/apps/server/internal/platform/sipws"
	"servify/apps/server/internal/platform/voiceprotocol"
	"servify/apps/server/internal/services"
)

type runtimeAssemblyState struct {
	aiAssembly          *AIAssembly
	wsHub               *services.WebSocketHub
	routingService      *routingapp.Service
	agentAssembly       *services.AgentServiceAssembly
	satisfactionService *services.SatisfactionService
}

func initializeObservability(rt *Runtime) {
	if !rt.Config.Monitoring.Enabled {
		return
	}
	svcmetrics.DefaultRegistry.RegisterGoCollector()
	svcmetrics.DefaultRegistry.RegisterProcessCollector()
	rt.HTTPMetrics = svcmetrics.NewHTTPMetrics(svcmetrics.DefaultRegistry)
}

func wireAIRuntime(rt *Runtime) (*AIAssembly, error) {
	aiAssembly, err := BuildAIAssembly(rt.Config, rt.Logger, AIAssemblyOptions{})
	if err != nil {
		return nil, err
	}
	rt.AIService = NewScopedAIRuntimeService(rt.Config, rt.Logger, rt.DB, aiAssembly.RuntimeService)
	rt.AIHandlerService = NewScopedAIHandlerService(rt.Config, rt.Logger, rt.DB, aiAssembly.Service)
	return aiAssembly, nil
}

func wireRealtimeRuntime(rt *Runtime) *services.WebSocketHub {
	wsHub := services.NewWebSocketHub()
	rt.wsRuntime = wsHub
	rt.RealtimeGateway = realtimeplatform.NewWebSocketAdapter(wsHub)
	return wsHub
}

func wireConversationRuntime(rt *Runtime, wsHub *services.WebSocketHub) {
	conversationRepo := conversationinfra.NewGormRepository(rt.DB)
	conversationService := conversationapp.NewService(conversationRepo, rt.Bus)
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
	rt.emailAdapter = emaildelivery.NewAdapter(emaildelivery.AdapterDeps{
		IMAP:     imapClient,
		SMTP:     smtpSender,
		Ingestor: conversationService,
		From:     smtpFrom,
		Interval: time.Duration(cfg.PollIntervalSeconds) * time.Second,
		Logger:   rt.Logger,
	})
}

func wireRoutingRuntime(rt *Runtime) *routingapp.Service {
	routingRepo := routinginfra.NewGormRepository(rt.DB)
	return routingapp.NewService(routingRepo, rt.Bus)
}

func wireRealtimeGateways(rt *Runtime, wsHub *services.WebSocketHub) *services.WebRTCService {
	webrtcService := services.NewWebRTCService(rt.Config.WebRTC.STUNServer, wsHub)
	wsHub.SetWebRTCService(webrtcService)
	rt.RTCGateway = realtimeplatform.NewWebRTCAdapter(webrtcService)
	rt.MessageRouter = services.NewMessageRouter(rt.AIService, wsHub, rt.DB)
	wsHub.SetAIService(rt.AIService)
	return webrtcService
}

func wireVoiceRuntime(rt *Runtime, webrtcService *services.WebRTCService) error {
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
	_ = rt.VoiceProtocolRegistry.RegisterMedia(voicedelivery.NewWebRTCAdapter(voiceService))
	_ = rt.VoiceProtocolRegistry.RegisterMedia(voicedelivery.NewRTPAdapter())
	_ = rt.VoiceProtocolRegistry.RegisterMedia(voicedelivery.NewSRTPAdapter())
	return nil
}

func wireOperationalServices(rt *Runtime, state *runtimeAssemblyState) {
	slaService := services.NewSLAService(rt.DB, rt.Logger)
	rt.SLAService = slaService
	rt.slaService = slaService

	automationService := services.NewAutomationService(rt.DB, rt.Logger)
	rt.AutomationHandlerService = automationdelivery.NewHandlerService(rt.DB)
	automationService.SetEventBus(rt.Bus)
	slaService.SetAutomationService(automationService)

	rt.CustomerHandlerService = customerdelivery.NewHandlerService(rt.DB)

	agentAssembly := services.BuildAgentServiceAssembly(rt.DB, rt.Logger, rt.Redis)
	rt.AgentHandlerService = agentAssembly.Service
	go agentAssembly.Maintenance.Start()
	state.agentAssembly = agentAssembly

	statisticsService := services.NewStatisticsService(rt.DB, rt.Logger)
	statisticsService.SetEventBus(rt.Bus)
	rt.StatisticsHandlerService = statisticsService
	rt.statisticsService = statisticsService

	satisfactionService := services.NewSatisfactionService(rt.DB, rt.Logger)
	rt.SatisfactionService = satisfactionService
	state.satisfactionService = satisfactionService

	rt.ShiftService = services.NewShiftService(rt.DB, rt.Logger)
	rt.WorkspaceService = services.NewWorkspaceService(rt.DB, agentAssembly.Service)
	rt.MacroService = services.NewMacroService(rt.DB)
	rt.AppIntegrationService = services.NewAppIntegrationService(rt.DB, rt.Logger)
	rt.CustomFieldService = services.NewCustomFieldService(rt.DB)
	rt.KnowledgeDocHandler = knowledgedelivery.NewHandlerServiceWithProvider(rt.DB, state.aiAssembly.KnowledgeProvider(rt.Config))
	rt.SuggestionService = suggestiondelivery.NewHandlerService(rt.DB)
	rt.GamificationService = gamificationdelivery.NewHandlerService(rt.DB)
	rt.TicketHandlerService = ticketdelivery.NewHandlerServiceWithDependencies(ticketdelivery.HandlerAssemblyDependencies{
		DB:           rt.DB,
		Logger:       rt.Logger,
		Bus:          rt.Bus,
		SLA:          slaService,
		Satisfaction: satisfactionService,
	})
	rt.TicketReaderService = ticketdelivery.NewReaderServiceAdapter(rt.DB)

	// 出站 Webhook：订阅 bus 事件入队（同步、只落库），投递与重试全部走后台 worker。
	webhookService := webhookapp.NewService(webhookinfra.NewGormRepository(rt.DB), rt.Logger)
	webhookService.SetDeliverer(webhookinfra.NewHTTPDeliverer())
	webhookdelivery.NewEventBusSubscriber(webhookService).Register(rt.Bus)
	// automation call_webhook 动作复用同一投递器（单次投递 + 审计行）。
	automationService.SetWebhookDispatcher(webhookdelivery.NewAutomationWebhookDispatcher(webhookService))
	rt.webhookService = webhookService
	rt.WebhookHandlerService = webhookdelivery.NewHandlerServiceAdapter(webhookService)

	// 开放平台：API Key 签发/吊销管理（X-API-Key 认证分支在 AuthMiddleware 内）。
	rt.APIKeyService = services.NewAPIKeyService(rt.DB)

	// 质检：规则 + 可选 LLM 打分，扫描走后台 worker（quality.enabled 才装配）。
	rt.qualityService = rt.buildQualityService()
	if rt.qualityService != nil {
		rt.QualityHandlerService = qualitydelivery.NewHandlerServiceAdapter(rt.qualityService)
	}
}
func wireTransferRuntime(rt *Runtime, state *runtimeAssemblyState) {
	transferService := routingdelivery.NewHandlerService(routingdelivery.HandlerDependencies{
		DB:           rt.DB,
		Logger:       rt.Logger,
		AI:           rt.AIService,
		Agents:       state.agentAssembly.Service,
		Notifier:     newRoutingTransferNotifier(rt.RealtimeGateway),
		Routing:      routingdelivery.NewSessionTransferAdapter(state.routingService, rt.Bus),
		Tickets:      ticketdelivery.NewRuntimeAdapter(rt.Bus),
		Conversation: conversationdelivery.NewRuntimeAdapter(rt.DB, rt.Bus),
		AgentLoad:    agentdelivery.NewTransferRuntimeAdapter(),
	})
	rt.TransferHandlerService = transferService
	state.wsHub.SetSessionTransferService(transferService)
}
