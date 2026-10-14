package infra

// auth GORM 仓储：auth 应用层 Repository 端口的落地实现。
// SQL 与原 service.go/totp.go/oidc.go 内联查询逐字一致（含 COALESCE
// token_version 自增、LOWER(email) 查询、恢复码 used_at IS NULL 原子核销、
// 整批替换的事务包裹），错误按端口约定返回。

import (
	"context"
	"errors"
	"time"

	"servify/apps/server/internal/models"
	slapp "servify/apps/server/internal/modules/auth/application"

	"gorm.io/gorm"
)

type GormRepository struct {
	db *gorm.DB
}

var _ slapp.Repository = (*GormRepository)(nil)

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

// mapUserNotFound 把 gorm.ErrRecordNotFound 映射为端口哨兵，其余错误原样透传。
func mapUserNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return slapp.ErrAuthUserNotFound
	}
	return err
}

func (r *GormRepository) CountUsersByUsernameOrEmail(ctx context.Context, username, email string) int64 {
	var count int64
	_ = r.db.WithContext(ctx).Model(&models.User{}).
		Where("username = ? OR email = ?", username, email).Count(&count).Error
	return count
}

func (r *GormRepository) CountUsers(ctx context.Context) int64 {
	var total int64
	_ = r.db.WithContext(ctx).Model(&models.User{}).Count(&total).Error
	return total
}

func (r *GormRepository) CreateUser(ctx context.Context, user *models.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *GormRepository) GetUserByUsernameOrEmail(ctx context.Context, identifier string) (*models.User, error) {
	var user models.User
	if err := r.db.WithContext(ctx).Where("username = ? OR email = ?", identifier, identifier).First(&user).Error; err != nil {
		return nil, mapUserNotFound(err)
	}
	return &user, nil
}

func (r *GormRepository) GetUserByID(ctx context.Context, id uint) (*models.User, error) {
	var user models.User
	if err := r.db.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, mapUserNotFound(err)
	}
	return &user, nil
}

func (r *GormRepository) GetUserByEmailLower(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	if err := r.db.WithContext(ctx).Where("LOWER(email) = ?", email).First(&user).Error; err != nil {
		return nil, mapUserNotFound(err)
	}
	return &user, nil
}

func (r *GormRepository) UpdateUserLastLogin(ctx context.Context, user *models.User, now time.Time) error {
	return r.db.WithContext(ctx).Model(user).Update("last_login", now).Error
}

func (r *GormRepository) EnableUserTwoFactor(ctx context.Context, userID uint, secret string, now time.Time) error {
	return r.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"totp_secret":       secret,
		"totp_enabled":      true,
		"totp_enabled_at":   now,
		"token_valid_after": now,
	}).Error
}

func (r *GormRepository) DisableUserTwoFactor(ctx context.Context, userID uint, tokenValidAfter time.Time) error {
	return r.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"totp_secret":       "",
		"totp_enabled":      false,
		"totp_enabled_at":   nil,
		"token_valid_after": tokenValidAfter,
	}).Error
}

func (r *GormRepository) CreateAuthSession(ctx context.Context, session *models.UserAuthSession) error {
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *GormRepository) GetAuthSession(ctx context.Context, sessionID string, userID uint) (*models.UserAuthSession, error) {
	var session models.UserAuthSession
	if err := r.db.WithContext(ctx).First(&session, "id = ? AND user_id = ?", sessionID, userID).Error; err != nil {
		return nil, mapUserNotFound(err)
	}
	return &session, nil
}

func (r *GormRepository) ListAuthSessionsByUser(ctx context.Context, userID uint) ([]models.UserAuthSession, error) {
	var sessions []models.UserAuthSession
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("updated_at desc, created_at desc").
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

func (r *GormRepository) RevokeAuthSession(ctx context.Context, sessionID string, userID uint) (int64, error) {
	result := r.db.WithContext(ctx).Model(&models.UserAuthSession{}).
		Where("id = ? AND user_id = ?", sessionID, userID).
		Updates(map[string]any{
			"status":        "revoked",
			"revoked_at":    time.Now().UTC(),
			"token_version": gorm.Expr("COALESCE(token_version, 0) + 1"),
		})
	return result.RowsAffected, result.Error
}

func (r *GormRepository) RevokeOtherAuthSessions(ctx context.Context, userID uint, excludeSessionID string) (int64, error) {
	result := r.db.WithContext(ctx).Model(&models.UserAuthSession{}).
		Where("user_id = ? AND id <> ? AND status = ? AND revoked_at IS NULL", userID, excludeSessionID, "active").
		Updates(map[string]any{
			"status":        "revoked",
			"revoked_at":    time.Now().UTC(),
			"token_version": gorm.Expr("COALESCE(token_version, 0) + 1"),
		})
	return result.RowsAffected, result.Error
}

func (r *GormRepository) RevokeActiveAuthSession(ctx context.Context, sessionID string, userID uint) error {
	return r.db.WithContext(ctx).Model(&models.UserAuthSession{}).
		Where("id = ? AND user_id = ? AND status = ?", sessionID, userID, "active").
		Updates(map[string]any{
			"status":        "revoked",
			"revoked_at":    time.Now().UTC(),
			"token_version": gorm.Expr("COALESCE(token_version, 0) + 1"),
		}).Error
}

func (r *GormRepository) RotateAuthSession(ctx context.Context, sessionID string, userID uint, fields slapp.AuthSessionRotation) error {
	updates := map[string]interface{}{
		"status":             "active",
		"token_version":      gorm.Expr("COALESCE(token_version, 0) + 1"),
		"device_fingerprint": fields.DeviceFingerprint,
		"user_agent":         fields.UserAgent,
		"client_ip":          fields.ClientIP,
		"last_seen_at":       fields.Now,
		"last_refreshed_at":  fields.Now,
		"revoked_at":         nil,
	}
	return r.db.WithContext(ctx).Model(&models.UserAuthSession{}).
		Where("id = ? AND user_id = ?", sessionID, userID).
		Updates(updates).Error
}

func (r *GormRepository) CountUnusedRecoveryCodes(ctx context.Context, userID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.UserRecoveryCode{}).
		Where("user_id = ? AND used_at IS NULL", userID).Count(&count).Error
	return count, err
}

func (r *GormRepository) UseRecoveryCode(ctx context.Context, userID uint, codeHash string) (int64, error) {
	result := r.db.WithContext(ctx).Model(&models.UserRecoveryCode{}).
		Where("user_id = ? AND code_hash = ? AND used_at IS NULL", userID, codeHash).
		Update("used_at", time.Now())
	return result.RowsAffected, result.Error
}

func (r *GormRepository) DeleteRecoveryCodesByUser(ctx context.Context, userID uint) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&models.UserRecoveryCode{}).Error
}

func (r *GormRepository) ReplaceRecoveryCodes(ctx context.Context, userID uint, hashes []string, createdAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&models.UserRecoveryCode{}).Error; err != nil {
			return err
		}
		rows := make([]models.UserRecoveryCode, 0, len(hashes))
		for _, h := range hashes {
			rows = append(rows, models.UserRecoveryCode{UserID: userID, CodeHash: h, CreatedAt: createdAt})
		}
		return tx.Create(&rows).Error
	})
}
