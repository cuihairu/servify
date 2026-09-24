package server

import (
	"servify/apps/server/internal/handlers"
	"servify/apps/server/internal/middleware"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	realtimeplatform "servify/apps/server/internal/platform/realtime"

	"github.com/gin-gonic/gin"
)

func registerRealtimeRoutes(r *gin.Engine, deps Dependencies) {
	// WS 建连 Origin 白名单（P2-5 第一刀）：空配置保持放行所有来源。
	realtimeplatform.SetWebsocketAllowedOrigins(deps.Config.Security.WebsocketAllowedOrigins)
	wsHandler := handlers.NewWebSocketHandler(deps.RealtimeGateway)
	publicV1 := r.Group("/api/v1")
	publicV1.GET("/ws", wsHandler.HandleWebSocket)
	// 访客工单创建（M3 移动 SDK 配套 §10 #4）：访客配套 REST 与 WS 同前缀，
	// 按 session 归属租户 scope，坐席侧管理面 tickets 列表自然可见。
	publicV1.POST("/tickets", handlers.NewVisitorTicketHandler(deps.VisitorTicketService, deps.Logger).CreateVisitorTicket)
	// 推送 token 注册（M3 移动 SDK 配套 §10 #5）：SDK connect 后上报
	// FCM/APNs token，按 session 归属租户 scope，同 session+platform 幂等。
	publicV1.POST("/push/register", handlers.NewVisitorPushHandler(deps.PushRegistrationService, deps.Logger).RegisterPushToken)
	// 访客消息增量拉取（M3 移动 SDK 配套 §10 #1）：lastMessageId 游标语义，
	// 与 WS 同 /api/v1 前缀；会话不存在 404 / 游标非法 400。
	publicV1.GET("/sessions/:session_id/messages", handlers.NewVisitorMessagesHandler(deps.VisitorMessagesService, deps.Logger).ListAfter)

	webrtcHandler := handlers.NewWebRTCHandler(deps.RTCGateway)
	messageHandler := handlers.NewMessageHandler(deps.MessageRouter)
	aiHandler := handlers.NewAIHandler(deps.AIHandlerService)

	managementV1 := r.Group("/api/v1")
	managementV1.Use(middleware.AuthMiddleware(deps.Config, deps.DB, authPolicies(deps.DB)...))
	managementV1.Use(middleware.EnforceRequestScope())
	managementV1.Use(middleware.RequirePrincipalKinds("agent", "admin", "service"))
	managementV1.GET("/ws/stats", wsHandler.GetStats)
	managementV1.GET("/webrtc/stats", webrtcHandler.GetStats)
	managementV1.GET("/webrtc/connections", webrtcHandler.GetConnections)
	// ICE 配置下发（docs/TURN_DEPLOYMENT.md 切片三）：与 WS webrtc-ice-config 同形。
	managementV1.GET("/rtc/ice-servers", handlers.NewRTCICEHandler(deps.RTCIceSource).GetIceServers)
	managementV1.GET("/messages/platforms", messageHandler.GetPlatformStats)

	aiAPI := managementV1.Group("/ai")
	aiAPI.POST("/query", aiHandler.ProcessQuery)
	// 坐席 AI 辅助：建议回复 / 一键改写 / 会话摘要（action 分发单端点）。
	aiAPI.POST("/copilot", handlers.NewAICopilotHandler(deps.AICopilot).Copilot)
	aiAPI.GET("/status", aiHandler.GetStatus)
	aiAPI.GET("/metrics", aiHandler.GetMetrics)
	aiAPI.POST("/knowledge/upload", aiHandler.UploadDocument)
	aiAPI.POST("/knowledge/sync", aiHandler.SyncKnowledgeBase)
	aiAPI.PUT("/knowledge-provider/enable", aiHandler.EnableKnowledgeProvider)
	aiAPI.PUT("/knowledge-provider/disable", aiHandler.DisableKnowledgeProvider)
	aiAPI.POST("/circuit-breaker/reset", aiHandler.ResetCircuitBreaker)

	aggregator := handlers.NewMetricsAggregator()
	ingest := handlers.NewMetricsIngestHandler(aggregator)
	// 客户端上报指标桥接进 prometheus /metrics 端点；条件与 health.go 的
	// PrometheusHandler 分支对齐（Monitoring 关闭或降级 JSON handler 时无
	// prometheus 出口，注册没有意义且 MustRegister 不可重入）。
	if deps.Config.Monitoring.Enabled && deps.HTTPMetrics != nil {
		svcmetrics.DefaultRegistry.MustRegister(handlers.NewMetricsPrometheusCollector(aggregator))
	}
	serviceV1 := r.Group("/api/v1")
	serviceV1.Use(middleware.AuthMiddleware(deps.Config, deps.DB, authPolicies(deps.DB)...))
	serviceV1.Use(middleware.EnforceRequestScope())
	serviceV1.Use(middleware.RequirePrincipalKinds("service"))
	serviceV1.POST("/metrics/ingest", ingest.Ingest)
	// 访客 token 签发（M3 移动 SDK 配套 §10 #2 / 设计文档 D6）：宿主后端持
	// service API key 为访客会话换取 WS 握手 token（guest session 面不暴露
	// 在免认证链，签发权收敛在服务信任域内）。
	serviceV1.POST("/guest/session", handlers.NewVisitorGuestSessionHandler(deps.GuestTokenIssuer, deps.Logger).Issue)
}
