package config

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

const DefaultOpenAIModel = "gpt-4.1-mini"

// DefaultAnthropicModel provider 侧模型兜底与配置默认值共用；历史默认
// claude-3-haiku-20240307 已停更，切到当前主力 Sonnet 系。
const DefaultAnthropicModel = "claude-sonnet-5"

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
	RagFlow    RagFlowConfig    `yaml:"ragflow"`
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
	Push       PushConfig       `yaml:"push"`
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
	// STUNServer 是单值形态，保留向后兼容；stun_servers 列表非空时优先生效
	// （docs/TURN_DEPLOYMENT.md 第 5 节配置面口径）。
	STUNServer  string               `yaml:"stun_server"`
	STUNServers []string             `yaml:"stun_servers"`
	TURN        TURNCredentialConfig `yaml:"turn"`
}

// TURNCredentialConfig 对接 coturn use-auth-secret 时间限凭据模式：
// secret 只存服务端，短时凭据运行时生成并经信令下发；URL 为空即禁用。
type TURNCredentialConfig struct {
	URL              string        `yaml:"url"`
	Realm            string        `yaml:"realm"`
	StaticAuthSecret string        `yaml:"static_auth_secret"`
	TTL              time.Duration `yaml:"ttl"`
}

type VoiceConfig struct {
	RecordingProvider  string         `yaml:"recording_provider"`
	TranscriptProvider string         `yaml:"transcript_provider"`
	PSTN               PSTNConfig     `yaml:"pstn"`
	Twilio             TwilioConfig   `yaml:"twilio"`
	Deepgram           DeepgramConfig `yaml:"deepgram"`
}

// PSTNConfig controls the hosted-vendor PSTN webhook ingress
// (platform/twiliovoice). The auth token is shared with voice.twilio.
type PSTNConfig struct {
	// Provider selects the PSTN ingress: "disabled" (no public webhook route)
	// or "twilio".
	Provider string `yaml:"provider"`
	// ValidateSignature rejects webhooks whose X-Twilio-Signature does not
	// verify; only disable for local debugging.
	ValidateSignature bool `yaml:"validate_signature"`
}

type TwilioConfig struct {
	AccountSID string `yaml:"account_sid"`
	AuthToken  string `yaml:"auth_token"`
}

type DeepgramConfig struct {
	APIKey string `yaml:"api_key"`
}

// AIConfig LLM 供应商配置：Provider 全局唯一决定走哪家实现（openai | anthropic，
// 空 = openai），OpenAI/Anthropic 各自携带该供应商的参数面。model/temperature/
// max_tokens/timeout 会在编排层写入每次 LLM 调用，零值由 provider 侧默认兜底。
// 作用域覆盖（租户/工作区）当前仅覆盖 openai 族；anthropic 仅全局配置。
type AIConfig struct {
	Provider  string          `yaml:"provider" json:"provider,omitempty"`
	OpenAI    OpenAIConfig    `yaml:"openai" json:"openai,omitempty"`
	Anthropic AnthropicConfig `yaml:"anthropic" json:"anthropic,omitempty"`
	Handoff   HandoffConfig   `yaml:"handoff" json:"handoff,omitempty"`
	// 语音链路（Phase 2 实时翻译，docs/realtime-translation-design.md §2/§3.1）：
	// provider 空 = 未启用（合法形态，装配层跳过接线）。放 ai.* 是因为
	// 翻译语音是 LLM 同源的 AI 出站面（§4.2），与 voice.*（通话录音/转写
	// 持久化，另一条特性线）互不共用。
	ASR ASRConfig `yaml:"asr" json:"asr,omitempty"`
	TTS TTSConfig `yaml:"tts" json:"tts,omitempty"`
}

// HandoffConfig 首答置信门："答不上来时平滑转人工"的建议开关。只产出
// next_action=handoff 元数据（不改写答案、不执行转接），阈值默认 0.65
// 恰落在零命中 confidence=0.6 与有命中 confidence≥0.7 之间。
type HandoffConfig struct {
	Enabled             bool    `yaml:"enabled" json:"enabled"`
	ConfidenceThreshold float64 `yaml:"confidence_threshold" json:"confidence_threshold"`
}

// AnthropicConfig Anthropic（Claude 系列）接入参数。
type AnthropicConfig struct {
	APIKey      string        `yaml:"api_key" json:"api_key,omitempty"`
	BaseURL     string        `yaml:"base_url" json:"base_url,omitempty"`
	Model       string        `yaml:"model" json:"model,omitempty"`
	Temperature float64       `yaml:"temperature" json:"temperature,omitempty"`
	MaxTokens   int           `yaml:"max_tokens" json:"max_tokens,omitempty"`
	Timeout     time.Duration `yaml:"timeout" json:"timeout,omitempty"`
}

type OpenAIConfig struct {
	APIKey      string        `yaml:"api_key" json:"api_key,omitempty"`
	BaseURL     string        `yaml:"base_url" json:"base_url,omitempty"`
	Model       string        `yaml:"model" json:"model,omitempty"`
	Temperature float64       `yaml:"temperature" json:"temperature,omitempty"`
	MaxTokens   int           `yaml:"max_tokens" json:"max_tokens,omitempty"`
	Timeout     time.Duration `yaml:"timeout" json:"timeout,omitempty"`
}

// ASRConfig 流式语音识别接入参数（Phase 2 语音实时翻译刀一契约面，
// platform/asr + platform/asr/factory）。Provider 空 = 未启用；托管流式
// provider（Deepgram/火山/阿里等，§3.1 选型）随语音管线刀进 factory switch。
type ASRConfig struct {
	Provider string        `yaml:"provider" json:"provider,omitempty"`
	APIKey   string        `yaml:"api_key" json:"api_key,omitempty"`
	BaseURL  string        `yaml:"base_url" json:"base_url,omitempty"`
	Language string        `yaml:"language" json:"language,omitempty"`
	Timeout  time.Duration `yaml:"timeout" json:"timeout,omitempty"`
}

// TTSConfig 语音合成接入参数（Phase 2 逐句整段合成口径，流式 chunked 并入
// Phase 3；platform/tts + platform/tts/factory）。Voice/Format 空由 provider
// 侧默认兜底。
type TTSConfig struct {
	Provider string        `yaml:"provider" json:"provider,omitempty"`
	APIKey   string        `yaml:"api_key" json:"api_key,omitempty"`
	BaseURL  string        `yaml:"base_url" json:"base_url,omitempty"`
	Voice    string        `yaml:"voice" json:"voice,omitempty"`
	Format   string        `yaml:"format" json:"format,omitempty"`
	Timeout  time.Duration `yaml:"timeout" json:"timeout,omitempty"`
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

// RagFlowConfig 接入 RAGFlow（InfiniFlow 开源 RAG 引擎）知识库，API 面锚定 v0.27.x。
type RagFlowConfig struct {
	Enabled   bool                `yaml:"enabled" json:"enabled,omitempty"`
	BaseURL   string              `yaml:"base_url" json:"base_url,omitempty"`
	APIKey    string              `yaml:"api_key" json:"api_key,omitempty"`
	DatasetID string              `yaml:"dataset_id" json:"dataset_id,omitempty"`
	Timeout   time.Duration       `yaml:"timeout" json:"timeout,omitempty"`
	Search    RagFlowSearchConfig `yaml:"search" json:"search,omitempty"`
}

type RagFlowSearchConfig struct {
	TopK           int     `yaml:"top_k" json:"top_k,omitempty"`
	ScoreThreshold float64 `yaml:"score_threshold" json:"score_threshold,omitempty"`
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
	// P2-5 第一刀：运行时暴露面基线。默认全部关闭以保持既有部署行为不变，
	// 生产模板经 security.headers 节显式开启。
	Headers                 SecurityHeadersConfig `yaml:"headers"`
	MaxBodyBytes            int64                 `yaml:"max_body_bytes"`
	WebsocketAllowedOrigins []string              `yaml:"websocket_allowed_origins"`
	// GuestToken 控制访客 WS 握手 token 校验（§10 #2 / 设计文档 D6）。
	// 默认关闭以保持既有部署行为不变；开启后 /api/v1/ws 必须携带
	// /api/v1/guest/session 签发的短期 token。
	GuestToken GuestTokenConfig `yaml:"guest_token"`
}

// GuestTokenConfig 是访客 token 校验开关与签发参数。
type GuestTokenConfig struct {
	Required bool `yaml:"required"`
	// TTL 是签发有效期；非正值在装配层归一到默认 24h（不作为告警面）。
	TTL time.Duration `yaml:"ttl"`
}

// SecurityHeadersConfig 控制统一安全响应头。HSTS 默认关闭：TLS 通常在
// 反向代理终结，由代理注入；仅当服务直连 TLS 时开启。
type SecurityHeadersConfig struct {
	Enabled        bool   `yaml:"enabled"`
	HSTSEnabled    bool   `yaml:"hsts_enabled"`
	HSTSMaxAge     int    `yaml:"hsts_max_age_seconds"`
	FrameOptions   string `yaml:"frame_options"`
	ReferrerPolicy string `yaml:"referrer_policy"`
	// ContentSecurityPolicy 为空时不发送 CSP 头（SPA/文档站按需配置）
	ContentSecurityPolicy string `yaml:"content_security_policy"`
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
	// LoginEnforcement 控制登录风险执行档位（P2-5 第二刀）：
	//   "" / off — 不执行（默认，保持既有行为）
	//   step_up  — 高风险来源强制第二因子（已绑定 TOTP 走挑战步，未绑定拒绝）
	//   block    — 高风险来源直接拒绝
	// 风险判定依赖 security.session_ip_intelligence 的情报标签：内建启发式
	// 分类（public/private/loopback/unknown）不算高风险，接入外部情报源后
	// 出现的其他标签（hosting/proxy/tor/…）视为高风险。
	LoginEnforcement string `yaml:"login_enforcement" json:"login_enforcement,omitempty"`
	// RefreshReusePolicy 控制 refresh token 重放（reuse）处置档位（P2-5 第三刀）：
	//   "" / off           — 旧 token 重放仍被拒绝但不吊销会话（默认，既有行为）
	//   revoke_family      — 检测到已轮换 token 重放时吊销整个会话（家族），
	//                        该家族内包括最新 token 在内的所有 refresh 一律失效，
	//                        迫使重新登录
	// 一个 session 即一个家族：登录后所有 refresh 轮换共享同一 session 行。
	RefreshReusePolicy string `yaml:"refresh_reuse_policy" json:"refresh_reuse_policy,omitempty"`
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

// PushConfig 配置移动端推送下发（FCM Android / APNs iOS）。
// 消费 push_tokens 注册行（迁移 000012），零新表；enabled=false 时零组件不订阅事件。
// 真机凭证联调依赖 FCM 服务账号 / APNs 密钥（P1-1）；HTTP 传输与编排面已就绪。
type PushConfig struct {
	Enabled bool           `yaml:"enabled" json:"enabled,omitempty"`
	FCM     PushFCMConfig  `yaml:"fcm" json:"fcm,omitempty"`
	APNs    PushAPNsConfig `yaml:"apns" json:"apns,omitempty"`
}

// PushFCMConfig 是 Android 通道（FCM HTTP v1）。CredentialsJSON 为 Firebase
// 服务账号 JSON 全文（client_email/private_key 必备，project_id 由此解析）。
type PushFCMConfig struct {
	CredentialsJSON string `yaml:"credentials_json" json:"-"`
}

// PushAPNsConfig 是 iOS 通道（APNs token-based）。PrivateKey 为 .p8 文件全文（PEM）。
type PushAPNsConfig struct {
	KeyID    string `yaml:"key_id" json:"key_id,omitempty"`
	TeamID   string `yaml:"team_id" json:"team_id,omitempty"`
	BundleID string `yaml:"bundle_id" json:"bundle_id,omitempty"`
	// PrivateKey 是 APNs 签名私钥（ES256，敏感面不进日志/JSON）
	PrivateKey string `yaml:"private_key" json:"-"`
	// Sandbox 走沙箱网关（api.sandbox.push.apple.com）
	Sandbox bool `yaml:"sandbox" json:"sandbox,omitempty"`
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

	// P3-3：in-memory 事件总线是默认值，production/staging 不允许随默认配置
	// 误接入（事件不持久、重启即丢）；显式配置 redis 才能进入对外环境。
	eventBusProvider := strings.ToLower(strings.TrimSpace(cfg.EventBus.Provider))
	if eventBusProvider == "" || eventBusProvider == "inmemory" {
		warnings = append(warnings, "event_bus.provider is 'inmemory' (default); production and staging must use 'redis' for durable events")
	}

	// ai.provider 是全局 LLM 出站选型，非法值在装配层（llm factory）必然启动
	// 失败；这里前置到配置层再拦一次，让 production/staging 在加载期就拿到
	// 明确的报错而不是等到装配期。
	switch aiProvider := strings.ToLower(strings.TrimSpace(cfg.AI.Provider)); aiProvider {
	case "", "openai", "anthropic":
	default:
		warnings = append(warnings, fmt.Sprintf("ai.provider must be 'openai' or 'anthropic' (got %q)", aiProvider))
	}

	// 置信门开着就必须给 (0,1] 的阈值：0 或负值会让所有首答都建议转人工，
	// >1 则永远不触发——两种都不是运维想要的语义，production/staging 在
	// 加载期直接拦下。
	if cfg.AI.Handoff.Enabled && (cfg.AI.Handoff.ConfidenceThreshold <= 0 || cfg.AI.Handoff.ConfidenceThreshold > 1) {
		warnings = append(warnings, fmt.Sprintf("ai.handoff.confidence_threshold must be in (0,1] when handoff is enabled (got %v)", cfg.AI.Handoff.ConfidenceThreshold))
	}

	if strings.EqualFold(strings.TrimSpace(cfg.Upload.Provider), "s3") {
		if strings.TrimSpace(cfg.Upload.S3.Bucket) == "" {
			warnings = append(warnings, "upload.provider is s3 but upload.s3.bucket is empty")
		}
		if strings.TrimSpace(cfg.Upload.S3.Region) == "" {
			warnings = append(warnings, "upload.provider is s3 but upload.s3.region is empty")
		}
	}

	// TURN 时间限凭据模式（docs/TURN_DEPLOYMENT.md）：启用即必须完整。
	// secret 为空的典型原因是模板 ${ENV} 未注入——coturn 会拒绝所有
	// allocation，远程协助在对称 NAT 下静默失去中继能力。
	if strings.TrimSpace(cfg.WebRTC.TURN.URL) != "" {
		if strings.TrimSpace(cfg.WebRTC.TURN.Realm) == "" {
			warnings = append(warnings, "webrtc.turn.url is set but webrtc.turn.realm is empty")
		}
		if strings.TrimSpace(cfg.WebRTC.TURN.StaticAuthSecret) == "" {
			warnings = append(warnings, "webrtc.turn.url is set but webrtc.turn.static_auth_secret is empty")
		}
		if cfg.WebRTC.TURN.TTL <= 0 {
			warnings = append(warnings, "webrtc.turn.url is set but webrtc.turn.ttl is not positive")
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
	if cfg.Push.Enabled {
		fcmReady := pushFCMCredentialsValid(cfg.Push.FCM.CredentialsJSON) == ""
		apns := cfg.Push.APNs
		apnsReady := strings.TrimSpace(apns.PrivateKey) != ""
		if !fcmReady && !apnsReady {
			warnings = append(warnings, "push.enabled is true but push.fcm/push.apns credentials are both missing; dispatch subscribes nothing")
		}
		if apnsReady && (strings.TrimSpace(apns.KeyID) == "" || strings.TrimSpace(apns.TeamID) == "" || strings.TrimSpace(apns.BundleID) == "") {
			warnings = append(warnings, "push.apns is partially configured; push.apns.key_id/team_id/bundle_id are all required with private_key")
		}
	}

	// 访客 token（§10 #2 / D6）与 jwt.secret 同信任域自签自验：required
	// 开启而 secret 仍是默认值时，任何人都可自签合法访客 token 握手 WS。
	if cfg.Security.GuestToken.Required && InsecureJWTSecrets[cfg.JWT.Secret] {
		warnings = append(warnings, "security.guest_token.required is true but jwt.secret is using a default value; guest tokens are forgeable")
	}

	return warnings
}

// pushFCMCredentialsValid 校验 FCM 服务账号 JSON：可解析且 client_email/
// private_key 齐备；返回首个问题（空串 = 合法）。
func pushFCMCredentialsValid(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "credentials_json is empty"
	}
	var svc struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		ProjectID   string `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(raw), &svc); err != nil {
		return "credentials_json is not valid JSON: " + err.Error()
	}
	if strings.TrimSpace(svc.ClientEmail) == "" {
		return "credentials_json missing client_email"
	}
	if strings.TrimSpace(svc.PrivateKey) == "" {
		return "credentials_json missing private_key"
	}
	return ""
}

// ValidateResult contains the result of config validation
type ValidateResult struct {
	Warnings []string
	Valid    bool
}

// canonicalEnvironment 把 environment 的常见别名归一到规范值，
// 防止 "prod" 这类拼写绕过 production 严格校验（P2-2）。
func canonicalEnvironment(env string) string {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "prod", "production":
		return "production"
	case "stage", "staging":
		return "staging"
	case "dev", "development":
		return "development"
	default:
		return strings.TrimSpace(env)
	}
}

// Validate checks for insecure default values that should not be used in production
// Returns ValidateResult with warnings and validity. Caller is responsible for logging.
func Validate(cfg *Config) ValidateResult {
	warnings := InsecureDefaults(cfg)
	if len(warnings) == 0 {
		return ValidateResult{Valid: true}
	}

	// production 与 staging（预生产）对已知不安全默认值零容忍：
	// 占位凭证 / dev 默认值不允许随配置进入任何对外环境（P2-2）。
	if cfg.Server.Environment == "production" || cfg.Server.Environment == "staging" {
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
	// environment 可能经 ${ENV} 注入，展开后再归一别名。
	cfg.Server.Environment = canonicalEnvironment(cfg.Server.Environment)

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
			TURN: TURNCredentialConfig{
				// 默认禁用（URL 为空）；TTL 兜底与 iceturn.DefaultTTL 对齐。
				TTL: 5 * time.Minute,
			},
		},
		Voice: VoiceConfig{
			RecordingProvider:  "disabled",
			TranscriptProvider: "disabled",
			PSTN: PSTNConfig{
				Provider:          "disabled",
				ValidateSignature: true,
			},
		},
		AI: AIConfig{
			OpenAI: OpenAIConfig{
				BaseURL:     "https://api.openai.com/v1",
				Model:       DefaultOpenAIModel,
				Temperature: 0.7,
				MaxTokens:   1000,
				Timeout:     30 * time.Second,
			},
			Anthropic: AnthropicConfig{
				BaseURL:     "https://api.anthropic.com/v1",
				Model:       DefaultAnthropicModel,
				Temperature: 0.7,
				MaxTokens:   1000,
				Timeout:     30 * time.Second,
			},
			Handoff: HandoffConfig{
				Enabled:             true,
				ConfidenceThreshold: 0.65,
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
		RagFlow: RagFlowConfig{
			Enabled:   false,
			BaseURL:   "http://localhost:9380",
			DatasetID: "",
			Timeout:   30 * time.Second,
			Search: RagFlowSearchConfig{
				TopK:           10,
				ScoreThreshold: 0.2,
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
			// 访客 token 校验默认关闭（兼容既有部署）；TTL 默认 24h。
			GuestToken: GuestTokenConfig{
				Required: false,
				TTL:      24 * time.Hour,
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
			// 默认不启用任何自建知识源；pgvector 需在配置文件显式声明
			// provider: "pgvector"（需要 pg+pgvector 扩展与 embedding 服务）。
			Provider: "",
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
		}, Push: PushConfig{
			// 推送下发默认关闭；启用需 fcm.credentials_json 或 apns 凭证组，
			// production 下缺凭证告警即拒绝启动（见 InsecureDefaults）
			Enabled: false,
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
