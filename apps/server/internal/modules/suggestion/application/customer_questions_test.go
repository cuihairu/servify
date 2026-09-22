package application_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

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

	// score 不等时按命中率降序：query 四个 token，ID1 全命中(1.0)、ID4 命中
	// 密/码两个(0.5)，排序比较走 score 不等分支。
	t.Run("sorts by score descending", func(t *testing.T) {
		repo := &suggestionRepoStub{
			publicDocRows: []suggestionapp.KnowledgeDocCandidate{
				{ID: 1, Title: "密码咨询", Content: "账号", Tags: ""},
				{ID: 2, Title: "密码重置指南", Content: "密码 重置", Tags: ""},
			},
		}
		svc := suggestionapp.NewService(repo)

		resp, err := svc.NextQuestions(context.Background(), &suggestioncontract.NextQuestionsRequest{Query: "密码重置"})
		if err != nil {
			t.Fatalf("NextQuestions() error = %v", err)
		}
		if len(resp.Questions) != 2 || resp.Questions[0].Question != "密码重置指南" || resp.Questions[0].Score <= resp.Questions[1].Score {
			t.Fatalf("expected score-descending order, got %+v", resp.Questions)
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

func (r *errRepoStub) RecordExposure(ctx context.Context, rec suggestionapp.ExposureRecord) error {
	return errors.New("boom")
}

func (r *errRepoStub) FindLatestOpenExposure(ctx context.Context, sessionID string) (*suggestionapp.OpenExposure, error) {
	return nil, errors.New("boom")
}

func (r *errRepoStub) MarkExposureConverted(ctx context.Context, exposureID uint, question string, at time.Time) error {
	return errors.New("boom")
}

func (r *errRepoStub) ExposureSummary(ctx context.Context) (*suggestionapp.ExposureSummary, error) {
	return nil, errors.New("boom")
}

func TestServiceInitialQuestionsRecordsExposure(t *testing.T) {
	t.Run("records session and returned questions", func(t *testing.T) {
		repo := &suggestionRepoStub{
			publicDocRows: []suggestionapp.KnowledgeDocCandidate{
				{ID: 1, Title: " 密码重置指南 "},
				{ID: 2, Title: "  "}, // 空白标题不入曝光列表
			},
		}
		svc := suggestionapp.NewService(repo)

		if _, err := svc.InitialQuestions(context.Background(), &suggestioncontract.InitialQuestionsRequest{SessionID: " s-1 "}); err != nil {
			t.Fatalf("InitialQuestions() error = %v", err)
		}
		if len(repo.exposures) != 1 {
			t.Fatalf("exposures = %d, want 1", len(repo.exposures))
		}
		rec := repo.exposures[0]
		if rec.SessionID != "s-1" || rec.Kind != "initial" || rec.Strategy != "public_knowledge_recency" {
			t.Fatalf("exposure = %+v", rec)
		}
		if !reflect.DeepEqual(rec.Questions, []string{"密码重置指南"}) {
			t.Fatalf("exposed questions = %v", rec.Questions)
		}
	})

	t.Run("nil request records empty session", func(t *testing.T) {
		repo := &suggestionRepoStub{}
		svc := suggestionapp.NewService(repo)
		if _, err := svc.InitialQuestions(context.Background(), nil); err != nil {
			t.Fatalf("InitialQuestions(nil) error = %v", err)
		}
		if len(repo.exposures) != 1 || repo.exposures[0].SessionID != "" || len(repo.exposures[0].Questions) != 0 {
			t.Fatalf("exposures = %+v", repo.exposures)
		}
	})
}

func TestServiceNextQuestionsRecordsExposure(t *testing.T) {
	repo := &suggestionRepoStub{
		publicDocRows: []suggestionapp.KnowledgeDocCandidate{
			{ID: 1, Title: "密码重置指南", Content: "密码 重置", Tags: "密码"},
			{ID: 2, Title: "密码重置指南", Content: "密码 重置", Tags: "密码"}, // 同题去重
		},
	}
	svc := suggestionapp.NewService(repo)

	resp, err := svc.NextQuestions(context.Background(), &suggestioncontract.NextQuestionsRequest{Query: "密码", SessionID: "s-2", Limit: 5})
	if err != nil {
		t.Fatalf("NextQuestions() error = %v", err)
	}
	if len(repo.exposures) != 1 {
		t.Fatalf("exposures = %d, want 1", len(repo.exposures))
	}
	rec := repo.exposures[0]
	if rec.SessionID != "s-2" || rec.Kind != "next" || rec.Strategy != "public_knowledge_scored" {
		t.Fatalf("exposure = %+v", rec)
	}
	// 只记实际返回（去重、截断后）的问题列表
	want := make([]string, 0, len(resp.Questions))
	for _, q := range resp.Questions {
		want = append(want, q.Question)
	}
	if !reflect.DeepEqual(rec.Questions, want) || len(want) != 1 {
		t.Fatalf("exposed = %v, want %v", rec.Questions, want)
	}
}

func TestNormalizeQuestionText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"collapses whitespace", "  如何   重置\t密码 \n? ", "如何 重置 密码 ?"},
		{"lowercases ascii", "Reset  PASSWORD", "reset password"},
		{"blank", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := suggestionapp.NormalizeQuestionText(tt.in); got != tt.want {
				t.Fatalf("NormalizeQuestionText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestServiceMatchSuggestionConversion(t *testing.T) {
	t.Run("blank session or content skips repo", func(t *testing.T) {
		repo := &suggestionRepoStub{}
		svc := suggestionapp.NewService(repo)
		if err := svc.MatchSuggestionConversion(context.Background(), "", "密码"); err != nil {
			t.Fatalf("error = %v", err)
		}
		if err := svc.MatchSuggestionConversion(context.Background(), "s-1", "   "); err != nil {
			t.Fatalf("error = %v", err)
		}
		if repo.findOpenCalls != 0 || len(repo.markCalls) != 0 {
			t.Fatalf("repo must not be touched, findOpenCalls=%d markCalls=%d", repo.findOpenCalls, len(repo.markCalls))
		}
	})

	t.Run("no open exposure is a no-op", func(t *testing.T) {
		repo := &suggestionRepoStub{}
		svc := suggestionapp.NewService(repo)
		if err := svc.MatchSuggestionConversion(context.Background(), "s-1", "密码"); err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(repo.markCalls) != 0 {
			t.Fatalf("markCalls = %+v", repo.markCalls)
		}
	})

	t.Run("repo error propagates", func(t *testing.T) {
		svc := suggestionapp.NewService(&errRepoStub{})
		if err := svc.MatchSuggestionConversion(context.Background(), "s-1", "密码"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("normalizes whitespace and case on hit", func(t *testing.T) {
		repo := &suggestionRepoStub{
			openExposure: &suggestionapp.OpenExposure{ID: 7, Questions: []string{"Reset  Password", "导出账单"}},
		}
		svc := suggestionapp.NewService(repo)
		if err := svc.MatchSuggestionConversion(context.Background(), "s-1", " reset password "); err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(repo.markCalls) != 1 {
			t.Fatalf("markCalls = %+v", repo.markCalls)
		}
		mark := repo.markCalls[0]
		// 归因回填的是曝光时的原文案，不是客户输入
		if mark.id != 7 || mark.question != "Reset  Password" {
			t.Fatalf("mark = %+v", mark)
		}
		if mark.at.IsZero() {
			t.Fatal("mark time must be set")
		}
	})

	t.Run("miss leaves exposure open", func(t *testing.T) {
		repo := &suggestionRepoStub{
			openExposure: &suggestionapp.OpenExposure{ID: 7, Questions: []string{"导出账单"}},
		}
		svc := suggestionapp.NewService(repo)
		if err := svc.MatchSuggestionConversion(context.Background(), "s-1", "重置密码"); err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(repo.markCalls) != 0 {
			t.Fatalf("markCalls = %+v", repo.markCalls)
		}
	})

	t.Run("mark error propagates", func(t *testing.T) {
		repo := &suggestionRepoStub{
			openExposure: &suggestionapp.OpenExposure{ID: 7, Questions: []string{"导出账单"}},
			markErr:      errors.New("boom"),
		}
		svc := suggestionapp.NewService(repo)
		if err := svc.MatchSuggestionConversion(context.Background(), "s-1", "导出账单"); err == nil {
			t.Fatal("expected error")
		}
	})
}
