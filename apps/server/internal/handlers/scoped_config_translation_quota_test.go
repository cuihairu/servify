package handlers

import (
	"context"
	"net/http"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/configscope"

	"github.com/gin-gonic/gin"
)

func sccQuotaLimit(limit int64) *config.TranslationQuotaOverrideConfig {
	return &config.TranslationQuotaOverrideConfig{Limit: limit}
}

func sccTemplateHasCheckID(template scopedConfigVerificationTemplate, id string) bool {
	for _, check := range template.Checks {
		if check.ID == id {
			return true
		}
	}
	return false
}

func TestSccMergedDocumentKeepsTranslationQuota(t *testing.T) {
	current := &configscope.ScopedConfigDocument{TenantID: "tenant-a", TranslationQuota: sccQuotaLimit(100)}
	merged := scopedConfigMergedDocument(current, configscope.ScopedConfigDocument{Portal: sccPortal("Brand")}, "tenant-a", "")
	if merged.TranslationQuota == nil || merged.TranslationQuota.Limit != 100 {
		t.Fatalf("expected current translation_quota kept got %+v", merged.TranslationQuota)
	}
	if merged.Portal == nil {
		t.Fatalf("expected portal payload applied got %+v", merged)
	}

	merged = scopedConfigMergedDocument(current, configscope.ScopedConfigDocument{TranslationQuota: sccQuotaLimit(500)}, "tenant-a", "workspace-1")
	if merged.TranslationQuota == nil || merged.TranslationQuota.Limit != 500 {
		t.Fatalf("expected translation_quota payload to win got %+v", merged.TranslationQuota)
	}
	if merged.TenantID != "tenant-a" || merged.WorkspaceID != "workspace-1" {
		t.Fatalf("expected scope pinned got %+v", merged)
	}
}

func TestSccTranslationQuotaDiffAndSections(t *testing.T) {
	diff := buildScopedConfigDiff(
		&configscope.ScopedConfigDocument{TenantID: "tenant-a"},
		&configscope.ScopedConfigDocument{TenantID: "tenant-a", TranslationQuota: sccQuotaLimit(500)},
	)
	if diff["translation_quota_changed"] != true {
		t.Fatalf("expected translation_quota_changed=true diff=%+v", diff)
	}
	paths := diff["translation_quota_paths"].([]string)
	if len(paths) != 1 || paths[0] != "translation_quota.limit" {
		t.Fatalf("unexpected translation_quota_paths=%v", paths)
	}
	if changes := diff["translation_quota_changes"].([]gin.H); len(changes) == 0 {
		t.Fatalf("expected translation_quota changes diff=%+v", diff)
	}

	sections := scopedConfigChangedSections(
		&configscope.ScopedConfigDocument{},
		&configscope.ScopedConfigDocument{TranslationQuota: sccQuotaLimit(500)},
	)
	if len(sections) != 1 || sections[0] != "translation_quota" {
		t.Fatalf("expected translation_quota section got %v", sections)
	}
	changedPaths := scopedConfigChangedPaths(
		&configscope.ScopedConfigDocument{},
		&configscope.ScopedConfigDocument{TranslationQuota: sccQuotaLimit(500)},
	)
	if len(changedPaths) != 1 || changedPaths[0] != "translation_quota.limit" {
		t.Fatalf("unexpected changed paths %v", changedPaths)
	}
	fromPaths := scopedConfigChangedSectionsFromPaths([]string{"portal.brand_name", "translation_quota.limit"})
	if len(fromPaths) != 2 || fromPaths[0] != "portal" || fromPaths[1] != "translation_quota" {
		t.Fatalf("expected portal and translation_quota sections got %v", fromPaths)
	}
	present := presentSections(&configscope.ScopedConfigDocument{TranslationQuota: sccQuotaLimit(500)})
	if len(present) != 1 || present[0] != "translation_quota" {
		t.Fatalf("expected translation_quota present section got %v", present)
	}
}

func TestSccTranslationQuotaVerificationTemplateAndRisk(t *testing.T) {
	template := scopedConfigVerificationTemplateForDocuments("update",
		&configscope.ScopedConfigDocument{},
		&configscope.ScopedConfigDocument{TranslationQuota: sccQuotaLimit(500)},
	)
	if len(template.ChangedSections) != 1 || template.ChangedSections[0] != "translation_quota" {
		t.Fatalf("expected translation_quota changed section got %v", template.ChangedSections)
	}
	if !sccTemplateHasCheckID(template, "translation_quota_verified") {
		t.Fatalf("expected section verification check got %+v", template.Checks)
	}
	sectionRisk := scopedConfigChangeRiskForTemplate("update", template)
	if sectionRisk.RiskLevel != "medium" || sectionRisk.ApprovalRequired {
		t.Fatalf("expected medium non-approval section risk got %+v", sectionRisk)
	}
	if !sccContainsReason(sectionRisk.RiskReasons, "translation_quota_config_change") {
		t.Fatalf("expected section risk reason got %v", sectionRisk.RiskReasons)
	}

	pathTemplate := scopedConfigVerificationTemplateForChangeSet("update", []string{"translation_quota"}, []string{"translation_quota.limit"})
	if !sccTemplateHasCheckID(pathTemplate, "translation_quota_override_verified") {
		t.Fatalf("expected path verification check got %+v", pathTemplate.Checks)
	}
	if reason := scopedConfigChangeRiskReason(scopedConfigVerificationCheckDefinition{ID: "translation_quota_override_verified"}); reason != "translation_quota_override_change" {
		t.Fatalf("unexpected path risk reason %q", reason)
	}
	pathRisk := scopedConfigChangeRiskForTemplate("update", pathTemplate)
	if pathRisk.RiskLevel != "medium" || pathRisk.ApprovalRequired {
		t.Fatalf("expected medium non-approval path risk got %+v", pathRisk)
	}
	if !sccContainsReason(pathRisk.RiskReasons, "translation_quota_override_change") {
		t.Fatalf("expected path risk reason got %v", pathRisk.RiskReasons)
	}
}

func sccContainsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func TestSccPutConfigKeepsTranslationQuota(t *testing.T) {
	body := `{"change_ref":"CHG-9001","reason":"tenant translation quota","portal":{"brand_name":"Brand"},"translation_quota":{"limit":500}}`
	t.Run("tenant scope", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		store := configscope.NewGormConfigStore(db)
		r := sccRouter(store, sccEmptyAudit(), "tenant-a", "", uint(9))
		w := sccDo(r, http.MethodPut, "/security/config/tenant", body)
		sccAssert(t, w, http.StatusOK, `"translation_quota":{"limit":500}`)
		doc, ok, err := store.GetTenantConfig(context.Background(), "tenant-a")
		if err != nil || !ok {
			t.Fatalf("expected saved tenant config ok=%v err=%v", ok, err)
		}
		if doc.TranslationQuota == nil || doc.TranslationQuota.Limit != 500 {
			t.Fatalf("expected persisted translation_quota limit 500 got %+v", doc.TranslationQuota)
		}
		if doc.Portal == nil {
			t.Fatalf("expected portal kept got %+v", doc)
		}
		if doc.TenantID != "tenant-a" || doc.WorkspaceID != "" {
			t.Fatalf("expected tenant scope kept got %+v", doc)
		}
	})
	t.Run("workspace scope", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		store := configscope.NewGormConfigStore(db)
		r := sccRouter(store, sccEmptyAudit(), "tenant-a", "workspace-1", uint(9))
		w := sccDo(r, http.MethodPut, "/security/config/workspace", body)
		sccAssert(t, w, http.StatusOK, `"translation_quota":{"limit":500}`)
		doc, ok, err := store.GetWorkspaceConfig(context.Background(), "tenant-a", "workspace-1")
		if err != nil || !ok {
			t.Fatalf("expected saved workspace config ok=%v err=%v", ok, err)
		}
		if doc.TranslationQuota == nil || doc.TranslationQuota.Limit != 500 {
			t.Fatalf("expected persisted translation_quota limit 500 got %+v", doc.TranslationQuota)
		}
		if doc.TenantID != "tenant-a" || doc.WorkspaceID != "workspace-1" {
			t.Fatalf("expected workspace scope kept got %+v", doc)
		}
	})
}
