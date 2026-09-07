package services

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"
	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"
)

func TestKnowledgeDocService_CRUD(t *testing.T) {
	db := newServicesTestDB(t, &models.KnowledgeDoc{}, &models.KnowledgeIndexJob{})
	svc := NewKnowledgeDocService(db)
	ctx := context.Background()

	created, err := svc.Create(ctx, &KnowledgeDocCreateRequest{
		Title:   "Doc",
		Content: "Body",
		Tags:    []string{" a ", "", "b"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Tags != "a,b" {
		t.Fatalf("tags = %q", created.Tags)
	}

	if _, err := svc.Create(ctx, nil); err == nil {
		t.Fatal("expected error for nil create")
	}

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "Doc" {
		t.Fatalf("unexpected doc: %+v", got)
	}
	if _, err := svc.Get(ctx, 999); err == nil {
		t.Fatal("expected error for missing doc")
	}

	docs, total, err := svc.List(ctx, &KnowledgeDocListRequest{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(docs) != 1 {
		t.Fatalf("unexpected list: %d %d", total, len(docs))
	}
	if _, _, err := svc.List(ctx, nil); err != nil {
		t.Fatalf("List nil: %v", err)
	}

	updated, err := svc.Update(ctx, created.ID, &KnowledgeDocUpdateRequest{
		Title:    stringPtr("Doc2"),
		Content:  stringPtr("Body2"),
		Category: stringPtr("cat"),
		Tags:     &[]string{"x"},
		IsPublic: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Title != "Doc2" || !updated.IsPublic {
		t.Fatalf("unexpected update: %+v", updated)
	}
	if _, err := svc.Update(ctx, created.ID, nil); err == nil {
		t.Fatal("expected error for nil update")
	}

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := svc.List(ctx, nil); err != nil {
		t.Fatalf("List after delete: %v", err)
	}
}

func TestKnowledgeDocHelpers(t *testing.T) {
	if joinTagsCSV(nil) != "" {
		t.Fatal("nil tags should be empty")
	}
	if joinTagsCSV([]string{" ", ""}) != "" {
		t.Fatal("blank tags should be empty")
	}
	if joinTagsCSV([]string{"a", " b"}) != "a,b" {
		t.Fatal("unexpected join")
	}

	if m, err := knowledgeDocFromDomain(nil); m != nil || err != nil {
		t.Fatalf("nil domain: %v %v", m, err)
	}
	if _, err := knowledgeDocFromDomain(&knowledgedomain.Document{ID: "not-a-number"}); err == nil {
		t.Fatal("expected invalid id error")
	}
}
