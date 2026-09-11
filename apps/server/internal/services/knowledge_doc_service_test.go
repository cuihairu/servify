//go:build integration
// +build integration

package services

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"servify/apps/server/internal/models"
)

func newKnowledgeDocTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := t.Name()
	dsn := uniqueMemDSN("file:knowledge_doc_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.KnowledgeDoc{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestKnowledgeDocService_Create(t *testing.T) {
	db := newKnowledgeDocTestDB(t)
	svc := NewKnowledgeDocService(db)

	tests := []struct {
		name    string
		req     *KnowledgeDocCreateRequest
		wantErr bool
	}{
		{
			name: "valid doc",
			req: &KnowledgeDocCreateRequest{
				Title:   "Test Document",
				Content: "This is test content",
			},
			wantErr: false,
		},
		{
			name: "with category and tags",
			req: &KnowledgeDocCreateRequest{
				Title:    "Tagged Doc",
				Content:  "Content with tags",
				Category: "Technical",
				Tags:     []string{"api", "guide"},
				IsPublic: true,
			},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty title",
			req: &KnowledgeDocCreateRequest{
				Title:   "",
				Content: "Content",
			},
			wantErr: true,
		},
		{
			name: "empty content",
			req: &KnowledgeDocCreateRequest{
				Title:   "Title",
				Content: "",
			},
			wantErr: true,
		},
		{
			name: "whitespace title",
			req: &KnowledgeDocCreateRequest{
				Title:   "   ",
				Content: "Content",
			},
			wantErr: true,
		},
		{
			name: "tags with empty strings",
			req: &KnowledgeDocCreateRequest{
				Title:   "Tags Test",
				Content: "Content",
				Tags:    []string{"tag1", "", "tag2"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := svc.Create(context.Background(), tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if doc.ID == 0 {
					t.Error("expected non-zero ID")
				}
				if doc.CreatedAt.IsZero() {
					t.Error("expected CreatedAt to be set")
				}
				if tt.req != nil && doc.IsPublic != tt.req.IsPublic {
					t.Errorf("expected IsPublic=%v, got %v", tt.req.IsPublic, doc.IsPublic)
				}
			}
		})
	}
}

func TestKnowledgeDocService_Get(t *testing.T) {
	db := newKnowledgeDocTestDB(t)
	svc := NewKnowledgeDocService(db)

	// Create test doc
	doc, _ := svc.Create(context.Background(), &KnowledgeDocCreateRequest{
		Title:   "Find Me",
		Content: "Content",
	})

	tests := []struct {
		name    string
		id      uint
		wantErr bool
	}{
		{
			name:    "existing doc",
			id:      doc.ID,
			wantErr: false,
		},
		{
			name:    "non-existent doc",
			id:      9999,
			wantErr: true,
		},
		{
			name:    "zero id",
			id:      0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, err := svc.Get(context.Background(), tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.id == doc.ID {
				if found.Title != "Find Me" {
					t.Errorf("expected title 'Find Me', got '%s'", found.Title)
				}
			}
		})
	}
}

func TestKnowledgeDocService_List(t *testing.T) {
	db := newKnowledgeDocTestDB(t)
	svc := NewKnowledgeDocService(db)

	// Create test docs
	svc.Create(context.Background(), &KnowledgeDocCreateRequest{
		Title:    "Doc 1",
		Content:  "Content 1",
		Category: "Technical",
		Tags:     []string{"api"},
		IsPublic: true,
	})
	svc.Create(context.Background(), &KnowledgeDocCreateRequest{
		Title:    "Doc 2",
		Content:  "Content 2",
		Category: "User Guide",
		Tags:     []string{"guide"},
	})

	tests := []struct {
		name    string
		req     *KnowledgeDocListRequest
		wantMin int
		wantMax int
		wantErr bool
	}{
		{
			name: "list all",
			req: &KnowledgeDocListRequest{
				Page:     1,
				PageSize: 10,
			},
			wantMin: 2,
			wantMax: 2,
			wantErr: false,
		},
		{
			name: "filter by category",
			req: &KnowledgeDocListRequest{
				Page:     1,
				PageSize: 10,
				Category: "Technical",
			},
			wantMin: 1,
			wantMax: 1,
			wantErr: false,
		},
		{
			name: "search",
			req: &KnowledgeDocListRequest{
				Page:     1,
				PageSize: 10,
				Search:   "Doc",
			},
			wantMin: 2,
			wantMax: 2,
			wantErr: false,
		},
		{
			name: "pagination",
			req: &KnowledgeDocListRequest{
				Page:     1,
				PageSize: 1,
			},
			wantMin: 1,
			wantMax: 1,
			wantErr: false,
		},
		{
			name: "max page size",
			req: &KnowledgeDocListRequest{
				Page:     1,
				PageSize: 200,
			},
			wantMin: 2,
			wantMax: 2,
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantMin: 2,
			wantMax: 2,
			wantErr: false,
		},
		{
			name: "public only",
			req: &KnowledgeDocListRequest{
				Page:       1,
				PageSize:   10,
				PublicOnly: true,
			},
			wantMin: 1,
			wantMax: 1,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs, total, err := svc.List(context.Background(), tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("List() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(docs) < tt.wantMin || len(docs) > tt.wantMax {
					t.Errorf("expected between %d and %d docs, got %d", tt.wantMin, tt.wantMax, len(docs))
				}
				if total < int64(tt.wantMin) {
					t.Errorf("expected total >= %d, got %d", tt.wantMin, total)
				}
			}
		})
	}
}

func TestKnowledgeDocService_ListScopedByWorkspace(t *testing.T) {
	db := newKnowledgeDocTestDB(t)
	svc := NewKnowledgeDocService(db)

	ctxA := scopedContext("tenant-a", "workspace-a")
	ctxB := scopedContext("tenant-a", "workspace-b")

	if _, err := svc.Create(ctxA, &KnowledgeDocCreateRequest{Title: "Doc A", Content: "alpha"}); err != nil {
		t.Fatalf("create A failed: %v", err)
	}
	if _, err := svc.Create(ctxB, &KnowledgeDocCreateRequest{Title: "Doc B", Content: "beta"}); err != nil {
		t.Fatalf("create B failed: %v", err)
	}

	docs, total, err := svc.List(ctxA, &KnowledgeDocListRequest{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list A failed: %v", err)
	}
	if total != 1 || len(docs) != 1 || docs[0].Title != "Doc A" {
		t.Fatalf("unexpected scoped docs: total=%d docs=%+v", total, docs)
	}
}

func TestKnowledgeDocService_Update(t *testing.T) {
	db := newKnowledgeDocTestDB(t)
	svc := NewKnowledgeDocService(db)

	// Create test doc
	doc, _ := svc.Create(context.Background(), &KnowledgeDocCreateRequest{
		Title:    "Original Title",
		Content:  "Original Content",
		Category: "Original Category",
		Tags:     []string{"original"},
	})

	newTitle := "Updated Title"
	newContent := "Updated Content"
	newCategory := "Updated Category"
	newTags := []string{"updated", "tag"}

	tests := []struct {
		name    string
		id      uint
		req     *KnowledgeDocUpdateRequest
		wantErr bool
	}{
		{
			name: "update all fields",
			id:   doc.ID,
			req: &KnowledgeDocUpdateRequest{
				Title:    &newTitle,
				Content:  &newContent,
				Category: &newCategory,
				Tags:     &newTags,
			},
			wantErr: false,
		},
		{
			name: "update only title",
			id:   doc.ID,
			req: &KnowledgeDocUpdateRequest{
				Title: &newTitle,
			},
			wantErr: false,
		},
		{
			name: "update to empty title",
			id:   doc.ID,
			req: &KnowledgeDocUpdateRequest{
				Title: stringPtr(""),
			},
			wantErr: true,
		},
		{
			name: "update to empty content",
			id:   doc.ID,
			req: &KnowledgeDocUpdateRequest{
				Content: stringPtr(""),
			},
			wantErr: true,
		},
		{
			name: "non-existent doc",
			id:   9999,
			req: &KnowledgeDocUpdateRequest{
				Title: &newTitle,
			},
			wantErr: true,
		},
		{
			name:    "nil request",
			id:      doc.ID,
			req:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, err := svc.Update(context.Background(), tt.id, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Update() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if tt.req.Title != nil && updated.Title != *tt.req.Title {
					t.Errorf("expected title %s, got %s", *tt.req.Title, updated.Title)
				}
			}
		})
	}
}

func TestKnowledgeDocService_Delete(t *testing.T) {
	db := newKnowledgeDocTestDB(t)
	svc := NewKnowledgeDocService(db)

	// Create test doc
	doc, _ := svc.Create(context.Background(), &KnowledgeDocCreateRequest{
		Title:   "To Delete",
		Content: "Content",
	})

	tests := []struct {
		name    string
		id      uint
		wantErr bool
	}{
		{
			name:    "existing doc",
			id:      doc.ID,
			wantErr: false,
		},
		{
			name:    "non-existent doc",
			id:      9999,
			wantErr: true,
		},
		{
			name:    "zero id",
			id:      0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.Delete(context.Background(), tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.id == doc.ID {
				// Verify deleted
				_, err := svc.Get(context.Background(), tt.id)
				if err == nil {
					t.Error("expected doc to be deleted")
				}
			}
		})
	}
}

func TestJoinTagsCSV(t *testing.T) {
	tests := []struct {
		name     string
		tags     []string
		expected string
	}{
		{
			name:     "normal tags",
			tags:     []string{"tag1", "tag2", "tag3"},
			expected: "tag1,tag2,tag3",
		},
		{
			name:     "empty slice",
			tags:     []string{},
			expected: "",
		},
		{
			name:     "tags with empty strings",
			tags:     []string{"tag1", "", "tag2"},
			expected: "tag1,tag2",
		},
		{
			name:     "tags with whitespace",
			tags:     []string{" tag1 ", " tag2 "},
			expected: "tag1,tag2",
		},
		{
			name:     "only empty strings",
			tags:     []string{"", "", ""},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := joinTagsCSV(tt.tags)
			if result != tt.expected {
				t.Errorf("joinTagsCSV() = %q, want %q", result, tt.expected)
			}
		})
	}
}
