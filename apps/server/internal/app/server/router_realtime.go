package server

import (
	"servify/apps/server/internal/handlers"
	"servify/apps/server/internal/middleware"
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
	managementV1.GET("/messages/platforms", messageHandler.GetPlatformStats)

	aiAPI := managementV1.Group("/ai")
	aiAPI.POST("/query", aiHandler.ProcessQuery)
	aiAPI.GET("/status", aiHandler.GetStatus)
	aiAPI.GET("/metrics", aiHandler.GetMetrics)
	aiAPI.POST("/knowledge/upload", aiHandler.UploadDocument)
	aiAPI.POST("/knowledge/sync", aiHandler.SyncKnowledgeBase)
	aiAPI.PUT("/knowledge-provider/enable", aiHandler.EnableKnowledgeProvider)
	aiAPI.PUT("/knowledge-provider/disable", aiHandler.DisableKnowledgeProvider)
	aiAPI.POST("/circuit-breaker/reset", aiHandler.ResetCircuitBreaker)

	ingest := handlers.NewMetricsIngestHandler(handlers.NewMetricsAggregator())
	serviceV1 := r.Group("/api/v1")
	serviceV1.Use(middleware.AuthMiddleware(deps.Config, deps.DB, authPolicies(deps.DB)...))
	serviceV1.Use(middleware.EnforceRequestScope())
	serviceV1.Use(middleware.RequirePrincipalKinds("service"))
	serviceV1.POST("/metrics/ingest", ingest.Ingest)
}
