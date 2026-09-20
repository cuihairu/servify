package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	auditplatform "servify/apps/server/internal/platform/audit"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/configscope"

	"github.com/gin-gonic/gin"
)

type ScopedConfigHandler struct {
	store *configscope.GormConfigStore
	audit auditplatform.QueryService
}

func NewScopedConfigHandler(store *configscope.GormConfigStore, auditServices ...auditplatform.QueryService) *ScopedConfigHandler {
	h := &ScopedConfigHandler{store: store}
	if len(auditServices) > 0 {
		h.audit = auditServices[0]
	}
	return h
}

func (h *ScopedConfigHandler) GetTenantConfig(c *gin.Context) {
	tenantID := platformauth.TenantIDFromContext(c.Request.Context())
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Tenant scope required", Message: "tenant_id is required in request scope"})
		return
	}
	if h == nil || h.store == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Config store unavailable", Message: "config store not configured"})
		return
	}
	doc, ok, err := h.store.GetTenantConfig(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load tenant config", Message: err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusOK, gin.H{"tenant_id": tenantID, "config": configscope.ScopedConfigDocument{TenantID: tenantID}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tenant_id": tenantID, "config": doc})
}

func (h *ScopedConfigHandler) PutTenantConfig(c *gin.Context) {
	tenantID := platformauth.TenantIDFromContext(c.Request.Context())
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Tenant scope required", Message: "tenant_id is required in request scope"})
		return
	}
	if h == nil || h.store == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Config store unavailable", Message: "config store not configured"})
		return
	}
	payload, changeControl, errResponse := bindScopedConfigWriteRequest(c)
	if errResponse != nil {
		c.JSON(errResponse.Code, errResponse)
		return
	}
	var current *configscope.ScopedConfigDocument
	if before, ok, err := h.store.GetTenantConfig(c.Request.Context(), tenantID); err == nil && ok {
		auditplatform.SetBefore(c, before)
		current = before
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load tenant config", Message: err.Error()})
		return
	}
	changeTemplate := scopedConfigVerificationTemplateForDocuments("update", current, scopedConfigMergedDocument(current, payload, tenantID, ""))
	changeRisk := scopedConfigChangeRiskForTemplate("update", changeTemplate)
	approval, err := h.latestScopedConfigApproval(c.Request.Context(), "tenant", tenantID, "", changeControl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load scoped config approval", Message: err.Error()})
		return
	}
	if errResponse := validateScopedConfigApproval(changeControl, changeRisk, approval, scopedConfigContextUserID(c)); errResponse != nil {
		c.JSON(errResponse.Code, errResponse)
		return
	}
	governanceStatus := scopedConfigGovernanceStatus("update", changeRisk, changeControl, approval, nil)
	setScopedConfigAuditMeta(c, "tenant", tenantID, "", "update", changeControl)
	auditplatform.MergeRequestMetadata(c, changeRisk.metadata())
	doc, err := h.store.UpsertTenantConfig(c.Request.Context(), tenantID, payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to save tenant config", Message: err.Error()})
		return
	}
	auditplatform.SetAfter(c, doc)
	c.JSON(http.StatusOK, gin.H{
		"tenant_id":         tenantID,
		"config":            doc,
		"change_control":    changeControl.response(),
		"change_risk":       changeRisk.response(),
		"approval_policy":   scopedConfigApprovalPolicy(changeRisk, changeControl, approval),
		"governance_status": governanceStatus,
		"governance_policy": scopedConfigGovernancePolicy("update", changeRisk, changeControl, approval, nil),
	})
}

func bindScopedConfigWriteRequest(c *gin.Context) (configscope.ScopedConfigDocument, scopedConfigChangeControl, *ErrorResponse) {
	var req scopedConfigWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return configscope.ScopedConfigDocument{}, scopedConfigChangeControl{}, &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid request", Message: err.Error()}
	}
	changeControl, errResponse := resolveScopedConfigChangeControl(c, req.ChangeRef, req.Reason, req.ApprovalRef)
	if errResponse != nil {
		return configscope.ScopedConfigDocument{}, scopedConfigChangeControl{}, errResponse
	}
	return req.ScopedConfigDocument, changeControl, nil
}

func (h *ScopedConfigHandler) GetTenantConfigHistory(c *gin.Context) {
	h.listHistory(c, "tenant")
}

func (h *ScopedConfigHandler) GetTenantConfigHistoryEntry(c *gin.Context) {
	h.historyEntry(c, "tenant")
}

func (h *ScopedConfigHandler) ApproveTenantConfig(c *gin.Context) {
	h.approve(c, "tenant")
}

func (h *ScopedConfigHandler) RollbackTenantConfig(c *gin.Context) {
	h.rollback(c, "tenant")
}

func (h *ScopedConfigHandler) VerifyTenantConfig(c *gin.Context) {
	h.verify(c, "tenant")
}

func (h *ScopedConfigHandler) GetWorkspaceConfig(c *gin.Context) {
	tenantID := platformauth.TenantIDFromContext(c.Request.Context())
	workspaceID := platformauth.WorkspaceIDFromContext(c.Request.Context())
	if tenantID == "" || workspaceID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Workspace scope required", Message: "tenant_id and workspace_id are required in request scope"})
		return
	}
	if h == nil || h.store == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Config store unavailable", Message: "config store not configured"})
		return
	}
	doc, ok, err := h.store.GetWorkspaceConfig(c.Request.Context(), tenantID, workspaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load workspace config", Message: err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusOK, gin.H{"tenant_id": tenantID, "workspace_id": workspaceID, "config": configscope.ScopedConfigDocument{TenantID: tenantID, WorkspaceID: workspaceID}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tenant_id": tenantID, "workspace_id": workspaceID, "config": doc})
}

func (h *ScopedConfigHandler) PutWorkspaceConfig(c *gin.Context) {
	tenantID := platformauth.TenantIDFromContext(c.Request.Context())
	workspaceID := platformauth.WorkspaceIDFromContext(c.Request.Context())
	if tenantID == "" || workspaceID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Workspace scope required", Message: "tenant_id and workspace_id are required in request scope"})
		return
	}
	if h == nil || h.store == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Config store unavailable", Message: "config store not configured"})
		return
	}
	payload, changeControl, errResponse := bindScopedConfigWriteRequest(c)
	if errResponse != nil {
		c.JSON(errResponse.Code, errResponse)
		return
	}
	var current *configscope.ScopedConfigDocument
	if before, ok, err := h.store.GetWorkspaceConfig(c.Request.Context(), tenantID, workspaceID); err == nil && ok {
		auditplatform.SetBefore(c, before)
		current = before
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load workspace config", Message: err.Error()})
		return
	}
	changeTemplate := scopedConfigVerificationTemplateForDocuments("update", current, scopedConfigMergedDocument(current, payload, tenantID, workspaceID))
	changeRisk := scopedConfigChangeRiskForTemplate("update", changeTemplate)
	approval, err := h.latestScopedConfigApproval(c.Request.Context(), "workspace", tenantID, workspaceID, changeControl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load scoped config approval", Message: err.Error()})
		return
	}
	if errResponse := validateScopedConfigApproval(changeControl, changeRisk, approval, scopedConfigContextUserID(c)); errResponse != nil {
		c.JSON(errResponse.Code, errResponse)
		return
	}
	governanceStatus := scopedConfigGovernanceStatus("update", changeRisk, changeControl, approval, nil)
	setScopedConfigAuditMeta(c, "workspace", tenantID, workspaceID, "update", changeControl)
	auditplatform.MergeRequestMetadata(c, changeRisk.metadata())
	doc, err := h.store.UpsertWorkspaceConfig(c.Request.Context(), tenantID, workspaceID, payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to save workspace config", Message: err.Error()})
		return
	}
	auditplatform.SetAfter(c, doc)
	c.JSON(http.StatusOK, gin.H{
		"tenant_id":         tenantID,
		"workspace_id":      workspaceID,
		"config":            doc,
		"change_control":    changeControl.response(),
		"change_risk":       changeRisk.response(),
		"approval_policy":   scopedConfigApprovalPolicy(changeRisk, changeControl, approval),
		"governance_status": governanceStatus,
		"governance_policy": scopedConfigGovernancePolicy("update", changeRisk, changeControl, approval, nil),
	})
}

func (h *ScopedConfigHandler) GetWorkspaceConfigHistory(c *gin.Context) {
	h.listHistory(c, "workspace")
}

func (h *ScopedConfigHandler) GetWorkspaceConfigHistoryEntry(c *gin.Context) {
	h.historyEntry(c, "workspace")
}

func (h *ScopedConfigHandler) ApproveWorkspaceConfig(c *gin.Context) {
	h.approve(c, "workspace")
}

func (h *ScopedConfigHandler) RollbackWorkspaceConfig(c *gin.Context) {
	h.rollback(c, "workspace")
}

func (h *ScopedConfigHandler) VerifyWorkspaceConfig(c *gin.Context) {
	h.verify(c, "workspace")
}

func (h *ScopedConfigHandler) historyEntry(c *gin.Context, scope string) {
	tenantID, workspaceID, ok := requireScope(c, scope)
	if !ok {
		return
	}
	if h == nil || h.store == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Config store unavailable", Message: "config store not configured"})
		return
	}
	entry, snapshot, response, err := h.loadHistoryEntry(c, scope, tenantID, workspaceID)
	if response != nil {
		c.JSON(response.Code, response)
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load config history entry", Message: err.Error()})
		return
	}
	current, err := h.currentConfigForScope(c.Request.Context(), scope, tenantID, workspaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load current scoped config", Message: err.Error()})
		return
	}
	verifications, err := h.listScopedConfigVerificationHistory(c.Request.Context(), scope, tenantID, workspaceID, entry.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load config verification history", Message: err.Error()})
		return
	}
	operation := scopedConfigOperation(entry.Action)
	verificationTemplate := scopedConfigVerificationTemplateForHistoryEntry(entry, current, snapshot)
	changeControl := extractScopedConfigChangeControl(entry.RequestJSON)
	changeRisk := scopedConfigChangeRiskForTemplate(operation, verificationTemplate)
	approval, err := h.latestScopedConfigApproval(c.Request.Context(), scope, tenantID, workspaceID, changeControl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load config approval history", Message: err.Error()})
		return
	}
	governanceStatus := scopedConfigGovernanceStatus(operation, changeRisk, changeControl, approval, verifications)
	c.JSON(http.StatusOK, gin.H{
		"audit":                 entry,
		"snapshot":              snapshot,
		"current":               current,
		"diff":                  buildScopedConfigDiff(current, snapshot),
		"change_control":        changeControl.response(),
		"change_risk":           changeRisk.response(),
		"approval_policy":       scopedConfigApprovalPolicy(changeRisk, changeControl, approval),
		"governance_status":     governanceStatus,
		"governance_policy":     scopedConfigGovernancePolicy(operation, changeRisk, changeControl, approval, verifications),
		"verification_required": scopedConfigVerificationRequired(operation),
		"verification_status":   scopedConfigVerificationStatus(operation, verifications),
		"verification_path":     fmt.Sprintf("/security/config/%s/verify/%d", scope, entry.ID),
		"latest_verification":   latestScopedConfigVerificationResponse(verifications),
		"verification_template": verificationTemplate,
		"verification_policy":   scopedConfigVerificationPolicy(operation, entry.ActorUserID, verificationTemplate, verifications),
		"verifications":         scopedConfigVerificationResponses(verifications),
	})
}

func (h *ScopedConfigHandler) listHistory(c *gin.Context, scope string) {
	tenantID, workspaceID, ok := requireScope(c, scope)
	if !ok {
		return
	}
	if h == nil || h.audit == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Audit service unavailable", Message: "audit query service not configured"})
		return
	}

	page, pageSize := normalizeAuditPage(intQuery(c, "page", 1), intQuery(c, "page_size", 20))
	items, err := h.listScopedConfigHistoryEntries(c.Request.Context(), scope, tenantID, workspaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list config history", Message: err.Error()})
		return
	}
	items = filterScopedConfigHistoryEntries(items, c.Query("action"))
	verificationIndex, err := h.listScopedConfigVerificationIndex(c.Request.Context(), scope, tenantID, workspaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load config verification history", Message: err.Error()})
		return
	}
	approvalIndex, err := h.listScopedConfigApprovalIndex(c.Request.Context(), scope, tenantID, workspaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load config approval history", Message: err.Error()})
		return
	}
	filters := scopedConfigHistoryFiltersFromRequest(c)
	historyItems := buildScopedConfigHistoryItems(scope, items, verificationIndex, approvalIndex)
	historyItems = filterScopedConfigHistoryResponses(historyItems, filters)
	total := int64(len(historyItems))
	pages := 0
	if total > 0 {
		pages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	c.JSON(http.StatusOK, gin.H{
		"data":               sliceScopedConfigHistoryItemsPage(historyItems, page, pageSize),
		"total":              total,
		"page":               page,
		"page_size":          pageSize,
		"pages":              pages,
		"governance_summary": summarizeScopedConfigHistoryResponses(historyItems),
		"applied_filters":    filters.response(),
	})
}

func buildScopedConfigHistoryItems(scope string, items []models.AuditLog, verificationIndex map[uint][]scopedConfigVerificationRecord, approvalIndex map[string][]scopedConfigApprovalRecord) []gin.H {
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		hasSnapshot := strings.TrimSpace(item.AfterJSON) != ""
		operation := scopedConfigOperation(item.Action)
		changeControl := extractScopedConfigChangeControl(item.RequestJSON)
		verifications := verificationIndex[item.ID]
		verificationTemplate := scopedConfigVerificationTemplateFromAudit(&item)
		changeRisk := scopedConfigChangeRiskForTemplate(operation, verificationTemplate)
		approval := latestScopedConfigApprovalFromIndex(approvalIndex, changeControl)
		governanceStatus := scopedConfigGovernanceStatus(operation, changeRisk, changeControl, approval, verifications)
		result = append(result, gin.H{
			"audit":                 item,
			"operation":             operation,
			"has_snapshot":          hasSnapshot,
			"can_rollback":          hasSnapshot && (operation == "update" || operation == "rollback"),
			"preview_path":          fmt.Sprintf("/security/config/%s/history/%d", scope, item.ID),
			"rollback_path":         fmt.Sprintf("/security/config/%s/rollback/%d", scope, item.ID),
			"verify_path":           fmt.Sprintf("/security/config/%s/verify/%d", scope, item.ID),
			"change_control":        changeControl.response(),
			"change_risk":           changeRisk.response(),
			"approval_policy":       scopedConfigApprovalPolicy(changeRisk, changeControl, approval),
			"governance_status":     governanceStatus,
			"governance_policy":     scopedConfigGovernancePolicy(operation, changeRisk, changeControl, approval, verifications),
			"verification_required": scopedConfigVerificationRequired(operation),
			"verification_status":   scopedConfigVerificationStatus(operation, verifications),
			"verification_count":    len(verifications),
			"latest_verification":   latestScopedConfigVerificationResponse(verifications),
			"verification_template": verificationTemplate,
			"verification_policy":   scopedConfigVerificationPolicy(operation, item.ActorUserID, verificationTemplate, verifications),
		})
	}
	return result
}

type scopedConfigHistoryFilters struct {
	Action             string
	GovernanceStatus   string
	RiskLevel          string
	ApprovalStatus     string
	VerificationStatus string
	NeedsAction        *bool
}

func scopedConfigHistoryFiltersFromRequest(c *gin.Context) scopedConfigHistoryFilters {
	if c == nil {
		return scopedConfigHistoryFilters{}
	}
	filters := scopedConfigHistoryFilters{
		Action:             strings.TrimSpace(c.Query("action")),
		GovernanceStatus:   strings.TrimSpace(c.Query("governance_status")),
		RiskLevel:          strings.TrimSpace(c.Query("risk_level")),
		ApprovalStatus:     strings.TrimSpace(c.Query("approval_status")),
		VerificationStatus: strings.TrimSpace(c.Query("verification_status")),
	}
	if raw, ok := c.GetQuery("needs_action"); ok {
		trimmed := strings.TrimSpace(raw)
		if strings.EqualFold(trimmed, "true") {
			value := true
			filters.NeedsAction = &value
		} else if strings.EqualFold(trimmed, "false") {
			value := false
			filters.NeedsAction = &value
		}
	}
	return filters
}

func (f scopedConfigHistoryFilters) response() gin.H {
	result := gin.H{}
	if f.Action != "" {
		result["action"] = f.Action
	}
	if f.GovernanceStatus != "" {
		result["governance_status"] = f.GovernanceStatus
	}
	if f.RiskLevel != "" {
		result["risk_level"] = f.RiskLevel
	}
	if f.ApprovalStatus != "" {
		result["approval_status"] = f.ApprovalStatus
	}
	if f.VerificationStatus != "" {
		result["verification_status"] = f.VerificationStatus
	}
	if f.NeedsAction != nil {
		result["needs_action"] = *f.NeedsAction
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func sliceScopedConfigHistoryItemsPage(items []gin.H, page, pageSize int) []gin.H {
	page, pageSize = normalizeAuditPage(page, pageSize)
	start := (page - 1) * pageSize
	if start >= len(items) {
		return nil
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func filterScopedConfigHistoryResponses(items []gin.H, filters scopedConfigHistoryFilters) []gin.H {
	if len(items) == 0 {
		return nil
	}
	if filters.GovernanceStatus == "" && filters.RiskLevel == "" && filters.ApprovalStatus == "" && filters.VerificationStatus == "" && filters.NeedsAction == nil {
		return items
	}
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		if filters.GovernanceStatus != "" && historyItemString(item, "governance_status") != filters.GovernanceStatus {
			continue
		}
		if filters.RiskLevel != "" && historyItemNestedString(item, "change_risk", "risk_level") != filters.RiskLevel {
			continue
		}
		if filters.ApprovalStatus != "" && historyItemNestedString(item, "approval_policy", "approval_status") != filters.ApprovalStatus {
			continue
		}
		if filters.VerificationStatus != "" && historyItemString(item, "verification_status") != filters.VerificationStatus {
			continue
		}
		if filters.NeedsAction != nil && scopedConfigGovernanceNeedsAction(historyItemString(item, "governance_status")) != *filters.NeedsAction {
			continue
		}
		result = append(result, item)
	}
	return result
}

func summarizeScopedConfigHistoryResponses(items []gin.H) gin.H {
	summary := gin.H{
		"total_items":                len(items),
		"needs_action_count":         0,
		"workflow_complete_count":    0,
		"status_counts":              gin.H{},
		"risk_level_counts":          gin.H{},
		"approval_status_counts":     gin.H{},
		"verification_status_counts": gin.H{},
	}
	if len(items) == 0 {
		return summary
	}
	statusCounts := summary["status_counts"].(gin.H)
	riskCounts := summary["risk_level_counts"].(gin.H)
	approvalCounts := summary["approval_status_counts"].(gin.H)
	verificationCounts := summary["verification_status_counts"].(gin.H)
	needsActionCount := 0
	for _, item := range items {
		status := historyItemString(item, "governance_status")
		incrementHistorySummaryCounter(statusCounts, status)
		incrementHistorySummaryCounter(riskCounts, historyItemNestedString(item, "change_risk", "risk_level"))
		incrementHistorySummaryCounter(approvalCounts, historyItemNestedString(item, "approval_policy", "approval_status"))
		incrementHistorySummaryCounter(verificationCounts, historyItemString(item, "verification_status"))
		if scopedConfigGovernanceNeedsAction(status) {
			needsActionCount++
		}
	}
	summary["needs_action_count"] = needsActionCount
	summary["workflow_complete_count"] = len(items) - needsActionCount
	return summary
}

func incrementHistorySummaryCounter(target gin.H, key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "unknown"
	}
	current, _ := target[key].(int)
	target[key] = current + 1
}

func historyItemString(item gin.H, key string) string {
	if item == nil {
		return ""
	}
	return stringValueFromAny(item[key])
}

func historyItemNestedString(item gin.H, key, nestedKey string) string {
	switch typed := item[key].(type) {
	case gin.H:
		return stringValueFromAny(typed[nestedKey])
	case map[string]interface{}:
		return stringValueFromAny(typed[nestedKey])
	default:
		return ""
	}
}

func scopedConfigGovernanceNeedsAction(status string) bool {
	switch strings.TrimSpace(status) {
	case "awaiting_approval", "awaiting_verification", "verification_failed":
		return true
	default:
		return false
	}
}

func scopedConfigOperation(action string) string {
	action = strings.TrimSpace(action)
	parts := strings.Split(action, ".")
	if len(parts) >= 3 {
		return parts[len(parts)-1]
	}
	return action
}

type scopedConfigRollbackRequest struct {
	Confirm     bool   `json:"confirm"`
	ChangeRef   string `json:"change_ref"`
	Reason      string `json:"reason"`
	ApprovalRef string `json:"approval_ref"`
}

type scopedConfigWriteRequest struct {
	configscope.ScopedConfigDocument
	ChangeRef   string `json:"change_ref"`
	Reason      string `json:"reason"`
	ApprovalRef string `json:"approval_ref"`
}

type scopedConfigApprovalRequest struct {
	ChangeRef   string   `json:"change_ref"`
	Reason      string   `json:"reason"`
	ApprovalRef string   `json:"approval_ref"`
	Notes       string   `json:"notes"`
	Evidence    []string `json:"evidence"`
}

type scopedConfigVerificationRequest struct {
	Status      string                                `json:"status"`
	Notes       string                                `json:"notes"`
	Evidence    []string                              `json:"evidence"`
	Checks      []scopedConfigVerificationCheckResult `json:"checks"`
	ChangeRef   string                                `json:"change_ref"`
	Reason      string                                `json:"reason"`
	ApprovalRef string                                `json:"approval_ref"`
}

type scopedConfigChangeControl struct {
	ChangeRef   string `json:"change_ref"`
	Reason      string `json:"reason"`
	ApprovalRef string `json:"approval_ref,omitempty"`
}

type scopedConfigChangeRisk struct {
	RiskLevel        string   `json:"risk_level"`
	RiskReasons      []string `json:"risk_reasons,omitempty"`
	ChangedPaths     []string `json:"changed_paths,omitempty"`
	ApprovalRequired bool     `json:"approval_required"`
}

type scopedConfigVerificationRecord struct {
	AuditID       uint
	SourceAuditID uint
	Status        string
	Notes         string
	Evidence      []string
	Checks        []scopedConfigVerificationCheckResult
	CreatedAt     time.Time
	ActorUserID   *uint
	ChangeControl scopedConfigChangeControl
}

type scopedConfigApprovalRecord struct {
	AuditID       uint
	Notes         string
	Evidence      []string
	CreatedAt     time.Time
	ActorUserID   *uint
	ChangeControl scopedConfigChangeControl
}

type scopedConfigVerificationCheckResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Notes  string `json:"notes,omitempty"`
}

type scopedConfigVerificationCheckDefinition struct {
	ID           string   `json:"id"`
	Section      string   `json:"section"`
	RiskLevel    string   `json:"risk_level,omitempty"`
	ChangedPaths []string `json:"changed_paths,omitempty"`
	Title        string   `json:"title"`
	EvidenceHint string   `json:"evidence_hint,omitempty"`
	Required     bool     `json:"required"`
}

type scopedConfigVerificationTemplate struct {
	Operation       string                                    `json:"operation"`
	ChangedSections []string                                  `json:"changed_sections,omitempty"`
	ChangedPaths    []string                                  `json:"changed_paths,omitempty"`
	Checks          []scopedConfigVerificationCheckDefinition `json:"checks,omitempty"`
}

func (c scopedConfigChangeControl) metadata() map[string]interface{} {
	if strings.TrimSpace(c.ChangeRef) == "" && strings.TrimSpace(c.Reason) == "" && strings.TrimSpace(c.ApprovalRef) == "" {
		return nil
	}
	metadata := map[string]interface{}{
		"change_ref": strings.TrimSpace(c.ChangeRef),
		"reason":     strings.TrimSpace(c.Reason),
	}
	if approvalRef := strings.TrimSpace(c.ApprovalRef); approvalRef != "" {
		metadata["approval_ref"] = approvalRef
	}
	return metadata
}

func (c scopedConfigChangeControl) response() gin.H {
	if strings.TrimSpace(c.ChangeRef) == "" && strings.TrimSpace(c.Reason) == "" && strings.TrimSpace(c.ApprovalRef) == "" {
		return nil
	}
	result := gin.H{
		"change_ref": strings.TrimSpace(c.ChangeRef),
		"reason":     strings.TrimSpace(c.Reason),
	}
	if approvalRef := strings.TrimSpace(c.ApprovalRef); approvalRef != "" {
		result["approval_ref"] = approvalRef
	}
	return result
}

func (r scopedConfigChangeRisk) metadata() map[string]interface{} {
	if strings.TrimSpace(r.RiskLevel) == "" && len(r.RiskReasons) == 0 && len(r.ChangedPaths) == 0 && !r.ApprovalRequired {
		return nil
	}
	metadata := map[string]interface{}{
		"risk_level":        strings.TrimSpace(r.RiskLevel),
		"approval_required": r.ApprovalRequired,
	}
	if len(r.RiskReasons) > 0 {
		metadata["risk_reasons"] = append([]string(nil), r.RiskReasons...)
	}
	if len(r.ChangedPaths) > 0 {
		metadata["changed_paths"] = append([]string(nil), r.ChangedPaths...)
	}
	return metadata
}

func (r scopedConfigChangeRisk) response() gin.H {
	result := gin.H{
		"risk_level":        strings.TrimSpace(r.RiskLevel),
		"approval_required": r.ApprovalRequired,
	}
	if len(r.RiskReasons) > 0 {
		result["risk_reasons"] = append([]string(nil), r.RiskReasons...)
	}
	if len(r.ChangedPaths) > 0 {
		result["changed_paths"] = append([]string(nil), r.ChangedPaths...)
	}
	return result
}

func (v scopedConfigVerificationRecord) metadata() map[string]interface{} {
	metadata := map[string]interface{}{
		"source_audit_id": v.SourceAuditID,
		"status":          v.Status,
	}
	if v.Notes != "" {
		metadata["notes"] = v.Notes
	}
	if len(v.Evidence) > 0 {
		metadata["evidence"] = append([]string(nil), v.Evidence...)
	}
	if len(v.Checks) > 0 {
		metadata["checks"] = append([]scopedConfigVerificationCheckResult(nil), v.Checks...)
	}
	return metadata
}

func (a scopedConfigApprovalRecord) metadata() map[string]interface{} {
	metadata := map[string]interface{}{}
	if a.Notes != "" {
		metadata["notes"] = a.Notes
	}
	if len(a.Evidence) > 0 {
		metadata["evidence"] = append([]string(nil), a.Evidence...)
	}
	return metadata
}

func (a scopedConfigApprovalRecord) response() gin.H {
	result := gin.H{
		"audit_id":   a.AuditID,
		"created_at": a.CreatedAt,
	}
	if a.ActorUserID != nil {
		result["actor_user_id"] = *a.ActorUserID
	}
	if a.Notes != "" {
		result["notes"] = a.Notes
	}
	if len(a.Evidence) > 0 {
		result["evidence"] = append([]string(nil), a.Evidence...)
	}
	if changeControl := a.ChangeControl.response(); changeControl != nil {
		result["change_control"] = changeControl
	}
	return result
}

func (v scopedConfigVerificationRecord) response() gin.H {
	result := gin.H{
		"audit_id":        v.AuditID,
		"source_audit_id": v.SourceAuditID,
		"status":          v.Status,
		"created_at":      v.CreatedAt,
	}
	if v.ActorUserID != nil {
		result["actor_user_id"] = *v.ActorUserID
	}
	if v.Notes != "" {
		result["notes"] = v.Notes
	}
	if len(v.Evidence) > 0 {
		result["evidence"] = append([]string(nil), v.Evidence...)
	}
	if len(v.Checks) > 0 {
		result["checks"] = append([]scopedConfigVerificationCheckResult(nil), v.Checks...)
	}
	if changeControl := v.ChangeControl.response(); changeControl != nil {
		result["change_control"] = changeControl
	}
	return result
}

func resolveScopedConfigChangeControl(c *gin.Context, bodyChangeRef, bodyReason, bodyApprovalRef string) (scopedConfigChangeControl, *ErrorResponse) {
	changeControl := scopedConfigChangeControl{
		ChangeRef:   firstNonEmpty(bodyChangeRef, c.GetHeader("X-Change-Ref")),
		Reason:      firstNonEmpty(bodyReason, c.GetHeader("X-Change-Reason")),
		ApprovalRef: firstNonEmpty(bodyApprovalRef, c.GetHeader("X-Approval-Ref")),
	}
	if strings.TrimSpace(changeControl.ChangeRef) == "" || strings.TrimSpace(changeControl.Reason) == "" {
		return scopedConfigChangeControl{}, &ErrorResponse{
			Code:    http.StatusBadRequest,
			Error:   "Change control required",
			Message: "provide change_ref and reason via JSON body or X-Change-Ref / X-Change-Reason headers",
		}
	}
	return changeControl, nil
}

func parseScopedConfigRollbackRequest(c *gin.Context) (scopedConfigRollbackRequest, *ErrorResponse) {
	var req scopedConfigRollbackRequest
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return req, nil
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return scopedConfigRollbackRequest{}, &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid request", Message: err.Error()}
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if len(bytes.TrimSpace(body)) == 0 {
		return req, nil
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return scopedConfigRollbackRequest{}, &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid request", Message: err.Error()}
	}
	return req, nil
}

func parseScopedConfigApprovalRequest(c *gin.Context) (scopedConfigApprovalRequest, scopedConfigChangeControl, *ErrorResponse) {
	var req scopedConfigApprovalRequest
	if c == nil {
		return req, scopedConfigChangeControl{}, &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid request", Message: "request context is required"}
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return scopedConfigApprovalRequest{}, scopedConfigChangeControl{}, &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid request", Message: err.Error()}
	}
	req.Notes = strings.TrimSpace(req.Notes)
	req.Evidence = sanitizeStringSlice(req.Evidence)
	if req.Notes == "" {
		return scopedConfigApprovalRequest{}, scopedConfigChangeControl{}, &ErrorResponse{
			Code:    http.StatusBadRequest,
			Error:   "Approval notes required",
			Message: "notes are required when recording scoped config approval",
		}
	}
	changeControl, errResponse := resolveScopedConfigChangeControl(c, req.ChangeRef, req.Reason, req.ApprovalRef)
	if errResponse != nil {
		return scopedConfigApprovalRequest{}, scopedConfigChangeControl{}, errResponse
	}
	if strings.TrimSpace(changeControl.ApprovalRef) == "" {
		return scopedConfigApprovalRequest{}, scopedConfigChangeControl{}, &ErrorResponse{
			Code:    http.StatusBadRequest,
			Error:   "Approval reference required",
			Message: "approval_ref is required via JSON body or X-Approval-Ref header when recording scoped config approval",
		}
	}
	return req, changeControl, nil
}

func parseScopedConfigVerificationRequest(c *gin.Context) (scopedConfigVerificationRequest, scopedConfigChangeControl, *ErrorResponse) {
	var req scopedConfigVerificationRequest
	if c == nil {
		return req, scopedConfigChangeControl{}, &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid request", Message: "request context is required"}
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return scopedConfigVerificationRequest{}, scopedConfigChangeControl{}, &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid request", Message: err.Error()}
	}
	req.Status = normalizeScopedConfigVerificationStatus(req.Status)
	if req.Status == "" {
		return scopedConfigVerificationRequest{}, scopedConfigChangeControl{}, &ErrorResponse{
			Code:    http.StatusBadRequest,
			Error:   "Invalid verification status",
			Message: "status must be passed or failed",
		}
	}
	req.Notes = strings.TrimSpace(req.Notes)
	req.Evidence = sanitizeStringSlice(req.Evidence)
	req.Checks = sanitizeScopedConfigVerificationChecks(req.Checks)
	if req.Status == "passed" && len(req.Evidence) == 0 {
		return scopedConfigVerificationRequest{}, scopedConfigChangeControl{}, &ErrorResponse{
			Code:    http.StatusBadRequest,
			Error:   "Verification evidence required",
			Message: "evidence is required when verification status is passed",
		}
	}
	if req.Status == "failed" && req.Notes == "" {
		return scopedConfigVerificationRequest{}, scopedConfigChangeControl{}, &ErrorResponse{
			Code:    http.StatusBadRequest,
			Error:   "Verification notes required",
			Message: "notes are required when verification status is failed",
		}
	}
	changeControl, errResponse := resolveScopedConfigChangeControl(c, req.ChangeRef, req.Reason, req.ApprovalRef)
	if errResponse != nil {
		return scopedConfigVerificationRequest{}, scopedConfigChangeControl{}, errResponse
	}
	return req, changeControl, nil
}

func requireRollbackConfirmation(c *gin.Context, bodyConfirm bool) bool {
	if strings.EqualFold(strings.TrimSpace(c.Query("confirm")), "true") {
		return true
	}
	return bodyConfirm
}

func (h *ScopedConfigHandler) approve(c *gin.Context, scope string) {
	tenantID, workspaceID, ok := requireScope(c, scope)
	if !ok {
		return
	}
	if h == nil || h.audit == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Audit service unavailable", Message: "audit query service not configured"})
		return
	}
	req, changeControl, errResponse := parseScopedConfigApprovalRequest(c)
	if errResponse != nil {
		c.JSON(errResponse.Code, errResponse)
		return
	}
	approverUserID := scopedConfigContextUserID(c)
	if approverUserID == nil {
		c.JSON(http.StatusForbidden, ErrorResponse{
			Error:   "Approval reviewer required",
			Message: "scoped config approval requires an authenticated approver user_id in request context",
		})
		return
	}
	approval := scopedConfigApprovalRecord{
		Notes:         req.Notes,
		Evidence:      req.Evidence,
		CreatedAt:     time.Now().UTC(),
		ActorUserID:   approverUserID,
		ChangeControl: changeControl,
	}
	if latestApproval, err := h.latestScopedConfigApproval(c.Request.Context(), scope, tenantID, workspaceID, changeControl); err == nil && latestApproval != nil {
		auditplatform.SetBefore(c, latestApproval.response())
	}
	setScopedConfigAuditMeta(c, scope, tenantID, workspaceID, "approve", changeControl)
	auditplatform.MergeRequestMetadata(c, approval.metadata())
	auditplatform.SetAfter(c, approval.response())
	c.JSON(http.StatusOK, gin.H{
		"tenant_id":       tenantID,
		"workspace_id":    workspaceID,
		"approval":        approval.response(),
		"change_control":  changeControl.response(),
		"approval_policy": gin.H{"approval_status": "approved", "required": true, "same_actor_allowed": false},
	})
}

func (h *ScopedConfigHandler) rollback(c *gin.Context, scope string) {
	tenantID, workspaceID, ok := requireScope(c, scope)
	if !ok {
		return
	}
	if h == nil || h.store == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Config store unavailable", Message: "config store not configured"})
		return
	}
	if h.audit == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Audit service unavailable", Message: "audit query service not configured"})
		return
	}
	req, errResponse := parseScopedConfigRollbackRequest(c)
	if errResponse != nil {
		c.JSON(errResponse.Code, errResponse)
		return
	}
	if !requireRollbackConfirmation(c, req.Confirm) {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Rollback confirmation required", Message: "set confirm=true in query or JSON body to execute rollback"})
		return
	}
	changeControl, errResponse := resolveScopedConfigChangeControl(c, req.ChangeRef, req.Reason, req.ApprovalRef)
	if errResponse != nil {
		c.JSON(errResponse.Code, errResponse)
		return
	}
	entry, snapshot, response, err := h.loadHistoryEntry(c, scope, tenantID, workspaceID)
	if response != nil {
		c.JSON(response.Code, response)
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load config history entry", Message: err.Error()})
		return
	}
	var current *configscope.ScopedConfigDocument
	if scope == "tenant" {
		snapshot.TenantID = tenantID
		snapshot.WorkspaceID = ""
		if before, ok, err := h.store.GetTenantConfig(c.Request.Context(), tenantID); err == nil && ok {
			auditplatform.SetBefore(c, before)
			current = before
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load tenant config", Message: err.Error()})
			return
		}
		changeTemplate := scopedConfigVerificationTemplateForDocuments("rollback", current, snapshot)
		changeRisk := scopedConfigChangeRiskForTemplate("rollback", changeTemplate)
		approval, err := h.latestScopedConfigApproval(c.Request.Context(), scope, tenantID, "", changeControl)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load scoped config approval", Message: err.Error()})
			return
		}
		if errResponse := validateScopedConfigApproval(changeControl, changeRisk, approval, scopedConfigContextUserID(c)); errResponse != nil {
			c.JSON(errResponse.Code, errResponse)
			return
		}
		governanceStatus := scopedConfigGovernanceStatus("rollback", changeRisk, changeControl, approval, nil)
		setScopedConfigAuditMeta(c, scope, tenantID, "", "rollback", changeControl)
		auditplatform.MergeRequestMetadata(c, changeRisk.metadata())
		doc, err := h.store.UpsertTenantConfig(c.Request.Context(), tenantID, *snapshot)
		if err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to rollback tenant config", Message: err.Error()})
			return
		}
		auditplatform.SetAfter(c, doc)
		c.JSON(http.StatusOK, gin.H{
			"tenant_id":         tenantID,
			"source_audit_id":   entry.ID,
			"config":            doc,
			"change_control":    changeControl.response(),
			"change_risk":       changeRisk.response(),
			"approval_policy":   scopedConfigApprovalPolicy(changeRisk, changeControl, approval),
			"governance_status": governanceStatus,
			"governance_policy": scopedConfigGovernancePolicy("rollback", changeRisk, changeControl, approval, nil),
		})
		return
	}

	snapshot.TenantID = tenantID
	snapshot.WorkspaceID = workspaceID
	if before, ok, err := h.store.GetWorkspaceConfig(c.Request.Context(), tenantID, workspaceID); err == nil && ok {
		auditplatform.SetBefore(c, before)
		current = before
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load workspace config", Message: err.Error()})
		return
	}
	changeTemplate := scopedConfigVerificationTemplateForDocuments("rollback", current, snapshot)
	changeRisk := scopedConfigChangeRiskForTemplate("rollback", changeTemplate)
	approval, err := h.latestScopedConfigApproval(c.Request.Context(), scope, tenantID, workspaceID, changeControl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load scoped config approval", Message: err.Error()})
		return
	}
	if errResponse := validateScopedConfigApproval(changeControl, changeRisk, approval, scopedConfigContextUserID(c)); errResponse != nil {
		c.JSON(errResponse.Code, errResponse)
		return
	}
	governanceStatus := scopedConfigGovernanceStatus("rollback", changeRisk, changeControl, approval, nil)
	setScopedConfigAuditMeta(c, scope, tenantID, workspaceID, "rollback", changeControl)
	auditplatform.MergeRequestMetadata(c, changeRisk.metadata())
	doc, err := h.store.UpsertWorkspaceConfig(c.Request.Context(), tenantID, workspaceID, *snapshot)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed to rollback workspace config", Message: err.Error()})
		return
	}
	auditplatform.SetAfter(c, doc)
	c.JSON(http.StatusOK, gin.H{
		"tenant_id":         tenantID,
		"workspace_id":      workspaceID,
		"source_audit_id":   entry.ID,
		"config":            doc,
		"change_control":    changeControl.response(),
		"change_risk":       changeRisk.response(),
		"approval_policy":   scopedConfigApprovalPolicy(changeRisk, changeControl, approval),
		"governance_status": governanceStatus,
		"governance_policy": scopedConfigGovernancePolicy("rollback", changeRisk, changeControl, approval, nil),
	})
}

func (h *ScopedConfigHandler) verify(c *gin.Context, scope string) {
	tenantID, workspaceID, ok := requireScope(c, scope)
	if !ok {
		return
	}
	if h == nil || h.audit == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Audit service unavailable", Message: "audit query service not configured"})
		return
	}
	req, changeControl, errResponse := parseScopedConfigVerificationRequest(c)
	if errResponse != nil {
		c.JSON(errResponse.Code, errResponse)
		return
	}

	entry, _, response, err := h.loadHistoryEntry(c, scope, tenantID, workspaceID)
	if response != nil {
		c.JSON(response.Code, response)
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load config history entry", Message: err.Error()})
		return
	}
	reviewerUserID := scopedConfigContextUserID(c)
	if reviewerUserID == nil {
		c.JSON(http.StatusForbidden, ErrorResponse{
			Error:   "Verification reviewer required",
			Message: "verification requires an authenticated reviewer user_id in request context",
		})
		return
	}
	if entry.ActorUserID != nil && *entry.ActorUserID == *reviewerUserID {
		c.JSON(http.StatusForbidden, ErrorResponse{
			Error:   "Verification reviewer separation required",
			Message: "verification must be completed by a different user than the original config operator",
		})
		return
	}
	verificationTemplate := scopedConfigVerificationTemplateFromAudit(entry)
	sourceChangeControl := extractScopedConfigChangeControl(entry.RequestJSON)
	sourceChangeRisk := scopedConfigChangeRiskForTemplate(scopedConfigOperation(entry.Action), verificationTemplate)
	if errResponse := validateScopedConfigVerificationSubmission(req, verificationTemplate); errResponse != nil {
		c.JSON(errResponse.Code, errResponse)
		return
	}

	existingVerifications, err := h.listScopedConfigVerificationHistory(c.Request.Context(), scope, tenantID, workspaceID, entry.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load config verification history", Message: err.Error()})
		return
	}
	sourceApproval, err := h.latestScopedConfigApproval(c.Request.Context(), scope, tenantID, workspaceID, sourceChangeControl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to load config approval history", Message: err.Error()})
		return
	}
	verification := scopedConfigVerificationRecord{
		SourceAuditID: entry.ID,
		Status:        req.Status,
		Notes:         req.Notes,
		Evidence:      req.Evidence,
		Checks:        req.Checks,
		CreatedAt:     time.Now().UTC(),
		ActorUserID:   reviewerUserID,
		ChangeControl: changeControl,
	}
	responseVerifications := append([]scopedConfigVerificationRecord{verification}, existingVerifications...)
	sourceGovernanceStatus := scopedConfigGovernanceStatus(scopedConfigOperation(entry.Action), sourceChangeRisk, sourceChangeControl, sourceApproval, responseVerifications)
	if len(existingVerifications) > 0 {
		auditplatform.SetBefore(c, existingVerifications[0].response())
	} else {
		auditplatform.SetBefore(c, gin.H{
			"source_audit_id": entry.ID,
			"status":          "pending",
		})
	}
	setScopedConfigAuditMeta(c, scope, tenantID, workspaceID, "verify", changeControl)
	auditplatform.MergeRequestMetadata(c, verification.metadata())
	auditplatform.SetAfter(c, verification.response())

	c.JSON(http.StatusOK, gin.H{
		"source_audit_id":          entry.ID,
		"source_action":            entry.Action,
		"verification_status":      req.Status,
		"verification":             verification.response(),
		"source_change_control":    sourceChangeControl.response(),
		"source_change_risk":       sourceChangeRisk.response(),
		"source_approval_policy":   scopedConfigApprovalPolicy(sourceChangeRisk, sourceChangeControl, sourceApproval),
		"source_governance_status": sourceGovernanceStatus,
		"source_governance_policy": scopedConfigGovernancePolicy(scopedConfigOperation(entry.Action), sourceChangeRisk, sourceChangeControl, sourceApproval, responseVerifications),
		"verification_template":    verificationTemplate,
		"verification_required":    true,
		"verification_policy":      scopedConfigVerificationPolicy(scopedConfigOperation(entry.Action), entry.ActorUserID, verificationTemplate, responseVerifications),
	})
}

func requireScope(c *gin.Context, scope string) (string, string, bool) {
	tenantID := platformauth.TenantIDFromContext(c.Request.Context())
	workspaceID := platformauth.WorkspaceIDFromContext(c.Request.Context())
	if scope == "tenant" {
		if tenantID == "" {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Tenant scope required", Message: "tenant_id is required in request scope"})
			return "", "", false
		}
		return tenantID, "", true
	}
	if tenantID == "" || workspaceID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Workspace scope required", Message: "tenant_id and workspace_id are required in request scope"})
		return "", "", false
	}
	return tenantID, workspaceID, true
}

func setScopedConfigAuditMeta(c *gin.Context, scope, tenantID, workspaceID, operation string, changeControl scopedConfigChangeControl) {
	if c == nil {
		return
	}
	auditplatform.SetAction(c, fmt.Sprintf("scoped_config.%s.%s", scope, operation))
	auditplatform.SetResourceType(c, "scoped_config")
	auditplatform.SetResourceID(c, scopedConfigResourceID(tenantID, workspaceID))
	auditplatform.MergeRequestMetadata(c, changeControl.metadata())
}

func scopedConfigResourceID(tenantID, workspaceID string) string {
	if workspaceID == "" {
		return tenantID
	}
	return tenantID + "/" + workspaceID
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func extractScopedConfigChangeControl(requestJSON string) scopedConfigChangeControl {
	if strings.TrimSpace(requestJSON) == "" {
		return scopedConfigChangeControl{}
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(requestJSON), &payload); err != nil {
		return scopedConfigChangeControl{}
	}
	changeControl := scopedConfigChangeControl{
		ChangeRef:   stringValueFromAny(payload["change_ref"]),
		Reason:      stringValueFromAny(payload["reason"]),
		ApprovalRef: stringValueFromAny(payload["approval_ref"]),
	}
	if changeControl.ChangeRef != "" && changeControl.Reason != "" {
		return changeControl
	}
	auditValue, ok := payload["_audit"]
	if !ok {
		return changeControl
	}
	auditMap, ok := auditValue.(map[string]interface{})
	if !ok {
		return changeControl
	}
	return scopedConfigChangeControl{
		ChangeRef:   firstNonEmpty(changeControl.ChangeRef, stringValueFromAny(auditMap["change_ref"])),
		Reason:      firstNonEmpty(changeControl.Reason, stringValueFromAny(auditMap["reason"])),
		ApprovalRef: firstNonEmpty(changeControl.ApprovalRef, stringValueFromAny(auditMap["approval_ref"])),
	}
}

func stringValueFromAny(value interface{}) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func scopedConfigContextUserID(c *gin.Context) *uint {
	if c == nil {
		return nil
	}
	value, ok := c.Get("user_id")
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case uint:
		return &typed
	case int:
		if typed <= 0 {
			return nil
		}
		id := uint(typed)
		return &id
	default:
		return nil
	}
}

func normalizeScopedConfigVerificationStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "passed", "failed":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return ""
	}
}

func sanitizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeScopedConfigVerificationCheckStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "passed", "failed", "skipped":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return ""
	}
}

func sanitizeScopedConfigVerificationChecks(checks []scopedConfigVerificationCheckResult) []scopedConfigVerificationCheckResult {
	if len(checks) == 0 {
		return nil
	}
	result := make([]scopedConfigVerificationCheckResult, 0, len(checks))
	seen := make(map[string]struct{}, len(checks))
	for _, check := range checks {
		check.ID = strings.TrimSpace(check.ID)
		check.Status = normalizeScopedConfigVerificationCheckStatus(check.Status)
		check.Notes = strings.TrimSpace(check.Notes)
		if check.ID == "" || check.Status == "" {
			continue
		}
		if _, exists := seen[check.ID]; exists {
			continue
		}
		seen[check.ID] = struct{}{}
		result = append(result, check)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func stringSliceFromAny(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text := stringValueFromAny(item); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func scopedConfigVerificationChecksFromAny(value interface{}) []scopedConfigVerificationCheckResult {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]scopedConfigVerificationCheckResult, 0, len(items))
	for _, item := range items {
		record, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		result = append(result, scopedConfigVerificationCheckResult{
			ID:     payloadStringValue(record, "id"),
			Status: normalizeScopedConfigVerificationCheckStatus(payloadStringValue(record, "status")),
			Notes:  payloadStringValue(record, "notes"),
		})
	}
	return sanitizeScopedConfigVerificationChecks(result)
}

func uintValueFromAny(value interface{}) uint {
	switch typed := value.(type) {
	case float64:
		if typed <= 0 {
			return 0
		}
		return uint(typed)
	case int:
		if typed <= 0 {
			return 0
		}
		return uint(typed)
	case int64:
		if typed <= 0 {
			return 0
		}
		return uint(typed)
	case uint:
		return typed
	case uint64:
		return uint(typed)
	case string:
		raw := strings.TrimSpace(typed)
		if raw == "" {
			return 0
		}
		id, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return 0
		}
		return uint(id)
	default:
		return 0
	}
}

func normalizeAuditPage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}

func filterScopedConfigHistoryEntries(items []models.AuditLog, action string) []models.AuditLog {
	action = strings.TrimSpace(action)
	if action == "" {
		return items
	}
	result := make([]models.AuditLog, 0, len(items))
	for _, item := range items {
		if item.Action == action || scopedConfigOperation(item.Action) == action {
			result = append(result, item)
		}
	}
	return result
}

func scopedConfigVerificationRequired(operation string) bool {
	return operation == "update" || operation == "rollback"
}

func scopedConfigVerificationTemplateFromAudit(entry *models.AuditLog) scopedConfigVerificationTemplate {
	if entry == nil {
		return scopedConfigVerificationTemplate{}
	}
	operation := scopedConfigOperation(entry.Action)
	if !scopedConfigVerificationRequired(operation) {
		return scopedConfigVerificationTemplate{Operation: operation}
	}
	before := scopedConfigDocumentFromAuditJSON(entry.BeforeJSON)
	after := scopedConfigDocumentFromAuditJSON(entry.AfterJSON)
	changedSections := scopedConfigChangedSections(before, after)
	changedPaths := scopedConfigChangedPaths(before, after)
	return scopedConfigVerificationTemplateForChangeSet(operation, changedSections, changedPaths)
}

func scopedConfigVerificationTemplateForChangeSet(operation string, changedSections, changedPaths []string) scopedConfigVerificationTemplate {
	checks := []scopedConfigVerificationCheckDefinition{
		{
			ID:           "change_scope_reviewed",
			Section:      "scope",
			Title:        "确认本次配置变更只影响目标 tenant/workspace 作用域",
			EvidenceHint: "作用域截图、租户/工作区定位信息或接口返回片段",
			Required:     true,
		},
		{
			ID:           "runtime_effect_confirmed",
			Section:      "runtime",
			Title:        "确认配置已在目标作用域生效且未影响其它作用域",
			EvidenceHint: "配置读取结果、冒烟验证结果或关键接口响应",
			Required:     true,
		},
	}
	if operation == "rollback" {
		checks = append(checks, scopedConfigVerificationCheckDefinition{
			ID:           "rollback_baseline_restored",
			Section:      "rollback",
			Title:        "确认回滚后基线已恢复且问题症状解除",
			EvidenceHint: "回滚前后对比、异常消失证明或恢复后的关键响应",
			Required:     true,
		})
	}
	for _, section := range changedSections {
		switch section {
		case "portal":
			checks = append(checks, scopedConfigVerificationCheckDefinition{
				ID:           "portal_render_verified",
				Section:      "portal",
				Title:        "确认 portal 外观、品牌和文案在目标作用域渲染正确",
				EvidenceHint: "目标 tenant/workspace 的 portal 页面截图或公开配置响应",
				Required:     true,
			})
		case "openai":
			checks = append(checks, scopedConfigVerificationCheckDefinition{
				ID:           "openai_provider_verified",
				Section:      "openai",
				Title:        "确认 OpenAI provider 配置可正常调用且模型/路由符合预期",
				EvidenceHint: "模型探活、一次真实请求结果或 provider 健康检查记录",
				Required:     true,
			})
		case "weknora":
			checks = append(checks, scopedConfigVerificationCheckDefinition{
				ID:           "weknora_provider_verified",
				Section:      "weknora",
				Title:        "确认 knowledge provider 配置、知识库映射和访问凭证在目标作用域可用（当前兼容路径可能为 WeKnora）",
				EvidenceHint: "知识库查询结果、provider 健康检查或同步/检索结果；若当前命中 WeKnora compatibility，也应附对应证据",
				Required:     true,
			})
		case "session_risk":
			checks = append(checks, scopedConfigVerificationCheckDefinition{
				ID:           "session_risk_policy_verified",
				Section:      "session_risk",
				Title:        "确认 session risk 阈值与风控行为符合目标环境预期",
				EvidenceHint: "登录/refresh 冒烟结果、风控评分样本或阈值读取结果",
				Required:     true,
			})
		}
	}
	if portalPublicPaths := changedPathsForPrefixes(changedPaths, "portal.brand_name", "portal.logo_url", "portal.primary_color", "portal.secondary_color", "portal.support_email"); len(portalPublicPaths) > 0 {
		checks = append(checks, scopedConfigVerificationCheckDefinition{
			ID:           "portal_public_surface_verified",
			Section:      "portal",
			RiskLevel:    "medium",
			ChangedPaths: portalPublicPaths,
			Title:        "确认 portal 对外品牌、主题和联系入口展示正确",
			EvidenceHint: "目标 tenant/workspace 的 portal 页面截图或公开配置响应",
			Required:     true,
		})
	}
	if portalLocalePaths := changedPathsForPrefixes(changedPaths, "portal.default_locale", "portal.locales"); len(portalLocalePaths) > 0 {
		checks = append(checks, scopedConfigVerificationCheckDefinition{
			ID:           "portal_locale_contract_verified",
			Section:      "portal",
			RiskLevel:    "medium",
			ChangedPaths: portalLocalePaths,
			Title:        "确认 portal 默认语言和可选语言契约符合预期",
			EvidenceHint: "多语言切换截图、locale 响应片段或页面文案检查记录",
			Required:     true,
		})
	}
	if openAIEndpointPaths := changedPathsForPrefixes(changedPaths, "openai.api_key", "openai.base_url", "openai.timeout"); len(openAIEndpointPaths) > 0 {
		checks = append(checks, scopedConfigVerificationCheckDefinition{
			ID:           "openai_provider_endpoint_verified",
			Section:      "openai",
			RiskLevel:    "high",
			ChangedPaths: openAIEndpointPaths,
			Title:        "确认 OpenAI provider 终端、凭证和超时配置可正常调用",
			EvidenceHint: "provider 健康检查、真实调用结果或连接性验证记录",
			Required:     true,
		})
	}
	if openAIModelPaths := changedPathsForPrefixes(changedPaths, "openai.model", "openai.temperature", "openai.max_tokens"); len(openAIModelPaths) > 0 {
		checks = append(checks, scopedConfigVerificationCheckDefinition{
			ID:           "openai_model_contract_verified",
			Section:      "openai",
			RiskLevel:    "medium",
			ChangedPaths: openAIModelPaths,
			Title:        "确认 OpenAI 模型、温度和 token 策略符合预期",
			EvidenceHint: "模型探活、一次真实请求结果或生成参数验证记录",
			Required:     true,
		})
	}
	if weKnoraEndpointPaths := changedPathsForPrefixes(changedPaths, "weknora.api_key", "weknora.base_url", "weknora.timeout", "weknora.max_retries"); len(weKnoraEndpointPaths) > 0 {
		checks = append(checks, scopedConfigVerificationCheckDefinition{
			ID:           "weknora_provider_endpoint_verified",
			Section:      "weknora",
			RiskLevel:    "high",
			ChangedPaths: weKnoraEndpointPaths,
			Title:        "确认 knowledge provider 终端、凭证和超时配置可正常访问（当前字段为 WeKnora compatibility 配置）",
			EvidenceHint: "provider 健康检查、连接探活结果或关键接口响应；若当前命中 WeKnora compatibility，应附兼容链路证据",
			Required:     true,
		})
	}
	if weKnoraMappingPaths := changedPathsForPrefixes(changedPaths, "weknora.enabled", "weknora.tenant_id", "weknora.knowledge_base_id", "weknora.search"); len(weKnoraMappingPaths) > 0 {
		checks = append(checks, scopedConfigVerificationCheckDefinition{
			ID:           "weknora_knowledge_mapping_verified",
			Section:      "weknora",
			RiskLevel:    "high",
			ChangedPaths: weKnoraMappingPaths,
			Title:        "确认 knowledge provider 知识库映射、检索策略和启停状态符合预期（当前字段为 WeKnora compatibility 配置）",
			EvidenceHint: "知识库查询结果、同步/检索结果或目标 KB 映射校验记录",
			Required:     true,
		})
	}
	if weKnoraHealthPaths := changedPathsForPrefixes(changedPaths, "weknora.health_check"); len(weKnoraHealthPaths) > 0 {
		checks = append(checks, scopedConfigVerificationCheckDefinition{
			ID:           "weknora_health_check_verified",
			Section:      "weknora",
			RiskLevel:    "medium",
			ChangedPaths: weKnoraHealthPaths,
			Title:        "确认 knowledge provider 健康检查策略与告警节奏符合预期（当前字段为 WeKnora compatibility 配置）",
			EvidenceHint: "健康检查配置读取结果、探测日志或告警样本",
			Required:     true,
		})
	}
	if sessionRiskScorePaths := changedPathsForPrefixes(changedPaths, "session_risk.medium_risk_score", "session_risk.high_risk_score"); len(sessionRiskScorePaths) > 0 {
		checks = append(checks, scopedConfigVerificationCheckDefinition{
			ID:           "session_risk_score_threshold_verified",
			Section:      "session_risk",
			RiskLevel:    "high",
			ChangedPaths: sessionRiskScorePaths,
			Title:        "确认 session risk 分级阈值符合目标环境风控预期",
			EvidenceHint: "风控评分样本、策略读取结果或登录/refresh 验证记录",
			Required:     true,
		})
	}
	if sessionRiskWindowPaths := changedPathsForPrefixes(changedPaths, "session_risk.hot_refresh_window_minutes", "session_risk.recent_refresh_window_minutes", "session_risk.today_refresh_window_hours", "session_risk.rapid_change_window_hours", "session_risk.stale_activity_window_days"); len(sessionRiskWindowPaths) > 0 {
		checks = append(checks, scopedConfigVerificationCheckDefinition{
			ID:           "session_risk_window_verified",
			Section:      "session_risk",
			RiskLevel:    "medium",
			ChangedPaths: sessionRiskWindowPaths,
			Title:        "确认 session risk 时间窗口配置符合预期",
			EvidenceHint: "时间窗口读取结果、风控样本或刷新链路验证记录",
			Required:     true,
		})
	}
	if sessionRiskConcurrencyPaths := changedPathsForPrefixes(changedPaths, "session_risk.multi_public_ip_threshold", "session_risk.many_sessions_threshold", "session_risk.hot_refresh_family_threshold"); len(sessionRiskConcurrencyPaths) > 0 {
		checks = append(checks, scopedConfigVerificationCheckDefinition{
			ID:           "session_risk_concurrency_threshold_verified",
			Section:      "session_risk",
			RiskLevel:    "medium",
			ChangedPaths: sessionRiskConcurrencyPaths,
			Title:        "确认 session risk 并发与漂移阈值符合预期",
			EvidenceHint: "多设备/多公网 IP 样本、风险分数或会话列表验证结果",
			Required:     true,
		})
	}
	return scopedConfigVerificationTemplate{
		Operation:       operation,
		ChangedSections: changedSections,
		ChangedPaths:    changedPaths,
		Checks:          checks,
	}
}

func scopedConfigVerificationTemplateForHistoryEntry(entry *models.AuditLog, current, snapshot *configscope.ScopedConfigDocument) scopedConfigVerificationTemplate {
	if entry == nil {
		return scopedConfigVerificationTemplate{}
	}
	auditTemplate := scopedConfigVerificationTemplateFromAudit(entry)
	if strings.TrimSpace(entry.BeforeJSON) != "" || current == nil || snapshot == nil {
		return auditTemplate
	}
	if changedPaths := extractScopedConfigChangedPaths(entry.RequestJSON); len(changedPaths) > 0 {
		return scopedConfigVerificationTemplateForChangeSet(
			scopedConfigOperation(entry.Action),
			scopedConfigChangedSectionsFromPaths(changedPaths),
			changedPaths,
		)
	}
	return scopedConfigVerificationTemplateForDocuments(scopedConfigOperation(entry.Action), current, snapshot)
}

func scopedConfigDocumentFromAuditJSON(raw string) *configscope.ScopedConfigDocument {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var doc configscope.ScopedConfigDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil
	}
	return &doc
}

func scopedConfigChangedSections(before, after *configscope.ScopedConfigDocument) []string {
	if before == nil {
		before = &configscope.ScopedConfigDocument{}
	}
	if after == nil {
		after = &configscope.ScopedConfigDocument{}
	}
	sections := make([]string, 0, 4)
	if len(diffJSONChanges(before.Portal, after.Portal, "portal")) > 0 {
		sections = append(sections, "portal")
	}
	if len(diffJSONChanges(before.OpenAI, after.OpenAI, "openai")) > 0 {
		sections = append(sections, "openai")
	}
	if len(diffJSONChanges(before.WeKnora, after.WeKnora, "weknora")) > 0 {
		sections = append(sections, "weknora")
	}
	if len(diffJSONChanges(before.SessionRisk, after.SessionRisk, "session_risk")) > 0 {
		sections = append(sections, "session_risk")
	}
	if len(sections) == 0 {
		sections = presentSections(after)
	}
	return sections
}

func scopedConfigChangedPaths(before, after *configscope.ScopedConfigDocument) []string {
	if before == nil {
		before = &configscope.ScopedConfigDocument{}
	}
	if after == nil {
		after = &configscope.ScopedConfigDocument{}
	}
	result := make([]string, 0, 8)
	result = append(result, changePaths(diffJSONChanges(before.Portal, after.Portal, "portal"))...)
	result = append(result, changePaths(diffJSONChanges(before.OpenAI, after.OpenAI, "openai"))...)
	result = append(result, changePaths(diffJSONChanges(before.WeKnora, after.WeKnora, "weknora"))...)
	result = append(result, changePaths(diffJSONChanges(before.SessionRisk, after.SessionRisk, "session_risk"))...)
	return normalizeScopedConfigChangedPaths(result)
}

func normalizeScopedConfigChangedPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	result := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	if len(result) == 0 {
		return nil
	}
	sort.Strings(result)
	return result
}

func scopedConfigChangedSectionsFromPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	sections := make([]string, 0, 4)
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		section := path
		if idx := strings.Index(section, "."); idx >= 0 {
			section = section[:idx]
		}
		switch section {
		case "portal", "openai", "weknora", "session_risk":
			if _, ok := seen[section]; ok {
				continue
			}
			seen[section] = struct{}{}
			sections = append(sections, section)
		}
	}
	if len(sections) == 0 {
		return nil
	}
	sort.Strings(sections)
	return sections
}

func changedPathsForPrefixes(paths []string, prefixes ...string) []string {
	if len(paths) == 0 || len(prefixes) == 0 {
		return nil
	}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		for _, prefix := range prefixes {
			prefix = strings.TrimSpace(prefix)
			if prefix == "" {
				continue
			}
			if path == prefix || strings.HasPrefix(path, prefix+".") {
				result = append(result, path)
				break
			}
		}
	}
	return normalizeScopedConfigChangedPaths(result)
}

func validateScopedConfigVerificationSubmission(req scopedConfigVerificationRequest, template scopedConfigVerificationTemplate) *ErrorResponse {
	if req.Status == "" {
		return &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid verification status", Message: "status must be passed or failed"}
	}
	if len(template.Checks) == 0 {
		return nil
	}
	if len(req.Checks) == 0 {
		return &ErrorResponse{
			Code:    http.StatusBadRequest,
			Error:   "Verification checks required",
			Message: "checks are required to submit scoped config verification",
		}
	}
	requiredChecks := make(map[string]struct{}, len(template.Checks))
	for _, definition := range template.Checks {
		requiredChecks[definition.ID] = struct{}{}
	}
	checkStatus := make(map[string]string, len(req.Checks))
	for _, check := range req.Checks {
		if _, ok := requiredChecks[check.ID]; !ok {
			return &ErrorResponse{
				Code:    http.StatusBadRequest,
				Error:   "Invalid verification check",
				Message: fmt.Sprintf("unknown verification check id %q", check.ID),
			}
		}
		checkStatus[check.ID] = check.Status
	}
	if req.Status == "passed" {
		for _, definition := range template.Checks {
			if !definition.Required {
				continue
			}
			if checkStatus[definition.ID] != "passed" {
				return &ErrorResponse{
					Code:    http.StatusBadRequest,
					Error:   "Verification checks incomplete",
					Message: fmt.Sprintf("required verification check %q must be passed before marking verification as passed", definition.ID),
				}
			}
		}
		return nil
	}
	for _, status := range checkStatus {
		if status == "failed" {
			return nil
		}
	}
	return &ErrorResponse{
		Code:    http.StatusBadRequest,
		Error:   "Failed verification check required",
		Message: "at least one verification check must be marked failed when verification status is failed",
	}
}

func extractScopedConfigChangedPaths(requestJSON string) []string {
	requestJSON = strings.TrimSpace(requestJSON)
	if requestJSON == "" {
		return nil
	}
	var payload struct {
		ChangedPaths []string `json:"changed_paths"`
	}
	if err := json.Unmarshal([]byte(requestJSON), &payload); err != nil {
		return nil
	}
	return normalizeScopedConfigChangedPaths(payload.ChangedPaths)
}

func scopedConfigVerificationTemplateForDocuments(operation string, before, after *configscope.ScopedConfigDocument) scopedConfigVerificationTemplate {
	return scopedConfigVerificationTemplateFromAudit(&models.AuditLog{
		Action:     operation,
		BeforeJSON: jsonValue(before),
		AfterJSON:  jsonValue(after),
	})
}

func scopedConfigMergedDocument(current *configscope.ScopedConfigDocument, payload configscope.ScopedConfigDocument, tenantID, workspaceID string) *configscope.ScopedConfigDocument {
	merged := &configscope.ScopedConfigDocument{
		TenantID:    tenantID,
		WorkspaceID: workspaceID,
	}
	if current != nil {
		*merged = *current
		merged.TenantID = tenantID
		merged.WorkspaceID = workspaceID
	}
	if payload.Portal != nil {
		merged.Portal = payload.Portal
	}
	if payload.OpenAI != nil {
		merged.OpenAI = payload.OpenAI
	}
	if payload.WeKnora != nil {
		merged.WeKnora = payload.WeKnora
	}
	if payload.SessionRisk != nil {
		merged.SessionRisk = payload.SessionRisk
	}
	return merged
}

func scopedConfigChangeRiskReason(check scopedConfigVerificationCheckDefinition) string {
	switch check.ID {
	case "rollback_baseline_restored":
		return "rollback_operation"
	case "portal_public_surface_verified":
		return "portal_public_surface_change"
	case "portal_locale_contract_verified":
		return "portal_locale_change"
	case "openai_provider_endpoint_verified":
		return "openai_provider_endpoint_change"
	case "openai_model_contract_verified":
		return "openai_model_contract_change"
	case "weknora_provider_endpoint_verified":
		return "weknora_provider_endpoint_change"
	case "weknora_knowledge_mapping_verified":
		return "weknora_knowledge_mapping_change"
	case "weknora_health_check_verified":
		return "weknora_health_policy_change"
	case "session_risk_score_threshold_verified":
		return "session_risk_threshold_change"
	case "session_risk_window_verified":
		return "session_risk_window_change"
	case "session_risk_concurrency_threshold_verified":
		return "session_risk_concurrency_change"
	default:
		if strings.TrimSpace(check.Section) != "" {
			return strings.TrimSpace(check.Section) + "_config_change"
		}
		return ""
	}
}

func normalizeScopedConfigRiskReasons(reasons []string) []string {
	if len(reasons) == 0 {
		return nil
	}
	result := make([]string, 0, len(reasons))
	seen := make(map[string]struct{}, len(reasons))
	for _, reason := range reasons {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			continue
		}
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		result = append(result, reason)
	}
	if len(result) == 0 {
		return nil
	}
	sort.Strings(result)
	return result
}

func scopedConfigChangeRiskForTemplate(operation string, template scopedConfigVerificationTemplate) scopedConfigChangeRisk {
	risk := scopedConfigChangeRisk{
		RiskLevel:    "low",
		ChangedPaths: append([]string(nil), template.ChangedPaths...),
	}
	if len(template.ChangedPaths) > 0 || len(template.Checks) > 0 {
		risk.RiskLevel = "medium"
		risk.RiskReasons = append(risk.RiskReasons, "scoped_config_change")
	}
	if operation == "rollback" {
		risk.RiskLevel = "high"
		risk.RiskReasons = append(risk.RiskReasons, "rollback_operation")
	}
	// 进入本循环意味着 len(template.Checks) > 0，上方已把 RiskLevel 提升到
	// "medium"（rollback 则为 "high"），因此循环内不可能回落到 "low"，
	// 也无需再处理 check.RiskLevel == "medium" 的情况。
	for _, check := range template.Checks {
		if reason := scopedConfigChangeRiskReason(check); reason != "" {
			risk.RiskReasons = append(risk.RiskReasons, reason)
		}
		if strings.TrimSpace(check.RiskLevel) == "high" {
			risk.RiskLevel = "high"
		}
	}
	risk.RiskReasons = normalizeScopedConfigRiskReasons(risk.RiskReasons)
	risk.ApprovalRequired = risk.RiskLevel == "high"
	return risk
}

func scopedConfigApprovalPolicy(changeRisk scopedConfigChangeRisk, changeControl scopedConfigChangeControl, approval *scopedConfigApprovalRecord) gin.H {
	approvalRefPresent := strings.TrimSpace(changeControl.ApprovalRef) != ""
	status := "not_required"
	if changeRisk.ApprovalRequired {
		status = "missing"
		if approvalRefPresent {
			status = "provided"
			if approval != nil {
				status = "approved"
			}
		}
	}
	policy := gin.H{
		"required":                changeRisk.ApprovalRequired,
		"approval_ref_required":   changeRisk.ApprovalRequired,
		"approval_ref_present":    approvalRefPresent,
		"approval_recorded":       approval != nil,
		"approval_status":         status,
		"same_actor_allowed":      false,
		"risk_level":              changeRisk.RiskLevel,
		"risk_reasons":            changeRisk.RiskReasons,
		"changed_paths":           changeRisk.ChangedPaths,
		"latest_approver_user_id": nil,
	}
	if approval != nil {
		policy["latest_approval"] = approval.response()
		policy["latest_approval_audit_id"] = approval.AuditID
		policy["latest_approval_at"] = approval.CreatedAt
		if approval.ActorUserID != nil {
			policy["latest_approver_user_id"] = *approval.ActorUserID
		}
	}
	return policy
}

func scopedConfigGovernanceStatus(operation string, changeRisk scopedConfigChangeRisk, changeControl scopedConfigChangeControl, approval *scopedConfigApprovalRecord, verifications []scopedConfigVerificationRecord) string {
	approvalReady := !changeRisk.ApprovalRequired || approval != nil
	approvalRefPresent := strings.TrimSpace(changeControl.ApprovalRef) != ""
	verificationRequired := scopedConfigVerificationRequired(operation)
	verificationStatus := scopedConfigVerificationStatus(operation, verifications)
	if changeRisk.ApprovalRequired && (!approvalRefPresent || !approvalReady) {
		return "awaiting_approval"
	}
	if verificationRequired {
		switch verificationStatus {
		case "pending":
			return "awaiting_verification"
		case "passed":
			return "verified"
		case "failed":
			return "verification_failed"
		default:
			return "awaiting_verification"
		}
	}
	if changeRisk.ApprovalRequired && approvalRefPresent {
		return "approved"
	}
	return "not_required"
}

func scopedConfigGovernanceBlockingRequirements(status string) []string {
	switch strings.TrimSpace(status) {
	case "awaiting_approval":
		return []string{"approval_ref"}
	case "awaiting_verification":
		return []string{"distinct_reviewer_verification"}
	case "verification_failed":
		return []string{"successful_verification_or_rollback"}
	default:
		return nil
	}
}

func scopedConfigGovernancePhase(status string) string {
	switch strings.TrimSpace(status) {
	case "awaiting_approval":
		return "approval"
	case "awaiting_verification", "verification_failed":
		return "verification"
	case "approved", "verified", "not_required":
		return "completed"
	default:
		return "verification"
	}
}

func scopedConfigGovernanceComplete(status string) bool {
	switch strings.TrimSpace(status) {
	case "approved", "verified", "not_required":
		return true
	default:
		return false
	}
}

func scopedConfigGovernancePolicy(operation string, changeRisk scopedConfigChangeRisk, changeControl scopedConfigChangeControl, approval *scopedConfigApprovalRecord, verifications []scopedConfigVerificationRecord) gin.H {
	status := scopedConfigGovernanceStatus(operation, changeRisk, changeControl, approval, verifications)
	approvalPolicy := scopedConfigApprovalPolicy(changeRisk, changeControl, approval)
	verificationStatus := scopedConfigVerificationStatus(operation, verifications)
	return gin.H{
		"status":                status,
		"phase":                 scopedConfigGovernancePhase(status),
		"workflow_complete":     scopedConfigGovernanceComplete(status),
		"approval_required":     changeRisk.ApprovalRequired,
		"approval_status":       approvalPolicy["approval_status"],
		"verification_required": scopedConfigVerificationRequired(operation),
		"verification_status":   verificationStatus,
		"blocking_requirements": scopedConfigGovernanceBlockingRequirements(status),
		"risk_level":            changeRisk.RiskLevel,
		"risk_reasons":          changeRisk.RiskReasons,
		"changed_paths":         changeRisk.ChangedPaths,
	}
}

func validateScopedConfigApproval(changeControl scopedConfigChangeControl, changeRisk scopedConfigChangeRisk, approval *scopedConfigApprovalRecord, operatorUserID *uint) *ErrorResponse {
	if !changeRisk.ApprovalRequired {
		return nil
	}
	if strings.TrimSpace(changeControl.ApprovalRef) == "" {
		return &ErrorResponse{
			Code:    http.StatusBadRequest,
			Error:   "Approval reference required",
			Message: "approval_ref is required via JSON body or X-Approval-Ref header for high-risk scoped config changes",
		}
	}
	if approval == nil {
		return &ErrorResponse{
			Code:    http.StatusBadRequest,
			Error:   "Approved change required",
			Message: "approval_ref must reference a recorded scoped config approval with the same change_ref before high-risk execution",
		}
	}
	if operatorUserID != nil && approval.ActorUserID != nil && *operatorUserID == *approval.ActorUserID {
		return &ErrorResponse{
			Code:    http.StatusForbidden,
			Error:   "Approval reviewer separation required",
			Message: "approval must be recorded by a different user than the scoped config operator",
		}
	}
	return nil
}

func requiredScopedConfigVerificationCheckIDs(template scopedConfigVerificationTemplate) []string {
	if len(template.Checks) == 0 {
		return nil
	}
	result := make([]string, 0, len(template.Checks))
	for _, definition := range template.Checks {
		if definition.Required {
			result = append(result, definition.ID)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func scopedConfigVerificationStatus(operation string, verifications []scopedConfigVerificationRecord) string {
	if !scopedConfigVerificationRequired(operation) {
		return "not_applicable"
	}
	if len(verifications) == 0 {
		return "pending"
	}
	return verifications[0].Status
}

func scopedConfigVerificationPolicy(operation string, sourceActorUserID *uint, template scopedConfigVerificationTemplate, verifications []scopedConfigVerificationRecord) gin.H {
	status := scopedConfigVerificationStatus(operation, verifications)
	checksRequired := len(template.Checks) > 0
	requiredCheckIDs := requiredScopedConfigVerificationCheckIDs(template)
	policy := gin.H{
		"required":                               scopedConfigVerificationRequired(operation),
		"same_actor_allowed":                     false,
		"reviewer_user_required":                 true,
		"evidence_required_for_pass":             true,
		"notes_required_for_fail":                true,
		"checks_required":                        checksRequired,
		"template_check_count":                   len(template.Checks),
		"required_check_ids":                     requiredCheckIDs,
		"allowed_check_statuses":                 []string{"passed", "failed", "skipped"},
		"all_required_checks_must_pass_for_pass": checksRequired,
		"failed_check_required_for_fail":         checksRequired,
		"verification_status":                    status,
		"verification_count":                     len(verifications),
		"latest_reviewer_user_id":                nil,
		"source_actor_user_id":                   nil,
		"dual_control_enforceable":               sourceActorUserID != nil,
		"verification_pending_reason":            nil,
	}
	if sourceActorUserID != nil {
		policy["source_actor_user_id"] = *sourceActorUserID
	}
	if status == "pending" {
		if sourceActorUserID == nil {
			policy["verification_pending_reason"] = "source_actor_unknown"
		} else {
			policy["verification_pending_reason"] = "awaiting_distinct_reviewer_verification"
		}
	}
	if len(verifications) > 0 && verifications[0].ActorUserID != nil {
		policy["latest_reviewer_user_id"] = *verifications[0].ActorUserID
	}
	return policy
}

func latestScopedConfigVerificationResponse(verifications []scopedConfigVerificationRecord) gin.H {
	if len(verifications) == 0 {
		return nil
	}
	return verifications[0].response()
}

func scopedConfigVerificationResponses(verifications []scopedConfigVerificationRecord) []gin.H {
	if len(verifications) == 0 {
		return nil
	}
	result := make([]gin.H, 0, len(verifications))
	for _, verification := range verifications {
		result = append(result, verification.response())
	}
	return result
}

func latestScopedConfigApprovalResponse(approval *scopedConfigApprovalRecord) gin.H {
	if approval == nil {
		return nil
	}
	return approval.response()
}

func latestScopedConfigApprovalFromIndex(index map[string][]scopedConfigApprovalRecord, changeControl scopedConfigChangeControl) *scopedConfigApprovalRecord {
	key := scopedConfigApprovalKey(changeControl)
	if key == "" || len(index[key]) == 0 {
		return nil
	}
	approval := index[key][0]
	return &approval
}

func extractScopedConfigApproval(log models.AuditLog) (scopedConfigApprovalRecord, bool) {
	if scopedConfigOperation(log.Action) != "approve" {
		return scopedConfigApprovalRecord{}, false
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(log.RequestJSON)), &payload); err != nil {
		return scopedConfigApprovalRecord{}, false
	}
	changeControl := extractScopedConfigChangeControl(log.RequestJSON)
	if strings.TrimSpace(changeControl.ChangeRef) == "" || strings.TrimSpace(changeControl.ApprovalRef) == "" {
		return scopedConfigApprovalRecord{}, false
	}
	return scopedConfigApprovalRecord{
		AuditID:       log.ID,
		Notes:         payloadStringValue(payload, "notes"),
		Evidence:      stringSliceFromAny(payload["evidence"]),
		CreatedAt:     log.CreatedAt,
		ActorUserID:   log.ActorUserID,
		ChangeControl: changeControl,
	}, true
}

func extractScopedConfigVerification(log models.AuditLog) (scopedConfigVerificationRecord, bool) {
	if scopedConfigOperation(log.Action) != "verify" {
		return scopedConfigVerificationRecord{}, false
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(log.RequestJSON)), &payload); err != nil {
		return scopedConfigVerificationRecord{}, false
	}
	verification := scopedConfigVerificationRecord{
		AuditID:       log.ID,
		SourceAuditID: uintValueFromAny(payload["source_audit_id"]),
		Status:        normalizeScopedConfigVerificationStatus(payloadStringValue(payload, "status")),
		Notes:         payloadStringValue(payload, "notes"),
		Evidence:      stringSliceFromAny(payload["evidence"]),
		Checks:        scopedConfigVerificationChecksFromAny(payload["checks"]),
		CreatedAt:     log.CreatedAt,
		ActorUserID:   log.ActorUserID,
		ChangeControl: extractScopedConfigChangeControl(log.RequestJSON),
	}
	if verification.SourceAuditID == 0 || verification.Status == "" {
		return scopedConfigVerificationRecord{}, false
	}
	return verification, true
}

func payloadStringValue(payload map[string]interface{}, key string) string {
	if len(payload) == 0 {
		return ""
	}
	return stringValueFromAny(payload[key])
}

func sortAuditLogsDesc(items []models.AuditLog) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
}

func sortScopedConfigVerificationsDesc(items []scopedConfigVerificationRecord) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].AuditID > items[j].AuditID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
}

func (h *ScopedConfigHandler) listScopedConfigAuditLogs(ctx context.Context, query auditplatform.ListQuery) ([]models.AuditLog, error) {
	if h == nil || h.audit == nil {
		return nil, nil
	}
	query.Page = 1
	query.PageSize = 200
	var result []models.AuditLog
	for {
		items, total, err := h.audit.List(ctx, query)
		if err != nil {
			return nil, err
		}
		result = append(result, items...)
		if int64(len(result)) >= total || len(items) == 0 {
			break
		}
		query.Page++
	}
	return result, nil
}

func (h *ScopedConfigHandler) listScopedConfigHistoryEntries(ctx context.Context, scope, tenantID, workspaceID string) ([]models.AuditLog, error) {
	resourceID := scopedConfigResourceID(tenantID, workspaceID)
	actions := []string{
		fmt.Sprintf("scoped_config.%s.update", scope),
		fmt.Sprintf("scoped_config.%s.rollback", scope),
	}
	result := make([]models.AuditLog, 0, 8)
	for _, action := range actions {
		items, err := h.listScopedConfigAuditLogs(ctx, auditplatform.ListQuery{
			Action:       action,
			ResourceType: "scoped_config",
			ResourceID:   resourceID,
			TenantID:     tenantID,
			WorkspaceID:  workspaceID,
		})
		if err != nil {
			return nil, err
		}
		result = append(result, items...)
	}
	sortAuditLogsDesc(result)
	return result, nil
}

func (h *ScopedConfigHandler) listScopedConfigVerificationEntries(ctx context.Context, scope, tenantID, workspaceID string) ([]models.AuditLog, error) {
	return h.listScopedConfigAuditLogs(ctx, auditplatform.ListQuery{
		Action:       fmt.Sprintf("scoped_config.%s.verify", scope),
		ResourceType: "scoped_config",
		ResourceID:   scopedConfigResourceID(tenantID, workspaceID),
		TenantID:     tenantID,
		WorkspaceID:  workspaceID,
	})
}

func (h *ScopedConfigHandler) listScopedConfigApprovalEntries(ctx context.Context, scope, tenantID, workspaceID string) ([]models.AuditLog, error) {
	return h.listScopedConfigAuditLogs(ctx, auditplatform.ListQuery{
		Action:       fmt.Sprintf("scoped_config.%s.approve", scope),
		ResourceType: "scoped_config",
		ResourceID:   scopedConfigResourceID(tenantID, workspaceID),
		TenantID:     tenantID,
		WorkspaceID:  workspaceID,
	})
}

func (h *ScopedConfigHandler) listScopedConfigVerificationIndex(ctx context.Context, scope, tenantID, workspaceID string) (map[uint][]scopedConfigVerificationRecord, error) {
	logs, err := h.listScopedConfigVerificationEntries(ctx, scope, tenantID, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make(map[uint][]scopedConfigVerificationRecord)
	for _, log := range logs {
		verification, ok := extractScopedConfigVerification(log)
		if !ok {
			continue
		}
		result[verification.SourceAuditID] = append(result[verification.SourceAuditID], verification)
	}
	for sourceAuditID := range result {
		sortScopedConfigVerificationsDesc(result[sourceAuditID])
	}
	return result, nil
}

func (h *ScopedConfigHandler) listScopedConfigVerificationHistory(ctx context.Context, scope, tenantID, workspaceID string, sourceAuditID uint) ([]scopedConfigVerificationRecord, error) {
	index, err := h.listScopedConfigVerificationIndex(ctx, scope, tenantID, workspaceID)
	if err != nil {
		return nil, err
	}
	return index[sourceAuditID], nil
}

func scopedConfigApprovalKey(changeControl scopedConfigChangeControl) string {
	changeRef := strings.TrimSpace(changeControl.ChangeRef)
	approvalRef := strings.TrimSpace(changeControl.ApprovalRef)
	if changeRef == "" || approvalRef == "" {
		return ""
	}
	return approvalRef + "|" + changeRef
}

func sortScopedConfigApprovalsDesc(items []scopedConfigApprovalRecord) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].AuditID > items[j].AuditID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
}

func (h *ScopedConfigHandler) listScopedConfigApprovalIndex(ctx context.Context, scope, tenantID, workspaceID string) (map[string][]scopedConfigApprovalRecord, error) {
	logs, err := h.listScopedConfigApprovalEntries(ctx, scope, tenantID, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]scopedConfigApprovalRecord)
	for _, log := range logs {
		approval, ok := extractScopedConfigApproval(log)
		if !ok {
			continue
		}
		// extractScopedConfigApproval 已拒绝 ChangeRef/ApprovalRef 为空的记录，
		// scopedConfigApprovalKey 在此恒非空，无需空 key 守卫。
		key := scopedConfigApprovalKey(approval.ChangeControl)
		result[key] = append(result[key], approval)
	}
	for key := range result {
		sortScopedConfigApprovalsDesc(result[key])
	}
	return result, nil
}

func (h *ScopedConfigHandler) latestScopedConfigApproval(ctx context.Context, scope, tenantID, workspaceID string, changeControl scopedConfigChangeControl) (*scopedConfigApprovalRecord, error) {
	index, err := h.listScopedConfigApprovalIndex(ctx, scope, tenantID, workspaceID)
	if err != nil {
		return nil, err
	}
	return latestScopedConfigApprovalFromIndex(index, changeControl), nil
}

func RegisterScopedConfigRoutes(r *gin.RouterGroup, handler *ScopedConfigHandler) {
	if r == nil || handler == nil {
		return
	}
	security := r.Group("/security/config")
	{
		security.GET("/tenant", handler.GetTenantConfig)
		security.PUT("/tenant", handler.PutTenantConfig)
		security.GET("/tenant/history", handler.GetTenantConfigHistory)
		security.GET("/tenant/history/:audit_id", handler.GetTenantConfigHistoryEntry)
		security.POST("/tenant/approve", handler.ApproveTenantConfig)
		security.POST("/tenant/rollback/:audit_id", handler.RollbackTenantConfig)
		security.POST("/tenant/verify/:audit_id", handler.VerifyTenantConfig)
		security.GET("/workspace", handler.GetWorkspaceConfig)
		security.PUT("/workspace", handler.PutWorkspaceConfig)
		security.GET("/workspace/history", handler.GetWorkspaceConfigHistory)
		security.GET("/workspace/history/:audit_id", handler.GetWorkspaceConfigHistoryEntry)
		security.POST("/workspace/approve", handler.ApproveWorkspaceConfig)
		security.POST("/workspace/rollback/:audit_id", handler.RollbackWorkspaceConfig)
		security.POST("/workspace/verify/:audit_id", handler.VerifyWorkspaceConfig)
	}
}

func (h *ScopedConfigHandler) loadHistoryEntry(c *gin.Context, scope, tenantID, workspaceID string) (*models.AuditLog, *configscope.ScopedConfigDocument, *ErrorResponse, error) {
	if h == nil || h.audit == nil {
		return nil, nil, &ErrorResponse{Code: http.StatusNotFound, Error: "Audit service unavailable", Message: "audit query service not configured"}, nil
	}
	auditID, err := strconv.ParseUint(strings.TrimSpace(c.Param("audit_id")), 10, 32)
	if err != nil || auditID == 0 {
		return nil, nil, &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid audit_id", Message: "audit_id must be a positive integer"}, nil
	}
	resourceID := scopedConfigResourceID(tenantID, workspaceID)
	entry, err := h.audit.Get(c.Request.Context(), uint(auditID), auditplatform.QueryScope{TenantID: tenantID, WorkspaceID: workspaceID})
	if err != nil {
		return nil, nil, nil, err
	}
	if entry == nil || entry.ResourceType != "scoped_config" || entry.ResourceID != resourceID {
		return nil, nil, &ErrorResponse{Code: http.StatusNotFound, Error: "Config history entry not found", Message: "no matching scoped config audit entry found"}, nil
	}
	operation := scopedConfigOperation(entry.Action)
	if operation != "update" && operation != "rollback" {
		return nil, nil, &ErrorResponse{Code: http.StatusBadRequest, Error: "Unsupported config history entry", Message: "selected audit entry is not a config snapshot event"}, nil
	}
	if strings.TrimSpace(entry.AfterJSON) == "" {
		return nil, nil, &ErrorResponse{Code: http.StatusBadRequest, Error: "Rollback snapshot unavailable", Message: "selected audit entry does not contain an after snapshot"}, nil
	}
	var snapshot configscope.ScopedConfigDocument
	if err := json.Unmarshal([]byte(entry.AfterJSON), &snapshot); err != nil {
		return nil, nil, &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid rollback snapshot", Message: err.Error()}, nil
	}
	if snapshot.TenantID != "" && snapshot.TenantID != tenantID {
		return nil, nil, &ErrorResponse{Code: http.StatusBadRequest, Error: "Rollback scope mismatch", Message: "snapshot tenant scope does not match request scope"}, nil
	}
	if scope == "workspace" && snapshot.WorkspaceID != "" && snapshot.WorkspaceID != workspaceID {
		return nil, nil, &ErrorResponse{Code: http.StatusBadRequest, Error: "Rollback scope mismatch", Message: "snapshot workspace scope does not match request scope"}, nil
	}
	return entry, &snapshot, nil, nil
}

func (h *ScopedConfigHandler) currentConfigForScope(ctx context.Context, scope, tenantID, workspaceID string) (*configscope.ScopedConfigDocument, error) {
	if h == nil || h.store == nil {
		return nil, nil
	}
	if scope == "tenant" {
		if doc, ok, err := h.store.GetTenantConfig(ctx, tenantID); err != nil {
			return nil, err
		} else if ok {
			return doc, nil
		}
		return &configscope.ScopedConfigDocument{TenantID: tenantID}, nil
	}
	if doc, ok, err := h.store.GetWorkspaceConfig(ctx, tenantID, workspaceID); err != nil {
		return nil, err
	} else if ok {
		return doc, nil
	}
	return &configscope.ScopedConfigDocument{TenantID: tenantID, WorkspaceID: workspaceID}, nil
}

func buildScopedConfigDiff(current, snapshot *configscope.ScopedConfigDocument) gin.H {
	if current == nil {
		current = &configscope.ScopedConfigDocument{}
	}
	if snapshot == nil {
		snapshot = &configscope.ScopedConfigDocument{}
	}
	portalChanges := diffJSONChanges(current.Portal, snapshot.Portal, "portal")
	openAIChanges := diffJSONChanges(current.OpenAI, snapshot.OpenAI, "openai")
	weKnoraChanges := diffJSONChanges(current.WeKnora, snapshot.WeKnora, "weknora")
	sessionRiskChanges := diffJSONChanges(current.SessionRisk, snapshot.SessionRisk, "session_risk")
	changes := append(append([]gin.H{}, portalChanges...), openAIChanges...)
	changes = append(changes, weKnoraChanges...)
	changes = append(changes, sessionRiskChanges...)
	changedPaths := changePaths(changes)
	return gin.H{
		"portal_changed":       len(portalChanges) > 0,
		"openai_changed":       len(openAIChanges) > 0,
		"weknora_changed":      len(weKnoraChanges) > 0,
		"session_risk_changed": len(sessionRiskChanges) > 0,
		"scope_changed":        current.TenantID != snapshot.TenantID || current.WorkspaceID != snapshot.WorkspaceID,
		"current_sections":     presentSections(current),
		"snapshot_sections":    presentSections(snapshot),
		"changed_paths":        changedPaths,
		"portal_paths":         changePaths(portalChanges),
		"openai_paths":         changePaths(openAIChanges),
		"weknora_paths":        changePaths(weKnoraChanges),
		"session_risk_paths":   changePaths(sessionRiskChanges),
		"changes":              changes,
		"portal_changes":       portalChanges,
		"openai_changes":       openAIChanges,
		"weknora_changes":      weKnoraChanges,
		"session_risk_changes": sessionRiskChanges,
	}
}

func presentSections(doc *configscope.ScopedConfigDocument) []string {
	if doc == nil {
		return nil
	}
	sections := make([]string, 0, 4)
	if doc.Portal != nil {
		sections = append(sections, "portal")
	}
	if doc.OpenAI != nil {
		sections = append(sections, "openai")
	}
	if doc.WeKnora != nil {
		sections = append(sections, "weknora")
	}
	if doc.SessionRisk != nil {
		sections = append(sections, "session_risk")
	}
	return sections
}

func jsonValue(value interface{}) string {
	if value == nil {
		return ""
	}
	body, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(body)
}

func diffJSONChanges(current, snapshot interface{}, prefix string) []gin.H {
	var currentValue interface{}
	var snapshotValue interface{}
	if body, err := json.Marshal(current); err == nil && len(body) > 0 && string(body) != "null" {
		_ = json.Unmarshal(body, &currentValue)
	}
	if body, err := json.Marshal(snapshot); err == nil && len(body) > 0 && string(body) != "null" {
		_ = json.Unmarshal(body, &snapshotValue)
	}
	currentValue = normalizeDiffJSONValue(currentValue)
	snapshotValue = normalizeDiffJSONValue(snapshotValue)
	changes := collectDiffChanges(currentValue, snapshotValue, prefix)
	sort.Slice(changes, func(i, j int) bool {
		return changes[i]["path"].(string) < changes[j]["path"].(string)
	})
	return changes
}

func normalizeDiffJSONValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		if len(typed) == 0 {
			return nil
		}
		normalized := make(map[string]interface{}, len(typed))
		for key, child := range typed {
			normalizedChild := normalizeDiffJSONValue(child)
			if normalizedChild == nil {
				continue
			}
			normalized[key] = normalizedChild
		}
		if len(normalized) == 0 {
			return nil
		}
		return normalized
	case []interface{}:
		if len(typed) == 0 {
			return typed
		}
		normalized := make([]interface{}, 0, len(typed))
		for _, child := range typed {
			normalized = append(normalized, normalizeDiffJSONValue(child))
		}
		return normalized
	default:
		return value
	}
}

func collectDiffChanges(current, snapshot interface{}, prefix string) []gin.H {
	if valuesEqual(current, snapshot) {
		return nil
	}
	currentMap, currentIsMap := current.(map[string]interface{})
	snapshotMap, snapshotIsMap := snapshot.(map[string]interface{})
	if currentIsMap || snapshotIsMap {
		keys := make(map[string]struct{}, len(currentMap)+len(snapshotMap))
		for key := range currentMap {
			keys[key] = struct{}{}
		}
		for key := range snapshotMap {
			keys[key] = struct{}{}
		}
		ordered := make([]string, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		var changes []gin.H
		for _, key := range ordered {
			nextPrefix := key
			if prefix != "" {
				nextPrefix = prefix + "." + key
			}
			changes = append(changes, collectDiffChanges(currentMap[key], snapshotMap[key], nextPrefix)...)
		}
		if len(changes) == 0 && prefix != "" {
			return []gin.H{{"path": prefix, "type": diffChangeType(current, snapshot), "current": current, "snapshot": snapshot}}
		}
		return changes
	}
	return []gin.H{{"path": prefix, "type": diffChangeType(current, snapshot), "current": current, "snapshot": snapshot}}
}

func diffChangeType(current, snapshot interface{}) string {
	if current == nil && snapshot != nil {
		return "added"
	}
	if current != nil && snapshot == nil {
		return "removed"
	}
	return "updated"
}

func valuesEqual(a, b interface{}) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func changePaths(changes []gin.H) []string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		path, _ := change["path"].(string)
		if path != "" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}
