package middleware

import (
	auditplatform "servify/apps/server/internal/platform/audit"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func AuditMiddleware(db *gorm.DB) gin.HandlerFunc {
	return auditplatform.Middleware(auditplatform.NewGormRecorder(db))
}

// AuditMiddlewareWithFailures 在 AuditMiddleware 基础上同时记录 4xx/5xx
// 响应：auth 公开面的失败登录、被风险策略拦截的登录等事件必须留痕。
func AuditMiddlewareWithFailures(db *gorm.DB) gin.HandlerFunc {
	return auditplatform.MiddlewareWithOptions(auditplatform.NewGormRecorder(db), auditplatform.Options{AuditFailures: true})
}
