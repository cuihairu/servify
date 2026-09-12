package middleware

import (
	"servify/apps/server/internal/config"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AuthMiddleware keeps the legacy middleware entrypoint but delegates to platform/auth.
// db 非空时同时启用 X-API-Key 认证（service principal，只读开放面）。
func AuthMiddleware(cfg *config.Config, db *gorm.DB, policies ...platformauth.TokenPolicy) gin.HandlerFunc {
	mwCfg := platformauth.MiddlewareConfigFromApp(cfg)
	mwCfg.Policy = platformauth.ComposeTokenPolicies(policies...)
	if db != nil {
		mwCfg.APIKeyResolver = platformauth.NewGormAPIKeyResolver(db)
	}
	return platformauth.AuthMiddleware(mwCfg)
}

// EnforceRequestScope keeps the compatibility layer aligned with platform/auth scope rules.
func EnforceRequestScope() gin.HandlerFunc {
	return platformauth.EnforceRequestScope()
}
