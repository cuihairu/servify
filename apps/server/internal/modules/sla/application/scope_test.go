package application

// scope 过滤行为对账（自 services TestScopeHelpers 迁入，覆盖本包复刻的
// tenantAndWorkspace/applyScopeFilter 与 services 原件同语义）。

import (
	"context"
	"testing"
)

func TestScopeHelpers(t *testing.T) {
	if tenant, ws := tenantAndWorkspace(context.Background()); tenant != "" || ws != "" {
		t.Fatalf("expected empty scope, got %q/%q", tenant, ws)
	}
	if tenant, ws := tenantAndWorkspace(unitScopedContext("t1", "w1")); tenant != "t1" || ws != "w1" {
		t.Fatalf("expected t1/w1, got %q/%q", tenant, ws)
	}

	db := newSLATestDB(t, &testScopeModel{})
	sealed := db.Where("1 = 1")
	out := applyScopeFilter(sealed, unitScopedContext("t1", "w1"))
	if err := out.Find(&[]testScopeModel{}).Error; err != nil {
		t.Fatalf("scoped query failed: %v", err)
	}
	out = applyScopeFilter(sealed, unitScopedContext("t1", ""))
	if err := out.Find(&[]testScopeModel{}).Error; err != nil {
		t.Fatalf("tenant-only query failed: %v", err)
	}
	out = applyScopeFilter(sealed, context.Background())
	if err := out.Find(&[]testScopeModel{}).Error; err != nil {
		t.Fatalf("unscoped query failed: %v", err)
	}
}

type testScopeModel struct {
	ID          uint   `gorm:"primaryKey"`
	TenantID    string `gorm:"index"`
	WorkspaceID string `gorm:"index"`
}
