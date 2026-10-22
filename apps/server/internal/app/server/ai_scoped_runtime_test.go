package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	routingapp "servify/apps/server/internal/modules/routing/application"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	routinginfra "servify/apps/server/internal/modules/routing/infra"
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

	runtimeSvc := NewScopedAIRuntimeService(config.GetDefaultConfig(), logrus.New(), db, stubRuntimeFallback{}, nil, nil)
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
	runtimeSvc := NewScopedAIRuntimeService(config.GetDefaultConfig(), logrus.New(), openScopedAITestDB(t), stubRuntimeFallback{}, nil, nil)
	if !runtimeSvc.ShouldTransferToHuman("transfer", nil) {
		t.Fatal("expected fallback transfer decision")
	}
	summary, err := runtimeSvc.GetSessionSummary(nil)
	if err != nil || summary != "summary" {
		t.Fatalf("summary = %q err=%v", summary, err)
	}
}

func TestScopedAIAgentToolPortsUseSessionScope(t *testing.T) {
	db := openScopedAITestDB(t)
	session := models.Session{ID: "sess-tools", TenantID: "tenant-a", WorkspaceID: "workspace-1", UserID: 7, Status: "active", StartedAt: time.Now()}
	customer := models.Customer{ID: 7, TenantID: "tenant-a", WorkspaceID: "workspace-1", UserID: 7, Company: "Acme", Priority: "high"}
	ticket := models.Ticket{ID: 3, TenantID: "tenant-a", WorkspaceID: "workspace-1", CustomerID: 7, Title: " outage", Status: "open"}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := db.Create(&customer).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := db.Create(&ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}

	handoff := routingapp.NewService(routinginfra.NewGormRepository(db), nil)
	ports := scopedAgentToolPorts{db: db, session: session.ID, handoff: routingdelivery.NewSessionTransferAdapter(handoff, nil)}
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	customerSummary, err := ports.GetCustomerSummary(ctx, customer.ID)
	if err != nil {
		t.Fatalf("customer lookup: %v", err)
	}
	if customerSummary["company"] != "Acme" || customerSummary["priority"] != "high" {
		t.Fatalf("unexpected customer summary: %+v", customerSummary)
	}
	ticketSummary, err := ports.GetTicketSummary(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("ticket lookup: %v", err)
	}
	if ticketSummary["id"] != ticket.ID || ticketSummary["customer_id"] != ticket.CustomerID {
		t.Fatalf("unexpected ticket summary: %+v", ticketSummary)
	}

	handoffResult, err := ports.RequestHandoff(ctx, session.ID, "请帮我转人工客服")
	if err != nil {
		t.Fatalf("request handoff: %v", err)
	}
	if handoffResult["status"] != "waiting" || handoffResult["queued"] != true {
		t.Fatalf("unexpected handoff result: %+v", handoffResult)
	}
	replayResult, err := ports.RequestHandoff(ctx, session.ID, "请再转一次")
	if err != nil {
		t.Fatalf("replay handoff: %v", err)
	}
	if replayResult["queued_at"] != handoffResult["queued_at"] {
		t.Fatalf("handoff replay must be idempotent: first=%+v second=%+v", handoffResult, replayResult)
	}
	var waitingRecords []models.WaitingRecord
	if err := db.Find(&waitingRecords).Error; err != nil {
		t.Fatalf("load waiting records: %v", err)
	}
	if len(waitingRecords) != 1 || waitingRecords[0].TenantID != "tenant-a" || waitingRecords[0].WorkspaceID != "workspace-1" {
		t.Fatalf("unexpected waiting records: %+v", waitingRecords)
	}
}

func TestScopedAIAgentToolPortsRejectForeignSessionScope(t *testing.T) {
	db := openScopedAITestDB(t)
	if err := db.Create(&models.Session{ID: "sess-foreign", TenantID: "tenant-a", WorkspaceID: "workspace-1", UserID: 7, Status: "active", StartedAt: time.Now()}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := db.Create(&models.Customer{ID: 7, TenantID: "tenant-b", WorkspaceID: "workspace-2", UserID: 7}).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	ports := scopedAgentToolPorts{db: db, session: "sess-foreign"}
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-b", "workspace-2")
	if _, err := ports.GetCustomerSummary(ctx, 7); err == nil || !strings.Contains(err.Error(), "tenant scope mismatch") {
		t.Fatalf("expected tenant scope mismatch, got %v", err)
	}
	if _, err := ports.RequestHandoff(ctx, "sess-foreign", "user request"); err == nil || !strings.Contains(err.Error(), "tenant scope mismatch") {
		t.Fatalf("expected handoff tenant scope mismatch, got %v", err)
	}
	if _, err := ports.RequestHandoff(ctx, "sess-other", "user request"); err == nil || !strings.Contains(err.Error(), "conversation mismatch") {
		t.Fatalf("expected conversation mismatch, got %v", err)
	}
}

func TestScopedAIRuntimeServiceMissingSessionBlocksHandoffCapableQuery(t *testing.T) {
	db := openScopedAITestDB(t)
	handoff := routingdelivery.NewSessionTransferAdapter(routingapp.NewService(routinginfra.NewGormRepository(db), nil), nil)
	runtimeSvc := NewScopedAIRuntimeService(config.GetDefaultConfig(), logrus.New(), db, stubRuntimeFallback{}, nil, handoff)
	resp, err := runtimeSvc.ProcessQuery(context.Background(), "请帮我转人工客服", "missing-session")
	if err == nil || !strings.Contains(err.Error(), "load agent session scope") {
		t.Fatalf("expected missing-session scope failure, got resp=%+v err=%v", resp, err)
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
