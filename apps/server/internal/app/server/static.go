package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"servify/apps/server/internal/config"

	"github.com/gin-gonic/gin"
)

var defaultStaticRoots = []string{
	"./apps/admin/dist",
	"./apps/admin",
	"../admin/dist",
	"../admin",
	"/app/apps/admin/dist",
	"/app/apps/admin",
}

// registerStatic 挂 SPA 静态兜底。demo-sdk 是开发演示资产，production 环境
// 不再默认暴露（P3-3）：非 production 才尝试从 ./apps/demo-sdk/ 直接服务。
func registerStatic(r staticRegistrar, cfg *config.Config) {
	root := detectStaticRoot(defaultStaticRoots)
	serveDemoSDK := cfg == nil ||
		!strings.EqualFold(strings.TrimSpace(cfg.Server.Environment), "production")

	// Serve static assets directly (no auth)
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Don't handle API routes
		if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/public") {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
			return
		}

		// Serve demo-sdk assets from ./apps/demo-sdk/ (non-production only)
		if serveDemoSDK {
			if rest, ok := strings.CutPrefix(path, "/demo-sdk/"); ok {
				filePath := filepath.Join(".", "apps", "demo-sdk", filepath.Clean(rest))
				if _, err := os.Stat(filePath); err == nil {
					c.File(filePath)
					return
				}
			}
		}

		// Try to serve the requested file from admin dist
		reqPath := path
		if reqPath == "/" {
			reqPath = "/index.html"
		}
		fullPath := filepath.Join(root, filepath.Clean(reqPath))

		// Check if file exists
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			// Fall back to index.html for SPA routing
			c.File(filepath.Join(root, "index.html"))
			return
		}

		c.File(fullPath)
	})
}

func detectStaticRoot(candidates []string) string {
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "./apps/admin/dist"
}

type staticRegistrar interface {
	NoRoute(handlers ...gin.HandlerFunc)
}
