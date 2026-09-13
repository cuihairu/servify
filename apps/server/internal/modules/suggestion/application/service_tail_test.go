package application_test

import (
	"context"
	"testing"
	"time"

	suggestionapp "servify/apps/server/internal/modules/suggestion/application"
	suggestioncontract "servify/apps/server/internal/modules/suggestion/contract"
)

// 覆盖 suggestTickets / suggestKnowledgeDocs 排序中「分数不等」的分支：
// 同查询下两条候选得分不同（0.5 vs 1.0），必须按分数降序排列。
func TestServiceSuggest_OrdersDistinctScoresFirst(t *testing.T) {
	base := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	repo := &suggestionRepoStub{
		ticketRows: []suggestionapp.TicketCandidate{
			{ID: 1, Title: "alpha alpha", Status: "open", CreatedAt: base.Add(2 * time.Hour)},
			{ID: 2, Title: "alpha beta", Status: "open", CreatedAt: base.Add(1 * time.Hour)},
		},
		docRows: []suggestionapp.KnowledgeDocCandidate{
			{ID: 10, Title: "alpha alpha", Content: "x", Tags: ""},
			{ID: 11, Title: "alpha beta", Content: "x", Tags: ""},
		},
	}
	resp, err := suggestionapp.NewService(repo).Suggest(context.Background(), &suggestioncontract.SuggestionRequest{
		Query: "alpha beta",
	})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if len(resp.SimilarTickets) != 2 || resp.SimilarTickets[0].ID != 2 || resp.SimilarTickets[1].ID != 1 {
		t.Fatalf("expected score-desc ticket order [2 1], got %+v", resp.SimilarTickets)
	}
	if resp.SimilarTickets[0].Score != 1.0 || resp.SimilarTickets[1].Score != 0.5 {
		t.Fatalf("unexpected scores: %+v", resp.SimilarTickets)
	}
	if len(resp.KnowledgeDocs) != 2 || resp.KnowledgeDocs[0].ID != 11 || resp.KnowledgeDocs[1].ID != 10 {
		t.Fatalf("expected score-desc doc order [11 10], got %+v", resp.KnowledgeDocs)
	}
}
