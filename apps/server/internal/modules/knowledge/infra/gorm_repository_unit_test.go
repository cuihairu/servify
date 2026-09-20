package infra

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
	"servify/apps/server/internal/modules/knowledge/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newKnowledgeUnitTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:"+t.Name()+"")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.KnowledgeDoc{}, &domain.KnowledgeIndexJob{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestGormDocumentRepositoryCRUDUnit(t *testing.T) {
	db := newKnowledgeUnitTestDB(t)
	repo := NewGormDocumentRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")

	created := &domain.Document{
		ProviderID: "pgvector",
		Title:      "Billing",
		Content:    "Billing details",
		Category:   "faq",
		Tags:       []string{"billing", "kb"},
		IsPublic:   true,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := repo.Create(ctx, created); err != nil {
		t.Fatalf("create doc: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated document id")
	}
	var stored domain.KnowledgeDoc
	if err := db.First(&stored, "id = ?", created.ID).Error; err != nil {
		t.Fatalf("load stored doc: %v", err)
	}
	if stored.TenantID != "tenant-a" || stored.WorkspaceID != "workspace-1" {
		t.Fatalf("unexpected scope: %+v", stored)
	}
	if stored.Tags != "billing,kb" {
		t.Fatalf("unexpected tags csv: %q", stored.Tags)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get doc: %v", err)
	}
	if got.Title != "Billing" || got.Category != "faq" || !got.IsPublic {
		t.Fatalf("unexpected doc: %+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "billing" || got.Tags[1] != "kb" {
		t.Fatalf("unexpected tags: %+v", got.Tags)
	}

	updated := *got
	updated.Title = "Billing v2"
	updated.Content = "Updated"
	updated.IsPublic = false
	if err := repo.Update(ctx, &updated); err != nil {
		t.Fatalf("update doc: %v", err)
	}
	got, err = repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get updated doc: %v", err)
	}
	if got.Title != "Billing v2" || got.IsPublic {
		t.Fatalf("unexpected updated doc: %+v", got)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete doc: %v", err)
	}
	if _, err := repo.Get(ctx, created.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected record not found, got %v", err)
	}
}

func TestGormDocumentRepositoryMissingRecords(t *testing.T) {
	db := newKnowledgeUnitTestDB(t)
	repo := NewGormDocumentRepository(db)
	ctx := context.Background()

	doc := &domain.Document{ID: "42", Title: "Ghost", Content: "none", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := repo.Update(ctx, doc); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected update not found, got %v", err)
	}
	if err := repo.Delete(ctx, "42"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected delete not found, got %v", err)
	}
	if _, err := repo.Get(ctx, "42"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected get not found, got %v", err)
	}
}

func TestGormDocumentRepositoryInvalidIDsUnit(t *testing.T) {
	db := newKnowledgeUnitTestDB(t)
	repo := NewGormDocumentRepository(db)
	ctx := context.Background()

	doc := &domain.Document{ID: "abc", Title: "t", Content: "c"}
	if err := repo.Update(ctx, doc); err == nil || err.Error() != "invalid document id" {
		t.Fatalf("expected invalid id error on update, got %v", err)
	}
	if err := repo.Delete(ctx, "0"); err == nil || err.Error() != "invalid document id" {
		t.Fatalf("expected invalid id error on delete, got %v", err)
	}
	if _, err := repo.Get(ctx, "-7"); err == nil || err.Error() != "invalid document id" {
		t.Fatalf("expected invalid id error on get, got %v", err)
	}
}

func TestGormDocumentRepositoryListUnit(t *testing.T) {
	db := newKnowledgeUnitTestDB(t)
	repo := NewGormDocumentRepository(db)

	tenantA := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")
	tenantB := platformauth.ContextWithScope(context.Background(), "tenant-b", "")

	base := time.Now()
	docs := []domain.Document{
		{Title: "Alpha Guide", Content: "alpha body", Category: "guide", Tags: []string{"howto"}, IsPublic: true, CreatedAt: base.Add(-3 * time.Hour), UpdatedAt: base},
		{Title: "Beta Guide", Content: "beta body", Category: "guide", IsPublic: false, CreatedAt: base.Add(-2 * time.Hour), UpdatedAt: base},
		{Title: "Gamma Faq", Content: "gamma body", Category: "faq", Tags: []string{"alpha"}, IsPublic: false, CreatedAt: base.Add(-1 * time.Hour), UpdatedAt: base},
	}
	for i := range docs {
		doc := docs[i]
		if err := repo.Create(tenantA, &doc); err != nil {
			t.Fatalf("create doc %d: %v", i, err)
		}
	}
	otherTenant := &domain.Document{Title: "Other Tenant", Content: "other", CreatedAt: base, UpdatedAt: base}
	if err := repo.Create(tenantB, otherTenant); err != nil {
		t.Fatalf("create other tenant doc: %v", err)
	}

	all, total, err := repo.List(tenantA, knowledgeapp.ListDocumentsFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("unexpected list result total=%d len=%d", total, len(all))
	}
	if all[0].Title != "Gamma Faq" {
		t.Fatalf("expected newest first, got %q", all[0].Title)
	}

	public, total, err := repo.List(tenantA, knowledgeapp.ListDocumentsFilter{Page: 1, PageSize: 10, PublicOnly: true})
	if err != nil {
		t.Fatalf("list public: %v", err)
	}
	if total != 1 || len(public) != 1 || public[0].Title != "Alpha Guide" {
		t.Fatalf("unexpected public list total=%d docs=%+v", total, public)
	}

	guides, total, err := repo.List(tenantA, knowledgeapp.ListDocumentsFilter{Page: 1, PageSize: 10, Category: " guide "})
	if err != nil {
		t.Fatalf("list by category: %v", err)
	}
	if total != 2 || len(guides) != 2 {
		t.Fatalf("unexpected category list total=%d len=%d", total, len(guides))
	}

	byTitle, _, err := repo.List(tenantA, knowledgeapp.ListDocumentsFilter{Page: 1, PageSize: 10, Search: "beta"})
	if err != nil {
		t.Fatalf("list by search: %v", err)
	}
	if len(byTitle) != 1 || byTitle[0].Title != "Beta Guide" {
		t.Fatalf("unexpected title search result: %+v", byTitle)
	}
	byTags, _, err := repo.List(tenantA, knowledgeapp.ListDocumentsFilter{Page: 1, PageSize: 10, Search: "howto"})
	if err != nil {
		t.Fatalf("list by tag search: %v", err)
	}
	if len(byTags) != 1 || byTags[0].Title != "Alpha Guide" {
		t.Fatalf("unexpected tag search result: %+v", byTags)
	}
	byContent, _, err := repo.List(tenantA, knowledgeapp.ListDocumentsFilter{Page: 1, PageSize: 10, Search: "gamma body"})
	if err != nil {
		t.Fatalf("list by content search: %v", err)
	}
	if len(byContent) != 1 || byContent[0].Title != "Gamma Faq" {
		t.Fatalf("unexpected content search result: %+v", byContent)
	}

	paged, total, err := repo.List(tenantA, knowledgeapp.ListDocumentsFilter{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("list paged: %v", err)
	}
	if total != 3 || len(paged) != 1 || paged[0].Title != "Alpha Guide" {
		t.Fatalf("unexpected paged result total=%d docs=%+v", total, paged)
	}

	scoped, total, err := repo.List(tenantB, knowledgeapp.ListDocumentsFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list scoped: %v", err)
	}
	if total != 1 || len(scoped) != 1 || scoped[0].Title != "Other Tenant" {
		t.Fatalf("unexpected scoped list total=%d docs=%+v", total, scoped)
	}
	if _, err := repo.Get(tenantB, all[0].ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected scope isolation on get, got %v", err)
	}
}

func TestGormDocumentRepositoryQueryErrors(t *testing.T) {
	db := newKnowledgeUnitTestDB(t)
	repo := NewGormDocumentRepository(db)
	ctx := context.Background()

	if err := repo.Create(ctx, &domain.Document{Title: "t", Content: "c", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("seed doc: %v", err)
	}

	if err := db.Migrator().DropTable(&domain.KnowledgeDoc{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	if err := repo.Create(ctx, &domain.Document{Title: "t", Content: "c"}); err == nil {
		t.Fatal("expected create error after drop")
	}
	if err := repo.Update(ctx, &domain.Document{ID: "1", Title: "t", Content: "c"}); err == nil {
		t.Fatal("expected update error after drop")
	}
	if err := repo.Delete(ctx, "1"); err == nil {
		t.Fatal("expected delete error after drop")
	}
	if _, err := repo.Get(ctx, "1"); err == nil {
		t.Fatal("expected get error after drop")
	}
	if _, _, err := repo.List(ctx, knowledgeapp.ListDocumentsFilter{Page: 1, PageSize: 10}); err == nil {
		t.Fatal("expected list count error after drop")
	}
}

func TestGormDocumentRepositoryListScanError(t *testing.T) {
	db := newKnowledgeUnitTestDB(t)
	repo := NewGormDocumentRepository(db)
	ctx := context.Background()

	if err := db.Exec(`INSERT INTO knowledge_docs (tenant_id, workspace_id, title, content, category, tags, is_public, created_at, updated_at) VALUES ('t', 'w', 'Bad Row', 'c', '', '', 0, 'not-a-timestamp', 'not-a-timestamp')`).Error; err != nil {
		t.Fatalf("seed invalid row: %v", err)
	}

	docs, _, err := repo.List(ctx, knowledgeapp.ListDocumentsFilter{Page: 1, PageSize: 10})
	if err == nil {
		t.Fatalf("expected scan error, got docs=%+v", docs)
	}
}

func TestGormIndexJobRepositoryUnit(t *testing.T) {
	db := newKnowledgeUnitTestDB(t)
	docRepo := NewGormDocumentRepository(db)
	jobRepo := NewGormIndexJobRepository(db)
	ctx := context.Background()

	doc := &domain.Document{Title: "Billing", Content: "Billing details", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := docRepo.Create(ctx, doc); err != nil {
		t.Fatalf("create doc: %v", err)
	}

	if err := jobRepo.Create(ctx, nil); err == nil || err.Error() != "index job required" {
		t.Fatalf("expected nil job error, got %v", err)
	}
	if err := jobRepo.Create(ctx, &domain.IndexJob{ID: "bad", DocumentID: "abc"}); err == nil || err.Error() != "invalid document id" {
		t.Fatalf("expected invalid document id error, got %v", err)
	}

	now := time.Now()
	job := &domain.IndexJob{ID: " job-1 ", DocumentID: " " + doc.ID, Status: domain.IndexJobQueued, CreatedAt: now, UpdatedAt: now}
	if err := jobRepo.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	got, err := jobRepo.Get(ctx, " job-1 ")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.DocumentID != doc.ID || got.Status != domain.IndexJobQueued {
		t.Fatalf("unexpected job: %+v", got)
	}

	if err := jobRepo.Update(ctx, nil); err == nil || err.Error() != "index job required" {
		t.Fatalf("expected nil job error on update, got %v", err)
	}
	if err := jobRepo.Update(ctx, &domain.IndexJob{ID: "bad", DocumentID: "abc"}); err == nil || err.Error() != "invalid document id" {
		t.Fatalf("expected invalid document id error on update, got %v", err)
	}

	completed := time.Now()
	got.Status = domain.IndexJobDone
	got.Error = ""
	got.CompletedAt = &completed
	got.UpdatedAt = completed
	if err := jobRepo.Update(ctx, got); err != nil {
		t.Fatalf("update job: %v", err)
	}
	refreshed, err := jobRepo.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("refresh job: %v", err)
	}
	if refreshed.Status != domain.IndexJobDone || refreshed.CompletedAt == nil {
		t.Fatalf("unexpected refreshed job: %+v", refreshed)
	}

	if err := jobRepo.Update(ctx, &domain.IndexJob{ID: "missing", DocumentID: doc.ID, Status: domain.IndexJobQueued}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected update not found, got %v", err)
	}
	if _, err := jobRepo.Get(ctx, "missing"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected get not found, got %v", err)
	}

	if err := db.Migrator().DropTable(&domain.KnowledgeIndexJob{}); err != nil {
		t.Fatalf("drop job table: %v", err)
	}
	if err := jobRepo.Create(ctx, &domain.IndexJob{ID: "x", DocumentID: doc.ID}); err == nil {
		t.Fatal("expected create error after drop")
	}
	if err := jobRepo.Update(ctx, got); err == nil {
		t.Fatal("expected update error after drop")
	}
	if _, err := jobRepo.Get(ctx, got.ID); err == nil {
		t.Fatal("expected get error after drop")
	}
}

func TestIndexJobModelFromDomainNil(t *testing.T) {
	if _, err := indexJobModelFromDomain(nil); err == nil || err.Error() != "index job required" {
		t.Fatalf("expected nil job error, got %v", err)
	}
}

func TestSplitTags(t *testing.T) {
	if got := splitTags(""); got != nil {
		t.Fatalf("expected nil for empty tags, got %+v", got)
	}
	if got := splitTags(" , ,, "); len(got) != 0 {
		t.Fatalf("expected empty tags, got %+v", got)
	}
	got := splitTags(" a , b ,, c ")
	if fmt.Sprint(got) != "[a b c]" {
		t.Fatalf("unexpected tags: %+v", got)
	}
}
