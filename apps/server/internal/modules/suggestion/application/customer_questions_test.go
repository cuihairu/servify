package application_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	suggestionapp "servify/apps/server/internal/modules/suggestion/application"
	suggestioncontract "servify/apps/server/internal/modules/suggestion/contract"
)

func TestServiceInitialQuestions(t *testing.T) {
	t.Run("returns deduped knowledge doc titles in recency order", func(t *testing.T) {
		repo := &suggestionRepoStub{
			publicDocRows: []suggestionapp.KnowledgeDocCandidate{
				{ID: 3, Title: "如何重置密码", Category: "account"},
				{ID: 2, Title: "如何导出账单", Category: "billing"},
				{ID: 1, Title: "如何重置密码", Category: "account"},
				{ID: 9, Title: "  ", Category: "faq"},
			},
		}
		svc := suggestionapp.NewService(repo)

		resp, err := svc.InitialQuestions(context.Background(), &suggestioncontract.InitialQuestionsRequest{Limit: 8})
		if err != nil {
			t.Fatalf("InitialQuestions() error = %v", err)
		}
		if repo.publicDocLimit != 8 {
			t.Fatalf("limit forwarded = %d, want 8", repo.publicDocLimit)
		}
		want := []string{"如何重置密码", "如何导出账单"}
		got := make([]string, 0, len(resp.Questions))
		for _, q := range resp.Questions {
			got = append(got, q.Question)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("questions = %v, want %v", got, want)
		}
		if resp.Questions[0].Source != "knowledge_doc" || resp.Questions[0].SourceID != "3" {
			t.Fatalf("source fields = %+v", resp.Questions[0])
		}
		if resp.Questions[0].Score <= resp.Questions[1].Score {
			t.Fatalf("recency scores must decrease: %v vs %v", resp.Questions[0].Score, resp.Questions[1].Score)
		}
		if resp.Meta["strategy"] != "public_knowledge_recency" {
			t.Fatalf("meta strategy = %v", resp.Meta["strategy"])
		}
	})

	t.Run("nil request uses default limit", func(t *testing.T) {
		repo := &suggestionRepoStub{}
		svc := suggestionapp.NewService(repo)
		if _, err := svc.InitialQuestions(context.Background(), nil); err != nil {
			t.Fatalf("InitialQuestions(nil) error = %v", err)
		}
		if repo.publicDocLimit != 8 {
			t.Fatalf("default limit = %d, want 8", repo.publicDocLimit)
		}
	})

	t.Run("limit is capped at 20", func(t *testing.T) {
		repo := &suggestionRepoStub{}
		svc := suggestionapp.NewService(repo)
		if _, err := svc.InitialQuestions(context.Background(), &suggestioncontract.InitialQuestionsRequest{Limit: 100}); err != nil {
			t.Fatalf("InitialQuestions() error = %v", err)
		}
		if repo.publicDocLimit != 20 {
			t.Fatalf("capped limit = %d, want 20", repo.publicDocLimit)
		}
	})

	t.Run("repository error propagates", func(t *testing.T) {
		repo := &errRepoStub{}
		svc := suggestionapp.NewService(repo)
		if _, err := svc.InitialQuestions(context.Background(), nil); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestServiceNextQuestions(t *testing.T) {
	t.Run("scores public docs and sorts by score desc", func(t *testing.T) {
		repo := &suggestionRepoStub{
			publicDocRows: []suggestionapp.KnowledgeDocCandidate{
				{ID: 1, Title: "密码重置指南", Content: "忘记密码时重置", Category: "account", Tags: "密码"},
				{ID: 2, Title: "发票开具流程", Content: "增值税发票", Category: "billing", Tags: "发票"},
			},
		}
		svc := suggestionapp.NewService(repo)

		resp, err := svc.NextQuestions(context.Background(), &suggestioncontract.NextQuestionsRequest{Query: " 密码 重置 "})
		if err != nil {
			t.Fatalf("NextQuestions() error = %v", err)
		}
		if len(resp.Questions) != 1 || resp.Questions[0].Question != "密码重置指南" {
			t.Fatalf("questions = %+v", resp.Questions)
		}
		if resp.Questions[0].Source != "knowledge_doc" || resp.Questions[0].SourceID != "1" {
			t.Fatalf("source fields = %+v", resp.Questions[0])
		}
		if !reflect.DeepEqual(repo.publicDocTokens, []string{"密", "码", "重", "置"}) {
			t.Fatalf("tokens = %v", repo.publicDocTokens)
		}
		if resp.Query != "密码 重置" {
			t.Fatalf("query = %q", resp.Query)
		}
		if resp.Meta["strategy"] != "public_knowledge_scored" || resp.Meta["intent"] == "" {
			t.Fatalf("meta = %+v", resp.Meta)
		}
	})

	t.Run("truncates to limit and dedupes blank titles", func(t *testing.T) {
		repo := &suggestionRepoStub{
			publicDocRows: []suggestionapp.KnowledgeDocCandidate{
				{ID: 1, Title: "密码重置指南", Content: "密码 重置", Tags: "密码"},
				{ID: 2, Title: "密码重置指南", Content: "密码 重置", Tags: "密码"},
				{ID: 3, Title: "  ", Content: "密码 重置", Tags: "密码"},
				{ID: 4, Title: "改绑手机号", Content: "密码 重置 手机", Tags: "密码"},
			},
		}
		svc := suggestionapp.NewService(repo)

		resp, err := svc.NextQuestions(context.Background(), &suggestioncontract.NextQuestionsRequest{Query: "密码", Limit: 1})
		if err != nil {
			t.Fatalf("NextQuestions() error = %v", err)
		}
		if len(resp.Questions) != 1 || resp.Questions[0].Question != "密码重置指南" {
			t.Fatalf("questions = %+v", resp.Questions)
		}
	})

	t.Run("nil request defaults query empty", func(t *testing.T) {
		repo := &suggestionRepoStub{}
		svc := suggestionapp.NewService(repo)
		resp, err := svc.NextQuestions(context.Background(), nil)
		if err != nil {
			t.Fatalf("NextQuestions(nil) error = %v", err)
		}
		if resp.Query != "" || resp.Questions == nil || resp.Meta["intent"] != "general" {
			t.Fatalf("resp = %+v", resp)
		}
	})

	t.Run("repository error propagates", func(t *testing.T) {
		repo := &errRepoStub{}
		svc := suggestionapp.NewService(repo)
		if _, err := svc.NextQuestions(context.Background(), &suggestioncontract.NextQuestionsRequest{Query: "密码"}); err == nil {
			t.Fatal("expected error")
		}
	})
}

// errRepoStub 所有查询恒定失败，验证错误传播路径。
type errRepoStub struct{}

func (r *errRepoStub) FindTicketCandidates(ctx context.Context, tokens []string, candidateMax int) ([]suggestionapp.TicketCandidate, error) {
	return nil, errors.New("boom")
}

func (r *errRepoStub) FindKnowledgeDocCandidates(ctx context.Context, tokens []string) ([]suggestionapp.KnowledgeDocCandidate, error) {
	return nil, errors.New("boom")
}

func (r *errRepoStub) FindPublicKnowledgeDocs(ctx context.Context, limit int) ([]suggestionapp.KnowledgeDocCandidate, error) {
	return nil, errors.New("boom")
}

func (r *errRepoStub) FindPublicKnowledgeDocCandidates(ctx context.Context, tokens []string) ([]suggestionapp.KnowledgeDocCandidate, error) {
	return nil, errors.New("boom")
}
