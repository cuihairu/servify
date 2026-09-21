package configscope

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"
)

type stubRagFlowProvider struct {
	value config.RagFlowConfig
	ok    bool
	err   error
}

func (s stubRagFlowProvider) LoadRagFlowConfig(ctx context.Context) (config.RagFlowConfig, bool, error) {
	return s.value, s.ok, s.err
}

func TestResolverResolveRagFlowFullMerge(t *testing.T) {
	resolver := NewResolver(&config.Config{
		RagFlow: config.RagFlowConfig{
			Enabled:   false,
			BaseURL:   "http://base:9380",
			APIKey:    "base-key",
			DatasetID: "ds-base",
			Timeout:   5 * time.Second,
			Search:    config.RagFlowSearchConfig{TopK: 4, ScoreThreshold: 0.1},
		},
	},
		WithTenantRagFlowProvider(stubRagFlowProvider{ok: true, value: config.RagFlowConfig{
			Enabled: true,
			BaseURL: "http://tenant:9380",
			Search:  config.RagFlowSearchConfig{TopK: 6},
		}}),
		WithWorkspaceRagFlowProvider(stubRagFlowProvider{ok: true, value: config.RagFlowConfig{
			APIKey:    "ws-key",
			DatasetID: "ds-ws",
			Timeout:   9 * time.Second,
			Search:    config.RagFlowSearchConfig{ScoreThreshold: 0.5},
		}}),
	)

	got := resolver.ResolveRagFlow(context.Background(), &config.RagFlowConfig{
		Search: config.RagFlowSearchConfig{TopK: 8},
	})
	want := config.RagFlowConfig{
		Enabled:   true,
		BaseURL:   "http://tenant:9380",
		APIKey:    "ws-key",
		DatasetID: "ds-ws",
		Timeout:   9 * time.Second,
		Search:    config.RagFlowSearchConfig{TopK: 8, ScoreThreshold: 0.5},
	}
	if got != want {
		t.Fatalf("ResolveRagFlow() = %+v, want %+v", got, want)
	}
}

func TestResolverResolveRagFlowZeroOverlayKeepsBase(t *testing.T) {
	resolver := NewResolver(&config.Config{
		RagFlow: config.RagFlowConfig{
			Enabled: true,
			BaseURL: "http://base:9380",
			Search:  config.RagFlowSearchConfig{TopK: 4, ScoreThreshold: 0.1},
		},
	},
		WithTenantRagFlowProvider(stubRagFlowProvider{err: context.DeadlineExceeded}),
		WithWorkspaceRagFlowProvider(stubRagFlowProvider{}),
	)

	got := resolver.ResolveRagFlow(context.Background(), &config.RagFlowConfig{})
	if got.BaseURL != "http://base:9380" || !got.Enabled ||
		got.Search.TopK != 4 || got.Search.ScoreThreshold != 0.1 {
		t.Fatalf("ResolveRagFlow() = %+v, want base preserved", got)
	}
}

func TestResolverResolveRagFlowNilSystemKeepsZero(t *testing.T) {
	var resolver *Resolver
	if got := resolver.ResolveRagFlow(context.Background(), nil); got != (config.RagFlowConfig{}) {
		t.Fatalf("ResolveRagFlow() on nil resolver = %+v", got)
	}
}

func TestGormProvidersLoadRagFlowConfig(t *testing.T) {
	db := openProviderTestDB(t)
	if err := db.Create(&models.TenantConfig{
		TenantID:    "tenant-a",
		RagFlowJSON: `{"enabled":true,"base_url":"http://tenant:9380","dataset_id":"ds-tenant","search":{"top_k":6}}`,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Create(&models.WorkspaceConfig{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		RagFlowJSON: `{"enabled":true,"dataset_id":"ds-ws","search":{"score_threshold":0.4}}`,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	tenantProvider := NewGormTenantConfigProvider(db)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "")
	tenantRagFlow, ok, err := tenantProvider.LoadRagFlowConfig(ctx)
	if err != nil || !ok || !tenantRagFlow.Enabled || tenantRagFlow.DatasetID != "ds-tenant" || tenantRagFlow.Search.TopK != 6 {
		t.Fatalf("tenant ragflow = %+v ok=%v err=%v", tenantRagFlow, ok, err)
	}

	workspaceProvider := NewGormWorkspaceConfigProvider(db)
	wsCtx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")
	wsRagFlow, ok, err := workspaceProvider.LoadRagFlowConfig(wsCtx)
	if err != nil || !ok || !wsRagFlow.Enabled || wsRagFlow.DatasetID != "ds-ws" || wsRagFlow.Search.ScoreThreshold != 0.4 {
		t.Fatalf("workspace ragflow = %+v ok=%v err=%v", wsRagFlow, ok, err)
	}

	// 作用域缺失（无 workspace id）→ 不加载。
	if _, ok, err := workspaceProvider.LoadRagFlowConfig(ctx); ok || err != nil {
		t.Fatalf("missing scope: ok=%v err=%v", ok, err)
	}
	// 无行 → 不加载。
	if _, ok, err := tenantProvider.LoadRagFlowConfig(platformauth.ContextWithScope(context.Background(), "tenant-missing", "")); ok || err != nil {
		t.Fatalf("missing row: ok=%v err=%v", ok, err)
	}
}

func TestGormConfigStoreRagFlowSectionRoundTrip(t *testing.T) {
	db := openProviderTestDB(t)
	store := NewGormConfigStore(db)
	ctx := context.Background()

	created, err := store.UpsertTenantConfig(ctx, "tenant-rf", ScopedConfigDocument{
		RagFlow: &config.RagFlowConfig{Enabled: true, BaseURL: "http://rf:9380", Search: config.RagFlowSearchConfig{TopK: 7}},
	})
	if err != nil {
		t.Fatalf("upsert tenant: %v", err)
	}
	if created.RagFlow == nil || !created.RagFlow.Enabled || created.RagFlow.BaseURL != "http://rf:9380" || created.RagFlow.Search.TopK != 7 {
		t.Fatalf("created tenant doc = %+v", created.RagFlow)
	}

	createdWS, err := store.UpsertWorkspaceConfig(ctx, "tenant-rf", "workspace-rf", ScopedConfigDocument{
		RagFlow: &config.RagFlowConfig{Enabled: true, DatasetID: "ds-ws"},
	})
	if err != nil {
		t.Fatalf("upsert workspace: %v", err)
	}
	if createdWS.RagFlow == nil || createdWS.RagFlow.DatasetID != "ds-ws" {
		t.Fatalf("created ws doc = %+v", createdWS.RagFlow)
	}

	fetched, ok, err := store.GetTenantConfig(ctx, "tenant-rf")
	if err != nil || !ok || fetched.RagFlow == nil || fetched.RagFlow.BaseURL != "http://rf:9380" {
		t.Fatalf("fetch tenant = %+v ok=%v err=%v", fetched.RagFlow, ok, err)
	}

	// 不带 ragflow 段的更新不抹掉已存段。
	updated, err := store.UpsertTenantConfig(ctx, "tenant-rf", ScopedConfigDocument{
		Portal: &config.PortalConfig{BrandName: "x"},
	})
	if err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	if updated.RagFlow == nil || updated.RagFlow.BaseURL != "http://rf:9380" {
		t.Fatalf("updated doc ragflow = %+v want preserved", updated.RagFlow)
	}
}

func TestGormConfigStoreRagFlowEncodeError(t *testing.T) {
	stubYamlMarshalError(t, encodeErr)
	store := NewGormConfigStore(openProviderTestDB(t))
	ctx := context.Background()

	if _, err := store.UpsertTenantConfig(ctx, "tenant-rf-enc", ScopedConfigDocument{
		RagFlow: &config.RagFlowConfig{Enabled: true},
	}); err == nil {
		t.Fatal("expected tenant ragflow encode error, got nil")
	}
	if _, err := store.UpsertWorkspaceConfig(ctx, "tenant-rf-enc", "workspace-rf", ScopedConfigDocument{
		RagFlow: &config.RagFlowConfig{Enabled: true},
	}); err == nil {
		t.Fatal("expected workspace ragflow encode error, got nil")
	}
}

func TestGormConfigStoreRagFlowInvalidYAML(t *testing.T) {
	db := openProviderTestDB(t)
	store := NewGormConfigStore(db)
	ctx := context.Background()

	if err := db.Create(&models.TenantConfig{
		TenantID:    "tenant-rf-bad",
		RagFlowJSON: "api_key: {unclosed",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Create(&models.WorkspaceConfig{
		TenantID:    "tenant-rf-bad",
		WorkspaceID: "workspace-rf-bad",
		RagFlowJSON: "api_key: {unclosed",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, _, err := store.GetTenantConfig(ctx, "tenant-rf-bad"); err == nil {
		t.Fatal("expected tenant ragflow decode error, got nil")
	}
	if _, _, err := store.GetWorkspaceConfig(ctx, "tenant-rf-bad", "workspace-rf-bad"); err == nil {
		t.Fatal("expected workspace ragflow decode error, got nil")
	}
}
