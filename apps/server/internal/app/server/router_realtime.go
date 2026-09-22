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
}
