package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/middleware"
	"servify/apps/server/internal/models"
	auditplatform "servify/apps/server/internal/platform/audit"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/configscope"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newScopedConfigTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.TenantConfig{}, &models.WorkspaceConfig{}, &models.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newScopedConfigTestRouter(db *gorm.DB, tenantID, workspaceID string, withAudit bool) *gin.Engine {
	return newScopedConfigTestRouterWithActor(db, tenantID, workspaceID, withAudit, 9)
}

func newScopedConfigTestRouterWithActor(db *gorm.DB, tenantID, workspaceID string, withAudit bool, actorUserID uint) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("principal_kind", "admin")
		c.Set("tenant_id", tenantID)
		if workspaceID != "" {
			c.Set("workspace_id", workspaceID)
		}
		c.Set("user_id", actorUserID)
		c.Request = c.Request.WithContext(platformauth.ContextWithScope(context.Background(), tenantID, workspaceID))
		c.Next()
	})
	if withAudit {
		r.Use(middleware.AuditMiddleware(db))
	}
	RegisterScopedConfigRoutes(&r.RouterGroup, NewScopedConfigHandler(configscope.NewGormConfigStore(db), auditplatform.NewGormQueryService(db)))
	return r
}

func scopedConfigWriteBody(body string) string {
	return scopedConfigWriteBodyWithControl("CHG-1001", "approved enterprise config update", body)
}

func scopedConfigWriteBodyWithControl(changeRef, reason, body string) string {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, "{")
	trimmed = strings.TrimSuffix(trimmed, "}")
	approvalRef := "APR-" + strings.TrimPrefix(changeRef, "CHG-")
	if trimmed == "" {
		return fmt.Sprintf(`{"change_ref":%q,"reason":%q,"approval_ref":%q}`, changeRef, reason, approvalRef)
	}
	return fmt.Sprintf(`{"change_ref":%q,"reason":%q,"approval_ref":%q,%s}`, changeRef, reason, approvalRef, trimmed)
}

func scopedConfigRollbackBody(confirm bool) string {
	return fmt.Sprintf(`{"confirm":%t,"change_ref":"CHG-2002","reason":"approved rollback execution","approval_ref":"APR-2002"}`, confirm)
}

func scopedConfigApprovalRef(changeRef string) string {
	changeRef = strings.TrimSpace(changeRef)
	return "APR-" + strings.TrimPrefix(changeRef, "CHG-")
}

func scopedConfigApprovalBody(changeRef, reason string) string {
	body := map[string]any{
		"change_ref":   changeRef,
		"reason":       reason,
		"approval_ref": scopedConfigApprovalRef(changeRef),
		"notes":        "approved by reviewer",
		"evidence":     []string{"cab-ticket"},
	}
	data, _ := json.Marshal(body)
	return string(data)
}

func seedScopedConfigApprovalLog(t *testing.T, db *gorm.DB, scope, tenantID, workspaceID, changeRef, reason string, actorUserID uint) models.AuditLog {
	t.Helper()
	log := models.AuditLog{
		PrincipalKind: "admin",
		Action:        fmt.Sprintf("scoped_config.%s.approve", scope),
		ResourceType:  "scoped_config",
		ResourceID:    scopedConfigResourceID(tenantID, workspaceID),
		Route:         fmt.Sprintf("/api/security/config/%s/approve", scope),
		Method:        http.MethodPost,
		StatusCode:    http.StatusOK,
		Success:       true,
		ActorUserID:   uintPtr(actorUserID),
		TenantID:      tenantID,
		WorkspaceID:   workspaceID,
		RequestJSON:   scopedConfigApprovalBody(changeRef, reason),
	}
	if err := db.Create(&log).Error; err != nil {
		t.Fatalf("seed approval audit log: %v", err)
	}
	return log
}

func seedScopedConfigUpdateAuditLog(t *testing.T, db *gorm.DB, scope, tenantID, workspaceID, changeRef, reason string, actorUserID uint, snapshot configscope.ScopedConfigDocument) models.AuditLog {
	t.Helper()
	afterJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal scoped config snapshot: %v", err)
	}
	log := models.AuditLog{
		PrincipalKind: "admin",
		Action:        fmt.Sprintf("scoped_config.%s.update", scope),
		ResourceType:  "scoped_config",
		ResourceID:    scopedConfigResourceID(tenantID, workspaceID),
		Route:         fmt.Sprintf("/api/security/config/%s", scope),
		Method:        http.MethodPut,
		StatusCode:    http.StatusOK,
		Success:       true,
		ActorUserID:   uintPtr(actorUserID),
		TenantID:      tenantID,
		WorkspaceID:   workspaceID,
		RequestJSON:   scopedConfigWriteBodyWithControl(changeRef, reason, string(afterJSON)),
		AfterJSON:     string(afterJSON),
	}
	if err := db.Create(&log).Error; err != nil {
		t.Fatalf("seed update audit log: %v", err)
	}
	return log
}

func seedScopedConfigVerificationLog(t *testing.T, db *gorm.DB, scope, tenantID, workspaceID string, sourceAuditID uint, status, notes string, actorUserID uint) models.AuditLog {
	t.Helper()
	payload := map[string]any{
		"source_audit_id": sourceAuditID,
		"status":          status,
		"change_ref":      "CHG-VERIFY-SEED",
		"reason":          "seeded verification result",
		"checks":          defaultScopedConfigVerificationChecks(status, notes),
	}
	if strings.TrimSpace(notes) != "" {
		payload["notes"] = notes
	}
	if status == "passed" {
		payload["evidence"] = []string{"seed-evidence"}
	}
	requestJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal verification request: %v", err)
	}
	log := models.AuditLog{
		PrincipalKind: "admin",
		Action:        fmt.Sprintf("scoped_config.%s.verify", scope),
		ResourceType:  "scoped_config",
		ResourceID:    scopedConfigResourceID(tenantID, workspaceID),
		Route:         fmt.Sprintf("/api/security/config/%s/verify/%d", scope, sourceAuditID),
		Method:        http.MethodPost,
		StatusCode:    http.StatusOK,
		Success:       true,
		ActorUserID:   uintPtr(actorUserID),
		TenantID:      tenantID,
		WorkspaceID:   workspaceID,
		RequestJSON:   string(requestJSON),
	}
	if err := db.Create(&log).Error; err != nil {
		t.Fatalf("seed verification audit log: %v", err)
	}
	return log
}

func scopedConfigVerificationBody(status, notes string, evidence ...string) string {
	body := map[string]any{
		"status":       status,
		"change_ref":   "CHG-3003",
		"reason":       "post-change verification completed",
		"approval_ref": "APR-3003",
		"checks":       defaultScopedConfigVerificationChecks(status, notes),
	}
	if strings.TrimSpace(notes) != "" {
		body["notes"] = notes
	}
	if len(evidence) > 0 {
		body["evidence"] = evidence
	}
	data, _ := json.Marshal(body)
	return string(data)
}

func defaultScopedConfigVerificationChecks(status, notes string) []map[string]any {
	switch strings.TrimSpace(status) {
	case "passed":
		return []map[string]any{
			{"id": "change_scope_reviewed", "status": "passed"},
			{"id": "runtime_effect_confirmed", "status": "passed"},
			{"id": "weknora_provider_verified", "status": "passed"},
			{"id": "weknora_knowledge_mapping_verified", "status": "passed"},
		}
	case "failed":
		check := map[string]any{"id": "runtime_effect_confirmed", "status": "failed"}
		if strings.TrimSpace(notes) != "" {
			check["notes"] = notes
		}
		return []map[string]any{check}
	default:
		return nil
	}
}

func scopedConfigTemplateCheckByID(template scopedConfigVerificationTemplate, id string) (scopedConfigVerificationCheckDefinition, bool) {
	for _, check := range template.Checks {
		if check.ID == id {
			return check, true
		}
	}
	return scopedConfigVerificationCheckDefinition{}, false
}

func uintPtr(v uint) *uint {
	return &v
}

func TestScopedConfigHandlerTenantRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "", false)
	seedScopedConfigApprovalLog(t, db, "tenant", "tenant-a", "", "CHG-1001", "approved enterprise config update", 12)

	putBody := `{"portal":{"brand_name":"Tenant Brand"},"openai":{"base_url":"https://tenant.example/v1"},"session_risk":{"high_risk_score":9}}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/security/config/tenant", strings.NewReader(scopedConfigWriteBody(putBody)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/security/config/tenant", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	configObj := body["config"].(map[string]any)
	portal := configObj["portal"].(map[string]any)
	if portal["brand_name"].(string) != "Tenant Brand" {
		t.Fatalf("unexpected portal payload: %+v", portal)
	}
	openai := configObj["openai"].(map[string]any)
	if openai["base_url"].(string) != "https://tenant.example/v1" {
		t.Fatalf("unexpected openai payload: %+v", openai)
	}
	sessionRisk := configObj["session_risk"].(map[string]any)
	if int(sessionRisk["high_risk_score"].(float64)) != 9 {
		t.Fatalf("unexpected session_risk payload: %+v", sessionRisk)
	}
}

func TestScopedConfigHandlerWorkspaceRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "workspace-1", false)
	seedScopedConfigApprovalLog(t, db, "workspace", "tenant-a", "workspace-1", "CHG-1001", "approved enterprise config update", 12)

	putBody := `{"weknora":{"enabled":true,"knowledge_base_id":"kb-workspace"}}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/security/config/workspace", strings.NewReader(scopedConfigWriteBody(putBody)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/security/config/workspace", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	configObj := body["config"].(map[string]any)
	weknora := configObj["weknora"].(map[string]any)
	if weknora["knowledge_base_id"].(string) != "kb-workspace" {
		t.Fatalf("unexpected weknora payload: %+v", weknora)
	}
}

func TestBuildScopedConfigDiffIncludesSessionRisk(t *testing.T) {
	diff := buildScopedConfigDiff(
		&configscope.ScopedConfigDocument{
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-1",
			SessionRisk: &config.SessionRiskPolicyConfig{
				HighRiskScore: 4,
			},
		},
		&configscope.ScopedConfigDocument{
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-1",
			SessionRisk: &config.SessionRiskPolicyConfig{
				HighRiskScore: 8,
			},
		},
	)

	if diff["session_risk_changed"] != true {
		t.Fatalf("expected session_risk_changed=true diff=%+v", diff)
	}
	paths := diff["session_risk_paths"].([]string)
	if len(paths) != 1 || paths[0] != "session_risk.high_risk_score" {
		t.Fatalf("unexpected session_risk_paths=%v", paths)
	}
	changes := diff["session_risk_changes"].([]gin.H)
	if len(changes) != 1 {
		t.Fatalf("expected one session_risk change diff=%+v", diff)
	}
	if changes[0]["path"].(string) != "session_risk.high_risk_score" {
		t.Fatalf("unexpected session_risk change=%+v", changes[0])
	}
}

func TestScopedConfigHandlerRejectsMissingScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterScopedConfigRoutes(&r.RouterGroup, NewScopedConfigHandler(nil))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/security/config/tenant", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScopedConfigHandlerWritesAuditLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "workspace-1", true)
	h := NewScopedConfigHandler(configscope.NewGormConfigStore(db), auditplatform.NewGormQueryService(db))

	seed := configscope.ScopedConfigDocument{TenantID: "tenant-a", WorkspaceID: "workspace-1", WeKnora: &configscopeTestWeKnora}
	if _, err := h.store.UpsertWorkspaceConfig(context.Background(), "tenant-a", "workspace-1", seed); err != nil {
		t.Fatalf("seed workspace config: %v", err)
	}
	seedScopedConfigApprovalLog(t, db, "workspace", "tenant-a", "workspace-1", "CHG-2201", "rotate workspace knowledge provider settings", 12)

	putBody := scopedConfigWriteBodyWithControl("CHG-2201", "rotate workspace knowledge provider settings", `{"weknora":{"enabled":true,"knowledge_base_id":"kb-updated","api_key":"secret-key"}}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/security/config/workspace", strings.NewReader(putBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"approval_status":"approved"`) || !strings.Contains(w.Body.String(), `"risk_level":"high"`) || !strings.Contains(w.Body.String(), `"governance_status":"awaiting_verification"`) {
		t.Fatalf("expected approval/risk metadata body=%s", w.Body.String())
	}

	var logs []models.AuditLog
	if err := db.Order("id asc").Find(&logs).Error; err != nil {
		t.Fatalf("load audit logs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 audit logs got %d", len(logs))
	}
	entry := logs[1]
	if entry.Action != "scoped_config.workspace.update" {
		t.Fatalf("action = %q want scoped_config.workspace.update", entry.Action)
	}
	if entry.ResourceType != "scoped_config" {
		t.Fatalf("resource_type = %q want scoped_config", entry.ResourceType)
	}
	if entry.ResourceID != "tenant-a/workspace-1" {
		t.Fatalf("resource_id = %q want tenant-a/workspace-1", entry.ResourceID)
	}
	if entry.ActorUserID == nil || *entry.ActorUserID != 9 {
		t.Fatalf("actor_user_id = %v want 9", entry.ActorUserID)
	}
	if entry.TenantID != "tenant-a" || entry.WorkspaceID != "workspace-1" {
		t.Fatalf("scope = %s/%s", entry.TenantID, entry.WorkspaceID)
	}
	if !strings.Contains(entry.BeforeJSON, "kb-old") {
		t.Fatalf("before_json = %q want kb-old", entry.BeforeJSON)
	}
	if !strings.Contains(entry.AfterJSON, "kb-updated") {
		t.Fatalf("after_json = %q want kb-updated", entry.AfterJSON)
	}
	if strings.Contains(entry.RequestJSON, "secret-key") || strings.Contains(entry.AfterJSON, "secret-key") {
		t.Fatalf("expected secret redaction, got request=%q after=%q", entry.RequestJSON, entry.AfterJSON)
	}
	if !strings.Contains(entry.RequestJSON, "[REDACTED]") || !strings.Contains(entry.AfterJSON, "[REDACTED]") {
		t.Fatalf("expected redaction markers, got request=%q after=%q", entry.RequestJSON, entry.AfterJSON)
	}
	if !strings.Contains(entry.RequestJSON, `"change_ref":"CHG-2201"`) {
		t.Fatalf("expected change_ref in request_json, got %q", entry.RequestJSON)
	}
	if !strings.Contains(entry.RequestJSON, `"reason":"rotate workspace knowledge provider settings"`) {
		t.Fatalf("expected reason in request_json, got %q", entry.RequestJSON)
	}
	if !strings.Contains(entry.RequestJSON, `"approval_ref":"APR-2201"`) {
		t.Fatalf("expected approval_ref in request_json, got %q", entry.RequestJSON)
	}
}

func TestScopedConfigHandlerHistoryListsScopedAuditEntries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "workspace-1", true)

	type changeCase struct {
		changeRef string
		body      string
	}
	for _, item := range []changeCase{
		{changeRef: "CHG-1001", body: `{"weknora":{"enabled":true,"knowledge_base_id":"kb-v1"}}`},
		{changeRef: "CHG-1002", body: `{"weknora":{"enabled":true,"knowledge_base_id":"kb-v2"}}`},
	} {
		seedScopedConfigApprovalLog(t, db, "workspace", "tenant-a", "workspace-1", item.changeRef, "approved enterprise config update", 12)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/security/config/workspace", strings.NewReader(scopedConfigWriteBodyWithControl(item.changeRef, "approved enterprise config update", item.body)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("put expected 200 got %d body=%s", w.Code, w.Body.String())
		}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/security/config/workspace/history?page_size=10", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("history expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var body PaginatedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if body.Total != 2 {
		t.Fatalf("history total = %d want 2 body=%s", body.Total, w.Body.String())
	}
	items := body.Data.([]any)
	if len(items) != 2 {
		t.Fatalf("history items = %d want 2 body=%s", len(items), w.Body.String())
	}
	first := items[0].(map[string]any)
	if first["operation"].(string) != "update" {
		t.Fatalf("operation = %v body=%s", first["operation"], w.Body.String())
	}
	if first["has_snapshot"] != true || first["can_rollback"] != true {
		t.Fatalf("unexpected rollback metadata body=%s", w.Body.String())
	}
	changeControl := first["change_control"].(map[string]any)
	if changeControl["change_ref"].(string) != "CHG-1002" {
		t.Fatalf("unexpected change_ref body=%s", w.Body.String())
	}
	if changeControl["approval_ref"].(string) != "APR-1002" {
		t.Fatalf("unexpected approval_ref body=%s", w.Body.String())
	}
	if changeControl["reason"].(string) != "approved enterprise config update" {
		t.Fatalf("unexpected reason body=%s", w.Body.String())
	}
	changeRisk := first["change_risk"].(map[string]any)
	if changeRisk["risk_level"].(string) != "high" {
		t.Fatalf("expected risk_level=high body=%s", w.Body.String())
	}
	approvalPolicy := first["approval_policy"].(map[string]any)
	if approvalPolicy["required"] != true || approvalPolicy["approval_status"].(string) != "approved" {
		t.Fatalf("expected approved approval policy body=%s", w.Body.String())
	}
	if first["governance_status"].(string) != "awaiting_verification" {
		t.Fatalf("expected governance_status=awaiting_verification body=%s", w.Body.String())
	}
	governancePolicy := first["governance_policy"].(map[string]any)
	if governancePolicy["status"].(string) != "awaiting_verification" || governancePolicy["phase"].(string) != "verification" {
		t.Fatalf("unexpected governance policy body=%s", w.Body.String())
	}
	if first["verification_status"].(string) != "pending" {
		t.Fatalf("expected verification_status=pending body=%s", w.Body.String())
	}
	if int(first["verification_count"].(float64)) != 0 {
		t.Fatalf("expected verification_count=0 body=%s", w.Body.String())
	}
	if first["latest_verification"] != nil {
		t.Fatalf("expected latest_verification=nil body=%s", w.Body.String())
	}
	verificationPolicy := first["verification_policy"].(map[string]any)
	if verificationPolicy["verification_pending_reason"].(string) != "awaiting_distinct_reviewer_verification" {
		t.Fatalf("unexpected verification_pending_reason body=%s", w.Body.String())
	}
	if verificationPolicy["checks_required"] != true || verificationPolicy["failed_check_required_for_fail"] != true {
		t.Fatalf("expected checks policy enabled body=%s", w.Body.String())
	}
	requiredCheckIDs := verificationPolicy["required_check_ids"].([]any)
	if len(requiredCheckIDs) != 4 {
		t.Fatalf("expected four required check ids body=%s", w.Body.String())
	}
	template := first["verification_template"].(map[string]any)
	if len(template["checks"].([]any)) != 4 {
		t.Fatalf("expected four template checks body=%s", w.Body.String())
	}
	if first["preview_path"].(string) == "" || first["rollback_path"].(string) == "" || first["verify_path"].(string) == "" {
		t.Fatalf("expected preview/rollback paths body=%s", w.Body.String())
	}
	audit := first["audit"].(map[string]any)
	if audit["resource_type"].(string) != "scoped_config" {
		t.Fatalf("resource_type = %v", audit["resource_type"])
	}
}

func TestScopedConfigHandlerHistorySupportsGovernanceQueueFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "workspace-1", false)

	seedScopedConfigUpdateAuditLog(t, db, "workspace", "tenant-a", "workspace-1", "CHG-7001", "await approval", 9, configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora:     &config.WeKnoraConfig{Enabled: true, KnowledgeBaseID: "kb-awaiting-approval"},
	})
	seedScopedConfigApprovalLog(t, db, "workspace", "tenant-a", "workspace-1", "CHG-7002", "await verification", 12)
	awaitingVerification := seedScopedConfigUpdateAuditLog(t, db, "workspace", "tenant-a", "workspace-1", "CHG-7002", "await verification", 9, configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora:     &config.WeKnoraConfig{Enabled: true, KnowledgeBaseID: "kb-awaiting-verification"},
	})
	seedScopedConfigApprovalLog(t, db, "workspace", "tenant-a", "workspace-1", "CHG-7003", "verification failed", 12)
	verificationFailed := seedScopedConfigUpdateAuditLog(t, db, "workspace", "tenant-a", "workspace-1", "CHG-7003", "verification failed", 9, configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora:     &config.WeKnoraConfig{Enabled: true, KnowledgeBaseID: "kb-verification-failed"},
	})
	seedScopedConfigVerificationLog(t, db, "workspace", "tenant-a", "workspace-1", verificationFailed.ID, "failed", "runtime smoke check failed", 13)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/security/config/workspace/history?page_size=10&needs_action=true", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("history queue expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal queue body: %v", err)
	}
	if int(body["total"].(float64)) != 3 {
		t.Fatalf("expected total=3 body=%s", w.Body.String())
	}
	items := body["data"].([]any)
	if len(items) != 3 {
		t.Fatalf("expected three queue items body=%s", w.Body.String())
	}
	appliedFilters := body["applied_filters"].(map[string]any)
	if appliedFilters["needs_action"] != true {
		t.Fatalf("expected needs_action filter body=%s", w.Body.String())
	}
	summary := body["governance_summary"].(map[string]any)
	if int(summary["total_items"].(float64)) != 3 || int(summary["needs_action_count"].(float64)) != 3 {
		t.Fatalf("unexpected governance summary body=%s", w.Body.String())
	}
	statusCounts := summary["status_counts"].(map[string]any)
	if int(statusCounts["awaiting_approval"].(float64)) != 1 || int(statusCounts["awaiting_verification"].(float64)) != 1 || int(statusCounts["verification_failed"].(float64)) != 1 {
		t.Fatalf("unexpected status counts body=%s", w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/security/config/workspace/history?page_size=10&governance_status=awaiting_verification", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("filtered history expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	body = map[string]any{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal filtered body: %v", err)
	}
	if int(body["total"].(float64)) != 1 {
		t.Fatalf("expected filtered total=1 body=%s", w.Body.String())
	}
	appliedFilters = body["applied_filters"].(map[string]any)
	if appliedFilters["governance_status"].(string) != "awaiting_verification" {
		t.Fatalf("expected governance_status filter body=%s", w.Body.String())
	}
	summary = body["governance_summary"].(map[string]any)
	if int(summary["workflow_complete_count"].(float64)) != 0 {
		t.Fatalf("expected workflow_complete_count=0 body=%s", w.Body.String())
	}
	statusCounts = summary["status_counts"].(map[string]any)
	if int(statusCounts["awaiting_verification"].(float64)) != 1 {
		t.Fatalf("unexpected filtered status counts body=%s", w.Body.String())
	}
	items = body["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one filtered item body=%s", w.Body.String())
	}
	item := items[0].(map[string]any)
	if uint(item["audit"].(map[string]any)["id"].(float64)) != awaitingVerification.ID {
		t.Fatalf("expected awaiting verification audit id=%d body=%s", awaitingVerification.ID, w.Body.String())
	}
	if item["governance_status"].(string) != "awaiting_verification" {
		t.Fatalf("expected awaiting_verification item body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerRollbackRestoresSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "workspace-1", true)
	store := configscope.NewGormConfigStore(db)

	current := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora: &config.WeKnoraConfig{
			Enabled:         true,
			KnowledgeBaseID: "kb-current",
		},
	}
	if _, err := store.UpsertWorkspaceConfig(context.Background(), "tenant-a", "workspace-1", current); err != nil {
		t.Fatalf("seed current config: %v", err)
	}

	rollbackSnapshot := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora: &config.WeKnoraConfig{
			Enabled:         true,
			KnowledgeBaseID: "kb-rollback",
			APIKey:          "rollback-secret",
		},
	}
	afterJSON, err := json.Marshal(rollbackSnapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	seedLog := models.AuditLog{
		PrincipalKind: "admin",
		Action:        "scoped_config.workspace.update",
		ResourceType:  "scoped_config",
		ResourceID:    "tenant-a/workspace-1",
		Route:         "/api/security/config/workspace",
		Method:        http.MethodPut,
		StatusCode:    http.StatusOK,
		Success:       true,
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-1",
		AfterJSON:     string(afterJSON),
	}
	if err := db.Create(&seedLog).Error; err != nil {
		t.Fatalf("seed audit log: %v", err)
	}
	seedScopedConfigApprovalLog(t, db, "workspace", "tenant-a", "workspace-1", "CHG-2002", "approved rollback execution", 12)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/security/config/workspace/rollback/1?confirm=true", strings.NewReader(scopedConfigRollbackBody(true)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rollback expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"governance_status":"awaiting_verification"`) {
		t.Fatalf("expected rollback governance metadata body=%s", w.Body.String())
	}

	doc, ok, err := store.GetWorkspaceConfig(context.Background(), "tenant-a", "workspace-1")
	if err != nil || !ok {
		t.Fatalf("load rolled back config ok=%v err=%v", ok, err)
	}
	if doc.WeKnora == nil || doc.WeKnora.KnowledgeBaseID != "kb-rollback" {
		t.Fatalf("rolled back config = %+v", doc)
	}

	var logs []models.AuditLog
	if err := db.Order("id asc").Find(&logs).Error; err != nil {
		t.Fatalf("load audit logs: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 audit logs got %d", len(logs))
	}
	rollbackLog := logs[2]
	if rollbackLog.Action != "scoped_config.workspace.rollback" {
		t.Fatalf("rollback action = %q want scoped_config.workspace.rollback", rollbackLog.Action)
	}
	if !strings.Contains(w.Body.String(), `"approval_status":"approved"`) || !strings.Contains(w.Body.String(), `"risk_level":"high"`) {
		t.Fatalf("expected rollback approval/risk metadata body=%s", w.Body.String())
	}
	if !strings.Contains(rollbackLog.BeforeJSON, "kb-current") {
		t.Fatalf("rollback before_json = %q", rollbackLog.BeforeJSON)
	}
	if !strings.Contains(rollbackLog.AfterJSON, "kb-rollback") {
		t.Fatalf("rollback after_json = %q", rollbackLog.AfterJSON)
	}
	if strings.Contains(rollbackLog.AfterJSON, "rollback-secret") {
		t.Fatalf("expected redacted rollback secret, got %q", rollbackLog.AfterJSON)
	}
	if !strings.Contains(rollbackLog.RequestJSON, `"change_ref":"CHG-2002"`) {
		t.Fatalf("expected rollback change_ref in request_json, got %q", rollbackLog.RequestJSON)
	}
	if !strings.Contains(rollbackLog.RequestJSON, `"approval_ref":"APR-2002"`) {
		t.Fatalf("expected rollback approval_ref in request_json, got %q", rollbackLog.RequestJSON)
	}
}

var configscopeTestWeKnora = config.WeKnoraConfig{
	Enabled:         true,
	KnowledgeBaseID: "kb-old",
	APIKey:          "seed-secret",
}

func TestScopedConfigHandlerHistoryEntryShowsDiffPreview(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "workspace-1", false)
	store := configscope.NewGormConfigStore(db)

	current := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		OpenAI:      &config.OpenAIConfig{Model: "gpt-current"},
		WeKnora:     &config.WeKnoraConfig{Enabled: true, KnowledgeBaseID: "kb-current"},
	}
	if _, err := store.UpsertWorkspaceConfig(context.Background(), "tenant-a", "workspace-1", current); err != nil {
		t.Fatalf("seed current config: %v", err)
	}

	snapshot := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		OpenAI:      &config.OpenAIConfig{Model: "gpt-preview"},
		WeKnora:     &config.WeKnoraConfig{Enabled: true, KnowledgeBaseID: "kb-current"},
	}
	afterJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	seedLog := models.AuditLog{
		PrincipalKind: "admin",
		Action:        "scoped_config.workspace.update",
		ResourceType:  "scoped_config",
		ResourceID:    "tenant-a/workspace-1",
		Route:         "/api/security/config/workspace",
		Method:        http.MethodPut,
		StatusCode:    http.StatusOK,
		Success:       true,
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-1",
		AfterJSON:     string(afterJSON),
	}
	if err := db.Create(&seedLog).Error; err != nil {
		t.Fatalf("seed audit log: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/security/config/workspace/history/1", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("history entry expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	diff := body["diff"].(map[string]any)
	if diff["openai_changed"] != true {
		t.Fatalf("expected openai_changed=true body=%s", w.Body.String())
	}
	if diff["weknora_changed"] != false {
		t.Fatalf("expected weknora_changed=false body=%s", w.Body.String())
	}
	changedPaths := diff["changed_paths"].([]any)
	if len(changedPaths) != 1 || changedPaths[0].(string) != "openai.model" {
		t.Fatalf("expected changed_paths=[openai.model] body=%s", w.Body.String())
	}
	openaiPaths := diff["openai_paths"].([]any)
	if len(openaiPaths) != 1 || openaiPaths[0].(string) != "openai.model" {
		t.Fatalf("expected openai_paths=[openai.model] body=%s", w.Body.String())
	}
	changes := diff["changes"].([]any)
	if len(changes) != 1 {
		t.Fatalf("expected one structured change body=%s", w.Body.String())
	}
	change := changes[0].(map[string]any)
	if change["path"].(string) != "openai.model" {
		t.Fatalf("expected change path=openai.model body=%s", w.Body.String())
	}
	if change["type"].(string) != "updated" {
		t.Fatalf("expected change type=updated body=%s", w.Body.String())
	}
	if change["current"].(string) != "gpt-current" || change["snapshot"].(string) != "gpt-preview" {
		t.Fatalf("unexpected change payload body=%s", w.Body.String())
	}
	openaiChanges := diff["openai_changes"].([]any)
	if len(openaiChanges) != 1 {
		t.Fatalf("expected one openai change body=%s", w.Body.String())
	}
	if value, ok := body["change_control"]; ok && value != nil {
		t.Fatalf("expected nil change control for legacy entry body=%s", w.Body.String())
	}
	if body["verification_status"].(string) != "pending" {
		t.Fatalf("expected verification_status=pending body=%s", w.Body.String())
	}
	if body["verification_required"] != true {
		t.Fatalf("expected verification_required=true body=%s", w.Body.String())
	}
	if body["latest_verification"] != nil {
		t.Fatalf("expected latest_verification=nil body=%s", w.Body.String())
	}
	verificationPolicy := body["verification_policy"].(map[string]any)
	if verificationPolicy["verification_pending_reason"].(string) != "source_actor_unknown" {
		t.Fatalf("expected source_actor_unknown body=%s", w.Body.String())
	}
	if verificationPolicy["checks_required"] != true || verificationPolicy["template_check_count"].(float64) != 4 {
		t.Fatalf("expected verification policy template metadata body=%s", w.Body.String())
	}
	if body["governance_status"].(string) != "awaiting_verification" {
		t.Fatalf("expected governance_status=awaiting_verification body=%s", w.Body.String())
	}
	template := body["verification_template"].(map[string]any)
	if template["operation"].(string) != "update" {
		t.Fatalf("expected verification template operation=update body=%s", w.Body.String())
	}
	if len(template["changed_paths"].([]any)) != 1 || template["changed_paths"].([]any)[0].(string) != "openai.model" {
		t.Fatalf("expected changed_paths=[openai.model] body=%s", w.Body.String())
	}
	snapshotBody := body["snapshot"].(map[string]any)
	openai := snapshotBody["openai"].(map[string]any)
	if openai["model"].(string) != "gpt-preview" {
		t.Fatalf("snapshot openai model=%v", openai["model"])
	}
}

func TestScopedConfigVerificationTemplateAddsFieldSpecificChecks(t *testing.T) {
	before := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora: &config.WeKnoraConfig{
			Enabled:         true,
			KnowledgeBaseID: "kb-old",
			MaxRetries:      1,
		},
	}
	after := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora: &config.WeKnoraConfig{
			Enabled:         true,
			KnowledgeBaseID: "kb-new",
			APIKey:          "rotated-secret",
			MaxRetries:      3,
		},
	}
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}

	template := scopedConfigVerificationTemplateFromAudit(&models.AuditLog{
		Action:     "scoped_config.workspace.update",
		BeforeJSON: string(beforeJSON),
		AfterJSON:  string(afterJSON),
	})
	if len(template.ChangedPaths) != 3 {
		t.Fatalf("expected three changed paths got %+v", template.ChangedPaths)
	}
	endpointCheck, ok := scopedConfigTemplateCheckByID(template, "weknora_provider_endpoint_verified")
	if !ok {
		t.Fatalf("expected weknora_provider_endpoint_verified in %+v", template.Checks)
	}
	if !strings.Contains(endpointCheck.Title, "knowledge provider") {
		t.Fatalf("expected generic knowledge provider title, got %+v", endpointCheck.Title)
	}
	if endpointCheck.RiskLevel != "high" {
		t.Fatalf("expected endpoint risk high got %+v", endpointCheck)
	}
	if len(endpointCheck.ChangedPaths) != 2 {
		t.Fatalf("expected two endpoint changed paths got %+v", endpointCheck.ChangedPaths)
	}
	mappingCheck, ok := scopedConfigTemplateCheckByID(template, "weknora_knowledge_mapping_verified")
	if !ok {
		t.Fatalf("expected weknora_knowledge_mapping_verified in %+v", template.Checks)
	}
	if !strings.Contains(mappingCheck.Title, "knowledge provider") {
		t.Fatalf("expected generic knowledge provider mapping title, got %+v", mappingCheck.Title)
	}
	if len(mappingCheck.ChangedPaths) != 1 || mappingCheck.ChangedPaths[0] != "weknora.knowledge_base_id" {
		t.Fatalf("unexpected mapping changed paths %+v", mappingCheck.ChangedPaths)
	}
}

func TestScopedConfigHandlerHistoryEntryMarksAddedFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "workspace-1", false)

	snapshot := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		Portal:      &config.PortalConfig{BrandName: "Added Brand"},
	}
	afterJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	seedLog := models.AuditLog{
		PrincipalKind: "admin",
		Action:        "scoped_config.workspace.update",
		ResourceType:  "scoped_config",
		ResourceID:    "tenant-a/workspace-1",
		Route:         "/api/security/config/workspace",
		Method:        http.MethodPut,
		StatusCode:    http.StatusOK,
		Success:       true,
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-1",
		AfterJSON:     string(afterJSON),
	}
	if err := db.Create(&seedLog).Error; err != nil {
		t.Fatalf("seed audit log: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/security/config/workspace/history/1", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("history entry expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	diff := body["diff"].(map[string]any)
	changes := diff["changes"].([]any)
	if len(changes) != 1 {
		t.Fatalf("expected one added change body=%s", w.Body.String())
	}
	change := changes[0].(map[string]any)
	if change["path"].(string) != "portal.brand_name" {
		t.Fatalf("unexpected path body=%s", w.Body.String())
	}
	if change["type"].(string) != "added" {
		t.Fatalf("expected added change body=%s", w.Body.String())
	}
	if change["current"] != nil || change["snapshot"].(string) != "Added Brand" {
		t.Fatalf("unexpected added payload body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerRollbackRequiresConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "workspace-1", true)

	snapshot := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora:     &config.WeKnoraConfig{Enabled: true, KnowledgeBaseID: "kb-rollback"},
	}
	afterJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	seedLog := models.AuditLog{
		PrincipalKind: "admin",
		Action:        "scoped_config.workspace.update",
		ResourceType:  "scoped_config",
		ResourceID:    "tenant-a/workspace-1",
		Route:         "/api/security/config/workspace",
		Method:        http.MethodPut,
		StatusCode:    http.StatusOK,
		Success:       true,
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-1",
		AfterJSON:     string(afterJSON),
	}
	if err := db.Create(&seedLog).Error; err != nil {
		t.Fatalf("seed audit log: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/security/config/workspace/rollback/1", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "confirm=true") {
		t.Fatalf("expected confirm hint body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerVerificationMarksHistoryEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	rWriter := newScopedConfigTestRouterWithActor(db, "tenant-a", "workspace-1", true, 9)
	rReviewer := newScopedConfigTestRouterWithActor(db, "tenant-a", "workspace-1", true, 12)
	seedScopedConfigApprovalLog(t, db, "workspace", "tenant-a", "workspace-1", "CHG-1001", "approved enterprise config update", 12)

	putBody := scopedConfigWriteBody(`{"weknora":{"enabled":true,"knowledge_base_id":"kb-verified"}}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/security/config/workspace", strings.NewReader(putBody))
	req.Header.Set("Content-Type", "application/json")
	rWriter.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/security/config/workspace/verify/2", strings.NewReader(scopedConfigVerificationBody("passed", "portal and auth smoke checks passed", "runbook-42", "kb-sync")))
	req.Header.Set("Content-Type", "application/json")
	rReviewer.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("verify expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var verifyResponse map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &verifyResponse); err != nil {
		t.Fatalf("unmarshal verify response: %v", err)
	}
	verifyTemplate := verifyResponse["verification_template"].(map[string]any)
	if len(verifyTemplate["checks"].([]any)) != 4 {
		t.Fatalf("expected four verify response template checks body=%s", w.Body.String())
	}
	sourceApprovalPolicy := verifyResponse["source_approval_policy"].(map[string]any)
	if sourceApprovalPolicy["required"] != true || sourceApprovalPolicy["approval_status"].(string) != "approved" {
		t.Fatalf("expected source approval policy body=%s", w.Body.String())
	}
	if verifyResponse["source_governance_status"].(string) != "verified" {
		t.Fatalf("expected source_governance_status=verified body=%s", w.Body.String())
	}
	sourceGovernancePolicy := verifyResponse["source_governance_policy"].(map[string]any)
	if sourceGovernancePolicy["status"].(string) != "verified" || sourceGovernancePolicy["workflow_complete"] != true {
		t.Fatalf("unexpected source governance policy body=%s", w.Body.String())
	}
	verifyPolicy := verifyResponse["verification_policy"].(map[string]any)
	if verifyPolicy["checks_required"] != true || len(verifyPolicy["required_check_ids"].([]any)) != 4 {
		t.Fatalf("expected verify response checks policy body=%s", w.Body.String())
	}

	var logs []models.AuditLog
	if err := db.Order("id asc").Find(&logs).Error; err != nil {
		t.Fatalf("load audit logs: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 audit logs got %d", len(logs))
	}
	verifyLog := logs[2]
	if verifyLog.Action != "scoped_config.workspace.verify" {
		t.Fatalf("verify action = %q want scoped_config.workspace.verify", verifyLog.Action)
	}
	if !strings.Contains(verifyLog.RequestJSON, `"source_audit_id":2`) {
		t.Fatalf("expected source_audit_id in request_json, got %q", verifyLog.RequestJSON)
	}
	if !strings.Contains(verifyLog.RequestJSON, `"status":"passed"`) {
		t.Fatalf("expected status in request_json, got %q", verifyLog.RequestJSON)
	}
	if !strings.Contains(verifyLog.RequestJSON, `"change_ref":"CHG-3003"`) {
		t.Fatalf("expected change_ref in request_json, got %q", verifyLog.RequestJSON)
	}
	if verifyLog.ActorUserID == nil || *verifyLog.ActorUserID != 12 {
		t.Fatalf("verify actor_user_id = %v want 12", verifyLog.ActorUserID)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/security/config/workspace/history?page_size=10", nil)
	rReviewer.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("history expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var history PaginatedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if history.Total != 1 {
		t.Fatalf("history total = %d want 1 body=%s", history.Total, w.Body.String())
	}
	item := history.Data.([]any)[0].(map[string]any)
	if item["verification_status"].(string) != "passed" {
		t.Fatalf("expected verification_status=passed body=%s", w.Body.String())
	}
	if int(item["verification_count"].(float64)) != 1 {
		t.Fatalf("expected verification_count=1 body=%s", w.Body.String())
	}
	latestVerification := item["latest_verification"].(map[string]any)
	if latestVerification["status"].(string) != "passed" {
		t.Fatalf("expected latest verification status=passed body=%s", w.Body.String())
	}
	checks := latestVerification["checks"].([]any)
	if len(checks) != 4 {
		t.Fatalf("expected four verification checks body=%s", w.Body.String())
	}
	template := item["verification_template"].(map[string]any)
	templateChecks := template["checks"].([]any)
	if len(templateChecks) != 4 {
		t.Fatalf("expected four template checks body=%s", w.Body.String())
	}
	verificationPolicy := item["verification_policy"].(map[string]any)
	if verificationPolicy["same_actor_allowed"] != false {
		t.Fatalf("expected same_actor_allowed=false body=%s", w.Body.String())
	}
	if item["governance_status"].(string) != "verified" {
		t.Fatalf("expected governance_status=verified body=%s", w.Body.String())
	}
	if verificationPolicy["checks_required"] != true || verificationPolicy["failed_check_required_for_fail"] != true {
		t.Fatalf("expected checks verification policy body=%s", w.Body.String())
	}
	if int(verificationPolicy["source_actor_user_id"].(float64)) != 9 {
		t.Fatalf("expected source_actor_user_id=9 body=%s", w.Body.String())
	}
	if int(verificationPolicy["latest_reviewer_user_id"].(float64)) != 12 {
		t.Fatalf("expected latest_reviewer_user_id=12 body=%s", w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/security/config/workspace/history/2", nil)
	rReviewer.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("history entry expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var detail map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detail["verification_status"].(string) != "passed" {
		t.Fatalf("expected verification_status=passed body=%s", w.Body.String())
	}
	if detail["governance_status"].(string) != "verified" {
		t.Fatalf("expected governance_status=verified body=%s", w.Body.String())
	}
	verifications := detail["verifications"].([]any)
	if len(verifications) != 1 {
		t.Fatalf("expected one verification body=%s", w.Body.String())
	}
	verification := verifications[0].(map[string]any)
	if verification["status"].(string) != "passed" {
		t.Fatalf("expected verification status=passed body=%s", w.Body.String())
	}
	if len(verification["checks"].([]any)) != 4 {
		t.Fatalf("expected four verification checks body=%s", w.Body.String())
	}
	changeControl := verification["change_control"].(map[string]any)
	if changeControl["change_ref"].(string) != "CHG-3003" {
		t.Fatalf("unexpected verification change_ref body=%s", w.Body.String())
	}
	policy := detail["verification_policy"].(map[string]any)
	if int(policy["source_actor_user_id"].(float64)) != 9 || int(policy["latest_reviewer_user_id"].(float64)) != 12 {
		t.Fatalf("unexpected verification_policy body=%s", w.Body.String())
	}
	if policy["template_check_count"].(float64) != 4 {
		t.Fatalf("expected template_check_count=4 body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerFailedVerificationMarksGovernanceFailed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	rWriter := newScopedConfigTestRouterWithActor(db, "tenant-a", "workspace-1", true, 9)
	rReviewer := newScopedConfigTestRouterWithActor(db, "tenant-a", "workspace-1", true, 12)
	seedScopedConfigApprovalLog(t, db, "workspace", "tenant-a", "workspace-1", "CHG-1001", "approved enterprise config update", 12)

	putBody := scopedConfigWriteBody(`{"weknora":{"enabled":true,"knowledge_base_id":"kb-failed"}}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/security/config/workspace", strings.NewReader(putBody))
	req.Header.Set("Content-Type", "application/json")
	rWriter.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/security/config/workspace/verify/2", strings.NewReader(scopedConfigVerificationBody("failed", "runtime smoke check failed")))
	req.Header.Set("Content-Type", "application/json")
	rReviewer.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("verify expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	var verifyResponse map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &verifyResponse); err != nil {
		t.Fatalf("unmarshal verify response: %v", err)
	}
	if verifyResponse["source_governance_status"].(string) != "verification_failed" {
		t.Fatalf("expected source_governance_status=verification_failed body=%s", w.Body.String())
	}
	sourceGovernancePolicy := verifyResponse["source_governance_policy"].(map[string]any)
	if sourceGovernancePolicy["status"].(string) != "verification_failed" || sourceGovernancePolicy["workflow_complete"] != false {
		t.Fatalf("unexpected source governance policy body=%s", w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/security/config/workspace/history?page_size=10", nil)
	rReviewer.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("history expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	var history PaginatedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	item := history.Data.([]any)[0].(map[string]any)
	if item["governance_status"].(string) != "verification_failed" {
		t.Fatalf("expected governance_status=verification_failed body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerVerificationFailureRequiresNotes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouterWithActor(db, "tenant-a", "workspace-1", true, 12)

	snapshot := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora:     &config.WeKnoraConfig{Enabled: true, KnowledgeBaseID: "kb-verify"},
	}
	afterJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	seedLog := models.AuditLog{
		PrincipalKind: "admin",
		Action:        "scoped_config.workspace.update",
		ResourceType:  "scoped_config",
		ResourceID:    "tenant-a/workspace-1",
		Route:         "/api/security/config/workspace",
		Method:        http.MethodPut,
		StatusCode:    http.StatusOK,
		Success:       true,
		ActorUserID:   uintPtr(9),
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-1",
		AfterJSON:     string(afterJSON),
	}
	if err := db.Create(&seedLog).Error; err != nil {
		t.Fatalf("seed audit log: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/security/config/workspace/verify/1", strings.NewReader(scopedConfigVerificationBody("failed", "")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Verification notes required") {
		t.Fatalf("expected verification notes error body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerVerificationPassedRequiresEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouterWithActor(db, "tenant-a", "workspace-1", true, 12)

	snapshot := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora:     &config.WeKnoraConfig{Enabled: true, KnowledgeBaseID: "kb-verify"},
	}
	afterJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	seedLog := models.AuditLog{
		PrincipalKind: "admin",
		Action:        "scoped_config.workspace.update",
		ResourceType:  "scoped_config",
		ResourceID:    "tenant-a/workspace-1",
		Route:         "/api/security/config/workspace",
		Method:        http.MethodPut,
		StatusCode:    http.StatusOK,
		Success:       true,
		ActorUserID:   uintPtr(9),
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-1",
		AfterJSON:     string(afterJSON),
	}
	if err := db.Create(&seedLog).Error; err != nil {
		t.Fatalf("seed audit log: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/security/config/workspace/verify/1", strings.NewReader(scopedConfigVerificationBody("passed", "smoke checks ok")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Verification evidence required") {
		t.Fatalf("expected verification evidence error body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerVerificationPassedRequiresAllTemplateChecks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouterWithActor(db, "tenant-a", "workspace-1", true, 12)

	snapshot := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora:     &config.WeKnoraConfig{Enabled: true, KnowledgeBaseID: "kb-verify"},
	}
	afterJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	seedLog := models.AuditLog{
		PrincipalKind: "admin",
		Action:        "scoped_config.workspace.update",
		ResourceType:  "scoped_config",
		ResourceID:    "tenant-a/workspace-1",
		Route:         "/api/security/config/workspace",
		Method:        http.MethodPut,
		StatusCode:    http.StatusOK,
		Success:       true,
		ActorUserID:   uintPtr(9),
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-1",
		AfterJSON:     string(afterJSON),
	}
	if err := db.Create(&seedLog).Error; err != nil {
		t.Fatalf("seed audit log: %v", err)
	}

	body := `{"status":"passed","notes":"smoke checks ok","evidence":["runbook-42"],"checks":[{"id":"change_scope_reviewed","status":"passed"},{"id":"runtime_effect_confirmed","status":"passed"}],"change_ref":"CHG-3003","reason":"post-change verification completed"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/security/config/workspace/verify/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Verification checks incomplete") {
		t.Fatalf("expected verification checks incomplete body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerVerificationFailedRequiresFailedCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouterWithActor(db, "tenant-a", "workspace-1", true, 12)

	snapshot := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora:     &config.WeKnoraConfig{Enabled: true, KnowledgeBaseID: "kb-verify"},
	}
	afterJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	seedLog := models.AuditLog{
		PrincipalKind: "admin",
		Action:        "scoped_config.workspace.update",
		ResourceType:  "scoped_config",
		ResourceID:    "tenant-a/workspace-1",
		Route:         "/api/security/config/workspace",
		Method:        http.MethodPut,
		StatusCode:    http.StatusOK,
		Success:       true,
		ActorUserID:   uintPtr(9),
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-1",
		AfterJSON:     string(afterJSON),
	}
	if err := db.Create(&seedLog).Error; err != nil {
		t.Fatalf("seed audit log: %v", err)
	}

	body := `{"status":"failed","notes":"runtime validation did not complete","checks":[{"id":"change_scope_reviewed","status":"passed"}],"change_ref":"CHG-3003","reason":"post-change verification completed"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/security/config/workspace/verify/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Failed verification check required") {
		t.Fatalf("expected failed verification check error body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerVerificationRejectsUnknownCheckID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouterWithActor(db, "tenant-a", "workspace-1", true, 12)

	snapshot := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora:     &config.WeKnoraConfig{Enabled: true, KnowledgeBaseID: "kb-verify"},
	}
	afterJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	seedLog := models.AuditLog{
		PrincipalKind: "admin",
		Action:        "scoped_config.workspace.update",
		ResourceType:  "scoped_config",
		ResourceID:    "tenant-a/workspace-1",
		Route:         "/api/security/config/workspace",
		Method:        http.MethodPut,
		StatusCode:    http.StatusOK,
		Success:       true,
		ActorUserID:   uintPtr(9),
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-1",
		AfterJSON:     string(afterJSON),
	}
	if err := db.Create(&seedLog).Error; err != nil {
		t.Fatalf("seed audit log: %v", err)
	}

	body := `{"status":"failed","notes":"runtime validation failed","checks":[{"id":"unknown_control","status":"failed"}],"change_ref":"CHG-3003","reason":"post-change verification completed"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/security/config/workspace/verify/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Invalid verification check") {
		t.Fatalf("expected invalid verification check error body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerRejectsSelfVerification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouterWithActor(db, "tenant-a", "workspace-1", true, 9)
	seedScopedConfigApprovalLog(t, db, "workspace", "tenant-a", "workspace-1", "CHG-1001", "approved enterprise config update", 12)

	putBody := scopedConfigWriteBody(`{"weknora":{"enabled":true,"knowledge_base_id":"kb-self"}}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/security/config/workspace", strings.NewReader(putBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/security/config/workspace/verify/2", strings.NewReader(scopedConfigVerificationBody("passed", "self verify attempt", "runbook-42")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Verification reviewer separation required") {
		t.Fatalf("expected reviewer separation error body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerRejectsWriteWithoutChangeControl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/security/config/tenant", strings.NewReader(`{"portal":{"brand_name":"Tenant Brand"}}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Change control required") {
		t.Fatalf("expected change control error body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerRejectsHighRiskWriteWithoutApprovalRef(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "workspace-1", false)

	body := `{"change_ref":"CHG-4401","reason":"rotate knowledge mapping","weknora":{"enabled":true,"knowledge_base_id":"kb-no-approval"}}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/security/config/workspace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Approval reference required") {
		t.Fatalf("expected approval reference error body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerRejectsHighRiskWriteWithoutApprovalRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "workspace-1", false)

	body := `{"change_ref":"CHG-4403","reason":"rotate knowledge mapping","approval_ref":"APR-4403","weknora":{"enabled":true,"knowledge_base_id":"kb-no-record"}}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/security/config/workspace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Approved change required") {
		t.Fatalf("expected approved change error body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerRejectsHighRiskWriteWithSelfApproval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouterWithActor(db, "tenant-a", "workspace-1", false, 9)
	seedScopedConfigApprovalLog(t, db, "workspace", "tenant-a", "workspace-1", "CHG-4404", "rotate knowledge mapping", 9)

	body := `{"change_ref":"CHG-4404","reason":"rotate knowledge mapping","approval_ref":"APR-4404","weknora":{"enabled":true,"knowledge_base_id":"kb-self-approved"}}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/security/config/workspace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Approval reviewer separation required") {
		t.Fatalf("expected approval separation error body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerApproveWritesAuditLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouterWithActor(db, "tenant-a", "workspace-1", true, 12)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/security/config/workspace/approve", strings.NewReader(scopedConfigApprovalBody("CHG-5501", "approve workspace knowledge change")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("approve expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal approve response: %v", err)
	}
	approval := body["approval"].(map[string]any)
	changeControl := approval["change_control"].(map[string]any)
	if changeControl["change_ref"].(string) != "CHG-5501" || changeControl["approval_ref"].(string) != "APR-5501" {
		t.Fatalf("unexpected approval change control body=%s", w.Body.String())
	}
	approvalPolicy := body["approval_policy"].(map[string]any)
	if approvalPolicy["approval_status"].(string) != "approved" {
		t.Fatalf("expected approval_status=approved body=%s", w.Body.String())
	}

	var logs []models.AuditLog
	if err := db.Order("id asc").Find(&logs).Error; err != nil {
		t.Fatalf("load audit logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 audit log got %d", len(logs))
	}
	entry := logs[0]
	if entry.Action != "scoped_config.workspace.approve" {
		t.Fatalf("approval action = %q want scoped_config.workspace.approve", entry.Action)
	}
	if entry.ActorUserID == nil || *entry.ActorUserID != 12 {
		t.Fatalf("approval actor_user_id = %v want 12", entry.ActorUserID)
	}
	if !strings.Contains(entry.RequestJSON, `"change_ref":"CHG-5501"`) || !strings.Contains(entry.RequestJSON, `"approval_ref":"APR-5501"`) {
		t.Fatalf("expected approval change control in request_json, got %q", entry.RequestJSON)
	}
	if !strings.Contains(entry.RequestJSON, `"notes":"approved by reviewer"`) {
		t.Fatalf("expected approval notes in request_json, got %q", entry.RequestJSON)
	}
}

func TestScopedConfigHandlerAcceptsHeaderChangeControl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/security/config/tenant", strings.NewReader(`{"portal":{"brand_name":"Tenant Header"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Change-Ref", "CHG-3301")
	req.Header.Set("X-Change-Reason", "header approved tenant branding update")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScopedConfigHandlerRollbackRequiresChangeControl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "workspace-1", true)

	snapshot := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora:     &config.WeKnoraConfig{Enabled: true, KnowledgeBaseID: "kb-rollback"},
	}
	afterJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	seedLog := models.AuditLog{
		PrincipalKind: "admin",
		Action:        "scoped_config.workspace.update",
		ResourceType:  "scoped_config",
		ResourceID:    "tenant-a/workspace-1",
		Route:         "/api/security/config/workspace",
		Method:        http.MethodPut,
		StatusCode:    http.StatusOK,
		Success:       true,
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-1",
		AfterJSON:     string(afterJSON),
	}
	if err := db.Create(&seedLog).Error; err != nil {
		t.Fatalf("seed audit log: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/security/config/workspace/rollback/1?confirm=true", strings.NewReader(`{"confirm":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Change control required") {
		t.Fatalf("expected change control error body=%s", w.Body.String())
	}
}

func TestScopedConfigHandlerRollbackRequiresApprovalRefForHighRiskChange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedConfigTestDB(t)
	r := newScopedConfigTestRouter(db, "tenant-a", "workspace-1", true)

	snapshot := configscope.ScopedConfigDocument{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-1",
		WeKnora:     &config.WeKnoraConfig{Enabled: true, KnowledgeBaseID: "kb-rollback"},
	}
	afterJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	seedLog := models.AuditLog{
		PrincipalKind: "admin",
		Action:        "scoped_config.workspace.update",
		ResourceType:  "scoped_config",
		ResourceID:    "tenant-a/workspace-1",
		Route:         "/api/security/config/workspace",
		Method:        http.MethodPut,
		StatusCode:    http.StatusOK,
		Success:       true,
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-1",
		AfterJSON:     string(afterJSON),
	}
	if err := db.Create(&seedLog).Error; err != nil {
		t.Fatalf("seed audit log: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/security/config/workspace/rollback/1?confirm=true", strings.NewReader(`{"confirm":true,"change_ref":"CHG-4402","reason":"execute rollback without approval"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Approval reference required") {
		t.Fatalf("expected approval reference error body=%s", w.Body.String())
	}
}
