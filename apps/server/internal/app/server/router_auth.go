package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/handlers"
	"servify/apps/server/internal/middleware"
	authapp "servify/apps/server/internal/modules/auth/application"
	authdelivery "servify/apps/server/internal/modules/auth/delivery"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/configscope"
	"servify/apps/server/internal/platform/storage"
	storagefactory "servify/apps/server/internal/platform/storage/factory"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func authPolicies(db *gorm.DB) []platformauth.TokenPolicy {
	return []platformauth.TokenPolicy{
		platformauth.NewRevokedTokenPolicy(db),
		platformauth.NewUserStateTokenPolicy(db),
	}
}

func registerAuthRoutes(r *gin.Engine, deps Dependencies) {
	auth := r.Group("/api/v1/auth")
	// auth 公开面审计（P2-5 第二刀）：登录/注册/刷新/2FA 挑战/登出成败均
	// 留痕（4xx 一并记录，凭据字段由审计层 redact）。
	auth.Use(middleware.AuditMiddlewareWithFailures(deps.DB))
	sessionRiskResolver := configscope.NewResolver(
		deps.Config,
		configscope.WithTenantSessionRiskProvider(configscope.NewGormTenantConfigProvider(deps.DB)),
		configscope.WithWorkspaceSessionRiskProvider(configscope.NewGormWorkspaceConfigProvider(deps.DB)),
	)
	authService := authapp.NewService(deps.DB, deps.Config)
	// refresh token 重放处置档位（P2-5 第三刀）：不依赖情报源，无条件注入
	// （off/未配置时等价 no-op，刷新行为不变）。
	authService.WithRefreshReusePolicy(deps.Config.Security.SessionRisk.RefreshReusePolicy)
	authHandler := handlers.NewAuthHandler(authService).WithSessionRiskResolver(sessionRiskResolver)
	if provider := sessionIPIntelligenceFromConfig(deps.Config); provider != nil {
		authHandler = authHandler.WithSessionIPIntelligence(provider)
		// 登录风险执行：情报源就位时按 login_enforcement 档位启用
		// （off/未配置时注入等价 no-op，登录行为不变）。WithLoginRiskEnforcement
		// 原地修改并返回同指针，2FA/OIDC handler 共享的本实例同步生效。
		authService.WithLoginRiskEnforcement(provider, deps.Config.Security.SessionRisk.LoginEnforcement)
	}

	auth.POST("/register", authHandler.Register)
	auth.POST("/login", authHandler.Login)
	auth.POST("/refresh", authHandler.RefreshToken)

	// TOTP 两步验证：挑战步换会话在 public 组（限流沿用 auth 前缀 25rpm）；
	// 绑定/解绑/恢复码自服务走 authMe 组。
	auth2FA := handlers.NewAuth2FAHandler(authService)
	auth.POST("/2fa/verify", auth2FA.VerifyLogin)

	authMe := auth.Group("")
	authMe.Use(middleware.AuthMiddleware(deps.Config, deps.DB, authPolicies(deps.DB)...))
	authMe.GET("/me", authHandler.GetCurrentUser)
	authMe.GET("/sessions", authHandler.ListSessions)
	authMe.POST("/sessions/logout-current", authHandler.LogoutCurrentSession)
	authMe.POST("/sessions/logout-others", authHandler.LogoutOtherSessions)
	authMe.POST("/2fa/setup", auth2FA.Setup)
	authMe.POST("/2fa/enable", auth2FA.Enable)
	authMe.POST("/2fa/disable", auth2FA.Disable)
	authMe.GET("/2fa/recovery-codes", auth2FA.RecoveryCodes)
	authMe.POST("/2fa/recovery-codes/regenerate", auth2FA.RegenerateRecoveryCodes)

	registerOIDCRoutes(r, deps, authService)
	registerUploadRoutes(r, deps)
}

// registerOIDCRoutes wires the admin SSO endpoints. /api/v1/auth/oidc/* stays
// inside the auth-public surface (rate limit + catalog), so the security
// surface catalog needs no changes.
func registerOIDCRoutes(r *gin.Engine, deps Dependencies, authService authdelivery.HandlerService) {
	if deps.OIDCProvider != nil {
		oidcHandler := handlers.NewOIDCHandler(
			deps.OIDCProvider,
			deps.Config.OIDC,
			authService,
			deps.Config.Server.Environment,
		)
		oidcGroup := r.Group("/api/v1/auth/oidc")
		oidcGroup.GET("/start", oidcHandler.Start)
		oidcGroup.GET("/callback", oidcHandler.Callback)
		oidcGroup.GET("/status", oidcHandler.Status)
		return
	}
	// SSO 关闭时 status 仍响应，前端据此决定是否渲染 SSO 按钮
	r.GET("/api/v1/auth/oidc/status", handlers.NewOIDCHandler(nil, deps.Config.OIDC, nil, deps.Config.Server.Environment).Status)
}

// registerUploadRoutes wires the upload endpoint and object serving from
// config.Upload (provider selection, size limit, extension whitelist).
// A failed S3 construction is fail-closed: routes are not registered.
func registerUploadRoutes(r *gin.Engine, deps Dependencies) {
	cfg := deps.Config.Upload
	logger := deps.Logger

	provider, err := storagefactory.NewProvider(context.Background(), storagefactory.FactoryConfig{
		Provider: cfg.Provider,
		Local:    storagefactory.LocalConfig{BaseDir: cfg.StoragePath, URLBase: "/uploads"},
		S3: storagefactory.S3Config{
			Region:               cfg.S3.Region,
			Bucket:               cfg.S3.Bucket,
			Endpoint:             cfg.S3.Endpoint,
			AccessKeyID:          cfg.S3.AccessKeyID,
			SecretAccessKey:      cfg.S3.SecretAccessKey,
			ForcePathStyle:       cfg.S3.ForcePathStyle,
			PresignExpirySeconds: cfg.S3.PresignExpirySeconds,
			PublicBaseURL:        cfg.S3.PublicBaseURL,
		},
	})
	if err != nil {
		if logger != nil {
			logger.WithError(err).Errorf("storage provider %q unavailable; upload endpoints disabled", cfg.Provider)
		}
		return
	}

	maxSize := int64(32 << 20)
	if n, err := config.ParseSizeBytes(cfg.MaxFileSize); err != nil {
		if logger != nil {
			logger.WithError(err).Warnf("invalid upload.max_file_size %q; falling back to 32MB", cfg.MaxFileSize)
		}
	} else if n > 0 {
		maxSize = n
	}

	uploadHandler := handlers.NewFileUploadHandlerWithConfig(provider, handlers.UploadHandlerConfig{
		MaxSize:     maxSize,
		AllowedExts: cfg.AllowedTypes,
	})
	r.POST("/api/v1/upload", middleware.AuthMiddleware(deps.Config, deps.DB, authPolicies(deps.DB)...), uploadHandler.Upload)

	// 访客面：远程协助录制元数据回写与同意表态（文件本体走上方
	// /api/v1/upload；归属校验在服务内）。写操作挂审计（RA-3）：拒绝
	// 表态与录制回写均落审计留痕。
	if deps.AssistHandlerService != nil {
		recordingHandler := handlers.NewAssistRecordingHandler(deps.AssistHandlerService)
		visitorAssistAuth := middleware.AuthMiddleware(deps.Config, deps.DB, authPolicies(deps.DB)...)
		r.POST("/api/v1/remote-assist/:id/recording",
			visitorAssistAuth,
			middleware.AuditMiddleware(deps.DB),
			recordingHandler.AttachRecording)
		r.POST("/api/v1/remote-assist/:id/consent",
			visitorAssistAuth,
			middleware.AuditMiddleware(deps.DB),
			recordingHandler.RespondConsent)
	}

	// 聊天文本实时翻译（Phase 0/0.5，docs/realtime-translation-design.md）：
	// 坐席与访客两面共用的单条消息翻译端点。单一注册点（与 /api/v1/upload
	// 同款：AuthMiddleware 认证即可，无主体种类限制），未装配时不注册
	// （nil 安全由装配层保证，不会出现匿名可打的降级端点）。
	// 租户级用量配额见设计文档 §3.2（Phase 3）。
	if deps.TranslationHandlerService != nil {
		r.POST("/api/v1/translation/translate",
			middleware.AuthMiddleware(deps.Config, deps.DB, authPolicies(deps.DB)...),
			handlers.NewTranslationHandler(deps.TranslationHandlerService).Translate)
	}

	isS3 := strings.EqualFold(strings.TrimSpace(cfg.Provider), "s3")
	if isS3 {
		// S3 模式：/uploads/<key> 302 到现签 presigned URL（或 public_base_url），
		// 消息中固化的链接因此永不过期，且每次访问都经过本服务的限流与审计。
		r.GET("/uploads/*filepath", uploadsPresignRedirect(provider, cfg.S3))
		return
	}
	// 本地模式不走 gin Static（底层 http.FileServer 会列目录）：公开面只
	// 允许命中具体文件，目录与缺失一律 404（P2-5 第一刀）。
	uploadsHandler := r.Group("/uploads")
	uploadsHandler.GET("/*filepath", uploadsLocalFileHandler(cfg.StoragePath))
	uploadsHandler.HEAD("/*filepath", uploadsLocalFileHandler(cfg.StoragePath))
}

// uploadsLocalFileHandler 提供本地上传文件的只读访问：经 http.Dir 解析
// （内部拒绝 `..` 逃逸），目录与不存在路径统一 404，不暴露存储拓扑。
func uploadsLocalFileHandler(root string) gin.HandlerFunc {
	fileSystem := http.Dir(root)
	return func(c *gin.Context) {
		rel := strings.TrimPrefix(c.Param("filepath"), "/")
		if rel == "" {
			c.Status(http.StatusNotFound)
			return
		}
		raw, err := fileSystem.Open("/" + rel)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		defer raw.Close()
		info, err := raw.Stat()
		if err != nil || !info.Mode().IsRegular() {
			c.Status(http.StatusNotFound)
			return
		}
		http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), raw)
	}
}

// uploadsPresignRedirect serves stable /uploads/<key> links against object storage.
func uploadsPresignRedirect(provider storage.Provider, s3Cfg config.S3UploadConfig) gin.HandlerFunc {
	expiry := time.Duration(s3Cfg.PresignExpirySeconds) * time.Second
	if expiry <= 0 {
		expiry = time.Hour
	}
	publicBase := strings.TrimSuffix(strings.TrimSpace(s3Cfg.PublicBaseURL), "/")
	return func(c *gin.Context) {
		key := strings.TrimPrefix(c.Param("filepath"), "/")
		if key == "" {
			c.Status(http.StatusNotFound)
			return
		}
		if publicBase != "" {
			c.Redirect(http.StatusFound, publicBase+"/"+key)
			return
		}
		url, err := provider.PresignedURL(key, int(expiry.Seconds()))
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "生成文件访问链接失败"})
			return
		}
		c.Redirect(http.StatusFound, url)
	}
}

func sessionIPIntelligenceFromConfig(cfg *config.Config) *handlers.HTTPSessionIPIntelligence {
	if cfg == nil {
		return nil
	}
	providerCfg := cfg.Security.SessionIPIntelligence
	if !providerCfg.Enabled || strings.TrimSpace(providerCfg.BaseURL) == "" {
		return nil
	}
	timeout := time.Duration(providerCfg.TimeoutMs) * time.Millisecond
	return handlers.NewHTTPSessionIPIntelligence(providerCfg.BaseURL, providerCfg.APIKey, providerCfg.AuthHeader, timeout)
}
