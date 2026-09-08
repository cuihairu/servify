package configscope

import (
	"context"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newCov3DB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:configscope_cov3_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.TenantConfig{}, &models.WorkspaceConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestCov3TenantRowToDocumentInvalidSections(t *testing.T) {
	store := NewGormConfigStore(newCov3DB(t))

	if err := store.db.Create(&models.TenantConfig{
		TenantID: "tenant-dify-bad",
		DifyJSON: "\tinvalid: [yaml",
	}).Error; err != nil {
		t.Fatalf("seed dify row: %v", err)
	}
	if _, _, err := store.GetTenantConfig(context.Background(), "tenant-dify-bad"); err == nil {
		t.Fatal("expected dify decode error")
	}

	if err := store.db.Create(&models.TenantConfig{
		TenantID:    "tenant-weknora-bad",
		WeKnoraJSON: "\tinvalid: {yaml",
	}).Error; err != nil {
		t.Fatalf("seed weknora row: %v", err)
	}
	if _, _, err := store.GetTenantConfig(context.Background(), "tenant-weknora-bad"); err == nil {
		t.Fatal("expected weknora decode error")
	}

	if err := store.db.Create(&models.TenantConfig{
		TenantID:        "tenant-risk-bad",
		SessionRiskJSON: "\tinvalid: nope",
	}).Error; err != nil {
		t.Fatalf("seed session risk row: %v", err)
	}
	if _, _, err := store.GetTenantConfig(context.Background(), "tenant-risk-bad"); err == nil {
		t.Fatal("expected session risk decode error")
	}
}

func TestCov3WorkspaceRowToDocumentInvalidSections(t *testing.T) {
	store := NewGormConfigStore(newCov3DB(t))

	seed := func(tenantID, workspaceID, column, body string) {
		t.Helper()
		row := &models.WorkspaceConfig{TenantID: tenantID, WorkspaceID: workspaceID}
		switch column {
		case "portal":
			row.PortalJSON = body
		case "openai":
			row.OpenAIJSON = body
		case "dify":
			row.DifyJSON = body
		case "session_risk":
			row.SessionRiskJSON = body
		}
		if err := store.db.Create(row).Error; err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}

	seed("tenant-w", "ws-portal", "portal", "\tinvalid: portal")
	if _, _, err := store.GetWorkspaceConfig(context.Background(), "tenant-w", "ws-portal"); err == nil {
		t.Fatal("expected portal decode error")
	}

	seed("tenant-w", "ws-openai", "openai", "\tinvalid: [")
	if _, _, err := store.GetWorkspaceConfig(context.Background(), "tenant-w", "ws-openai"); err == nil {
		t.Fatal("expected openai decode error")
	}

	seed("tenant-w", "ws-dify", "dify", "\tinvalid: {")
	if _, _, err := store.GetWorkspaceConfig(context.Background(), "tenant-w", "ws-dify"); err == nil {
		t.Fatal("expected dify decode error")
	}

	seed("tenant-w", "ws-risk", "session_risk", "\tbad yaml")
	if _, _, err := store.GetWorkspaceConfig(context.Background(), "tenant-w", "ws-risk"); err == nil {
		t.Fatal("expected session risk decode error")
	}
}

func TestCov3MergeOpenAIConfigOverlayValues(t *testing.T) {
	base := config.OpenAIConfig{APIKey: "base-key", BaseURL: "http://base", Model: "base-model"}
	overlay := config.OpenAIConfig{
		APIKey:      "overlay-key",
		BaseURL:     " http://overlay ",
		Model:       "overlay-model",
		Temperature: 0.7,
		MaxTokens:   1234,
		Timeout:     9,
	}

	merged := mergeOpenAIConfig(base, overlay)
	if merged.APIKey != "overlay-key" || merged.BaseURL != " http://overlay " || merged.Model != "overlay-model" {
		t.Fatalf("string overlays not applied: %+v", merged)
	}
	if merged.Temperature != 0.7 || merged.MaxTokens != 1234 || merged.Timeout != 9 {
		t.Fatalf("numeric overlays not applied: %+v", merged)
	}

	kept := mergeOpenAIConfig(base, config.OpenAIConfig{})
	if kept != base {
		t.Fatalf("empty overlay should keep base config: %+v", kept)
	}

	blankKept := mergeOpenAIConfig(base, config.OpenAIConfig{APIKey: "   ", BaseURL: "", Model: " "})
	if blankKept.APIKey != "base-key" || blankKept.Model != "base-model" {
		t.Fatalf("blank overlay values should be ignored: %+v", blankKept)
	}
}
