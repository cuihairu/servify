package audit

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"
)

func TestComputeEntryHash(t *testing.T) {
	if got := ComputeEntryHash(ChainGenesis, nil); got != "" {
		t.Fatalf("nil row hash = %q, want empty", got)
	}

	row := &models.AuditLog{PrincipalKind: "agent", Action: "tickets.update", Route: "/api/tickets/1", Method: "PUT", StatusCode: 200, Success: true}
	base := ComputeEntryHash(ChainGenesis, row)
	if len(base) != 64 {
		t.Fatalf("hash length = %d, want 64 hex chars", len(base))
	}
	if again := ComputeEntryHash(ChainGenesis, row); again != base {
		t.Fatal("hash must be deterministic")
	}
	if prev := ComputeEntryHash("ff"+ChainGenesis[2:], row); prev == base {
		t.Fatal("prev hash must affect entry hash")
	}

	tampered := *row
	tampered.Action = "tickets.delete"
	if ComputeEntryHash(ChainGenesis, &tampered) == base {
		t.Fatal("field change must change entry hash")
	}
}

func TestChainNeedsRowLock(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		if !chainNeedsRowLock(dialect) {
			t.Fatalf("%s should use row lock", dialect)
		}
	}
	for _, dialect := range []string{"sqlite", ""} {
		if chainNeedsRowLock(dialect) {
			t.Fatalf("%s should not use row lock", dialect)
		}
	}
	if clauses := chainLockClauses(true); len(clauses) != 1 {
		t.Fatalf("lock clauses = %d, want 1", len(clauses))
	}
	if clauses := chainLockClauses(false); len(clauses) != 0 {
		t.Fatalf("no-lock clauses = %d, want 0", len(clauses))
	}
}

func TestVerifyChainNilDB(t *testing.T) {
	if _, err := VerifyChain(context.Background(), nil); err == nil {
		t.Fatal("nil db should error")
	}
	var svc *GormQueryService
	if _, err := svc.VerifyChain(context.Background()); err == nil {
		t.Fatal("nil service should error")
	}
}

// TestChainRecordAndVerify 全链强度：三条记录经 GormRecorder 落库后从
// genesis 起链校验通过；改动任意历史行都会在断点暴露。
func TestChainRecordAndVerify(t *testing.T) {
	db := openTestDB(t)
	recorder := NewGormRecorder(db)
	ctx := context.Background()
	for _, action := range []string{"a.one", "a.two", "a.three"} {
		if err := recorder.Record(ctx, Entry{PrincipalKind: "agent", Action: action, Route: "/api/x", Method: "POST", Success: true}); err != nil {
			t.Fatalf("Record(%s) error = %v", action, err)
		}
	}

	var rows []models.AuditLog
	if err := db.Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load rows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].PrevHash != ChainGenesis {
		t.Fatalf("first prev = %q, want genesis", rows[0].PrevHash)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].PrevHash != rows[i-1].EntryHash {
			t.Fatalf("row %d prev not chained to predecessor", i)
		}
	}

	svc := NewGormQueryService(db)
	report, err := svc.VerifyChain(ctx)
	if err != nil {
		t.Fatalf("VerifyChain() error = %v", err)
	}
	if !report.OK || report.Hashed != 3 || report.Total != 3 || report.LegacyRows != 0 || report.Anchored {
		t.Fatalf("full-chain report = %+v", report)
	}
	if report.FirstID != rows[0].ID || report.LastID != rows[2].ID || report.BrokenAtID != 0 || report.Reason != "" {
		t.Fatalf("report ids/reason = %+v", report)
	}

	// 篡改历史字段 → entry_hash_mismatch 断在本行
	if err := db.Model(&models.AuditLog{}).Where("id = ?", rows[1].ID).Update("action", "a.TAMPERED").Error; err != nil {
		t.Fatalf("tamper: %v", err)
	}
	report, err = svc.VerifyChain(ctx)
	if err != nil {
		t.Fatalf("VerifyChain() after field tamper error = %v", err)
	}
	if report.OK || report.BrokenAtID != rows[1].ID || report.Reason != "entry_hash_mismatch" {
		t.Fatalf("field tamper report = %+v", report)
	}
	if err := db.Model(&models.AuditLog{}).Where("id = ?", rows[1].ID).Update("action", "a.two").Error; err != nil {
		t.Fatalf("restore: %v", err)
	}

	// 直接改 prev_hash → prev_hash_mismatch
	origPrev := rows[2].PrevHash
	if err := db.Model(&models.AuditLog{}).Where("id = ?", rows[2].ID).Update("prev_hash", "ab").Error; err != nil {
		t.Fatalf("tamper prev: %v", err)
	}
	report, _ = svc.VerifyChain(ctx)
	if report.OK || report.BrokenAtID != rows[2].ID || report.Reason != "prev_hash_mismatch" {
		t.Fatalf("prev tamper report = %+v", report)
	}
	if err := db.Model(&models.AuditLog{}).Where("id = ?", rows[2].ID).Update("prev_hash", origPrev).Error; err != nil {
		t.Fatalf("restore prev: %v", err)
	}

	// 删中间行：后行 prev_hash 指向被删行哈希，重算链必然断裂
	if err := db.Delete(&models.AuditLog{}, rows[1].ID).Error; err != nil {
		t.Fatalf("delete middle: %v", err)
	}
	report, _ = svc.VerifyChain(ctx)
	if report.OK || report.BrokenAtID != rows[2].ID || report.Reason != "prev_hash_mismatch" {
		t.Fatalf("middle delete report = %+v", report)
	}
}

// TestChainLegacyRowsAnchored 存量（未哈希）行不参与校验，链在其后重新锚定。
func TestChainLegacyRowsAnchored(t *testing.T) {
	db := openTestDB(t)
	for _, action := range []string{"legacy.one", "legacy.two"} {
		if err := db.Create(&models.AuditLog{PrincipalKind: "agent", Action: action, Route: "/api/x", Method: "POST", Success: true}).Error; err != nil {
			t.Fatalf("seed legacy: %v", err)
		}
	}
	recorder := NewGormRecorder(db)
	if err := recorder.Record(context.Background(), Entry{PrincipalKind: "agent", Action: "new.one", Route: "/api/x", Method: "POST", Success: true}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	report, err := NewGormQueryService(db).VerifyChain(context.Background())
	if err != nil {
		t.Fatalf("VerifyChain() error = %v", err)
	}
	if !report.OK || report.Hashed != 1 || report.LegacyRows != 2 || !report.Anchored {
		t.Fatalf("legacy report = %+v", report)
	}
}

// TestChainTailErrorPropagation 链尾读取失败（表被删）原样透传，不吞错。
func TestChainTailErrorPropagation(t *testing.T) {
	db := openTestDB(t)
	if err := db.Migrator().DropTable(&models.AuditLog{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if err := NewGormRecorder(db).Record(context.Background(), Entry{Action: "x"}); err == nil {
		t.Fatal("Record() on dropped table should fail")
	}
	if _, err := NewGormQueryService(db).VerifyChain(context.Background()); err == nil {
		t.Fatal("VerifyChain() on dropped table should fail")
	}
	if _, err := VerifyChain(context.Background(), db); err == nil {
		t.Fatal("VerifyChain() free fn on dropped table should fail")
	}
}
