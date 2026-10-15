package configscope

// translation_quota 租户覆盖值：enforcement 读取「system 默认 + 租户/工作区
// 覆盖」限额链（docs/design/tenant-quota-policy.md §4，2026-10-16 代决落地）。
// 与 portal/openai/session_risk 同构：ScopedConfigDocument.translation_quota
// ↔ 模型 translation_quota_json 列 ↔ provider Load ↔ resolver Resolve。

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"
)

type stubTranslationQuotaProvider struct {
	value config.TranslationQuotaOverrideConfig
	ok    bool
	err   error
}

func (s stubTranslationQuotaProvider) LoadTranslationQuotaConfig(ctx context.Context) (config.TranslationQuotaOverrideConfig, bool, error) {
	return s.value, s.ok, s.err
}

func TestTenantConfigProviderLoadsTranslationQuota(t *testing.T) {
	db := openProviderTestDB(t)
	if err := db.Create(&models.TenantConfig{
		TenantID:             "tenant-a",
		TranslationQuotaJSON: `{"limit":77}`,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	provider := NewGormTenantConfigProvider(db)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "")

	quota, ok, err := provider.LoadTranslationQuotaConfig(ctx)
	if err != nil || !ok || quota.Limit != 77 {
		t.Fatalf("translation quota = %+v ok=%v err=%v", quota, ok, err)
	}
}

func TestTenantConfigProviderMissingTranslationQuota(t *testing.T) {
	db := openProviderTestDB(t)
	if err := db.Create(&models.TenantConfig{TenantID: "tenant-a"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	provider := NewGormTenantConfigProvider(db)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "")

	quota, ok, err := provider.LoadTranslationQuotaConfig(ctx)
	if err != nil || ok || quota.Limit != 0 {
		t.Fatalf("missing override = %+v ok=%v err=%v, want zero/false/nil", quota, ok, err)
	}
}

func TestWorkspaceConfigProviderLoadsTranslationQuota(t *testing.T) {
	db := openProviderTestDB(t)
	if err := db.Create(&models.WorkspaceConfig{
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-1",
		TranslationQuotaJSON: `{"limit":88}`,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	provider := NewGormWorkspaceConfigProvider(db)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "workspace-1")

	quota, ok, err := provider.LoadTranslationQuotaConfig(ctx)
	if err != nil || !ok || quota.Limit != 88 {
		t.Fatalf("translation quota = %+v ok=%v err=%v", quota, ok, err)
	}
}

func TestWorkspaceConfigProviderWithoutWorkspaceScope(t *testing.T) {
	db := openProviderTestDB(t)
	provider := NewGormWorkspaceConfigProvider(db)
	// 无工作区作用域：不查库，直接视为无覆盖（回落系统默认限额）。
	quota, ok, err := provider.LoadTranslationQuotaConfig(
		platformauth.ContextWithScope(context.Background(), "tenant-a", ""),
	)
	if err != nil || ok || quota.Limit != 0 {
		t.Fatalf("missing workspace scope = %+v ok=%v err=%v, want zero/false/nil", quota, ok, err)
	}
}

func TestUpsertTenantConfigStoresTranslationQuota(t *testing.T) {
	db := openProviderTestDB(t)
	store := NewGormConfigStore(db)
	override := &config.TranslationQuotaOverrideConfig{Limit: 42}
	doc, err := store.UpsertTenantConfig(context.Background(), "tenant-a", ScopedConfigDocument{TranslationQuota: override})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if doc == nil || doc.TranslationQuota == nil || doc.TranslationQuota.Limit != 42 {
		t.Fatalf("roundtrip doc = %+v", doc)
	}

	got, ok, err := store.GetTenantConfig(context.Background(), "tenant-a")
	if err != nil || !ok || got == nil || got.TranslationQuota == nil || got.TranslationQuota.Limit != 42 {
		t.Fatalf("get doc = %+v ok=%v err=%v", got, ok, err)
	}
}

func TestUpsertWorkspaceConfigStoresTranslationQuota(t *testing.T) {
	db := openProviderTestDB(t)
	store := NewGormConfigStore(db)
	doc, err := store.UpsertWorkspaceConfig(context.Background(), "tenant-a", "workspace-1", ScopedConfigDocument{
		WorkspaceID:      "workspace-1",
		TranslationQuota: &config.TranslationQuotaOverrideConfig{Limit: 56},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if doc == nil || doc.TranslationQuota == nil || doc.TranslationQuota.Limit != 56 {
		t.Fatalf("roundtrip doc = %+v", doc)
	}

	got, ok, err := store.GetWorkspaceConfig(context.Background(), "tenant-a", "workspace-1")
	if err != nil || !ok || got == nil || got.TranslationQuota == nil || got.TranslationQuota.Limit != 56 {
		t.Fatalf("get doc = %+v ok=%v err=%v", got, ok, err)
	}
}

func TestResolverResolveTranslationQuotaLimit(t *testing.T) {
	systemLimit := int64(1000)
	system := &config.Config{}
	system.Translation.Quota.Limit = systemLimit
	ctx := context.Background()

	cases := []struct {
		name     string
		resolver *Resolver
		want     int64
	}{
		{"nil resolver", nil, 0},
		{"system only", NewResolver(system), systemLimit},
		{"tenant override wins", NewResolver(system,
			WithTenantTranslationQuotaProvider(stubTranslationQuotaProvider{value: config.TranslationQuotaOverrideConfig{Limit: 500}, ok: true}),
		), 500},
		{"workspace beats tenant", NewResolver(system,
			WithTenantTranslationQuotaProvider(stubTranslationQuotaProvider{value: config.TranslationQuotaOverrideConfig{Limit: 500}, ok: true}),
			WithWorkspaceTranslationQuotaProvider(stubTranslationQuotaProvider{value: config.TranslationQuotaOverrideConfig{Limit: 300}, ok: true}),
		), 300},
		{"zero tenant override falls back to system", NewResolver(system,
			WithTenantTranslationQuotaProvider(stubTranslationQuotaProvider{value: config.TranslationQuotaOverrideConfig{Limit: 0}, ok: true}),
		), systemLimit},
		{"zero workspace keeps tenant", NewResolver(system,
			WithTenantTranslationQuotaProvider(stubTranslationQuotaProvider{value: config.TranslationQuotaOverrideConfig{Limit: 500}, ok: true}),
			WithWorkspaceTranslationQuotaProvider(stubTranslationQuotaProvider{value: config.TranslationQuotaOverrideConfig{Limit: 0}, ok: true}),
		), 500},
		{"tenant error falls back to system", NewResolver(system,
			WithTenantTranslationQuotaProvider(stubTranslationQuotaProvider{ok: false, err: errors.New("db down")}),
		), systemLimit},
		{"tenant not found falls back to system", NewResolver(system,
			WithTenantTranslationQuotaProvider(stubTranslationQuotaProvider{ok: false}),
		), systemLimit},
		{"workspace error falls back to tenant", NewResolver(system,
			WithTenantTranslationQuotaProvider(stubTranslationQuotaProvider{value: config.TranslationQuotaOverrideConfig{Limit: 500}, ok: true}),
			WithWorkspaceTranslationQuotaProvider(stubTranslationQuotaProvider{ok: false, err: errors.New("db down")}),
		), 500},
		{"nil system falls back to zero", NewResolver(nil), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.resolver.ResolveTranslationQuotaLimit(ctx); got != tc.want {
				t.Fatalf("ResolveTranslationQuotaLimit = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestResolverResolveTranslationQuotaLimitWithTenantScope(t *testing.T) {
	// 租户上下文驱动：同一 resolver 对覆盖行与非覆盖行取不同限额（装配层
	// 每请求经 platformauth ctx 驱动，此处钉住 ctx 驱动语义）。
	db := openProviderTestDB(t)
	if err := db.Create(&models.TenantConfig{
		TenantID:             "tenant-a",
		TranslationQuotaJSON: `{"limit":123}`,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	resolver := NewResolver(nil,
		WithTenantTranslationQuotaProvider(NewGormTenantConfigProvider(db)),
	)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "")
	if got := resolver.ResolveTranslationQuotaLimit(ctx); got != 123 {
		t.Fatalf("tenant-a limit = %d, want 123", got)
	}
	other := platformauth.ContextWithScope(context.Background(), "tenant-b", "")
	if got := resolver.ResolveTranslationQuotaLimit(other); got != 0 {
		t.Fatalf("tenant-b limit = %d, want 0 (system nil → 0, 回落 0)", got)
	}
}
