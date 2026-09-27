package audit

import (
	"context"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

type Entry struct {
	ActorUserID   *uint
	PrincipalKind string
	Action        string
	ResourceType  string
	ResourceID    string
	Route         string
	Method        string
	StatusCode    int
	Success       bool
	RequestID     string
	ClientIP      string
	UserAgent     string
	TenantID      string
	WorkspaceID   string
	RequestJSON   string
	BeforeJSON    string
	AfterJSON     string
}

type Recorder interface {
	Record(ctx context.Context, entry Entry) error
}

type GormRecorder struct {
	db *gorm.DB
}

func NewGormRecorder(db *gorm.DB) *GormRecorder {
	if db == nil {
		return nil
	}
	return &GormRecorder{db: db}
}

func (r *GormRecorder) Record(ctx context.Context, entry Entry) error {
	if r == nil || r.db == nil {
		return nil
	}
	// 链式防篡改（R2）：事务内"读链尾→算哈希→落库"，保证 prev_hash 与
	// 落库时的链尾一致（postgres 行锁串行化，sqlite 单写者）。
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		prev, err := chainTail(tx)
		if err != nil {
			return err
		}
		record := models.AuditLog{
			ActorUserID:   entry.ActorUserID,
			PrincipalKind: entry.PrincipalKind,
			Action:        entry.Action,
			ResourceType:  entry.ResourceType,
			ResourceID:    entry.ResourceID,
			Route:         entry.Route,
			Method:        entry.Method,
			StatusCode:    entry.StatusCode,
			Success:       entry.Success,
			RequestID:     entry.RequestID,
			ClientIP:      entry.ClientIP,
			UserAgent:     entry.UserAgent,
			TenantID:      entry.TenantID,
			WorkspaceID:   entry.WorkspaceID,
			RequestJSON:   entry.RequestJSON,
			BeforeJSON:    entry.BeforeJSON,
			AfterJSON:     entry.AfterJSON,
			// 应用侧定值（截断到微秒）参与哈希，读回重算必须逐字节一致。
			CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
			PrevHash:  prev,
		}
		record.EntryHash = ComputeEntryHash(prev, &record)
		return tx.Create(&record).Error
	})
}
