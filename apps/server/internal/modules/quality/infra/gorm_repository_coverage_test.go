package infra

import (
	"context"
	qualitydomain "servify/apps/server/internal/modules/quality/domain"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/quality/application"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedScoredReview(t *testing.T, db *gorm.DB, sessionID string) *qualitydomain.QualityReview {
	t.Helper()
	review := &qualitydomain.QualityReview{SessionID: sessionID, Status: application.StatusScored, Trigger: "worker"}
	require.NoError(t, db.Create(review).Error)
	return review
}

// TestRepositoryListMessages 覆盖 ListMessages 的命中与空结果。
func TestRepositoryListMessages(t *testing.T) {
	db := newQualityUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	seedQualitySession(t, db, "sess-msg", "ended", time.Now().Add(-time.Hour),
		models.Message{Sender: "user", Content: "second", CreatedAt: time.Now().Add(-time.Minute)},
		models.Message{Sender: "agent", Content: "first", CreatedAt: time.Now().Add(-2 * time.Minute)},
	)

	msgs, err := repo.ListMessages(ctx, "sess-msg")
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, "first", msgs[0].Content, "messages must be ordered by created_at ASC")

	msgs, err = repo.ListMessages(ctx, "sess-empty")
	require.NoError(t, err)
	require.Empty(t, msgs)
}

// TestRepositoryListReviewsFiltersAndPaging 覆盖全部筛选分支与分页默认值。
func TestRepositoryListReviewsFiltersAndPaging(t *testing.T) {
	db := newQualityUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	score := 8.5
	agentID := uint(7)
	customerID := uint(9)
	hasViolations := true
	noViolations := false
	from := time.Now().Add(-24 * time.Hour)

	rows := []*qualitydomain.QualityReview{
		{SessionID: "r-1", Status: application.StatusScored, AgentID: &agentID, CustomerID: &customerID,
			ViolationCount: 2, MaxSeverity: application.SeverityHigh, LLMTotalScore: &score, Trigger: "worker"},
		{SessionID: "r-2", Status: application.StatusSkipped, Trigger: "worker"},
		{SessionID: "r-3", Status: application.StatusPending, Trigger: "worker"},
	}
	for _, r := range rows {
		require.NoError(t, db.Create(r).Error)
	}

	// 逐个过滤器都应至少命中 r-1 或按语义过滤
	cases := []struct {
		name  string
		query application.ReviewListQuery
		want  int64
	}{
		{name: "status scored", query: application.ReviewListQuery{Status: application.StatusScored}, want: 1},
		{name: "agent filter", query: application.ReviewListQuery{AgentID: &agentID}, want: 1},
		{name: "customer filter", query: application.ReviewListQuery{CustomerID: &customerID}, want: 1},
		{name: "has violations", query: application.ReviewListQuery{HasViolations: &hasViolations}, want: 1},
		{name: "no violations", query: application.ReviewListQuery{HasViolations: &noViolations}, want: 2},
		{name: "severity", query: application.ReviewListQuery{Severity: application.SeverityHigh}, want: 1},
		{name: "min score", query: application.ReviewListQuery{MinScore: &score}, want: 1},
		{name: "max score", query: application.ReviewListQuery{MaxScore: &score}, want: 1},
		{name: "from bound", query: application.ReviewListQuery{From: &from}, want: 3},
		{name: "to bound excludes future", query: application.ReviewListQuery{To: &from}, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, total, err := repo.ListReviews(ctx, tc.query)
			require.NoError(t, err)
			require.Equal(t, tc.want, total)
			require.Len(t, items, int(tc.want))
		})
	}

	// 非法分页归一化 + 排序
	items, total, err := repo.ListReviews(ctx, application.ReviewListQuery{Page: -1, PageSize: 500})
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, items, 3)
	require.Equal(t, "r-3", items[0].SessionID, "default order is created_at DESC")

	// 第二页只有 1 条
	items, total, err = repo.ListReviews(ctx, application.ReviewListQuery{Page: 2, PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, items, 1)
}

// TestRepositoryListReviewsCountError 用删表触发 Count 错误分支。
func TestRepositoryListReviewsCountError(t *testing.T) {
	db := newQualityUnitTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&qualitydomain.QualityReview{}))
	_, _, err := NewGormRepository(db).ListReviews(context.Background(), application.ReviewListQuery{})
	require.Error(t, err)
}

// TestRepositoryErrorBranches 覆盖 GetReviewBySession / InsertReviewIfAbsent 的非 NotFound 错误分支。
func TestRepositoryErrorBranches(t *testing.T) {
	db := newQualityUnitTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&qualitydomain.QualityReview{}))
	repo := NewGormRepository(db)
	ctx := context.Background()

	_, err := repo.GetReviewBySession(ctx, "sess-a")
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "no rows"), "dropped table must surface a real error, not NotFound")

	_, err = repo.InsertReviewIfAbsent(ctx, &qualitydomain.QualityReview{SessionID: "sess-a"})
	require.Error(t, err)
}

// TestRepositoryConfirmReview 覆盖 CAS 确认的命中、未命中、manual_score 可选与错误分支。
func TestRepositoryConfirmReview(t *testing.T) {
	db := newQualityUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	seedScoredReview(t, db, "sess-confirm")
	require.NoError(t, db.Create(&qualitydomain.QualityReview{SessionID: "sess-pending", Status: application.StatusPending, Trigger: "worker"}).Error)

	// 未命中：pending 不可确认
	manual := 7.5
	ok, err := repo.ConfirmReview(ctx, "sess-pending", application.ConfirmCommand{ManualScore: &manual})
	require.NoError(t, err)
	require.False(t, ok)

	// 命中：带 manual_score
	ok, err = repo.ConfirmReview(ctx, "sess-confirm", application.ConfirmCommand{
		ManualScore: &manual, ManualResult: "pass", ReviewNote: "抽检通过", ReviewedBy: 42,
	})
	require.NoError(t, err)
	require.True(t, ok)

	got, err := repo.GetReviewBySession(ctx, "sess-confirm")
	require.NoError(t, err)
	require.Equal(t, application.StatusConfirmed, got.Status)
	require.NotNil(t, got.ManualScore)
	require.Equal(t, 7.5, *got.ManualScore)
	require.Equal(t, "pass", got.ManualResult)
	require.Equal(t, "抽检通过", got.ReviewNote)
	require.NotNil(t, got.ReviewedBy)
	require.Equal(t, uint(42), *got.ReviewedBy)
	require.NotNil(t, got.ReviewedAt)
	require.Nil(t, got.NextRetryAt)

	// 不带 manual_score 的确认（清空覆盖语义）
	seedScoredReview(t, db, "sess-confirm-2")
	ok, err = repo.ConfirmReview(ctx, "sess-confirm-2", application.ConfirmCommand{ReviewNote: "second"})
	require.NoError(t, err)
	require.True(t, ok)

	// 错误分支：删表
	require.NoError(t, db.Migrator().DropTable(&qualitydomain.QualityReview{}))
	_, err = repo.ConfirmReview(ctx, "sess-confirm", application.ConfirmCommand{})
	require.Error(t, err)
}

// TestRepositoryRescheduleReview 覆盖 force / 非 force / 未命中 / 错误分支。
func TestRepositoryRescheduleReview(t *testing.T) {
	db := newQualityUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	seedScoredReview(t, db, "sess-reschedule")
	require.NoError(t, db.Create(&qualitydomain.QualityReview{SessionID: "sess-confirmed", Status: application.StatusConfirmed, Trigger: "worker"}).Error)

	// 非 force 不触碰 confirmed
	ok, err := repo.RescheduleReview(ctx, "sess-confirmed", false)
	require.NoError(t, err)
	require.False(t, ok)

	// 非 force 重置 scored
	ok, err = repo.RescheduleReview(ctx, "sess-reschedule", false)
	require.NoError(t, err)
	require.True(t, ok)
	got, err := repo.GetReviewBySession(ctx, "sess-reschedule")
	require.NoError(t, err)
	require.Equal(t, application.StatusPending, got.Status)
	require.Equal(t, "rescore", got.Trigger)
	require.Nil(t, got.NextRetryAt)
	require.Empty(t, got.LastError)

	// force 重置 confirmed
	ok, err = repo.RescheduleReview(ctx, "sess-confirmed", true)
	require.NoError(t, err)
	require.True(t, ok)

	// 未命中
	ok, err = repo.RescheduleReview(ctx, "sess-missing", true)
	require.NoError(t, err)
	require.False(t, ok)

	// 错误分支：删表
	require.NoError(t, db.Migrator().DropTable(&qualitydomain.QualityReview{}))
	_, err = repo.RescheduleReview(ctx, "sess-reschedule", true)
	require.Error(t, err)
}
