//go:build integration
// +build integration

package infra

// V1.0 收敛 B3-1a（docs/v1-convergence-plan.md §8.1/§8.2）：来源登记仓储
// 与索引任务按文档列表的 sqlite 集成行为（scope 过滤 + 引用计数守卫 +
// 版本字段往返）。

import (
	"context"
	"testing"
	"time"

	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
	"servify/apps/server/internal/modules/knowledge/domain"
	platformauth "servify/apps/server/internal/platform/auth"
)

func TestGormSourceRepositoryLifecycle(t *testing.T) {
	db := newKnowledgeInfraTestDB(t)
	if err := db.AutoMigrate(&domain.KnowledgeSource{}); err != nil {
		t.Fatalf("automigrate sources: %v", err)
	}
	sourceRepo := NewGormSourceRepository(db)
	docRepo := NewGormDocumentRepository(db)

	ctx := platformauth.ContextWithScope(context.Background(), "t-src", "w-src")
	now := time.Now()
	source := &domain.Source{Name: "FAQ 站", Type: "faq", Description: "帮助中心", CreatedAt: now, UpdatedAt: now}
	if err := sourceRepo.Create(ctx, source); err != nil {
		t.Fatalf("create: %v", err)
	}
	if source.ID == 0 {
		t.Fatal("expected generated id")
	}

	// 其他租户的来源不可见（scope 过滤）。
	otherCtx := platformauth.ContextWithScope(context.Background(), "t-other", "w-other")
	list, err := sourceRepo.List(otherCtx, knowledgeapp.ListSourcesFilter{})
	if err != nil || len(list) != 0 {
		t.Fatalf("cross-tenant list = %v err %v（scope 必须隔离）", list, err)
	}
	list, err = sourceRepo.List(ctx, knowledgeapp.ListSourcesFilter{Type: "faq"})
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v err %v", list, err)
	}

	// 引用计数：挂两个文档 → 2；删守卫依赖该计数。
	for _, title := range []string{"d1", "d2"} {
		doc := &domain.Document{Title: title, Content: "c", SourceID: source.ID, CreatedAt: now, UpdatedAt: now}
		if err := docRepo.Create(ctx, doc); err != nil {
			t.Fatalf("create doc: %v", err)
		}
	}
	references, err := sourceRepo.CountDocuments(ctx, source.ID)
	if err != nil || references != 2 {
		t.Fatalf("count = %d err %v want 2", references, err)
	}

	if err := sourceRepo.Delete(ctx, source.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := sourceRepo.Get(ctx, source.ID); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestGormIndexJobListByDocumentAndVersionRoundTrip(t *testing.T) {
	db := newKnowledgeInfraTestDB(t)
	docRepo := NewGormDocumentRepository(db)
	jobRepo := NewGormIndexJobRepository(db)

	ctx := context.Background()
	now := time.Now()
	doc := &domain.Document{Title: "Ix", Content: "c", Version: 3, CreatedAt: now, UpdatedAt: now}
	if err := docRepo.Create(ctx, doc); err != nil {
		t.Fatalf("create doc: %v", err)
	}
	loaded, err := docRepo.Get(ctx, doc.ID)
	if err != nil || loaded.Version != 3 {
		t.Fatalf("doc version round trip = %v/%v want 3", loaded, err)
	}

	jobA := &domain.IndexJob{ID: "j-a", DocumentID: doc.ID, Status: domain.IndexJobDone, DocumentVersion: 3, CreatedAt: now, UpdatedAt: now}
	jobB := &domain.IndexJob{ID: "j-b", DocumentID: doc.ID, Status: domain.IndexJobFailed, Error: "boom", CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	for _, job := range []*domain.IndexJob{jobA, jobB} {
		if err := jobRepo.Create(ctx, job); err != nil {
			t.Fatalf("create job: %v", err)
		}
	}
	jobs, err := jobRepo.ListByDocument(ctx, doc.ID, 10)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs = %v err %v", jobs, err)
	}
	// 新任务在前。
	if jobs[0].ID != "j-b" || jobs[0].DocumentVersion != 0 || jobs[0].Status != domain.IndexJobFailed {
		t.Fatalf("jobs[0] = %+v want j-b failed v0", jobs[0])
	}
	if jobs[1].ID != "j-a" || jobs[1].DocumentVersion != 3 {
		t.Fatalf("jobs[1] = %+v want j-a v3", jobs[1])
	}
}
