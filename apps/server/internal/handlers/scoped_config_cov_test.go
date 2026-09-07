package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"servify/apps/server/internal/models"
	auditplatform "servify/apps/server/internal/platform/audit"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/configscope"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ---- scc stubs and helpers ----

type sccErrReader struct{}

func (sccErrReader) Read([]byte) (int, error) { return 0, errors.New("scc read failed") }

type sccAuditStub struct {
	listErr       error
	errActions    map[string]error
	logsByAction  map[string][]models.AuditLog
	pagesByAction map[string][][]models.AuditLog
	getLog        *models.AuditLog
	getErr        error
}

func (s *sccAuditStub) List(_ context.Context, query auditplatform.ListQuery) ([]models.AuditLog, int64, error) {
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	if err, ok := s.errActions[query.Action]; ok {
		return nil, 0, err
	}
	if pages, ok := s.pagesByAction[query.Action]; ok {
		total := int64(0)
		for _, page := range pages {
			total += int64(len(page))
		}
		idx := query.Page - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(pages) {
			return nil, total, nil
		}
		return pages[idx], total, nil
	}
	if logs, ok := s.logsByAction[query.Action]; ok {
		return logs, int64(len(logs)), nil
	}
	return nil, 0, nil
}

func (s *sccAuditStub) Get(_ context.Context, _ uint, _ auditplatform.QueryScope) (*models.AuditLog, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.getLog, nil
}

func sccEmptyAudit() *sccAuditStub { return &sccAuditStub{} }

func sccRouter(store *configscope.GormConfigStore, audit auditplatform.QueryService, tenantID, workspaceID string, userID any) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if userID != nil {
			c.Set("user_id", userID)
		}
		c.Request = c.Request.WithContext(platformauth.ContextWithScope(context.Background(), tenantID, workspaceID))
		c.Next()
	})
	RegisterScopedConfigRoutes(&r.RouterGroup, NewScopedConfigHandler(store, audit))
	return r
}

func sccDo(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	r.ServeHTTP(w, req)
	return w
}

func sccAssert(t *testing.T, w *httptest.ResponseRecorder, code int, contains ...string) {
	t.Helper()
	if w.Code != code {
		t.Fatalf("expected %d got %d body=%s", code, w.Code, w.Body.String())
	}
	for _, fragment := range contains {
		if !strings.Contains(w.Body.String(), fragment) {
			t.Fatalf("expected body to contain %q body=%s", fragment, w.Body.String())
		}
	}
}

func sccBreakWrites(db *gorm.DB) {
	fail := func(tx *gorm.DB) { tx.Error = errors.New("scc forced write failure") }
	_ = db.Callback().Create().Before("gorm:create").Register("scc_fail_create", fail)
	_ = db.Callback().Update().Before("gorm:update").Register("scc_fail_update", fail)
}

func sccTenantUpdateLog() *models.AuditLog {
	return &models.AuditLog{
		ID:           5,
		ActorUserID:  uintPtr(9),
		Action:       "scoped_config.tenant.update",
		ResourceType: "scoped_config",
		ResourceID:   "tenant-a",
		TenantID:     "tenant-a",
		RequestJSON:  `{"change_ref":"CHG-5101","reason":"tenant branding refresh","approval_ref":"APR-5101"}`,
		AfterJSON:    `{"tenant_id":"tenant-a","portal":{"brand_name":"Stub Brand"}}`,
	}
}

func sccTenantApprovalLog() *models.AuditLog {
	return &models.AuditLog{
		ID:           6,
		ActorUserID:  uintPtr(12),
		Action:       "scoped_config.tenant.approve",
		ResourceType: "scoped_config",
		ResourceID:   "tenant-a",
		TenantID:     "tenant-a",
		RequestJSON:  `{"change_ref":"CHG-5101","reason":"tenant branding refresh","approval_ref":"APR-5101","notes":"approved","evidence":["cab"]}`,
	}
}

func sccTenantVerifyLog() *models.AuditLog {
	return &models.AuditLog{
		ID:           7,
		ActorUserID:  uintPtr(12),
		Action:       "scoped_config.tenant.verify",
		ResourceType: "scoped_config",
		ResourceID:   "tenant-a",
		TenantID:     "tenant-a",
		RequestJSON:  `{"source_audit_id":5,"status":"passed","notes":"smoke ok","evidence":["runbook"],"checks":[{"id":"runtime_effect_confirmed","status":"passed"}],"change_ref":"CHG-5102","reason":"verify"}`,
	}
}

func sccWorkspaceUpdateLog() *models.AuditLog {
	return &models.AuditLog{
		ID:           8,
		ActorUserID:  uintPtr(9),
		Action:       "scoped_config.workspace.update",
		ResourceType: "scoped_config",
		ResourceID:   "tenant-a/workspace-1",
		TenantID:     "tenant-a",
		WorkspaceID:  "workspace-1",
		RequestJSON:  `{"change_ref":"CHG-5201","reason":"workspace refresh","approval_ref":"APR-5201"}`,
		AfterJSON:    `{"tenant_id":"tenant-a","workspace_id":"workspace-1","portal":{"brand_name":"Ws Brand"}}`,
	}
}

// ---- GetTenantConfig / GetWorkspaceConfig variants ----

func TestSccGetTenantConfigVariants(t *testing.T) {
	t.Run("no scope", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant", "")
		sccAssert(t, w, http.StatusBadRequest, "Tenant scope required")
	})
	t.Run("store nil", func(t *testing.T) {
		r := sccRouter(nil, sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant", "")
		sccAssert(t, w, http.StatusNotFound, "Config store unavailable")
	})
	t.Run("store error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		if err := db.Migrator().DropTable(&models.TenantConfig{}); err != nil {
			t.Fatalf("drop table: %v", err)
		}
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant", "")
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load tenant config")
	})
	t.Run("missing config", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant", "")
		sccAssert(t, w, http.StatusOK, `"tenant_id":"tenant-a"`)
	})
}

func TestSccGetWorkspaceConfigVariants(t *testing.T) {
	t.Run("no workspace scope", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/workspace", "")
		sccAssert(t, w, http.StatusBadRequest, "Workspace scope required")
	})
	t.Run("store nil", func(t *testing.T) {
		r := sccRouter(nil, sccEmptyAudit(), "tenant-a", "workspace-1", nil)
		w := sccDo(r, http.MethodGet, "/security/config/workspace", "")
		sccAssert(t, w, http.StatusNotFound, "Config store unavailable")
	})
	t.Run("store error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		if err := db.Migrator().DropTable(&models.WorkspaceConfig{}); err != nil {
			t.Fatalf("drop table: %v", err)
		}
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "workspace-1", nil)
		w := sccDo(r, http.MethodGet, "/security/config/workspace", "")
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load workspace config")
	})
	t.Run("missing config", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "workspace-1", nil)
		w := sccDo(r, http.MethodGet, "/security/config/workspace", "")
		sccAssert(t, w, http.StatusOK, `"workspace_id":"workspace-1"`)
	})
}

// ---- Put variants ----

func TestSccPutTenantConfigPaths(t *testing.T) {
	body := `{"change_ref":"CHG-6101","reason":"tenant branding","portal":{"brand_name":"New Brand"}}`
	t.Run("no scope", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "", "", nil)
		w := sccDo(r, http.MethodPut, "/security/config/tenant", body)
		sccAssert(t, w, http.StatusBadRequest, "Tenant scope required")
	})
	t.Run("store nil", func(t *testing.T) {
		r := sccRouter(nil, sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPut, "/security/config/tenant", body)
		sccAssert(t, w, http.StatusNotFound, "Config store unavailable")
	})
	t.Run("malformed json", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPut, "/security/config/tenant", `{"portal":`)
		sccAssert(t, w, http.StatusBadRequest, "Invalid request")
	})
	t.Run("load current error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		if err := db.Migrator().DropTable(&models.TenantConfig{}); err != nil {
			t.Fatalf("drop table: %v", err)
		}
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPut, "/security/config/tenant", body)
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load tenant config")
	})
	t.Run("approval load error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.errActions = map[string]error{"scoped_config.tenant.approve": errors.New("scc approval list failed")}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodPut, "/security/config/tenant", body)
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load scoped config approval")
	})
	t.Run("upsert error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		sccBreakWrites(db)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPut, "/security/config/tenant", body)
		sccAssert(t, w, http.StatusBadRequest, "Failed to save tenant config")
	})
	t.Run("high risk without approval", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		highRiskBody := `{"change_ref":"CHG-6102","reason":"rotate weknora","weknora":{"enabled":true,"knowledge_base_id":"kb-tenant"}}`
		w := sccDo(r, http.MethodPut, "/security/config/tenant", highRiskBody)
		sccAssert(t, w, http.StatusBadRequest, "Approval reference required")
	})
	t.Run("success with existing config", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		store := configscope.NewGormConfigStore(db)
		if _, err := store.UpsertTenantConfig(context.Background(), "tenant-a", configscope.ScopedConfigDocument{Portal: sccPortal("Old Brand")}); err != nil {
			t.Fatalf("seed tenant config: %v", err)
		}
		r := sccRouter(store, sccEmptyAudit(), "tenant-a", "", uint(9))
		w := sccDo(r, http.MethodPut, "/security/config/tenant", body)
		sccAssert(t, w, http.StatusOK, `"governance_status"`)
	})
	t.Run("success", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", uint(9))
		w := sccDo(r, http.MethodPut, "/security/config/tenant", body)
		sccAssert(t, w, http.StatusOK, `"governance_status"`)
	})
}

func TestSccPutWorkspaceConfigPaths(t *testing.T) {
	body := `{"change_ref":"CHG-6201","reason":"workspace branding","portal":{"brand_name":"Ws Brand"}}`
	t.Run("no workspace scope", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPut, "/security/config/workspace", body)
		sccAssert(t, w, http.StatusBadRequest, "Workspace scope required")
	})
	t.Run("store nil", func(t *testing.T) {
		r := sccRouter(nil, sccEmptyAudit(), "tenant-a", "workspace-1", nil)
		w := sccDo(r, http.MethodPut, "/security/config/workspace", body)
		sccAssert(t, w, http.StatusNotFound, "Config store unavailable")
	})
	t.Run("malformed json", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "workspace-1", nil)
		w := sccDo(r, http.MethodPut, "/security/config/workspace", `not-json`)
		sccAssert(t, w, http.StatusBadRequest, "Invalid request")
	})
	t.Run("load current error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		if err := db.Migrator().DropTable(&models.WorkspaceConfig{}); err != nil {
			t.Fatalf("drop table: %v", err)
		}
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "workspace-1", nil)
		w := sccDo(r, http.MethodPut, "/security/config/workspace", body)
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load workspace config")
	})
	t.Run("approval load error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.errActions = map[string]error{"scoped_config.workspace.approve": errors.New("scc approval list failed")}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "workspace-1", nil)
		w := sccDo(r, http.MethodPut, "/security/config/workspace", body)
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load scoped config approval")
	})
	t.Run("upsert error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		sccBreakWrites(db)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "workspace-1", nil)
		w := sccDo(r, http.MethodPut, "/security/config/workspace", body)
		sccAssert(t, w, http.StatusBadRequest, "Failed to save workspace config")
	})
}

// ---- tenant history list ----

func TestSccTenantHistoryList(t *testing.T) {
	t.Run("no scope", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history", "")
		sccAssert(t, w, http.StatusBadRequest, "Tenant scope required")
	})
	t.Run("audit nil", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), nil, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history", "")
		sccAssert(t, w, http.StatusNotFound, "Audit service unavailable")
	})
	t.Run("history list error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.errActions = map[string]error{"scoped_config.tenant.update": errors.New("scc update list failed")}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history", "")
		sccAssert(t, w, http.StatusInternalServerError, "Failed to list config history")
	})
	t.Run("verification index error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.errActions = map[string]error{"scoped_config.tenant.verify": errors.New("scc verify list failed")}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history", "")
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load config verification history")
	})
	t.Run("approval index error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.errActions = map[string]error{"scoped_config.tenant.approve": errors.New("scc approve list failed")}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history", "")
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load config approval history")
	})
	t.Run("empty history", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history", "")
		sccAssert(t, w, http.StatusOK, `"total":0`)
	})
	t.Run("paged history with action filter", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		first := *sccTenantUpdateLog()
		second := *sccTenantUpdateLog()
		second.ID = 9
		second.Action = "scoped_config.tenant.rollback"
		stub := sccEmptyAudit()
		stub.pagesByAction = map[string][][]models.AuditLog{
			"scoped_config.tenant.update":   {{first}},
			"scoped_config.tenant.rollback": {{second}},
		}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history?action=update", "")
		sccAssert(t, w, http.StatusOK, `"total":1`)
	})
}

// ---- tenant history entry ----

func TestSccTenantHistoryEntry(t *testing.T) {
	t.Run("no scope", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history/5", "")
		sccAssert(t, w, http.StatusBadRequest, "Tenant scope required")
	})
	t.Run("no workspace scope", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/workspace/history/8", "")
		sccAssert(t, w, http.StatusBadRequest, "Workspace scope required")
	})
	t.Run("store nil", func(t *testing.T) {
		r := sccRouter(nil, sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history/5", "")
		sccAssert(t, w, http.StatusNotFound, "Config store unavailable")
	})
	t.Run("invalid audit id", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history/not-a-number", "")
		sccAssert(t, w, http.StatusBadRequest, "Invalid audit_id")
	})
	t.Run("audit get error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getErr = errors.New("scc get failed")
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history/5", "")
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load config history entry")
	})
	t.Run("entry not found", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		entry := sccTenantUpdateLog()
		entry.ResourceID = "tenant-other"
		stub.getLog = entry
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history/5", "")
		sccAssert(t, w, http.StatusNotFound, "Config history entry not found")
	})
	t.Run("unsupported operation", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		entry := sccTenantApprovalLog()
		entry.ResourceID = "tenant-a"
		stub.getLog = entry
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history/6", "")
		sccAssert(t, w, http.StatusBadRequest, "Unsupported config history entry")
	})
	t.Run("missing snapshot", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		entry := sccTenantUpdateLog()
		entry.AfterJSON = ""
		stub.getLog = entry
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history/5", "")
		sccAssert(t, w, http.StatusBadRequest, "Rollback snapshot unavailable")
	})
	t.Run("invalid snapshot", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		entry := sccTenantUpdateLog()
		entry.AfterJSON = `{"tenant_id":`
		stub.getLog = entry
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history/5", "")
		sccAssert(t, w, http.StatusBadRequest, "Invalid rollback snapshot")
	})
	t.Run("tenant scope mismatch", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		entry := sccTenantUpdateLog()
		entry.AfterJSON = `{"tenant_id":"tenant-other","portal":{"brand_name":"X"}}`
		stub.getLog = entry
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history/5", "")
		sccAssert(t, w, http.StatusBadRequest, "Rollback scope mismatch")
	})
	t.Run("workspace scope mismatch", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		entry := sccWorkspaceUpdateLog()
		entry.AfterJSON = `{"tenant_id":"tenant-a","workspace_id":"workspace-other"}`
		stub.getLog = entry
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "workspace-1", nil)
		w := sccDo(r, http.MethodGet, "/security/config/workspace/history/8", "")
		sccAssert(t, w, http.StatusBadRequest, "Rollback scope mismatch")
	})
	t.Run("current config error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		if err := db.Migrator().DropTable(&models.TenantConfig{}); err != nil {
			t.Fatalf("drop table: %v", err)
		}
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history/5", "")
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load current scoped config")
	})
	t.Run("verification history error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		stub.errActions = map[string]error{"scoped_config.tenant.verify": errors.New("scc verify list failed")}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history/5", "")
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load config verification history")
	})
	t.Run("approval history error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		stub.errActions = map[string]error{"scoped_config.tenant.approve": errors.New("scc approve list failed")}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history/5", "")
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load config approval history")
	})
	t.Run("success with malformed index logs", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		stub.logsByAction = map[string][]models.AuditLog{
			"scoped_config.tenant.verify": {
				{ID: 30, Action: "scoped_config.tenant.verify", RequestJSON: `not-json`},
			},
			"scoped_config.tenant.approve": {
				{ID: 31, Action: "scoped_config.tenant.approve", RequestJSON: `not-json`},
			},
		}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodGet, "/security/config/tenant/history/5", "")
		sccAssert(t, w, http.StatusOK, `"verification_path":"/security/config/tenant/verify/5"`)
	})
}

// ---- tenant rollback ----

func TestSccTenantRollback(t *testing.T) {
	rollbackBody := `{"confirm":true,"change_ref":"CHG-5101","reason":"rollback tenant branding","approval_ref":"APR-5101"}`
	t.Run("success", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		stub.logsByAction = map[string][]models.AuditLog{
			"scoped_config.tenant.approve": {*sccTenantApprovalLog()},
		}
		store := configscope.NewGormConfigStore(db)
		if _, err := store.UpsertTenantConfig(context.Background(), "tenant-a", configscope.ScopedConfigDocument{Portal: sccPortal("Current Brand")}); err != nil {
			t.Fatalf("seed current config: %v", err)
		}
		r := sccRouter(store, stub, "tenant-a", "", uint(9))
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusOK, `"source_audit_id":5`)
		doc, ok, err := store.GetTenantConfig(context.Background(), "tenant-a")
		if err != nil || !ok {
			t.Fatalf("load rolled back config ok=%v err=%v", ok, err)
		}
		if doc.Portal == nil || doc.Portal.BrandName != "Stub Brand" {
			t.Fatalf("unexpected rolled back doc %+v", doc)
		}
	})
	t.Run("no scope", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusBadRequest, "Tenant scope required")
	})
	t.Run("store nil", func(t *testing.T) {
		r := sccRouter(nil, sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusNotFound, "Config store unavailable")
	})
	t.Run("audit nil", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), nil, "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusNotFound, "Audit service unavailable")
	})
	t.Run("malformed json", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5?confirm=true", `{"confirm":`)
		sccAssert(t, w, http.StatusBadRequest, "Invalid request")
	})
	t.Run("confirmation required", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5", `{"change_ref":"CHG-5101","reason":"rollback"}`)
		sccAssert(t, w, http.StatusBadRequest, "Rollback confirmation required")
	})
	t.Run("change control required", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5?confirm=true", `{"confirm":true}`)
		sccAssert(t, w, http.StatusBadRequest, "Change control required")
	})
	t.Run("invalid audit id", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/bogus?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusBadRequest, "Invalid audit_id")
	})
	t.Run("audit get error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getErr = errors.New("scc get failed")
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load config history entry")
	})
	t.Run("entry not found", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusNotFound, "Config history entry not found")
	})
	t.Run("tenant config load error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		if err := db.Migrator().DropTable(&models.TenantConfig{}); err != nil {
			t.Fatalf("drop table: %v", err)
		}
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load tenant config")
	})
	t.Run("approval load error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		stub.errActions = map[string]error{"scoped_config.tenant.approve": errors.New("scc approve list failed")}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load scoped config approval")
	})
	t.Run("approval reference required", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5?confirm=true", `{"confirm":true,"change_ref":"CHG-5101","reason":"rollback"}`)
		sccAssert(t, w, http.StatusBadRequest, "Approval reference required")
	})
	t.Run("approved change required", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusBadRequest, "Approved change required")
	})
	t.Run("upsert error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		sccBreakWrites(db)
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		stub.logsByAction = map[string][]models.AuditLog{
			"scoped_config.tenant.approve": {*sccTenantApprovalLog()},
		}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", uint(9))
		w := sccDo(r, http.MethodPost, "/security/config/tenant/rollback/5?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusBadRequest, "Failed to rollback tenant config")
	})
}

func TestSccWorkspaceRollbackErrorPaths(t *testing.T) {
	rollbackBody := `{"confirm":true,"change_ref":"CHG-5201","reason":"rollback workspace branding","approval_ref":"APR-5201"}`
	t.Run("workspace config load error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		if err := db.Migrator().DropTable(&models.WorkspaceConfig{}); err != nil {
			t.Fatalf("drop table: %v", err)
		}
		stub := sccEmptyAudit()
		stub.getLog = sccWorkspaceUpdateLog()
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "workspace-1", nil)
		w := sccDo(r, http.MethodPost, "/security/config/workspace/rollback/8?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load workspace config")
	})
	t.Run("approval load error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getLog = sccWorkspaceUpdateLog()
		stub.errActions = map[string]error{"scoped_config.workspace.approve": errors.New("scc approve list failed")}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "workspace-1", nil)
		w := sccDo(r, http.MethodPost, "/security/config/workspace/rollback/8?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load scoped config approval")
	})
	t.Run("upsert error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		sccBreakWrites(db)
		stub := sccEmptyAudit()
		stub.getLog = sccWorkspaceUpdateLog()
		stub.logsByAction = map[string][]models.AuditLog{
			"scoped_config.workspace.approve": {
				{
					ID:           9,
					ActorUserID:  uintPtr(12),
					Action:       "scoped_config.workspace.approve",
					ResourceType: "scoped_config",
					ResourceID:   "tenant-a/workspace-1",
					TenantID:     "tenant-a",
					WorkspaceID:  "workspace-1",
					RequestJSON:  `{"change_ref":"CHG-5201","reason":"workspace refresh","approval_ref":"APR-5201","notes":"approved","evidence":["cab"]}`,
				},
			},
		}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "workspace-1", uint(9))
		w := sccDo(r, http.MethodPost, "/security/config/workspace/rollback/8?confirm=true", rollbackBody)
		sccAssert(t, w, http.StatusBadRequest, "Failed to rollback workspace config")
	})
}

// ---- tenant verify ----

func TestSccTenantVerify(t *testing.T) {
	verifyBody := `{"status":"passed","notes":"smoke ok","evidence":["runbook"],"checks":[{"id":"change_scope_reviewed","status":"passed"},{"id":"runtime_effect_confirmed","status":"passed"},{"id":"portal_render_verified","status":"passed"},{"id":"portal_public_surface_verified","status":"passed"}],"change_ref":"CHG-5301","reason":"verify"}`
	t.Run("success with existing verification", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		stub.logsByAction = map[string][]models.AuditLog{
			"scoped_config.tenant.verify":  {*sccTenantVerifyLog()},
			"scoped_config.tenant.approve": {},
		}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", uint(12))
		w := sccDo(r, http.MethodPost, "/security/config/tenant/verify/5", verifyBody)
		sccAssert(t, w, http.StatusOK, `"verification_status":"passed"`)
	})
	t.Run("no scope", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/verify/5", verifyBody)
		sccAssert(t, w, http.StatusBadRequest, "Tenant scope required")
	})
	t.Run("audit nil", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), nil, "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/verify/5", verifyBody)
		sccAssert(t, w, http.StatusNotFound, "Audit service unavailable")
	})
	t.Run("malformed json", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/verify/5", `{"status":`)
		sccAssert(t, w, http.StatusBadRequest, "Invalid request")
	})
	t.Run("invalid status", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/verify/5", `{"status":"unknown","notes":"n","change_ref":"CHG-5301","reason":"verify"}`)
		sccAssert(t, w, http.StatusBadRequest, "Invalid verification status")
	})
	t.Run("change control required", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/verify/5", `{"status":"failed","notes":"n"}`)
		sccAssert(t, w, http.StatusBadRequest, "Change control required")
	})
	t.Run("invalid audit id", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/verify/bogus", verifyBody)
		sccAssert(t, w, http.StatusBadRequest, "Invalid audit_id")
	})
	t.Run("audit get error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getErr = errors.New("scc get failed")
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/verify/5", verifyBody)
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load config history entry")
	})
	t.Run("reviewer required", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/verify/5", verifyBody)
		sccAssert(t, w, http.StatusForbidden, "Verification reviewer required")
	})
	t.Run("verification history error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		stub.errActions = map[string]error{"scoped_config.tenant.verify": errors.New("scc verify list failed")}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", uint(12))
		w := sccDo(r, http.MethodPost, "/security/config/tenant/verify/5", verifyBody)
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load config verification history")
	})
	t.Run("approval history error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.getLog = sccTenantUpdateLog()
		stub.errActions = map[string]error{"scoped_config.tenant.approve": errors.New("scc approve list failed")}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", uint(12))
		w := sccDo(r, http.MethodPost, "/security/config/tenant/verify/5", verifyBody)
		sccAssert(t, w, http.StatusInternalServerError, "Failed to load config approval history")
	})
}

// ---- tenant approve ----

func TestSccApproveTenant(t *testing.T) {
	approveBody := `{"change_ref":"CHG-9201","reason":"approve tenant change","approval_ref":"APR-9201","notes":"approved","evidence":["cab"]}`
	t.Run("success", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", uint(12))
		w := sccDo(r, http.MethodPost, "/security/config/tenant/approve", approveBody)
		sccAssert(t, w, http.StatusOK, `"approval_status":"approved"`)
	})
	t.Run("success with prior approval", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		stub := sccEmptyAudit()
		stub.logsByAction = map[string][]models.AuditLog{
			"scoped_config.tenant.approve": {
				{
					ID:           11,
					ActorUserID:  uintPtr(13),
					Action:       "scoped_config.tenant.approve",
					ResourceType: "scoped_config",
					ResourceID:   "tenant-a",
					TenantID:     "tenant-a",
					RequestJSON:  `{"change_ref":"CHG-9201","reason":"approve tenant change","approval_ref":"APR-9201","notes":"prior","evidence":["e1"]}`,
				},
			},
		}
		r := sccRouter(configscope.NewGormConfigStore(db), stub, "tenant-a", "", uint(12))
		w := sccDo(r, http.MethodPost, "/security/config/tenant/approve", approveBody)
		sccAssert(t, w, http.StatusOK, `"approval_status":"approved"`)
	})
	t.Run("no scope", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/approve", approveBody)
		sccAssert(t, w, http.StatusBadRequest, "Tenant scope required")
	})
	t.Run("audit nil", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), nil, "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/approve", approveBody)
		sccAssert(t, w, http.StatusNotFound, "Audit service unavailable")
	})
	t.Run("malformed json", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/approve", `{"notes":`)
		sccAssert(t, w, http.StatusBadRequest, "Invalid request")
	})
	t.Run("notes required", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/approve", `{"change_ref":"CHG-9201","reason":"r","approval_ref":"APR-9201"}`)
		sccAssert(t, w, http.StatusBadRequest, "Approval notes required")
	})
	t.Run("change control required", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/approve", `{"notes":"approved"}`)
		sccAssert(t, w, http.StatusBadRequest, "Change control required")
	})
	t.Run("approval reference required", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/approve", `{"change_ref":"CHG-9201","reason":"r","notes":"approved"}`)
		sccAssert(t, w, http.StatusBadRequest, "Approval reference required")
	})
	t.Run("reviewer required", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		r := sccRouter(configscope.NewGormConfigStore(db), sccEmptyAudit(), "tenant-a", "", nil)
		w := sccDo(r, http.MethodPost, "/security/config/tenant/approve", approveBody)
		sccAssert(t, w, http.StatusForbidden, "Approval reviewer required")
	})
}

// ---- direct low-level handler calls ----

func sccGinContext(method, target string, body io.Reader) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, body)
	return c, w
}

func TestSccLoadHistoryEntryDirect(t *testing.T) {
	t.Run("nil audit", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		h := NewScopedConfigHandler(configscope.NewGormConfigStore(db))
		c, _ := sccGinContext(http.MethodGet, "/security/config/tenant/history/5", nil)
		c.Params = gin.Params{{Key: "audit_id", Value: "5"}}
		_, _, response, err := h.loadHistoryEntry(c, "tenant", "tenant-a", "")
		if response == nil || response.Code != http.StatusNotFound {
			t.Fatalf("expected 404 response got %+v err=%v", response, err)
		}
	})
	t.Run("nil handler", func(t *testing.T) {
		var h *ScopedConfigHandler
		c, _ := sccGinContext(http.MethodGet, "/security/config/tenant/history/5", nil)
		c.Params = gin.Params{{Key: "audit_id", Value: "5"}}
		_, _, response, err := h.loadHistoryEntry(c, "tenant", "tenant-a", "")
		if response == nil || response.Code != http.StatusNotFound {
			t.Fatalf("expected 404 response got %+v err=%v", response, err)
		}
	})
	t.Run("non numeric audit id", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		h := NewScopedConfigHandler(configscope.NewGormConfigStore(db), sccEmptyAudit())
		c, _ := sccGinContext(http.MethodGet, "/security/config/tenant/history/bogus", nil)
		c.Params = gin.Params{{Key: "audit_id", Value: "bogus"}}
		_, _, response, err := h.loadHistoryEntry(c, "tenant", "tenant-a", "")
		if response == nil || response.Code != http.StatusBadRequest || err != nil {
			t.Fatalf("expected 400 response got %+v err=%v", response, err)
		}
	})
	t.Run("zero audit id", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		h := NewScopedConfigHandler(configscope.NewGormConfigStore(db), sccEmptyAudit())
		c, _ := sccGinContext(http.MethodGet, "/security/config/tenant/history/0", nil)
		c.Params = gin.Params{{Key: "audit_id", Value: "0"}}
		_, _, response, err := h.loadHistoryEntry(c, "tenant", "tenant-a", "")
		if response == nil || response.Code != http.StatusBadRequest || err != nil {
			t.Fatalf("expected 400 response got %+v err=%v", response, err)
		}
	})
}

func TestSccParseRollbackRequestDirect(t *testing.T) {
	t.Run("nil context", func(t *testing.T) {
		req, errResponse := parseScopedConfigRollbackRequest(nil)
		if errResponse != nil || req.Confirm {
			t.Fatalf("unexpected result req=%+v errResponse=%v", req, errResponse)
		}
	})
	t.Run("nil body", func(t *testing.T) {
		c, _ := sccGinContext(http.MethodPost, "/rollback", nil)
		c.Request.Body = nil
		req, errResponse := parseScopedConfigRollbackRequest(c)
		if errResponse != nil || req.Confirm {
			t.Fatalf("unexpected result req=%+v errResponse=%v", req, errResponse)
		}
	})
	t.Run("read error", func(t *testing.T) {
		c, _ := sccGinContext(http.MethodPost, "/rollback", io.NopCloser(sccErrReader{}))
		_, errResponse := parseScopedConfigRollbackRequest(c)
		if errResponse == nil || errResponse.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %+v", errResponse)
		}
	})
	t.Run("unmarshal error", func(t *testing.T) {
		c, _ := sccGinContext(http.MethodPost, "/rollback", strings.NewReader("not-json"))
		_, errResponse := parseScopedConfigRollbackRequest(c)
		if errResponse == nil || errResponse.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %+v", errResponse)
		}
	})
}

func TestSccParseNilContextErrors(t *testing.T) {
	if _, _, errResponse := parseScopedConfigApprovalRequest(nil); errResponse == nil || errResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected approval nil-context 400 got %+v", errResponse)
	}
	if _, _, errResponse := parseScopedConfigVerificationRequest(nil); errResponse == nil || errResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected verification nil-context 400 got %+v", errResponse)
	}
}

func TestSccRegisterRoutesNil(t *testing.T) {
	RegisterScopedConfigRoutes(nil, nil)
	r := gin.New()
	RegisterScopedConfigRoutes(&r.RouterGroup, nil)
}

func TestSccCurrentConfigForScopeBranches(t *testing.T) {
	ctx := context.Background()
	t.Run("nil handler", func(t *testing.T) {
		var h *ScopedConfigHandler
		doc, err := h.currentConfigForScope(ctx, "tenant", "tenant-a", "")
		if doc != nil || err != nil {
			t.Fatalf("expected nil doc and err got %+v %v", doc, err)
		}
	})
	t.Run("nil store", func(t *testing.T) {
		h := NewScopedConfigHandler(nil)
		doc, err := h.currentConfigForScope(ctx, "workspace", "tenant-a", "workspace-1")
		if doc != nil || err != nil {
			t.Fatalf("expected nil doc and err got %+v %v", doc, err)
		}
	})
	t.Run("tenant found", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		store := configscope.NewGormConfigStore(db)
		if _, err := store.UpsertTenantConfig(ctx, "tenant-a", configscope.ScopedConfigDocument{Portal: sccPortal("Found")}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		h := NewScopedConfigHandler(store)
		doc, err := h.currentConfigForScope(ctx, "tenant", "tenant-a", "")
		if err != nil || doc == nil || doc.Portal == nil || doc.Portal.BrandName != "Found" {
			t.Fatalf("unexpected doc=%+v err=%v", doc, err)
		}
	})
	t.Run("tenant missing", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		h := NewScopedConfigHandler(configscope.NewGormConfigStore(db))
		doc, err := h.currentConfigForScope(ctx, "tenant", "tenant-a", "")
		if err != nil || doc == nil || doc.TenantID != "tenant-a" {
			t.Fatalf("unexpected doc=%+v err=%v", doc, err)
		}
	})
	t.Run("tenant error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		if err := db.Migrator().DropTable(&models.TenantConfig{}); err != nil {
			t.Fatalf("drop table: %v", err)
		}
		h := NewScopedConfigHandler(configscope.NewGormConfigStore(db))
		doc, err := h.currentConfigForScope(ctx, "tenant", "tenant-a", "")
		if doc != nil || err == nil {
			t.Fatalf("expected error got doc=%+v err=%v", doc, err)
		}
	})
	t.Run("workspace found", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		store := configscope.NewGormConfigStore(db)
		if _, err := store.UpsertWorkspaceConfig(ctx, "tenant-a", "workspace-1", configscope.ScopedConfigDocument{Portal: sccPortal("Ws")}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		h := NewScopedConfigHandler(store)
		doc, err := h.currentConfigForScope(ctx, "workspace", "tenant-a", "workspace-1")
		if err != nil || doc == nil || doc.Portal == nil {
			t.Fatalf("unexpected doc=%+v err=%v", doc, err)
		}
	})
	t.Run("workspace missing", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		h := NewScopedConfigHandler(configscope.NewGormConfigStore(db))
		doc, err := h.currentConfigForScope(ctx, "workspace", "tenant-a", "workspace-1")
		if err != nil || doc == nil || doc.WorkspaceID != "workspace-1" {
			t.Fatalf("unexpected doc=%+v err=%v", doc, err)
		}
	})
	t.Run("workspace error", func(t *testing.T) {
		db := newScopedConfigTestDB(t)
		if err := db.Migrator().DropTable(&models.WorkspaceConfig{}); err != nil {
			t.Fatalf("drop table: %v", err)
		}
		h := NewScopedConfigHandler(configscope.NewGormConfigStore(db))
		doc, err := h.currentConfigForScope(ctx, "workspace", "tenant-a", "workspace-1")
		if doc != nil || err == nil {
			t.Fatalf("expected error got doc=%+v err=%v", doc, err)
		}
	})
}
