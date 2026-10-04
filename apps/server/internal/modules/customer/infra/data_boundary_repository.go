package infra

import (
	"context"
	"errors"
	"fmt"
	"time"

	"servify/apps/server/internal/models"
	customerapp "servify/apps/server/internal/modules/customer/application"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

// GormDataBoundaryRepository 数据边界读写口（B2-2）：跨表直查/直写，
// 与 customer 主仓储同库同 scope 口径。
type GormDataBoundaryRepository struct {
	db *gorm.DB
}

// NewGormDataBoundaryRepository 构造。
func NewGormDataBoundaryRepository(db *gorm.DB) *GormDataBoundaryRepository {
	return &GormDataBoundaryRepository{db: db}
}

func (r *GormDataBoundaryRepository) LoadCustomerWithUser(ctx context.Context, customerID uint) (*models.Customer, *models.User, error) {
	var customer models.Customer
	if err := r.db.WithContext(ctx).First(&customer, customerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, customerapp.ErrCustomerNotFound
		}
		return nil, nil, fmt.Errorf("load customer: %w", err)
	}
	var user models.User
	if err := r.db.WithContext(ctx).First(&user, customer.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, customerapp.ErrCustomerNotFound
		}
		return nil, nil, fmt.Errorf("load customer user: %w", err)
	}
	return &customer, &user, nil
}

func (r *GormDataBoundaryRepository) ListSessionsByUser(ctx context.Context, userID uint) ([]models.Session, error) {
	var items []models.Session
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("started_at ASC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return items, nil
}

func (r *GormDataBoundaryRepository) ListMessagesBySessions(ctx context.Context, sessionIDs []string) ([]models.Message, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	var items []models.Message
	if err := r.db.WithContext(ctx).Where("session_id IN ?", sessionIDs).Order("created_at ASC, id ASC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return items, nil
}

func (r *GormDataBoundaryRepository) ListTicketsByUser(ctx context.Context, userID uint) ([]models.Ticket, error) {
	var items []models.Ticket
	// Unscoped：导出与擦除都要覆盖已删工单（合规口径是数据主体全量）。
	if err := r.db.WithContext(ctx).Unscoped().Where("customer_id = ?", userID).Order("created_at ASC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list tickets: %w", err)
	}
	return items, nil
}

func (r *GormDataBoundaryRepository) ListCommentsByTickets(ctx context.Context, ticketIDs []uint) ([]models.TicketComment, error) {
	if len(ticketIDs) == 0 {
		return nil, nil
	}
	var items []models.TicketComment
	if err := r.db.WithContext(ctx).Unscoped().Where("ticket_id IN ?", ticketIDs).Order("created_at ASC, id ASC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list ticket comments: %w", err)
	}
	return items, nil
}

func (r *GormDataBoundaryRepository) ListFilesByTickets(ctx context.Context, ticketIDs []uint) ([]models.TicketFile, error) {
	if len(ticketIDs) == 0 {
		return nil, nil
	}
	var items []models.TicketFile
	if err := r.db.WithContext(ctx).Unscoped().Where("ticket_id IN ?", ticketIDs).Order("created_at ASC, id ASC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list ticket files: %w", err)
	}
	return items, nil
}

// applyBoundaryScope 数据边界租户过滤（跨表更新用，不带表名前缀）。
func applyBoundaryScope(db *gorm.DB, ctx context.Context) *gorm.DB {
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		db = db.Where("tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	return db
}

// EraseCustomerAndUser 匿名化：档案字段清空 + 身份替换为不可回溯标记
// （email 唯一约束用 erased-<customerID> 占位；username 唯一约束同理）。
func (r *GormDataBoundaryRepository) EraseCustomerAndUser(ctx context.Context, customerID, userID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		customerUpdates := map[string]interface{}{
			"company":  "",
			"industry": "",
			"tags":     "",
			"notes":    customerapp.ErasureErasedText,
		}
		if err := applyBoundaryScope(tx.Model(&models.Customer{}).Where("id = ?", customerID), ctx).
			Updates(customerUpdates).Error; err != nil {
			return fmt.Errorf("erase customer: %w", err)
		}
		erasedMark := fmt.Sprintf("erased-c%d", customerID)
		userUpdates := map[string]interface{}{
			"username": erasedMark,
			"email":    erasedMark + "@erased.local",
			"name":     customerapp.ErasureErasedText,
			"phone":    "",
			"avatar":   "",
		}
		if err := tx.Model(&models.User{}).Where("id = ?", userID).Updates(userUpdates).Error; err != nil {
			return fmt.Errorf("erase user: %w", err)
		}
		return nil
	})
}

func (r *GormDataBoundaryRepository) ScrubMessagesBySessions(ctx context.Context, sessionIDs []string) (int64, error) {
	result := r.db.WithContext(ctx).Model(&models.Message{}).
		Where("session_id IN ?", sessionIDs).
		Update("content", customerapp.ErasureErasedText)
	return result.RowsAffected, result.Error
}

func (r *GormDataBoundaryRepository) ScrubTicketsByUser(ctx context.Context, userID uint) (int64, error) {
	result := r.db.WithContext(ctx).Unscoped().Model(&models.Ticket{}).
		Where("customer_id = ?", userID).
		Updates(map[string]interface{}{
			"title":       customerapp.ErasureErasedText,
			"description": customerapp.ErasureErasedText,
			"ai_summary":  "",
			"tags":        "",
		})
	return result.RowsAffected, result.Error
}

func (r *GormDataBoundaryRepository) ScrubCommentsByTickets(ctx context.Context, ticketIDs []uint) (int64, error) {
	result := r.db.WithContext(ctx).Unscoped().Model(&models.TicketComment{}).
		Where("ticket_id IN ?", ticketIDs).
		Update("content", customerapp.ErasureErasedText)
	return result.RowsAffected, result.Error
}

func (r *GormDataBoundaryRepository) DeleteFilesByTickets(ctx context.Context, ticketIDs []uint) (int64, error) {
	result := r.db.WithContext(ctx).Unscoped().Where("ticket_id IN ?", ticketIDs).Delete(&models.TicketFile{})
	return result.RowsAffected, result.Error
}

// —— 数据保留清理（B2-2b）：按时间窗口批量擦除，口径与数据主体删除一致 ——

// endedSessionIDsSub 已结束（ended_at 早于 before）会话 ID 子查询。
func (r *GormDataBoundaryRepository) endedSessionIDsSub(before time.Time) *gorm.DB {
	return r.db.Model(&models.Session{}).Select("id").
		Where("ended_at IS NOT NULL AND ended_at < ?", before)
}

// closedTicketIDsSub 已关闭（closed_at 早于 before）工单 ID 子查询；
// Unscoped 让保留清理覆盖软删工单（挂其下的评论/附件同样要擦）。
func (r *GormDataBoundaryRepository) closedTicketIDsSub(before time.Time) *gorm.DB {
	return r.db.Unscoped().Model(&models.Ticket{}).Select("id").
		Where("closed_at IS NOT NULL AND closed_at < ?", before)
}

func (r *GormDataBoundaryRepository) ScrubMessagesInSessionsEndedBefore(ctx context.Context, before time.Time, replacement string) (int64, error) {
	result := r.db.WithContext(ctx).Model(&models.Message{}).
		Where("session_id IN (?)", r.endedSessionIDsSub(before)).
		// 只擦有内容的行：空串与已擦除标记不重复改写（幂等计数诚实）。
		Where("content <> ? AND content <> ?", replacement, "").
		Update("content", replacement)
	return result.RowsAffected, result.Error
}

func (r *GormDataBoundaryRepository) ScrubTicketsClosedBefore(ctx context.Context, before time.Time, replacement string) (int64, error) {
	result := r.db.WithContext(ctx).Unscoped().Model(&models.Ticket{}).
		Where("id IN (?)", r.closedTicketIDsSub(before)).
		Where("title <> ? OR description <> ?", replacement, replacement).
		Updates(map[string]interface{}{
			"title":       replacement,
			"description": replacement,
			"ai_summary":  "",
			"tags":        "",
		})
	return result.RowsAffected, result.Error
}

func (r *GormDataBoundaryRepository) ScrubCommentsOnTicketsClosedBefore(ctx context.Context, before time.Time, replacement string) (int64, error) {
	result := r.db.WithContext(ctx).Unscoped().Model(&models.TicketComment{}).
		Where("ticket_id IN (?)", r.closedTicketIDsSub(before)).
		Where("content <> ? AND content <> ?", replacement, "").
		Update("content", replacement)
	return result.RowsAffected, result.Error
}

func (r *GormDataBoundaryRepository) DeleteFilesOnTicketsClosedBefore(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Unscoped().
		Where("ticket_id IN (?)", r.closedTicketIDsSub(before)).
		Delete(&models.TicketFile{})
	return result.RowsAffected, result.Error
}
