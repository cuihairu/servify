package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	platformauth "servify/apps/server/internal/platform/auth"
	kpmock "servify/apps/server/internal/platform/knowledgeprovider/mock"
	llm "servify/apps/server/internal/platform/llm"
	llmmock "servify/apps/server/internal/platform/llm/mock"

	"github.com/sirupsen/logrus"
)

type stubRuntimeFallback struct{}

func (stubRuntimeFallback) ProcessQuery(context.Context, string, string) (*aidelivery.AIResponse, error) {
	return &aidelivery.AIResponse{Content: "fallback"}, nil
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

// TestScopedAIRuntimeServiceProcessQueryStream 流式首答透传：pgvector
// 直通 fallback 时委托具备流式能力的编排实例（增量+终帧完整首答）；
// fallback 无流式能力或 nil 接收者时显式报错，调用方回退非流式路径。
func TestScopedAIRuntimeServiceProcessQueryStream(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Knowledge.Provider = "pgvector"

	provider := &llmmock.Provider{StreamChunks: []llm.ChatChunk{
		{ContentDelta: "您好"},
		{ContentDelta: "，正在查询。"},
		{Done: true},
	}}
	fallback := aidelivery.NewOrchestratedEnhancedAIService(
		aidelivery.NewAIService("", ""),
		provider,
		&kpmock.Provider{},
		"",
		nil,
	)
	svc := &scopedAIRuntimeService{cfg: cfg, fallback: fallback}

	stream, err := svc.ProcessQueryStream(context.Background(), "物流进度", "sess-scoped-stream")
	if err != nil {
		t.Fatalf("ProcessQueryStream() error = %v", err)
	}
	var deltas []string
	var final *aidelivery.AIResponse
	for evt := range stream {
		if evt.Done {
			final = evt.Final
		} else {
			deltas = append(deltas, evt.ContentDelta)
		}
	}
	if strings.Join(deltas, "") != "您好，正在查询。" || final == nil || final.Content != "您好，正在查询。" {
		t.Fatalf("deltas=%q final=%+v", deltas, final)
	}

	// 无流式能力的 fallback：显式报错。
	plain := &scopedAIRuntimeService{cfg: cfg, fallback: stubRuntimeFallback{}}
	if _, err := plain.ProcessQueryStream(context.Background(), "q", "s"); err == nil {
		t.Fatal("non-streamer fallback must error")
	}

	// nil 接收者。
	var nilSvc *scopedAIRuntimeService
	if _, err := nilSvc.ProcessQueryStream(context.Background(), "q", "s"); err == nil {
		t.Fatal("nil receiver must error")
	}
}
