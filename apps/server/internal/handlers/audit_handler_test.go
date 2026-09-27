package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	auditplatform "servify/apps/server/internal/platform/audit"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var auditVerifyDBSeq atomic.Uint32

// uniqueAuditVerifyDSN 命名内存库 DSN 追加全局唯一序号，避免重复执行命中同一库。
func uniqueAuditVerifyDSN(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("file:audit_verify_%d?mode=memory&cache=shared", auditVerifyDBSeq.Add(1))
}

type stubAuditQueryService struct {
	items []models.AuditLog
	total int64
	err   error
	query auditplatform.ListQuery
	getID uint
	scope auditplatform.QueryScope
}

func (s *stubAuditQueryService) List(_ context.Context, query auditplatform.ListQuery) ([]models.AuditLog, int64, error) {
	s.query = query
	return s.items, s.total, s.err
}

func (s *stubAuditQueryService) Get(_ context.Context, id uint, scope auditplatform.QueryScope) (*models.AuditLog, error) {
	s.getID = id
	s.scope = scope
	for i := range s.items {
		if s.items[i].ID == id {
			return &s.items[i], s.err
		}
	}
	return nil, s.err
}

func TestAuditHandlerList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &stubAuditQueryService{
		items: []models.AuditLog{{ID: 1, Action: "tickets.create"}},
		total: 1,
	}
	r := gin.New()
	RegisterAuditRoutes(&r.RouterGroup, NewAuditHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/audit/logs?action=tickets.create&principal_kind=agent&success=true&from="+time.Now().UTC().Format(time.RFC3339), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if svc.query.Action != "tickets.create" || svc.query.PrincipalKind != "agent" {
		t.Fatalf("unexpected query: %+v", svc.query)
	}
	if svc.query.Success == nil || !*svc.query.Success {
		t.Fatalf("expected success filter true, got %+v", svc.query.Success)
	}
}

func TestAuditHandlerListProjectsScopeFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &stubAuditQueryService{
		items: []models.AuditLog{{ID: 1, Action: "tickets.create"}},
		total: 1,
	}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(platformauth.ContextWithScope(c.Request.Context(), "tenant-a", "workspace-1"))
		c.Next()
	})
	RegisterAuditRoutes(&r.RouterGroup, NewAuditHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/audit/logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if svc.query.TenantID != "tenant-a" || svc.query.WorkspaceID != "workspace-1" {
		t.Fatalf("unexpected scope query: %+v", svc.query)
	}
}

func TestAuditHandlerGet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &stubAuditQueryService{
		items: []models.AuditLog{{ID: 7, Action: "tickets.create", TenantID: "tenant-a", WorkspaceID: "workspace-1"}},
	}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(platformauth.ContextWithScope(c.Request.Context(), "tenant-a", "workspace-1"))
		c.Next()
	})
	RegisterAuditRoutes(&r.RouterGroup, NewAuditHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/audit/logs/7", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if svc.getID != 7 {
		t.Fatalf("unexpected get id: %d", svc.getID)
	}
	if svc.scope.TenantID != "tenant-a" || svc.scope.WorkspaceID != "workspace-1" {
		t.Fatalf("unexpected scope: %+v", svc.scope)
	}
}

func TestAuditHandlerGetRejectsInvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &stubAuditQueryService{}
	r := gin.New()
	RegisterAuditRoutes(&r.RouterGroup, NewAuditHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/audit/logs/not-a-number", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAuditHandlerGetDiff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &stubAuditQueryService{
		items: []models.AuditLog{{
			ID:          9,
			Action:      "tickets.update",
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-1",
			BeforeJSON:  `{"status":"open","agent_id":null,"title":"before"}`,
			AfterJSON:   `{"status":"resolved","agent_id":42,"title":"after"}`,
		}},
	}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(platformauth.ContextWithScope(c.Request.Context(), "tenant-a", "workspace-1"))
		c.Next()
	})
	RegisterAuditRoutes(&r.RouterGroup, NewAuditHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/audit/logs/9/diff", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, w.Body.String())
	}
	diff := body["diff"].(map[string]any)
	if diff["changed"] != true {
		t.Fatalf("expected changed=true body=%s", w.Body.String())
	}
	if int(diff["change_count"].(float64)) < 2 {
		t.Fatalf("expected at least 2 changes body=%s", w.Body.String())
	}
	paths := diff["changed_paths"].([]any)
	if len(paths) == 0 {
		t.Fatalf("expected changed paths body=%s", w.Body.String())
	}
	if svc.scope.TenantID != "tenant-a" || svc.scope.WorkspaceID != "workspace-1" {
		t.Fatalf("unexpected scope: %+v", svc.scope)
	}
}

func TestAuditHandlerGetDiffRequiresSnapshots(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &stubAuditQueryService{
		items: []models.AuditLog{{ID: 10, Action: "tickets.update"}},
	}
	r := gin.New()
	RegisterAuditRoutes(&r.RouterGroup, NewAuditHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/audit/logs/10/diff", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, w.Body.String())
	}
	diff := body["diff"].(map[string]any)
	if diff["changed"] != false || int(diff["change_count"].(float64)) != 0 {
		t.Fatalf("unexpected diff body=%s", w.Body.String())
	}
}

func TestAuditHandlerExportCSV(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &stubAuditQueryService{
		items: []models.AuditLog{{
			ID:            11,
			Action:        "tickets.update",
			ResourceType:  "tickets",
			ResourceID:    "1",
			PrincipalKind: "admin",
			TenantID:      "tenant-a",
			WorkspaceID:   "workspace-1",
			RequestJSON:   `{"status":"resolved"}`,
			BeforeJSON:    `{"status":"open"}`,
			AfterJSON:     `{"status":"resolved"}`,
			CreatedAt:     time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC),
		}},
		total: 1,
	}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(platformauth.ContextWithScope(c.Request.Context(), "tenant-a", "workspace-1"))
		c.Next()
	})
	RegisterAuditRoutes(&r.RouterGroup, NewAuditHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/audit/logs/export?action=tickets.update&limit=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if contentType := w.Header().Get("Content-Type"); !strings.Contains(contentType, "text/csv") {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	body := w.Body.String()
	if !strings.Contains(body, "tickets.update") || !strings.Contains(body, "status") {
		t.Fatalf("unexpected csv body=%s", body)
	}
	if svc.query.TenantID != "tenant-a" || svc.query.WorkspaceID != "workspace-1" {
		t.Fatalf("unexpected scope query: %+v", svc.query)
	}
	if svc.query.Page != 1 || svc.query.PageSize != 10 {
		t.Fatalf("unexpected paging query: %+v", svc.query)
	}
}

func TestAuditHandlerExportCSVRejectsInvalidFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &stubAuditQueryService{}
	r := gin.New()
	RegisterAuditRoutes(&r.RouterGroup, NewAuditHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/audit/logs/export?success=maybe", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAuditHandlerExportCSVClampsLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &stubAuditQueryService{}
	r := gin.New()
	RegisterAuditRoutes(&r.RouterGroup, NewAuditHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/audit/logs/export?limit=99999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if svc.query.Page != 1 || svc.query.PageSize != 5000 {
		t.Fatalf("unexpected paging query: %+v", svc.query)
	}
}

// stubChainVerifier 实现 ChainVerifier（Verify 端点专用桩）。
type stubChainVerifier struct {
	report *auditplatform.ChainReport
	err    error
	called bool
}

func (s *stubChainVerifier) VerifyChain(_ context.Context) (*auditplatform.ChainReport, error) {
	s.called = true
	return s.report, s.err
}

func TestAuditHandlerVerify(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("reports chain status", func(t *testing.T) {
		svc := &stubAuditQueryService{}
		verifier := &stubChainVerifier{report: &auditplatform.ChainReport{OK: true, Total: 3, Hashed: 3}}
		h := NewAuditHandler(svc)
		h.verifier = verifier
		r := gin.New()
		RegisterAuditRoutes(&r.RouterGroup, h)

		req := httptest.NewRequest(http.MethodGet, "/audit/verify", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK || !verifier.called {
			t.Fatalf("expected 200 + call, got %d called=%v", w.Code, verifier.called)
		}
		if !strings.Contains(w.Body.String(), `"ok":true`) || !strings.Contains(w.Body.String(), `"hashed":3`) {
			t.Fatalf("unexpected report body: %s", w.Body.String())
		}
	})

	t.Run("verifier error maps to 500", func(t *testing.T) {
		h := NewAuditHandler(&stubAuditQueryService{})
		h.verifier = &stubChainVerifier{err: errors.New("chain boom")}
		r := gin.New()
		RegisterAuditRoutes(&r.RouterGroup, h)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/audit/verify", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("no verifier configured maps to 404", func(t *testing.T) {
		r := gin.New()
		RegisterAuditRoutes(&r.RouterGroup, NewAuditHandler(&stubAuditQueryService{}))

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/audit/verify", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 got %d body=%s", w.Code, w.Body.String())
		}
	})
}

// TestAuditHandlerVerifyAgainstRealChain 端到端小回路：sqlite 真库写两条
// 带链哈希的记录，经 /audit/verify 校验通过并报告全链强度。
func TestAuditHandlerVerifyAgainstRealChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(uniqueAuditVerifyDSN(t)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	recorder := auditplatform.NewGormRecorder(db)
	for _, action := range []string{"s.one", "s.two"} {
		if err := recorder.Record(context.Background(), auditplatform.Entry{PrincipalKind: "agent", Action: action, Route: "/api/x", Method: "POST", Success: true}); err != nil {
			t.Fatalf("Record(%s) error = %v", action, err)
		}
	}

	r := gin.New()
	RegisterAuditRoutes(&r.RouterGroup, NewAuditHandler(auditplatform.NewGormQueryService(db)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/audit/verify", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	var report auditplatform.ChainReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if !report.OK || report.Hashed != 2 || report.Anchored || report.LegacyRows != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
}
