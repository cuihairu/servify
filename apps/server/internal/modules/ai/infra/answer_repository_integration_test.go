//go:build integration
// +build integration

package infra

// V1.0 收敛 B3-1b：ai_answers / answer_feedback 的 sqlite 集成行为——scope
// 隔离、聚合读口（top 问答/无命中/低置信/反馈计数）。

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/modules/ai/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newAIInfraTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.AIAnswer{}, &domain.AnswerFeedback{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestAIAnswerRepositoryScopeAndRoundTrip(t *testing.T) {
	db := newAIInfraTestDB(t)
	repo := NewGormAnswerRepository(db)

	ctx := platformauth.ContextWithScope(context.Background(), "t-ai", "w-ai")
	answer := &domain.AIAnswer{
		SessionID: "conv-1", Query: "退款政策是什么", Answer: "7 天无理由",
		Confidence: 0.9, Strategy: "weknora",
		SourcesJSON: `[{"document_id":"d1","title":"refund","score":0.91}]`,
	}
	if err := repo.Create(ctx, answer); err != nil {
		t.Fatalf("create: %v", err)
	}
	if answer.ID == 0 || answer.TenantID != "t-ai" || answer.WorkspaceID != "w-ai" {
		t.Fatalf("answer = %+v（scope 必须落库）", answer)
	}

	other := platformauth.ContextWithScope(context.Background(), "t-other", "w-other")
	if _, err := repo.Get(other, answer.ID); err == nil {
		t.Fatal("expected cross-tenant get to miss")
	}
	loaded, err := repo.Get(ctx, answer.ID)
	if err != nil || loaded.Query != answer.Query || loaded.SourcesJSON == "" {
		t.Fatalf("loaded = %+v err %v", loaded, err)
	}
}

func TestAIAnswerAnalyticsQueries(t *testing.T) {
	db := newAIInfraTestDB(t)
	repo := NewGormAnswerRepository(db)
	ctx := context.Background()

	now := time.Now()
	seed := []domain.AIAnswer{
		{SessionID: "s1", Query: "退款政策是什么", Confidence: 0.9, Strategy: "weknora", SourcesJSON: `[{"document_id":"d1"}]`, CreatedAt: now},
		{SessionID: "s1", Query: "退款政策是什么", Confidence: 0.88, Strategy: "weknora", SourcesJSON: `[{"document_id":"d1"}]`, CreatedAt: now},
		{SessionID: "s2", Query: "怎么开发票", Confidence: 0.2, Strategy: "llm", SourcesJSON: "", CreatedAt: now},
		{SessionID: "s3", Query: "怎么退运费", Confidence: 0.5, Strategy: "llm", SourcesJSON: "", CreatedAt: now},
	}
	for i := range seed {
		if err := repo.Create(ctx, &seed[i]); err != nil {
			t.Fatalf("seed[%d]: %v", i, err)
		}
	}
	// 窗口外的行不计。
	stale := domain.AIAnswer{Query: "去年的问题", Confidence: 0.1, CreatedAt: now.AddDate(0, 0, -30)}
	if err := repo.Create(ctx, &stale); err != nil {
		t.Fatalf("seed stale: %v", err)
	}

	since := now.AddDate(0, 0, -7)
	total, hit, lowConf, confSum, err := repo.CountInWindow(ctx, since)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 4 || hit != 2 || lowConf != 2 {
		t.Fatalf("count = %d/%d/%d want 4/2/2", total, hit, lowConf)
	}
	if confSum < 2.47 || confSum > 2.49 {
		t.Fatalf("confSum = %f want ~2.48", confSum)
	}

	top, err := repo.TopQuestions(ctx, since, 10, true)
	if err != nil || len(top) != 1 || top[0].Query != "退款政策是什么" || top[0].Count != 2 {
		t.Fatalf("top = %+v err %v", top, err)
	}
	noHit, err := repo.TopQuestions(ctx, since, 10, false)
	if err != nil || len(noHit) != 2 {
		t.Fatalf("nohit = %+v err %v", noHit, err)
	}
	low, err := repo.LowConfidenceQuestions(ctx, since, 10)
	if err != nil || len(low) != 2 || low[0].AvgConfidence > low[1].AvgConfidence {
		t.Fatalf("low = %+v err %v（必须按置信升序）", low, err)
	}

	if _, _, err := repo.CountFeedback(ctx, since); err != nil {
		t.Fatalf("feedback count: %v", err)
	}
	feedbackRepo := NewGormAnswerFeedbackRepository(db)
	for _, helpful := range []bool{true, true, false} {
		if err := feedbackRepo.Create(ctx, &domain.AnswerFeedback{AnswerID: seed[0].ID, Helpful: helpful, CreatedAt: now}); err != nil {
			t.Fatalf("create feedback: %v", err)
		}
	}
	helpful, notHelpful, err := repo.CountFeedback(ctx, since)
	if err != nil || helpful != 2 || notHelpful != 1 {
		t.Fatalf("feedback = %d/%d err %v want 2/1", helpful, notHelpful, err)
	}
}
