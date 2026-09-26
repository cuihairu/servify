package infra

import (
	"context"
	"errors"
	assistdomain "servify/apps/server/internal/modules/assist/domain"

	"servify/apps/server/internal/models"
	assistapp "servify/apps/server/internal/modules/assist/application"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

// GormRepository 远程协助持久化（pg/sqlite 双轨）。
// 读取面按 ctx 的租户/工作区过滤（RA-1 隔离收口）：scope 为空 = 不过滤，
// 与 macro/api_key/satisfaction 等模块的 applyScopeFilter 同口径；跨 scope
// 命中与不存在同返 not found（404 语义，不回显存在性）。
type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

// applyScopeFilter 按请求 scope 收紧协助会话查询。
func applyScopeFilter(tx *gorm.DB, ctx context.Context) *gorm.DB {
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		tx = tx.Where("tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		tx = tx.Where("workspace_id = ?", workspaceID)
	}
	return tx
}

// scopedSessions 构造带 scope 过滤的 sessions 子查询（供标注面守卫）。
func (r *GormRepository) scopedSessions(ctx context.Context) *gorm.DB {
	sub := r.db.Model(&assistdomain.RemoteAssistSession{}).Select("id")
	return applyScopeFilter(sub, ctx)
}

func (r *GormRepository) CreateSession(ctx context.Context, session *assistdomain.RemoteAssistSession) error {
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *GormRepository) GetSession(ctx context.Context, id uint) (*assistdomain.RemoteAssistSession, error) {
	var session assistdomain.RemoteAssistSession
	if err := applyScopeFilter(r.db.WithContext(ctx), ctx).First(&session, id).Error; err != nil {
		// 不存在与跨 scope 命中同返 not found（404 语义，不回显存在性）。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, assistapp.ErrAssistNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (r *GormRepository) ListSessions(ctx context.Context, conversationSessionID string, limit int) ([]assistdomain.RemoteAssistSession, error) {
	query := applyScopeFilter(r.db.WithContext(ctx).Order("started_at DESC").Limit(limit), ctx)
	if conversationSessionID != "" {
		query = query.Where("conversation_session_id = ?", conversationSessionID)
	}
	var sessions []assistdomain.RemoteAssistSession
	if err := query.Find(&sessions).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

func (r *GormRepository) SaveSession(ctx context.Context, session *assistdomain.RemoteAssistSession) error {
	return r.db.WithContext(ctx).Save(session).Error
}

// GetConversationSessionOwner 返回会话归属的访客用户 ID（不存在返回错误）。
func (r *GormRepository) GetConversationSessionOwner(ctx context.Context, sessionID string) (uint, error) {
	var session models.Session
	if err := r.db.WithContext(ctx).Select("user_id").Where("id = ?", sessionID).First(&session).Error; err != nil {
		return 0, err
	}
	return session.UserID, nil
}

// FindActiveSessionIDByConversation 返回该客服会话 active 协助的 ID（无则 0）。
func (r *GormRepository) FindActiveSessionIDByConversation(ctx context.Context, conversationSessionID string) (uint, error) {
	var id uint
	err := applyScopeFilter(r.db.WithContext(ctx).Model(&assistdomain.RemoteAssistSession{}), ctx).
		Select("id").
		Where("conversation_session_id = ? AND status = ?", conversationSessionID, assistapp.StatusActive).
		Order("id DESC").
		Limit(1).
		Scan(&id).Error
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (r *GormRepository) ListAnnotations(ctx context.Context, assistSessionID uint) ([]assistdomain.RemoteAssistAnnotation, error) {
	var annotations []assistdomain.RemoteAssistAnnotation
	err := r.db.WithContext(ctx).
		Where("assist_session_id = ?", assistSessionID).
		Where("assist_session_id IN (?)", r.scopedSessions(ctx)).
		Order("timestamp_ms ASC, id ASC").
		Find(&annotations).Error
	if err != nil {
		return nil, err
	}
	return annotations, nil
}

func (r *GormRepository) CreateAnnotation(ctx context.Context, annotation *assistdomain.RemoteAssistAnnotation) error {
	return r.db.WithContext(ctx).Create(annotation).Error
}

func (r *GormRepository) DeleteAnnotation(ctx context.Context, id uint) error {
	// scope 守卫经 sessions 子查询：跨 scope 的标注与不存在同返 not found。
	result := r.db.WithContext(ctx).
		Where("id = ?", id).
		Where("assist_session_id IN (?)", r.scopedSessions(ctx)).
		Delete(&assistdomain.RemoteAssistAnnotation{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return assistapp.ErrAssistAnnotationNotFound
	}
	return nil
}

// 编译期确认实现端口（与 quality/infra 同款防御）。
var _ assistapp.Repository = (*GormRepository)(nil)
