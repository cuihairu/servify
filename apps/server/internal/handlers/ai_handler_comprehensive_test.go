package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"servify/apps/server/internal/services"
)

// MockEnhancedAIService 用于测试增强AI服务的处理器
type MockEnhancedAIService struct {
	*services.AIService
	weKnoraEnabled bool
	metrics        *services.AIMetrics
	uploadErr      error
	syncErr        error
}

func (m *MockEnhancedAIService) ProcessQueryEnhanced(ctx context.Context, query, sessionID string) (*aidelivery.EnhancedAIResponse, error) {
	return &aidelivery.EnhancedAIResponse{
		AIResponse: &services.AIResponse{
			Content:    "Mock enhanced response",
			Confidence: 0.9,
		},
		Strategy: "mock",
	}, nil
}

func (m *MockEnhancedAIService) UploadKnowledgeDocument(ctx context.Context, title, content string, tags []string) error {
	if m.uploadErr != nil {
		return m.uploadErr
	}
	return nil
}

func (m *MockEnhancedAIService) SyncKnowledgeBase(ctx context.Context) error {
	if m.syncErr != nil {
		return m.syncErr
	}
	return nil
}

func (m *MockEnhancedAIService) GetMetrics() *services.AIMetrics {
	if m.metrics == nil {
		return &services.AIMetrics{
			QueryCount:         10,
			WeKnoraUsageCount:  5,
			FallbackUsageCount: 2,
			AverageLatency:     100 * time.Millisecond,
		}
	}
	return m.metrics
}

func (m *MockEnhancedAIService) SetKnowledgeProviderEnabled(enabled bool) {
	m.weKnoraEnabled = enabled
}

func (m *MockEnhancedAIService) SetFallbackEnabled(enabled bool) {
	// Mock implementation
}

func (m *MockEnhancedAIService) ResetCircuitBreaker() {
	// Mock implementation
}

func TestAIHandler_GetMetrics_EnhancedService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := &MockEnhancedAIService{
		AIService:      services.NewAIService("", ""),
		weKnoraEnabled: true,
	}
	mockService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(mockService))

	router := gin.New()
	router.GET("/api/v1/ai/metrics", handler.GetMetrics)

	req := httptest.NewRequest("GET", "/api/v1/ai/metrics", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response["success"] != true {
		t.Error("expected success=true")
	}
	if response["data"] == nil {
		t.Error("expected data to be present")
	}
}

func TestAIHandler_GetMetrics_StandardService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	baseService := services.NewAIService("", "")
	baseService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(baseService))

	router := gin.New()
	router.GET("/api/v1/ai/metrics", handler.GetMetrics)

	req := httptest.NewRequest("GET", "/api/v1/ai/metrics", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestAIHandler_UploadDocument_EnhancedService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := &MockEnhancedAIService{
		AIService:      services.NewAIService("", ""),
		weKnoraEnabled: true,
	}
	mockService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(mockService))

	router := gin.New()
	router.POST("/api/v1/ai/upload", handler.UploadDocument)

	payload := map[string]interface{}{
		"title":   "Test Document",
		"content": "This is test content",
		"tags":    []string{"test", "document"},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/ai/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestAIHandler_UploadDocument_InvalidRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := &MockEnhancedAIService{
		AIService:      services.NewAIService("", ""),
		weKnoraEnabled: true,
	}
	mockService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(mockService))

	router := gin.New()
	router.POST("/api/v1/ai/upload", handler.UploadDocument)

	// Missing title
	payload := map[string]interface{}{
		"content": "This is test content",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/ai/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestAIHandler_UploadDocument_StandardService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	baseService := services.NewAIService("", "")
	baseService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(baseService))

	router := gin.New()
	router.POST("/api/v1/ai/upload", handler.UploadDocument)

	payload := map[string]interface{}{
		"title":   "Test Document",
		"content": "This is test content",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/ai/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestAIHandler_SyncKnowledgeBase_EnhancedService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := &MockEnhancedAIService{
		AIService:      services.NewAIService("", ""),
		weKnoraEnabled: true,
	}
	mockService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(mockService))

	router := gin.New()
	router.POST("/api/v1/ai/sync", handler.SyncKnowledgeBase)

	req := httptest.NewRequest("POST", "/api/v1/ai/sync", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestAIHandler_SyncKnowledgeBase_StandardService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	baseService := services.NewAIService("", "")
	baseService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(baseService))

	router := gin.New()
	router.POST("/api/v1/ai/sync", handler.SyncKnowledgeBase)

	req := httptest.NewRequest("POST", "/api/v1/ai/sync", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestAIHandler_EnableKnowledgeProvider_EnhancedService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := &MockEnhancedAIService{
		AIService:      services.NewAIService("", ""),
		weKnoraEnabled: false,
	}
	mockService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(mockService))

	router := gin.New()
	router.PUT("/api/v1/ai/knowledge-provider/enable", handler.EnableKnowledgeProvider)

	req := httptest.NewRequest("PUT", "/api/v1/ai/knowledge-provider/enable", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if !mockService.weKnoraEnabled {
		t.Error("expected knowledge provider to be enabled")
	}
}

func TestAIHandler_EnableKnowledgeProvider_StandardService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	baseService := services.NewAIService("", "")
	baseService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(baseService))

	router := gin.New()
	router.PUT("/api/v1/ai/knowledge-provider/enable", handler.EnableKnowledgeProvider)

	req := httptest.NewRequest("PUT", "/api/v1/ai/knowledge-provider/enable", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestAIHandler_DisableKnowledgeProvider_EnhancedService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := &MockEnhancedAIService{
		AIService:      services.NewAIService("", ""),
		weKnoraEnabled: true,
	}
	mockService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(mockService))

	router := gin.New()
	router.PUT("/api/v1/ai/knowledge-provider/disable", handler.DisableKnowledgeProvider)

	req := httptest.NewRequest("PUT", "/api/v1/ai/knowledge-provider/disable", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if mockService.weKnoraEnabled {
		t.Error("expected knowledge provider to be disabled")
	}
}

func TestAIHandler_DisableKnowledgeProvider_StandardService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	baseService := services.NewAIService("", "")
	baseService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(baseService))

	router := gin.New()
	router.PUT("/api/v1/ai/knowledge-provider/disable", handler.DisableKnowledgeProvider)

	req := httptest.NewRequest("PUT", "/api/v1/ai/knowledge-provider/disable", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestAIHandler_UploadDocument_KnowledgeProviderDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := &MockEnhancedAIService{
		AIService: services.NewAIService("", ""),
		uploadErr: errors.New("knowledge provider is not enabled"),
	}
	mockService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(mockService))

	router := gin.New()
	router.POST("/api/v1/ai/upload", handler.UploadDocument)

	payload := map[string]interface{}{
		"title":   "Test Document",
		"content": "This is test content",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/ai/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAIHandler_SyncKnowledgeBase_KnowledgeProviderDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := &MockEnhancedAIService{
		AIService: services.NewAIService("", ""),
		syncErr:   errors.New("knowledge provider is not enabled"),
	}
	mockService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(mockService))

	router := gin.New()
	router.POST("/api/v1/ai/sync", handler.SyncKnowledgeBase)

	req := httptest.NewRequest("POST", "/api/v1/ai/sync", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAIHandler_ResetCircuitBreaker_EnhancedService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := &MockEnhancedAIService{
		AIService:      services.NewAIService("", ""),
		weKnoraEnabled: true,
	}
	mockService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(mockService))

	router := gin.New()
	router.POST("/api/v1/ai/circuit-breaker/reset", handler.ResetCircuitBreaker)

	req := httptest.NewRequest("POST", "/api/v1/ai/circuit-breaker/reset", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestAIHandler_ResetCircuitBreaker_StandardService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	baseService := services.NewAIService("", "")
	baseService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(baseService))

	router := gin.New()
	router.POST("/api/v1/ai/circuit-breaker/reset", handler.ResetCircuitBreaker)

	req := httptest.NewRequest("POST", "/api/v1/ai/circuit-breaker/reset", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestAIHandler_ProcessQuery_EnhancedService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := &MockEnhancedAIService{
		AIService:      services.NewAIService("", ""),
		weKnoraEnabled: true,
	}
	mockService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(mockService))

	router := gin.New()
	router.POST("/api/v1/ai/query", handler.ProcessQuery)

	payload := map[string]string{
		"query":      "测试查询",
		"session_id": "test-session",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/ai/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestAIHandler_ProcessQuery_MissingQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := &MockEnhancedAIService{
		AIService:      services.NewAIService("", ""),
		weKnoraEnabled: true,
	}
	mockService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(mockService))

	router := gin.New()
	router.POST("/api/v1/ai/query", handler.ProcessQuery)

	payload := map[string]string{
		"session_id": "test-session",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/ai/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestAIHandler_ProcessQuery_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := &MockEnhancedAIService{
		AIService:      services.NewAIService("", ""),
		weKnoraEnabled: true,
	}
	mockService.InitializeKnowledgeBase()
	handler := NewAIHandler(aidelivery.NewHandlerServiceAdapter(mockService))

	router := gin.New()
	router.POST("/api/v1/ai/query", handler.ProcessQuery)

	req := httptest.NewRequest("POST", "/api/v1/ai/query", bytes.NewReader([]byte("{invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}
