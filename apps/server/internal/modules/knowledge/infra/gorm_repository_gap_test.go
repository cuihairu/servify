package infra

// 目录仓储与文档仓储的分支补测（混合检索落地后 catalog/document Create 的
// 错误分支与列表映射此前未走全）：nil 入参、缺作用域、非法文档 id、
// 底层查询失败（丢表注入）、以及文档创建时 KnowledgeBase 指定/兜底两条
// 失败路径。

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/modules/knowledge/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

func TestKnowledgeCatalogRepositoryGuards(t *testing.T) {
	db := newKnowledgeUnitTestDB(t)
	catalog := NewGormKnowledgeCatalogRepository(db)
	scoped := platformauth.ContextWithScope(context.Background(), "t-gap", "w-gap")

	if err := catalog.Create(scoped, nil); !errors.Is(err, gorm.ErrInvalidValue) {
		t.Fatalf("nil base: err=%v want ErrInvalidValue", err)
	}
	if err := catalog.Create(context.Background(), &domain.KnowledgeBase{Name: "x"}); err == nil {
		t.Fatal("create without scope accepted")
	}
	if _, err := catalog.List(context.Background()); err == nil {
		t.Fatal("list without scope accepted")
	}
	if _, err := catalog.ListByDocument(context.Background(), "1"); err == nil {
		t.Fatal("list chunks without scope accepted")
	}
	if _, err := catalog.ListByDocument(scoped, "not-a-number"); err == nil {
		t.Fatal("non-numeric document id accepted")
	}

	if err := db.Migrator().DropTable(&domain.KnowledgeBase{}); err != nil {
		t.Fatalf("drop knowledge_bases: %v", err)
	}
	if err := db.Migrator().DropTable(&domain.KnowledgeChunk{}); err != nil {
		t.Fatalf("drop knowledge_chunks: %v", err)
	}
	if err := catalog.Create(scoped, &domain.KnowledgeBase{Name: "P"}); err == nil {
		t.Fatal("create on dropped table accepted")
	}
	if _, err := catalog.List(scoped); err == nil {
		t.Fatal("list on dropped table accepted")
	}
	if _, err := catalog.ListByDocument(scoped, "1"); err == nil {
		t.Fatal("list chunks on dropped join table accepted")
	}
}

func TestKnowledgeCatalogListMapsRows(t *testing.T) {
	db := newKnowledgeUnitTestDB(t)
	catalog := NewGormKnowledgeCatalogRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "t-map", "w-map")

	for _, name := range []string{"A", "B"} {
		if err := catalog.Create(ctx, &domain.KnowledgeBase{Name: name, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
			t.Fatalf("create base %s: %v", name, err)
		}
	}
	bases, err := catalog.List(ctx)
	if err != nil {
		t.Fatalf("list bases: %v", err)
	}
	if len(bases) != 2 || bases[0].Name != "A" || bases[1].Name != "B" {
		t.Fatalf("unexpected bases: %+v", bases)
	}
}

func TestGormDocumentCreateKnowledgeBaseBranches(t *testing.T) {
	db := newKnowledgeUnitTestDB(t)
	repo := NewGormDocumentRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "t-doc", "w-doc")

	// 指定的 KnowledgeBase 不存在 → ErrRecordNotFound
	doc := &domain.Document{KnowledgeBaseID: 9999, Title: "T", Content: "c", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := repo.Create(ctx, doc); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing base: err=%v want ErrRecordNotFound", err)
	}

	// 兜底 Default 基座创建失败 → 底层错误透出
	if err := db.Migrator().DropTable(&domain.KnowledgeBase{}); err != nil {
		t.Fatalf("drop knowledge_bases: %v", err)
	}
	if err := repo.Create(ctx, &domain.Document{Title: "T2", Content: "c2", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err == nil {
		t.Fatal("create doc without base table accepted")
	}
}
