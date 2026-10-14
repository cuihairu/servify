package application

// 纯函数与不经仓储的服务内部分支测试（GORM 写归属下沉 infra 后，与仓储无关的
// 分支留本包；db 型测试随 SQL 细节移至 infra）。

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"
)

func unitScopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func intPtr(v int) *int { return &v }

func TestSLAUnit_Helpers(t *testing.T) {
	if joinTags(nil) != "" || joinTags([]string{" ", ""}) != "" {
		t.Fatal("joinTags empty handling")
	}
	if got := joinTags([]string{" a ", "b"}); got != "a,b" {
		t.Fatalf("joinTags = %q", got)
	}
	if normalizeTier("  VIP ") != "vip" {
		t.Fatal("normalizeTier")
	}
	if defaultWarning(nil) != 80 || defaultWarning(intPtr(40)) != 50 || defaultWarning(intPtr(90)) != 90 || defaultWarning(intPtr(150)) != 100 {
		t.Fatal("defaultWarning")
	}

	ctx := slaRecordScopeContext(context.Background(), "r-t", "r-w")
	if tenant, ws := tenantAndWorkspace(ctx); tenant != "r-t" || ws != "r-w" {
		t.Fatalf("record scope fallback: %q/%q", tenant, ws)
	}
	scoped := slaRecordScopeContext(unitScopedContext("c-t", "c-w"), "r-t", "r-w")
	if tenant, ws := tenantAndWorkspace(scoped); tenant != "c-t" || ws != "c-w" {
		t.Fatalf("record scope keeps context: %q/%q", tenant, ws)
	}
}

func TestSLA_DetectViolation(t *testing.T) {
	svc := NewService(&stubRepo{}, nil)
	now := time.Now()
	cfg := &models.SLAConfig{ID: 1, FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30}

	// zero CreatedAt falls back to now -> no violation
	zero := &models.Ticket{ID: 1, Status: "open"}
	if v := svc.detectViolation(zero, cfg, now); v != nil {
		t.Fatalf("expected no violation, got %+v", v)
	}

	// 未分配坐席且超过首响期限 -> first_response
	late := &models.Ticket{ID: 2, Status: "open", CreatedAt: now.Add(-10 * time.Minute)}
	if v := svc.detectViolation(late, cfg, now); v == nil || v.ViolationType != "first_response" {
		t.Fatalf("expected first_response, got %+v", v)
	}

	// 已分配坐席 -> 不触发首响；超解决期限 -> resolution
	assigned := &models.Ticket{ID: 3, Status: "open", AgentID: uintPtr(9), CreatedAt: now.Add(-2 * time.Hour)}
	if v := svc.detectViolation(assigned, cfg, now); v == nil || v.ViolationType != "resolution" {
		t.Fatalf("expected resolution, got %+v", v)
	}

	// 已分配且已关闭工单 -> 首响不触发（已分配），解决违约被状态闸住
	closed := &models.Ticket{ID: 4, Status: "closed", AgentID: uintPtr(9), CreatedAt: now.Add(-2 * time.Hour)}
	if v := svc.detectViolation(closed, cfg, now); v != nil {
		t.Fatalf("expected no violation for closed ticket, got %+v", v)
	}
}

func TestSLA_ResolveCustomerTier(t *testing.T) {
	// 仓储错误按「无等级」处理（旧行为：任何查询失败返回空串）
	errRepo := &stubRepo{customerPriorityErr: errBoom}
	svc := NewService(errRepo, nil)
	if got := svc.resolveCustomerTier(context.Background(), 42); got != "" {
		t.Fatalf("expected empty tier on error, got %q", got)
	}

	if got := svc.resolveCustomerTier(context.Background(), 0); got != "" {
		t.Fatalf("expected empty tier for zero user id, got %q", got)
	}

	ok := &stubRepo{customerPriority: "  VIP "}
	if got := NewService(ok, nil).resolveCustomerTier(context.Background(), 42); got != "vip" {
		t.Fatalf("expected normalized vip, got %q", got)
	}
}
