package infra

import (
	"context"
	"errors"
	assistdomain "servify/apps/server/internal/modules/assist/domain"

	"servify/apps/server/internal/models"
	assistapp "servify/apps/server/internal/modules/assist/application"

	"gorm.io/gorm"
)

// GormRepository 远程协助持久化（pg/sqlite 双轨）。
type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) CreateSession(ctx context.Context, session *assistdomain.RemoteAssistSession) error {
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *GormRepository) GetSession(ctx context.Context, id uint) (*assistdomain.RemoteAssistSession, error) {
	var session assistdomain.RemoteAssistSession
	if err := r.db.WithContext(ctx).First(&session, id).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *GormRepository) ListSessions(ctx context.Context, conversationSessionID string, limit int) ([]assistdomain.RemoteAssistSession, error) {
	query := r.db.WithContext(ctx).Order("started_at DESC").Limit(limit)
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

func (r *GormRepository) ListAnnotations(ctx context.Context, assistSessionID uint) ([]assistdomain.RemoteAssistAnnotation, error) {
	var annotations []assistdomain.RemoteAssistAnnotation
	err := r.db.WithContext(ctx).
		Where("assist_session_id = ?", assistSessionID).
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

func (r *GormRepository) GetAnnotation(ctx context.Context, id uint) (*assistdomain.RemoteAssistAnnotation, error) {
	var annotation assistdomain.RemoteAssistAnnotation
	if err := r.db.WithContext(ctx).First(&annotation, id).Error; err != nil {
		return nil, err
	}
	return &annotation, nil
}

func (r *GormRepository) DeleteAnnotation(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Delete(&assistdomain.RemoteAssistAnnotation{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("annotation not found")
	}
	return nil
}

// 编译期确认实现端口（与 quality/infra 同款防御）。
var _ assistapp.Repository = (*GormRepository)(nil)
