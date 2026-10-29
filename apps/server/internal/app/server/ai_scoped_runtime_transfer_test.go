package server

// 覆盖补充：ProcessQuery 命中转人工意图且会话 scope 解析成功时，请求 ctx
// 被替换为会话作用域 ctx（ai_scoped_runtime.go 的 ctx = sessionScope 分支）。
// 现有用例只覆盖了 toolContext 报错路径（缺会话）。

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	routingapp "servify/apps/server/internal/modules/routing/application"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	routinginfra "servify/apps/server/internal/modules/routing/infra"

	"github.com/sirupsen/logrus"
)

func TestScopedAIRuntimeServiceProcessQueryAppliesSessionScope(t *testing.T) {
	db := openScopedAITestDB(t)
	if err := db.Create(&models.Session{ID: "sess-tf", TenantID: "tenant-a", WorkspaceID: "workspace-1", UserID: 7, Status: "active", StartedAt: time.Now()}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	handoff := routingdelivery.NewSessionTransferAdapter(routingapp.NewService(routinginfra.NewGormRepository(db), nil), nil)

	// pgvector 直通 fallback：buildService 返回 stub，无外部调用。
	cfg := config.GetDefaultConfig()
	cfg.Knowledge.Provider = "pgvector"
	runtimeSvc := NewScopedAIRuntimeService(cfg, logrus.New(), db, stubRuntimeFallback{}, nil, handoff)

	// 无请求级 scope 的 ctx：转人工意图命中后由会话行补齐 tenant/workspace。
	resp, err := runtimeSvc.ProcessQuery(context.Background(), "请帮我转人工客服", "sess-tf")
	if err != nil {
		t.Fatalf("ProcessQuery() error = %v", err)
	}
	if resp == nil || resp.Content != "fallback" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
