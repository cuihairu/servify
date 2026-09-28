package audit

import (
	"context"
	"fmt"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// RetentionService deletes expired audit logs according to a configured policy.
type RetentionService interface {
	Cleanup(ctx context.Context, now time.Time) (int64, error)
}

type GormRetentionService struct {
	db        *gorm.DB
	retention time.Duration
	batchSize int
	logger    *logrus.Logger
	// archiver 非空时（冷热分层）：每批删除前先整批归档，归档失败即中断
	// 本轮清理且不删除（见 archive.go）。
	archiver ArchiveWriter
}

func NewGormRetentionService(db *gorm.DB, retention time.Duration, batchSize int) *GormRetentionService {
	if db == nil || retention <= 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	return &GormRetentionService{
		db:        db,
		retention: retention,
		batchSize: batchSize,
		logger:    logrus.StandardLogger(),
	}
}

// WithArchive 挂载冷层归档器（链式返回自身，便于装配处内联使用）。
func (s *GormRetentionService) WithArchive(w ArchiveWriter) *GormRetentionService {
	if s != nil {
		s.archiver = w
	}
	return s
}

func (s *GormRetentionService) Cleanup(ctx context.Context, now time.Time) (int64, error) {
	if s == nil || s.db == nil || s.retention <= 0 {
		return 0, nil
	}

	cutoff := now.Add(-s.retention)
	var deleted int64

	for {
		var rows []models.AuditLog
		if err := s.db.WithContext(ctx).
			Model(&models.AuditLog{}).
			Where("created_at < ?", cutoff).
			Order("created_at ASC").
			Limit(s.batchSize).
			Find(&rows).Error; err != nil {
			return deleted, err
		}
		if len(rows) == 0 {
			return deleted, nil
		}
		ids := make([]uint, len(rows))
		for i := range rows {
			ids[i] = rows[i].ID
		}

		// 冷热分层：先归档后删除，归档失败不产生任何删除。
		if s.archiver != nil {
			if err := s.archiver.Write(ctx, rows); err != nil {
				return deleted, fmt.Errorf("archive expired audit logs before delete: %w", err)
			}
		}

		res := s.db.WithContext(ctx).Delete(&models.AuditLog{}, ids)
		if res.Error != nil {
			return deleted, res.Error
		}
		deleted += res.RowsAffected
		if len(ids) < s.batchSize {
			return deleted, nil
		}
		if err := ctx.Err(); err != nil {
			return deleted, err
		}
	}
}
