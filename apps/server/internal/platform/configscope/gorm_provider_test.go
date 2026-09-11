package configscope

import (
	"context"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openProviderTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:"+t.Name()+"")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.TenantConfig{}, &models.WorkspaceConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestGormTenantConfigProviderLoadsScopedConfigs(t *testing.T) {
	db := openProviderTestDB(t)
	seed := models.TenantConfig{
		TenantID:        "tenant-a",
		PortalJSON:      `{"brand_name":"Tenant Brand"}`,
		OpenAIJSON:      `{"model":"gpt-tenant"}`,
		WeKnoraJSON:     `{"knowledge_base_id":"kb-tenant"}`,
		SessionRiskJSON: `{"high_risk_score":9}`,
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "")
	provider := NewGormTenantConfigProvider(db)

	portal, ok, err := provider.LoadPortalConfig(ctx)
	if err != nil || !ok || portal.BrandName != "Tenant Brand" {
		t.Fatalf("portal = %+v ok=%v err=%v", portal, ok, err)
	}
	openai, ok, err := provider.LoadOpenAIConfig(ctx)
	if err != nil || !ok || openai.Model != "gpt-tenant" {
		t.Fatalf("openai = %+v ok=%v err=%v", openai, ok, err)
	}
	weknora, ok, err := provider.LoadWeKnoraConfig(ctx)
	if err != nil || !ok || weknora.KnowledgeBaseID != "kb-tenant" {
		t.Fatalf("weknora = %+v ok=%v err=%v", weknora, ok, err)
	}
	sessionRisk, ok, err := provider.LoadSessionRiskConfig(ctx)
	if err != nil || !ok || sessionRisk.HighRiskScore != 9 {
		t.Fatalf("session risk = %+v ok=%v err=%v", sessionRisk, ok, err)
	}
}

func TestGormWorkspaceConfigProviderLoadsScopedConfigs(t *testing.T) {
	db := openProviderTestDB(t)
	seed := models.WorkspaceConfig{
		TenantID:        "tenant-a",
		WorkspaceID:     "workspace-1",
		PortalJSON:      `{"brand_name":"Workspace Brand"}`,
		OpenAIJSON:      `{"model":"gpt-workspace"}`,
		WeKnoraJSON:     `{"knowledge_base_id":"kb-workspace"}`,
		SessionRiskJSON: `{"high_risk_score":11}`,
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	provider := NewGormWorkspaceConfigProvider(db)

	portal, ok, err := provider.LoadPortalConfig(ctx)
	if err != nil || !ok || portal.BrandName != "Workspace Brand" {
		t.Fatalf("portal = %+v ok=%v err=%v", portal, ok, err)
	}
	openai, ok, err := provider.LoadOpenAIConfig(ctx)
	if err != nil || !ok || openai.Model != "gpt-workspace" {
		t.Fatalf("openai = %+v ok=%v err=%v", openai, ok, err)
	}
	weknora, ok, err := provider.LoadWeKnoraConfig(ctx)
	if err != nil || !ok || weknora.KnowledgeBaseID != "kb-workspace" {
		t.Fatalf("weknora = %+v ok=%v err=%v", weknora, ok, err)
	}
	sessionRisk, ok, err := provider.LoadSessionRiskConfig(ctx)
	if err != nil || !ok || sessionRisk.HighRiskScore != 11 {
		t.Fatalf("session risk = %+v ok=%v err=%v", sessionRisk, ok, err)
	}
}

func TestResolverWithGormProvidersPrecedence(t *testing.T) {
	db := openProviderTestDB(t)
	if err := db.Create(&models.TenantConfig{
		TenantID:   "tenant-a",
		PortalJSON: `{"brand_name":"Tenant Brand","primary_color":"#111111"}`,
	}).Error; err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := db.Create(&models.WorkspaceConfig{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		PortalJSON:  `{"brand_name":"Workspace Brand"}`,
	}).Error; err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	resolver := NewResolver(&config.Config{Portal: config.PortalConfig{BrandName: "System Brand", PrimaryColor: "#000000"}},
		WithTenantPortalProvider(NewGormTenantConfigProvider(db)),
		WithWorkspacePortalProvider(NewGormWorkspaceConfigProvider(db)),
	)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	got := resolver.ResolvePortal(ctx, &config.PortalConfig{SupportEmail: "ops@example.com"})
	if got.BrandName != "Workspace Brand" {
		t.Fatalf("brand = %q want workspace", got.BrandName)
	}
	if got.PrimaryColor != "#111111" {
		t.Fatalf("primary = %q want tenant", got.PrimaryColor)
	}
	if got.SupportEmail != "ops@example.com" {
		t.Fatalf("support email = %q want runtime", got.SupportEmail)
	}
}
