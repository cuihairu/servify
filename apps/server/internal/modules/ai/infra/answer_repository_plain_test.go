package infra

// 裸模式（无 integration tag）单测：ai_answers / answer_feedback 仓储的
// nil 入参防御、scope 隔离矩阵与窗口/阈值边界。与 integration 侧
// （answer_repository_integration_test.go / answer_repository_coverage_test.go）
// 互补：这里用表驱动钉死边界语义（created_at >= since 含端点、低置信
// 严格小于阈值、反馈显式作用域优先于上下文），语句级错误注入留在
// integration 侧。

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"testing"
	"time"

	aiapp "servify/apps/server/internal/modules/ai/application"
	"servify/apps/server/internal/modules/ai/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var plainAnswerDBSeq atomic.Uint32

func newPlainAnswerDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:plain_answer_%d?mode=memory&cache=shared", plainAnswerDBSeq.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.AIAnswer{}, &domain.AnswerFeedback{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestGormAnswerPlainNilGuardsAndStamping(t *testing.T) {
	db := newPlainAnswerDB(t)
	repo := NewGormAnswerRepository(db)
	feedbackRepo := NewGormAnswerFeedbackRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "t-plain", "w-plain")

	if err := repo.Create(ctx, nil); !errors.Is(err, gorm.ErrInvalidValue) {
		t.Fatalf("nil answer: expected ErrInvalidValue, got %v", err)
	}
	if err := feedbackRepo.Create(ctx, nil); !errors.Is(err, gorm.ErrInvalidValue) {
		t.Fatalf("nil feedback: expected ErrInvalidValue, got %v", err)
	}

	answer := &domain.AIAnswer{
		SessionID: "s1", Query: "退货要邮费吗", Answer: "7 天无理由",
		Confidence: 0.9, Strategy: "weknora",
		SourcesJSON: `[{"document_id":"d1"}]`,
	}
	if err := repo.Create(ctx, answer); err != nil {
		t.Fatalf("create: %v", err)
	}
	if answer.ID == 0 || answer.TenantID != "t-plain" || answer.WorkspaceID != "w-plain" {
		t.Fatalf("answer = %+v（scope 必须落库）", answer)
	}
	loaded, err := repo.Get(ctx, answer.ID)
	if err != nil || loaded.Query != answer.Query || loaded.SourcesJSON == "" {
		t.Fatalf("loaded = %+v err %v", loaded, err)
	}
}

func TestGormAnswerPlainGetScopeMatrix(t *testing.T) {
	db := newPlainAnswerDB(t)
	repo := NewGormAnswerRepository(db)
	seedCtx := platformauth.ContextWithScope(context.Background(), "t1", "w1")
	answer := &domain.AIAnswer{SessionID: "s1", Query: "q", Confidence: 0.9, Strategy: "weknora"}
	if err := repo.Create(seedCtx, answer); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cases := []struct {
		name      string
		tenant    string
		workspace string
		visible   bool
	}{
		{"same tenant same workspace", "t1", "w1", true},
		{"same tenant other workspace", "t1", "w2", false},
		{"other tenant same workspace", "t2", "w1", false},
		{"no scope sees all", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := platformauth.ContextWithScope(context.Background(), tc.tenant, tc.workspace)
			got, err := repo.Get(ctx, answer.ID)
			if tc.visible {
				if err != nil || got == nil {
					t.Fatalf("expected visible, got %v %+v", err, got)
				}
				return
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("expected ErrRecordNotFound, got %v %+v", err, got)
			}
		})
	}
}

func TestGormAnswerPlainWindowBoundaryInclusive(t *testing.T) {
	db := newPlainAnswerDB(t)
	repo := NewGormAnswerRepository(db)
	ctx := context.Background()

	since := time.Now().Truncate(time.Second)
	inWindow := domain.AIAnswer{SessionID: "s1", Query: "q", Confidence: 0.9, Strategy: "llm", SourcesJSON: `[{"document_id":"d1"}]`, CreatedAt: since}
	stale := domain.AIAnswer{SessionID: "s2", Query: "old", Confidence: 0.1, Strategy: "llm", CreatedAt: since.Add(-24 * time.Hour)}
	if err := repo.Create(ctx, &inWindow); err != nil {
		t.Fatalf("seed in-window: %v", err)
	}
	if err := repo.Create(ctx, &stale); err != nil {
		t.Fatalf("seed stale: %v", err)
	}

	// created_at >= since 含端点：恰好压线的行计入窗口。
	total, hit, lowConf, confSum, err := repo.CountInWindow(ctx, since)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 1 || hit != 1 || lowConf != 0 {
		t.Fatalf("count = %d/%d/%d want 1/1/0", total, hit, lowConf)
	}
	if math.Abs(confSum-0.9) > 1e-6 {
		t.Fatalf("confSum = %f want 0.9", confSum)
	}
}

func TestGormAnswerPlainTopQuestionsHitSplit(t *testing.T) {
	db := newPlainAnswerDB(t)
	repo := NewGormAnswerRepository(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	seed := []domain.AIAnswer{
		{SessionID: "s1", Query: "退款政策", Confidence: 0.9, Strategy: "weknora", SourcesJSON: `[{"document_id":"d1"}]`, CreatedAt: now},
		{SessionID: "s2", Query: "退款政策", Confidence: 0.88, Strategy: "weknora", SourcesJSON: `[{"document_id":"d1"}]`, CreatedAt: now},
		{SessionID: "s3", Query: "开发票", Confidence: 0.9, Strategy: "weknora", SourcesJSON: `[{"document_id":"d2"}]`, CreatedAt: now},
		{SessionID: "s4", Query: "零命中问题", Confidence: 0.5, Strategy: "llm", SourcesJSON: "", CreatedAt: now},
	}
	for i := range seed {
		if err := repo.Create(ctx, &seed[i]); err != nil {
			t.Fatalf("seed[%d]: %v", i, err)
		}
	}
	since := now.Add(-time.Hour)

	hits, err := repo.TopQuestions(ctx, since, 10, true)
	if err != nil || len(hits) != 2 {
		t.Fatalf("hits = %+v err %v（只统计带引用提问）", hits, err)
	}
	if hits[0].Query != "退款政策" || hits[0].Count != 2 {
		t.Fatalf("hits[0] = %+v（count DESC 置顶）", hits[0])
	}
	if hits[1].Query != "开发票" || hits[1].Count != 1 {
		t.Fatalf("hits[1] = %+v", hits[1])
	}
	if math.Abs(hits[0].AvgConfidence-0.89) > 1e-6 {
		t.Fatalf("avg = %f want 0.89", hits[0].AvgConfidence)
	}

	top1, err := repo.TopQuestions(ctx, since, 1, true)
	if err != nil || len(top1) != 1 || top1[0].Query != "退款政策" {
		t.Fatalf("limit=1 → %+v err %v", top1, err)
	}

	noHits, err := repo.TopQuestions(ctx, since, 10, false)
	if err != nil || len(noHits) != 1 || noHits[0].Query != "零命中问题" {
		t.Fatalf("nohits = %+v err %v（只统计零命中提问）", noHits, err)
	}
}

func TestGormAnswerPlainLowConfidenceThresholdBoundary(t *testing.T) {
	db := newPlainAnswerDB(t)
	repo := NewGormAnswerRepository(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	seed := []domain.AIAnswer{
		{SessionID: "s1", Query: "q-low", Confidence: 0.30, Strategy: "llm", CreatedAt: now},
		{SessionID: "s2", Query: "q-mid", Confidence: 0.64, Strategy: "llm", CreatedAt: now},
		// 恰在阈值上的不算低置信（判定是严格小于 LowConfidenceThreshold）。
		{SessionID: "s3", Query: "q-edge", Confidence: aiapp.LowConfidenceThreshold, Strategy: "llm", CreatedAt: now},
		{SessionID: "s4", Query: "q-high", Confidence: 0.9, Strategy: "weknora", CreatedAt: now},
	}
	for i := range seed {
		if err := repo.Create(ctx, &seed[i]); err != nil {
			t.Fatalf("seed[%d]: %v", i, err)
		}
	}
	since := now.Add(-time.Hour)

	low, err := repo.LowConfidenceQuestions(ctx, since, 10)
	if err != nil {
		t.Fatalf("low: %v", err)
	}
	if len(low) != 2 {
		t.Fatalf("low = %+v want 2 rows（阈值上的 q-edge 不计入）", low)
	}
	if low[0].Query != "q-low" || low[1].Query != "q-mid" {
		t.Fatalf("low = %+v（必须按平均置信升序）", low)
	}

	top1, err := repo.LowConfidenceQuestions(ctx, since, 1)
	if err != nil || len(top1) != 1 || top1[0].Query != "q-low" {
		t.Fatalf("limit=1 → %+v err %v", top1, err)
	}
}

func TestGormAnswerPlainFeedbackStampMatrix(t *testing.T) {
	db := newPlainAnswerDB(t)
	repo := NewGormAnswerRepository(db)
	feedbackRepo := NewGormAnswerFeedbackRepository(db)
	answer := &domain.AIAnswer{SessionID: "s1", Query: "q", Confidence: 0.9, Strategy: "weknora"}
	if err := repo.Create(platformauth.ContextWithScope(context.Background(), "t-ctx", "w-ctx"), answer); err != nil {
		t.Fatalf("seed answer: %v", err)
	}

	cases := []struct {
		name          string
		inTenant      string
		inWorkspace   string
		ctxTenant     string
		ctxWorkspace  string
		wantTenant    string
		wantWorkspace string
	}{
		{"explicit scope kept", "t-exp", "w-exp", "t-ctx", "w-ctx", "t-exp", "w-exp"},
		{"empty scope filled from ctx", "", "", "t-ctx", "w-ctx", "t-ctx", "w-ctx"},
		{"tenant kept workspace filled", "t-exp", "", "t-ctx", "w-ctx", "t-exp", "w-ctx"},
		{"workspace kept tenant filled", "", "w-exp", "t-ctx", "w-ctx", "t-ctx", "w-exp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			feedback := &domain.AnswerFeedback{AnswerID: answer.ID, Helpful: true, TenantID: tc.inTenant, WorkspaceID: tc.inWorkspace}
			ctx := platformauth.ContextWithScope(context.Background(), tc.ctxTenant, tc.ctxWorkspace)
			if err := feedbackRepo.Create(ctx, feedback); err != nil {
				t.Fatalf("create: %v", err)
			}
			if feedback.ID == 0 || feedback.TenantID != tc.wantTenant || feedback.WorkspaceID != tc.wantWorkspace {
				t.Fatalf("feedback = %+v want tenant=%q workspace=%q", feedback, tc.wantTenant, tc.wantWorkspace)
			}
		})
	}
}

func TestGormAnswerPlainFeedbackCountWindowAndScope(t *testing.T) {
	db := newPlainAnswerDB(t)
	repo := NewGormAnswerRepository(db)
	feedbackRepo := NewGormAnswerFeedbackRepository(db)

	now := time.Now().Truncate(time.Second)
	since := now.Add(-time.Hour)
	answer := &domain.AIAnswer{SessionID: "s1", Query: "q", Confidence: 0.9, Strategy: "weknora"}
	if err := repo.Create(context.Background(), answer); err != nil {
		t.Fatalf("seed answer: %v", err)
	}

	seed := []struct {
		helpful   bool
		tenant    string
		workspace string
		createdAt time.Time
	}{
		{true, "t1", "w1", now},
		{true, "t1", "w1", now},
		{false, "t1", "w1", now},
		// 窗口外不计。
		{true, "t1", "w1", since.Add(-24 * time.Hour)},
		// 其他租户不计入 t1 视角。
		{true, "t2", "w1", now},
	}
	for i, row := range seed {
		feedback := &domain.AnswerFeedback{AnswerID: answer.ID, Helpful: row.helpful, TenantID: row.tenant, WorkspaceID: row.workspace, CreatedAt: row.createdAt}
		if err := feedbackRepo.Create(context.Background(), feedback); err != nil {
			t.Fatalf("seed[%d]: %v", i, err)
		}
	}

	ctxT1 := platformauth.ContextWithScope(context.Background(), "t1", "w1")
	helpful, notHelpful, err := repo.CountFeedback(ctxT1, since)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if helpful != 2 || notHelpful != 1 {
		t.Fatalf("feedback = %d/%d want 2/1（窗口外与他租户不计）", helpful, notHelpful)
	}

	ctxT2 := platformauth.ContextWithScope(context.Background(), "t2", "w1")
	helpfulT2, notHelpfulT2, err := repo.CountFeedback(ctxT2, since)
	if err != nil || helpfulT2 != 1 || notHelpfulT2 != 0 {
		t.Fatalf("t2 feedback = %d/%d err %v want 1/0", helpfulT2, notHelpfulT2, err)
	}

	// 零反馈窗口返回零值（total=0 短路 confSum 的姊妹分支）。
	helpfulEmpty, notHelpfulEmpty, err := repo.CountFeedback(ctxT1, now.Add(time.Hour))
	if err != nil || helpfulEmpty != 0 || notHelpfulEmpty != 0 {
		t.Fatalf("empty window = %d/%d err %v want 0/0", helpfulEmpty, notHelpfulEmpty, err)
	}
}

// TestGormAnswerPlainDBErrors 查询错误走既有约定 drop-table 注入；语句级
// 逐条注错（Count/Scan 中段失败）属 integration 侧 covFailQueue 职责。
func TestGormAnswerPlainDBErrors(t *testing.T) {
	db := newPlainAnswerDB(t)
	repo := NewGormAnswerRepository(db)
	feedbackRepo := NewGormAnswerFeedbackRepository(db)
	ctx := context.Background()

	if err := db.Migrator().DropTable(&domain.AIAnswer{}); err != nil {
		t.Fatalf("drop answer table: %v", err)
	}
	if err := repo.Create(ctx, &domain.AIAnswer{Query: "q"}); err == nil {
		t.Fatal("expected create error after drop")
	}
	if _, err := repo.Get(ctx, 1); err == nil {
		t.Fatal("expected get error after drop")
	}
	if _, _, _, _, err := repo.CountInWindow(ctx, time.Now()); err == nil {
		t.Fatal("expected count error after drop")
	}
	if _, err := repo.TopQuestions(ctx, time.Now(), 10, true); err == nil {
		t.Fatal("expected top questions error after drop")
	}
	if _, err := repo.LowConfidenceQuestions(ctx, time.Now(), 10); err == nil {
		t.Fatal("expected low confidence error after drop")
	}

	if err := db.Migrator().DropTable(&domain.AnswerFeedback{}); err != nil {
		t.Fatalf("drop feedback table: %v", err)
	}
	if err := feedbackRepo.Create(ctx, &domain.AnswerFeedback{Helpful: true}); err == nil {
		t.Fatal("expected feedback create error after drop")
	}
	if _, _, err := repo.CountFeedback(ctx, time.Now()); err == nil {
		t.Fatal("expected feedback count error after drop")
	}
}
