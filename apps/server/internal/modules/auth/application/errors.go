package application

import "errors"

var (
	// ErrAuthInvalidDB 对应原 gorm.ErrInvalidDB 语义（nil 仓储/nil 服务保护），
	// 文案保持 "invalid db"；仅应用层单测断言，handlers 不感知。
	ErrAuthInvalidDB = errors.New("invalid db")
	// ErrAuthUserNotFound 端口查不到用户的统一哨兵（映射原 gorm.ErrRecordNotFound
	// 的语义，应用层不再引用 gorm）。
	ErrAuthUserNotFound = errors.New("auth user not found")

	ErrInvalidAuthInput        = errors.New("invalid auth input")
	ErrAuthUserAlreadyExists   = errors.New("auth user already exists")
	ErrAuthInvalidCredentials  = errors.New("invalid auth credentials")
	ErrAuthUserDisabled        = errors.New("auth user disabled")
	ErrAuthInvalidRefreshToken = errors.New("invalid auth refresh token")
	// 登录风险执行（P2-5 第二刀）：高风险来源登录被策略拦截。block 模式
	// 直接拒绝；step_up 模式下用户无可用的第二因子时同样落到此错误。
	// 对外只映射为 403 通用失败，不回显判定依据。
	ErrLoginBlockedByRisk = errors.New("login blocked by risk policy")
	// OIDC SSO 登录专用错误（对外只映射为通用失败码，见 handlers/oidc_handler.go）
	ErrOIDCUnverifiedEmail       = errors.New("oidc identity email is missing or unverified")
	ErrOIDCDomainNotAllowed      = errors.New("oidc identity email domain is not allowed")
	ErrOIDCAutoProvisionDisabled = errors.New("oidc identity has no local account and auto-provision is disabled")
)
