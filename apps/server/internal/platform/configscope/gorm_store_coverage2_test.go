package configscope

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

func cov2CloseDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}
}

func TestCoverage2NewGormConfigStoreNilDB(t *testing.T) {
	if NewGormConfigStore(nil) != nil {
		t.Fatalf("expected nil store for nil db")
	}
}

func TestCoverage2GormConfigStoreNilReceiver(t *testing.T) {
	var store *GormConfigStore

	doc, ok, err := store.GetTenantConfig(context.Background(), "tenant-a")
	if doc != nil || ok || err != nil {
		t.Fatalf("tenant get = %+v ok=%v err=%v want nil/false/nil", doc, ok, err)
	}
	doc, err = store.UpsertTenantConfig(context.Background(), "tenant-a", ScopedConfigDocument{})
	if doc != nil || err != nil {
		t.Fatalf("tenant upsert = %+v err=%v want nil/nil", doc, err)
	}
	doc, ok, err = store.GetWorkspaceConfig(context.Background(), "tenant-a", "workspace-1")
	if doc != nil || ok || err != nil {
		t.Fatalf("workspace get = %+v ok=%v err=%v want nil/false/nil", doc, ok, err)
	}
	doc, err = store.UpsertWorkspaceConfig(context.Background(), "tenant-a", "workspace-1", ScopedConfigDocument{})
	if doc != nil || err != nil {
		t.Fatalf("workspace upsert = %+v err=%v want nil/nil", doc, err)
	}
}

func TestCoverage2GormConfigStoreTenantRoundTrip(t *testing.T) {
	db := openProviderTestDB(t)
	store := NewGormConfigStore(db)
	ctx := context.Background()

	doc, ok, err := store.GetTenantConfig(ctx, "")
	if doc != nil || ok || err != nil {
		t.Fatalf("empty tenant id: doc=%+v ok=%v err=%v", doc, ok, err)
	}
	doc, ok, err = store.GetTenantConfig(ctx, "  ")
	if doc != nil || ok || err != nil {
		t.Fatalf("blank tenant id: doc=%+v ok=%v err=%v", doc, ok, err)
	}
	doc, ok, err = store.GetTenantConfig(ctx, "tenant-missing")
	if doc != nil || ok || err != nil {
		t.Fatalf("missing tenant: doc=%+v ok=%v err=%v", doc, ok, err)
	}

	doc, err = store.UpsertTenantConfig(ctx, "", ScopedConfigDocument{})
	if doc != nil || err != nil {
		t.Fatalf("upsert empty tenant id: doc=%+v err=%v", doc, err)
	}

	payload := ScopedConfigDocument{
		Portal:  &config.PortalConfig{BrandName: "Tenant Brand"},
		OpenAI:  &config.OpenAIConfig{Model: "gpt-tenant"},
		Dify:    &config.DifyConfig{Enabled: true},
		WeKnora: &config.WeKnoraConfig{KnowledgeBaseID: "kb-tenant"},
		SessionRisk: &config.SessionRiskPolicyConfig{
			HighRiskScore: 9,
		},
	}
	created, err := store.UpsertTenantConfig(ctx, " tenant-a ", payload)
	if err != nil {
		t.Fatalf("upsert insert: %v", err)
	}
	if created.TenantID != "tenant-a" {
		t.Fatalf("tenant id = %q want tenant-a", created.TenantID)
	}
	if created.Portal == nil || created.Portal.BrandName != "Tenant Brand" ||
		created.OpenAI == nil || created.OpenAI.Model != "gpt-tenant" ||
		created.Dify == nil || !created.Dify.Enabled ||
		created.WeKnora == nil || created.WeKnora.KnowledgeBaseID != "kb-tenant" ||
		created.SessionRisk == nil || created.SessionRisk.HighRiskScore != 9 {
		t.Fatalf("created doc = %+v want all sections decoded", created)
	}

	fetched, ok, err := store.GetTenantConfig(ctx, "tenant-a")
	if err != nil || !ok {
		t.Fatalf("get after insert: ok=%v err=%v", ok, err)
	}
	if fetched.Portal == nil || fetched.Portal.BrandName != "Tenant Brand" {
		t.Fatalf("fetched portal = %+v", fetched.Portal)
	}

	updated, err := store.UpsertTenantConfig(ctx, "tenant-a", ScopedConfigDocument{
		Dify: &config.DifyConfig{BaseURL: "https://dify.example"},
	})
	if err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	if updated.Dify == nil || updated.Dify.BaseURL != "https://dify.example" || updated.Dify.Enabled {
		t.Fatalf("updated dify = %+v want new values", updated.Dify)
	}
	if updated.Portal == nil || updated.Portal.BrandName != "Tenant Brand" {
		t.Fatalf("updated portal = %+v want previous section preserved", updated.Portal)
	}
	if updated.OpenAI == nil || updated.WeKnora == nil || updated.SessionRisk == nil {
		t.Fatalf("updated doc = %+v want previous sections preserved", updated)
	}
}

func TestCoverage2GormConfigStoreTenantInvalidYAML(t *testing.T) {
	db := openProviderTestDB(t)
	store := NewGormConfigStore(db)
	ctx := context.Background()

	if err := db.Create(&models.TenantConfig{
		TenantID:   "tenant-bad",
		PortalJSON: "brand_name: [unclosed",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, ok, err := store.GetTenantConfig(ctx, "tenant-bad")
	if ok || err == nil {
		t.Fatalf("expected decode error, ok=%v err=%v", ok, err)
	}

	if err := db.Create(&models.TenantConfig{
		TenantID:   "tenant-bad-openai",
		OpenAIJSON: "api_key: [unclosed",
		PortalJSON: `{"brand_name":"ok"}`,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err = store.UpsertTenantConfig(ctx, "tenant-bad-openai", ScopedConfigDocument{
		Dify: &config.DifyConfig{Enabled: true},
	})
	if err == nil {
		t.Fatalf("expected decode error on upsert read-back")
	}
}

func TestCoverage2GormConfigStoreTenantDBErrors(t *testing.T) {
	db := openProviderTestDB(t)
	store := NewGormConfigStore(db)
	ctx := context.Background()

	if err := db.Create(&models.TenantConfig{TenantID: "tenant-a", PortalJSON: `{"brand_name":"ok"}`}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	cov2CloseDB(t, db)

	_, _, err := store.GetTenantConfig(ctx, "tenant-a")
	if err == nil {
		t.Fatalf("expected error from closed db on get")
	}
	_, err = store.UpsertTenantConfig(ctx, "tenant-a", ScopedConfigDocument{Portal: &config.PortalConfig{}})
	if err == nil {
		t.Fatalf("expected error from closed db on upsert")
	}
}

func TestCoverage2GormConfigStoreTenantSaveError(t *testing.T) {
	db := openProviderTestDB(t)
	store := NewGormConfigStore(db)
	ctx := context.Background()

	if err := db.Create(&models.TenantConfig{TenantID: "tenant-a"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("coverage2:fail_update", func(tx *gorm.DB) {
		tx.AddError(errors.New("injected update failure"))
	}); err != nil {
		t.Fatalf("register update callback: %v", err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("coverage2:fail_create", func(tx *gorm.DB) {
		tx.AddError(errors.New("injected create failure"))
	}); err != nil {
		t.Fatalf("register create callback: %v", err)
	}

	_, err := store.UpsertTenantConfig(ctx, "tenant-a", ScopedConfigDocument{Portal: &config.PortalConfig{BrandName: "x"}})
	if err == nil || !strings.Contains(err.Error(), "injected update failure") {
		t.Fatalf("expected injected update failure, got %v", err)
	}
	_, err = store.UpsertTenantConfig(ctx, "tenant-new", ScopedConfigDocument{Portal: &config.PortalConfig{BrandName: "x"}})
	if err == nil || !strings.Contains(err.Error(), "injected create failure") {
		t.Fatalf("expected injected create failure, got %v", err)
	}
}

func TestCoverage2GormConfigStoreWorkspaceRoundTrip(t *testing.T) {
	db := openProviderTestDB(t)
	store := NewGormConfigStore(db)
	ctx := context.Background()

	doc, ok, err := store.GetWorkspaceConfig(ctx, "tenant-a", "")
	if doc != nil || ok || err != nil {
		t.Fatalf("empty workspace id: doc=%+v ok=%v err=%v", doc, ok, err)
	}
	doc, ok, err = store.GetWorkspaceConfig(ctx, "", "workspace-1")
	if doc != nil || ok || err != nil {
		t.Fatalf("empty tenant id: doc=%+v ok=%v err=%v", doc, ok, err)
	}
	doc, ok, err = store.GetWorkspaceConfig(ctx, "tenant-a", "workspace-missing")
	if doc != nil || ok || err != nil {
		t.Fatalf("missing workspace: doc=%+v ok=%v err=%v", doc, ok, err)
	}

	doc, err = store.UpsertWorkspaceConfig(ctx, "tenant-a", "", ScopedConfigDocument{})
	if doc != nil || err != nil {
		t.Fatalf("upsert empty workspace id: doc=%+v err=%v", doc, err)
	}

	payload := ScopedConfigDocument{
		Portal:  &config.PortalConfig{BrandName: "Workspace Brand"},
		OpenAI:  &config.OpenAIConfig{Model: "gpt-workspace"},
		Dify:    &config.DifyConfig{DatasetID: "ds-workspace"},
		WeKnora: &config.WeKnoraConfig{TenantID: "tenant-a"},
		SessionRisk: &config.SessionRiskPolicyConfig{
			MediumRiskScore: 3,
		},
	}
	created, err := store.UpsertWorkspaceConfig(ctx, " tenant-a ", " workspace-1 ", payload)
	if err != nil {
		t.Fatalf("upsert insert: %v", err)
	}
	if created.TenantID != "tenant-a" || created.WorkspaceID != "workspace-1" {
		t.Fatalf("scope = %q/%q want tenant-a/workspace-1", created.TenantID, created.WorkspaceID)
	}
	if created.Portal == nil || created.Portal.BrandName != "Workspace Brand" ||
		created.OpenAI == nil || created.OpenAI.Model != "gpt-workspace" ||
		created.Dify == nil || created.Dify.DatasetID != "ds-workspace" ||
		created.WeKnora == nil || created.WeKnora.TenantID != "tenant-a" ||
		created.SessionRisk == nil || created.SessionRisk.MediumRiskScore != 3 {
		t.Fatalf("created doc = %+v want all sections decoded", created)
	}

	fetched, ok, err := store.GetWorkspaceConfig(ctx, "tenant-a", "workspace-1")
	if err != nil || !ok {
		t.Fatalf("get after insert: ok=%v err=%v", ok, err)
	}
	if fetched.Portal == nil || fetched.Portal.BrandName != "Workspace Brand" {
		t.Fatalf("fetched portal = %+v", fetched.Portal)
	}

	updated, err := store.UpsertWorkspaceConfig(ctx, "tenant-a", "workspace-1", ScopedConfigDocument{
		OpenAI: &config.OpenAIConfig{Model: "gpt-updated"},
	})
	if err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	if updated.OpenAI == nil || updated.OpenAI.Model != "gpt-updated" {
		t.Fatalf("updated openai = %+v", updated.OpenAI)
	}
	if updated.Portal == nil || updated.Dify == nil || updated.WeKnora == nil || updated.SessionRisk == nil {
		t.Fatalf("updated doc = %+v want previous sections preserved", updated)
	}
}

func TestCoverage2GormConfigStoreWorkspaceInvalidYAML(t *testing.T) {
	db := openProviderTestDB(t)
	store := NewGormConfigStore(db)
	ctx := context.Background()

	if err := db.Create(&models.WorkspaceConfig{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-bad",
		WeKnoraJSON: "api_key: {unclosed",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, ok, err := store.GetWorkspaceConfig(ctx, "tenant-a", "workspace-bad")
	if ok || err == nil {
		t.Fatalf("expected decode error, ok=%v err=%v", ok, err)
	}

	if err := db.Create(&models.WorkspaceConfig{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-bad-portal",
		OpenAIJSON:  "model: [unclosed",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err = store.UpsertWorkspaceConfig(ctx, "tenant-a", "workspace-bad-portal", ScopedConfigDocument{
		Portal: &config.PortalConfig{BrandName: "x"},
	})
	if err == nil {
		t.Fatalf("expected decode error on upsert read-back")
	}
}

func TestCoverage2GormConfigStoreWorkspaceDBErrors(t *testing.T) {
	db := openProviderTestDB(t)
	store := NewGormConfigStore(db)
	ctx := context.Background()

	if err := db.Create(&models.WorkspaceConfig{TenantID: "tenant-a", WorkspaceID: "workspace-1"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	cov2CloseDB(t, db)

	_, _, err := store.GetWorkspaceConfig(ctx, "tenant-a", "workspace-1")
	if err == nil {
		t.Fatalf("expected error from closed db on get")
	}
	_, err = store.UpsertWorkspaceConfig(ctx, "tenant-a", "workspace-1", ScopedConfigDocument{})
	if err == nil {
		t.Fatalf("expected error from closed db on upsert")
	}
}

func TestCoverage2GormConfigStoreWorkspaceSaveError(t *testing.T) {
	db := openProviderTestDB(t)
	store := NewGormConfigStore(db)
	ctx := context.Background()

	if err := db.Create(&models.WorkspaceConfig{TenantID: "tenant-a", WorkspaceID: "workspace-1"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("coverage2:fail_ws_update", func(tx *gorm.DB) {
		tx.AddError(errors.New("injected ws update failure"))
	}); err != nil {
		t.Fatalf("register update callback: %v", err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("coverage2:fail_ws_create", func(tx *gorm.DB) {
		tx.AddError(errors.New("injected ws create failure"))
	}); err != nil {
		t.Fatalf("register create callback: %v", err)
	}

	_, err := store.UpsertWorkspaceConfig(ctx, "tenant-a", "workspace-1", ScopedConfigDocument{
		Portal: &config.PortalConfig{BrandName: "x"},
	})
	if err == nil || !strings.Contains(err.Error(), "injected ws update failure") {
		t.Fatalf("expected injected ws update failure, got %v", err)
	}
	_, err = store.UpsertWorkspaceConfig(ctx, "tenant-a", "workspace-new", ScopedConfigDocument{
		Portal: &config.PortalConfig{BrandName: "x"},
	})
	if err == nil || !strings.Contains(err.Error(), "injected ws create failure") {
		t.Fatalf("expected injected ws create failure, got %v", err)
	}
}

func TestCoverage2GormConfigStoreEmptySectionsDecode(t *testing.T) {
	db := openProviderTestDB(t)
	store := NewGormConfigStore(db)
	ctx := context.Background()

	if err := db.Create(&models.TenantConfig{TenantID: "tenant-empty"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	doc, ok, err := store.GetTenantConfig(ctx, "tenant-empty")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if doc.Portal != nil || doc.OpenAI != nil || doc.Dify != nil || doc.WeKnora != nil || doc.SessionRisk != nil {
		t.Fatalf("doc = %+v want nil sections for empty payloads", doc)
	}

	if err := db.Create(&models.WorkspaceConfig{TenantID: "tenant-empty", WorkspaceID: "workspace-empty"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	wsDoc, ok, err := store.GetWorkspaceConfig(ctx, "tenant-empty", "workspace-empty")
	if err != nil || !ok {
		t.Fatalf("get workspace: ok=%v err=%v", ok, err)
	}
	if wsDoc.Portal != nil || wsDoc.OpenAI != nil || wsDoc.Dify != nil || wsDoc.WeKnora != nil || wsDoc.SessionRisk != nil {
		t.Fatalf("ws doc = %+v want nil sections for empty payloads", wsDoc)
	}
}

func TestCoverage2EncodeConfigRoundTrip(t *testing.T) {
	encoded, err := encodeConfig(config.OpenAIConfig{APIKey: "k", Timeout: 1500 * time.Millisecond})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, ok, err := decodeConfig[config.OpenAIConfig](encoded)
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	if decoded.APIKey != "k" || decoded.Timeout != 1500*time.Millisecond {
		t.Fatalf("decoded = %+v want round trip values", decoded)
	}
}
