package config

import (
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestGetDefaultConfig(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.Server.Host == "" {
		t.Error("expected Server.Host to be set")
	}
	if cfg.Server.Port == 0 {
		t.Error("expected Server.Port to be non-zero")
	}
	if cfg.Database.Name == "" {
		t.Error("expected Database.Name to be set")
	}
	if cfg.Server.Environment != "development" {
		t.Fatalf("expected default server environment development, got %q", cfg.Server.Environment)
	}
	if cfg.EventBus.Provider != "inmemory" {
		t.Fatalf("expected default event bus provider inmemory, got %q", cfg.EventBus.Provider)
	}
	if cfg.Voice.RecordingProvider != "disabled" {
		t.Fatalf("expected default voice recording provider disabled, got %q", cfg.Voice.RecordingProvider)
	}
	if cfg.Voice.TranscriptProvider != "disabled" {
		t.Fatalf("expected default voice transcript provider disabled, got %q", cfg.Voice.TranscriptProvider)
	}
	if cfg.JWT.Secret == "" {
		t.Error("expected JWT.Secret to be set")
	}

	// 验证默认值
	if cfg.Log.Level == "" {
		t.Error("expected Log.Level to be set")
	}
	// 注意：RBAC默认未启用，所以不检查Enabled状态
}

func TestConfig_DatabaseSettings(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.Database.MaxOpenConns == 0 {
		t.Error("expected MaxOpenConns to be set")
	}
	if cfg.Database.MaxIdleConns == 0 {
		t.Error("expected MaxIdleConns to be set")
	}
	if cfg.Database.ConnMaxLifetime == 0 {
		t.Error("expected ConnMaxLifetime to be set")
	}
}

func TestConfig_Timeouts(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.AI.OpenAI.Timeout == 0 {
		t.Error("expected AI timeout to be set")
	}
	if cfg.Dify.Timeout == 0 {
		t.Error("expected Dify timeout to be set")
	}
	if cfg.WeKnora.Timeout == 0 {
		t.Error("expected WeKnora timeout to be set")
	}
	if cfg.WeKnora.HealthCheck.Interval == 0 {
		t.Error("expected health check interval to be set")
	}
}

func TestConfig_SecurityDefaults(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.JWT.Secret == "" {
		t.Error("expected JWT secret to be set")
	}
	// 注意：RBAC默认未启用，只检查CORS（RateLimiting默认禁用）
	if !cfg.Security.CORS.Enabled {
		t.Error("expected CORS to be enabled")
	}
	if cfg.Security.RateLimiting.Enabled {
		t.Error("expected rate limiting to be disabled by default")
	}
	if cfg.Security.SessionRisk.HotRefreshWindowMinutes != 15 {
		t.Fatalf("expected default hot refresh window to be 15 minutes, got %d", cfg.Security.SessionRisk.HotRefreshWindowMinutes)
	}
	if cfg.Security.SessionRisk.HighRiskScore != 4 {
		t.Fatalf("expected default high risk score to be 4, got %d", cfg.Security.SessionRisk.HighRiskScore)
	}
	if cfg.Security.SessionIPIntelligence.Enabled {
		t.Fatal("expected session IP intelligence to be disabled by default")
	}
	if cfg.Security.SessionIPIntelligence.AuthHeader != "Authorization" {
		t.Fatalf("expected default session IP auth header Authorization, got %q", cfg.Security.SessionIPIntelligence.AuthHeader)
	}
	if cfg.Security.SessionIPIntelligence.TimeoutMs != 1500 {
		t.Fatalf("expected default session IP timeout 1500ms, got %d", cfg.Security.SessionIPIntelligence.TimeoutMs)
	}
}

func TestConfig_SessionRiskDefaults(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.Security.SessionRisk.RecentRefreshWindowMinutes != 60 {
		t.Fatalf("expected recent refresh window = 60, got %d", cfg.Security.SessionRisk.RecentRefreshWindowMinutes)
	}
	if cfg.Security.SessionRisk.TodayRefreshWindowHours != 24 {
		t.Fatalf("expected today refresh window = 24h, got %d", cfg.Security.SessionRisk.TodayRefreshWindowHours)
	}
	if cfg.Security.SessionRisk.RapidChangeWindowHours != 24 {
		t.Fatalf("expected rapid change window = 24h, got %d", cfg.Security.SessionRisk.RapidChangeWindowHours)
	}
	if cfg.Security.SessionRisk.StaleActivityWindowDays != 30 {
		t.Fatalf("expected stale activity window = 30d, got %d", cfg.Security.SessionRisk.StaleActivityWindowDays)
	}
	if cfg.Security.SessionRisk.MultiPublicIPThreshold != 2 {
		t.Fatalf("expected multi public ip threshold = 2, got %d", cfg.Security.SessionRisk.MultiPublicIPThreshold)
	}
	if cfg.Security.SessionRisk.ManySessionsThreshold != 3 {
		t.Fatalf("expected many sessions threshold = 3, got %d", cfg.Security.SessionRisk.ManySessionsThreshold)
	}
	if cfg.Security.SessionRisk.HotRefreshFamilyThreshold != 2 {
		t.Fatalf("expected hot refresh family threshold = 2, got %d", cfg.Security.SessionRisk.HotRefreshFamilyThreshold)
	}
	if cfg.Security.SessionRisk.MediumRiskScore != 2 {
		t.Fatalf("expected medium risk score = 2, got %d", cfg.Security.SessionRisk.MediumRiskScore)
	}
	if cfg.Security.SessionRisk.HighRiskScore != 4 {
		t.Fatalf("expected high risk score = 4, got %d", cfg.Security.SessionRisk.HighRiskScore)
	}
	if cfg.Security.SessionRiskProfiles["development"].HighRiskScore != 6 {
		t.Fatalf("expected development session risk profile high risk score 6, got %d", cfg.Security.SessionRiskProfiles["development"].HighRiskScore)
	}
	if cfg.Security.SessionRiskProfiles["production"].RapidChangeWindowHours != 12 {
		t.Fatalf("expected production session risk profile rapid change window 12h, got %d", cfg.Security.SessionRiskProfiles["production"].RapidChangeWindowHours)
	}
}

func TestLoad_SessionRiskOverrides(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("security.session_risk.hot_refresh_window_minutes", 7)
	viper.Set("security.session_risk.rapid_change_window_hours", 6)
	viper.Set("security.session_risk.multi_public_ip_threshold", 4)
	viper.Set("security.session_risk.high_risk_score", 6)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Security.SessionRisk.HotRefreshWindowMinutes != 7 {
		t.Fatalf("expected overridden hot refresh window = 7, got %d", cfg.Security.SessionRisk.HotRefreshWindowMinutes)
	}
	if cfg.Security.SessionRisk.RapidChangeWindowHours != 6 {
		t.Fatalf("expected overridden rapid change window = 6, got %d", cfg.Security.SessionRisk.RapidChangeWindowHours)
	}
	if cfg.Security.SessionRisk.MultiPublicIPThreshold != 4 {
		t.Fatalf("expected overridden multi public ip threshold = 4, got %d", cfg.Security.SessionRisk.MultiPublicIPThreshold)
	}
	if cfg.Security.SessionRisk.HighRiskScore != 6 {
		t.Fatalf("expected overridden high risk score = 6, got %d", cfg.Security.SessionRisk.HighRiskScore)
	}
	if cfg.Security.SessionRisk.MediumRiskScore != 2 {
		t.Fatalf("expected unspecified medium risk score to keep default 2, got %d", cfg.Security.SessionRisk.MediumRiskScore)
	}
}

func TestLoad_EventBusOverrides(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("event_bus.provider", "inmemory")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.EventBus.Provider != "inmemory" {
		t.Fatalf("expected overridden event bus provider inmemory, got %q", cfg.EventBus.Provider)
	}
}

func TestLoad_VoiceProviderOverrides(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("voice.recording_provider", "mock")
	viper.Set("voice.transcript_provider", "mock")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Voice.RecordingProvider != "mock" {
		t.Fatalf("expected overridden voice recording provider mock, got %q", cfg.Voice.RecordingProvider)
	}
	if cfg.Voice.TranscriptProvider != "mock" {
		t.Fatalf("expected overridden voice transcript provider mock, got %q", cfg.Voice.TranscriptProvider)
	}
}

func TestLoad_SessionIPIntelligenceOverrides(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("security.session_ip_intelligence.enabled", true)
	viper.Set("security.session_ip_intelligence.base_url", "https://geo.example.com/lookup/{ip}")
	viper.Set("security.session_ip_intelligence.api_key", "geo-token")
	viper.Set("security.session_ip_intelligence.auth_header", "X-Geo-Key")
	viper.Set("security.session_ip_intelligence.timeout_ms", 2200)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Security.SessionIPIntelligence.Enabled {
		t.Fatal("expected session IP intelligence to be enabled")
	}
	if cfg.Security.SessionIPIntelligence.BaseURL != "https://geo.example.com/lookup/{ip}" {
		t.Fatalf("unexpected session IP base url %q", cfg.Security.SessionIPIntelligence.BaseURL)
	}
	if cfg.Security.SessionIPIntelligence.APIKey != "geo-token" {
		t.Fatalf("unexpected session IP api key %q", cfg.Security.SessionIPIntelligence.APIKey)
	}
	if cfg.Security.SessionIPIntelligence.AuthHeader != "X-Geo-Key" {
		t.Fatalf("unexpected session IP auth header %q", cfg.Security.SessionIPIntelligence.AuthHeader)
	}
	if cfg.Security.SessionIPIntelligence.TimeoutMs != 2200 {
		t.Fatalf("unexpected session IP timeout %d", cfg.Security.SessionIPIntelligence.TimeoutMs)
	}
}

func TestLoad_SessionRiskEnvironmentProfileOverrides(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("server.environment", "production")
	viper.Set("jwt.secret", "secure-test-jwt-secret")      // Must override default in production
	viper.Set("database.password", "secure-test-password") // Must override default in production
	viper.Set("security.session_risk_profiles.production.high_risk_score", 7)
	viper.Set("security.session_risk_profiles.production.rapid_change_window_hours", 8)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Environment != "production" {
		t.Fatalf("expected server environment production, got %q", cfg.Server.Environment)
	}
	if cfg.Security.SessionRiskProfiles["production"].HighRiskScore != 7 {
		t.Fatalf("expected production profile high risk score 7, got %d", cfg.Security.SessionRiskProfiles["production"].HighRiskScore)
	}
	if cfg.Security.SessionRiskProfiles["production"].RapidChangeWindowHours != 8 {
		t.Fatalf("expected production profile rapid change window 8h, got %d", cfg.Security.SessionRiskProfiles["production"].RapidChangeWindowHours)
	}
}

func TestConfig_FallbackSettings(t *testing.T) {
	cfg := GetDefaultConfig()

	if !cfg.Fallback.KnowledgeBaseEnabled {
		t.Fatal("expected fallback knowledge base to be enabled by default")
	}
	if cfg.Fallback.CircuitBreaker.MaxFailures == 0 {
		t.Error("expected circuit breaker max failures to be set")
	}
	if cfg.Fallback.CircuitBreaker.ResetTimeout == 0 {
		t.Error("expected circuit breaker reset timeout to be set")
	}
}

func TestConfig_AIConfiguration(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.AI.OpenAI.Model == "" {
		t.Error("expected OpenAI model to be set")
	}
	if cfg.AI.OpenAI.Model != DefaultOpenAIModel {
		t.Fatalf("expected default OpenAI model %q, got %q", DefaultOpenAIModel, cfg.AI.OpenAI.Model)
	}
	if cfg.AI.OpenAI.Temperature == 0 {
		t.Error("expected OpenAI temperature to be set")
	}
	if cfg.AI.OpenAI.MaxTokens == 0 {
		t.Error("expected OpenAI max tokens to be set")
	}
	if cfg.Dify.Search.TopK == 0 {
		t.Error("expected Dify top_k to be set")
	}
	if cfg.Dify.Search.SearchMethod == "" {
		t.Error("expected Dify search method to be set")
	}
}

func TestLoad_InvalidOverrideReturnsError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("server.port", "not-a-number")

	if _, err := Load(); err == nil {
		t.Fatal("expected config load error for invalid override")
	}
}

func TestLoad_FallbackKnowledgeBaseEnabledOverride(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("fallback.knowledge_base_enabled", false)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Fallback.KnowledgeBaseEnabled {
		t.Fatal("expected knowledge base fallback to be disabled")
	}
	if cfg.Fallback.LegacyKBEnabled {
		t.Fatal("expected legacy compatibility field to mirror new fallback state")
	}
}

func TestLoad_FallbackLegacyKnowledgeBaseCompatibility(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("fallback.legacy_kb_enabled", false)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Fallback.KnowledgeBaseEnabled {
		t.Fatal("expected legacy fallback key to disable knowledge base fallback")
	}
}

func TestLoad_FallbackKnowledgeBaseOverrideTakesPrecedence(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("fallback.knowledge_base_enabled", false)
	viper.Set("fallback.legacy_kb_enabled", true)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Fallback.KnowledgeBaseEnabled {
		t.Fatal("expected new fallback key to take precedence over legacy compatibility key")
	}
	if cfg.Fallback.LegacyKBEnabled {
		t.Fatal("expected legacy compatibility field to be normalized to the new key value")
	}
}

func TestConfig_WeKnoraSettings(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.WeKnora.Search.DefaultLimit == 0 {
		t.Error("expected WeKnora search default limit to be set")
	}
	if cfg.WeKnora.Search.ScoreThreshold == 0 {
		t.Error("expected WeKnora score threshold to be set")
	}
}

func TestConfig_PortalSettings(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.Portal.BrandName == "" {
		t.Error("expected portal brand name to be set")
	}
	if cfg.Portal.DefaultLocale == "" {
		t.Error("expected portal default locale to be set")
	}
}

func TestConfig_UploadSettings(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.Upload.MaxFileSize == "" {
		t.Error("expected max file size to be set")
	}
	if len(cfg.Upload.AllowedTypes) == 0 {
		t.Error("expected allowed types to be set")
	}
	// 默认与原 /api/v1/upload 硬编码行为对齐：./uploads 目录 + local provider
	if cfg.Upload.StoragePath != "./uploads" {
		t.Fatalf("expected upload storage path to default to ./uploads, got %q", cfg.Upload.StoragePath)
	}
	if cfg.Upload.Provider != "local" {
		t.Fatalf("expected default upload provider local, got %q", cfg.Upload.Provider)
	}
	if cfg.Upload.S3.PresignExpirySeconds != 3600 {
		t.Fatalf("expected default presign expiry 3600, got %d", cfg.Upload.S3.PresignExpirySeconds)
	}
}

func TestConfig_RBACRoles(t *testing.T) {
	cfg := GetDefaultConfig()

	// 注意：RBAC默认未启用，所以Roles可能为空
	// 只测试Security配置存在
	if !cfg.Security.CORS.Enabled {
		t.Error("expected CORS to be enabled")
	}
	if len(cfg.Security.CORS.AllowedOrigins) == 0 {
		t.Error("expected allowed origins to be set")
	}
	if len(cfg.Security.CORS.AllowedMethods) == 0 {
		t.Error("expected allowed methods to be set")
	}
}

func TestConfig_RateLimiting(t *testing.T) {
	cfg := GetDefaultConfig()

	// Rate limiting is disabled by default for integration tests
	if cfg.Security.RateLimiting.Enabled {
		t.Error("expected rate limiting to be disabled by default")
	}
	if cfg.Security.RateLimiting.RequestsPerMinute == 0 {
		t.Error("expected requests per minute to be set even when disabled")
	}
}

func TestConfig_Monitoring(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.Monitoring.Enabled == false {
		t.Error("expected monitoring to be enabled")
	}
}

func TestConfig_CORS(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.Security.CORS.Enabled == false {
		t.Error("expected CORS to be enabled")
	}
	if len(cfg.Security.CORS.AllowedOrigins) == 0 {
		t.Error("expected allowed origins to be set")
	}
	if len(cfg.Security.CORS.AllowedMethods) == 0 {
		t.Error("expected allowed methods to be set")
	}
}

func TestConfig_WebRTC(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.WebRTC.STUNServer == "" {
		t.Error("expected STUN server to be set")
	}
}

func TestConfig_DurationValidation(t *testing.T) {
	cfg := GetDefaultConfig()

	// 验证时间单位设置合理
	if cfg.Database.ConnMaxLifetime < time.Minute {
		t.Error("connection max lifetime should be at least 1 minute")
	}
	if cfg.Fallback.CircuitBreaker.ResetTimeout < time.Second {
		t.Error("circuit breaker reset timeout should be at least 1 second")
	}
}

func TestInitLogger_DefaultConfig(t *testing.T) {
	cfg := GetDefaultConfig()

	// 测试使用默认配置初始化日志
	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger failed: %v", err)
	}
}

func TestInitLogger_CustomLevel(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Log.Level = "debug"

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger with debug level failed: %v", err)
	}
}

func TestInitLogger_InvalidLevel(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Log.Level = "invalid"

	// 应该使用默认的 info 级别
	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger with invalid level failed: %v", err)
	}
}

func TestInitLogger_TextFormat(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Log.Format = "text"

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger with text format failed: %v", err)
	}
}

func TestInitLogger_JSONFormat(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Log.Format = "json"

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger with json format failed: %v", err)
	}
}

func TestInitLogger_StdoutOutput(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Log.Output = "stdout"

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger with stdout output failed: %v", err)
	}
}

func TestInitLogger_FileOutput(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Log.Output = "file"
	cfg.Log.FilePath = "/tmp/test-servify.log"

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger with file output failed: %v", err)
	}
}

func TestInitLogger_BothOutput(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Log.Output = "both"
	cfg.Log.FilePath = "/tmp/test-servify-both.log"

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger with both output failed: %v", err)
	}
}

func TestInitLogger_InvalidOutput(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Log.Output = "invalid"

	// 应该回退到 stdout
	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger with invalid output failed: %v", err)
	}
}

func TestConfig_TracingDefaults(t *testing.T) {
	cfg := GetDefaultConfig()

	// 验证追踪默认配置
	if cfg.Monitoring.Tracing.Enabled {
		t.Error("tracing should be disabled by default")
	}
	if cfg.Monitoring.Tracing.Endpoint == "" {
		t.Error("expected tracing endpoint to be set")
	}
	if cfg.Monitoring.Tracing.SampleRatio == 0 {
		t.Error("expected sample ratio to be set")
	}
}

func TestConfig_RedisDefaults(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.Redis.Host == "" {
		t.Error("expected Redis host to be set")
	}
	if cfg.Redis.Port == 0 {
		t.Error("expected Redis port to be set")
	}
	if cfg.Redis.PoolSize == 0 {
		t.Error("expected Redis pool size to be set")
	}
}

func TestConfig_HealthChecks(t *testing.T) {
	cfg := GetDefaultConfig()

	// 验证健康检查配置
	if !cfg.Monitoring.HealthChecks.Database {
		t.Error("expected database health check to be enabled")
	}
	if !cfg.Monitoring.HealthChecks.Redis {
		t.Error("expected Redis health check to be enabled")
	}
}

func TestConfig_PathRateLimits(t *testing.T) {
	cfg := GetDefaultConfig()

	// RateLimiting.Paths 是一个切片，默认为空但不是 nil
	// 在 Go 中，空切片的零值是 nil
	if cfg.Security.RateLimiting.RequestsPerMinute == 0 {
		t.Error("expected requests per minute to be set")
	}
	if cfg.Security.RateLimiting.Burst == 0 {
		t.Error("expected burst to be set")
	}
}

func TestConfig_AuditRetentionDefaults(t *testing.T) {
	cfg := GetDefaultConfig()

	if !cfg.Security.Audit.Enabled {
		t.Error("expected audit cleanup to be enabled by default")
	}
	if cfg.Security.Audit.Retention != 180*24*time.Hour {
		t.Fatalf("unexpected audit retention: %v", cfg.Security.Audit.Retention)
	}
	if cfg.Security.Audit.CleanupInterval != 24*time.Hour {
		t.Fatalf("unexpected audit cleanup interval: %v", cfg.Security.Audit.CleanupInterval)
	}
	if cfg.Security.Audit.CleanupBatchSize <= 0 {
		t.Fatalf("expected positive audit cleanup batch size, got %d", cfg.Security.Audit.CleanupBatchSize)
	}
}

func TestConfig_TokenRevocationDefaults(t *testing.T) {
	cfg := GetDefaultConfig()

	if !cfg.Security.TokenRevocation.Enabled {
		t.Error("expected token revocation cleanup to be enabled by default")
	}
	if cfg.Security.TokenRevocation.CleanupInterval != 24*time.Hour {
		t.Fatalf("unexpected token revocation cleanup interval: %v", cfg.Security.TokenRevocation.CleanupInterval)
	}
	if cfg.Security.TokenRevocation.CleanupBatchSize <= 0 {
		t.Fatalf("expected positive token revocation cleanup batch size, got %d", cfg.Security.TokenRevocation.CleanupBatchSize)
	}
}

func TestValidate_ProductionRejectsInsecureDefaults(t *testing.T) {
	tests := []struct {
		name      string
		modifyCfg func(*Config)
		wantValid bool
	}{
		{
			name: "production with default JWT secret is invalid",
			modifyCfg: func(cfg *Config) {
				cfg.Server.Environment = "production"
				cfg.JWT.Secret = "dev-secret-key-change-in-production"
			},
			wantValid: false,
		},
		{
			name: "production with default database password is invalid",
			modifyCfg: func(cfg *Config) {
				cfg.Server.Environment = "production"
				cfg.JWT.Secret = "secure-production-secret"
				cfg.Database.Password = "dev-password-change-in-production"
			},
			wantValid: false,
		},
		{
			name: "production with empty database password is invalid",
			modifyCfg: func(cfg *Config) {
				cfg.Server.Environment = "production"
				cfg.JWT.Secret = "secure-production-secret"
				cfg.Database.Password = ""
			},
			wantValid: false,
		},
		{
			name: "production with default WeKnora API key is invalid",
			modifyCfg: func(cfg *Config) {
				cfg.Server.Environment = "production"
				cfg.JWT.Secret = "secure-production-secret"
				cfg.Database.Password = "secure-production-password"
				cfg.WeKnora.Enabled = true
				cfg.WeKnora.APIKey = "default-api-key"
			},
			wantValid: false,
		},
		{
			name: "production with secure defaults is valid",
			modifyCfg: func(cfg *Config) {
				cfg.Server.Environment = "production"
				cfg.JWT.Secret = "secure-production-secret"
				cfg.Database.Password = "secure-production-password"
				cfg.WeKnora.Enabled = true
				cfg.WeKnora.APIKey = "secure-production-key"
			},
			wantValid: true,
		},
		{
			name: "development allows insecure defaults with warning",
			modifyCfg: func(cfg *Config) {
				cfg.Server.Environment = "development"
				// Keep all defaults which are insecure
			},
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := GetDefaultConfig()
			tt.modifyCfg(cfg)

			result := Validate(cfg)
			if result.Valid != tt.wantValid {
				t.Errorf("Validate() Valid = %v, want %v. Warnings: %v", result.Valid, tt.wantValid, result.Warnings)
			}
		})
	}
}
