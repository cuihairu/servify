//go:build integration
// +build integration

package infra

// V1.0 收敛 B3-1b 补齐：ai_answers / answer_feedback 仓储的空入参防御与
// 语句级错误分支。错误注入走 GORM 回调队列：Count 走 Query 回调链、Scan
// （经 Rows）走 Row 回调链、Create 走 Create 回调链，按队列逐语句放行/
// 注错，覆盖 CountInWindow/CountFeedback 各段计数与各 Scan 的失败路径。

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"servify/apps/server/internal/modules/ai/domain"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// covFailQueue 语句级错误注入队列：每执行一条语句消费一个元素，nil=放行。
// 测试串行执行，队列按消费序排布即可精确命中目标语句。
type covFailQueue struct {
	mu   sync.Mutex
	plan []error
}

func (q *covFailQueue) arm(plan ...error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.plan = plan
}

func (q *covFailQueue) drained() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.plan) == 0
}

func (q *covFailQueue) apply(tx *gorm.DB) {
	q.mu.Lock()
	if len(q.plan) == 0 {
		q.mu.Unlock()
		return
	}
	err := q.plan[0]
	q.plan = q.plan[1:]
	q.mu.Unlock()
	if err != nil {
		tx.AddError(err)
	}
}

func newAIInfraCovDB(t *testing.T) (*gorm.DB, *covFailQueue) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.AIAnswer{}, &domain.AnswerFeedback{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	q := &covFailQueue{}
	if err := db.Callback().Query().Before("gorm:query").Register("cov_extra_query_guard", q.apply); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	if err := db.Callback().Row().Before("gorm:row").Register("cov_extra_row_guard", q.apply); err != nil {
		t.Fatalf("register row callback: %v", err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("cov_extra_create_guard", q.apply); err != nil {
		t.Fatalf("register create callback: %v", err)
	}
	return db, q
}

func requireCovDrained(t *testing.T, q *covFailQueue) {
	t.Helper()
	if !q.drained() {
		t.Fatal("error queue not fully consumed")
	}
}

func TestGormAnswerRepositoryNilAndCreateErrors(t *testing.T) {
	db, q := newAIInfraCovDB(t)
	repo := NewGormAnswerRepository(db)
	feedbackRepo := NewGormAnswerFeedbackRepository(db)
	ctx := context.Background()
	boom := errors.New("db down")

	// 空入参防御。
	if err := repo.Create(ctx, nil); !errors.Is(err, gorm.ErrInvalidValue) {
		t.Fatalf("nil answer create err = %v", err)
	}
	if err := feedbackRepo.Create(ctx, nil); !errors.Is(err, gorm.ErrInvalidValue) {
		t.Fatalf("nil feedback create err = %v", err)
	}

	// 落库失败（答案/反馈各一次）。
	q.arm(boom)
	if err := repo.Create(ctx, &domain.AIAnswer{Query: "q", Answer: "a"}); !errors.Is(err, boom) {
		t.Fatalf("answer create err = %v", err)
	}
	requireCovDrained(t, q)

	q.arm(boom)
	if err := feedbackRepo.Create(ctx, &domain.AnswerFeedback{AnswerID: 1, Helpful: true}); !errors.Is(err, boom) {
		t.Fatalf("feedback create err = %v", err)
	}
	requireCovDrained(t, q)

	// 注入点不破坏正常路径。
	answer := &domain.AIAnswer{Query: "退款政策是什么", Answer: "7 天无理由", Confidence: 0.9, CreatedAt: time.Now()}
	if err := repo.Create(ctx, answer); err != nil || answer.ID == 0 {
		t.Fatalf("create after injections = %+v err %v", answer, err)
	}
}

func TestGormAnswerAnalyticsErrorBranches(t *testing.T) {
	db, q := newAIInfraCovDB(t)
	repo := NewGormAnswerRepository(db)
	ctx := context.Background()
	boom := errors.New("analytics down")
	now := time.Now()

	seed := []domain.AIAnswer{
		{Query: "退款政策是什么", Confidence: 0.9, SourcesJSON: `[{"document_id":"d1"}]`, CreatedAt: now},
		{Query: "怎么开发票", Confidence: 0.2, CreatedAt: now},
	}
	for i := range seed {
		if err := repo.Create(ctx, &seed[i]); err != nil {
			t.Fatalf("seed[%d]: %v", i, err)
		}
	}
	since := now.AddDate(0, 0, -1)

	// CountInWindow 四段（总数/命中/低置信/置信合计）各自的失败分支。
	q.arm(boom)
	if _, _, _, _, err := repo.CountInWindow(ctx, since); !errors.Is(err, boom) {
		t.Fatalf("total count err = %v", err)
	}
	requireCovDrained(t, q)

	q.arm(nil, boom)
	if _, _, _, _, err := repo.CountInWindow(ctx, since); !errors.Is(err, boom) {
		t.Fatalf("hit count err = %v", err)
	}
	requireCovDrained(t, q)

	q.arm(nil, nil, boom)
	if _, _, _, _, err := repo.CountInWindow(ctx, since); !errors.Is(err, boom) {
		t.Fatalf("lowconf count err = %v", err)
	}
	requireCovDrained(t, q)

	q.arm(nil, nil, nil, boom)
	if _, _, _, _, err := repo.CountInWindow(ctx, since); !errors.Is(err, boom) {
		t.Fatalf("confsum scan err = %v", err)
	}
	requireCovDrained(t, q)

	// Scan 类读口（Row 回调链）。
	q.arm(boom)
	if _, err := repo.TopQuestions(ctx, since, 10, true); !errors.Is(err, boom) {
		t.Fatalf("top questions err = %v", err)
	}
	requireCovDrained(t, q)

	q.arm(boom)
	if _, err := repo.LowConfidenceQuestions(ctx, since, 10); !errors.Is(err, boom) {
		t.Fatalf("lowconf questions err = %v", err)
	}
	requireCovDrained(t, q)

	// CountFeedback 两段计数。
	q.arm(boom)
	if _, _, err := repo.CountFeedback(ctx, since); !errors.Is(err, boom) {
		t.Fatalf("helpful count err = %v", err)
	}
	requireCovDrained(t, q)

	q.arm(nil, boom)
	if _, _, err := repo.CountFeedback(ctx, since); !errors.Is(err, boom) {
		t.Fatalf("not helpful count err = %v", err)
	}
	requireCovDrained(t, q)
}
