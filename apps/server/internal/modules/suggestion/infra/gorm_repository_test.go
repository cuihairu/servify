package infra

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	suggestionapp "servify/apps/server/internal/modules/suggestion/application"
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
	if err := db.AutoMigrate(&models.Ticket{}, &models.KnowledgeDoc{}, &models.SuggestionExposureLog{}); err != nil {
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

func TestGormSuggestionFindPublicKnowledgeDocs(t *testing.T) {
	db := newSuggestionInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	base := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	docs := []models.KnowledgeDoc{
		{ID: 1, Title: "private refund policy", ProviderID: "p", ExternalID: "e1", TenantID: "t1", IsPublic: false, CreatedAt: base, UpdatedAt: base.Add(time.Hour)},
		{ID: 2, Title: "public billing faq", ProviderID: "p", ExternalID: "e2", TenantID: "t1", IsPublic: true, CreatedAt: base, UpdatedAt: base},
		{ID: 3, Title: "public account faq", ProviderID: "p", ExternalID: "e3", TenantID: "t1", IsPublic: true, CreatedAt: base, UpdatedAt: base.Add(2 * time.Hour)},
	}
	if err := db.Create(&docs).Error; err != nil {
		t.Fatalf("seed docs: %v", err)
	}

	// 仅公开文档，updated_at 新近优先
	rows, err := repo.FindPublicKnowledgeDocs(ctx, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 || rows[0].ID != 3 || rows[1].ID != 2 {
		t.Fatalf("expected public docs by recency, got %+v", rows)
	}

	// limit 截断
	rows, err = repo.FindPublicKnowledgeDocs(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != 3 {
		t.Fatalf("expected limit 1 newest, got %+v", rows)
	}

	// 租户隔离
	scoped := platformauth.ContextWithScope(ctx, "t-other", "")
	rows, err = repo.FindPublicKnowledgeDocs(scoped, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no cross-tenant docs, got %+v", rows)
	}
}

func TestGormSuggestionFindPublicKnowledgeDocCandidates(t *testing.T) {
	db := newSuggestionInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	base := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	docs := []models.KnowledgeDoc{
		{ID: 1, Title: "refund guide", Content: "how to refund", ProviderID: "p", ExternalID: "e1", TenantID: "t1", IsPublic: true, CreatedAt: base, UpdatedAt: base},
		{ID: 2, Title: "secret refund policy", Content: "internal only", ProviderID: "p", ExternalID: "e2", TenantID: "t1", IsPublic: false, CreatedAt: base, UpdatedAt: base},
	}
	if err := db.Create(&docs).Error; err != nil {
		t.Fatalf("seed docs: %v", err)
	}

	// 空 token 直接空集（不扫表）
	rows, err := repo.FindPublicKnowledgeDocCandidates(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected empty result for empty tokens, got %+v", rows)
	}

	// 命中仅限公开文档
	rows, err = repo.FindPublicKnowledgeDocCandidates(ctx, []string{"refund"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != 1 {
		t.Fatalf("expected only public doc, got %+v", rows)
	}

	// 工作区隔离
	scoped := platformauth.ContextWithScope(ctx, "", "ws-404")
	rows, err = repo.FindPublicKnowledgeDocCandidates(scoped, []string{"refund"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no cross-workspace docs, got %+v", rows)
	}
}

func TestGormSuggestionFindPublicKnowledgeDocs_QueryError(t *testing.T) {
	db := newSuggestionInfraTestDB(t)
	repo := NewGormRepository(db)
	if err := db.Migrator().DropTable("knowledge_docs"); err != nil {
		t.Fatalf("drop docs: %v", err)
	}
	if _, err := repo.FindPublicKnowledgeDocs(context.Background(), 10); err == nil {
		t.Fatal("expected query error after dropping docs")
	}
}

func TestGormSuggestionFindPublicKnowledgeDocCandidates_QueryError(t *testing.T) {
	db := newSuggestionInfraTestDB(t)
	repo := NewGormRepository(db)
	if err := db.Migrator().DropTable("knowledge_docs"); err != nil {
		t.Fatalf("drop docs: %v", err)
	}
	if _, err := repo.FindPublicKnowledgeDocCandidates(context.Background(), []string{"refund"}); err == nil {
		t.Fatal("expected query error after dropping docs")
	}
}

func TestGormSuggestionExposureLifecycle(t *testing.T) {
	db := newSuggestionInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	base := time.Date(2026, 6, 3, 8, 0, 0, 0, time.UTC)

	// 无曝光时返回 nil, nil（非错误）
	open, err := repo.FindLatestOpenExposure(ctx, "s-1")
	if err != nil || open != nil {
		t.Fatalf("FindLatestOpenExposure on empty table = (%v, %v), want (nil, nil)", open, err)
	}

	// 落两行曝光：JSON 数组序列化/反序列化 roundtrip
	for _, rec := range []suggestionapp.ExposureRecord{
		{SessionID: "s-1", Kind: "initial", Strategy: "public_knowledge_recency", Questions: []string{"Q1", "Q2"}},
		{SessionID: "s-1", Kind: "next", Strategy: "public_knowledge_scored", Questions: []string{"Q3"}},
	} {
		if err := repo.RecordExposure(ctx, rec); err != nil {
			t.Fatalf("RecordExposure(%+v): %v", rec, err)
		}
	}
	// 固定 created_at 时序，规避 sqlite 时间精度抖动
	if err := db.Model(&models.SuggestionExposureLog{}).Where("id = ?", 1).Update("created_at", base).Error; err != nil {
		t.Fatalf("set created_at: %v", err)
	}
	if err := db.Model(&models.SuggestionExposureLog{}).Where("id = ?", 2).Update("created_at", base.Add(time.Hour)).Error; err != nil {
		t.Fatalf("set created_at: %v", err)
	}

	open, err = repo.FindLatestOpenExposure(ctx, "s-1")
	if err != nil {
		t.Fatalf("FindLatestOpenExposure: %v", err)
	}
	if open == nil || open.ID != 2 || len(open.Questions) != 1 || open.Questions[0] != "Q3" {
		t.Fatalf("latest open = %+v, want id 2 with [Q3]", open)
	}

	// session 隔离：其他 session 查不到
	if open, err := repo.FindLatestOpenExposure(ctx, "s-other"); err != nil || open != nil {
		t.Fatalf("cross-session open = (%v, %v), want (nil, nil)", open, err)
	}

	// 转化后该行退出 open 集合，回退到上一次未转化曝光
	if err := repo.MarkExposureConverted(ctx, 2, "Q3", base.Add(2*time.Hour)); err != nil {
		t.Fatalf("MarkExposureConverted: %v", err)
	}
	open, err = repo.FindLatestOpenExposure(ctx, "s-1")
	if err != nil {
		t.Fatalf("FindLatestOpenExposure after convert: %v", err)
	}
	if open == nil || open.ID != 1 {
		t.Fatalf("latest open after convert = %+v, want id 1", open)
	}

	// converted_question = '' 守卫：已转化行不可被二次归因覆盖
	if err := repo.MarkExposureConverted(ctx, 2, "Q3-again", base.Add(3*time.Hour)); err != nil {
		t.Fatalf("second MarkExposureConverted: %v", err)
	}
	var row models.SuggestionExposureLog
	if err := db.First(&row, 2).Error; err != nil {
		t.Fatalf("reload row: %v", err)
	}
	if row.ConvertedQuestion != "Q3" || !row.ConvertedAt.Equal(base.Add(2*time.Hour)) {
		t.Fatalf("converted row mutated: %+v", row)
	}
}

func TestGormSuggestionExposureCorruptQuestions(t *testing.T) {
	db := newSuggestionInfraTestDB(t)
	repo := NewGormRepository(db)
	if err := db.Create(&models.SuggestionExposureLog{
		SessionID: "s-x", Kind: "initial", Strategy: "st",
		Questions: "{oops", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed corrupt row: %v", err)
	}
	if _, err := repo.FindLatestOpenExposure(context.Background(), "s-x"); err == nil {
		t.Fatal("expected unmarshal error for corrupt questions JSON")
	}
}

func TestGormSuggestionExposureSummary(t *testing.T) {
	db := newSuggestionInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	// 空表：零值聚合
	empty, err := repo.ExposureSummary(ctx)
	if err != nil {
		t.Fatalf("ExposureSummary on empty table: %v", err)
	}
	if empty.TotalExposures != 0 || empty.ConvertedExposures != 0 || len(empty.ByKind) != 0 {
		t.Fatalf("empty summary = %+v", empty)
	}

	// 4 行曝光：initial 2 行（1 转化）、next 2 行（2 转化）
	for _, rec := range []suggestionapp.ExposureRecord{
		{SessionID: "s-1", Kind: "initial", Questions: []string{"Q1"}},
		{SessionID: "s-1", Kind: "initial", Questions: []string{"Q2"}},
		{SessionID: "s-1", Kind: "next", Questions: []string{"Q3"}},
		{SessionID: "s-1", Kind: "next", Questions: []string{"Q4"}},
	} {
		if err := repo.RecordExposure(ctx, rec); err != nil {
			t.Fatalf("RecordExposure(%+v): %v", rec, err)
		}
	}
	now := time.Now()
	for _, id := range []uint{1, 3, 4} {
		if err := repo.MarkExposureConverted(ctx, id, "Q", now); err != nil {
			t.Fatalf("MarkExposureConverted(%d): %v", id, err)
		}
	}

	summary, err := repo.ExposureSummary(ctx)
	if err != nil {
		t.Fatalf("ExposureSummary: %v", err)
	}
	if summary.TotalExposures != 4 || summary.ConvertedExposures != 3 {
		t.Fatalf("summary totals = %+v, want 4/3", summary)
	}
	byKind := make(map[string]suggestionapp.ExposureKindSummary, len(summary.ByKind))
	for _, k := range summary.ByKind {
		byKind[k.Kind] = k
	}
	if len(byKind) != 2 {
		t.Fatalf("by-kind groups = %+v, want 2", summary.ByKind)
	}
	if byKind["initial"].TotalExposures != 2 || byKind["initial"].ConvertedExposures != 1 {
		t.Fatalf("initial group = %+v", byKind["initial"])
	}
	if byKind["next"].TotalExposures != 2 || byKind["next"].ConvertedExposures != 2 {
		t.Fatalf("next group = %+v", byKind["next"])
	}
}

func TestGormSuggestionExposure_QueryErrors(t *testing.T) {
	tests := []struct {
		name string
		call func(repo *GormRepository) error
	}{
		{"record", func(repo *GormRepository) error {
			return repo.RecordExposure(context.Background(), suggestionapp.ExposureRecord{Kind: "initial"})
		}},
		{"find latest open", func(repo *GormRepository) error {
			_, err := repo.FindLatestOpenExposure(context.Background(), "s-1")
			return err
		}},
		{"mark converted", func(repo *GormRepository) error {
			return repo.MarkExposureConverted(context.Background(), 1, "Q", time.Now())
		}},
		{"summary", func(repo *GormRepository) error {
			_, err := repo.ExposureSummary(context.Background())
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newSuggestionInfraTestDB(t)
			repo := NewGormRepository(db)
			if err := db.Migrator().DropTable("suggestion_exposure_logs"); err != nil {
				t.Fatalf("drop table: %v", err)
			}
			if err := tt.call(repo); err == nil {
				t.Fatal("expected query error after dropping table")
			}
		})
	}
}
