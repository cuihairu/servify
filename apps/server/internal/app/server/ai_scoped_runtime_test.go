package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/services"

	"github.com/sirupsen/logrus"
)

type stubRuntimeFallback struct{}

func (stubRuntimeFallback) ProcessQuery(context.Context, string, string) (*services.AIResponse, error) {
	return &services.AIResponse{Content: "fallback"}, nil
}
func (stubRuntimeFallback) ShouldTransferToHuman(query string, _ []models.Message) bool {
	return query == "transfer"
}
func (stubRuntimeFallback) GetSessionSummary(_ []models.Message) (string, error) {
	return "summary", nil
}
func (stubRuntimeFallback) GetStatus(context.Context) map[string]interface{} {
	return map[string]interface{}{"type": "fallback-runtime"}
}

func TestScopedAIRuntimeServiceProcessQueryUsesWorkspaceOpenAIOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"finish_reason": "stop",
				"message":       map[string]interface{}{"content": "runtime-scoped-response"},
			}},
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

	runtimeSvc := NewScopedAIRuntimeService(config.GetDefaultConfig(), logrus.New(), db, stubRuntimeFallback{}, nil)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	resp, err := runtimeSvc.ProcessQuery(ctx, "hello", "session-1")
	if err != nil {
		t.Fatalf("ProcessQuery() error = %v", err)
	}
	if resp == nil || resp.Content != "runtime-scoped-response" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestScopedAIRuntimeServiceDelegatesTransferAndSummaryToFallback(t *testing.T) {
	runtimeSvc := NewScopedAIRuntimeService(config.GetDefaultConfig(), logrus.New(), openScopedAITestDB(t), stubRuntimeFallback{}, nil)
	if !runtimeSvc.ShouldTransferToHuman("transfer", nil) {
		t.Fatal("expected fallback transfer decision")
	}
	summary, err := runtimeSvc.GetSessionSummary(nil)
	if err != nil || summary != "summary" {
		t.Fatalf("summary = %q err=%v", summary, err)
	}
}

func TestScopedAIRuntimeServiceBuildServiceNilReceiverAndPgvectorShortcut(t *testing.T) {
	var nilSvc *scopedAIRuntimeService
	if got := nilSvc.buildService(context.Background()); got != nil {
		t.Fatalf("nil receiver should return nil, got %#v", got)
	}

	// knowledge.provider=pgvector 时直通 fallback（启动装配的全局实例）。
	cfg := config.GetDefaultConfig()
	cfg.Knowledge.Provider = "pgvector"
	fallback := stubRuntimeFallback{}
	svc := &scopedAIRuntimeService{cfg: cfg, fallback: fallback}
	if got := svc.buildService(context.Background()); got != aidelivery.RuntimeService(fallback) {
		t.Fatalf("expected fallback passthrough for pgvector, got %#v", got)
	}
}
