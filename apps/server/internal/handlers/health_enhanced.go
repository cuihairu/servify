package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"servify/apps/server/internal/config"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"servify/apps/server/internal/platform/configscope"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// DatabasePing 数据库ping接口
type DatabasePing interface {
	DB() (*sql.DB, error)
}

// EnhancedHealthHandler 增强的健康检查处理器
type EnhancedHealthHandler struct {
	config    *config.Config
	aiService aidelivery.HandlerService
	db        DatabasePing
	redis     *redis.Client
	logger    *logrus.Logger
}

// NewEnhancedHealthHandler 创建增强的健康检查处理器
func NewEnhancedHealthHandler(cfg *config.Config, aiService aidelivery.HandlerService, db DatabasePing, redisClient *redis.Client) *EnhancedHealthHandler {
	return &EnhancedHealthHandler{
		config:    cfg,
		aiService: aiService,
		db:        db,
		redis:     redisClient,
		logger:    logrus.StandardLogger(),
	}
}

// HealthResponse 健康检查响应
type HealthResponse struct {
	Status    string                 `json:"status"`
	Version   string                 `json:"version"`
	Timestamp time.Time              `json:"timestamp"`
	Services  map[string]ServiceInfo `json:"services"`
	System    SystemInfo             `json:"system"`
}

// ServiceInfo 服务信息
type ServiceInfo struct {
	Status  string      `json:"status"`
	Latency string      `json:"latency,omitempty"`
	Error   string      `json:"error,omitempty"`
	Details interface{} `json:"details,omitempty"`
}

// SystemInfo 系统信息
type SystemInfo struct {
	Uptime    time.Duration `json:"uptime"`
	Version   string        `json:"version"`
	GoVersion string        `json:"go_version"`
}

var startTime = time.Now()

// Health 健康检查端点
func (h *EnhancedHealthHandler) Health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	response := HealthResponse{
		Status:    "healthy",
		Version:   "1.1.0",
		Timestamp: time.Now(),
		Services:  make(map[string]ServiceInfo),
		System: SystemInfo{
			Uptime:    time.Since(startTime),
			Version:   "1.1.0",
			GoVersion: "1.25+",
		},
	}

	allHealthy := true

	// 检查 AI 服务状态
	h.checkAIService(ctx, &response, &allHealthy)

	// 检查数据库（如果启用）
	if h.config.Monitoring.HealthChecks.Database {
		h.checkDatabase(ctx, &response, &allHealthy)
	}

	// 检查 Redis（如果启用）
	if h.config.Monitoring.HealthChecks.Redis {
		h.checkRedis(ctx, &response, &allHealthy)
	}

	// 检查知识 provider（Dify / WeKnora）
	weKnoraConfig := configscope.NewResolver(h.config).ResolveWeKnora(context.Background(), nil)
	if h.config.Monitoring.HealthChecks.KnowledgeProviderEnabled() && (weKnoraConfig.Enabled || (h.config != nil && h.config.Dify.Enabled)) {
		h.checkKnowledgeProvider(ctx, &response, &allHealthy)
	}

	// 设置总体状态
	if !allHealthy {
		response.Status = "degraded"
	}

	// response.Status 只可能为 "healthy" 或 "degraded"（"unhealthy" 仅出现在
	// 各子服务的 ServiceInfo.Status 里），整体健康端点恒返回 200。
	c.JSON(http.StatusOK, response)
}

// Ready 就绪检查端点
func (h *EnhancedHealthHandler) Ready(c *gin.Context) {
	// 简单的就绪检查，只检查核心服务
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	ready := true
	services := make(map[string]string)

	// 检查 AI 服务
	if status := h.aiService.GetStatus(ctx); status != nil {
		services["ai"] = "ready"
	} else {
		services["ai"] = "not_ready"
		ready = false
	}

	// 检查数据库连接（如果配置了）
	if h.db != nil {
		sqlDB, err := h.db.DB()
		if err == nil {
			if err := sqlDB.PingContext(ctx); err != nil {
				services["database"] = "not_ready"
				ready = false
			} else {
				services["database"] = "ready"
			}
		} else {
			services["database"] = "not_ready"
			ready = false
		}
	} else {
		// DB 未配置（测试环境），不影响就绪状态
		services["database"] = "disabled"
	}

	// 基础的就绪响应
	response := map[string]interface{}{
		"ready":     ready,
		"timestamp": time.Now(),
		"services":  services,
	}

	statusCode := http.StatusOK
	if !ready {
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, response)
}

// checkAIService 检查 AI 服务状态
func (h *EnhancedHealthHandler) checkAIService(ctx context.Context, response *HealthResponse, allHealthy *bool) {
	start := time.Now()

	status := h.aiService.GetStatus(ctx)
	if status == nil {
		response.Services["ai"] = ServiceInfo{
			Status: "unhealthy",
			Error:  "AI service not responding",
		}
		*allHealthy = false
		return
	}

	serviceInfo := ServiceInfo{
		Status:  "healthy",
		Latency: time.Since(start).String(),
		Details: status,
	}

	// 检查 WeKnora 状态（如果是增强服务）
	if metrics, ok := h.aiService.GetMetrics(); ok {
		serviceInfo.Details = map[string]interface{}{
			"type":    "enhanced",
			"status":  status,
			"metrics": metrics,
		}
	}

	response.Services["ai"] = serviceInfo
}

// checkDatabase 检查数据库状态
func (h *EnhancedHealthHandler) checkDatabase(ctx context.Context, response *HealthResponse, allHealthy *bool) {
	start := time.Now()

	dbHealthy := true
	var dbError string

	// 真实的数据库连接检查
	if h.db != nil {
		sqlDB, err := h.db.DB()
		if err != nil {
			dbHealthy = false
			dbError = "failed to get database connection: " + err.Error()
		} else {
			if err := sqlDB.PingContext(ctx); err != nil {
				dbHealthy = false
				dbError = err.Error()
			}
		}
	} else {
		dbHealthy = false
		dbError = "database connection not initialized"
	}

	serviceInfo := ServiceInfo{
		Latency: time.Since(start).String(),
		Details: map[string]interface{}{
			"driver": "postgresql",
			"host":   h.config.Database.Host,
			"port":   h.config.Database.Port,
		},
	}

	if dbHealthy {
		serviceInfo.Status = "healthy"
	} else {
		serviceInfo.Status = "unhealthy"
		serviceInfo.Error = dbError
		*allHealthy = false
	}

	response.Services["database"] = serviceInfo
}

func (h *EnhancedHealthHandler) checkRedis(ctx context.Context, response *HealthResponse, allHealthy *bool) {
	start := time.Now()

	serviceInfo := ServiceInfo{
		Latency: time.Since(start).String(),
		Details: map[string]interface{}{
			"host":            h.config.Redis.Host,
			"port":            h.config.Redis.Port,
			"connection_type": "shared_client",
		},
	}
	if h.redis == nil {
		serviceInfo.Status = "disabled"
		response.Services["redis"] = serviceInfo
		return
	}
	if err := h.redis.Ping(ctx).Err(); err != nil {
		serviceInfo.Status = "unhealthy"
		serviceInfo.Error = err.Error()
		*allHealthy = false
	} else {
		serviceInfo.Status = "healthy"
	}
	response.Services["redis"] = serviceInfo
}

// checkKnowledgeProvider 检查当前知识 provider 状态。
func (h *EnhancedHealthHandler) checkKnowledgeProvider(ctx context.Context, response *HealthResponse, allHealthy *bool) {
	start := time.Now()
	weKnoraConfig := configscope.NewResolver(h.config).ResolveWeKnora(context.Background(), nil)

	// 如果是增强 AI 服务，获取当前知识 provider 状态
	if _, ok := h.aiService.GetMetrics(); ok {
		status := h.aiService.GetStatus(ctx)
		providerID, _ := status["knowledge_provider"].(string)
		if providerID == "" {
			if h.config != nil && h.config.Dify.Enabled {
				providerID = "dify"
			} else {
				providerID = "weknora"
			}
		}

		healthyKey := providerID + "_healthy"
		providerHealthy, exists := status[healthyKey].(bool)
		if !exists {
			providerHealthy, _ = status["knowledge_provider_healthy"].(bool)
		}
		if !providerHealthy {
			response.Services[providerID] = ServiceInfo{
				Status:  "unhealthy",
				Latency: time.Since(start).String(),
				Error:   providerID + " service unavailable",
			}
			h.logger.Warnf("%s service is unhealthy, but fallback is available", providerID)
		} else {
			details := map[string]interface{}{}
			switch providerID {
			case "dify":
				details["base_url"] = h.config.Dify.BaseURL
				details["dataset_id"] = h.config.Dify.DatasetID
			default:
				details["base_url"] = weKnoraConfig.BaseURL
				details["kb_id"] = weKnoraConfig.KnowledgeBaseID
			}
			response.Services[providerID] = ServiceInfo{
				Status:  "healthy",
				Latency: time.Since(start).String(),
				Details: details,
			}
		}
	} else {
		response.Services["knowledge_provider"] = ServiceInfo{
			Status: "disabled",
		}
	}
}
