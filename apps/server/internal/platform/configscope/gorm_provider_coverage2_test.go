package configscope

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"
)

func TestCoverage2GormTenantConfigProviderNilAndMissingScope(t *testing.T) {
	var nilProvider *GormTenantConfigProvider
	_, ok, err := nilProvider.LoadPortalConfig(context.Background())
	if ok || err != nil {
		t.Fatalf("nil provider portal: ok=%v err=%v", ok, err)
	}
	_, ok, err = nilProvider.LoadOpenAIConfig(context.Background())
	if ok || err != nil {
		t.Fatalf("nil provider openai: ok=%v err=%v", ok, err)
	}
	_, ok, err = nilProvider.LoadDifyConfig(context.Background())
	if ok || err != nil {
		t.Fatalf("nil provider dify: ok=%v err=%v", ok, err)
	}
	_, ok, err = nilProvider.LoadWeKnoraConfig(context.Background())
	if ok || err != nil {
		t.Fatalf("nil provider weknora: ok=%v err=%v", ok, err)
	}
	_, ok, err = nilProvider.LoadSessionRiskConfig(context.Background())
	if ok || err != nil {
		t.Fatalf("nil provider session risk: ok=%v err=%v", ok, err)
	}

	if NewGormTenantConfigProvider(nil) != nil {
		t.Fatalf("expected nil tenant provider for nil db")
	}

	db := openProviderTestDB(t)
	provider := NewGormTenantConfigProvider(db)

	_, ok, err = provider.LoadPortalConfig(context.Background())
	if ok || err != nil {
		t.Fatalf("no tenant in ctx: ok=%v err=%v", ok, err)
	}

	blankCtx := platformauth.ContextWithScope(context.Background(), "   ", "")
	_, ok, err = provider.LoadPortalConfig(blankCtx)
	if ok || err != nil {
		t.Fatalf("blank tenant in ctx: ok=%v err=%v", ok, err)
	}

	scopedCtx := platformauth.ContextWithScope(context.Background(), "tenant-missing", "")
	_, ok, err = provider.LoadPortalConfig(scopedCtx)
	if ok || err != nil {
		t.Fatalf("missing tenant row: ok=%v err=%v", ok, err)
	}
}

func TestCoverage2GormTenantConfigProviderDifyAndEmptyPayloads(t *testing.T) {
	db := openProviderTestDB(t)
	if err := db.Create(&models.TenantConfig{
		TenantID:   "tenant-a",
		PortalJSON: "",
		DifyJSON:   `{"enabled":true,"base_url":"https://dify.example","dataset_id":"ds-1","search":{"top_k":5,"score_threshold":0.4,"search_method":"hybrid","reranking_enable":true}}`,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "")
	provider := NewGormTenantConfigProvider(db)

	portal, ok, err := provider.LoadPortalConfig(ctx)
	if ok || err != nil || portal.BrandName != "" {
		t.Fatalf("portal empty payload: cfg=%+v ok=%v err=%v", portal, ok, err)
	}

	dify, ok, err := provider.LoadDifyConfig(ctx)
	if err != nil || !ok {
		t.Fatalf("dify: ok=%v err=%v", ok, err)
	}
	if !dify.Enabled || dify.BaseURL != "https://dify.example" || dify.DatasetID != "ds-1" ||
		dify.Search.TopK != 5 || dify.Search.ScoreThreshold != 0.4 ||
		dify.Search.SearchMethod != "hybrid" || !dify.Search.RerankingEnable {
		t.Fatalf("dify = %+v want seeded values", dify)
	}
}

func TestCoverage2GormTenantConfigProviderInvalidYAML(t *testing.T) {
	db := openProviderTestDB(t)
	if err := db.Create(&models.TenantConfig{
		TenantID:   "tenant-a",
		PortalJSON: "brand_name: [unclosed",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "")
	provider := NewGormTenantConfigProvider(db)

	_, ok, err := provider.LoadPortalConfig(ctx)
	if ok || err == nil {
		t.Fatalf("expected yaml decode error, ok=%v err=%v", ok, err)
	}
}

func TestCoverage2GormTenantConfigProviderDBError(t *testing.T) {
	db := openProviderTestDB(t)
	provider := NewGormTenantConfigProvider(db)
	cov2CloseDB(t, db)

	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "")
	_, ok, err := provider.LoadPortalConfig(ctx)
	if ok || err == nil {
		t.Fatalf("expected db error, ok=%v err=%v", ok, err)
	}
	_, ok, err = provider.LoadDifyConfig(ctx)
	if ok || err == nil {
		t.Fatalf("expected db error on dify, ok=%v err=%v", ok, err)
	}
}

func TestCoverage2GormWorkspaceConfigProviderNilAndMissingScope(t *testing.T) {
	var nilProvider *GormWorkspaceConfigProvider
	_, ok, err := nilProvider.LoadPortalConfig(context.Background())
	if ok || err != nil {
		t.Fatalf("nil provider portal: ok=%v err=%v", ok, err)
	}
	_, ok, err = nilProvider.LoadOpenAIConfig(context.Background())
	if ok || err != nil {
		t.Fatalf("nil provider openai: ok=%v err=%v", ok, err)
	}
	_, ok, err = nilProvider.LoadDifyConfig(context.Background())
	if ok || err != nil {
		t.Fatalf("nil provider dify: ok=%v err=%v", ok, err)
	}
	_, ok, err = nilProvider.LoadWeKnoraConfig(context.Background())
	if ok || err != nil {
		t.Fatalf("nil provider weknora: ok=%v err=%v", ok, err)
	}
	_, ok, err = nilProvider.LoadSessionRiskConfig(context.Background())
	if ok || err != nil {
		t.Fatalf("nil provider session risk: ok=%v err=%v", ok, err)
	}

	if NewGormWorkspaceConfigProvider(nil) != nil {
		t.Fatalf("expected nil workspace provider for nil db")
	}

	db := openProviderTestDB(t)
	provider := NewGormWorkspaceConfigProvider(db)

	tenantOnly := platformauth.ContextWithScope(context.Background(), "tenant-a", "")
	_, ok, err = provider.LoadPortalConfig(tenantOnly)
	if ok || err != nil {
		t.Fatalf("no workspace in ctx: ok=%v err=%v", ok, err)
	}

	blankCtx := platformauth.ContextWithScope(context.Background(), "tenant-a", "   ")
	_, ok, err = provider.LoadPortalConfig(blankCtx)
	if ok || err != nil {
		t.Fatalf("blank workspace in ctx: ok=%v err=%v", ok, err)
	}

	scopedCtx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-missing")
	_, ok, err = provider.LoadPortalConfig(scopedCtx)
	if ok || err != nil {
		t.Fatalf("missing workspace row: ok=%v err=%v", ok, err)
	}
}

func TestCoverage2GormWorkspaceConfigProviderDifyAndEmptyPayloads(t *testing.T) {
	db := openProviderTestDB(t)
	if err := db.Create(&models.WorkspaceConfig{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		OpenAIJSON:  "",
		DifyJSON:    `{"enabled":true,"api_key":"ws-key","timeout":"3s"}`,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	provider := NewGormWorkspaceConfigProvider(db)

	openai, ok, err := provider.LoadOpenAIConfig(ctx)
	if ok || err != nil || openai.Model != "" {
		t.Fatalf("openai empty payload: cfg=%+v ok=%v err=%v", openai, ok, err)
	}

	dify, ok, err := provider.LoadDifyConfig(ctx)
	if err != nil || !ok {
		t.Fatalf("dify: ok=%v err=%v", ok, err)
	}
	if !dify.Enabled || dify.APIKey != "ws-key" || dify.Timeout != 3*time.Second {
		t.Fatalf("dify = %+v want seeded values", dify)
	}
}

func TestCoverage2GormWorkspaceConfigProviderInvalidYAML(t *testing.T) {
	db := openProviderTestDB(t)
	if err := db.Create(&models.WorkspaceConfig{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		OpenAIJSON:  "model: {unclosed",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	provider := NewGormWorkspaceConfigProvider(db)

	_, ok, err := provider.LoadOpenAIConfig(ctx)
	if ok || err == nil {
		t.Fatalf("expected yaml decode error, ok=%v err=%v", ok, err)
	}
}

func TestCoverage2GormWorkspaceConfigProviderDBError(t *testing.T) {
	db := openProviderTestDB(t)
	provider := NewGormWorkspaceConfigProvider(db)
	cov2CloseDB(t, db)

	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	_, ok, err := provider.LoadPortalConfig(ctx)
	if ok || err == nil {
		t.Fatalf("expected db error, ok=%v err=%v", ok, err)
	}
	_, ok, err = provider.LoadWeKnoraConfig(ctx)
	if ok || err == nil {
		t.Fatalf("expected db error on weknora, ok=%v err=%v", ok, err)
	}
}
