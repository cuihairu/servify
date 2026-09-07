package application_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	suggestionapp "servify/apps/server/internal/modules/suggestion/application"
	suggestioncontract "servify/apps/server/internal/modules/suggestion/contract"
)

type suggestionRepoStub struct {
	ticketTokens       []string
	ticketCandidateMax int
	docTokens          []string
	ticketRows         []suggestionapp.TicketCandidate
	docRows            []suggestionapp.KnowledgeDocCandidate
}

func (r *suggestionRepoStub) FindTicketCandidates(ctx context.Context, tokens []string, candidateMax int) ([]suggestionapp.TicketCandidate, error) {
	r.ticketTokens = append([]string(nil), tokens...)
	r.ticketCandidateMax = candidateMax
	out := make([]suggestionapp.TicketCandidate, len(r.ticketRows))
	copy(out, r.ticketRows)
	return out, nil
}

func (r *suggestionRepoStub) FindKnowledgeDocCandidates(ctx context.Context, tokens []string) ([]suggestionapp.KnowledgeDocCandidate, error) {
	r.docTokens = append([]string(nil), tokens...)
	out := make([]suggestionapp.KnowledgeDocCandidate, len(r.docRows))
	copy(out, r.docRows)
	return out, nil
}

func TestServiceSuggest_SortsAndTrimsResults(t *testing.T) {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	repo := &suggestionRepoStub{
		ticketRows: []suggestionapp.TicketCandidate{
			{ID: 2, Title: "beta", Status: "open", Category: "support", Priority: "high", CreatedAt: base},
			{ID: 1, Title: "alpha", Status: "pending", Category: "support", Priority: "medium", CreatedAt: base.Add(-time.Hour)},
			{ID: 3, Title: "gamma", Status: "closed", Category: "support", Priority: "low", CreatedAt: base.Add(-2 * time.Hour)},
		},
		docRows: []suggestionapp.KnowledgeDocCandidate{
			{ID: 9, Title: "alpha", Category: "faq", Tags: "alpha"},
			{ID: 4, Title: "beta", Category: "faq", Tags: "beta"},
		},
	}
	svc := suggestionapp.NewService(repo)

	resp, err := svc.Suggest(context.Background(), &suggestioncontract.SuggestionRequest{
		Query:              " alpha beta ",
		TicketLimit:        2,
		KnowledgeDocLimit:  2,
		CandidateTicketMax: 3,
	})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.Query != "alpha beta" {
		t.Fatalf("unexpected query: %q", resp.Query)
	}
	if resp.Intent.Label != "general" {
		t.Fatalf("unexpected intent: %+v", resp.Intent)
	}
	if !reflect.DeepEqual(repo.ticketTokens, []string{"alpha", "beta"}) {
		t.Fatalf("unexpected ticket tokens: %+v", repo.ticketTokens)
	}
	if !reflect.DeepEqual(repo.docTokens, []string{"alpha", "beta"}) {
		t.Fatalf("unexpected doc tokens: %+v", repo.docTokens)
	}
	if repo.ticketCandidateMax != 3 {
		t.Fatalf("unexpected candidate max: %d", repo.ticketCandidateMax)
	}
	if got, ok := resp.Meta["tokens"].([]string); !ok || !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("unexpected meta tokens: %#v", resp.Meta["tokens"])
	}
	if got, ok := resp.Meta["ticket_candidates"].(int); !ok || got != 3 {
		t.Fatalf("unexpected ticket candidates: %#v", resp.Meta["ticket_candidates"])
	}
	if got, ok := resp.Meta["doc_candidates"].(int); !ok || got != 2 {
		t.Fatalf("unexpected doc candidates: %#v", resp.Meta["doc_candidates"])
	}
	if len(resp.SimilarTickets) != 2 {
		t.Fatalf("unexpected ticket count: %d", len(resp.SimilarTickets))
	}
	if resp.SimilarTickets[0].ID != 2 || resp.SimilarTickets[1].ID != 1 {
		t.Fatalf("unexpected ticket order: %+v", resp.SimilarTickets)
	}
	if !resp.SimilarTickets[0].CreatedAt.Equal(base) || !resp.SimilarTickets[1].CreatedAt.Equal(base.Add(-time.Hour)) {
		t.Fatalf("unexpected ticket timestamps: %+v", resp.SimilarTickets)
	}
	if resp.SimilarTickets[0].Score != 0.5 || resp.SimilarTickets[1].Score != 0.5 {
		t.Fatalf("unexpected ticket scores: %+v", resp.SimilarTickets)
	}
	if len(resp.KnowledgeDocs) != 2 {
		t.Fatalf("unexpected doc count: %d", len(resp.KnowledgeDocs))
	}
	if resp.KnowledgeDocs[0].ID != 4 || resp.KnowledgeDocs[1].ID != 9 {
		t.Fatalf("unexpected doc order: %+v", resp.KnowledgeDocs)
	}
	if resp.KnowledgeDocs[0].Score != 0.5 || resp.KnowledgeDocs[1].Score != 0.5 {
		t.Fatalf("unexpected doc scores: %+v", resp.KnowledgeDocs)
	}
}

func TestServiceSuggest_DefaultsOnNilRequest(t *testing.T) {
	repo := &suggestionRepoStub{
		ticketRows: []suggestionapp.TicketCandidate{
			{ID: 1, Title: "alpha", Status: "open"},
		},
		docRows: []suggestionapp.KnowledgeDocCandidate{
			{ID: 1, Title: "alpha", Category: "faq"},
		},
	}
	svc := suggestionapp.NewService(repo)

	resp, err := svc.Suggest(context.Background(), nil)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.Query != "" {
		t.Fatalf("unexpected query: %q", resp.Query)
	}
	if resp.Intent.Label != "general" || resp.Intent.Confidence != 0.2 {
		t.Fatalf("unexpected intent: %+v", resp.Intent)
	}
	if repo.ticketCandidateMax != 200 {
		t.Fatalf("unexpected default candidate max: %d", repo.ticketCandidateMax)
	}
	if len(repo.ticketTokens) != 0 || len(repo.docTokens) != 0 {
		t.Fatalf("expected no tokens for empty query, got tickets=%+v docs=%+v", repo.ticketTokens, repo.docTokens)
	}
	if len(resp.SimilarTickets) != 0 || len(resp.KnowledgeDocs) != 0 {
		t.Fatalf("expected no suggestions for empty query, got %+v", resp)
	}
}
