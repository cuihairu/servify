package infra

// 裸模式（无 integration tag）单测：来源登记仓储的 Get/Delete 未命中与
// 查询错误分支、CountDocuments 删除守卫（scope 过滤 + 计数口径）、索引
// 任务按文档列表的排序与 limit。沿用既有约定：查询错误用 drop-table 强制
// 触发（见 gorm_repository_unit_test.go / source_repository_extra_test.go）。

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"servify/apps/server/internal/modules/knowledge/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

func TestGormSourcePlainGetMatrixAndDelete(t *testing.T) {
	db := newSourceRepoTestDB(t)
	repo := NewGormSourceRepository(db)
	seedCtx := platformauth.ContextWithScope(context.Background(), "t1", "w1")

	source := &domain.Source{Name: "FAQ", Type: "faq", Description: "d"}
	if err := repo.Create(seedCtx, source); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.Get(seedCtx, source.ID)
	if err != nil || got.Name != "FAQ" || got.Type != "faq" {
		t.Fatalf("get = %+v err %v", got, err)
	}
	otherWorkspace := platformauth.ContextWithScope(context.Background(), "t1", "w2")
	if _, err := repo.Get(otherWorkspace, source.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross workspace get: expected ErrRecordNotFound, got %v", err)
	}
	if _, err := repo.Get(seedCtx, 424242); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing id get: expected ErrRecordNotFound, got %v", err)
	}

	if err := repo.Delete(seedCtx, source.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(seedCtx, source.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("get after delete: expected ErrRecordNotFound, got %v", err)
	}
}

func TestGormSourcePlainGetAndDeleteDBErrors(t *testing.T) {
	db := newSourceRepoTestDB(t)
	repo := NewGormSourceRepository(db)
	ctx := context.Background()

	if err := db.Migrator().DropTable(&domain.KnowledgeSource{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if _, err := repo.Get(ctx, 1); err == nil {
		t.Fatal("expected get error after drop")
	}
	if err := repo.Delete(ctx, 1); err == nil {
		t.Fatal("expected delete error after drop")
	}
}

func TestGormSourcePlainCountDocumentsGuard(t *testing.T) {
	db := newSourceRepoTestDB(t)
	repo := NewGormSourceRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "t1", "w1")

	source := &domain.Source{Name: "FAQ", Type: "faq"}
	if err := repo.Create(ctx, source); err != nil {
		t.Fatalf("create source: %v", err)
	}
	seed := []domain.KnowledgeDoc{
		// 引用该来源的本租户文档 ×2 → 计入。
		{TenantID: "t1", WorkspaceID: "w1", SourceID: source.ID, Title: "d1", Content: "c"},
		{TenantID: "t1", WorkspaceID: "w1", SourceID: source.ID, Title: "d2", Content: "c"},
		// 未挂来源 → 不计入。
		{TenantID: "t1", WorkspaceID: "w1", Title: "d3", Content: "c"},
		// 跨租户引用 → 不计入本租户视角（scope 过滤的删除守卫口径）。
		{TenantID: "t2", WorkspaceID: "w1", SourceID: source.ID, Title: "d4", Content: "c"},
	}
	for i := range seed {
		if err := db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed doc[%d]: %v", i, err)
		}
	}

	count, err := repo.CountDocuments(ctx, source.ID)
	if err != nil || count != 2 {
		t.Fatalf("count = %d err %v want 2", count, err)
	}
	if count, err := repo.CountDocuments(ctx, 424242); err != nil || count != 0 {
		t.Fatalf("missing source count = %d err %v want 0", count, err)
	}
	ctxT2 := platformauth.ContextWithScope(context.Background(), "t2", "w1")
	if count, err := repo.CountDocuments(ctxT2, source.ID); err != nil || count != 1 {
		t.Fatalf("t2 count = %d err %v want 1", count, err)
	}

	if err := db.Migrator().DropTable(&domain.KnowledgeDoc{}); err != nil {
		t.Fatalf("drop doc table: %v", err)
	}
	if _, err := repo.CountDocuments(ctx, source.ID); err == nil {
		t.Fatal("expected count error after drop")
	}
}

func TestGormIndexJobPlainListByDocumentOrderAndLimit(t *testing.T) {
	db := newKnowledgeUnitTestDB(t)
	repo := NewGormIndexJobRepository(db)
	docRepo := NewGormDocumentRepository(db)
	ctx := context.Background()

	doc := &domain.Document{Title: "d", Content: "c", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := docRepo.Create(ctx, doc); err != nil {
		t.Fatalf("create doc: %v", err)
	}
	docNumericID, err := strconv.ParseUint(doc.ID, 10, 64)
	if err != nil {
		t.Fatalf("doc id not numeric: %q", doc.ID)
	}
	docID := strconv.FormatUint(docNumericID, 10)

	jobs := []domain.KnowledgeIndexJob{
		{ID: "job-old", DocumentID: uint(docNumericID), Status: "done", CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour)},
		{ID: "job-new", DocumentID: uint(docNumericID), Status: "queued", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	for i := range jobs {
		if err := db.Create(&jobs[i]).Error; err != nil {
			t.Fatalf("seed job[%d]: %v", i, err)
		}
	}

	list, err := repo.ListByDocument(ctx, docID, 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v err %v want 2 rows", list, err)
	}
	// 新任务在前（created_at DESC）。
	if list[0].ID != "job-new" || list[1].ID != "job-old" {
		t.Fatalf("order = %s,%s want job-new,job-old", list[0].ID, list[1].ID)
	}
	if list[0].DocumentID != docID || list[0].Status != domain.IndexJobStatus("queued") {
		t.Fatalf("mapping = %+v", list[0])
	}

	top1, err := repo.ListByDocument(ctx, docID, 1)
	if err != nil || len(top1) != 1 || top1[0].ID != "job-new" {
		t.Fatalf("limit=1 → %+v err %v want newest only", top1, err)
	}

	empty, err := repo.ListByDocument(ctx, "999999", 10)
	if err != nil || len(empty) != 0 {
		t.Fatalf("doc without jobs = %+v err %v want empty", empty, err)
	}
}
