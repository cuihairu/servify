//go:build !weknora
// +build !weknora

package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	appbootstrap "servify/apps/server/internal/app/bootstrap"
	appserver "servify/apps/server/internal/app/server"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/handlers"
	"servify/apps/server/internal/middleware"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"servify/apps/server/internal/platform/llm/openai"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the servify application",
	Long:  `Run the servify application`,
	Run:   run,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

// run 的测试 seam：默认指向生产实现，生产行为不变。run() 只能在子进程测试
// 里执行，且 setupRouter 的静态路由注册（router.Static("/")）与已注册的
// /health、/api/v1/* 在 gin radix tree 上冲突必然 panic，run() 的错误
// fatal 与收尾段无法从 run() 正常流程触达，子进程测试通过
// CLI_RUN_VARIANT=start-failure（见 cli_shutdown_test.go）注入覆盖。
var (
	startMessageRouter = func(rt *appserver.RealtimeRuntime) error { return rt.Start() }
	shutdownHTTPServer = func(srv *http.Server, ctx context.Context) error { return srv.Shutdown(ctx) }
)

// shutdownRuntime 执行 run() 的收尾段：停消息路由、关 HTTP server、跑
// shutdown hooks。从 run() 抽出成可直接调用的函数，使该段在 gin 静态路由
// 注册 panic 使其从 run() 流程不可达的情况下仍可测试；语句与拆分前逐一致。
func shutdownRuntime(appLogger *logrus.Logger, rt *appserver.RealtimeRuntime, srv *http.Server, app *appbootstrap.App) {
	ctx, cancel := appbootstrap.ShutdownContext(30 * time.Second)
	defer cancel()

	// 停止消息路由
	if err := rt.Stop(ctx); err != nil {
		appLogger.Errorf("Failed to stop message router: %v", err)
	}

	// 关闭服务器
	if err := shutdownHTTPServer(srv, ctx); err != nil {
		appLogger.Errorf("Server forced to shutdown: %v", err)
	}
	if err := app.RunShutdownHooks(); err != nil {
		appLogger.Errorf("Failed to run shutdown hooks: %v", err)
	}

	appLogger.Info("Server exited")
}

func run(cmd *cobra.Command, args []string) {
	cfg, err := appbootstrap.LoadConfig("")
	if err != nil {
		logrus.Fatalf("Failed to load config: %v", err)
	}
	app, err := appbootstrap.BuildApp(cfg)
	if err != nil {
		logrus.Fatalf("Failed to build app: %v", err)
	}
	appLogger := app.Logger
	if err := appbootstrap.SetupObservability(context.Background(), cfg, app); err != nil {
		appLogger.Warnf("init tracing: %v", err)
	}

	db, err := appbootstrap.OpenDatabase(cfg, appbootstrap.DatabaseOptions{})
	if err != nil {
		appLogger.Warnf("DB connect failed, message persistence disabled: %v", err)
	}
	app.DB = db

	openAIProvider := openai.NewProvider(cfg.AI.OpenAI.APIKey, cfg.AI.OpenAI.BaseURL)
	baseAI := aidelivery.NewAIService(cfg.AI.OpenAI.APIKey, cfg.AI.OpenAI.BaseURL)
	baseAI.InitializeKnowledgeBase()
	aiService := aidelivery.NewOrchestratedEnhancedAIService(
		baseAI,
		openAIProvider,
		nil,
		"",
		nil,
		"",
		app.Logger,
	)
	runtime := appserver.BuildRealtimeRuntime(cfg, app.Logger, db, aiService, aidelivery.NewHandlerServiceAdapter(aiService))
	if err := startMessageRouter(runtime); err != nil {
		logrus.Fatalf("Failed to start message router: %v", err)
	}

	// 设置 Gin 模式
	if cfg.Server.Host != "localhost" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建路由
	router := setupRouter(cfg, runtime)

	server := appbootstrap.NewHTTPServer(cfg, router, appbootstrap.HTTPServerOptions{})
	appbootstrap.StartHTTPServer(server, appLogger, fmt.Sprintf("Starting server on %s", server.Addr))
	appbootstrap.WaitForShutdownSignal()

	appLogger.Info("Shutting down server...")

	shutdownRuntime(appLogger, runtime, server, app)
}

func setupRouter(cfg *config.Config, runtime *appserver.RealtimeRuntime) *gin.Engine {
	router := gin.New()

	// 中间件
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(corsMiddlewareWithConfig(cfg))
	router.Use(middleware.RateLimitMiddlewareFromConfig(cfg))
	if cfg.Monitoring.Tracing.Enabled {
		router.Use(otelgin.Middleware(cfg.Monitoring.Tracing.ServiceName))
	}

	// 健康检查
	healthHandler := handlers.NewHealthHandler()
	router.GET("/health", healthHandler.Health)
	router.GET("/ready", healthHandler.Ready)

	// API 路由组
	api := router.Group("/api/v1")
	{
		// WebSocket 连接
		wsHandler := handlers.NewWebSocketHandler(runtime.RealtimeGateway)
		api.GET("/ws", wsHandler.HandleWebSocket)
		api.GET("/ws/stats", wsHandler.GetStats)

		// WebRTC 相关
		webrtcHandler := handlers.NewWebRTCHandler(runtime.RTCGateway)
		api.GET("/webrtc/stats", webrtcHandler.GetStats)
		api.GET("/webrtc/connections", webrtcHandler.GetConnections)

		// 消息路由
		messageHandler := handlers.NewMessageHandler(runtime.MessageRouter)
		api.GET("/messages/platforms", messageHandler.GetPlatformStats)

		// 轻量指标上报（可选）
		ingest := handlers.NewMetricsIngestHandler(handlers.NewMetricsAggregator())
		api.POST("/metrics/ingest", ingest.Ingest)
	}

	// 静态文件服务（尝试多路径）
	staticRoots := []string{
		"./apps/admin",
		"../admin",
		"/app/apps/admin",
	}
	sr := "./apps/admin"
	for _, p := range staticRoots {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			sr = p
			break
		}
	}
	router.Static("/", sr)

	return router
}

func corsMiddlewareWithConfig(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		origins := "*"
		methods := "GET, POST, PUT, DELETE, OPTIONS"
		headers := "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With"
		if cfg != nil && cfg.Security.CORS.Enabled {
			if len(cfg.Security.CORS.AllowedOrigins) > 0 {
				origins = strings.Join(cfg.Security.CORS.AllowedOrigins, ", ")
			}
			if len(cfg.Security.CORS.AllowedMethods) > 0 {
				methods = strings.Join(cfg.Security.CORS.AllowedMethods, ", ")
			}
			if len(cfg.Security.CORS.AllowedHeaders) > 0 {
				headers = strings.Join(cfg.Security.CORS.AllowedHeaders, ", ")
			}
		}
		c.Header("Access-Control-Allow-Origin", origins)
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Headers", headers)
		c.Header("Access-Control-Allow-Methods", methods)
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
