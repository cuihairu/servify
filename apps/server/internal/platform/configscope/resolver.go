package configscope

import (
	"context"
	"strings"

	"servify/apps/server/internal/config"
)

type PortalConfigProvider interface {
	LoadPortalConfig(ctx context.Context) (config.PortalConfig, bool, error)
}

type OpenAIConfigProvider interface {
	LoadOpenAIConfig(ctx context.Context) (config.OpenAIConfig, bool, error)
}

type DifyConfigProvider interface {
	LoadDifyConfig(ctx context.Context) (config.DifyConfig, bool, error)
}

type WeKnoraConfigProvider interface {
	LoadWeKnoraConfig(ctx context.Context) (config.WeKnoraConfig, bool, error)
}

type RagFlowConfigProvider interface {
	LoadRagFlowConfig(ctx context.Context) (config.RagFlowConfig, bool, error)
}

type SessionRiskConfigProvider interface {
	LoadSessionRiskConfig(ctx context.Context) (config.SessionRiskPolicyConfig, bool, error)
}

type Resolver struct {
	system               *config.Config
	tenantPortal         PortalConfigProvider
	workspacePortal      PortalConfigProvider
	tenantOpenAI         OpenAIConfigProvider
	workspaceOpenAI      OpenAIConfigProvider
	tenantDify           DifyConfigProvider
	workspaceDify        DifyConfigProvider
	tenantWeKnora        WeKnoraConfigProvider
	workspaceWeKnora     WeKnoraConfigProvider
	tenantRagFlow        RagFlowConfigProvider
	workspaceRagFlow     RagFlowConfigProvider
	tenantSessionRisk    SessionRiskConfigProvider
	workspaceSessionRisk SessionRiskConfigProvider
}

func NewResolver(cfg *config.Config, opts ...Option) *Resolver {
	r := &Resolver{system: cfg}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

type Option func(*Resolver)

func WithTenantPortalProvider(provider PortalConfigProvider) Option {
	return func(r *Resolver) {
		r.tenantPortal = provider
	}
}

func WithWorkspacePortalProvider(provider PortalConfigProvider) Option {
	return func(r *Resolver) {
		r.workspacePortal = provider
	}
}

func WithTenantOpenAIProvider(provider OpenAIConfigProvider) Option {
	return func(r *Resolver) {
		r.tenantOpenAI = provider
	}
}

func WithWorkspaceOpenAIProvider(provider OpenAIConfigProvider) Option {
	return func(r *Resolver) {
		r.workspaceOpenAI = provider
	}
}

func WithTenantDifyProvider(provider DifyConfigProvider) Option {
	return func(r *Resolver) {
		r.tenantDify = provider
	}
}

func WithWorkspaceDifyProvider(provider DifyConfigProvider) Option {
	return func(r *Resolver) {
		r.workspaceDify = provider
	}
}

func WithTenantWeKnoraProvider(provider WeKnoraConfigProvider) Option {
	return func(r *Resolver) {
		r.tenantWeKnora = provider
	}
}

func WithWorkspaceWeKnoraProvider(provider WeKnoraConfigProvider) Option {
	return func(r *Resolver) {
		r.workspaceWeKnora = provider
	}
}

func WithTenantRagFlowProvider(provider RagFlowConfigProvider) Option {
	return func(r *Resolver) {
		r.tenantRagFlow = provider
	}
}

func WithWorkspaceRagFlowProvider(provider RagFlowConfigProvider) Option {
	return func(r *Resolver) {
		r.workspaceRagFlow = provider
	}
}

func WithTenantSessionRiskProvider(provider SessionRiskConfigProvider) Option {
	return func(r *Resolver) {
		r.tenantSessionRisk = provider
	}
}

func WithWorkspaceSessionRiskProvider(provider SessionRiskConfigProvider) Option {
	return func(r *Resolver) {
		r.workspaceSessionRisk = provider
	}
}

func (r *Resolver) ResolvePortal(ctx context.Context, runtime *config.PortalConfig) config.PortalConfig {
	var resolved config.PortalConfig
	if r != nil && r.system != nil {
		resolved = r.system.Portal
	}

	if r != nil && r.tenantPortal != nil {
		if next, ok, err := r.tenantPortal.LoadPortalConfig(ctx); err == nil && ok {
			resolved = mergePortalConfig(resolved, next)
		}
	}
	if r != nil && r.workspacePortal != nil {
		if next, ok, err := r.workspacePortal.LoadPortalConfig(ctx); err == nil && ok {
			resolved = mergePortalConfig(resolved, next)
		}
	}
	if runtime != nil {
		resolved = mergePortalConfig(resolved, *runtime)
	}

	return applyPortalDefaults(resolved)
}

func mergePortalConfig(base config.PortalConfig, overlay config.PortalConfig) config.PortalConfig {
	if strings.TrimSpace(overlay.BrandName) != "" {
		base.BrandName = overlay.BrandName
	}
	if strings.TrimSpace(overlay.LogoURL) != "" {
		base.LogoURL = overlay.LogoURL
	}
	if strings.TrimSpace(overlay.PrimaryColor) != "" {
		base.PrimaryColor = overlay.PrimaryColor
	}
	if strings.TrimSpace(overlay.SecondaryColor) != "" {
		base.SecondaryColor = overlay.SecondaryColor
	}
	if strings.TrimSpace(overlay.DefaultLocale) != "" {
		base.DefaultLocale = overlay.DefaultLocale
	}
	if len(overlay.Locales) > 0 {
		base.Locales = append([]string(nil), overlay.Locales...)
	}
	if strings.TrimSpace(overlay.SupportEmail) != "" {
		base.SupportEmail = overlay.SupportEmail
	}
	return base
}

func applyPortalDefaults(p config.PortalConfig) config.PortalConfig {
	if strings.TrimSpace(p.BrandName) == "" {
		p.BrandName = "Servify"
	}
	if strings.TrimSpace(p.PrimaryColor) == "" {
		p.PrimaryColor = "#4299e1"
	}
	if strings.TrimSpace(p.SecondaryColor) == "" {
		p.SecondaryColor = "#764ba2"
	}
	if strings.TrimSpace(p.DefaultLocale) == "" {
		p.DefaultLocale = "zh-CN"
	}
	if len(p.Locales) == 0 {
		p.Locales = []string{"zh-CN", "en-US"}
	}
	return p
}

func (r *Resolver) ResolveOpenAI(ctx context.Context, runtime *config.OpenAIConfig) config.OpenAIConfig {
	var resolved config.OpenAIConfig
	if r != nil && r.system != nil {
		resolved = r.system.AI.OpenAI
	}
	if r != nil && r.tenantOpenAI != nil {
		if next, ok, err := r.tenantOpenAI.LoadOpenAIConfig(ctx); err == nil && ok {
			resolved = mergeOpenAIConfig(resolved, next)
		}
	}
	if r != nil && r.workspaceOpenAI != nil {
		if next, ok, err := r.workspaceOpenAI.LoadOpenAIConfig(ctx); err == nil && ok {
			resolved = mergeOpenAIConfig(resolved, next)
		}
	}
	if runtime != nil {
		resolved = mergeOpenAIConfig(resolved, *runtime)
	}
	return resolved
}

func (r *Resolver) ResolveDify(ctx context.Context, runtime *config.DifyConfig) config.DifyConfig {
	var resolved config.DifyConfig
	if r != nil && r.system != nil {
		resolved = r.system.Dify
	}
	if r != nil && r.tenantDify != nil {
		if next, ok, err := r.tenantDify.LoadDifyConfig(ctx); err == nil && ok {
			resolved = mergeDifyConfig(resolved, next)
		}
	}
	if r != nil && r.workspaceDify != nil {
		if next, ok, err := r.workspaceDify.LoadDifyConfig(ctx); err == nil && ok {
			resolved = mergeDifyConfig(resolved, next)
		}
	}
	if runtime != nil {
		resolved = mergeDifyConfig(resolved, *runtime)
	}
	return resolved
}

func (r *Resolver) ResolveWeKnora(ctx context.Context, runtime *config.WeKnoraConfig) config.WeKnoraConfig {
	var resolved config.WeKnoraConfig
	if r != nil && r.system != nil {
		resolved = r.system.WeKnora
	}
	if r != nil && r.tenantWeKnora != nil {
		if next, ok, err := r.tenantWeKnora.LoadWeKnoraConfig(ctx); err == nil && ok {
			resolved = mergeWeKnoraConfig(resolved, next)
		}
	}
	if r != nil && r.workspaceWeKnora != nil {
		if next, ok, err := r.workspaceWeKnora.LoadWeKnoraConfig(ctx); err == nil && ok {
			resolved = mergeWeKnoraConfig(resolved, next)
		}
	}
	if runtime != nil {
		resolved = mergeWeKnoraConfig(resolved, *runtime)
	}
	return resolved
}

func (r *Resolver) ResolveRagFlow(ctx context.Context, runtime *config.RagFlowConfig) config.RagFlowConfig {
	var resolved config.RagFlowConfig
	if r != nil && r.system != nil {
		resolved = r.system.RagFlow
	}
	if r != nil && r.tenantRagFlow != nil {
		if next, ok, err := r.tenantRagFlow.LoadRagFlowConfig(ctx); err == nil && ok {
			resolved = mergeRagFlowConfig(resolved, next)
		}
	}
	if r != nil && r.workspaceRagFlow != nil {
		if next, ok, err := r.workspaceRagFlow.LoadRagFlowConfig(ctx); err == nil && ok {
			resolved = mergeRagFlowConfig(resolved, next)
		}
	}
	if runtime != nil {
		resolved = mergeRagFlowConfig(resolved, *runtime)
	}
	return resolved
}

func (r *Resolver) ResolveSessionRisk(ctx context.Context, runtime *config.SessionRiskPolicyConfig) config.SessionRiskPolicyConfig {
	var resolved config.SessionRiskPolicyConfig
	if r != nil && r.system != nil {
		resolved = r.system.Security.SessionRisk
		if env := strings.TrimSpace(r.system.Server.Environment); env != "" {
			if profile, ok := r.system.Security.SessionRiskProfiles[env]; ok {
				resolved = mergeSessionRiskConfig(resolved, profile)
			}
		}
	}
	if r != nil && r.tenantSessionRisk != nil {
		if next, ok, err := r.tenantSessionRisk.LoadSessionRiskConfig(ctx); err == nil && ok {
			resolved = mergeSessionRiskConfig(resolved, next)
		}
	}
	if r != nil && r.workspaceSessionRisk != nil {
		if next, ok, err := r.workspaceSessionRisk.LoadSessionRiskConfig(ctx); err == nil && ok {
			resolved = mergeSessionRiskConfig(resolved, next)
		}
	}
	if runtime != nil {
		resolved = mergeSessionRiskConfig(resolved, *runtime)
	}
	return resolved
}

func mergeOpenAIConfig(base config.OpenAIConfig, overlay config.OpenAIConfig) config.OpenAIConfig {
	if strings.TrimSpace(overlay.APIKey) != "" {
		base.APIKey = overlay.APIKey
	}
	if strings.TrimSpace(overlay.BaseURL) != "" {
		base.BaseURL = overlay.BaseURL
	}
	if strings.TrimSpace(overlay.Model) != "" {
		base.Model = overlay.Model
	}
	if overlay.Temperature != 0 {
		base.Temperature = overlay.Temperature
	}
	if overlay.MaxTokens != 0 {
		base.MaxTokens = overlay.MaxTokens
	}
	if overlay.Timeout != 0 {
		base.Timeout = overlay.Timeout
	}
	return base
}

func mergeDifyConfig(base config.DifyConfig, overlay config.DifyConfig) config.DifyConfig {
	if overlay.Enabled {
		base.Enabled = true
	}
	if strings.TrimSpace(overlay.BaseURL) != "" {
		base.BaseURL = overlay.BaseURL
	}
	if strings.TrimSpace(overlay.APIKey) != "" {
		base.APIKey = overlay.APIKey
	}
	if strings.TrimSpace(overlay.DatasetID) != "" {
		base.DatasetID = overlay.DatasetID
	}
	if overlay.Timeout != 0 {
		base.Timeout = overlay.Timeout
	}
	if overlay.Search.TopK != 0 {
		base.Search.TopK = overlay.Search.TopK
	}
	if overlay.Search.ScoreThreshold != 0 {
		base.Search.ScoreThreshold = overlay.Search.ScoreThreshold
	}
	if strings.TrimSpace(overlay.Search.SearchMethod) != "" {
		base.Search.SearchMethod = overlay.Search.SearchMethod
	}
	if overlay.Search.RerankingEnable {
		base.Search.RerankingEnable = true
	}
	return base
}

func mergeWeKnoraConfig(base config.WeKnoraConfig, overlay config.WeKnoraConfig) config.WeKnoraConfig {
	if overlay.Enabled {
		base.Enabled = true
	}
	if strings.TrimSpace(overlay.BaseURL) != "" {
		base.BaseURL = overlay.BaseURL
	}
	if strings.TrimSpace(overlay.APIKey) != "" {
		base.APIKey = overlay.APIKey
	}
	if strings.TrimSpace(overlay.TenantID) != "" {
		base.TenantID = overlay.TenantID
	}
	if strings.TrimSpace(overlay.KnowledgeBaseID) != "" {
		base.KnowledgeBaseID = overlay.KnowledgeBaseID
	}
	if overlay.Timeout != 0 {
		base.Timeout = overlay.Timeout
	}
	if overlay.MaxRetries != 0 {
		base.MaxRetries = overlay.MaxRetries
	}
	if overlay.Search.DefaultLimit != 0 {
		base.Search.DefaultLimit = overlay.Search.DefaultLimit
	}
	if overlay.Search.ScoreThreshold != 0 {
		base.Search.ScoreThreshold = overlay.Search.ScoreThreshold
	}
	if strings.TrimSpace(overlay.Search.Strategy) != "" {
		base.Search.Strategy = overlay.Search.Strategy
	}
	if overlay.HealthCheck.Interval != 0 {
		base.HealthCheck.Interval = overlay.HealthCheck.Interval
	}
	if overlay.HealthCheck.Timeout != 0 {
		base.HealthCheck.Timeout = overlay.HealthCheck.Timeout
	}
	return base
}

func mergeRagFlowConfig(base config.RagFlowConfig, overlay config.RagFlowConfig) config.RagFlowConfig {
	if overlay.Enabled {
		base.Enabled = true
	}
	if strings.TrimSpace(overlay.BaseURL) != "" {
		base.BaseURL = overlay.BaseURL
	}
	if strings.TrimSpace(overlay.APIKey) != "" {
		base.APIKey = overlay.APIKey
	}
	if strings.TrimSpace(overlay.DatasetID) != "" {
		base.DatasetID = overlay.DatasetID
	}
	if overlay.Timeout != 0 {
		base.Timeout = overlay.Timeout
	}
	if overlay.Search.TopK != 0 {
		base.Search.TopK = overlay.Search.TopK
	}
	if overlay.Search.ScoreThreshold != 0 {
		base.Search.ScoreThreshold = overlay.Search.ScoreThreshold
	}
	return base
}

func mergeSessionRiskConfig(base config.SessionRiskPolicyConfig, overlay config.SessionRiskPolicyConfig) config.SessionRiskPolicyConfig {
	if overlay.HotRefreshWindowMinutes > 0 {
		base.HotRefreshWindowMinutes = overlay.HotRefreshWindowMinutes
	}
	if overlay.RecentRefreshWindowMinutes > 0 {
		base.RecentRefreshWindowMinutes = overlay.RecentRefreshWindowMinutes
	}
	if overlay.TodayRefreshWindowHours > 0 {
		base.TodayRefreshWindowHours = overlay.TodayRefreshWindowHours
	}
	if overlay.RapidChangeWindowHours > 0 {
		base.RapidChangeWindowHours = overlay.RapidChangeWindowHours
	}
	if overlay.StaleActivityWindowDays > 0 {
		base.StaleActivityWindowDays = overlay.StaleActivityWindowDays
	}
	if overlay.MultiPublicIPThreshold > 0 {
		base.MultiPublicIPThreshold = overlay.MultiPublicIPThreshold
	}
	if overlay.ManySessionsThreshold > 0 {
		base.ManySessionsThreshold = overlay.ManySessionsThreshold
	}
	if overlay.HotRefreshFamilyThreshold > 0 {
		base.HotRefreshFamilyThreshold = overlay.HotRefreshFamilyThreshold
	}
	if overlay.MediumRiskScore > 0 {
		base.MediumRiskScore = overlay.MediumRiskScore
	}
	if overlay.HighRiskScore > 0 {
		base.HighRiskScore = overlay.HighRiskScore
	}
	return base
}
