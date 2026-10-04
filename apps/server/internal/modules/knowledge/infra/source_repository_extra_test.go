package infra

// 覆盖率补充：来源登记仓储的全量分支（nil 入参、未命中、成功回读与更新、
// 查询错误）与索引任务按文档列表的参数/查询错误。沿用既有约定：查询错误
// 用 drop-table 强制触发（见 gorm_repository_unit_test.go）。

import (
	"context"
	"errors"
	"testing"
	"time"

	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
	"servify/apps/server/internal/modules/knowledge/domain"

	"gorm.io/gorm"
)

func newSourceRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newKnowledgeUnitTestDB(t)
	if err := db.AutoMigrate(&domain.KnowledgeSource{}); err != nil {
		t.Fatalf("automigrate sources: %v", err)
	}
	return db
}

func TestGormSourceRepositoryNilInputsAndMissesExtra(t *testing.T) {
	db := newSourceRepoTestDB(t)
	repo := NewGormSourceRepository(db)
	ctx := context.Background()

	if err := repo.Create(ctx, nil); !errors.Is(err, gorm.ErrInvalidValue) {
		t.Fatalf("expected invalid value on nil create, got %v", err)
	}
	if err := repo.Update(ctx, nil); !errors.Is(err, gorm.ErrInvalidValue) {
		t.Fatalf("expected invalid value on nil update, got %v", err)
	}
	if err := repo.Update(ctx, &domain.Source{ID: 424242, Name: "ghost", Type: "faq"}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected update not found, got %v", err)
	}
	if err := repo.Delete(ctx, 424242); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected delete not found, got %v", err)
	}
}

func TestGormSourceRepositoryUpdateAndGetRoundTripExtra(t *testing.T) {
	db := newSourceRepoTestDB(t)
	repo := NewGormSourceRepository(db)
	ctx := context.Background()
	now := time.Now()

	source := &domain.Source{Name: "FAQ", Type: "faq", Description: "d", CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(ctx, source); err != nil {
		t.Fatalf("create: %v", err)
	}
	source.Description = "d2"
	source.UpdatedAt = now.Add(time.Second)
	if err := repo.Update(ctx, source); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := repo.Get(ctx, source.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Description != "d2" || got.Name != "FAQ" || got.Type != "faq" {
		t.Fatalf("unexpected source: %+v", got)
	}

	list, err := repo.List(ctx, knowledgeapp.ListSourcesFilter{Type: " faq "})
	if err != nil || len(list) != 1 || list[0].Description != "d2" {
		t.Fatalf("list = %+v err %v", list, err)
	}
}

func TestGormSourceRepositoryQueryErrorsExtra(t *testing.T) {
	db := newSourceRepoTestDB(t)
	repo := NewGormSourceRepository(db)
	ctx := context.Background()

	if err := db.Migrator().DropTable(&domain.KnowledgeSource{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if err := repo.Create(ctx, &domain.Source{Name: "x", Type: "faq"}); err == nil {
		t.Fatal("expected create error after drop")
	}
	if err := repo.Update(ctx, &domain.Source{ID: 1, Name: "x", Type: "faq"}); err == nil {
		t.Fatal("expected update error after drop")
	}
	if err := repo.Delete(ctx, 1); err == nil {
		t.Fatal("expected delete error after drop")
	}
	if _, err := repo.List(ctx, knowledgeapp.ListSourcesFilter{}); err == nil {
		t.Fatal("expected list error after drop")
	}
}

func TestGormIndexJobRepositoryListByDocumentErrorsExtra(t *testing.T) {
	db := newKnowledgeUnitTestDB(t)
	repo := NewGormIndexJobRepository(db)
	ctx := context.Background()

	if _, err := repo.ListByDocument(ctx, "abc", 10); err == nil || err.Error() != "invalid document id" {
		t.Fatalf("expected invalid document id, got %v", err)
	}

	doc := &domain.Document{Title: "d", Content: "c", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := NewGormDocumentRepository(db).Create(ctx, doc); err != nil {
		t.Fatalf("create doc: %v", err)
	}
	if err := db.Migrator().DropTable(&domain.KnowledgeIndexJob{}); err != nil {
		t.Fatalf("drop job table: %v", err)
	}
	if _, err := repo.ListByDocument(ctx, doc.ID, 10); err == nil {
		t.Fatal("expected list error after drop")
	}
}
