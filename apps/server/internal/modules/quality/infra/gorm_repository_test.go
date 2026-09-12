package infra

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/quality/application"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var qualityMemDBSeq atomic.Uint32

func uniqueQualityDSN(base string) string {
	return base + "_" + strconv.FormatUint(uint64(qualityMemDBSeq.Add(1)), 10) + "?mode=memory&cache=shared"
}

func newQualityUnitTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := uniqueQualityDSN("file:quality_" + name)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.QualityReview{}, &models.Session{}, &models.Message{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&models.QualityReview{}, &models.Session{}, &models.Message{})
	})
	return db
}

func seedQualitySession(t *testing.T, db *gorm.DB, id string, status string, endedAt time.Time, msgs ...models.Message) *models.Session {
	t.Helper()
	started := endedAt.Add(-10 * time.Minute)
	session := &models.Session{ID: id, Status: status, Platform: "web", StartedAt: started, EndedAt: &endedAt}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	for i := range msgs {
		if msgs[i].SessionID == "" {
			msgs[i].SessionID = id
		}
		if err := db.Create(&msgs[i]).Error; err != nil {
			t.Fatalf("seed message: %v", err)
		}
	}
	return session
}

func TestRepositoryCandidateAndCASLifecycle(t *testing.T) {
	db := newQualityUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	now := time.Now()

	ended := now.Add(-time.Hour)
	seedQualitySession(t, db, "sess-a", "ended", ended,
		models.Message{Sender: "user", Content: "hi", CreatedAt: ended.Add(-time.Minute)},
		models.Message{Sender: "agent", Content: "hello", CreatedAt: ended.Add(-30 * time.Second)},
	)
	seedQualitySession(t, db, "sess-active", "active", now, // 未结束：不是候选
		models.Message{Sender: "user", Content: "x", CreatedAt: now},
	)

	candidates, err := repo.ListReviewCandidates(ctx, now.Add(-24*time.Hour), 10)
	if err != nil {
		t.Fatalf("list candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != "sess-a" {
		t.Fatalf("unexpected candidates: %+v", candidates)
	}

	// 插入 pending；重复插入被唯一键挡下
	review := &models.QualityReview{SessionID: "sess-a", Status: "pending", Trigger: "worker"}
	if inserted, err := repo.InsertReviewIfAbsent(ctx, review); err != nil || !inserted {
		t.Fatalf("first insert: inserted=%v err=%v", inserted, err)
	}
	if inserted, err := repo.InsertReviewIfAbsent(ctx, review); err != nil || inserted {
		t.Fatalf("dup insert must be no-op: inserted=%v err=%v", inserted, err)
	}

	// 打分 CAS：允许 pending → scored
	ok, err := repo.MarkReviewScored(ctx, "sess-a", []string{"pending", "failed"}, application.ScoredFields{
		DimensionsJSON: `{"attitude":{"score":8}}`, TotalScore: floatPtr(8.5), ScoredAt: now,
	})
	if err != nil || !ok {
		t.Fatalf("mark scored: ok=%v err=%v", ok, err)
	}
	got, err := repo.GetReviewBySession(ctx, "sess-a")
	if err != nil {
		t.Fatalf("get review: %v", err)
	}
	if got.Status != "scored" || got.LLMTotalScore == nil || *got.LLMTotalScore != 8.5 {
		t.Fatalf("unexpected scored review: %+v", got)
	}

	// scored 不在 allowedFrom：不得再次改写（confirmed 保护同源）
	ok, _ = repo.MarkReviewFailed(ctx, "sess-a", []string{"pending", "failed"}, 1, now, "boom")
	if ok {
		t.Fatal("scored row must not be re-marked failed")
	}

	// failed 记账 + 复活查询
	if ok, err := repo.MarkReviewFailed(ctx, "sess-a", []string{"scored"}, 1, now.Add(time.Minute), "llm down"); err != nil || !ok {
		t.Fatalf("mark failed from scored: ok=%v err=%v", ok, err)
	}
	retries, err := repo.ListReviewsForRetry(ctx, 3, now.Add(2*time.Minute), 10)
	if err != nil || len(retries) != 1 || retries[0].SessionID != "sess-a" {
		t.Fatalf("retry list: %+v err=%v", retries, err)
	}
	// attempt 达上限后不再复活
	if ok, _ := repo.MarkReviewFailed(ctx, "sess-a", []string{"failed"}, 3, now, "again"); !ok {
		t.Fatal("failed re-mark must apply")
	}
	retries, _ = repo.ListReviewsForRetry(ctx, 3, now.Add(2*time.Minute), 10)
	if len(retries) != 0 {
		t.Fatalf("exhausted review must not revive: %+v", retries)
	}
}

func TestGetReviewBySessionNotFound(t *testing.T) {
	db := newQualityUnitTestDB(t)
	repo := NewGormRepository(db)
	if _, err := repo.GetReviewBySession(context.Background(), "nope"); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func floatPtr(v float64) *float64 { return &v }

// 防止 logrus 导入悬空（service 侧用到，这里保持依赖可见）。
var _ = logrus.PanicLevel
