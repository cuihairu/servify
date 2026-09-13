package infra

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var suggestionInfraDBSeq atomic.Uint64

func newSuggestionInfraTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:sugginfra_" + strings.ReplaceAll(t.Name(), "/", "_") + "_" + strconv.FormatUint(suggestionInfraDBSeq.Add(1), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.Ticket{}, &models.KnowledgeDoc{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return db
}

func TestGormSuggestionRepoConstruction(t *testing.T) {
	if NewGormRepository(newSuggestionInfraTestDB(t)) == nil {
		t.Fatal("expected repository instance")
	}
}

func TestGormSuggestionFindTicketCandidates(t *testing.T) {
	db := newSuggestionInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	tickets := []models.Ticket{
		{ID: 1, Title: "printer broken", Description: "cannot print invoices", CustomerID: 7, Status: "open", Category: "hardware", Priority: "high", TenantID: "t1", CreatedAt: base, UpdatedAt: base},
		{ID: 2, Title: "login issue", Description: "sso redirect loop", CustomerID: 7, Status: "open", Category: "account", Priority: "normal", TenantID: "t1", CreatedAt: base.Add(time.Hour), UpdatedAt: base.Add(time.Hour)},
	}
	if err := db.Create(&tickets).Error; err != nil {
		t.Fatalf("seed tickets: %v", err)
	}

	rows, err := repo.FindTicketCandidates(ctx, []string{"printer"}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != 1 || rows[0].Title != "printer broken" || rows[0].Status != "open" {
		t.Fatalf("unexpected candidates: %+v", rows)
	}

	// 描述命中
	rows, err = repo.FindTicketCandidates(ctx, []string{"sso"}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != 2 {
		t.Fatalf("expected description match, got %+v", rows)
	}

	// 租户隔离
	scoped := platformauth.ContextWithScope(ctx, "t-other", "")
	rows, err = repo.FindTicketCandidates(scoped, []string{"printer"}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no cross-tenant candidates, got %+v", rows)
	}

	if err := db.Migrator().DropTable(&models.Ticket{}); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if _, err := repo.FindTicketCandidates(ctx, []string{"printer"}, 10); err == nil {
		t.Fatal("expected query error after dropping tickets")
	}
}

func TestGormSuggestionFindKnowledgeDocCandidates(t *testing.T) {
	db := newSuggestionInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	docs := []models.KnowledgeDoc{
		{ID: 1, Title: "refund policy", Content: "how to refund within 30 days", Category: "billing", Tags: "refund,money", TenantID: "t1", CreatedAt: base, UpdatedAt: base},
		{ID: 2, Title: "vpn setup", Content: "install vpn client", Category: "it", Tags: "network", TenantID: "t1", CreatedAt: base.Add(time.Hour), UpdatedAt: base.Add(time.Hour)},
	}
	if err := db.Create(&docs).Error; err != nil {
		t.Fatalf("seed docs: %v", err)
	}

	// 标题命中
	rows, err := repo.FindKnowledgeDocCandidates(ctx, []string{"refund"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != 1 || rows[0].Category != "billing" || rows[0].Tags != "refund,money" {
		t.Fatalf("unexpected doc candidates: %+v", rows)
	}

	// 内容命中
	rows, err = repo.FindKnowledgeDocCandidates(ctx, []string{"vpn"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != 2 {
		t.Fatalf("expected content match, got %+v", rows)
	}

	// 工作区隔离
	scoped := platformauth.ContextWithScope(ctx, "", "ws-404")
	rows, err = repo.FindKnowledgeDocCandidates(scoped, []string{"refund"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no cross-workspace candidates, got %+v", rows)
	}

	if err := db.Migrator().DropTable(&models.KnowledgeDoc{}); err != nil {
		t.Fatalf("drop docs: %v", err)
	}
	if _, err := repo.FindKnowledgeDocCandidates(ctx, []string{"refund"}); err == nil {
		t.Fatal("expected query error after dropping docs")
	}
}
