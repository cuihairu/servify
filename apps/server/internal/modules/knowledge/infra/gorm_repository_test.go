//go:build integration
// +build integration

package infra

import (
	"context"
	"testing"
	"time"

	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
	"servify/apps/server/internal/modules/knowledge/domain"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newKnowledgeInfraTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.KnowledgeDoc{}, &domain.KnowledgeIndexJob{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestGormIndexJobRepositoryRoundTrip(t *testing.T) {
	db := newKnowledgeInfraTestDB(t)
	docRepo := NewGormDocumentRepository(db)
	jobRepo := NewGormIndexJobRepository(db)

	doc := &domain.Document{
		Title:     "Billing",
		Content:   "Billing details",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := docRepo.Create(context.Background(), doc); err != nil {
		t.Fatalf("create doc: %v", err)
	}

	job := &domain.IndexJob{
		ID:         "job-1",
		DocumentID: doc.ID,
		Status:     domain.IndexJobQueued,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := jobRepo.Create(context.Background(), job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	completedAt := time.Now()
	job.Status = domain.IndexJobDone
	job.CompletedAt = &completedAt
	job.UpdatedAt = completedAt
	if err := jobRepo.Update(context.Background(), job); err != nil {
		t.Fatalf("update job: %v", err)
	}

	got, err := jobRepo.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.DocumentID != doc.ID {
		t.Fatalf("document id = %s want %s", got.DocumentID, doc.ID)
	}
	if got.Status != domain.IndexJobDone {
		t.Fatalf("status = %s want %s", got.Status, domain.IndexJobDone)
	}
	if got.CompletedAt == nil {
		t.Fatal("expected completed_at to be persisted")
	}
}

func TestGormDocumentRepository_PublicFilter(t *testing.T) {
	db := newKnowledgeInfraTestDB(t)
	repo := NewGormDocumentRepository(db)

	publicDoc := &domain.Document{
		Title:     "Public Billing",
		Content:   "Public details",
		IsPublic:  true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	internalDoc := &domain.Document{
		Title:     "Internal Billing",
		Content:   "Internal details",
		IsPublic:  false,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := repo.Create(context.Background(), publicDoc); err != nil {
		t.Fatalf("create public doc: %v", err)
	}
	if err := repo.Create(context.Background(), internalDoc); err != nil {
		t.Fatalf("create internal doc: %v", err)
	}

	docs, total, err := repo.List(context.Background(), knowledgeapp.ListDocumentsFilter{
		Page:       1,
		PageSize:   10,
		PublicOnly: true,
	})
	if err != nil {
		t.Fatalf("list public docs: %v", err)
	}
	if total != 1 || len(docs) != 1 {
		t.Fatalf("unexpected public docs total=%d len=%d docs=%+v", total, len(docs), docs)
	}
	if docs[0].Title != "Public Billing" || !docs[0].IsPublic {
		t.Fatalf("unexpected public doc %+v", docs[0])
	}
}

func TestGormDocumentRepository_PersistsExternalMapping(t *testing.T) {
	db := newKnowledgeInfraTestDB(t)
	repo := NewGormDocumentRepository(db)

	doc := &domain.Document{
		ProviderID: "dify",
		ExternalID: "ext-123",
		Title:      "Billing",
		Content:    "Billing details",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := repo.Create(context.Background(), doc); err != nil {
		t.Fatalf("create doc: %v", err)
	}

	got, err := repo.Get(context.Background(), doc.ID)
	if err != nil {
		t.Fatalf("get doc: %v", err)
	}
	if got.ProviderID != "dify" || got.ExternalID != "ext-123" {
		t.Fatalf("unexpected provider mapping: %+v", got)
	}
}
