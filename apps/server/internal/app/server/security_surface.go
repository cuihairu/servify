package server

import (
	"fmt"
	"sort"
	"strings"

	"servify/apps/server/internal/config"

	"github.com/gin-gonic/gin"
)

type securitySurfaceMatchMode string

const (
	securitySurfaceExact  securitySurfaceMatchMode = "exact"
	securitySurfacePrefix securitySurfaceMatchMode = "prefix"
)

type SecuritySurface struct {
	Name                       string
	Path                       string
	MatchMode                  securitySurfaceMatchMode
	Exposure                   string
	RequiresDedicatedRateLimit bool
	Reason                     string
}

func SecuritySurfaceCatalog(cfg *config.Config) []SecuritySurface {
	surfaces := []SecuritySurface{
		{
			Name:      "health",
			Path:      "/health",
			MatchMode: securitySurfaceExact,
			Exposure:  "public",
			Reason:    "anonymous health probe",
		},
		{
			Name:      "readiness",
			Path:      "/ready",
			MatchMode: securitySurfaceExact,
			Exposure:  "public",
			Reason:    "anonymous readiness probe",
		},
		{
			Name:      "portal-config",
			Path:      "/public/portal/config",
			MatchMode: securitySurfaceExact,
			Exposure:  "public",
			Reason:    "anonymous portal bootstrap config",
		},
		{
			Name:                       "public-knowledge-base",
			Path:                       "/public/kb/",
			MatchMode:                  securitySurfacePrefix,
			Exposure:                   "public",
			RequiresDedicatedRateLimit: true,
			Reason:                     "public knowledge base crawl surface",
		},
		{
			Name:                       "public-csat",
			Path:                       "/public/csat/",
			MatchMode:                  securitySurfacePrefix,
			Exposure:                   "public",
			RequiresDedicatedRateLimit: true,
			Reason:                     "public survey token access and submission surface",
		},
		{
			Name:                       "public-suggestions",
			Path:                       "/public/suggestions/",
			MatchMode:                  securitySurfacePrefix,
			Exposure:                   "public",
			RequiresDedicatedRateLimit: true,
			Reason:                     "anonymous customer-side suggested questions surface (P2-0; serves public knowledge docs only)",
		},
		{
			Name:                       "public-voice-pstn-webhook",
			Path:                       "/public/voice/webhooks/",
			MatchMode:                  securitySurfacePrefix,
			Exposure:                   "public",
			RequiresDedicatedRateLimit: true,
			Reason:                     "hosted vendor voice webhook ingress (vendor-signature authenticated status callbacks)",
		},
		{
			Name:                       "public-realtime",
			Path:                       "/api/v1/ws",
			MatchMode:                  securitySurfaceExact,
			Exposure:                   "public",
			RequiresDedicatedRateLimit: true,
			Reason:                     "anonymous realtime connection surface",
		},
		{
			// 语音翻译通道（Phase 2 刀二b-2）：与 /api/v1/ws 同款免认证
			// 建连面（guest_token.required 开启时同源 token 校验），另受
			// 装配层 ai.asr 配置门控（未配置不注册路由）。
			Name:                       "public-realtime-voice",
			Path:                       "/api/v1/ws/voice",
			MatchMode:                  securitySurfaceExact,
			Exposure:                   "public",
			RequiresDedicatedRateLimit: true,
			Reason:                     "anonymous voice translation channel surface (audio uplink; same trust domain as /api/v1/ws)",
		},
		{
			Name:                       "auth-public",
			Path:                       "/api/v1/auth/",
			MatchMode:                  securitySurfacePrefix,
			Exposure:                   "auth",
			RequiresDedicatedRateLimit: true,
			Reason:                     "anonymous authentication entrypoints",
		},
		{
			Name:                       "public-uploads",
			Path:                       "/uploads/",
			MatchMode:                  securitySurfacePrefix,
			Exposure:                   "public",
			RequiresDedicatedRateLimit: true,
			Reason:                     "public uploaded asset surface",
		},
	}

	if cfg != nil && cfg.Monitoring.Enabled {
		metricsPath := strings.TrimSpace(cfg.Monitoring.MetricsPath)
		if metricsPath != "" {
			surfaces = append(surfaces, SecuritySurface{
				Name:      "metrics",
				Path:      metricsPath,
				MatchMode: securitySurfaceExact,
				Exposure:  "operations",
				Reason:    "anonymous prometheus metrics endpoint",
			})
		}
	}

	return surfaces
}

// routeSecurityWarnings 是包级 seam（默认 RouteSecurityWarnings）：
// BuildRouter 内注册的路由全部来自目录内路径，真实警告在启动装配下
// 不会出现，测试注入以覆盖告警日志分支。
var routeSecurityWarnings = RouteSecurityWarnings

func RouteSecurityWarnings(routes gin.RoutesInfo, cfg *config.Config) []string {
	catalog := SecuritySurfaceCatalog(cfg)
	if len(routes) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	warnings := make([]string, 0)
	for _, route := range routes {
		path := strings.TrimSpace(route.Path)
		if !routeRequiresSecurityCatalog(path, cfg) {
			continue
		}
		if routeMatchesAnySurface(path, catalog) {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		warning := fmt.Sprintf("security surface catalog is missing an entry for %s %s", route.Method, path)
		warnings = append(warnings, warning)
	}
	sort.Strings(warnings)
	return warnings
}

func routeRequiresSecurityCatalog(path string, cfg *config.Config) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if path == "/health" || path == "/ready" || path == "/api/v1/ws" || path == "/api/v1/ws/voice" {
		return true
	}
	if strings.HasPrefix(path, "/public/") || strings.HasPrefix(path, "/uploads/") || strings.HasPrefix(path, "/api/v1/auth/") {
		return true
	}
	if cfg != nil && cfg.Monitoring.Enabled && strings.TrimSpace(cfg.Monitoring.MetricsPath) == path {
		return true
	}
	return false
}

func routeMatchesAnySurface(path string, catalog []SecuritySurface) bool {
	for _, surface := range catalog {
		if routeMatchesSurface(path, surface) {
			return true
		}
	}
	return false
}

func routeMatchesSurface(path string, surface SecuritySurface) bool {
	switch surface.MatchMode {
	case securitySurfacePrefix:
		return strings.HasPrefix(path, surface.Path)
	default:
		return path == surface.Path
	}
}
