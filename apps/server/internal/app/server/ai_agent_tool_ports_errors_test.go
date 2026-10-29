package server

// 覆盖补充：scopedAgentToolPorts 的错误/防御分支——客户/工单加载失败、
// 会话 scope 缺失、workspace 不匹配、handoff 队列查询/入队失败，以及
// truncateAgentSummary 超限截断。惯例参照 ai_scoped_runtime_test.go。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

// errHandoffRuntime 只覆写 GetWaitingRecord / AddToWaitingQueue 的注入失败
// stub；其余方法不在这两条路径上被调用。
type errHandoffRuntime struct {
	routingdelivery.RuntimeService
	getErr error
	addErr error
	record *models.WaitingRecord
}

func (h errHandoffRuntime) GetWaitingRecord(context.Context, string) (*models.WaitingRecord, error) {
	if h.getErr != nil {
		return nil, h.getErr
	}
	return h.record, nil
}

func (h errHandoffRuntime) AddToWaitingQueue(context.Context, *gorm.DB, string, string, []string, uint, string, string) (*models.WaitingRecord, error) {
	if h.addErr != nil {
		return nil, h.addErr
	}
	return h.record, nil
}

func newAgentPortsTestSession(t *testing.T, db *gorm.DB, id, tenant, workspace string) {
	t.Helper()
	if err := db.Create(&models.Session{ID: id, TenantID: tenant, WorkspaceID: workspace, UserID: 7, Status: "active", StartedAt: time.Now()}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

// TestScopedAIAgentToolPortsLookupErrors 覆盖客户/工单加载失败与会话
// scope 防御分支：目标行不存在、会话 scope 字段缺失、workspace 不匹配。
func TestScopedAIAgentToolPortsLookupErrors(t *testing.T) {
	db := openScopedAITestDB(t)
	newAgentPortsTestSession(t, db, "sess-errors", "tenant-a", "workspace-1")
	ports := scopedAgentToolPorts{db: db, session: "sess-errors"}

	// 目标客户/工单不存在 → 加载失败上抛。
	if _, err := ports.GetCustomerSummary(context.Background(), 999); err == nil || !strings.Contains(err.Error(), "load customer") {
		t.Fatalf("expected load customer failure, got %v", err)
	}
	if _, err := ports.GetTicketSummary(context.Background(), 999); err == nil || !strings.Contains(err.Error(), "load ticket") {
		t.Fatalf("expected load ticket failure, got %v", err)
	}

	// 会话 tenant/workspace 缺失 → scope 不完整拒绝。
	newAgentPortsTestSession(t, db, "sess-blank", "", "")
	blankPorts := scopedAgentToolPorts{db: db, session: "sess-blank"}
	if _, err := blankPorts.GetCustomerSummary(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "agent session scope is incomplete") {
		t.Fatalf("expected incomplete scope failure, got %v", err)
	}

	// 请求级 workspace 与会话不一致 → workspace scope mismatch。
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-9")
	if _, err := ports.GetCustomerSummary(ctx, 1); err == nil || !strings.Contains(err.Error(), "workspace scope mismatch") {
		t.Fatalf("expected workspace scope mismatch, got %v", err)
	}
}

// TestScopedAIAgentToolPortsHandoffQueueErrors 覆盖 handoff 队列查询失败
// （非 ErrRecordNotFound）与入队失败分支。
func TestScopedAIAgentToolPortsHandoffQueueErrors(t *testing.T) {
	db := openScopedAITestDB(t)
	newAgentPortsTestSession(t, db, "sess-handoff-err", "tenant-a", "workspace-1")
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")

	getErrPorts := scopedAgentToolPorts{db: db, session: "sess-handoff-err", handoff: errHandoffRuntime{getErr: errors.New("queue down")}}
	if _, err := getErrPorts.RequestHandoff(ctx, "sess-handoff-err", "转人工"); err == nil || !strings.Contains(err.Error(), "check handoff queue") {
		t.Fatalf("expected check handoff queue failure, got %v", err)
	}

	addErrPorts := scopedAgentToolPorts{db: db, session: "sess-handoff-err", handoff: errHandoffRuntime{addErr: errors.New("enqueue down")}}
	if _, err := addErrPorts.RequestHandoff(ctx, "sess-handoff-err", "转人工"); err == nil || !strings.Contains(err.Error(), "request handoff") {
		t.Fatalf("expected request handoff failure, got %v", err)
	}
}

// TestTruncateAgentSummaryOverflow 覆盖超限截断分支（中文多字节 rune
// 不被截半）。
func TestTruncateAgentSummaryOverflow(t *testing.T) {
	long := strings.Repeat("客", 1001)
	got := truncateAgentSummary(long, 1000)
	if got != strings.Repeat("客", 1000)+"…" {
		t.Fatalf("unexpected truncation: %q", got)
	}
	if truncateAgentSummary("short", 1000) != "short" {
		t.Fatal("short value must pass through")
	}
}
