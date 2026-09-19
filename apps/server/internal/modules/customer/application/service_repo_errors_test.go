package application

// Repository 错误传播对账（原 services 侧 hscov seam 测试的 customer 段，
// 刀 18 随 realtime 迁移拆回模块）。

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type repoErrCustomerRepo struct {
	Repository
	activityErr error
	statsErr    error
}

func (r *repoErrCustomerRepo) GetCustomerActivity(ctx context.Context, customerID uint, limit int) (*CustomerActivityDTO, error) {
	return nil, r.activityErr
}

func (r *repoErrCustomerRepo) GetStats(ctx context.Context) (*CustomerStatsDTO, error) {
	return nil, r.statsErr
}

func TestService_RepoErrorsPropagate(t *testing.T) {
	repo := &repoErrCustomerRepo{activityErr: errors.New("boom: activity"), statsErr: errors.New("boom: stats")}
	svc := NewService(repo)
	ctx := context.Background()

	if _, err := svc.GetCustomerActivity(ctx, 1, 5); err == nil || !strings.Contains(err.Error(), "activity") {
		t.Fatalf("expected activity module error, got %v", err)
	}
	if _, err := svc.GetStats(ctx); err == nil || !strings.Contains(err.Error(), "stats") {
		t.Fatalf("expected stats module error, got %v", err)
	}
}
