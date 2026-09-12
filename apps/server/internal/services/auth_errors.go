package services

import "errors"

var (
	ErrInvalidAuthInput        = errors.New("invalid auth input")
	ErrAuthUserAlreadyExists   = errors.New("auth user already exists")
	ErrAuthInvalidCredentials  = errors.New("invalid auth credentials")
	ErrAuthUserDisabled        = errors.New("auth user disabled")
	ErrAuthInvalidRefreshToken = errors.New("invalid auth refresh token")
	// OIDC SSO 登录专用错误（对外只映射为通用失败码，见 handlers/oidc_handler.go）
	ErrOIDCUnverifiedEmail       = errors.New("oidc identity email is missing or unverified")
	ErrOIDCDomainNotAllowed      = errors.New("oidc identity email domain is not allowed")
	ErrOIDCAutoProvisionDisabled = errors.New("oidc identity has no local account and auto-provision is disabled")
)
