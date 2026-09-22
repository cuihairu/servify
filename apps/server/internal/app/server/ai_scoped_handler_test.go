package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type stubFallbackAIHandler struct{}

func (stubFallbackAIHandler) ProcessQuery(context.Context, string, string) (interface{}, error) {
	return nil, nil
}
func (stubFallbackAIHandler) GetStatus(context.Context) map[string]interface{} {
	return map[string]interface{}{"type": "fallback"}
}
func (stubFallbackAIHandler) GetMetrics() (*aidelivery.AIMetrics, bool) {
	return &aidelivery.AIMetrics{}, true
}
func (stubFallbackAIHandler) UploadKnowledgeDocument(context.Context, string, string, []string) error {
	return nil
}
func (stubFallbackAIHandler) SyncKnowledgeBase(context.Context) error { return nil }
func (stubFallbackAIHandler) SetKnowledgeProviderEnabled(bool) bool   { return true }
func (stubFallbackAIHandler) ResetCircuitBreaker() bool               { return true }

func openScopedAITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:"+t.Name()+"")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.TenantConfig{}, &models.WorkspaceConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestScopedAIHandlerServiceProcessQueryUsesWorkspaceOpenAIOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"finish_reason": "stop",
				"message":       map[string]interface{}{"content": "scoped-response"},
			}},
			"usage": map[string]interface{}{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer srv.Close()

	db := openScopedAITestDB(t)
	if err := db.Create(&models.WorkspaceConfig{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		OpenAIJSON:  "api_key: scoped-key\nbase_url: " + srv.URL + "\n",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	handler := NewScopedAIHandlerService(config.GetDefaultConfig(), logrus.New(), db, stubFallbackAIHandler{}, nil, nil)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	resp, err := handler.ProcessQuery(ctx, "hello", "session-1")
	if err != nil {
		t.Fatalf("ProcessQuery() error = %v", err)
	}
	enhanced, ok := resp.(*aidelivery.EnhancedAIResponse)
	if !ok {
		t.Fatalf("expected enhanced response, got %T", resp)
	}
	if enhanced.Content != "scoped-response" {
		t.Fatalf("content = %q want scoped-response", enhanced.Content)
	}
}

func TestScopedAIHandlerServiceGetStatusUsesWorkspaceWeKnoraOverride(t *testing.T) {
	db := openScopedAITestDB(t)
	if err := db.Create(&models.WorkspaceConfig{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnoraJSON: "enabled: true\nbase_url: http://127.0.0.1:1\napi_key: scoped-key\ntenant_id: tenant-a\nknowledge_base_id: kb-scoped\ntimeout: 1s\n",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	handler := NewScopedAIHandlerService(config.GetDefaultConfig(), logrus.New(), db, stubFallbackAIHandler{}, nil, nil)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	status := handler.GetStatus(ctx)
	if enabled, _ := status["knowledge_provider_enabled"].(bool); !enabled {
		t.Fatalf("expected scoped knowledge provider enabled status, got %+v", status)
	}
	if provider, _ := status["knowledge_provider"].(string); provider != "weknora" {
		t.Fatalf("expected scoped knowledge provider to stay weknora, got %+v", status)
	}
}

func TestScopedAIHandlerServiceGetStatusUsesWorkspaceRagFlowOverride(t *testing.T) {
	db := openScopedAITestDB(t)
	if err := db.Create(&models.WorkspaceConfig{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		RagFlowJSON: "enabled: true\nbase_url: http://127.0.0.1:1\napi_key: ragflow-key\ndataset_id: ds-scoped\n",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	handler := NewScopedAIHandlerService(config.GetDefaultConfig(), logrus.New(), db, stubFallbackAIHandler{}, nil, nil)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	status := handler.GetStatus(ctx)
	if enabled, _ := status["knowledge_provider_enabled"].(bool); !enabled {
		t.Fatalf("expected scoped ragflow provider enabled, got %+v", status)
	}
	if provider, _ := status["knowledge_provider"].(string); provider != "ragflow" {
		t.Fatalf("expected workspace ragflow override, got %+v", status)
	}
}

func TestScopedAIHandlerServiceGetMetricsFallsBackToBaseHandler(t *testing.T) {
	handler := NewScopedAIHandlerService(config.GetDefaultConfig(), logrus.New(), openScopedAITestDB(t), stubFallbackAIHandler{}, nil, nil)
	metrics, ok := handler.GetMetrics()
	if !ok || metrics == nil {
		t.Fatal("expected fallback metrics")
	}
}

func TestRuntimeServiceFromResolvedConfigWithoutWeKnora(t *testing.T) {
	service := runtimeServiceFromResolvedConfig(config.OpenAIConfig{APIKey: "", BaseURL: ""}, config.DifyConfig{}, config.RagFlowConfig{}, config.WeKnoraConfig{}, config.AIConfig{}, logrus.New(), nil)
	status := service.GetStatus(context.Background())
	if typ, _ := status["type"].(string); typ == "" {
		t.Fatalf("expected service status type, got %+v", status)
	}
}

func TestRuntimeServiceFromResolvedConfigWithWeKnoraScopedKnowledgeBase(t *testing.T) {
	service := runtimeServiceFromResolvedConfig(config.OpenAIConfig{}, config.DifyConfig{}, config.RagFlowConfig{}, config.WeKnoraConfig{
		Enabled:         true,
		BaseURL:         "http://127.0.0.1:1",
		APIKey:          "key",
		TenantID:        "tenant-a",
		KnowledgeBaseID: "kb-scoped",
		Timeout:         time.Second,
	}, config.AIConfig{}, logrus.New(), nil)
	status := service.GetStatus(context.Background())
	if enabled, _ := status["knowledge_provider_enabled"].(bool); !enabled {
		t.Fatalf("expected knowledge provider enabled, got %+v", status)
	}
	if provider, _ := status["knowledge_provider"].(string); provider != "weknora" {
		t.Fatalf("expected weknora knowledge provider, got %+v", status)
	}
}

func TestScopedAIHandlerServiceSetKnowledgeProviderEnabledAppliesToFutureRequests(t *testing.T) {
	handler := NewScopedAIHandlerService(config.GetDefaultConfig(), logrus.New(), openScopedAITestDB(t), stubFallbackAIHandler{}, nil, nil)

	if !handler.SetKnowledgeProviderEnabled(true) {
		t.Fatal("expected enable to succeed")
	}

	status := handler.GetStatus(context.Background())
	if enabled, _ := status["knowledge_provider_enabled"].(bool); !enabled {
		t.Fatalf("expected runtime override to enable knowledge provider, got %+v", status)
	}

	if !handler.SetKnowledgeProviderEnabled(false) {
		t.Fatal("expected disable to succeed")
	}

	status = handler.GetStatus(context.Background())
	if enabled, _ := status["knowledge_provider_enabled"].(bool); enabled {
		t.Fatalf("expected runtime override to disable knowledge provider, got %+v", status)
	}
}

func TestScopedAIHandlerServiceUploadUsesRuntimeKnowledgeProviderOverride(t *testing.T) {
	db := openScopedAITestDB(t)
	if err := db.Create(&models.WorkspaceConfig{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnoraJSON: "enabled: true\nbase_url: http://127.0.0.1:1\napi_key: scoped-key\ntenant_id: tenant-a\nknowledge_base_id: kb-scoped\ntimeout: 1s\nmax_retries: 0\n",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	handler := NewScopedAIHandlerService(config.GetDefaultConfig(), logrus.New(), db, stubFallbackAIHandler{}, nil, nil)
	if !handler.SetKnowledgeProviderEnabled(true) {
		t.Fatal("expected enable to succeed")
	}

	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	err := handler.UploadKnowledgeDocument(ctx, "doc-1", "content", []string{"tag"})
	if err == nil {
		t.Fatal("expected provider call to fail against unreachable weknora")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected deadline error without entering provider path: %v", err)
	}
	if strings.Contains(err.Error(), "knowledge provider is not enabled") {
		t.Fatalf("expected runtime override to reach provider path, got %v", err)
	}
}

var _ aidelivery.HandlerService = (*scopedAIHandlerService)(nil)

// pgvector 是全局配置：请求级重建不认识它，声明时必须直通启动装配的全局实例，
// 否则 HTTP 面的 ai/status 恒报 disabled（第八刀修复的第二装配缺口）。
func TestScopedAIHandlerServiceBuildServicePrefersStartupForPgvector(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Knowledge.Provider = "pgvector"
	startup := stubRuntimeFallback{}
	handler := &scopedAIHandlerService{cfg: cfg, logger: logrus.New(), fallback: stubFallbackAIHandler{}, startup: startup}
	if got := handler.buildService(context.Background()); got != aidelivery.RuntimeService(startup) {
		t.Fatalf("expected startup instance passthrough, got %#v", got)
	}
}
