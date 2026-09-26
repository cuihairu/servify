package server

import (
	"context"
	"errors"
	"fmt"
	pushinfra "servify/apps/server/internal/modules/push/infra"
	"strings"
	"time"

	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	agentinfra "servify/apps/server/internal/modules/agent/infra"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
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
	pushdelivery "servify/apps/server/internal/modules/push/delivery"
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
	translationapp "servify/apps/server/internal/modules/translation/application"
	translationdelivery "servify/apps/server/internal/modules/translation/delivery"
	translationinfra "servify/apps/server/internal/modules/translation/infra"
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
	"servify/apps/server/internal/platform/asr"
	asrfactory "servify/apps/server/internal/platform/asr/factory"
	iceturn "servify/apps/server/internal/platform/iceturn"
	"servify/apps/server/internal/platform/pstnprovider"
	realtimeplatform "servify/apps/server/internal/platform/realtime"
	"servify/apps/server/internal/platform/sip"
	"servify/apps/server/internal/platform/sipws"
	"servify/apps/server/internal/platform/tts"
	ttsfactory "servify/apps/server/internal/platform/tts/factory"
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
	rt.AICopilot = aiAssembly.Copilot
	rt.TranslationHandlerService = aiAssembly.Translation
	// 会话翻译语言偏好（Phase 1 刀一，docs/realtime-translation-design.md）：
	// 存储依赖主库；无 DB 部署形态不装配（端点不注册，与 translate 端点
	// 的 nil 安全口径一致）。
	if rt.DB != nil {
		prefService := translationapp.NewPreferenceService(translationinfra.NewGormPreferenceRepository(rt.DB))
		rt.TranslationPreferenceHandlerService = translationdelivery.NewPreferenceHandlerService(prefService)
		// 刀二/刀三：落库旁路的自动翻译复用同一偏好服务与翻译门面
		// （aiAssembly.Translation 即 HandlerService 门面，结构化满足
		// TranslateInvoker），仅偏好读向不同——hub 消费 agent 读向（访客
		// → 坐席），坐席发送口消费 visitor 读向（坐席 → 访客）。无偏好/
		// provider 未配置由契约静默跳过，错误由消费方记 Warn。
		rt.RealtimeTranslateService = translationdelivery.NewRealtimeTranslateService(aiAssembly.Translation, prefService, translationdelivery.ViewerRoleAgent)
		rt.RealtimeVisitorTranslateService = translationdelivery.NewRealtimeTranslateService(aiAssembly.Translation, prefService, translationdelivery.ViewerRoleVisitor)
		// Phase 1 收尾：工作台历史面批量标注复用同一偏好服务与门面，
		// 消费 agent 读向（坐席读访客消息的译文）。
		rt.HistoryTranslateService = translationdelivery.NewHistoryTranslateService(aiAssembly.Translation, prefService, translationdelivery.ViewerRoleAgent)
		// Phase 2 刀二b-2：语音翻译通道（设计文档 §1.2 分发半边）。
		// ai.asr 非空才装配（路由随之注册）；ai.tts 未配置降级仅字幕
		// 形态（§3.2 合法降级面），其余 factory 错误记 Error 后停用
		// （语音链路可选，不阻断核心启动）。
		recognizer, asrErr := asrfactory.New(rt.Config.AI.ASR)
		switch {
		case asrErr == nil:
			var synth tts.Synthesizer
			s, ttsErr := ttsfactory.New(rt.Config.AI.TTS)
			switch {
			case ttsErr == nil:
				synth = s
			case !errors.Is(ttsErr, tts.ErrNotConfigured):
				rt.Logger.Errorf("voice translation tts disabled (tts factory): %v", ttsErr)
			}
			rt.VoiceTranslationRuntime = translationdelivery.NewVoiceChannelService(aiAssembly.Translation, prefService, recognizer, synth)
		case !errors.Is(asrErr, asr.ErrNotConfigured):
			rt.Logger.Errorf("voice translation disabled (asr factory): %v", asrErr)
		}
	}
	return aiAssembly, nil
}

func wireRealtimeRuntime(rt *Runtime) (*realtimeplatform.WebSocketHub, *realtimeplatform.VoiceHub) {
	wsHub := realtimeplatform.NewWebSocketHub()
	rt.wsRuntime = wsHub
	rt.RealtimeGateway = realtimeplatform.NewWebSocketAdapter(wsHub)
	// 语音翻译通道 hub（Phase 2 刀二b-2）：runtime 由 wireAIRuntime 装配
	// （ai.asr 未配置为 nil = 通道禁用，握手 503 兜底）。
	voiceHub := realtimeplatform.NewVoiceHub()
	voiceHub.SetVoiceTranslationRuntime(rt.VoiceTranslationRuntime)
	rt.voiceHub = voiceHub
	return wsHub, voiceHub
}

func wireConversationRuntime(rt *Runtime, wsHub *realtimeplatform.WebSocketHub, voiceHub *realtimeplatform.VoiceHub) (*conversationdelivery.WebSocketMessageAdapter, error) {
	conversationRepo := conversationinfra.NewGormRepository(rt.DB)
	conversationService := conversationapp.NewService(conversationRepo, rt.Bus).AttachBusinessMetrics(rt.BusinessMetrics)
	rt.ConversationHandler = conversationdelivery.NewHandlerService(conversationService)
	historyAdapter := conversationdelivery.NewWebSocketMessageAdapter(conversationService)
	wsHub.SetConversationMessageWriter(historyAdapter)
	// 开放平台：X-API-Key 只读会话面复用同一 conversation service。
	rt.OpenConversationReader = conversationdelivery.NewOpenConversationAdapter(conversationService)
	// 访客消息增量拉取（M3 §10 #1）：免认证访客端点读入口（会话存在校验 +
	// lastMessageId 单调游标分页），路由挂 /api/v1 免认证链；conversation
	// 服务同源复用（同一 ListMessagesAfter 查询链）。
	rt.VisitorMessagesService = conversationdelivery.NewVisitorMessagesAdapter(conversationService, rt.DB)
	// 访客 token 签发与 WS 握手校验（M3 §10 #2 / D6）：与 jwt.secret 同信任
	// 域自签自验。装配层兜底空白 secret 拒启动（config gate 的第二层）；
	// required 开启时 hub 挂校验闭包，关闭保持既有免校验握手行为。
	guestIssuer, err := conversationdelivery.NewGuestTokenService(rt.Config.JWT.Secret, rt.Config.Security.GuestToken.TTL)
	if err != nil {
		return nil, fmt.Errorf("guest token issuer: %w", err)
	}
	rt.GuestTokenIssuer = guestIssuer
	// 访客未读数/已读游标（M3 §10 #3）：同一 conversation service 与
	// 会话存在性口径（首条消息持久化建行），免认证 REST 面与 §10 #1 同构。
	rt.VisitorReadService = conversationdelivery.NewVisitorReadAdapter(conversationService, rt.DB)
	if rt.Config.Security.GuestToken.Required {
		wsHub.SetTokenValidator(conversationdelivery.NewGuestTokenValidator(rt.Config.JWT.Secret))
		// 语音通道同源校验：同一访客 token 信任域、同一会话绑定语义。
		if voiceHub != nil {
			voiceHub.SetTokenValidator(conversationdelivery.NewGuestTokenValidator(rt.Config.JWT.Secret))
		}
	}
	wireEmailRuntime(rt, conversationService)
	return historyAdapter, nil
}

// attachSessionHistory 把会话历史读取口回填给 AI 面（多轮上下文）：
// scoped 包装器覆盖请求级重建，启动装配的编排实例覆盖 pgvector 等全局
// 路径。conversation service 晚于 AI 装配构建，所以是事后回填而不是
// 构造参数；全部 nil 安全，loader 为 nil 时整体跳过（单轮历史行为）。
func attachSessionHistory(rt *Runtime, aiAssembly *AIAssembly, loader aidelivery.SessionHistoryLoader) {
	if rt == nil || aiAssembly == nil || loader == nil {
		return
	}
	if v, ok := rt.AIService.(*scopedAIRuntimeService); ok {
		v.WithSessionHistory(loader)
	}
	if v, ok := rt.AIHandlerService.(*scopedAIHandlerService); ok {
		v.WithSessionHistory(loader)
	}
	if v, ok := aiAssembly.RuntimeService.(*aidelivery.OrchestratedEnhancedAIService); ok {
		v.WithSessionHistory(loader)
	}
	// 坐席 Copilot 与首答同用一份会话历史口径（suggest_reply/摘要）。
	if rt.AICopilot != nil {
		rt.AICopilot.WithSessionHistory(loader)
	}
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

// wirePushRuntime 按需构建移动端推送下发通道（enabled=false 时不构建任何组件）。
// 双层 gate 兜底层：凭证畸形（服务账号 JSON / .p8 解析失败）在装配处返回
// error 拒绝启动——config.InsecureDefaults 的 warning 面已在 production
// 拒绝缺凭证，这里拦"配了但坏"。双通道都未配凭证时静默不订阅（告警面已提示）。
func wirePushRuntime(rt *Runtime, wsHub *realtimeplatform.WebSocketHub) error {
	cfg := rt.Config.Push
	if !cfg.Enabled {
		return nil
	}
	var fcm *pushinfra.FCMClient
	if raw := strings.TrimSpace(cfg.FCM.CredentialsJSON); raw != "" {
		client, err := pushinfra.NewFCMClient(raw)
		if err != nil {
			return fmt.Errorf("push.fcm: %w", err)
		}
		fcm = client
	}
	var apns *pushinfra.APNsClient
	if strings.TrimSpace(cfg.APNs.PrivateKey) != "" {
		client, err := pushinfra.NewAPNsClient(cfg.APNs.KeyID, cfg.APNs.TeamID, cfg.APNs.BundleID, cfg.APNs.PrivateKey, cfg.APNs.Sandbox)
		if err != nil {
			return fmt.Errorf("push.apns: %w", err)
		}
		apns = client
	}
	if fcm == nil && apns == nil {
		return nil
	}
	NewPushOutboundDispatcher(rt.DB, wsHub, fcm, apns, rt.Logger).Register(rt.Bus)
	return nil
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
	// 刀二：hub 自动翻译旁路（可选；未装配保持纯广播行为）。
	if rt.RealtimeTranslateService != nil {
		wsHub.SetSessionTranslationService(rt.RealtimeTranslateService)
	}
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
	ticketHandler := ticketdelivery.NewHandlerServiceWithDependencies(ticketdelivery.HandlerAssemblyDependencies{
		DB:           rt.DB,
		Logger:       rt.Logger,
		Bus:          rt.Bus,
		SLA:          slaService,
		Satisfaction: satisfactionService,
	}).AttachBusinessMetrics(rt.BusinessMetrics)
	rt.TicketHandlerService = ticketHandler
	rt.TicketReaderService = ticketdelivery.NewReaderServiceAdapter(rt.DB)
	// 访客工单（M3 §10 #4）：与管理面 ticket 装配同源复用（同一 adapter 实现
	// VisitorTicketService 窄接口），路由挂 /api/v1 免认证链。
	rt.VisitorTicketService = ticketHandler

	// 推送注册（M3 §10 #5）：免认证访客端点写入口（编排校验 + 幂等落库），
	// 路由挂 /api/v1 免认证链；下发侧消费按 session 查最新 token（凭证到位后接线）。
	rt.PushRegistrationService = pushdelivery.NewPushRegistrationAdapter(rt.DB)

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
