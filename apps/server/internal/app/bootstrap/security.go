package bootstrap

import (
	"fmt"
	"strings"

	"servify/apps/server/internal/config"

	"github.com/sirupsen/logrus"
)

// Use shared insecure defaults lists from config package to avoid duplication

type requiredRateLimitPath struct {
	prefix string
	reason string
}

var requiredSecurityRateLimitPaths = []requiredRateLimitPath{
	{prefix: "/public/", reason: "anonymous public surface baseline"},
	{prefix: "/public/kb/", reason: "public knowledge base enumeration and crawl risk"},
	{prefix: "/public/csat/", reason: "public survey token access and submission risk"},
	{prefix: "/api/v1/auth/", reason: "anonymous auth entrypoints"},
	{prefix: "/api/v1/ws", reason: "anonymous realtime connection surface"},
	{prefix: "/uploads/", reason: "public uploaded asset surface"},
	{prefix: "/api/v1/metrics/ingest", reason: "service ingestion surface"},
	{prefix: "/api/", reason: "management surface"},
}

func SecurityWarnings(cfg *config.Config) []string {
	if cfg == nil {
		return []string{"config is nil; security defaults may be unsafe"}
	}

	var warnings []string

	// Check for insecure JWT secret (empty or known insecure default)
	secret := strings.TrimSpace(cfg.JWT.Secret)
	if secret == "" || config.InsecureJWTSecrets[secret] {
		warnings = append(warnings, "jwt.secret is empty or using the default value")
	}
	// Check for insecure database password (using shared list from config)
	if config.InsecureDatabasePasswords[cfg.Database.Password] {
		warnings = append(warnings, "database.password is empty or using a default value")
	}
	if cfg.Security.CORS.Enabled && len(cfg.Security.CORS.AllowedOrigins) == 1 && strings.TrimSpace(cfg.Security.CORS.AllowedOrigins[0]) == "*" {
		warnings = append(warnings, "security.cors.allowed_origins allows all origins")
	}
	if !cfg.Security.RateLimiting.Enabled {
		warnings = append(warnings, "security.rate_limiting is disabled")
	} else {
		for _, required := range requiredSecurityRateLimitPaths {
			if !hasRateLimitPrefix(cfg.Security.RateLimiting.Paths, required.prefix) {
				warnings = append(warnings, fmt.Sprintf("security.rate_limiting.paths has no dedicated limit for %s (%s)", required.prefix, required.reason))
			}
		}
	}
	if strings.TrimSpace(cfg.AI.OpenAI.APIKey) == "" {
		warnings = append(warnings, "ai.openai.api_key is empty")
	}
	if cfg.Dify.Enabled && strings.TrimSpace(cfg.Dify.APIKey) == "" {
		warnings = append(warnings, "dify is enabled but dify.api_key is empty")
	}
	if cfg.Dify.Enabled && strings.TrimSpace(cfg.Dify.DatasetID) == "" {
		warnings = append(warnings, "dify is enabled but dify.dataset_id is empty")
	}
	if cfg.RagFlow.Enabled && strings.TrimSpace(cfg.RagFlow.APIKey) == "" {
		warnings = append(warnings, "ragflow is enabled but ragflow.api_key is empty")
	}
	if cfg.RagFlow.Enabled && strings.TrimSpace(cfg.RagFlow.DatasetID) == "" {
		warnings = append(warnings, "ragflow is enabled but ragflow.dataset_id is empty")
	}
	// 移除知识provider未启用的警告（v0.1.0允许fallback模式）
	// if !cfg.Dify.Enabled && !cfg.WeKnora.Enabled {
	// 	warnings = append(warnings, "no external knowledge provider is enabled; ai will rely on fallback mode only")
	// }
	if cfg.WeKnora.Enabled {
		apiKey := strings.TrimSpace(cfg.WeKnora.APIKey)
		if apiKey == "" || config.InsecureWeKnoraAPIKeys[apiKey] {
			warnings = append(warnings, "weknora is enabled but weknora.api_key is empty or using default value")
		}
	}

	return warnings
}

func hasRateLimitPrefix(paths []config.PathRateLimitConfig, prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}
	for _, path := range paths {
		if !path.Enabled {
			continue
		}
		if strings.TrimSpace(path.Prefix) == prefix {
			return true
		}
	}
	return false
}

func LogSecurityWarnings(logger *logrus.Logger, cfg *config.Config) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	for _, warning := range SecurityWarnings(cfg) {
		logger.Warn(fmt.Sprintf("security baseline warning: %s", warning))
	}
}
