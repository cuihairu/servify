package delivery

// types.go re-export 应用层类型与错误：handler 只 import delivery 一包。

import (
	authapp "servify/apps/server/internal/modules/auth/application"
)

type (
	// RegisterInput 注册请求
	RegisterInput = authapp.RegisterInput
	// LoginInput 登录请求
	LoginInput = authapp.LoginInput
	// AuthSessionMetadata 会话元数据（设备指纹/UA/IP）
	AuthSessionMetadata = authapp.AuthSessionMetadata
	// AuthResult 登录/刷新/OIDC 的签发结果
	AuthResult = authapp.AuthResult
	// LoginOutcome 区分单因子直登与两步挑战
	LoginOutcome = authapp.LoginOutcome
	// TwoFactorSetup 绑定流程第一步的产物
	TwoFactorSetup = authapp.TwoFactorSetup
	// TwoFactorVerifyInput 挑战步换取会话的输入
	TwoFactorVerifyInput = authapp.TwoFactorVerifyInput
	// OIDCIdentity 已验证的 IdP 身份声明
	OIDCIdentity = authapp.OIDCIdentity
	// LoginRiskIntel 登录来源 IP 情报源契约
	LoginRiskIntel = authapp.LoginRiskIntel
)

// 认证与两步验证错误（消息保持原字符串，handler 按文案映射状态码）。
var (
	ErrInvalidAuthInput          = authapp.ErrInvalidAuthInput
	ErrAuthUserAlreadyExists     = authapp.ErrAuthUserAlreadyExists
	ErrAuthInvalidCredentials    = authapp.ErrAuthInvalidCredentials
	ErrAuthUserDisabled          = authapp.ErrAuthUserDisabled
	ErrAuthInvalidRefreshToken   = authapp.ErrAuthInvalidRefreshToken
	ErrLoginBlockedByRisk        = authapp.ErrLoginBlockedByRisk
	ErrOIDCUnverifiedEmail       = authapp.ErrOIDCUnverifiedEmail
	ErrOIDCDomainNotAllowed      = authapp.ErrOIDCDomainNotAllowed
	ErrOIDCAutoProvisionDisabled = authapp.ErrOIDCAutoProvisionDisabled
	ErrTwoFactorDisabled         = authapp.ErrTwoFactorDisabled
	ErrTwoFactorAlreadyEnabled   = authapp.ErrTwoFactorAlreadyEnabled
	ErrTwoFactorNotEnabled       = authapp.ErrTwoFactorNotEnabled
	ErrAuthInvalid2FACode        = authapp.ErrAuthInvalid2FACode
	ErrAuthInvalid2FAChallenge   = authapp.ErrAuthInvalid2FAChallenge
)
