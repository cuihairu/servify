package server

import (
	"net/http"
	"strings"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/middleware"
	svcerrors "servify/apps/server/internal/observability/errors"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	"servify/apps/server/internal/observability/telemetry"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// bodyLimitBytes 归一化全局 body 上限：<=0 视为未配置（中间件 no-op）。
func bodyLimitBytes(cfg *config.Config) int64 {
	if cfg == nil {
		return 0
	}
	return cfg.Security.MaxBodyBytes
}

func registerBaseMiddleware(r *gin.Engine, cfg *config.Config, httpMetrics *svcmetrics.HTTPMetrics) {
	if cfg != nil && cfg.Log.Level == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r.Use(telemetry.RequestIDMiddleware())
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	// 安全响应头在限流/body 上限之前挂载：429/413 等中止响应也带安全头。
	r.Use(middleware.SecurityHeadersMiddleware(cfg))
	r.Use(corsMiddlewareWithConfig(cfg))
	r.Use(middleware.MaxBodyBytesMiddleware(bodyLimitBytes(cfg)))
	r.Use(middleware.RateLimitMiddlewareFromConfig(cfg))
	if cfg != nil && cfg.Monitoring.Tracing.Enabled {
		r.Use(otelgin.Middleware(cfg.Monitoring.Tracing.ServiceName))
	}
	if httpMetrics != nil {
		r.Use(httpMetrics.Middleware())
		// errors_total 统一出口：与 HTTPMetrics 同一挂载条件（监控启用），
		// 覆盖全部路由的 5xx 错误分类打点。
		r.Use(svcerrors.StatusMiddleware())
	}
}

// corsMiddlewareWithConfig 装配 CORS。
//   - 未启用或白名单为空：无条件放行 `*`（开发默认，保持既有行为）。
//   - 单一 origin：无条件回显该 origin——单站点部署语义即"只有它用"，
//     且预检（OPTIONS 无 Origin 头）也必须拿到 ACAO。
//   - 多 origin（P2-5 第一刀修复）：此前 strings.Join 输出多值 ACAO 头
//     属非法 HTTP（浏览器一律拒绝），改为按请求 Origin 白名单回显；
//     未命中不带 ACAO（由浏览器侧拦截），并加 Vary: Origin 防中间层缓存串答。
func corsMiddlewareWithConfig(cfg *config.Config) gin.HandlerFunc {
	allowedOrigins := "*"
	allowedMethods := "GET, POST, PUT, DELETE, OPTIONS"
	allowedHeaders := "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Request-ID"
	perRequestOrigins := false
	if cfg != nil && cfg.Security.CORS.Enabled {
		if len(cfg.Security.CORS.AllowedOrigins) == 1 {
			allowedOrigins = cfg.Security.CORS.AllowedOrigins[0]
		} else if len(cfg.Security.CORS.AllowedOrigins) > 1 {
			perRequestOrigins = true
		}
		if len(cfg.Security.CORS.AllowedMethods) > 0 {
			allowedMethods = strings.Join(cfg.Security.CORS.AllowedMethods, ", ")
		}
		if len(cfg.Security.CORS.AllowedHeaders) > 0 {
			allowedHeaders = strings.Join(cfg.Security.CORS.AllowedHeaders, ", ")
		}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if perRequestOrigins {
			c.Header("Vary", "Origin")
			allowed := false
			for _, o := range cfg.Security.CORS.AllowedOrigins {
				if origin == o {
					allowed = true
					break
				}
			}
			if allowed {
				c.Header("Access-Control-Allow-Origin", origin)
			}
		} else {
			c.Header("Access-Control-Allow-Origin", allowedOrigins)
		}
		c.Header("Access-Control-Allow-Methods", allowedMethods)
		c.Header("Access-Control-Allow-Headers", allowedHeaders)
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
