package configscope

import (
	"context"
	"strings"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

type GormTenantConfigProvider struct {
	db *gorm.DB
}

func NewGormTenantConfigProvider(db *gorm.DB) *GormTenantConfigProvider {
	if db == nil {
		return nil
	}
	return &GormTenantConfigProvider{db: db}
}

func (p *GormTenantConfigProvider) LoadPortalConfig(ctx context.Context) (config.PortalConfig, bool, error) {
	var cfg models.TenantConfig
	ok, err := p.load(ctx, &cfg)
	if !ok || err != nil {
		return config.PortalConfig{}, ok, err
	}
	return decodeConfig[config.PortalConfig](cfg.PortalJSON)
}

func (p *GormTenantConfigProvider) LoadOpenAIConfig(ctx context.Context) (config.OpenAIConfig, bool, error) {
	var cfg models.TenantConfig
	ok, err := p.load(ctx, &cfg)
	if !ok || err != nil {
		return config.OpenAIConfig{}, ok, err
	}
	return decodeConfig[config.OpenAIConfig](cfg.OpenAIJSON)
}

func (p *GormTenantConfigProvider) LoadDifyConfig(ctx context.Context) (config.DifyConfig, bool, error) {
	var cfg models.TenantConfig
	ok, err := p.load(ctx, &cfg)
	if !ok || err != nil {
		return config.DifyConfig{}, ok, err
	}
	return decodeConfig[config.DifyConfig](cfg.DifyJSON)
}

func (p *GormTenantConfigProvider) LoadWeKnoraConfig(ctx context.Context) (config.WeKnoraConfig, bool, error) {
	var cfg models.TenantConfig
	ok, err := p.load(ctx, &cfg)
	if !ok || err != nil {
		return config.WeKnoraConfig{}, ok, err
	}
	return decodeConfig[config.WeKnoraConfig](cfg.WeKnoraJSON)
}

func (p *GormTenantConfigProvider) LoadRagFlowConfig(ctx context.Context) (config.RagFlowConfig, bool, error) {
	var cfg models.TenantConfig
	ok, err := p.load(ctx, &cfg)
	if !ok || err != nil {
		return config.RagFlowConfig{}, ok, err
	}
	return decodeConfig[config.RagFlowConfig](cfg.RagFlowJSON)
}

func (p *GormTenantConfigProvider) LoadSessionRiskConfig(ctx context.Context) (config.SessionRiskPolicyConfig, bool, error) {
	var cfg models.TenantConfig
	ok, err := p.load(ctx, &cfg)
	if !ok || err != nil {
		return config.SessionRiskPolicyConfig{}, ok, err
	}
	return decodeConfig[config.SessionRiskPolicyConfig](cfg.SessionRiskJSON)
}

func (p *GormTenantConfigProvider) load(ctx context.Context, out *models.TenantConfig) (bool, error) {
	if p == nil || p.db == nil {
		return false, nil
	}
	tenantID := strings.TrimSpace(platformauth.TenantIDFromContext(ctx))
	if tenantID == "" {
		return false, nil
	}
	err := p.db.WithContext(ctx).Where("tenant_id = ?", tenantID).First(out).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

type GormWorkspaceConfigProvider struct {
	db *gorm.DB
}

func NewGormWorkspaceConfigProvider(db *gorm.DB) *GormWorkspaceConfigProvider {
	if db == nil {
		return nil
	}
	return &GormWorkspaceConfigProvider{db: db}
}

func (p *GormWorkspaceConfigProvider) LoadPortalConfig(ctx context.Context) (config.PortalConfig, bool, error) {
	var cfg models.WorkspaceConfig
	ok, err := p.load(ctx, &cfg)
	if !ok || err != nil {
		return config.PortalConfig{}, ok, err
	}
	return decodeConfig[config.PortalConfig](cfg.PortalJSON)
}

func (p *GormWorkspaceConfigProvider) LoadOpenAIConfig(ctx context.Context) (config.OpenAIConfig, bool, error) {
	var cfg models.WorkspaceConfig
	ok, err := p.load(ctx, &cfg)
	if !ok || err != nil {
		return config.OpenAIConfig{}, ok, err
	}
	return decodeConfig[config.OpenAIConfig](cfg.OpenAIJSON)
}

func (p *GormWorkspaceConfigProvider) LoadDifyConfig(ctx context.Context) (config.DifyConfig, bool, error) {
	var cfg models.WorkspaceConfig
	ok, err := p.load(ctx, &cfg)
	if !ok || err != nil {
		return config.DifyConfig{}, ok, err
	}
	return decodeConfig[config.DifyConfig](cfg.DifyJSON)
}

func (p *GormWorkspaceConfigProvider) LoadWeKnoraConfig(ctx context.Context) (config.WeKnoraConfig, bool, error) {
	var cfg models.WorkspaceConfig
	ok, err := p.load(ctx, &cfg)
	if !ok || err != nil {
		return config.WeKnoraConfig{}, ok, err
	}
	return decodeConfig[config.WeKnoraConfig](cfg.WeKnoraJSON)
}

func (p *GormWorkspaceConfigProvider) LoadRagFlowConfig(ctx context.Context) (config.RagFlowConfig, bool, error) {
	var cfg models.WorkspaceConfig
	ok, err := p.load(ctx, &cfg)
	if !ok || err != nil {
		return config.RagFlowConfig{}, ok, err
	}
	return decodeConfig[config.RagFlowConfig](cfg.RagFlowJSON)
}

func (p *GormWorkspaceConfigProvider) LoadSessionRiskConfig(ctx context.Context) (config.SessionRiskPolicyConfig, bool, error) {
	var cfg models.WorkspaceConfig
	ok, err := p.load(ctx, &cfg)
	if !ok || err != nil {
		return config.SessionRiskPolicyConfig{}, ok, err
	}
	return decodeConfig[config.SessionRiskPolicyConfig](cfg.SessionRiskJSON)
}

func (p *GormWorkspaceConfigProvider) load(ctx context.Context, out *models.WorkspaceConfig) (bool, error) {
	if p == nil || p.db == nil {
		return false, nil
	}
	tenantID := strings.TrimSpace(platformauth.TenantIDFromContext(ctx))
	workspaceID := strings.TrimSpace(platformauth.WorkspaceIDFromContext(ctx))
	if tenantID == "" || workspaceID == "" {
		return false, nil
	}
	err := p.db.WithContext(ctx).Where("tenant_id = ? AND workspace_id = ?", tenantID, workspaceID).First(out).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func decodeConfig[T any](payload string) (T, bool, error) {
	var out T
	if strings.TrimSpace(payload) == "" {
		return out, false, nil
	}
	if err := yaml.Unmarshal([]byte(payload), &out); err != nil {
		return out, false, err
	}
	return out, true, nil
}
