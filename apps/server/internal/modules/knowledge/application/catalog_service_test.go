package application

import (
	"context"
	"testing"

	"servify/apps/server/internal/modules/knowledge/domain"
)

type catalogBaseStub struct {
	created *domain.KnowledgeBase
	list    []domain.KnowledgeBase
	err     error
}

func (s *catalogBaseStub) Create(_ context.Context, base *domain.KnowledgeBase) error {
	s.created = base
	return s.err
}
func (s *catalogBaseStub) List(context.Context) ([]domain.KnowledgeBase, error) { return s.list, s.err }

type catalogChunkStub struct {
	gotID string
	rows  []domain.Chunk
	err   error
}

func (s *catalogChunkStub) ListByDocument(_ context.Context, id string) ([]domain.Chunk, error) {
	s.gotID = id
	return s.rows, s.err
}

func TestCatalogServiceValidatesAndDelegates(t *testing.T) {
	bases, chunks := &catalogBaseStub{}, &catalogChunkStub{rows: []domain.Chunk{{Sequence: 0, Content: "entry"}}}
	svc := NewCatalogService(bases, chunks)
	if _, err := svc.CreateKnowledgeBase(context.Background(), "  ", false); err == nil {
		t.Fatal("blank knowledge base name accepted")
	}
	base, err := svc.CreateKnowledgeBase(context.Background(), " Policies ", true)
	if err != nil {
		t.Fatalf("create knowledge base: %v", err)
	}
	if base.Name != "Policies" || !base.IsPublic || bases.created != base {
		t.Fatalf("unexpected created base: %+v", base)
	}
	rows, err := svc.ListChunks(context.Background(), "doc-9")
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	if chunks.gotID != "doc-9" || len(rows) != 1 || rows[0].Sequence != 0 {
		t.Fatalf("unexpected chunk result: %+v", rows)
	}
	if _, err := svc.ListChunks(context.Background(), " "); err == nil {
		t.Fatal("blank document id accepted")
	}
}
