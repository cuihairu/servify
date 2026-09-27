package audit

import (
	"context"
	"errors"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

type ListQuery struct {
	Action        string
	ResourceType  string
	ResourceID    string
	PrincipalKind string
	ActorUserID   *uint
	Success       *bool
	TenantID      string
	WorkspaceID   string
	From          *time.Time
	To            *time.Time
	Page          int
	PageSize      int
}

type QueryScope struct {
	TenantID    string
	WorkspaceID string
}

type QueryService interface {
	List(ctx context.Context, query ListQuery) ([]models.AuditLog, int64, error)
	Get(ctx context.Context, id uint, scope QueryScope) (*models.AuditLog, error)
}

type GormQueryService struct {
	db *gorm.DB
}

func NewGormQueryService(db *gorm.DB) *GormQueryService {
	if db == nil {
		return nil
	}
	return &GormQueryService{db: db}
}

func (s *GormQueryService) Get(ctx context.Context, id uint, scope QueryScope) (*models.AuditLog, error) {
	if s == nil || s.db == nil || id == 0 {
		return nil, nil
	}

	tx := s.db.WithContext(ctx).Model(&models.AuditLog{}).Where("id = ?", id)
	if scope.TenantID != "" {
		tx = tx.Where("tenant_id = ?", scope.TenantID)
	}
	if scope.WorkspaceID != "" {
		tx = tx.Where("workspace_id = ?", scope.WorkspaceID)
	}

	var log models.AuditLog
	if err := tx.First(&log).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &log, nil
}

func (s *GormQueryService) List(ctx context.Context, query ListQuery) ([]models.AuditLog, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, nil
	}

	page := query.Page
	if page <= 0 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	tx := s.db.WithContext(ctx).Model(&models.AuditLog{})
	if query.Action != "" {
		tx = tx.Where("action = ?", query.Action)
	}
	if query.ResourceType != "" {
		tx = tx.Where("resource_type = ?", query.ResourceType)
	}
	if query.ResourceID != "" {
		tx = tx.Where("resource_id = ?", query.ResourceID)
	}
	if query.PrincipalKind != "" {
		tx = tx.Where("principal_kind = ?", query.PrincipalKind)
	}
	if query.ActorUserID != nil {
		tx = tx.Where("actor_user_id = ?", *query.ActorUserID)
	}
	if query.Success != nil {
		tx = tx.Where("success = ?", *query.Success)
	}
	if query.TenantID != "" {
		tx = tx.Where("tenant_id = ?", query.TenantID)
	}
	if query.WorkspaceID != "" {
		tx = tx.Where("workspace_id = ?", query.WorkspaceID)
	}
	if query.From != nil {
		tx = tx.Where("created_at >= ?", *query.From)
	}
	if query.To != nil {
		tx = tx.Where("created_at <= ?", *query.To)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var logs []models.AuditLog
	if err := tx.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// VerifyChain 链式完整性校验（R2 防篡改）：走全表哈希链，报告断点。
func (s *GormQueryService) VerifyChain(ctx context.Context) (*ChainReport, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("audit chain verify: nil db")
	}
	return VerifyChain(ctx, s.db)
}
