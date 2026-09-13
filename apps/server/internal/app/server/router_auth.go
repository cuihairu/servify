package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/handlers"
	"servify/apps/server/internal/middleware"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/configscope"
	"servify/apps/server/internal/platform/storage"
	storagefactory "servify/apps/server/internal/platform/storage/factory"
	"servify/apps/server/internal/services"

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
	sessionRiskResolver := configscope.NewResolver(
		deps.Config,
		configscope.WithTenantSessionRiskProvider(configscope.NewGormTenantConfigProvider(deps.DB)),
		configscope.WithWorkspaceSessionRiskProvider(configscope.NewGormWorkspaceConfigProvider(deps.DB)),
	)
	authHandler := handlers.NewAuthHandler(services.NewAuthService(deps.DB, deps.Config)).WithSessionRiskResolver(sessionRiskResolver)
	if provider := sessionIPIntelligenceFromConfig(deps.Config); provider != nil {
		authHandler = authHandler.WithSessionIPIntelligence(provider)
	}

	auth.POST("/register", authHandler.Register)
	auth.POST("/login", authHandler.Login)
	auth.POST("/refresh", authHandler.RefreshToken)

	// TOTP 两步验证：挑战步换会话在 public 组（限流沿用 auth 前缀 25rpm）；
	// 绑定/解绑/恢复码自服务走 authMe 组。
	auth2FA := handlers.NewAuth2FAHandler(services.NewAuthService(deps.DB, deps.Config))
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

	registerOIDCRoutes(r, deps)
	registerUploadRoutes(r, deps)
}

// registerOIDCRoutes wires the admin SSO endpoints. /api/v1/auth/oidc/* stays
// inside the auth-public surface (rate limit + catalog), so the security
// surface catalog needs no changes.
func registerOIDCRoutes(r *gin.Engine, deps Dependencies) {
	if deps.OIDCProvider != nil {
		oidcHandler := handlers.NewOIDCHandler(
			deps.OIDCProvider,
			deps.Config.OIDC,
			services.NewAuthService(deps.DB, deps.Config),
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

	// 访客面：远程协助录制元数据回写（文件本体走上方 /api/v1/upload；归属校验在服务内）
	if deps.AssistHandlerService != nil {
		recordingHandler := handlers.NewAssistRecordingHandler(deps.AssistHandlerService)
		r.POST("/api/v1/remote-assist/:id/recording",
			middleware.AuthMiddleware(deps.Config, deps.DB, authPolicies(deps.DB)...),
			recordingHandler.AttachRecording)
	}

	isS3 := strings.EqualFold(strings.TrimSpace(cfg.Provider), "s3")
	if isS3 {
		// S3 模式：/uploads/<key> 302 到现签 presigned URL（或 public_base_url），
		// 消息中固化的链接因此永不过期，且每次访问都经过本服务的限流与审计。
		r.GET("/uploads/*filepath", uploadsPresignRedirect(provider, cfg.S3))
		return
	}
	r.Static("/uploads", cfg.StoragePath)
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
