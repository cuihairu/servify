package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	auditplatform "servify/apps/server/internal/platform/audit"
	"servify/apps/server/internal/platform/configscope"

	"github.com/gin-gonic/gin"
)

func sccPortal(brand string) *config.PortalConfig {
	return &config.PortalConfig{BrandName: brand}
}

func sccBoolPtr(v bool) *bool { return &v }

func sccContextWithQuery(target string) *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return c
}

func TestSccHistoryFiltersFromRequest(t *testing.T) {
	if f := scopedConfigHistoryFiltersFromRequest(nil); f.Action != "" || f.NeedsAction != nil {
		t.Fatalf("expected zero filters got %+v", f)
	}
	f := scopedConfigHistoryFiltersFromRequest(sccContextWithQuery("/?needs_action=false"))
	if f.NeedsAction == nil || *f.NeedsAction {
		t.Fatalf("expected needs_action=false got %+v", f)
	}
	f = scopedConfigHistoryFiltersFromRequest(sccContextWithQuery("/?needs_action=maybe"))
	if f.NeedsAction != nil {
		t.Fatalf("expected nil needs_action got %+v", f)
	}
	f = scopedConfigHistoryFiltersFromRequest(sccContextWithQuery("/?action=update&governance_status=verified&risk_level=high&approval_status=approved&verification_status=passed&needs_action=true"))
	if f.Action != "update" || f.GovernanceStatus != "verified" || f.RiskLevel != "high" || f.ApprovalStatus != "approved" || f.VerificationStatus != "passed" || f.NeedsAction == nil || !*f.NeedsAction {
		t.Fatalf("unexpected filters %+v", f)
	}
	response := f.response()
	for _, key := range []string{"action", "governance_status", "risk_level", "approval_status", "verification_status", "needs_action"} {
		if _, ok := response[key]; !ok {
			t.Fatalf("expected key %q in %+v", key, response)
		}
	}
	if r := (scopedConfigHistoryFilters{}).response(); r != nil {
		t.Fatalf("expected nil response got %+v", r)
	}
}

func TestSccFilterScopedConfigHistoryResponses(t *testing.T) {
	if got := filterScopedConfigHistoryResponses(nil, scopedConfigHistoryFilters{RiskLevel: "high"}); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	items := []gin.H{{"governance_status": "verified"}}
	if got := filterScopedConfigHistoryResponses(items, scopedConfigHistoryFilters{}); len(got) != 1 {
		t.Fatalf("expected passthrough got %+v", got)
	}
	riskItem := gin.H{"change_risk": gin.H{"risk_level": "low"}}
	if got := filterScopedConfigHistoryResponses([]gin.H{riskItem}, scopedConfigHistoryFilters{RiskLevel: "high"}); len(got) != 0 {
		t.Fatalf("expected filtered out got %+v", got)
	}
	approvalItem := gin.H{"approval_policy": map[string]interface{}{"approval_status": "missing"}}
	if got := filterScopedConfigHistoryResponses([]gin.H{approvalItem}, scopedConfigHistoryFilters{ApprovalStatus: "approved"}); len(got) != 0 {
		t.Fatalf("expected filtered out got %+v", got)
	}
	verificationItem := gin.H{"verification_status": "pending"}
	if got := filterScopedConfigHistoryResponses([]gin.H{verificationItem}, scopedConfigHistoryFilters{VerificationStatus: "passed"}); len(got) != 0 {
		t.Fatalf("expected filtered out got %+v", got)
	}
	completeItem := gin.H{"governance_status": "verified"}
	if got := filterScopedConfigHistoryResponses([]gin.H{completeItem}, scopedConfigHistoryFilters{NeedsAction: sccBoolPtr(true)}); len(got) != 0 {
		t.Fatalf("expected filtered out got %+v", got)
	}
	if got := filterScopedConfigHistoryResponses([]gin.H{completeItem}, scopedConfigHistoryFilters{GovernanceStatus: "verified", RiskLevel: "", ApprovalStatus: "", VerificationStatus: ""}); len(got) != 1 {
		t.Fatalf("expected kept item got %+v", got)
	}
}

func TestSccHistoryItemHelpers(t *testing.T) {
	if got := historyItemString(nil, "governance_status"); got != "" {
		t.Fatalf("expected empty got %q", got)
	}
	item := gin.H{"change_risk": gin.H{"risk_level": "high"}}
	if got := historyItemNestedString(item, "change_risk", "risk_level"); got != "high" {
		t.Fatalf("expected high got %q", got)
	}
	mapItem := gin.H{"approval_policy": map[string]interface{}{"approval_status": "approved"}}
	if got := historyItemNestedString(mapItem, "approval_policy", "approval_status"); got != "approved" {
		t.Fatalf("expected approved got %q", got)
	}
	if got := historyItemNestedString(gin.H{"approval_policy": 42}, "approval_policy", "approval_status"); got != "" {
		t.Fatalf("expected empty got %q", got)
	}
	counter := gin.H{}
	incrementHistorySummaryCounter(counter, "   ")
	if counter["unknown"] != 1 {
		t.Fatalf("expected unknown counter got %+v", counter)
	}
	summary := summarizeScopedConfigHistoryResponses(nil)
	if summary["total_items"] != 0 {
		t.Fatalf("expected empty summary got %+v", summary)
	}
}

func TestSccSliceAndPageHelpers(t *testing.T) {
	if got := sliceScopedConfigHistoryItemsPage(nil, 1, 20); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	items := []gin.H{{"a": 1}, {"b": 2}, {"c": 3}}
	if got := sliceScopedConfigHistoryItemsPage(items, 2, 2); len(got) != 1 {
		t.Fatalf("expected one item got %+v", got)
	}
	if got := sliceScopedConfigHistoryItemsPage(items, 9, 2); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	logs := []models.AuditLog{{ID: 1}, {ID: 2}, {ID: 3}}
	if got := sliceAuditLogsPage(logs, 1, 2); len(got) != 2 {
		t.Fatalf("expected two logs got %+v", got)
	}
	if got := sliceAuditLogsPage(logs, 2, 2); len(got) != 1 {
		t.Fatalf("expected one log got %+v", got)
	}
	if got := sliceAuditLogsPage(logs, 5, 2); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if page, size := normalizeAuditPage(0, 0); page != 1 || size != 20 {
		t.Fatalf("unexpected normalize %+v %+v", page, size)
	}
	if page, size := normalizeAuditPage(1, 500); page != 1 || size != 200 {
		t.Fatalf("unexpected normalize %+v %+v", page, size)
	}
	if page, size := normalizeAuditPage(3, 50); page != 3 || size != 50 {
		t.Fatalf("unexpected normalize %+v %+v", page, size)
	}
}

func TestSccFilterScopedConfigHistoryEntries(t *testing.T) {
	items := []models.AuditLog{
		{ID: 1, Action: "scoped_config.tenant.update"},
		{ID: 2, Action: "scoped_config.tenant.rollback"},
	}
	if got := filterScopedConfigHistoryEntries(items, ""); len(got) != 2 {
		t.Fatalf("expected passthrough got %+v", got)
	}
	if got := filterScopedConfigHistoryEntries(items, "scoped_config.tenant.update"); len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("expected full action match got %+v", got)
	}
	if got := filterScopedConfigHistoryEntries(items, "update"); len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("expected operation match got %+v", got)
	}
	if got := filterScopedConfigHistoryEntries(items, "approve"); got != nil && len(got) != 0 {
		t.Fatalf("expected no matches got %+v", got)
	}
}

func TestSccUintValueFromAny(t *testing.T) {
	cases := []struct {
		value interface{}
		want  uint
	}{
		{float64(-1), 0},
		{float64(7.0), 7},
		{int(-2), 0},
		{int(3), 3},
		{int64(-3), 0},
		{int64(4), 4},
		{uint(5), 5},
		{uint64(6), 6},
		{"", 0},
		{"nope", 0},
		{"8", 8},
		{" 8 ", 8},
		{"0", 0},
		{true, 0},
		{nil, 0},
	}
	for _, tc := range cases {
		if got := uintValueFromAny(tc.value); got != tc.want {
			t.Fatalf("uintValueFromAny(%v) = %d want %d", tc.value, got, tc.want)
		}
	}
}

func TestSccContextUserID(t *testing.T) {
	if got := scopedConfigContextUserID(nil); got != nil {
		t.Fatalf("expected nil got %v", got)
	}
	c, _ := sccGinContext(http.MethodGet, "/", nil)
	if got := scopedConfigContextUserID(c); got != nil {
		t.Fatalf("expected nil got %v", got)
	}
	c.Set("user_id", uint(11))
	if got := scopedConfigContextUserID(c); got == nil || *got != 11 {
		t.Fatalf("expected 11 got %v", got)
	}
	c.Set("user_id", 22)
	if got := scopedConfigContextUserID(c); got == nil || *got != 22 {
		t.Fatalf("expected 22 got %v", got)
	}
	c.Set("user_id", 0)
	if got := scopedConfigContextUserID(c); got != nil {
		t.Fatalf("expected nil got %v", got)
	}
	c.Set("user_id", -4)
	if got := scopedConfigContextUserID(c); got != nil {
		t.Fatalf("expected nil got %v", got)
	}
	c.Set("user_id", "eleven")
	if got := scopedConfigContextUserID(c); got != nil {
		t.Fatalf("expected nil got %v", got)
	}
}

func TestSccVerificationStatusHelpers(t *testing.T) {
	if got := normalizeScopedConfigVerificationStatus(" PASSED "); got != "passed" {
		t.Fatalf("expected passed got %q", got)
	}
	if got := normalizeScopedConfigVerificationStatus("Failed"); got != "failed" {
		t.Fatalf("expected failed got %q", got)
	}
	if got := normalizeScopedConfigVerificationStatus("weird"); got != "" {
		t.Fatalf("expected empty got %q", got)
	}
	if got := normalizeScopedConfigVerificationCheckStatus(" Skipped "); got != "skipped" {
		t.Fatalf("expected skipped got %q", got)
	}
	if got := normalizeScopedConfigVerificationCheckStatus("zzz"); got != "" {
		t.Fatalf("expected empty got %q", got)
	}
	if got := sanitizeStringSlice(nil); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := sanitizeStringSlice([]string{"  ", ""}); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := sanitizeStringSlice([]string{" a ", "", "b"}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected slice %+v", got)
	}
	if got := sanitizeScopedConfigVerificationChecks(nil); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	badChecks := []scopedConfigVerificationCheckResult{
		{ID: "", Status: "passed"},
		{ID: "ok1", Status: "bogus"},
		{ID: "ok1", Status: "passed"},
		{ID: "ok1", Status: "failed"},
		{ID: "ok2", Status: "failed", Notes: " n "},
	}
	got := sanitizeScopedConfigVerificationChecks(badChecks)
	if len(got) != 2 || got[0].ID != "ok1" || got[1].ID != "ok2" || got[1].Notes != "n" {
		t.Fatalf("unexpected checks %+v", got)
	}
	if got := sanitizeScopedConfigVerificationChecks([]scopedConfigVerificationCheckResult{{ID: "", Status: ""}}); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := scopedConfigVerificationChecksFromAny(42); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	fromAny := scopedConfigVerificationChecksFromAny([]interface{}{
		"not-a-map",
		map[string]interface{}{"id": "c1", "status": "passed", "notes": "note"},
	})
	if len(fromAny) != 1 || fromAny[0].ID != "c1" || fromAny[0].Status != "passed" {
		t.Fatalf("unexpected checks %+v", fromAny)
	}
	if got := scopedConfigVerificationChecksFromAny([]interface{}{map[string]interface{}{"id": "c2", "status": "bogus"}}); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := scopedConfigVerificationStatus("approve", nil); got != "not_applicable" {
		t.Fatalf("expected not_applicable got %q", got)
	}
	if got := scopedConfigVerificationStatus("update", nil); got != "pending" {
		t.Fatalf("expected pending got %q", got)
	}
	if got := scopedConfigVerificationStatus("rollback", []scopedConfigVerificationRecord{{Status: "passed"}}); got != "passed" {
		t.Fatalf("expected passed got %q", got)
	}
}

func TestSccExtractChangeControl(t *testing.T) {
	if got := extractScopedConfigChangeControl("   "); got.ChangeRef != "" || got.Reason != "" {
		t.Fatalf("expected zero got %+v", got)
	}
	if got := extractScopedConfigChangeControl("not-json"); got.ChangeRef != "" {
		t.Fatalf("expected zero got %+v", got)
	}
	partial := extractScopedConfigChangeControl(`{"reason":"only reason"}`)
	if partial.ChangeRef != "" || partial.Reason != "only reason" {
		t.Fatalf("unexpected partial %+v", partial)
	}
	notMap := extractScopedConfigChangeControl(`{"_audit":"nope"}`)
	if notMap.ChangeRef != "" || notMap.Reason != "" {
		t.Fatalf("unexpected not-map result %+v", notMap)
	}
	merged := extractScopedConfigChangeControl(`{"_audit":{"change_ref":"CR","reason":"RR","approval_ref":"AR"}}`)
	if merged.ChangeRef != "CR" || merged.Reason != "RR" || merged.ApprovalRef != "AR" {
		t.Fatalf("unexpected merged %+v", merged)
	}
	if got := payloadStringValue(nil, "notes"); got != "" {
		t.Fatalf("expected empty got %q", got)
	}
	if got := payloadStringValue(map[string]interface{}{"notes": 42}, "notes"); got != "" {
		t.Fatalf("expected empty got %q", got)
	}
}

func TestSccExtractApprovalAndVerification(t *testing.T) {
	if _, ok := extractScopedConfigApproval(models.AuditLog{Action: "scoped_config.tenant.update"}); ok {
		t.Fatalf("expected non-approval rejection")
	}
	if _, ok := extractScopedConfigApproval(models.AuditLog{Action: "scoped_config.tenant.approve", RequestJSON: "not-json"}); ok {
		t.Fatalf("expected malformed rejection")
	}
	if _, ok := extractScopedConfigApproval(models.AuditLog{Action: "scoped_config.tenant.approve", RequestJSON: `{"change_ref":"CR","reason":"r"}`}); ok {
		t.Fatalf("expected missing approval_ref rejection")
	}
	approval, ok := extractScopedConfigApproval(models.AuditLog{
		ID:          3,
		Action:      "scoped_config.tenant.approve",
		RequestJSON: `{"change_ref":"CR","reason":"r","approval_ref":"AR","notes":"n","evidence":["e1","e2"]}`,
	})
	if !ok || approval.AuditID != 3 || approval.Notes != "n" || len(approval.Evidence) != 2 || approval.ChangeControl.ApprovalRef != "AR" {
		t.Fatalf("unexpected approval %+v", approval)
	}
	if _, ok := extractScopedConfigVerification(models.AuditLog{Action: "scoped_config.tenant.update"}); ok {
		t.Fatalf("expected non-verification rejection")
	}
	if _, ok := extractScopedConfigVerification(models.AuditLog{Action: "scoped_config.tenant.verify", RequestJSON: "not-json"}); ok {
		t.Fatalf("expected malformed rejection")
	}
	if _, ok := extractScopedConfigVerification(models.AuditLog{Action: "scoped_config.tenant.verify", RequestJSON: `{"status":"passed"}`}); ok {
		t.Fatalf("expected missing source rejection")
	}
	if _, ok := extractScopedConfigVerification(models.AuditLog{Action: "scoped_config.tenant.verify", RequestJSON: `{"source_audit_id":4}`}); ok {
		t.Fatalf("expected missing status rejection")
	}
	verification, ok := extractScopedConfigVerification(models.AuditLog{
		ID:          4,
		Action:      "scoped_config.tenant.verify",
		RequestJSON: `{"source_audit_id":9,"status":"failed","notes":"n","checks":[{"id":"c","status":"failed"}],"change_ref":"CR","reason":"r"}`,
	})
	if !ok || verification.SourceAuditID != 9 || verification.Status != "failed" || len(verification.Checks) != 1 {
		t.Fatalf("unexpected verification %+v", verification)
	}
}

func TestSccExtractChangedPathsAndAuditMeta(t *testing.T) {
	if got := extractScopedConfigChangedPaths(""); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := extractScopedConfigChangedPaths("not-json"); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := extractScopedConfigChangedPaths(`{"changed_paths":[" portal.a ", "", "portal.a", "weknora.b"]}`); len(got) != 2 || got[0] != "portal.a" || got[1] != "weknora.b" {
		t.Fatalf("unexpected paths %+v", got)
	}
	setScopedConfigAuditMeta(nil, "tenant", "tenant-a", "", "update", scopedConfigChangeControl{})
}

func TestSccSortDescHelpers(t *testing.T) {
	base := time.Now()
	logs := []models.AuditLog{{ID: 1, CreatedAt: base}, {ID: 2, CreatedAt: base}}
	sortAuditLogsDesc(logs)
	if logs[0].ID != 2 {
		t.Fatalf("expected id tiebreak desc got %+v", logs)
	}
	logs = []models.AuditLog{{ID: 1, CreatedAt: base.Add(time.Minute)}, {ID: 2, CreatedAt: base}}
	sortAuditLogsDesc(logs)
	if logs[0].ID != 1 {
		t.Fatalf("expected newest first got %+v", logs)
	}
	verifications := []scopedConfigVerificationRecord{{AuditID: 1, CreatedAt: base}, {AuditID: 2, CreatedAt: base}}
	sortScopedConfigVerificationsDesc(verifications)
	if verifications[0].AuditID != 2 {
		t.Fatalf("expected id tiebreak desc got %+v", verifications)
	}
	verifications = []scopedConfigVerificationRecord{{AuditID: 1, CreatedAt: base.Add(time.Minute)}, {AuditID: 2, CreatedAt: base}}
	sortScopedConfigVerificationsDesc(verifications)
	if verifications[0].AuditID != 1 {
		t.Fatalf("expected newest first got %+v", verifications)
	}
	approvals := []scopedConfigApprovalRecord{{AuditID: 1, CreatedAt: base}, {AuditID: 2, CreatedAt: base}}
	sortScopedConfigApprovalsDesc(approvals)
	if approvals[0].AuditID != 2 {
		t.Fatalf("expected id tiebreak desc got %+v", approvals)
	}
	approvals = []scopedConfigApprovalRecord{{AuditID: 1, CreatedAt: base.Add(time.Minute)}, {AuditID: 2, CreatedAt: base}}
	sortScopedConfigApprovalsDesc(approvals)
	if approvals[0].AuditID != 1 {
		t.Fatalf("expected newest first got %+v", approvals)
	}
}

func TestSccChangedPathsHelpers(t *testing.T) {
	before := configscope.ScopedConfigDocument{Portal: sccPortal("Old")}
	if got := scopedConfigChangedSections(&before, nil); len(got) != 1 || got[0] != "portal" {
		t.Fatalf("unexpected sections %+v", got)
	}
	empty := configscope.ScopedConfigDocument{}
	sections := scopedConfigChangedSections(nil, &empty)
	if len(sections) != 0 {
		t.Fatalf("expected no sections got %+v", sections)
	}
	if got := scopedConfigChangedPaths(&before, nil); len(got) != 1 || got[0] != "portal.brand_name" {
		t.Fatalf("unexpected paths %+v", got)
	}
	if got := normalizeScopedConfigChangedPaths(nil); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := normalizeScopedConfigChangedPaths([]string{"  ", "", "a", "a"}); len(got) != 1 || got[0] != "a" {
		t.Fatalf("unexpected paths %+v", got)
	}
	if got := normalizeScopedConfigChangedPaths([]string{" ", ""}); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := scopedConfigChangedSectionsFromPaths(nil); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := scopedConfigChangedSectionsFromPaths([]string{"", "other.x", "portal.a", "portal.b", "zzz"}); len(got) != 1 || got[0] != "portal" {
		t.Fatalf("unexpected sections %+v", got)
	}
	if got := scopedConfigChangedSectionsFromPaths([]string{"other.x", "zzz"}); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := changedPathsForPrefixes(nil, "portal.brand_name"); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := changedPathsForPrefixes([]string{"portal.brand_name"}, ""); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := changedPathsForPrefixes([]string{"portal", "portal.brand_name", "openai.model"}, "portal"); len(got) != 2 {
		t.Fatalf("expected exact and nested match got %+v", got)
	}
}

func TestSccVerificationTemplates(t *testing.T) {
	if got := scopedConfigVerificationTemplateFromAudit(nil); got.Operation != "" || got.Checks != nil {
		t.Fatalf("expected zero template got %+v", got)
	}
	approveOnly := scopedConfigVerificationTemplateFromAudit(&models.AuditLog{Action: "scoped_config.tenant.approve"})
	if approveOnly.Operation != "approve" || approveOnly.Checks != nil {
		t.Fatalf("expected operation-only template got %+v", approveOnly)
	}
	if got := scopedConfigVerificationTemplateForHistoryEntry(nil, nil, nil); got.Operation != "" {
		t.Fatalf("expected zero template got %+v", got)
	}
	entry := &models.AuditLog{Action: "scoped_config.tenant.update", BeforeJSON: `{}`, AfterJSON: `{"portal":{"brand_name":"X"}}`}
	if got := scopedConfigVerificationTemplateForHistoryEntry(entry, nil, nil); got.Operation != "update" {
		t.Fatalf("expected audit template fallback got %+v", got)
	}
	entry.BeforeJSON = ""
	entry.RequestJSON = `{"changed_paths":["portal.brand_name"]}`
	fromPaths := scopedConfigVerificationTemplateForHistoryEntry(entry, sccPortalDoc("current"), sccPortalDoc("snapshot"))
	if len(fromPaths.ChangedPaths) != 1 || fromPaths.ChangedPaths[0] != "portal.brand_name" {
		t.Fatalf("expected change-set template got %+v", fromPaths)
	}
	entry.RequestJSON = `{}`
	fromDocs := scopedConfigVerificationTemplateForHistoryEntry(entry, sccPortalDoc("current"), sccPortalDoc("snapshot"))
	if len(fromDocs.ChangedPaths) != 1 || fromDocs.ChangedPaths[0] != "portal.brand_name" {
		t.Fatalf("expected document template got %+v", fromDocs)
	}
	if got := scopedConfigDocumentFromAuditJSON("not-json"); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := scopedConfigDocumentFromAuditJSON("   "); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}

	template := scopedConfigVerificationTemplateForChangeSet("rollback", []string{"portal", "openai", "weknora", "session_risk"}, []string{
		"portal.brand_name", "portal.logo_url", "portal.primary_color", "portal.secondary_color", "portal.support_email",
		"portal.default_locale", "portal.locales",
		"openai.api_key", "openai.base_url", "openai.timeout",
		"openai.model", "openai.temperature", "openai.max_tokens",
		"weknora.api_key", "weknora.base_url", "weknora.timeout", "weknora.max_retries",
		"weknora.enabled", "weknora.tenant_id", "weknora.knowledge_base_id", "weknora.search",
		"weknora.health_check",
		"session_risk.medium_risk_score", "session_risk.high_risk_score",
		"session_risk.hot_refresh_window_minutes", "session_risk.recent_refresh_window_minutes", "session_risk.today_refresh_window_hours",
		"session_risk.rapid_change_window_hours", "session_risk.stale_activity_window_days",
		"session_risk.multi_public_ip_threshold", "session_risk.many_sessions_threshold", "session_risk.hot_refresh_family_threshold",
	})
	wantIDs := []string{
		"change_scope_reviewed", "runtime_effect_confirmed", "rollback_baseline_restored",
		"portal_render_verified", "openai_provider_verified", "weknora_provider_verified", "session_risk_policy_verified",
		"portal_public_surface_verified", "portal_locale_contract_verified",
		"openai_provider_endpoint_verified", "openai_model_contract_verified",
		"weknora_provider_endpoint_verified", "weknora_knowledge_mapping_verified", "weknora_health_check_verified",
		"session_risk_score_threshold_verified", "session_risk_window_verified", "session_risk_concurrency_threshold_verified",
	}
	seen := map[string]bool{}
	for _, check := range template.Checks {
		seen[check.ID] = true
	}
	for _, id := range wantIDs {
		if !seen[id] {
			t.Fatalf("expected check %q in template got %+v", id, template.Checks)
		}
	}
}

func sccPortalDoc(brand string) *configscope.ScopedConfigDocument {
	return &configscope.ScopedConfigDocument{Portal: sccPortal(brand)}
}

func TestSccChangeRiskReason(t *testing.T) {
	cases := map[string]string{
		"rollback_baseline_restored":                  "rollback_operation",
		"portal_public_surface_verified":              "portal_public_surface_change",
		"portal_locale_contract_verified":             "portal_locale_change",
		"openai_provider_endpoint_verified":           "openai_provider_endpoint_change",
		"openai_model_contract_verified":              "openai_model_contract_change",
		"weknora_provider_endpoint_verified":          "weknora_provider_endpoint_change",
		"weknora_knowledge_mapping_verified":          "weknora_knowledge_mapping_change",
		"weknora_health_check_verified":               "weknora_health_policy_change",
		"session_risk_score_threshold_verified":       "session_risk_threshold_change",
		"session_risk_window_verified":                "session_risk_window_change",
		"session_risk_concurrency_threshold_verified": "session_risk_concurrency_change",
	}
	for id, want := range cases {
		if got := scopedConfigChangeRiskReason(scopedConfigVerificationCheckDefinition{ID: id}); got != want {
			t.Fatalf("risk reason for %q = %q want %q", id, got, want)
		}
	}
	if got := scopedConfigChangeRiskReason(scopedConfigVerificationCheckDefinition{ID: "other", Section: "custom"}); got != "custom_config_change" {
		t.Fatalf("expected section reason got %q", got)
	}
	if got := scopedConfigChangeRiskReason(scopedConfigVerificationCheckDefinition{ID: "other"}); got != "" {
		t.Fatalf("expected empty reason got %q", got)
	}
	if got := normalizeScopedConfigRiskReasons(nil); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := normalizeScopedConfigRiskReasons([]string{"", "  ", "b", "b", "a"}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected reasons %+v", got)
	}
	if got := normalizeScopedConfigRiskReasons([]string{" ", ""}); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	risk := scopedConfigChangeRiskForTemplate("update", scopedConfigVerificationTemplate{
		Checks: []scopedConfigVerificationCheckDefinition{{ID: "custom_check", Section: "custom", RiskLevel: "medium"}},
	})
	if risk.RiskLevel != "medium" || risk.ApprovalRequired {
		t.Fatalf("expected medium risk without approval got %+v", risk)
	}
}

func TestSccGovernanceHelpers(t *testing.T) {
	highRisk := scopedConfigChangeRisk{RiskLevel: "high", ApprovalRequired: true}
	if got := scopedConfigGovernanceStatus("update", highRisk, scopedConfigChangeControl{ApprovalRef: "AR"}, nil, nil); got != "awaiting_approval" {
		t.Fatalf("expected awaiting_approval got %q", got)
	}
	if got := scopedConfigGovernanceStatus("update", scopedConfigChangeRisk{}, scopedConfigChangeControl{}, nil, []scopedConfigVerificationRecord{{Status: "weird"}}); got != "awaiting_verification" {
		t.Fatalf("expected awaiting_verification got %q", got)
	}
	if got := scopedConfigGovernanceStatus("approve", highRisk, scopedConfigChangeControl{ApprovalRef: "AR"}, &scopedConfigApprovalRecord{}, nil); got != "approved" {
		t.Fatalf("expected approved got %q", got)
	}
	if got := scopedConfigGovernanceStatus("approve", scopedConfigChangeRisk{}, scopedConfigChangeControl{}, nil, nil); got != "not_required" {
		t.Fatalf("expected not_required got %q", got)
	}
	if got := scopedConfigGovernancePhase("mystery"); got != "verification" {
		t.Fatalf("expected verification got %q", got)
	}
	if got := requiredScopedConfigVerificationCheckIDs(scopedConfigVerificationTemplate{}); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := requiredScopedConfigVerificationCheckIDs(scopedConfigVerificationTemplate{Checks: []scopedConfigVerificationCheckDefinition{{ID: "opt", Required: false}}}); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := latestScopedConfigApprovalResponse(nil); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := latestScopedConfigApprovalResponse(&scopedConfigApprovalRecord{AuditID: 3}); got == nil || got["audit_id"] != uint(3) {
		t.Fatalf("unexpected response %+v", got)
	}
}

func TestSccValidateSubmission(t *testing.T) {
	if r := validateScopedConfigVerificationSubmission(scopedConfigVerificationRequest{Status: "failed"}, scopedConfigVerificationTemplate{}); r != nil {
		t.Fatalf("expected nil for empty template got %+v", r)
	}
	template := scopedConfigVerificationTemplate{Checks: []scopedConfigVerificationCheckDefinition{
		{ID: "req", Required: true},
		{ID: "opt", Required: false},
	}}
	if r := validateScopedConfigVerificationSubmission(scopedConfigVerificationRequest{Status: "failed"}, template); r == nil || r.Error != "Verification checks required" {
		t.Fatalf("expected checks required got %+v", r)
	}
	if r := validateScopedConfigVerificationSubmission(scopedConfigVerificationRequest{Status: "failed", Checks: []scopedConfigVerificationCheckResult{{ID: "unknown", Status: "failed"}}}, template); r == nil || r.Error != "Invalid verification check" {
		t.Fatalf("expected invalid check got %+v", r)
	}
	passed := scopedConfigVerificationRequest{Status: "passed", Checks: []scopedConfigVerificationCheckResult{
		{ID: "req", Status: "passed"},
		{ID: "opt", Status: "skipped"},
	}}
	if r := validateScopedConfigVerificationSubmission(passed, template); r != nil {
		t.Fatalf("expected acceptance got %+v", r)
	}
	if r := validateScopedConfigVerificationSubmission(scopedConfigVerificationRequest{Status: "passed", Checks: []scopedConfigVerificationCheckResult{{ID: "req", Status: "failed"}}}, template); r == nil || r.Error != "Verification checks incomplete" {
		t.Fatalf("expected incomplete got %+v", r)
	}
	if r := validateScopedConfigVerificationSubmission(scopedConfigVerificationRequest{Status: "failed", Checks: []scopedConfigVerificationCheckResult{{ID: "req", Status: "passed"}}}, template); r == nil || r.Error != "Failed verification check required" {
		t.Fatalf("expected failed check required got %+v", r)
	}
	if r := validateScopedConfigVerificationSubmission(scopedConfigVerificationRequest{Status: "failed", Checks: []scopedConfigVerificationCheckResult{{ID: "req", Status: "failed"}}}, template); r != nil {
		t.Fatalf("expected acceptance got %+v", r)
	}
	if r := validateScopedConfigVerificationSubmission(scopedConfigVerificationRequest{Status: ""}, template); r == nil || r.Error != "Invalid verification status" {
		t.Fatalf("expected invalid status got %+v", r)
	}
}

func TestSccDiffHelpers(t *testing.T) {
	if got := diffChangeType(nil, "a"); got != "added" {
		t.Fatalf("expected added got %q", got)
	}
	if got := diffChangeType("a", nil); got != "removed" {
		t.Fatalf("expected removed got %q", got)
	}
	if got := diffChangeType("a", "b"); got != "updated" {
		t.Fatalf("expected updated got %q", got)
	}
	if got := jsonValue(nil); got != "" {
		t.Fatalf("expected empty got %q", got)
	}
	if got := jsonValue(make(chan int)); got != "" {
		t.Fatalf("expected empty got %q", got)
	}
	if got := presentSections(nil); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := normalizeDiffJSONValue(map[string]interface{}{"a": nil}); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	if got := normalizeDiffJSONValue([]interface{}{}); len(got.([]interface{})) != 0 {
		t.Fatalf("expected empty slice got %+v", got)
	}
	if got := normalizeDiffJSONValue([]interface{}{1, nil, map[string]interface{}{"x": nil}}); len(got.([]interface{})) != 3 {
		t.Fatalf("expected normalized slice got %+v", got)
	}
	if got := normalizeDiffJSONValue(7); got != 7 {
		t.Fatalf("expected passthrough got %+v", got)
	}
	withEmptyPrefix := collectDiffChanges(map[string]interface{}{"a": 1}, map[string]interface{}{}, "")
	if len(withEmptyPrefix) != 1 || withEmptyPrefix[0]["path"] != "a" {
		t.Fatalf("expected bare key path got %+v", withEmptyPrefix)
	}
	nestedEqual := collectDiffChanges(map[string]interface{}{"a": nil}, map[string]interface{}{}, "p")
	if len(nestedEqual) != 1 || nestedEqual[0]["path"] != "p" {
		t.Fatalf("expected aggregate change got %+v", nestedEqual)
	}
	if got := collectDiffChanges(nil, nil, "p"); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	diff := buildScopedConfigDiff(nil, sccPortalDoc("X"))
	if diff["portal_changed"] != true {
		t.Fatalf("expected portal change got %+v", diff)
	}
	diff = buildScopedConfigDiff(sccPortalDoc("X"), nil)
	if diff["portal_changed"] != true {
		t.Fatalf("expected portal change got %+v", diff)
	}
}

func TestSccMetadataEdges(t *testing.T) {
	if got := (scopedConfigChangeControl{}).metadata(); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	controlMeta := (scopedConfigChangeControl{ChangeRef: "CR", Reason: "RR", ApprovalRef: "AR"}).metadata()
	if controlMeta["change_ref"] != "CR" || controlMeta["reason"] != "RR" || controlMeta["approval_ref"] != "AR" {
		t.Fatalf("unexpected control metadata %+v", controlMeta)
	}
	if got := (scopedConfigChangeRisk{}).metadata(); got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	riskMeta := (scopedConfigChangeRisk{RiskLevel: "low", RiskReasons: []string{"r1"}, ChangedPaths: []string{"p1"}}).metadata()
	if riskMeta["risk_level"] != "low" || riskMeta["risk_reasons"].([]string)[0] != "r1" || riskMeta["changed_paths"].([]string)[0] != "p1" {
		t.Fatalf("unexpected risk metadata %+v", riskMeta)
	}
	verificationMeta := (scopedConfigVerificationRecord{SourceAuditID: 3, Status: "passed", Notes: "n", Evidence: []string{"e"}, Checks: []scopedConfigVerificationCheckResult{{ID: "c", Status: "passed"}}}).metadata()
	if verificationMeta["notes"] != "n" || verificationMeta["status"] != "passed" || verificationMeta["source_audit_id"] != uint(3) || verificationMeta["evidence"] == nil || verificationMeta["checks"] == nil {
		t.Fatalf("unexpected verification metadata %+v", verificationMeta)
	}
	approvalMeta := (scopedConfigApprovalRecord{Notes: "n", Evidence: []string{"e"}}).metadata()
	if approvalMeta["notes"] != "n" || len(approvalMeta) != 2 {
		t.Fatalf("unexpected approval metadata %+v", approvalMeta)
	}
	approvalResponse := (scopedConfigApprovalRecord{AuditID: 4, ActorUserID: uintPtr(7), Notes: "n", Evidence: []string{"e"}, ChangeControl: scopedConfigChangeControl{ChangeRef: "CR", Reason: "RR"}}).response()
	if approvalResponse["actor_user_id"] != uint(7) || approvalResponse["notes"] != "n" || approvalResponse["change_control"] == nil {
		t.Fatalf("unexpected approval response %+v", approvalResponse)
	}
	verificationResponse := (scopedConfigVerificationRecord{AuditID: 5, SourceAuditID: 6, Status: "passed", ActorUserID: uintPtr(8), Notes: "n", Evidence: []string{"e"}, Checks: []scopedConfigVerificationCheckResult{{ID: "c", Status: "passed"}}, ChangeControl: scopedConfigChangeControl{ChangeRef: "CR", Reason: "RR"}}).response()
	if verificationResponse["actor_user_id"] != uint(8) || verificationResponse["checks"] == nil || verificationResponse["change_control"] == nil {
		t.Fatalf("unexpected verification response %+v", verificationResponse)
	}
}

func TestSccListScopedConfigAuditLogsPagination(t *testing.T) {
	db := newScopedConfigTestDB(t)
	h := NewScopedConfigHandler(configscope.NewGormConfigStore(db))
	var nilHandler *ScopedConfigHandler
	if logs, err := nilHandler.listScopedConfigAuditLogs(context.Background(), auditplatform.ListQuery{}); logs != nil || err != nil {
		t.Fatalf("expected nil result got %+v %v", logs, err)
	}
	hNoAudit := NewScopedConfigHandler(configscope.NewGormConfigStore(db))
	if logs, err := hNoAudit.listScopedConfigAuditLogs(context.Background(), auditplatform.ListQuery{}); logs != nil || err != nil {
		t.Fatalf("expected nil result got %+v %v", logs, err)
	}
	stub := &sccAuditStub{listErr: errors.New("scc list failed")}
	h = NewScopedConfigHandler(configscope.NewGormConfigStore(db), stub)
	if _, err := h.listScopedConfigAuditLogs(context.Background(), auditplatform.ListQuery{Action: "scoped_config.tenant.update"}); err == nil {
		t.Fatalf("expected list error")
	}
	paged := &sccAuditStub{pagesByAction: map[string][][]models.AuditLog{
		"scoped_config.tenant.update": {{models.AuditLog{ID: 1}}, {models.AuditLog{ID: 2}}},
	}}
	h = NewScopedConfigHandler(configscope.NewGormConfigStore(db), paged)
	logs, err := h.listScopedConfigAuditLogs(context.Background(), auditplatform.ListQuery{Action: "scoped_config.tenant.update"})
	if err != nil || len(logs) != 2 {
		t.Fatalf("expected two paged logs got %+v err=%v", logs, err)
	}
}

func TestSccListScopedConfigVerificationIndex(t *testing.T) {
	db := newScopedConfigTestDB(t)
	h := NewScopedConfigHandler(configscope.NewGormConfigStore(db), &sccAuditStub{})
	index, err := h.listScopedConfigVerificationIndex(context.Background(), "tenant", "tenant-a", "")
	if err != nil || index == nil {
		t.Fatalf("expected empty index got %+v err=%v", index, err)
	}
	verifications, err := h.listScopedConfigVerificationHistory(context.Background(), "tenant", "tenant-a", "", 5)
	if err != nil || verifications != nil {
		t.Fatalf("expected empty history got %+v err=%v", verifications, err)
	}
}
