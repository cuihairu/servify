package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	svrmetrics "servify/apps/server/internal/metrics"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"servify/apps/server/internal/platform/realtime"
	"servify/apps/server/internal/version"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// AIHandler AI 服务处理器
type AIHandler struct {
	aiService aidelivery.HandlerService
	logger    *logrus.Logger
}

// NewAIHandler 创建 AI 处理器
func NewAIHandler(aiService aidelivery.HandlerService) *AIHandler {
	return &AIHandler{
		aiService: aiService,
		logger:    logrus.StandardLogger(),
	}
}

// QueryRequest 查询请求
type QueryRequest struct {
	Query     string `json:"query" binding:"required"`
	SessionID string `json:"session_id"`
}

// QueryResponse 查询响应
type QueryResponse struct {
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
	Duration  string      `json:"duration"`
}

// ProcessQuery 处理 AI 查询
func (h *AIHandler) ProcessQuery(c *gin.Context) {
	start := time.Now()

	var req QueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, QueryResponse{
			Success:   false,
			Error:     "Invalid request format: " + err.Error(),
			Timestamp: time.Now(),
			Duration:  time.Since(start).String(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	response, err := h.aiService.ProcessQuery(ctx, req.Query, req.SessionID)
	if err != nil {
		h.logger.Errorf("AI query failed: %v", err)
		c.JSON(http.StatusInternalServerError, QueryResponse{
			Success:   false,
			Error:     "AI processing failed: " + err.Error(),
			Timestamp: time.Now(),
			Duration:  time.Since(start).String(),
		})
		return
	}

	c.JSON(http.StatusOK, QueryResponse{
		Success:   true,
		Data:      response,
		Timestamp: time.Now(),
		Duration:  time.Since(start).String(),
	})
}

// GetStatus 获取 AI 服务状态
func (h *AIHandler) GetStatus(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	status := h.aiService.GetStatus(ctx)

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"data":      status,
		"timestamp": time.Now(),
	})
}

// GetMetrics 获取 AI 服务指标（仅增强服务支持）
func (h *AIHandler) GetMetrics(c *gin.Context) {
	metrics, ok := h.aiService.GetMetrics()
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   "metrics are not available for the current AI service",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"data":      metrics,
		"timestamp": time.Now(),
	})
}

// UploadDocumentRequest 文档上传请求
type UploadDocumentRequest struct {
	Title   string   `json:"title" binding:"required"`
	Content string   `json:"content" binding:"required"`
	Tags    []string `json:"tags"`
}

// UploadDocument 上传文档到知识库
func (h *AIHandler) UploadDocument(c *gin.Context) {
	var req UploadDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request format: " + err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	if err := h.aiService.UploadKnowledgeDocument(ctx, req.Title, req.Content, req.Tags); err != nil {
		c.JSON(aiCapabilityStatusCode(err), gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Document uploaded successfully",
		"data": gin.H{
			"title": req.Title,
			"tags":  req.Tags,
		},
	})
}

// SyncKnowledgeBase 同步知识库
func (h *AIHandler) SyncKnowledgeBase(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	if err := h.aiService.SyncKnowledgeBase(ctx); err != nil {
		c.JSON(aiCapabilityStatusCode(err), gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Knowledge base synchronized successfully",
	})
}

// EnableKnowledgeProvider 启用外部知识库 provider
func (h *AIHandler) EnableKnowledgeProvider(c *gin.Context) {
	if !h.aiService.SetKnowledgeProviderEnabled(true) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   "knowledge provider control is not available for the current AI service",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "knowledge provider enabled",
	})
}

// DisableKnowledgeProvider 禁用外部知识库 provider
func (h *AIHandler) DisableKnowledgeProvider(c *gin.Context) {
	if !h.aiService.SetKnowledgeProviderEnabled(false) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   "knowledge provider control is not available for the current AI service",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "knowledge provider disabled",
	})
}

// ResetCircuitBreaker 重置熔断器
func (h *AIHandler) ResetCircuitBreaker(c *gin.Context) {
	if !h.aiService.ResetCircuitBreaker() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   "circuit breaker control is not available for the current AI service",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Circuit breaker reset",
	})
}

// MetricsHandler 指标处理器
type MetricsHandler struct {
	wsHub         realtime.RealtimeGateway
	webrtcService realtime.RTCGateway
	aiService     aidelivery.HandlerService
	startedAt     time.Time
	dbStats       DBStatsProvider
}

type DBStatsProvider interface {
	Stats() (sql.DBStats, bool)
}

// NewMetricsHandler 创建指标处理器
func NewMetricsHandler(wsHub realtime.RealtimeGateway, webrtc realtime.RTCGateway, ai aidelivery.HandlerService, dbStats DBStatsProvider) *MetricsHandler {
	return &MetricsHandler{wsHub: wsHub, webrtcService: webrtc, aiService: ai, startedAt: time.Now(), dbStats: dbStats}
}

// GetMetrics 获取系统指标（Prometheus 格式）
func (h *MetricsHandler) GetMetrics(c *gin.Context) {
	c.Header("Content-Type", "text/plain")

	// 采样运行态
	uptime := time.Since(h.startedAt).Seconds()
	wsClients := 0
	webrtcConns := 0
	if h.wsHub != nil {
		wsClients = h.wsHub.ClientCount()
	}
	if h.webrtcService != nil {
		webrtcConns = h.webrtcService.ConnectionCount()
	}

	var aiQueries, aiDify, aiWeKnora, aiProvider, aiFallback int64
	var aiAvgLatency float64
	if m, ok := h.aiService.GetMetrics(); ok && m != nil {
		aiQueries = m.QueryCount
		aiDify = m.DifyUsageCount
		aiWeKnora = m.WeKnoraUsageCount
		aiProvider = m.KnowledgeProviderUsageCount
		aiFallback = m.FallbackUsageCount
		aiAvgLatency = m.AverageLatency.Seconds()
	}

	// Prometheus exposition format
	b := &strings.Builder{}
	fmt.Fprintf(b, "# HELP servify_info Information about the Servify instance\n")
	fmt.Fprintf(b, "# TYPE servify_info gauge\n")
	// include labels for version/commit/build_time
	v := strings.ReplaceAll(version.Version, "\"", "\\\"")
	cmt := strings.ReplaceAll(version.Commit, "\"", "\\\"")
	bt := strings.ReplaceAll(version.BuildTime, "\"", "\\\"")
	fmt.Fprintf(b, "servify_info{version=\"%s\",commit=\"%s\",build_time=\"%s\"} 1\n\n", v, cmt, bt)

	fmt.Fprintf(b, "# HELP servify_uptime_seconds Total uptime of the Servify instance in seconds\n")
	fmt.Fprintf(b, "# TYPE servify_uptime_seconds counter\n")
	fmt.Fprintf(b, "servify_uptime_seconds %.0f\n\n", uptime)

	fmt.Fprintf(b, "# HELP servify_websocket_active_connections Active WebSocket connections\n")
	fmt.Fprintf(b, "# TYPE servify_websocket_active_connections gauge\n")
	fmt.Fprintf(b, "servify_websocket_active_connections %d\n\n", wsClients)

	fmt.Fprintf(b, "# HELP servify_webrtc_connections Active WebRTC peer connections\n")
	fmt.Fprintf(b, "# TYPE servify_webrtc_connections gauge\n")
	fmt.Fprintf(b, "servify_webrtc_connections %d\n\n", webrtcConns)

	fmt.Fprintf(b, "# HELP servify_ai_requests_total Total AI queries processed\n")
	fmt.Fprintf(b, "# TYPE servify_ai_requests_total counter\n")
	fmt.Fprintf(b, "servify_ai_requests_total %d\n\n", aiQueries)

	fmt.Fprintf(b, "# HELP servify_ai_knowledge_provider_usage_total Total AI queries served via an external knowledge provider\n")
	fmt.Fprintf(b, "# TYPE servify_ai_knowledge_provider_usage_total counter\n")
	fmt.Fprintf(b, "servify_ai_knowledge_provider_usage_total %d\n\n", aiProvider)

	fmt.Fprintf(b, "# HELP servify_ai_dify_usage_total Total AI queries served via Dify\n")
	fmt.Fprintf(b, "# TYPE servify_ai_dify_usage_total counter\n")
	fmt.Fprintf(b, "servify_ai_dify_usage_total %d\n\n", aiDify)

	fmt.Fprintf(b, "# HELP servify_ai_weknora_usage_total Total AI queries served via WeKnora compatibility provider\n")
	fmt.Fprintf(b, "# TYPE servify_ai_weknora_usage_total counter\n")
	fmt.Fprintf(b, "servify_ai_weknora_usage_total %d\n\n", aiWeKnora)

	fmt.Fprintf(b, "# HELP servify_ai_fallback_usage_total Total AI queries served via fallback KB\n")
	fmt.Fprintf(b, "# TYPE servify_ai_fallback_usage_total counter\n")
	fmt.Fprintf(b, "servify_ai_fallback_usage_total %d\n\n", aiFallback)

	fmt.Fprintf(b, "# HELP servify_ai_avg_latency_seconds Average AI processing latency seconds\n")
	fmt.Fprintf(b, "# TYPE servify_ai_avg_latency_seconds gauge\n")
	fmt.Fprintf(b, "servify_ai_avg_latency_seconds %.3f\n\n", aiAvgLatency)

	// Go runtime minimal metrics
	fmt.Fprintf(b, "# HELP servify_go_goroutines Number of goroutines\n")
	fmt.Fprintf(b, "# TYPE servify_go_goroutines gauge\n")
	fmt.Fprintf(b, "servify_go_goroutines %d\n\n", runtime.NumGoroutine())

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Fprintf(b, "# HELP servify_go_mem_alloc_bytes Bytes of allocated heap objects\n")
	fmt.Fprintf(b, "# TYPE servify_go_mem_alloc_bytes gauge\n")
	fmt.Fprintf(b, "servify_go_mem_alloc_bytes %d\n", ms.Alloc)

	// Database/sql stats (if available)
	if h.dbStats != nil {
		if ds, ok := h.dbStats.Stats(); ok {
			fmt.Fprintf(b, "\n# HELP servify_db_max_open_connections Maximum number of open connections to the database\n")
			fmt.Fprintf(b, "# TYPE servify_db_max_open_connections gauge\n")
			fmt.Fprintf(b, "servify_db_max_open_connections %d\n", ds.MaxOpenConnections)

			fmt.Fprintf(b, "# HELP servify_db_open_connections The number of established connections both in use and idle\n")
			fmt.Fprintf(b, "# TYPE servify_db_open_connections gauge\n")
			fmt.Fprintf(b, "servify_db_open_connections %d\n", ds.OpenConnections)

			fmt.Fprintf(b, "# HELP servify_db_inuse_connections The number of connections currently in use\n")
			fmt.Fprintf(b, "# TYPE servify_db_inuse_connections gauge\n")
			fmt.Fprintf(b, "servify_db_inuse_connections %d\n", ds.InUse)

			fmt.Fprintf(b, "# HELP servify_db_idle_connections The number of idle connections\n")
			fmt.Fprintf(b, "# TYPE servify_db_idle_connections gauge\n")
			fmt.Fprintf(b, "servify_db_idle_connections %d\n", ds.Idle)

			fmt.Fprintf(b, "# HELP servify_db_wait_count The total number of connections waited for\n")
			fmt.Fprintf(b, "# TYPE servify_db_wait_count counter\n")
			fmt.Fprintf(b, "servify_db_wait_count %d\n", ds.WaitCount)

			fmt.Fprintf(b, "# HELP servify_db_wait_duration_seconds The total time blocked waiting for a new connection\n")
			fmt.Fprintf(b, "# TYPE servify_db_wait_duration_seconds counter\n")
			fmt.Fprintf(b, "servify_db_wait_duration_seconds %.6f\n", ds.WaitDuration.Seconds())

			fmt.Fprintf(b, "# HELP servify_db_max_idle_closed_total The total number of connections closed due to SetMaxIdleConns\n")
			fmt.Fprintf(b, "# TYPE servify_db_max_idle_closed_total counter\n")
			fmt.Fprintf(b, "servify_db_max_idle_closed_total %d\n", ds.MaxIdleClosed)

			fmt.Fprintf(b, "# HELP servify_db_max_lifetime_closed_total The total number of connections closed due to SetConnMaxLifetime\n")
			fmt.Fprintf(b, "# TYPE servify_db_max_lifetime_closed_total counter\n")
			fmt.Fprintf(b, "servify_db_max_lifetime_closed_total %d\n", ds.MaxLifetimeClosed)
		}
	}

	// Rate limit drops (by prefix)
	totalDrops, byPrefix := svrmetrics.RateLimitSnapshot()
	fmt.Fprintf(b, "\n# HELP servify_ratelimit_dropped_total Total HTTP 429 responses due to rate limiting\n")
	fmt.Fprintf(b, "# TYPE servify_ratelimit_dropped_total counter\n")
	if len(byPrefix) == 0 {
		fmt.Fprintf(b, "servify_ratelimit_dropped_total{prefix=\"global\"} %d\n", 0)
	} else {
		for p, v := range byPrefix {
			label := strings.ReplaceAll(p, "\"", "\\\"")
			fmt.Fprintf(b, "servify_ratelimit_dropped_total{prefix=\"%s\"} %d\n", label, v)
		}
	}
	// Optional sum without labels
	fmt.Fprintf(b, "servify_ratelimit_dropped_sum %d\n", totalDrops)

	c.String(http.StatusOK, b.String())
}

func aiCapabilityStatusCode(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if aidelivery.IsUnsupportedEnhancedFeature(err) {
		return http.StatusServiceUnavailable
	}
	errMsg := strings.ToLower(err.Error())
	if strings.Contains(errMsg, "not enabled") ||
		strings.Contains(errMsg, "not initialized") ||
		strings.Contains(errMsg, "unavailable") {
		return http.StatusServiceUnavailable
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}
