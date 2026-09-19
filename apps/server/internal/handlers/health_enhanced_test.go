package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/alicebob/miniredis/v2"
	"servify/apps/server/internal/config"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
)

func TestEnhancedHealthHandler_Health_And_Ready(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.GetDefaultConfig()
	// Keep tests hermetic: skip external deps checks.
	cfg.Monitoring.HealthChecks.Database = false
	cfg.Monitoring.HealthChecks.Redis = false
	cfg.Monitoring.HealthChecks.KnowledgeProvider = false
	cfg.Monitoring.HealthChecks.WeKnora = false
	cfg.WeKnora.Enabled = false

	ai := aidelivery.NewAIService("", "")
	ai.InitializeKnowledgeBase()

	h := NewEnhancedHealthHandler(cfg, aidelivery.NewHandlerServiceAdapter(ai), nil, nil)

	r := gin.New()
	r.GET("/health", h.Health)
	r.GET("/ready", h.Ready)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", w.Code, w.Body.String())
	}

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/ready", nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("ready status=%d body=%s", w2.Code, w2.Body.String())
	}
}

func TestEnhancedHealthHandler_Health_WithDatabaseCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.GetDefaultConfig()
	// Enable database check to increase coverage
	cfg.Monitoring.HealthChecks.Database = true
	cfg.Monitoring.HealthChecks.Redis = false
	cfg.Monitoring.HealthChecks.KnowledgeProvider = false
	cfg.Monitoring.HealthChecks.WeKnora = false

	ai := aidelivery.NewAIService("", "")
	ai.InitializeKnowledgeBase()

	h := NewEnhancedHealthHandler(cfg, aidelivery.NewHandlerServiceAdapter(ai), nil, nil)

	r := gin.New()
	r.GET("/health", h.Health)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)

	// Should return 200 even without actual DB (simulated)
	if w.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestEnhancedHealthHandler_Health_WithRedisCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.GetDefaultConfig()
	// Enable redis check to increase coverage
	cfg.Monitoring.HealthChecks.Database = false
	cfg.Monitoring.HealthChecks.Redis = true
	cfg.Monitoring.HealthChecks.KnowledgeProvider = false
	cfg.Monitoring.HealthChecks.WeKnora = false

	ai := aidelivery.NewAIService("", "")
	ai.InitializeKnowledgeBase()

	h := NewEnhancedHealthHandler(cfg, aidelivery.NewHandlerServiceAdapter(ai), nil, nil)

	r := gin.New()
	r.GET("/health", h.Health)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)

	// Redis client 未初始化时仍返回 200，但状态应降级而不是假装 healthy。
	if w.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestEnhancedHealthHandler_Health_WithKnowledgeProviderCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.GetDefaultConfig()
	// Enable knowledge provider check to increase coverage.
	cfg.Monitoring.HealthChecks.Database = false
	cfg.Monitoring.HealthChecks.Redis = false
	cfg.Monitoring.HealthChecks.KnowledgeProvider = true
	cfg.Monitoring.HealthChecks.WeKnora = false
	cfg.WeKnora.Enabled = true

	ai := aidelivery.NewAIService("", "")
	ai.InitializeKnowledgeBase()

	h := NewEnhancedHealthHandler(cfg, aidelivery.NewHandlerServiceAdapter(ai), nil, nil)

	r := gin.New()
	r.GET("/health", h.Health)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)

	// Should return 200
	if w.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestEnhancedHealthHandler_CheckRedisWithSharedClient(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	cfg := config.GetDefaultConfig()
	cfg.Monitoring.HealthChecks.Redis = true

	ai := aidelivery.NewAIService("", "")
	ai.InitializeKnowledgeBase()
	h := NewEnhancedHealthHandler(cfg, aidelivery.NewHandlerServiceAdapter(ai), nil, client)

	response := &HealthResponse{Services: map[string]ServiceInfo{}}
	allHealthy := true
	h.checkRedis(context.Background(), response, &allHealthy)

	if !allHealthy {
		t.Fatal("expected redis health check to keep service healthy")
	}
	if response.Services["redis"].Status != "healthy" {
		t.Fatalf("expected redis status healthy, got %q", response.Services["redis"].Status)
	}
}
