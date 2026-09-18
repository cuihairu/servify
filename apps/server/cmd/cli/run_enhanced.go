//go:build weknora
// +build weknora

package cli

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	appbootstrap "servify/apps/server/internal/app/bootstrap"
	appserver "servify/apps/server/internal/app/server"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/handlers"
	"servify/apps/server/pkg/weknora"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

type gormDBStatsProvider struct {
	db *gorm.DB
}

func newGormDBStatsProvider(db *gorm.DB) *gormDBStatsProvider {
	if db == nil {
		return nil
	}
	return &gormDBStatsProvider{db: db}
}

func (p *gormDBStatsProvider) Stats() (sql.DBStats, bool) {
	if p == nil || p.db == nil {
		return sql.DBStats{}, false
	}
	sqlDB, err := p.db.DB()
	if err != nil || sqlDB == nil {
		return sql.DBStats{}, false
	}
	return sqlDB.Stats(), true
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the servify application with WeKnora integration",
	Long:  `Run the servify application with enhanced AI capabilities powered by WeKnora`,
	Run:   run,
}

func init() {
	rootCmd.AddCommand(runCmd)
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

	appLogger.Info("🚀 Starting Servify with WeKnora integration...")

	db, err := appbootstrap.OpenDatabase(cfg, appbootstrap.DatabaseOptions{})
	if err != nil {
		appLogger.Warnf("DB connect failed, message persistence disabled: %v", err)
	}
	app.DB = db

	appLogger.Info("📚 Initializing AI assembly...")
	aiAssembly, err := appserver.BuildAIAssembly(cfg, app.Logger, appserver.AIAssemblyOptions{
		RequireWeKnoraHealthy: false,
		SyncKnowledgeBase:     cfg.Upload.AutoIndex,
		HealthCheckTimeout:    10 * time.Second,
		DB:                    db,
	})
	if err != nil {
		appLogger.Fatalf("❌ Failed to initialize AI assembly: %v", err)
	}
	weKnoraClient := aiAssembly.WeKnoraClient
	aiService := aiAssembly.RuntimeService
	aiHandlerService := aiAssembly.Service
	if aiAssembly.WeKnoraHealthy {
		appLogger.Info("✅ Enhanced AI service with WeKnora initialized")
	} else if cfg.WeKnora.Enabled {
		appLogger.Warn("🔄 WeKnora unavailable, using fallback AI assembly")
	} else {
		appLogger.Info("✅ Standard AI service initialized")
	}

	runtime := appserver.BuildRealtimeRuntime(cfg, app.Logger, db, aiService, aiHandlerService)
	appLogger.Info("🔌 Starting background services...")
	if err := runtime.Start(); err != nil {
		appLogger.Fatalf("❌ Failed to start message router: %v", err)
	}

	// 设置 Gin 模式
	if cfg.Log.Level == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建路由
	router := setupEnhancedRouter(cfg, runtime, db)

	// 创建服务器
	server := appbootstrap.NewHTTPServer(cfg, router, appbootstrap.HTTPServerOptions{
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	})
	appbootstrap.StartHTTPServer(server, appLogger, fmt.Sprintf("🌐 Server starting on %s", server.Addr))
	appLogger.Infof("📍 Web UI: http://%s", server.Addr)
	appLogger.Infof("🔗 API Base: http://%s/api/v1", server.Addr)
	appLogger.Infof("🔌 WebSocket: ws://%s/api/v1/ws", server.Addr)
	if cfg.WeKnora.Enabled {
		appLogger.Infof("📚 WeKnora: %s", cfg.WeKnora.BaseURL)
	}

	// 启动健康检查（如果启用）
	if cfg.Monitoring.Enabled {
		go startHealthMonitoring(cfg, weKnoraClient)
	}

	appbootstrap.WaitForShutdownSignal()

	appLogger.Info("🛑 Shutting down server...")

	// 优雅关闭
	ctx, cancel := appbootstrap.ShutdownContext(30 * time.Second)
	defer cancel()

	// 停止消息路由
	if err := runtime.Stop(ctx); err != nil {
		appLogger.Errorf("❌ Failed to stop message router: %v", err)
	}

	// 关闭服务器
	if err := server.Shutdown(ctx); err != nil {
		appLogger.Errorf("❌ Server forced to shutdown: %v", err)
	}
	if err := app.RunShutdownHooks(); err != nil {
		appLogger.Errorf("Failed to run shutdown hooks: %v", err)
	}

	appLogger.Info("✅ Server shutdown complete")
}

func setupEnhancedRouter(
	cfg *config.Config,
	runtime *appserver.RealtimeRuntime,
	db *gorm.DB,
) *gin.Engine {
	router := gin.New()

	// 中间件
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(enhancedCorsMiddleware(cfg))
	if cfg.Monitoring.Tracing.Enabled {
		router.Use(otelgin.Middleware(cfg.Monitoring.Tracing.ServiceName))
	}

	// 速率限制中间件（如果启用）
	if cfg.Security.RateLimiting.Enabled {
		router.Use(rateLimitMiddleware(cfg))
		logrus.Info("🔒 Rate limiting enabled")
	}

	// 根路径重定向到静态文件
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/index.html")
	})

	// 健康检查
	healthHandler := handlers.NewEnhancedHealthHandler(cfg, runtime.AIHandlerService, db, nil)
	router.GET("/health", healthHandler.Health)
	router.GET("/ready", healthHandler.Ready)

	// 监控端点
	if cfg.Monitoring.Enabled {
		router.GET(cfg.Monitoring.MetricsPath, handlers.NewMetricsHandler(runtime.RealtimeGateway, runtime.RTCGateway, runtime.AIHandlerService, newGormDBStatsProvider(db)).GetMetrics)
	}

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

		// AI 相关 API
		aiHandler := handlers.NewAIHandler(runtime.AIHandlerService)
		aiAPI := api.Group("/ai")
		{
			aiAPI.POST("/query", aiHandler.ProcessQuery)
			aiAPI.GET("/status", aiHandler.GetStatus)
			aiAPI.GET("/metrics", aiHandler.GetMetrics)
			aiAPI.POST("/knowledge/upload", aiHandler.UploadDocument)
			aiAPI.POST("/knowledge/sync", aiHandler.SyncKnowledgeBase)
			aiAPI.PUT("/knowledge-provider/enable", aiHandler.EnableKnowledgeProvider)
			aiAPI.PUT("/knowledge-provider/disable", aiHandler.DisableKnowledgeProvider)
			aiAPI.POST("/circuit-breaker/reset", aiHandler.ResetCircuitBreaker)
		}

		// 轻量指标上报（客户端/前端）
		ingest := handlers.NewMetricsIngestHandler(handlers.NewMetricsAggregator())
		api.POST("/metrics/ingest", ingest.Ingest)

		// 文件上传 API（如果启用）必须放在相同作用域下，复用 api 组
		if cfg.Upload.Enabled {
			uploadHandler := handlers.NewUploadHandler(cfg, runtime.AIHandlerService)
			api.POST("/upload", uploadHandler.UploadFile)
			api.GET("/upload/status/:id", uploadHandler.GetUploadStatus)
		}
	}

	// 静态文件服务
	router.Static("/static", "./static")
	router.Static("/uploads", cfg.Upload.StoragePath)
	router.Static("/", "./web") // 服务官网静态文件

	return router
}

func enhancedCorsMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 动态配置 CORS
		if cfg.Security.CORS.Enabled {
			origins := "*"
			if len(cfg.Security.CORS.AllowedOrigins) > 0 && cfg.Security.CORS.AllowedOrigins[0] != "*" {
				// 在生产环境中应该验证 Origin
				origins = cfg.Security.CORS.AllowedOrigins[0]
			}

			c.Header("Access-Control-Allow-Origin", origins)
			c.Header("Access-Control-Allow-Credentials", "true")

			allowedHeaders := "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With"
			if len(cfg.Security.CORS.AllowedHeaders) > 0 {
				allowedHeaders = cfg.Security.CORS.AllowedHeaders[0]
			}
			c.Header("Access-Control-Allow-Headers", allowedHeaders)

			allowedMethods := "POST, OPTIONS, GET, PUT, DELETE"
			if len(cfg.Security.CORS.AllowedMethods) > 0 {
				allowedMethods = cfg.Security.CORS.AllowedMethods[0]
			}
			c.Header("Access-Control-Allow-Methods", allowedMethods)
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// startHealthMonitoring 启动健康监控
func startHealthMonitoring(cfg *config.Config, weKnoraClient weknora.WeKnoraInterface) {
	ticker := time.NewTicker(cfg.WeKnora.HealthCheck.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 检查 WeKnora 健康状态
			if cfg.WeKnora.Enabled && weKnoraClient != nil {
				ctx, cancel := context.WithTimeout(context.Background(), cfg.WeKnora.HealthCheck.Timeout)
				err := weKnoraClient.HealthCheck(ctx)
				cancel()

				if err != nil {
					logrus.Warnf("⚠️  WeKnora health check failed: %v", err)
				} else {
					logrus.Debug("✅ WeKnora health check passed")
				}
			}
		}
	}
}

// rateLimitMiddleware 速率限制中间件
func rateLimitMiddleware(cfg *config.Config) gin.HandlerFunc {
	// 令牌桶实现：
	// - 速率：RequestsPerMinute / 60 tokens/sec
	// - 桶容量：Burst（若 Burst 未配置则退化为 RequestsPerMinute）

	type bucket struct {
		tokens     float64
		lastRefill time.Time
		mutex      sync.Mutex
	}

	ratePerSec := float64(cfg.Security.RateLimiting.RequestsPerMinute) / 60.0
	capacity := cfg.Security.RateLimiting.Burst
	if capacity <= 0 {
		capacity = cfg.Security.RateLimiting.RequestsPerMinute
		if capacity <= 0 {
			capacity = 60
		}
	}

	buckets := make(map[string]*bucket)
	var bucketsMu sync.RWMutex

	return func(c *gin.Context) {
		clientIP := c.ClientIP()

		bucketsMu.RLock()
		b, ok := buckets[clientIP]
		bucketsMu.RUnlock()
		if !ok {
			bucketsMu.Lock()
			if b, ok = buckets[clientIP]; !ok {
				b = &bucket{tokens: float64(capacity), lastRefill: time.Now()}
				buckets[clientIP] = b
			}
			bucketsMu.Unlock()
		}

		b.mutex.Lock()
		now := time.Now()
		elapsed := now.Sub(b.lastRefill).Seconds()
		// refill
		b.tokens += elapsed * ratePerSec
		if b.tokens > float64(capacity) {
			b.tokens = float64(capacity)
		}
		b.lastRefill = now

		if b.tokens >= 1.0 {
			b.tokens -= 1.0
			b.mutex.Unlock()
			c.Next()
			return
		}

		// 计算重试时间
		need := 1.0 - b.tokens
		retryAfter := 1
		if ratePerSec > 0 {
			secs := int(need/ratePerSec + 0.9999) // ceil
			if secs > 0 {
				retryAfter = secs
			}
		}
		b.mutex.Unlock()

		c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":       "Rate limit exceeded",
			"message":     fmt.Sprintf("Too many requests. Limit: %d req/min (burst %d)", cfg.Security.RateLimiting.RequestsPerMinute, capacity),
			"retry_after": retryAfter,
		})
		c.Abort()
		return
	}
}
