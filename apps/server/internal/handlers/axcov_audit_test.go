package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/gin-gonic/gin"
)

func axcAuditRouter(h *AuditHandler) *gin.Engine {
	r := gin.New()
	r.GET("/audit/logs", h.List)
	r.GET("/audit/logs/export", h.ExportCSV)
	r.GET("/audit/logs/:id", h.Get)
	r.GET("/audit/logs/:id/diff", h.GetDiff)
	return r
}

func axcAuditDo(r *gin.Engine, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAxcAuditList(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("nil handler returns not found", func(t *testing.T) {
		var h *AuditHandler
		if w := axcAuditDo(axcAuditRouter(h), "/audit/logs"); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("nil service returns not found", func(t *testing.T) {
		if w := axcAuditDo(axcAuditRouter(NewAuditHandler(nil)), "/audit/logs"); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("query errors return bad request", func(t *testing.T) {
		r := axcAuditRouter(NewAuditHandler(&stubAuditQueryService{}))
		for _, q := range []string{
			"?actor_user_id=abc",
			"?success=maybe",
			"?from=not-a-time",
			"?to=not-a-time",
		} {
			if w := axcAuditDo(r, "/audit/logs"+q); w.Code != http.StatusBadRequest {
				t.Fatalf("query %q: status=%d body=%s", q, w.Code, w.Body.String())
			}
		}
	})
	t.Run("list error returns 500", func(t *testing.T) {
		svc := &stubAuditQueryService{err: errors.New("axc audit failure")}
		r := axcAuditRouter(NewAuditHandler(svc))
		if w := axcAuditDo(r, "/audit/logs"); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("valid filters build query", func(t *testing.T) {
		from := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		to := from.Add(24 * time.Hour)
		svc := &stubAuditQueryService{items: []models.AuditLog{{ID: 1, Action: "a.create"}}, total: 1}
		r := axcAuditRouter(NewAuditHandler(svc))
		w := axcAuditDo(r, "/audit/logs?action=a.create&resource_type=tickets&resource_id=9&principal_kind=agent&actor_user_id=7&success=false&from="+from.Format(time.RFC3339)+"&to="+to.Format(time.RFC3339)+"&page=3&page_size=7")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		q := svc.query
		if q.Action != "a.create" || q.ResourceType != "tickets" || q.ResourceID != "9" || q.PrincipalKind != "agent" {
			t.Fatalf("unexpected query: %+v", q)
		}
		if q.ActorUserID == nil || *q.ActorUserID != 7 || q.Success == nil || *q.Success {
			t.Fatalf("unexpected filters: %+v", q)
		}
		if q.From == nil || !q.From.Equal(from) || q.To == nil || !q.To.Equal(to) {
			t.Fatalf("unexpected time filters: %+v", q)
		}
		if q.Page != 3 || q.PageSize != 7 {
			t.Fatalf("unexpected paging: %+v", q)
		}
	})
	t.Run("invalid paging falls back", func(t *testing.T) {
		svc := &stubAuditQueryService{}
		r := axcAuditRouter(NewAuditHandler(svc))
		if w := axcAuditDo(r, "/audit/logs?page=bad&page_size=-3"); w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if svc.query.Page != 1 || svc.query.PageSize != 20 {
			t.Fatalf("unexpected paging fallback: %+v", svc.query)
		}
	})
}

func TestAxcAuditExportCSV(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("nil handler returns not found", func(t *testing.T) {
		var h *AuditHandler
		if w := axcAuditDo(axcAuditRouter(h), "/audit/logs/export"); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("nil service returns not found", func(t *testing.T) {
		if w := axcAuditDo(axcAuditRouter(NewAuditHandler(nil)), "/audit/logs/export"); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("invalid filters rejected", func(t *testing.T) {
		r := axcAuditRouter(NewAuditHandler(&stubAuditQueryService{}))
		if w := axcAuditDo(r, "/audit/logs/export?success=maybe"); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("list error returns 500", func(t *testing.T) {
		svc := &stubAuditQueryService{err: errors.New("axc audit failure")}
		r := axcAuditRouter(NewAuditHandler(svc))
		if w := axcAuditDo(r, "/audit/logs/export"); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("clamps limit and exports rows", func(t *testing.T) {
		actor := uint(55)
		svc := &stubAuditQueryService{
			items: []models.AuditLog{
				{
					ID: 11, Action: "t.update", ResourceType: "tickets", ResourceID: "7", PrincipalKind: "admin",
					ActorUserID: &actor, Success: true, StatusCode: 200, Route: "/tickets", Method: "POST",
					RequestID: "req-1", TenantID: "tenant-a", WorkspaceID: "ws-1", ClientIP: "203.0.113.1",
					UserAgent: "ua", RequestJSON: `{"a":1}`, BeforeJSON: `{"a":0}`, AfterJSON: `{"a":1}`,
					CreatedAt: time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC),
				},
				{ID: 12, Action: "t.delete", CreatedAt: time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)},
			},
			total: 2,
		}
		r := axcAuditRouter(NewAuditHandler(svc))
		w := axcAuditDo(r, "/audit/logs/export?limit=99999")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if svc.query.Page != 1 || svc.query.PageSize != 5000 {
			t.Fatalf("unexpected paging: %+v", svc.query)
		}
		body := w.Body.String()
		for _, want := range []string{"t.update", "t.delete", "55", "true", "200", "req-1", "tenant-a", "ws-1", "203.0.113.1", `""a"":1`} {
			if !strings.Contains(body, want) {
				t.Fatalf("csv missing %q: %s", want, body)
			}
		}
		if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment; filename=audit_logs_") {
			t.Fatalf("unexpected content disposition: %q", cd)
		}
		w = axcAuditDo(r, "/audit/logs/export?limit=bad")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestAxcAuditGet(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("nil handler returns not found", func(t *testing.T) {
		var h *AuditHandler
		if w := axcAuditDo(axcAuditRouter(h), "/audit/logs/1"); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d", w.Code)
		}
	})
	t.Run("nil service returns not found", func(t *testing.T) {
		if w := axcAuditDo(axcAuditRouter(NewAuditHandler(nil)), "/audit/logs/1"); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d", w.Code)
		}
	})
	t.Run("rejects invalid ids", func(t *testing.T) {
		r := axcAuditRouter(NewAuditHandler(&stubAuditQueryService{}))
		if w := axcAuditDo(r, "/audit/logs/xyz"); w.Code != http.StatusBadRequest {
			t.Fatalf("non numeric: status=%d", w.Code)
		}
		if w := axcAuditDo(r, "/audit/logs/0"); w.Code != http.StatusBadRequest {
			t.Fatalf("zero id: status=%d", w.Code)
		}
	})
	t.Run("service error returns 500", func(t *testing.T) {
		svc := &stubAuditQueryService{err: errors.New("axc audit failure")}
		r := axcAuditRouter(NewAuditHandler(svc))
		if w := axcAuditDo(r, "/audit/logs/5"); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("missing item returns 404", func(t *testing.T) {
		svc := &stubAuditQueryService{}
		r := axcAuditRouter(NewAuditHandler(svc))
		if w := axcAuditDo(r, "/audit/logs/5"); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("success returns item", func(t *testing.T) {
		svc := &stubAuditQueryService{items: []models.AuditLog{{ID: 21, Action: "t.create"}}}
		r := axcAuditRouter(NewAuditHandler(svc))
		w := axcAuditDo(r, "/audit/logs/21")
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"action":"t.create"`) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestAxcAuditGetDiff(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("nil handler returns not found", func(t *testing.T) {
		var h *AuditHandler
		if w := axcAuditDo(axcAuditRouter(h), "/audit/logs/1/diff"); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d", w.Code)
		}
	})
	t.Run("nil service returns not found", func(t *testing.T) {
		if w := axcAuditDo(axcAuditRouter(NewAuditHandler(nil)), "/audit/logs/1/diff"); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d", w.Code)
		}
	})
	t.Run("rejects invalid ids", func(t *testing.T) {
		r := axcAuditRouter(NewAuditHandler(&stubAuditQueryService{}))
		if w := axcAuditDo(r, "/audit/logs/abc/diff"); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d", w.Code)
		}
		if w := axcAuditDo(r, "/audit/logs/0/diff"); w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d", w.Code)
		}
	})
	t.Run("service error returns 500", func(t *testing.T) {
		svc := &stubAuditQueryService{err: errors.New("axc audit failure")}
		r := axcAuditRouter(NewAuditHandler(svc))
		if w := axcAuditDo(r, "/audit/logs/5/diff"); w.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d", w.Code)
		}
	})
	t.Run("missing item returns 404", func(t *testing.T) {
		svc := &stubAuditQueryService{}
		r := axcAuditRouter(NewAuditHandler(svc))
		if w := axcAuditDo(r, "/audit/logs/5/diff"); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d", w.Code)
		}
	})
	t.Run("invalid snapshot json returns bad request", func(t *testing.T) {
		svc := &stubAuditQueryService{items: []models.AuditLog{{ID: 31, BeforeJSON: "{bad", AfterJSON: `{"a":1}`}}}
		r := axcAuditRouter(NewAuditHandler(svc))
		if w := axcAuditDo(r, "/audit/logs/31/diff"); w.Code != http.StatusBadRequest {
			t.Fatalf("before json: status=%d body=%s", w.Code, w.Body.String())
		}
		svc2 := &stubAuditQueryService{items: []models.AuditLog{{ID: 32, BeforeJSON: `{"a":1}`, AfterJSON: "[bad"}}}
		r2 := axcAuditRouter(NewAuditHandler(svc2))
		if w := axcAuditDo(r2, "/audit/logs/32/diff"); w.Code != http.StatusBadRequest {
			t.Fatalf("after json: status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("success returns diff", func(t *testing.T) {
		svc := &stubAuditQueryService{items: []models.AuditLog{{
			ID:         33,
			BeforeJSON: `{"status":"open","nested":{"x":1}}`,
			AfterJSON:  `{"status":"closed","nested":{"x":2}}`,
		}}}
		r := axcAuditRouter(NewAuditHandler(svc))
		w := axcAuditDo(r, "/audit/logs/33/diff")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, `"changed":true`) || !strings.Contains(body, `"path":"status"`) || !strings.Contains(body, `"path":"nested.x"`) {
			t.Fatalf("unexpected body: %s", body)
		}
	})
}

func TestAxcAuditRegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	RegisterAuditRoutes(nil, NewAuditHandler(&stubAuditQueryService{}))

	r := gin.New()
	RegisterAuditRoutes(&r.RouterGroup, nil)
	if len(r.Routes()) != 0 {
		t.Fatalf("expected no routes for nil handler, got %d", len(r.Routes()))
	}

	r2 := gin.New()
	RegisterAuditRoutes(&r2.RouterGroup, NewAuditHandler(&stubAuditQueryService{}))
	if len(r2.Routes()) != 4 {
		t.Fatalf("expected 4 routes, got %d", len(r2.Routes()))
	}
}

func TestAxcAuditIntQuery(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	if got := intQuery(nil, "page", 42); got != 42 {
		t.Fatalf("nil context: %d", got)
	}
	if got := intQuery(c, "page", 9); got != 9 {
		t.Fatalf("missing param: %d", got)
	}
	for _, raw := range []string{"?page=abc", "?page=0", "?page=-4"} {
		c2, _ := gin.CreateTestContext(httptest.NewRecorder())
		c2.Request = httptest.NewRequest(http.MethodGet, "/x"+raw, nil)
		if got := intQuery(c2, "page", 9); got != 9 {
			t.Fatalf("query %q: %d", raw, got)
		}
	}
	c3, _ := gin.CreateTestContext(httptest.NewRecorder())
	c3.Request = httptest.NewRequest(http.MethodGet, "/x?page=13", nil)
	if got := intQuery(c3, "page", 9); got != 13 {
		t.Fatalf("valid param: %d", got)
	}
}

func TestAxcAuditDiffHelpers(t *testing.T) {
	t.Run("parseAuditJSON", func(t *testing.T) {
		if v, err := parseAuditJSON(""); v != nil || err != nil {
			t.Fatalf("empty: %v %v", v, err)
		}
		if v, err := parseAuditJSON("   "); v != nil || err != nil {
			t.Fatalf("blank: %v %v", v, err)
		}
		if _, err := parseAuditJSON("nope"); err == nil {
			t.Fatal("expected error")
		}
		v, err := parseAuditJSON(` {"a":1} `)
		if err != nil {
			t.Fatalf("valid: %v", err)
		}
		if _, ok := v.(map[string]interface{}); !ok {
			t.Fatalf("expected map, got %T", v)
		}
	})

	t.Run("toAuditJSON", func(t *testing.T) {
		if got := toAuditJSON(nil); got != "null" {
			t.Fatalf("nil: %q", got)
		}
		if got := toAuditJSON(map[string]int{"a": 1}); got != `{"a":1}` {
			t.Fatalf("map: %q", got)
		}
		if got := toAuditJSON(func() {}); got != "" {
			t.Fatalf("unsupported type should yield empty, got %q", got)
		}
	})

	t.Run("auditValuesEqual", func(t *testing.T) {
		if !auditValuesEqual(make(chan int), make(chan string)) {
			t.Fatal("two unsupported values serialize to empty and should be equal")
		}
		if !auditValuesEqual(1, 1) {
			t.Fatal("same values should be equal")
		}
		if auditValuesEqual("a", "b") {
			t.Fatal("different values should not be equal")
		}
	})

	t.Run("auditDiffType", func(t *testing.T) {
		if got := auditDiffType(nil, 1); got != "added" {
			t.Fatalf("added: %q", got)
		}
		if got := auditDiffType(1, nil); got != "removed" {
			t.Fatalf("removed: %q", got)
		}
		if got := auditDiffType(1, 2); got != "updated" {
			t.Fatalf("updated: %q", got)
		}
		if got := auditDiffType(nil, nil); got != "updated" {
			t.Fatalf("nil-nil default: %q", got)
		}
	})

	t.Run("collectAuditDiffPaths", func(t *testing.T) {
		changes := []gin.H{
			{"path": "a"},
			{"path": ""},
			{"other": 1},
			{"path": 42},
			{"path": "b"},
		}
		got := collectAuditDiffPaths(changes)
		if len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Fatalf("unexpected paths: %v", got)
		}
	})

	t.Run("diffAuditValues", func(t *testing.T) {
		if got := diffAuditValues(1, 1, "same"); got != nil {
			t.Fatalf("equal scalars: %+v", got)
		}
		changes := diffAuditValues(map[string]interface{}{"nested": map[string]interface{}{"x": 1}}, map[string]interface{}{"nested": map[string]interface{}{"x": 2}}, "")
		if len(changes) != 1 || changes[0]["path"] != "nested.x" || changes[0]["type"] != "updated" {
			t.Fatalf("nested change: %+v", changes)
		}
		changes = diffAuditValues(1, map[string]interface{}{}, "root")
		if len(changes) != 1 || changes[0]["type"] != "updated" {
			t.Fatalf("scalar vs map: %+v", changes)
		}
	})

	t.Run("buildAuditDiff", func(t *testing.T) {
		if _, err := buildAuditDiff("{bad", `{"a":1}`); err == nil {
			t.Fatal("expected before error")
		}
		if _, err := buildAuditDiff(`{"a":1}`, "{bad"); err == nil {
			t.Fatal("expected after error")
		}
		diff, err := buildAuditDiff(`{"a":1}`, `{"a":2}`)
		if err != nil {
			t.Fatalf("valid diff: %v", err)
		}
		if diff["changed"] != true || diff["has_before"] != true || diff["has_after"] != true {
			t.Fatalf("unexpected diff: %+v", diff)
		}
		empty, err := buildAuditDiff("", "")
		if err != nil {
			t.Fatalf("empty diff: %v", err)
		}
		if empty["changed"] != false || empty["has_before"] != false || empty["has_after"] != false {
			t.Fatalf("unexpected empty diff: %+v", empty)
		}
	})
}
