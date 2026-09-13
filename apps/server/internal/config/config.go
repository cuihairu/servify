package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

const DefaultOpenAIModel = "gpt-4.1-mini"

// InsecureJWTSecrets contains known insecure JWT secret values that must not be used in production.
var InsecureJWTSecrets = map[string]bool{
	"default-secret-key":                      true,
	"dev-secret-key-change-in-production":     true,
	"default-secret-key-change-in-production": true,
}

// InsecureDatabasePasswords contains known insecure database password values that must not be used in production.
var InsecureDatabasePasswords = map[string]bool{
	"":                                  true,
	"password":                          true,
	"changeme":                          true,
	"dev-password-change-in-production": true,
}

// InsecureWeKnoraAPIKeys contains known insecure WeKnora API key values.
var InsecureWeKnoraAPIKeys = map[string]bool{
	"default-api-key": true,
}

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	EventBus   EventBusConfig   `yaml:"event_bus"`
	Database   DatabaseConfig   `yaml:"database"`
	Redis      RedisConfig      `yaml:"redis"`
	WebRTC     WebRTCConfig     `yaml:"webrtc"`
	Voice      VoiceConfig      `yaml:"voice"`
	AI         AIConfig         `yaml:"ai"`
	Dify       DifyConfig       `yaml:"dify"`
	WeKnora    WeKnoraConfig    `yaml:"weknora"`
	Fallback   FallbackConfig   `yaml:"fallback"`
	JWT        JWTConfig        `yaml:"jwt"`
	Log        LogConfig        `yaml:"log"`
	Monitoring MonitoringConfig `yaml:"monitoring"`
	Security   SecurityConfig   `yaml:"security"`
	Portal     PortalConfig     `yaml:"portal"`
	Upload     UploadConfig     `yaml:"upload"`
	OIDC       OIDCConfig       `yaml:"oidc"`
	Embedding  EmbeddingConfig  `yaml:"embedding"`
	Knowledge  KnowledgeConfig  `yaml:"knowledge"`
	Email      EmailConfig      `yaml:"email"`
	Quality    QualityConfig    `yaml:"quality"`
	Routing    RoutingConfig    `yaml:"routing"`
	Automation AutomationConfig `yaml:"automation"`
}

type ServerConfig struct {
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	Environment string `yaml:"environment"`
	// PublicBaseURL 是对外可访问的基地址（如 https://support.example.com），
	// 用于拼装对外链接（CSAT 评分页等）；空则生成相对路径。
	PublicBaseURL string `yaml:"public_base_url"`
}

type EventBusConfig struct {
	Provider string `yaml:"provider"`
}

type DatabaseConfig struct {
	Host            string        `yaml:"host"`
	Port            int           `yaml:"port"`
	User            string        `yaml:"user"`
	Password        string        `yaml:"password"`
	Name            string        `yaml:"name"`
	MaxOpenConns    int           `yaml:"max_open_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
}

type RedisConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	Password     string `yaml:"password"`
	DB           int    `yaml:"db"`
	PoolSize     int    `yaml:"pool_size"`
	MinIdleConns int    `yaml:"min_idle_conns"`
}

type WebRTCConfig struct {
	STUNServer string `yaml:"stun_server"`
}

type VoiceConfig struct {
	RecordingProvider  string         `yaml:"recording_provider"`
	TranscriptProvider string         `yaml:"transcript_provider"`
	Twilio             TwilioConfig   `yaml:"twilio"`
	Deepgram           DeepgramConfig `yaml:"deepgram"`
}

type TwilioConfig struct {
	AccountSID string `yaml:"account_sid"`
	AuthToken  string `yaml:"auth_token"`
}

type DeepgramConfig struct {
	APIKey string `yaml:"api_key"`
}

type AIConfig struct {
	OpenAI OpenAIConfig `yaml:"openai"`
}

type OpenAIConfig struct {
	APIKey      string        `yaml:"api_key" json:"api_key,omitempty"`
	BaseURL     string        `yaml:"base_url" json:"base_url,omitempty"`
	Model       string        `yaml:"model" json:"model,omitempty"`
	Temperature float64       `yaml:"temperature" json:"temperature,omitempty"`
	MaxTokens   int           `yaml:"max_tokens" json:"max_tokens,omitempty"`
	Timeout     time.Duration `yaml:"timeout" json:"timeout,omitempty"`
}

type DifyConfig struct {
	Enabled   bool             `yaml:"enabled" json:"enabled,omitempty"`
	BaseURL   string           `yaml:"base_url" json:"base_url,omitempty"`
	APIKey    string           `yaml:"api_key" json:"api_key,omitempty"`
	DatasetID string           `yaml:"dataset_id" json:"dataset_id,omitempty"`
	Timeout   time.Duration    `yaml:"timeout" json:"timeout,omitempty"`
	Search    DifySearchConfig `yaml:"search" json:"search,omitempty"`
}

type DifySearchConfig struct {
	TopK            int     `yaml:"top_k" json:"top_k,omitempty"`
	ScoreThreshold  float64 `yaml:"score_threshold" json:"score_threshold,omitempty"`
	SearchMethod    string  `yaml:"search_method" json:"search_method,omitempty"`
	RerankingEnable bool    `yaml:"reranking_enable" json:"reranking_enable,omitempty"`
}

type WeKnoraConfig struct {
	Enabled         bool                `yaml:"enabled" json:"enabled,omitempty"`
	BaseURL         string              `yaml:"base_url" json:"base_url,omitempty"`
	APIKey          string              `yaml:"api_key" json:"api_key,omitempty"`
	TenantID        string              `yaml:"tenant_id" json:"tenant_id,omitempty"`
	KnowledgeBaseID string              `yaml:"knowledge_base_id" json:"knowledge_base_id,omitempty"`
	Timeout         time.Duration       `yaml:"timeout" json:"timeout,omitempty"`
	MaxRetries      int                 `yaml:"max_retries" json:"max_retries,omitempty"`
	Search          WeKnoraSearchConfig `yaml:"search" json:"search,omitempty"`
	HealthCheck     WeKnoraHealthConfig `yaml:"health_check" json:"health_check,omitempty"`
}

type WeKnoraSearchConfig struct {
	DefaultLimit   int     `yaml:"default_limit" json:"default_limit,omitempty"`
	ScoreThreshold float64 `yaml:"score_threshold" json:"score_threshold,omitempty"`
	Strategy       string  `yaml:"strategy" json:"strategy,omitempty"`
}

type WeKnoraHealthConfig struct {
	Interval time.Duration `yaml:"interval" json:"interval,omitempty"`
	Timeout  time.Duration `yaml:"timeout" json:"timeout,omitempty"`
}

type FallbackConfig struct {
	Enabled              bool                 `yaml:"enabled"`
	KnowledgeBaseEnabled bool                 `yaml:"knowledge_base_enabled"`
	LegacyKBEnabled      bool                 `yaml:"legacy_kb_enabled" json:"-"`
	CircuitBreaker       CircuitBreakerConfig `yaml:"circuit_breaker"`
}

type CircuitBreakerConfig struct {
	Enabled         bool          `yaml:"enabled"`
	MaxFailures     int           `yaml:"max_failures"`
	ResetTimeout    time.Duration `yaml:"reset_timeout"`
	HalfOpenMaxReqs int           `yaml:"half_open_max_requests"`
}

type JWTConfig struct {
	Secret           string        `yaml:"secret"`
	ExpiresIn        time.Duration `yaml:"expires_in"`
	RefreshExpiresIn time.Duration `yaml:"refresh_expires_in"`
}

type LogConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"` // json, text
	Output     string `yaml:"output"` // stdout, file, both
	FilePath   string `yaml:"file_path"`
	MaxSize    int    `yaml:"max_size"`    // MB
	MaxAge     int    `yaml:"max_age"`     // days
	MaxBackups int    `yaml:"max_backups"` // number of backup files
	Compress   bool   `yaml:"compress"`    // compress backup files
}

type MonitoringConfig struct {
	Enabled      bool                     `yaml:"enabled"`
	MetricsPath  string                   `yaml:"metrics_path"`
	Performance  PerformanceMonitorConfig `yaml:"performance"`
	HealthChecks HealthChecksConfig       `yaml:"health_checks"`
	Tracing      TracingConfig            `yaml:"tracing"`
}

type PerformanceMonitorConfig struct {
	SlowQueryThreshold   time.Duration `yaml:"slow_query_threshold"`
	EnableRequestLogging bool          `yaml:"enable_request_logging"`
}

type HealthChecksConfig struct {
	Database          bool `yaml:"database"`
	Redis             bool `yaml:"redis"`
	KnowledgeProvider bool `yaml:"knowledge_provider"`
	WeKnora           bool `yaml:"weknora"`
	OpenAI            bool `yaml:"openai"`
}

func (c HealthChecksConfig) KnowledgeProviderEnabled() bool {
	return c.KnowledgeProvider || c.WeKnora
}

// TracingConfig OpenTelemetry 追踪配置
type TracingConfig struct {
	Enabled     bool    `yaml:"enabled"`
	Endpoint    string  `yaml:"endpoint"`     // OTLP gRPC 端点，例如 http://otel-collector:4317 或 0.0.0.0:4317
	Insecure    bool    `yaml:"insecure"`     // 是否使用明文（本地/开发）
	SampleRatio float64 `yaml:"sample_ratio"` // 采样率 0.0~1.0
	ServiceName string  `yaml:"service_name"` // 自定义服务名，缺省使用 "servify"
}

type SecurityConfig struct {
	CORS                  CORSConfig                         `yaml:"cors"`
	RateLimiting          RateLimitingConfig                 `yaml:"rate_limiting"`
	RBAC                  RBACConfig                         `yaml:"rbac"`
	Audit                 AuditConfig                        `yaml:"audit"`
	TokenRevocation       TokenRevocationConfig              `yaml:"token_revocation"`
	SessionRisk           SessionRiskPolicyConfig            `yaml:"session_risk"`
	SessionRiskProfiles   map[string]SessionRiskPolicyConfig `yaml:"session_risk_profiles"`
	SessionIPIntelligence SessionIPIntelligenceConfig        `yaml:"session_ip_intelligence"`
	TwoFactor             TwoFactorConfig                    `yaml:"two_factor"`
}

// TwoFactorConfig 是 TOTP 两步验证配置。
type TwoFactorConfig struct {
	// Enabled 是总开关（kill-switch）：关闭时登录不进入挑战步（已启用用户
	// 降级为单因子直登），setup/enable 端点拒绝；disable 不受限
	Enabled bool `yaml:"enabled" json:"enabled,omitempty"`
	// Issuer 是 TOTP otpauth URI 中的发行方标识（认证器 App 里显示的站点名）
	Issuer string `yaml:"issuer" json:"issuer,omitempty"`
	// ChallengeTTL 是登录挑战 JWT 的有效期（首轮密码验证通过后到完成第二因子的窗口）
	ChallengeTTL time.Duration `yaml:"challenge_ttl" json:"challenge_ttl,omitempty"`
}

type CORSConfig struct {
	Enabled        bool     `yaml:"enabled"`
	AllowedOrigins []string `yaml:"allowed_origins"`
	AllowedMethods []string `yaml:"allowed_methods"`
	AllowedHeaders []string `yaml:"allowed_headers"`
}

type RBACConfig struct {
	Enabled bool                `yaml:"enabled"`
	Roles   map[string][]string `yaml:"roles"`
}

type AuditConfig struct {
	Enabled          bool          `yaml:"enabled"`
	Retention        time.Duration `yaml:"retention"`
	CleanupInterval  time.Duration `yaml:"cleanup_interval"`
	CleanupBatchSize int           `yaml:"cleanup_batch_size"`
}

type TokenRevocationConfig struct {
	Enabled          bool          `yaml:"enabled"`
	CleanupInterval  time.Duration `yaml:"cleanup_interval"`
	CleanupBatchSize int           `yaml:"cleanup_batch_size"`
}

type SessionRiskPolicyConfig struct {
	HotRefreshWindowMinutes    int `yaml:"hot_refresh_window_minutes" json:"hot_refresh_window_minutes,omitempty"`
	RecentRefreshWindowMinutes int `yaml:"recent_refresh_window_minutes" json:"recent_refresh_window_minutes,omitempty"`
	TodayRefreshWindowHours    int `yaml:"today_refresh_window_hours" json:"today_refresh_window_hours,omitempty"`
	RapidChangeWindowHours     int `yaml:"rapid_change_window_hours" json:"rapid_change_window_hours,omitempty"`
	StaleActivityWindowDays    int `yaml:"stale_activity_window_days" json:"stale_activity_window_days,omitempty"`
	MultiPublicIPThreshold     int `yaml:"multi_public_ip_threshold" json:"multi_public_ip_threshold,omitempty"`
	ManySessionsThreshold      int `yaml:"many_sessions_threshold" json:"many_sessions_threshold,omitempty"`
	HotRefreshFamilyThreshold  int `yaml:"hot_refresh_family_threshold" json:"hot_refresh_family_threshold,omitempty"`
	MediumRiskScore            int `yaml:"medium_risk_score" json:"medium_risk_score,omitempty"`
	HighRiskScore              int `yaml:"high_risk_score" json:"high_risk_score,omitempty"`
}

type SessionIPIntelligenceConfig struct {
	Enabled    bool   `yaml:"enabled"`
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	AuthHeader string `yaml:"auth_header"`
	TimeoutMs  int    `yaml:"timeout_ms"`
}

type RateLimitingConfig struct {
	Enabled           bool                  `yaml:"enabled"`
	RequestsPerMinute int                   `yaml:"requests_per_minute"`
	Burst             int                   `yaml:"burst"`
	Paths             []PathRateLimitConfig `yaml:"paths"`
	// Optional: use specific header value as rate-limit key (e.g., X-Forwarded-For, X-API-Key)
	KeyHeader string `yaml:"key_header"`
	// Optional: bypass limit for these IPs (matches client IP)
	WhitelistIPs []string `yaml:"whitelist_ips"`
	// Optional: bypass limit for these header key values (when KeyHeader set)
	WhitelistKeys []string `yaml:"whitelist_keys"`
}

// PathRateLimitConfig allows overriding rate limits for specific path prefixes.
// The first matching Prefix will be used.
type PathRateLimitConfig struct {
	Enabled           bool   `yaml:"enabled"`
	Prefix            string `yaml:"prefix"`
	RequestsPerMinute int    `yaml:"requests_per_minute"`
	Burst             int    `yaml:"burst"`
}

// PortalConfig controls public portal branding and i18n defaults for static pages.
type PortalConfig struct {
	BrandName      string   `yaml:"brand_name" json:"brand_name,omitempty"`
	LogoURL        string   `yaml:"logo_url" json:"logo_url,omitempty"`
	PrimaryColor   string   `yaml:"primary_color" json:"primary_color,omitempty"`
	SecondaryColor string   `yaml:"secondary_color" json:"secondary_color,omitempty"`
	DefaultLocale  string   `yaml:"default_locale" json:"default_locale,omitempty"` // e.g. zh-CN, en-US
	Locales        []string `yaml:"locales" json:"locales,omitempty"`               // allowed locales
	SupportEmail   string   `yaml:"support_email" json:"support_email,omitempty"`
}
type UploadConfig struct {
	Enabled      bool           `yaml:"enabled"`
	Provider     string         `yaml:"provider"` // "local"(default) | "s3"
	MaxFileSize  string         `yaml:"max_file_size"`
	AllowedTypes []string       `yaml:"allowed_types"`
	StoragePath  string         `yaml:"storage_path"`
	AutoProcess  bool           `yaml:"auto_process"`
	AutoIndex    bool           `yaml:"auto_index"`
	S3           S3UploadConfig `yaml:"s3"`
}

// S3UploadConfig 参数化 S3 兼容对象存储后端（AWS S3、MinIO 等）。
// access_key_id/secret_access_key 同时为空时走默认凭据链（env/IAM role）。
type S3UploadConfig struct {
	Region               string `yaml:"region" json:"region,omitempty"`
	Bucket               string `yaml:"bucket" json:"bucket,omitempty"`
	Endpoint             string `yaml:"endpoint" json:"endpoint,omitempty"`
	AccessKeyID          string `yaml:"access_key_id" json:"access_key_id,omitempty"`
	SecretAccessKey      string `yaml:"secret_access_key" json:"secret_access_key,omitempty"`
	ForcePathStyle       bool   `yaml:"force_path_style" json:"force_path_style,omitempty"`
	PresignExpirySeconds int    `yaml:"presign_expiry_seconds" json:"presign_expiry_seconds,omitempty"`
	PublicBaseURL        string `yaml:"public_base_url" json:"public_base_url,omitempty"`
}

// OIDCConfig 配置管理端 OIDC SSO 登录（授权码 + PKCE）。
// 启用后 /api/v1/auth/oidc/* 提供 Start/Callback/Status；
// RoleMapping 把 IdP 角色/组映射到本地角色（仅 admin/agent 生效）。
type OIDCConfig struct {
	Enabled      bool     `yaml:"enabled" json:"enabled,omitempty"`
	Issuer       string   `yaml:"issuer" json:"issuer,omitempty"`
	ClientID     string   `yaml:"client_id" json:"client_id,omitempty"`
	ClientSecret string   `yaml:"client_secret" json:"-"`
	RedirectURL  string   `yaml:"redirect_url" json:"redirect_url,omitempty"`
	Scopes       []string `yaml:"scopes" json:"scopes,omitempty"`
	// FrontendBaseURL 是登录成功后 302 交接 token 的前端基地址（fragment 传递，不进服务器日志）
	FrontendBaseURL string            `yaml:"frontend_base_url" json:"frontend_base_url,omitempty"`
	RoleClaims      []string          `yaml:"role_claims" json:"role_claims,omitempty"`
	RoleMapping     map[string]string `yaml:"role_mapping" json:"role_mapping,omitempty"`
	DefaultRole     string            `yaml:"default_role" json:"default_role,omitempty"`
	// AutoProvision 为 true 时允许首次登录的 IdP 用户自动建号；默认拒绝
	AutoProvision  bool     `yaml:"auto_provision" json:"auto_provision,omitempty"`
	AllowedDomains []string `yaml:"allowed_domains" json:"allowed_domains,omitempty"`
}

// EmbeddingConfig 是文本嵌入服务配置
type EmbeddingConfig struct {
	Provider   string                `yaml:"provider" json:"provider,omitempty"`
	OpenAI     OpenAIEmbedConfig     `yaml:"openai" json:"openai,omitempty"`
	TEI        TEIEmbedConfig        `yaml:"tei" json:"tei,omitempty"`
	Xinference XinferenceEmbedConfig `yaml:"xinference" json:"xinference,omitempty"`
}

// OpenAIEmbedConfig 是 OpenAI Embedding 配置
type OpenAIEmbedConfig struct {
	APIKey  string `yaml:"api_key" json:"api_key,omitempty"`
	BaseURL string `yaml:"base_url" json:"base_url,omitempty"`
	Model   string `yaml:"model" json:"model,omitempty"`
}

// TEIEmbedConfig 是 TEI Embedding 配置
type TEIEmbedConfig struct {
	BaseURL string `yaml:"base_url" json:"base_url,omitempty"`
	Model   string `yaml:"model" json:"model,omitempty"`
}

// XinferenceEmbedConfig 是 Xinference Embedding 配置
type XinferenceEmbedConfig struct {
	BaseURL  string `yaml:"base_url" json:"base_url,omitempty"`
	ModelUID string `yaml:"model_uid" json:"model_uid,omitempty"`
}

// KnowledgeConfig 是知识库配置
type KnowledgeConfig struct {
	Provider string         `yaml:"provider" json:"provider,omitempty"`
	Pgvector PgvectorConfig `yaml:"pgvector" json:"pgvector,omitempty"`
}

// PgvectorConfig 是 pgvector 知识库配置
type PgvectorConfig struct {
	Search   SearchConfig   `yaml:"search" json:"search,omitempty"`
	Indexing IndexingConfig `yaml:"indexing" json:"indexing,omitempty"`
}

// SearchConfig 是向量搜索配置
type SearchConfig struct {
	TopK      int     `yaml:"top_k" json:"top_k,omitempty"`
	Threshold float64 `yaml:"threshold" json:"threshold,omitempty"`
	Strategy  string  `yaml:"strategy" json:"strategy,omitempty"`
}

// IndexingConfig 是索引配置
type IndexingConfig struct {
	ChunkSize    int `yaml:"chunk_size" json:"chunk_size,omitempty"`
	ChunkOverlap int `yaml:"chunk_overlap" json:"chunk_overlap,omitempty"`
}

// EmailConfig 配置单邮箱 IMAP 收件渠道（定时轮询）与 SMTP 出站。
// 零表零迁移：入站邮件按发件人地址归并进 conversation 模块；带附件邮件跳过。
// 出站 Send 本期已实现但无生产触发点（坐席回复转 email 留待后续）。
type EmailConfig struct {
	Enabled  bool   `yaml:"enabled" json:"enabled,omitempty"`
	Host     string `yaml:"host" json:"host,omitempty"`
	Port     int    `yaml:"port" json:"port,omitempty"`
	Username string `yaml:"username" json:"username,omitempty"`
	// Password 是 IMAP 登录口令（JSON 序列化不落日志）
	Password string `yaml:"password" json:"-"`
	// Mailbox 是轮询的 IMAP 文件夹（默认 INBOX）
	Mailbox    string `yaml:"mailbox" json:"mailbox,omitempty"`
	UseTLS     bool   `yaml:"use_tls" json:"use_tls,omitempty"`
	SkipVerify bool   `yaml:"skip_verify" json:"skip_verify,omitempty"`
	// PollIntervalSeconds 是轮询间隔；过小会触发部分服务商限流
	PollIntervalSeconds int             `yaml:"poll_interval_seconds" json:"poll_interval_seconds,omitempty"`
	SMTP                EmailSMTPConfig `yaml:"smtp" json:"smtp,omitempty"`
}

// EmailSMTPConfig 是出站 SMTP 配置（本期 Send 通路就绪，生产触发点留待后续版本）
type EmailSMTPConfig struct {
	Host     string `yaml:"host" json:"host,omitempty"`
	Port     int    `yaml:"port" json:"port,omitempty"`
	Username string `yaml:"username" json:"username,omitempty"`
	Password string `yaml:"password" json:"-"`
	// From 是出站邮件的发件人地址；空则回退 email.username
	From        string `yaml:"from" json:"from,omitempty"`
	UseSTARTTLS bool   `yaml:"use_starttls" json:"use_starttls,omitempty"`
	SkipVerify  bool   `yaml:"skip_verify" json:"skip_verify,omitempty"`
}

// QualityConfig 配置质检全链路：定时扫描已结束会话 → 规则质检 → LLM 打分（可选）。
// 禁用或 LLM 未启用时降级为 rules-only。LLM 成本由 sample_rate × batch_size ×
// max_attempts × retry_backoff 四级上限控制。
type QualityConfig struct {
	Enabled             bool `yaml:"enabled" json:"enabled,omitempty"`
	ScanIntervalSeconds int  `yaml:"scan_interval_seconds" json:"scan_interval_seconds,omitempty"`
	// BatchSize 是单轮扫描处理的会话上限
	BatchSize int `yaml:"batch_size" json:"batch_size,omitempty"`
	// SampleRate 是抽样百分比 0-100；100 = 全量
	SampleRate   float64 `yaml:"sample_rate" json:"sample_rate,omitempty"`
	LookbackDays int     `yaml:"lookback_days" json:"lookback_days,omitempty"`
	// MinMessages 是参与质检的最少消息数，低于此值直接 skipped
	MinMessages         int                `yaml:"min_messages" json:"min_messages,omitempty"`
	MaxAttempts         int                `yaml:"max_attempts" json:"max_attempts,omitempty"`
	RetryBackoffSeconds int                `yaml:"retry_backoff_seconds" json:"retry_backoff_seconds,omitempty"`
	Rules               QualityRulesConfig `yaml:"rules" json:"rules,omitempty"`
	LLM                 QualityLLMConfig   `yaml:"llm" json:"llm,omitempty"`
}

// QualityRulesConfig 是规则质检配置（违禁词走 config，不上表）
type QualityRulesConfig struct {
	BannedWords                 []string `yaml:"banned_words" json:"banned_words,omitempty"`
	ResponseTimeoutSeconds      int      `yaml:"response_timeout_seconds" json:"response_timeout_seconds,omitempty"`
	FirstResponseTimeoutSeconds int      `yaml:"first_response_timeout_seconds" json:"first_response_timeout_seconds,omitempty"`
}

// QualityLLMConfig 是 LLM 打分配置；Enabled 且有可用 provider 时才打分
type QualityLLMConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled,omitempty"`
	// Model 为空时回退 ai.openai.model
	Model          string  `yaml:"model" json:"model,omitempty"`
	Temperature    float64 `yaml:"temperature" json:"temperature,omitempty"`
	MaxInputChars  int     `yaml:"max_input_chars" json:"max_input_chars,omitempty"`
	MaxTurnChars   int     `yaml:"max_turn_chars" json:"max_turn_chars,omitempty"`
	TimeoutSeconds int     `yaml:"timeout_seconds" json:"timeout_seconds,omitempty"`
}

// RoutingConfig 是坐席分派/等待队列配置
type RoutingConfig struct {
	// Enabled 控制等待队列自动分派 worker；关闭时仍可手动触发分派端点
	Enabled bool `yaml:"enabled" json:"enabled,omitempty"`
	// DispatchIntervalSeconds 是等待队列自动分派 worker 的轮询间隔
	DispatchIntervalSeconds int `yaml:"dispatch_interval_seconds" json:"dispatch_interval_seconds,omitempty"`
	// ClaimLeaseSeconds 是 claim-then-process 的认领租约时长，过期自动复活
	ClaimLeaseSeconds int `yaml:"claim_lease_seconds" json:"claim_lease_seconds,omitempty"`
	// DispatchBatchSize 是单轮分派的最大认领条数
	DispatchBatchSize int `yaml:"dispatch_batch_size" json:"dispatch_batch_size,omitempty"`
}

// AutomationConfig 是自动化触发器配置：delay 动作入队到期执行单，
// 由 timer worker 按扫描间隔周期执行（多实例下乐观抢占保证恰好一次）。
type AutomationConfig struct {
	// TimerScanIntervalSeconds 是到期执行单的扫描间隔
	TimerScanIntervalSeconds int `yaml:"timer_scan_interval_seconds" json:"timer_scan_interval_seconds,omitempty"`
	// TimerBatchSize 是单轮扫描处理的执行单上限
	TimerBatchSize int `yaml:"timer_batch_size" json:"timer_batch_size,omitempty"`
}

func Load() (*Config, error) {
	// Start with default config to ensure all fields have valid defaults
	config := GetDefaultConfig()
	// Viper unmarshalling uses mapstructure tags by default; explicitly decode via our `yaml` tags
	// to keep config files consistent (e.g. `stun_server`, `max_open_conns`, etc.).
	if err := viper.Unmarshal(config, func(dc *mapstructure.DecoderConfig) {
		dc.TagName = "yaml"
	}); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	normalizeConfig(config)
	result := Validate(config)
	if !result.Valid {
		return nil, fmt.Errorf("config validation failed: %v", result.Warnings)
	}
	// Warnings are available via InsecureDefaults() or LoadWithResult()
	// Caller is responsible for logging if needed
	return config, nil
}

// LoadWithResult loads config and returns validation result for structured warning handling
func LoadWithResult() (*Config, ValidateResult, error) {
	config := GetDefaultConfig()
	if err := viper.Unmarshal(config, func(dc *mapstructure.DecoderConfig) {
		dc.TagName = "yaml"
	}); err != nil {
		return nil, ValidateResult{}, fmt.Errorf("unmarshal config: %w", err)
	}
	normalizeConfig(config)
	result := Validate(config)
	if !result.Valid {
		return config, result, fmt.Errorf("config validation failed: %v", result.Warnings)
	}
	return config, result, nil
}

// InsecureDefaults returns a list of security warnings for insecure default values
func InsecureDefaults(cfg *Config) []string {
	if cfg == nil {
		return []string{"config is nil; security defaults may be unsafe"}
	}

	var warnings []string

	if InsecureJWTSecrets[cfg.JWT.Secret] {
		warnings = append(warnings, "jwt.secret is using a default value")
	}

	if InsecureWeKnoraAPIKeys[cfg.WeKnora.APIKey] && cfg.WeKnora.Enabled {
		warnings = append(warnings, "weknora.api_key is using the default value")
	}

	if InsecureDatabasePasswords[cfg.Database.Password] {
		warnings = append(warnings, "database.password is empty or using a default value")
	}

	if strings.EqualFold(strings.TrimSpace(cfg.Upload.Provider), "s3") {
		if strings.TrimSpace(cfg.Upload.S3.Bucket) == "" {
			warnings = append(warnings, "upload.provider is s3 but upload.s3.bucket is empty")
		}
		if strings.TrimSpace(cfg.Upload.S3.Region) == "" {
			warnings = append(warnings, "upload.provider is s3 but upload.s3.region is empty")
		}
	}

	if cfg.OIDC.Enabled {
		if strings.TrimSpace(cfg.OIDC.Issuer) == "" {
			warnings = append(warnings, "oidc.enabled is true but oidc.issuer is empty")
		}
		if strings.TrimSpace(cfg.OIDC.ClientID) == "" || strings.TrimSpace(cfg.OIDC.ClientSecret) == "" {
			warnings = append(warnings, "oidc.enabled is true but oidc.client_id/oidc.client_secret is empty")
		}
		if strings.TrimSpace(cfg.OIDC.RedirectURL) == "" {
			warnings = append(warnings, "oidc.enabled is true but oidc.redirect_url is empty")
		}
		if strings.TrimSpace(cfg.OIDC.FrontendBaseURL) == "" {
			warnings = append(warnings, "oidc.enabled is true but oidc.frontend_base_url is empty")
		}
		if strings.HasPrefix(strings.TrimSpace(cfg.OIDC.Issuer), "http://") {
			warnings = append(warnings, "oidc.issuer uses http; production issuers must be https")
		}
		if cfg.OIDC.AutoProvision && len(cfg.OIDC.AllowedDomains) == 0 {
			warnings = append(warnings, "oidc.auto_provision is true without oidc.allowed_domains; any verified IdP identity can create an account")
		}
	}

	if cfg.Email.Enabled {
		if strings.TrimSpace(cfg.Email.Host) == "" {
			warnings = append(warnings, "email.enabled is true but email.host is empty")
		}
		if strings.TrimSpace(cfg.Email.Username) == "" || strings.TrimSpace(cfg.Email.Password) == "" {
			warnings = append(warnings, "email.enabled is true but email.username/email.password is empty")
		}
		if !cfg.Email.UseTLS {
			warnings = append(warnings, "email.use_tls is false; production mailboxes must use IMAPS (port 993)")
		}
		if cfg.Email.SkipVerify {
			warnings = append(warnings, "email.skip_verify is true; IMAP TLS certificates will not be verified")
		}
		if strings.TrimSpace(cfg.Email.SMTP.Host) == "" {
			warnings = append(warnings, "email.enabled is true but email.smtp.host is empty; outbound email cannot be sent")
		}
	}

	return warnings
}

// ValidateResult contains the result of config validation
type ValidateResult struct {
	Warnings []string
	Valid    bool
}

// Validate checks for insecure default values that should not be used in production
// Returns ValidateResult with warnings and validity. Caller is responsible for logging.
func Validate(cfg *Config) ValidateResult {
	warnings := InsecureDefaults(cfg)
	if len(warnings) == 0 {
		return ValidateResult{Valid: true}
	}

	// In production, any insecure default is invalid
	if cfg.Server.Environment == "production" {
		return ValidateResult{
			Warnings: warnings,
			Valid:    false,
		}
	}

	// In development, mark as valid but with warnings
	return ValidateResult{
		Warnings: warnings,
		Valid:    true,
	}
}

func normalizeConfig(cfg *Config) {
	expandEnvPlaceholders(reflect.ValueOf(cfg))

	knowledgeBaseConfigured := viper.IsSet("fallback.knowledge_base_enabled")
	legacyConfigured := viper.IsSet("fallback.legacy_kb_enabled")

	switch {
	case knowledgeBaseConfigured:
		cfg.Fallback.LegacyKBEnabled = cfg.Fallback.KnowledgeBaseEnabled
	case legacyConfigured:
		cfg.Fallback.KnowledgeBaseEnabled = cfg.Fallback.LegacyKBEnabled
	default:
		cfg.Fallback.LegacyKBEnabled = cfg.Fallback.KnowledgeBaseEnabled
	}
}

func expandEnvPlaceholders(value reflect.Value) {
	if !value.IsValid() {
		return
	}

	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return
		}
		expandEnvPlaceholders(value.Elem())
	case reflect.Interface:
		if value.IsNil() {
			return
		}
		expandEnvPlaceholders(value.Elem())
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			expandEnvPlaceholders(value.Field(i))
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			expandEnvPlaceholders(value.Index(i))
		}
	case reflect.Map:
		iter := value.MapRange()
		for iter.Next() {
			entry := iter.Value()
			updated := reflect.New(entry.Type()).Elem()
			updated.Set(entry)
			expandEnvPlaceholders(updated)
			value.SetMapIndex(iter.Key(), updated)
		}
	case reflect.String:
		if value.CanSet() {
			value.SetString(os.ExpandEnv(value.String()))
		}
	}
}

// GetDefaultConfig 返回默认配置
func GetDefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:        "0.0.0.0",
			Port:        8080,
			Environment: "development",
		},
		EventBus: EventBusConfig{
			Provider: "inmemory",
		},
		Database: DatabaseConfig{
			Host:            "localhost",
			Port:            5432,
			User:            "postgres",
			Password:        "dev-password-change-in-production",
			Name:            "servify",
			MaxOpenConns:    100,
			MaxIdleConns:    10,
			ConnMaxLifetime: 3600 * time.Second,
		},
		Redis: RedisConfig{
			Host:         "localhost",
			Port:         6379,
			Password:     "",
			DB:           0,
			PoolSize:     10,
			MinIdleConns: 5,
		},
		WebRTC: WebRTCConfig{
			STUNServer: "stun:stun.l.google.com:19302",
		},
		Voice: VoiceConfig{
			RecordingProvider:  "disabled",
			TranscriptProvider: "disabled",
		},
		AI: AIConfig{
			OpenAI: OpenAIConfig{
				BaseURL:     "https://api.openai.com/v1",
				Model:       DefaultOpenAIModel,
				Temperature: 0.7,
				MaxTokens:   1000,
				Timeout:     30 * time.Second,
			},
		},
		Dify: DifyConfig{
			Enabled:   false,
			BaseURL:   "http://localhost:5001/v1",
			DatasetID: "",
			Timeout:   30 * time.Second,
			Search: DifySearchConfig{
				TopK:            5,
				ScoreThreshold:  0.7,
				SearchMethod:    "semantic_search",
				RerankingEnable: false,
			},
		},
		WeKnora: WeKnoraConfig{
			Enabled:         false,
			BaseURL:         "http://localhost:9000",
			APIKey:          "default-api-key",
			TenantID:        "default-tenant",
			KnowledgeBaseID: "default-kb",
			Timeout:         30 * time.Second,
			MaxRetries:      3,
			Search: WeKnoraSearchConfig{
				DefaultLimit:   5,
				ScoreThreshold: 0.7,
				Strategy:       "hybrid",
			},
			HealthCheck: WeKnoraHealthConfig{
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
			},
		},
		Fallback: FallbackConfig{
			Enabled:              true,
			KnowledgeBaseEnabled: true,
			LegacyKBEnabled:      true,
			CircuitBreaker: CircuitBreakerConfig{
				Enabled:         true,
				MaxFailures:     5,
				ResetTimeout:    60 * time.Second,
				HalfOpenMaxReqs: 3,
			},
		},
		JWT: JWTConfig{
			Secret:           "dev-secret-key-change-in-production",
			ExpiresIn:        24 * time.Hour,
			RefreshExpiresIn: 7 * 24 * time.Hour,
		},
		Log: LogConfig{
			Level:      "info",
			Format:     "json",
			Output:     "both",
			FilePath:   "./logs/servify.log",
			MaxSize:    100,
			MaxAge:     7,
			MaxBackups: 3,
			Compress:   true,
		},
		Monitoring: MonitoringConfig{
			Enabled:     true,
			MetricsPath: "/metrics",
			Performance: PerformanceMonitorConfig{
				SlowQueryThreshold:   1 * time.Second,
				EnableRequestLogging: true,
			},
			HealthChecks: HealthChecksConfig{
				Database:          true,
				Redis:             true,
				KnowledgeProvider: true,
				WeKnora:           true,
				OpenAI:            false,
			},
			Tracing: TracingConfig{
				Enabled:     false,
				Endpoint:    "http://localhost:4317",
				Insecure:    true,
				SampleRatio: 0.1,
				ServiceName: "servify",
			},
		},
		Security: SecurityConfig{
			CORS: CORSConfig{
				Enabled:        true,
				AllowedOrigins: []string{"*"},
				AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
				AllowedHeaders: []string{"*"},
			},
			RateLimiting: RateLimitingConfig{
				Enabled:           false,
				RequestsPerMinute: 300,
				Burst:             50,
			},
			TwoFactor: TwoFactorConfig{
				// 两步验证默认关闭（企业客户按需开启）；挑战窗口 5 分钟
				Enabled:      false,
				Issuer:       "Servify",
				ChallengeTTL: 5 * time.Minute,
			},
			Audit: AuditConfig{
				Enabled:          true,
				Retention:        180 * 24 * time.Hour,
				CleanupInterval:  24 * time.Hour,
				CleanupBatchSize: 500,
			},
			TokenRevocation: TokenRevocationConfig{
				Enabled:          true,
				CleanupInterval:  24 * time.Hour,
				CleanupBatchSize: 500,
			},
			SessionRisk: SessionRiskPolicyConfig{
				HotRefreshWindowMinutes:    15,
				RecentRefreshWindowMinutes: 60,
				TodayRefreshWindowHours:    24,
				RapidChangeWindowHours:     24,
				StaleActivityWindowDays:    30,
				MultiPublicIPThreshold:     2,
				ManySessionsThreshold:      3,
				HotRefreshFamilyThreshold:  2,
				MediumRiskScore:            2,
				HighRiskScore:              4,
			},
			SessionRiskProfiles: map[string]SessionRiskPolicyConfig{
				"development": {
					HighRiskScore: 6,
				},
				"staging": {
					HighRiskScore:          5,
					RapidChangeWindowHours: 12,
				},
				"production": {
					HotRefreshWindowMinutes:    10,
					RecentRefreshWindowMinutes: 30,
					RapidChangeWindowHours:     12,
					StaleActivityWindowDays:    14,
					HighRiskScore:              4,
				},
			},
			SessionIPIntelligence: SessionIPIntelligenceConfig{
				Enabled:    false,
				AuthHeader: "Authorization",
				TimeoutMs:  1500,
			},
		},
		Portal: PortalConfig{
			BrandName:      "Servify",
			LogoURL:        "",
			PrimaryColor:   "#4299e1",
			SecondaryColor: "#764ba2",
			DefaultLocale:  "zh-CN",
			Locales:        []string{"zh-CN", "en-US"},
			SupportEmail:   "",
		},
		Upload: UploadConfig{
			Enabled:  true,
			Provider: "local",
			// 与此前 /api/v1/upload 硬编码的 32MB 上限保持一致；收紧留给部署显式配置
			MaxFileSize: "32MB",
			// 上传附件白名单（原 handler 硬编码集），与知识文档类型取并集语义
			AllowedTypes: []string{
				".jpg", ".jpeg", ".png", ".gif", ".webp",
				".pdf", ".doc", ".docx", ".xls", ".xlsx",
				".txt", ".csv", ".md", ".zip", ".mp3", ".mp4",
			},
			StoragePath: "./uploads",
			AutoProcess: true,
			AutoIndex:   true,
			S3: S3UploadConfig{
				PresignExpirySeconds: 3600,
			},
		},
		OIDC: OIDCConfig{
			// 管理端 SSO 默认关闭；启用需完整配置 issuer/client/redirect，
			// 生产环境 issuer 必须 https（见 InsecureDefaults 与 oidc.NewFromConfig）
			Enabled:     false,
			DefaultRole: "agent",
			RoleClaims:  []string{"roles", "groups"},
		},
		Embedding: EmbeddingConfig{
			Provider: "openai",
			OpenAI: OpenAIEmbedConfig{
				BaseURL: "https://api.openai.com/v1",
				Model:   "text-embedding-3-small",
			},
		},
		Knowledge: KnowledgeConfig{
			Provider: "pgvector",
			Pgvector: PgvectorConfig{
				Search: SearchConfig{
					TopK:      5,
					Threshold: 0.7,
					Strategy:  "semantic",
				},
				Indexing: IndexingConfig{
					ChunkSize:    1000,
					ChunkOverlap: 200,
				},
			},
		},
		Email: EmailConfig{
			// 单邮箱 IMAP 渠道默认关闭；启用需 host/username/password + smtp.host，
			// 生产环境必须 use_tls（见 InsecureDefaults 与 email 模块连接逻辑）
			Enabled:             false,
			Port:                993,
			Mailbox:             "INBOX",
			UseTLS:              true,
			PollIntervalSeconds: 60,
			SMTP: EmailSMTPConfig{
				Port:        587,
				UseSTARTTLS: true,
			},
		},
		Quality: QualityConfig{
			// 质检默认关闭；LLM 打分独立开关（无 key 时 rules-only）
			Enabled:             false,
			ScanIntervalSeconds: 300,
			BatchSize:           20,
			SampleRate:          100,
			LookbackDays:        7,
			MinMessages:         3,
			MaxAttempts:         3,
			RetryBackoffSeconds: 600,
			Rules: QualityRulesConfig{
				ResponseTimeoutSeconds:      120,
				FirstResponseTimeoutSeconds: 60,
			},
			LLM: QualityLLMConfig{
				Temperature:    0.1,
				MaxInputChars:  12000,
				MaxTurnChars:   500,
				TimeoutSeconds: 30,
			},
		},
		Routing: RoutingConfig{
			// 等待队列自动分派默认关闭；开启后按间隔轮询分派
			Enabled:                 false,
			DispatchIntervalSeconds: 30,
			ClaimLeaseSeconds:       120,
			DispatchBatchSize:       10,
		},
		Automation: AutomationConfig{
			TimerScanIntervalSeconds: 30,
			TimerBatchSize:           50,
		},
	}
}
