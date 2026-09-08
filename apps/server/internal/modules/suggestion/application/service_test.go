package application_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
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

type suggestionRepoFailing struct {
	ticketErr bool
	docErr    bool
}

func (r *suggestionRepoFailing) FindTicketCandidates(ctx context.Context, tokens []string, candidateMax int) ([]suggestionapp.TicketCandidate, error) {
	if r.ticketErr {
		return nil, errors.New("ticket lookup failed")
	}
	return nil, nil
}

func (r *suggestionRepoFailing) FindKnowledgeDocCandidates(ctx context.Context, tokens []string) ([]suggestionapp.KnowledgeDocCandidate, error) {
	if r.docErr {
		return nil, errors.New("doc lookup failed")
	}
	return nil, nil
}

func TestServiceSuggest_PropagatesRepoErrors(t *testing.T) {
	if _, err := suggestionapp.NewService(&suggestionRepoFailing{ticketErr: true}).Suggest(context.Background(), &suggestioncontract.SuggestionRequest{Query: "alpha"}); err == nil {
		t.Fatal("expected ticket repo error")
	}
	if _, err := suggestionapp.NewService(&suggestionRepoFailing{docErr: true}).Suggest(context.Background(), &suggestioncontract.SuggestionRequest{Query: "alpha"}); err == nil {
		t.Fatal("expected doc repo error")
	}
}

func TestServiceSuggest_ClampsLimitsAndSkipsZeroScore(t *testing.T) {
	repo := &suggestionRepoStub{
		ticketRows: []suggestionapp.TicketCandidate{
			{ID: 1, Title: "alpha", Status: "open"},
			{ID: 2, Title: "unrelated", Status: "open"},
		},
		docRows: []suggestionapp.KnowledgeDocCandidate{
			{ID: 1, Title: "alpha"},
			{ID: 2, Title: "unrelated"},
		},
	}
	svc := suggestionapp.NewService(repo)
	resp, err := svc.Suggest(context.Background(), &suggestioncontract.SuggestionRequest{
		Query:              "alpha",
		TicketLimit:        50,
		KnowledgeDocLimit:  50,
		CandidateTicketMax: 5000,
	})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if len(resp.SimilarTickets) != 1 || resp.SimilarTickets[0].ID != 1 {
		t.Fatalf("unexpected tickets: %+v", resp.SimilarTickets)
	}
	if len(resp.KnowledgeDocs) != 1 || resp.KnowledgeDocs[0].ID != 1 {
		t.Fatalf("unexpected docs: %+v", resp.KnowledgeDocs)
	}
	if repo.ticketCandidateMax != 1000 {
		t.Fatalf("candidate max clamped to %d, want 1000", repo.ticketCandidateMax)
	}
}

func TestBuildLikeWhereTokens_SkipsEmptyTokens(t *testing.T) {
	where, args := suggestionapp.BuildLikeWhereTokens([]string{"title"}, []string{"", "api"}, 3)
	if where != "(title LIKE ?)" || len(args) != 1 {
		t.Fatalf("where=%q args=%v", where, args)
	}
	if where, args := suggestionapp.BuildLikeWhereTokens([]string{"title"}, []string{""}, 3); where != "" || args != nil {
		t.Fatalf("all-empty tokens should return empty, got %q %v", where, args)
	}
}

func TestClassifyIntent_ConfidenceCap(t *testing.T) {
	result := suggestionapp.ClassifyIntent("complaint angry refund invoice payment")
	if result.Label != "billing" && result.Label != "complaint" {
		t.Fatalf("unexpected label: %+v", result)
	}
	if result.Confidence > 0.95 {
		t.Fatalf("confidence should be capped at 0.95, got %f", result.Confidence)
	}
	if len(result.Matches) == 0 {
		t.Fatal("expected keyword matches")
	}
}

func TestServiceSuggest_TicketTiebreakAndTrim(t *testing.T) {
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	repo := &suggestionRepoStub{
		ticketRows: []suggestionapp.TicketCandidate{
			{ID: 1, Title: "alpha", Status: "open", CreatedAt: base},
			{ID: 2, Title: "alpha", Status: "open", CreatedAt: base.Add(time.Hour)},
			{ID: 3, Title: "alpha", Status: "open", CreatedAt: base.Add(2 * time.Hour)},
		},
		docRows: []suggestionapp.KnowledgeDocCandidate{
			{ID: 3, Title: "alpha"},
			{ID: 2, Title: "alpha"},
			{ID: 1, Title: "alpha"},
		},
	}
	svc := suggestionapp.NewService(repo)
	resp, err := svc.Suggest(context.Background(), &suggestioncontract.SuggestionRequest{
		Query:             "alpha",
		TicketLimit:       2,
		KnowledgeDocLimit: 2,
	})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if len(resp.SimilarTickets) != 2 {
		t.Fatalf("expected trimmed tickets, got %+v", resp.SimilarTickets)
	}
	if resp.SimilarTickets[0].ID != 3 || resp.SimilarTickets[1].ID != 2 {
		t.Fatalf("expected newest first on tie, got %+v", resp.SimilarTickets)
	}
	if len(resp.KnowledgeDocs) != 2 {
		t.Fatalf("expected trimmed docs, got %+v", resp.KnowledgeDocs)
	}
	if resp.KnowledgeDocs[0].ID != 1 || resp.KnowledgeDocs[1].ID != 2 {
		t.Fatalf("expected ascending id on tie, got %+v", resp.KnowledgeDocs)
	}
}

func TestExtractTokens_EdgeCases(t *testing.T) {
	if got := suggestionapp.ExtractTokens("!!! ???"); got != nil {
		t.Fatalf("punctuation-only tokens = %v", got)
	}
	long := strings.Repeat("a", 40)
	if got := suggestionapp.ExtractTokens("short " + long); len(got) != 1 || got[0] != "short" {
		t.Fatalf("expected long token dropped, got %v", got)
	}
}

func TestBuildLikeWhereTokens_StopsAtMaxTokens(t *testing.T) {
	where, args := suggestionapp.BuildLikeWhereTokens([]string{"title"}, []string{"a", "b", "c"}, 2)
	if where != "(title LIKE ?) OR (title LIKE ?)" || len(args) != 2 {
		t.Fatalf("where=%q args=%v", where, args)
	}
}

func TestClassifyIntent_ManyHitsCapConfidence(t *testing.T) {
	result := suggestionapp.ClassifyIntent("发票 付款 支付 收费 价格")
	if result.Label != "billing" {
		t.Fatalf("label = %q", result.Label)
	}
	if result.Confidence != 0.95 {
		t.Fatalf("confidence = %f, want 0.95", result.Confidence)
	}
}
