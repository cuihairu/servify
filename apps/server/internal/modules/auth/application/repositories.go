package application

// Repository 端口（GORM 写归属下沉 infra 后 auth 应用层唯一的持久化出口）。
//
// 返回语义约定：
//   - 用户查询查不到 → 哨兵 ErrAuthUserNotFound（Login 分岔 401、OIDC 分岔
//     自动开通，其余错误原样透传）
//   - CountUsersByUsernameOrEmail / CountUsers 的查询失败沿用旧行为：静默按
//     0 处理（唯一键兜底 / 首个 admin 引导不因查询错误卡死）
//   - UpdateUserLastLogin 错误由调用方吞掉（旧行为 `_ = ...Error`）
//   - 其余错误原样透传
//
// SQL 细节全部留在 infra/gorm_repository.go：COALESCE(token_version)+1、
// LOWER(email) 查询、恢复码原子核销（used_at IS NULL 才置位）与整批替换事务。

import (
	"context"
	"time"

	"servify/apps/server/internal/models"
)

type Repository interface {
	// users
	CountUsersByUsernameOrEmail(ctx context.Context, username, email string) int64
	CountUsers(ctx context.Context) int64
	CreateUser(ctx context.Context, user *models.User) error
	GetUserByUsernameOrEmail(ctx context.Context, identifier string) (*models.User, error)
	GetUserByID(ctx context.Context, id uint) (*models.User, error)
	GetUserByEmailLower(ctx context.Context, email string) (*models.User, error)
	UpdateUserLastLogin(ctx context.Context, user *models.User, now time.Time) error

	// 2FA 绑定/解绑（随 enable/disable 一并推进 token_valid_after）
	EnableUserTwoFactor(ctx context.Context, userID uint, secret string, now time.Time) error
	DisableUserTwoFactor(ctx context.Context, userID uint, tokenValidAfter time.Time) error

	// user_auth_sessions
	CreateAuthSession(ctx context.Context, session *models.UserAuthSession) error
	GetAuthSession(ctx context.Context, sessionID string, userID uint) (*models.UserAuthSession, error)
	ListAuthSessionsByUser(ctx context.Context, userID uint) ([]models.UserAuthSession, error)
	RevokeAuthSession(ctx context.Context, sessionID string, userID uint) (int64, error)
	RevokeOtherAuthSessions(ctx context.Context, userID uint, excludeSessionID string) (int64, error)
	RevokeActiveAuthSession(ctx context.Context, sessionID string, userID uint) error
	RotateAuthSession(ctx context.Context, sessionID string, userID uint, fields AuthSessionRotation) error

	// user_recovery_codes
	CountUnusedRecoveryCodes(ctx context.Context, userID uint) (int64, error)
	UseRecoveryCode(ctx context.Context, userID uint, codeHash string) (int64, error)
	DeleteRecoveryCodesByUser(ctx context.Context, userID uint) error
	ReplaceRecoveryCodes(ctx context.Context, userID uint, hashes []string, createdAt time.Time) error
}

// AuthSessionRotation 携带 refresh 轮换时应用层已归一化的会话字段
// （normalizeAuthSessionField 截断后的指纹/UA/IP）与轮换时间。
type AuthSessionRotation struct {
	DeviceFingerprint string
	UserAgent         string
	ClientIP          string
	Now               time.Time
}
