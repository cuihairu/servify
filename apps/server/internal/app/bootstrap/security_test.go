package bootstrap

import (
	"bytes"
	"strings"
	"testing"

	"servify/apps/server/internal/config"

	"github.com/sirupsen/logrus"
)

func TestSecurityWarnings(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Security.RateLimiting.Enabled = false
	cfg.Security.CORS.AllowedOrigins = []string{"*"}
	cfg.AI.OpenAI.APIKey = ""
	cfg.Dify.Enabled = true
	cfg.Dify.APIKey = ""
	cfg.Dify.DatasetID = ""
	cfg.WeKnora.Enabled = true
	cfg.WeKnora.APIKey = ""
	cfg.RagFlow.Enabled = true
	cfg.RagFlow.APIKey = ""
	cfg.RagFlow.DatasetID = ""

	warnings := SecurityWarnings(cfg)
	joined := strings.Join(warnings, "\n")

	for _, want := range []string{
		"jwt.secret is empty or using the default value",
		"database.password is empty or using a default value",
		"security.cors.allowed_origins allows all origins",
		"security.rate_limiting is disabled",
		"ai.openai.api_key is empty",
		"dify is enabled but dify.api_key is empty",
		"dify is enabled but dify.dataset_id is empty",
		"weknora is enabled but weknora.api_key is empty or using default value",
		"ragflow is enabled but ragflow.api_key is empty",
		"ragflow is enabled but ragflow.dataset_id is empty",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing warning %q in %q", want, joined)
		}
	}
}

func TestSecurityWarnings_NoExternalKnowledgeProviderEnabled(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.JWT.Secret = "prod-secret"
	cfg.Database.Password = "secure-db-password" // Override default
	cfg.Security.RateLimiting.Enabled = true
	cfg.Security.CORS.AllowedOrigins = []string{"https://app.example.com"}
	cfg.Security.RateLimiting.Paths = []config.PathRateLimitConfig{
		{Enabled: true, Prefix: "/public/", RequestsPerMinute: 120, Burst: 30},
		{Enabled: true, Prefix: "/public/kb/", RequestsPerMinute: 60, Burst: 15},
		{Enabled: true, Prefix: "/public/csat/", RequestsPerMinute: 30, Burst: 10},
		{Enabled: true, Prefix: "/api/v1/auth/", RequestsPerMinute: 25, Burst: 10},
		{Enabled: true, Prefix: "/api/v1/ws", RequestsPerMinute: 30, Burst: 10},
		{Enabled: true, Prefix: "/uploads/", RequestsPerMinute: 90, Burst: 20},
		{Enabled: true, Prefix: "/api/v1/metrics/ingest", RequestsPerMinute: 120, Burst: 30},
		{Enabled: true, Prefix: "/api/", RequestsPerMinute: 90, Burst: 20},
	}
	cfg.AI.OpenAI.APIKey = "openai-key"
	cfg.Dify.Enabled = false
	cfg.WeKnora.Enabled = false
	cfg.RagFlow.Enabled = false

	// v0.1.0允许fallback模式，不应对无知识provider产生警告
	warnings := SecurityWarnings(cfg)
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for v0.1.0 fallback mode, got %d: %q", len(warnings), warnings)
	}
}

func TestSecurityWarnings_PublicSurfaceRateLimitCoverage(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.JWT.Secret = "prod-secret"
	cfg.Database.Password = "secure-db-password"
	cfg.Security.CORS.AllowedOrigins = []string{"https://app.example.com"}
	cfg.Security.RateLimiting.Enabled = true
	cfg.Security.RateLimiting.Paths = []config.PathRateLimitConfig{
		{
			Enabled:           true,
			Prefix:            "/api/",
			RequestsPerMinute: 60,
			Burst:             10,
		},
	}
	cfg.AI.OpenAI.APIKey = "openai-key"

	warnings := SecurityWarnings(cfg)
	joined := strings.Join(warnings, "\n")

	for _, want := range []string{
		"security.rate_limiting.paths has no dedicated limit for /public/ (anonymous public surface baseline)",
		"security.rate_limiting.paths has no dedicated limit for /public/kb/ (public knowledge base enumeration and crawl risk)",
		"security.rate_limiting.paths has no dedicated limit for /public/csat/ (public survey token access and submission risk)",
		"security.rate_limiting.paths has no dedicated limit for /api/v1/auth/ (anonymous auth entrypoints)",
		"security.rate_limiting.paths has no dedicated limit for /api/v1/ws (anonymous realtime connection surface)",
		"security.rate_limiting.paths has no dedicated limit for /uploads/ (public uploaded asset surface)",
		"security.rate_limiting.paths has no dedicated limit for /api/v1/metrics/ingest (service ingestion surface)",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing warning %q in %q", want, joined)
		}
	}
}

func TestSecurityWarnings_PublicSurfaceRateLimitCoverageSatisfied(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.JWT.Secret = "prod-secret"
	cfg.Database.Password = "secure-db-password"
	cfg.Security.CORS.AllowedOrigins = []string{"https://app.example.com"}
	cfg.Security.RateLimiting.Enabled = true
	cfg.Security.RateLimiting.Paths = []config.PathRateLimitConfig{
		{
			Enabled:           true,
			Prefix:            "/public/",
			RequestsPerMinute: 120,
			Burst:             30,
		},
		{
			Enabled:           true,
			Prefix:            "/public/kb/",
			RequestsPerMinute: 60,
			Burst:             15,
		},
		{
			Enabled:           true,
			Prefix:            "/public/csat/",
			RequestsPerMinute: 30,
			Burst:             10,
		},
		{
			Enabled:           true,
			Prefix:            "/api/v1/auth/",
			RequestsPerMinute: 25,
			Burst:             10,
		},
		{
			Enabled:           true,
			Prefix:            "/api/v1/ws",
			RequestsPerMinute: 30,
			Burst:             10,
		},
		{
			Enabled:           true,
			Prefix:            "/uploads/",
			RequestsPerMinute: 90,
			Burst:             20,
		},
		{
			Enabled:           true,
			Prefix:            "/api/v1/metrics/ingest",
			RequestsPerMinute: 120,
			Burst:             30,
		},
		{
			Enabled:           true,
			Prefix:            "/api/",
			RequestsPerMinute: 90,
			Burst:             20,
		},
	}
	cfg.AI.OpenAI.APIKey = "openai-key"

	warnings := strings.Join(SecurityWarnings(cfg), "\n")
	for _, denied := range []string{"/public/", "/public/kb/", "/public/csat/", "/api/v1/auth/", "/api/v1/ws", "/uploads/", "/api/v1/metrics/ingest", "/api/"} {
		if strings.Contains(warnings, denied) {
			t.Fatalf("unexpected public/service/management surface warning for %s in %q", denied, warnings)
		}
	}
}

func TestSecurityWarnings_PublicSurfaceInheritedLimitDoesNotCountAsDedicated(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.JWT.Secret = "prod-secret"
	cfg.Database.Password = "secure-db-password"
	cfg.Security.CORS.AllowedOrigins = []string{"https://app.example.com"}
	cfg.Security.RateLimiting.Enabled = true
	cfg.Security.RateLimiting.Paths = []config.PathRateLimitConfig{
		{
			Enabled:           true,
			Prefix:            "/public/",
			RequestsPerMinute: 120,
			Burst:             30,
		},
		{
			Enabled:           true,
			Prefix:            "/api/v1/ws",
			RequestsPerMinute: 30,
			Burst:             10,
		},
		{
			Enabled:           true,
			Prefix:            "/uploads/",
			RequestsPerMinute: 90,
			Burst:             20,
		},
		{
			Enabled:           true,
			Prefix:            "/api/v1/metrics/ingest",
			RequestsPerMinute: 120,
			Burst:             30,
		},
		{
			Enabled:           true,
			Prefix:            "/api/",
			RequestsPerMinute: 90,
			Burst:             20,
		},
	}
	cfg.AI.OpenAI.APIKey = "openai-key"

	warnings := strings.Join(SecurityWarnings(cfg), "\n")
	for _, want := range []string{"/public/kb/", "/public/csat/", "/api/v1/auth/"} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("expected dedicated-path warning for %s in %q", want, warnings)
		}
	}
	if strings.Contains(warnings, "/public/ (anonymous public surface baseline)") {
		t.Fatalf("unexpected public surface warning in %q", warnings)
	}
}

func TestLogSecurityWarnings(t *testing.T) {
	cfg := config.GetDefaultConfig()

	var buf bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})

	LogSecurityWarnings(logger, cfg)

	output := buf.String()
	if !strings.Contains(output, "security baseline warning") {
		t.Fatalf("expected warning output, got %q", output)
	}
	if !strings.Contains(output, "jwt.secret is empty or using the default value") {
		t.Fatalf("expected jwt warning in %q", output)
	}
	if !strings.Contains(output, "database.password is empty or using a default value") {
		t.Fatalf("expected database password warning in %q", output)
	}
}
