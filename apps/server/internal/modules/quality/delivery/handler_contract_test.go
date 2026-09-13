package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/quality/application"
)

// contractRepo 内联仓储桩：内存实现 application.Repository，验证适配器透传。
type contractRepo struct {
	reviews map[string]*models.QualityReview

	getErr        error
	confirmErr    error
	confirmOK     bool
	rescheduleErr error
	rescheduleOK  bool
}

func newContractRepo() *contractRepo {
	return &contractRepo{reviews: map[string]*models.QualityReview{}}
}

func (m *contractRepo) ListReviewCandidates(_ context.Context, _ time.Time, _ int) ([]models.Session, error) {
	return nil, nil
}

func (m *contractRepo) ListReviewsForRetry(_ context.Context, _ int, _ time.Time, _ int) ([]models.QualityReview, error) {
	return nil, nil
}

func (m *contractRepo) GetReviewBySession(_ context.Context, sessionID string) (*models.QualityReview, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	review, ok := m.reviews[sessionID]
	if !ok {
		return nil, application.ErrNotFound
	}
	cp := *review
	return &cp, nil
}

func (m *contractRepo) ListMessages(_ context.Context, _ string) ([]models.Message, error) {
	return nil, nil
}

func (m *contractRepo) InsertReviewIfAbsent(_ context.Context, review *models.QualityReview) (bool, error) {
	if _, ok := m.reviews[review.SessionID]; ok {
		return false, nil
	}
	m.reviews[review.SessionID] = review
	return true, nil
}

func (m *contractRepo) MarkReviewScored(_ context.Context, sessionID string, _ []string, _ application.ScoredFields) (bool, error) {
	review, ok := m.reviews[sessionID]
	if !ok {
		return false, nil
	}
	review.Status = application.StatusScored
	return true, nil
}

func (m *contractRepo) MarkReviewFailed(_ context.Context, _ string, _ []string, _ int, _ time.Time, _ string) (bool, error) {
	return true, nil
}

func (m *contractRepo) ListReviews(_ context.Context, _ application.ReviewListQuery) ([]models.QualityReview, int64, error) {
	out := make([]models.QualityReview, 0, len(m.reviews))
	for _, r := range m.reviews {
		out = append(out, *r)
	}
	return out, int64(len(out)), nil
}

func (m *contractRepo) ConfirmReview(_ context.Context, sessionID string, cmd application.ConfirmCommand) (bool, error) {
	if m.confirmErr != nil {
		return false, m.confirmErr
	}
	if !m.confirmOK {
		return false, nil
	}
	m.reviews[sessionID].Status = application.StatusConfirmed
	m.reviews[sessionID].ReviewNote = cmd.ReviewNote
	return true, nil
}

func (m *contractRepo) RescheduleReview(_ context.Context, sessionID string, _ bool) (bool, error) {
	if m.rescheduleErr != nil {
		return false, m.rescheduleErr
	}
	if !m.rescheduleOK {
		return false, nil
	}
	m.reviews[sessionID].Status = application.StatusPending
	return true, nil
}

func newContractService(repo *contractRepo) *HandlerServiceAdapter {
	return NewHandlerServiceAdapter(application.NewQualityService(repo, nil, application.ServiceConfig{}, nil))
}

// TestHandlerServiceAdapterDelegates 覆盖 5 个适配方法的成功与错误透传。
func TestHandlerServiceAdapterDelegates(t *testing.T) {
	ctx := context.Background()
	repo := newContractRepo()
	repo.reviews["sess-1"] = &models.QualityReview{SessionID: "sess-1", Status: application.StatusScored}
	adapter := newContractService(repo)

	// ListReviews
	items, total, err := adapter.ListReviews(ctx, ReviewListQuery{Status: application.StatusScored})
	if err != nil || total != 1 || len(items) != 1 || items[0].SessionID != "sess-1" {
		t.Fatalf("ListReviews() = %+v, %d, %v", items, total, err)
	}

	// GetReview
	review, err := adapter.GetReview(ctx, "sess-1")
	if err != nil || review.Status != application.StatusScored {
		t.Fatalf("GetReview() = %+v, %v", review, err)
	}
	if _, err := adapter.GetReview(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetReview(missing) error = %v, want ErrNotFound", err)
	}

	// ConfirmReview：先确认未命中（ErrNotScoreable）再成功
	if err := adapter.ConfirmReview(ctx, "sess-1", ConfirmCommand{ReviewNote: "ok"}); !errors.Is(err, ErrNotScoreable) {
		t.Fatalf("ConfirmReview() unconfirmed error = %v, want ErrNotScoreable", err)
	}
	repo.confirmOK = true
	if err := adapter.ConfirmReview(ctx, "sess-1", ConfirmCommand{ReviewNote: "ok"}); err != nil {
		t.Fatalf("ConfirmReview() error = %v", err)
	}
	repo.confirmErr = errors.New("confirm boom")
	if err := adapter.ConfirmReview(ctx, "sess-1", ConfirmCommand{}); err == nil || err.Error() != "confirm boom" {
		t.Fatalf("want raw confirm error, got %v", err)
	}

	// RescoreReview：confirmed 无 force 拒绝 → force 成功 → 未命中 ErrNotFound → 透传错误
	if err := adapter.RescoreReview(ctx, "sess-1", false); !errors.Is(err, ErrConfirmedNeedsForce) {
		t.Fatalf("RescoreReview() confirmed error = %v, want ErrConfirmedNeedsForce", err)
	}
	repo.rescheduleOK = true
	if err := adapter.RescoreReview(ctx, "sess-1", true); err != nil {
		t.Fatalf("RescoreReview(force) error = %v", err)
	}
	repo.rescheduleOK = false
	if err := adapter.RescoreReview(ctx, "sess-1", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RescoreReview() miss error = %v, want ErrNotFound", err)
	}
	repo.rescheduleErr = errors.New("reschedule boom")
	if err := adapter.RescoreReview(ctx, "sess-1", true); err == nil || err.Error() != "reschedule boom" {
		t.Fatalf("want raw reschedule error, got %v", err)
	}

	// ScorerEnabled：无 provider 时恒为 false
	if adapter.ScorerEnabled() {
		t.Fatal("ScorerEnabled() must be false without a provider")
	}
}
