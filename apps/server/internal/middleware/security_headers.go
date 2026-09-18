package middleware

import (
	"net/http"
	"strconv"

	"servify/apps/server/internal/config"

	"github.com/gin-gonic/gin"
)

// SecurityHeadersMiddleware 在所有响应上注入统一安全响应头（P2-5 第一刀）。
// cfg.Security.Headers.Enabled 为 false 时 no-op，保持既有部署行为不变。
// HSTS 仅在服务直连 TLS 时开启（反代终结 TLS 的部署由代理注入）。
func SecurityHeadersMiddleware(cfg *config.Config) gin.HandlerFunc {
	h := config.SecurityHeadersConfig{}
	if cfg != nil {
		h = cfg.Security.Headers
	}
	if !h.Enabled {
		return func(c *gin.Context) { c.Next() }
	}
	frame := h.FrameOptions
	if frame == "" {
		frame = "DENY"
	}
	referrer := h.ReferrerPolicy
	if referrer == "" {
		referrer = "strict-origin-when-cross-origin"
	}
	hstsMaxAge := h.HSTSMaxAge
	if hstsMaxAge <= 0 {
		hstsMaxAge = 31536000
	}
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", frame)
		c.Header("Referrer-Policy", referrer)
		if h.ContentSecurityPolicy != "" {
			c.Header("Content-Security-Policy", h.ContentSecurityPolicy)
		}
		if h.HSTSEnabled {
			c.Header("Strict-Transport-Security", "max-age="+strconv.Itoa(hstsMaxAge))
		}
		c.Next()
	}
}

// MaxBodyBytesMiddleware 施加全局请求体上限。maxBytes<=0 时 no-op
// （保持既有行为；上传端点另有自身 MaxBytesReader）。
// Content-Length 超限时直接 413；分块传输的超限读取由 http.MaxBytesReader
// 在 handler 读取 body 时兜底失败。
func MaxBodyBytesMiddleware(maxBytes int64) gin.HandlerFunc {
	if maxBytes <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":   "Request Entity Too Large",
				"message": "request body exceeds the configured limit",
			})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
