package delivery

// HandlerService 是认证核心（注册/登录/会话/刷新/OIDC SSO）面向 HTTP 层的
// 契约；TwoFactorService 是 TOTP 两步验证端点的窄服务面（Login 已覆盖挑战步
// 发起，这里补绑定/解绑/恢复码与挑战步换取会话）。两个契约均由
// application.Service 直接满足，无需适配层。

import (
	"context"

	"servify/apps/server/internal/models"
)

type HandlerService interface {
	Register(ctx context.Context, req RegisterInput, meta AuthSessionMetadata) (*AuthResult, error)
	Login(ctx context.Context, req LoginInput, meta AuthSessionMetadata) (*LoginOutcome, error)
	GetCurrentUser(ctx context.Context, userID uint) (*models.User, error)
	ListAuthSessions(ctx context.Context, userID uint) ([]models.UserAuthSession, error)
	RevokeCurrentSession(ctx context.Context, userID uint, sessionID string) (*models.UserAuthSession, error)
	RevokeOtherSessions(ctx context.Context, userID uint, currentSessionID string) (int, error)
	RefreshToken(ctx context.Context, refreshToken string, meta AuthSessionMetadata) (*AuthResult, error)
	LoginWithOIDC(ctx context.Context, identity OIDCIdentity, meta AuthSessionMetadata) (*AuthResult, error)
}

type TwoFactorService interface {
	SetupTwoFactor(ctx context.Context, userID uint) (*TwoFactorSetup, error)
	EnableTwoFactor(ctx context.Context, userID uint, secret, code string) ([]string, error)
	DisableTwoFactor(ctx context.Context, userID uint, password, code string) error
	RegenerateRecoveryCodes(ctx context.Context, userID uint, code string) ([]string, error)
	RecoveryCodesRemaining(ctx context.Context, userID uint) (int64, error)
	VerifyTwoFactorLogin(ctx context.Context, req TwoFactorVerifyInput, meta AuthSessionMetadata) (*AuthResult, error)
}
