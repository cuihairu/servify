package audit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type stubRecorder struct {
	entries []Entry
}

func (s *stubRecorder) Record(_ context.Context, entry Entry) error {
	s.entries = append(s.entries, entry)
	return nil
}

func TestMiddlewareRecordsSuccessfulWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &stubRecorder{}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(7))
		c.Set("principal_kind", "agent")
		c.Next()
	})
	r.Use(Middleware(recorder))
	r.POST("/api/tickets/:id/assign", func(c *gin.Context) {
		SetBefore(c, gin.H{"agent_id": nil})
		SetAfter(c, gin.H{"agent_id": 42})
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/tickets/123/assign", strings.NewReader(`{"target_agent_id":42}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if len(recorder.entries) != 1 {
		t.Fatalf("expected 1 audit entry got %d", len(recorder.entries))
	}
	entry := recorder.entries[0]
	if entry.ResourceType != "tickets" {
		t.Fatalf("resource_type = %q want tickets", entry.ResourceType)
	}
	if entry.ResourceID != "123" {
		t.Fatalf("resource_id = %q want 123", entry.ResourceID)
	}
	if entry.Action != "tickets.assign" {
		t.Fatalf("action = %q want tickets.assign", entry.Action)
	}
	if entry.PrincipalKind != "agent" {
		t.Fatalf("principal_kind = %q want agent", entry.PrincipalKind)
	}
	if entry.ActorUserID == nil || *entry.ActorUserID != 7 {
		t.Fatalf("actor_user_id = %v want 7", entry.ActorUserID)
	}
	if entry.RequestID != "req-1" {
		t.Fatalf("request_id = %q want req-1", entry.RequestID)
	}
	if entry.RequestJSON != `{"target_agent_id":42}` {
		t.Fatalf("request_json = %q", entry.RequestJSON)
	}
	if entry.BeforeJSON == "" || entry.AfterJSON == "" {
		t.Fatalf("expected before/after json, got before=%q after=%q", entry.BeforeJSON, entry.AfterJSON)
	}
}

func TestMiddlewareSkipsFailedWriteAndReads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &stubRecorder{}

	r := gin.New()
	r.Use(Middleware(recorder))
	r.GET("/api/tickets", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.POST("/api/tickets", func(c *gin.Context) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
	})

	wRead := httptest.NewRecorder()
	r.ServeHTTP(wRead, httptest.NewRequest(http.MethodGet, "/api/tickets", nil))

	wWrite := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/tickets", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wWrite, req)

	if len(recorder.entries) != 0 {
		t.Fatalf("expected no audit entries, got %d", len(recorder.entries))
	}
}

func TestMiddlewareRedactsSensitiveFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &stubRecorder{}

	r := gin.New()
	r.Use(Middleware(recorder))
	r.POST("/api/apps/config", func(c *gin.Context) {
		SetBefore(c, gin.H{"api_key": "before-secret"})
		SetAfter(c, gin.H{"nested": gin.H{"access_token": "after-secret"}})
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/apps/config", strings.NewReader(`{"password":"p@ss","profile":{"api_key":"k-1"},"list":[{"refresh_token":"r-1"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if len(recorder.entries) != 1 {
		t.Fatalf("expected 1 audit entry got %d", len(recorder.entries))
	}

	entry := recorder.entries[0]
	if strings.Contains(entry.RequestJSON, "p@ss") || strings.Contains(entry.RequestJSON, "k-1") || strings.Contains(entry.RequestJSON, "r-1") {
		t.Fatalf("request_json should redact secrets, got %q", entry.RequestJSON)
	}
	if strings.Contains(entry.BeforeJSON, "before-secret") {
		t.Fatalf("before_json should redact secrets, got %q", entry.BeforeJSON)
	}
	if strings.Contains(entry.AfterJSON, "after-secret") {
		t.Fatalf("after_json should redact secrets, got %q", entry.AfterJSON)
	}
	if !strings.Contains(entry.RequestJSON, "[REDACTED]") || !strings.Contains(entry.BeforeJSON, "[REDACTED]") || !strings.Contains(entry.AfterJSON, "[REDACTED]") {
		t.Fatalf("expected redaction markers, got request=%q before=%q after=%q", entry.RequestJSON, entry.BeforeJSON, entry.AfterJSON)
	}
}

func TestMiddlewareAllowsAuditOverrides(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &stubRecorder{}

	r := gin.New()
	r.Use(Middleware(recorder))
	r.PUT("/security/config/tenant", func(c *gin.Context) {
		SetAction(c, "scoped_config.tenant.update")
		SetResourceType(c, "scoped_config")
		SetResourceID(c, "tenant-a")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/security/config/tenant", strings.NewReader(`{"portal":{"brand_name":"Tenant"}}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if len(recorder.entries) != 1 {
		t.Fatalf("expected 1 audit entry got %d", len(recorder.entries))
	}
	entry := recorder.entries[0]
	if entry.Action != "scoped_config.tenant.update" {
		t.Fatalf("action = %q want scoped_config.tenant.update", entry.Action)
	}
	if entry.ResourceType != "scoped_config" {
		t.Fatalf("resource_type = %q want scoped_config", entry.ResourceType)
	}
	if entry.ResourceID != "tenant-a" {
		t.Fatalf("resource_id = %q want tenant-a", entry.ResourceID)
	}
}

func TestMiddlewareMergesRequestMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &stubRecorder{}

	r := gin.New()
	r.Use(Middleware(recorder))
	r.PUT("/security/config/tenant", func(c *gin.Context) {
		MergeRequestMetadata(c, map[string]interface{}{
			"change_ref": "CHG-1001",
			"reason":     "approve portal branding rollout",
		})
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/security/config/tenant", strings.NewReader(`{"portal":{"brand_name":"Tenant"}}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if len(recorder.entries) != 1 {
		t.Fatalf("expected 1 audit entry got %d", len(recorder.entries))
	}
	entry := recorder.entries[0]
	if !strings.Contains(entry.RequestJSON, `"change_ref":"CHG-1001"`) {
		t.Fatalf("request_json missing change_ref: %q", entry.RequestJSON)
	}
	if !strings.Contains(entry.RequestJSON, `"reason":"approve portal branding rollout"`) {
		t.Fatalf("request_json missing reason: %q", entry.RequestJSON)
	}
}

type errRecorder struct{}

func (errRecorder) Record(_ context.Context, _ Entry) error {
	return errors.New("record failed")
}

func TestMiddlewareRecorderErrorStillSucceeds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware(errRecorder{}))
	r.POST("/api/tickets", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/tickets", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

func TestMiddlewareNilRecorderSkipsAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware(nil))
	r.POST("/api/tickets", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/tickets", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

func TestMiddlewareOverridesWithBlankStringsFallBackToInference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &stubRecorder{}
	r := gin.New()
	r.Use(Middleware(recorder))
	r.DELETE("/api/tickets/:id", func(c *gin.Context) {
		SetAction(c, "   ")
		SetResourceType(c, "  ")
		SetResourceID(c, " ")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/tickets/5", nil))
	if len(recorder.entries) != 1 {
		t.Fatalf("expected 1 entry got %d", len(recorder.entries))
	}
	entry := recorder.entries[0]
	if entry.Action != "tickets.delete" || entry.ResourceType != "tickets" || entry.ResourceID != "5" {
		t.Fatalf("inference fallback failed: %+v", entry)
	}
}

func TestMergeRequestMetadataSkipsInvalidInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	MergeRequestMetadata(nil, map[string]interface{}{"a": 1})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(requestMetaKey, "not-a-map")
	MergeRequestMetadata(c, map[string]interface{}{"valid": "x"})
	v, ok := c.Get(requestMetaKey)
	if !ok {
		t.Fatal("expected merged metadata stored")
	}
	merged, ok := v.(map[string]interface{})
	if !ok || merged["valid"] != "x" {
		t.Fatalf("expected non-map metadata replaced, got %v", v)
	}

	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	MergeRequestMetadata(c2, map[string]interface{}{"": 1, "nil": nil, "blank": "   "})
	if _, ok := c2.Get(requestMetaKey); ok {
		t.Fatal("expected no metadata stored when all values filtered")
	}
}

func TestMiddlewareMetadataOnlyAndInvalidJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &stubRecorder{}
	r := gin.New()
	r.Use(Middleware(recorder))
	r.POST("/api/tickets", func(c *gin.Context) {
		MergeRequestMetadata(c, map[string]interface{}{"change_ref": "CHG-2"})
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/tickets", strings.NewReader("not-json")))

	if len(recorder.entries) != 1 {
		t.Fatalf("expected 1 entry got %d", len(recorder.entries))
	}
	entry := recorder.entries[0]
	if !strings.Contains(entry.RequestJSON, `"request":"not-json"`) || !strings.Contains(entry.RequestJSON, `"change_ref":"CHG-2"`) {
		t.Fatalf("unexpected request_json: %q", entry.RequestJSON)
	}
}

func TestMiddlewareMetadataConflictsGoIntoAuditNamespace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &stubRecorder{}
	r := gin.New()
	r.Use(Middleware(recorder))
	r.POST("/api/tickets", func(c *gin.Context) {
		MergeRequestMetadata(c, map[string]interface{}{"status": "conflict", "same": "equal", "_audit": "clobbered"})
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/tickets", strings.NewReader(`{"status":"open","same":"equal","_audit":{"keep":1}}`)))

	if len(recorder.entries) != 1 {
		t.Fatalf("expected 1 entry got %d", len(recorder.entries))
	}
	entry := recorder.entries[0]
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(entry.RequestJSON), &payload); err != nil {
		t.Fatalf("request_json invalid: %q", entry.RequestJSON)
	}
	auditNS, ok := payload["_audit"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected _audit namespace, got %q", entry.RequestJSON)
	}
	if auditNS["status"] != "conflict" {
		t.Fatalf("expected conflict under _audit, got %q", entry.RequestJSON)
	}
	if auditNS["keep"] != float64(1) {
		t.Fatalf("expected existing _audit fields preserved, got %q", entry.RequestJSON)
	}
	if payload["status"] != "open" {
		t.Fatalf("original key should win, got %q", entry.RequestJSON)
	}
}

func TestMergeRequestJSONNonObjectPayload(t *testing.T) {
	merged := mergeRequestJSON(`[1,2]`, map[string]interface{}{"m": 1})
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(merged), &payload); err != nil {
		t.Fatalf("merged json invalid: %q", merged)
	}
	if _, ok := payload["request"].([]interface{}); !ok {
		t.Fatalf("expected request array, got %q", merged)
	}
	if payload["metadata"] == nil {
		t.Fatalf("expected metadata, got %q", merged)
	}
}

func TestInferResourceTypeVariants(t *testing.T) {
	cases := map[string]string{
		"":                       "unknown",
		"/":                      "unknown",
		"/api":                   "unknown",
		"/api/v1":                "unknown",
		"/api/omni":              "omni",
		"/omni/tickets":          "tickets",
		"/api/omni/tickets":      "tickets",
		"/api/apps":              "apps",
		"/api/apps/whatsapp":     "app_whatsapp",
		"/api/tickets":           "tickets",
		"/api/health-checks":     "health_checks",
		"/api/v1/voice-gateways": "voice_gateways",
	}
	for route, want := range cases {
		if got := inferResourceType(route); got != want {
			t.Fatalf("inferResourceType(%q) = %q want %q", route, got, want)
		}
	}
}

func TestInferActionVariants(t *testing.T) {
	cases := []struct {
		route  string
		method string
		want   string
	}{
		{"", "POST", "post"},
		{"/api/tickets", "POST", "tickets.create"},
		{"/api/tickets/:id", "POST", "tickets.create"},
		{"/api/tickets/:id/assign", "POST", "tickets.assign"},
		{"/api/tickets/:id", "PUT", "tickets.update"},
		{"/api/tickets/:id", "PATCH", "tickets.update"},
		{"/api/tickets/:id", "DELETE", "tickets.delete"},
		{"/api/tickets", "GET", "tickets.get"},
		{"/api/tickets/:id/assign", "PUT", "tickets.assign"},
		{"/api/tickets/:id/assign", "DELETE", "tickets.delete"},
	}
	for _, tc := range cases {
		if got := inferAction(tc.route, tc.method); got != tc.want {
			t.Fatalf("inferAction(%q, %q) = %q want %q", tc.route, tc.method, got, tc.want)
		}
	}
}

func TestInferResourceIDPriority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/sessions/:session_id/recordings/:recording_id", func(c *gin.Context) {
		if got := inferResourceID(c); got != "sess-1" {
			t.Fatalf("session_id should win, got %q", got)
		}
		c.JSON(http.StatusOK, nil)
	})
	r.GET("/protocols/:protocol", func(c *gin.Context) {
		if got := inferResourceID(c); got != "sip" {
			t.Fatalf("protocol param should win, got %q", got)
		}
		c.JSON(http.StatusOK, nil)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sessions/sess-1/recordings/rec-9", nil))
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/protocols/sip", nil))

	if inferResourceID(nil) != "" {
		t.Fatal("nil context should return empty resource id")
	}
}

func TestActorUserIDNonUintValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_id", "7")
	if id := actorUserID(c); id != nil {
		t.Fatalf("expected nil for string user_id, got %v", id)
	}
	if id := actorUserID(nil); id != nil {
		t.Fatalf("expected nil for nil ctx, got %v", id)
	}
}

func TestCaptureRequestBodyNilBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if body := captureRequestBody(c); body != "" {
		t.Fatalf("expected empty body, got %q", body)
	}
}

func TestContextJSONInvalidAndRedactedValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(beforeKey, make(chan int))
	if out := contextJSON(c, beforeKey); out != "" {
		t.Fatalf("expected empty json for unmarshalable value, got %q", out)
	}
	if out := contextJSON(nil, beforeKey); out != "" {
		t.Fatalf("expected empty json for nil ctx, got %q", out)
	}

	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Set(afterKey, nil)
	if out := contextJSON(c2, afterKey); out != "" {
		t.Fatalf("expected empty json for nil value, got %q", out)
	}
}

func TestRedactJSONTextEdgeCases(t *testing.T) {
	if got := redactJSONText(""); got != "" {
		t.Fatalf("empty input = %q", got)
	}
	if got := redactJSONText("plain text"); got != "plain text" {
		t.Fatalf("non-json passthrough = %q", got)
	}
	if got := redactJSONText(`{"token":"abc"}`); got != `{"token":"[REDACTED]"}` {
		t.Fatalf("redaction failed: %q", got)
	}
}

type failingBody struct{}

func (failingBody) Read(_ []byte) (int, error) { return 0, errors.New("read failed") }
func (failingBody) Close() error               { return nil }

func TestCaptureRequestBodyReadError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/api/tickets", nil)
	req.Body = failingBody{}
	c.Request = req
	if body := captureRequestBody(c); body != "" {
		t.Fatalf("expected empty body on read error, got %q", body)
	}
}

func TestStringAndRequestMetadataNilContext(t *testing.T) {
	if got := stringValue(nil, "user_id"); got != "" {
		t.Fatalf("stringValue(nil) = %q", got)
	}
	if got := requestMetadata(nil); got != nil {
		t.Fatalf("requestMetadata(nil) = %v", got)
	}

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(requestMetaKey, "not-a-map")
	if got := requestMetadata(c); got != nil {
		t.Fatalf("requestMetadata(non-map) = %v", got)
	}
}

func TestMergeRequestMetadataAppendsToExisting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	MergeRequestMetadata(c, map[string]interface{}{"first": "1"})
	MergeRequestMetadata(c, map[string]interface{}{"second": 2})
	v, ok := c.Get(requestMetaKey)
	if !ok {
		t.Fatal("expected metadata stored")
	}
	merged := v.(map[string]interface{})
	if merged["first"] != "1" || merged["second"] != 2 {
		t.Fatalf("unexpected merged metadata: %v", merged)
	}
}

func TestMergeRequestJSONMarshalFailures(t *testing.T) {
	ch := map[string]interface{}{"ch": make(chan int)}

	if got := mergeRequestJSON("", ch); got != "" {
		t.Fatalf("metadata-only marshal error = %q want empty", got)
	}
	if got := mergeRequestJSON("plain", ch); got != "plain" {
		t.Fatalf("non-json marshal error = %q want raw", got)
	}
	if got := mergeRequestJSON(`[1,2]`, ch); got != `[1,2]` {
		t.Fatalf("array marshal error = %q want raw", got)
	}
	if got := mergeRequestJSON(`{"a":1}`, ch); got != `{"a":1}` {
		t.Fatalf("object marshal error = %q want raw", got)
	}
	if got := mergeRequestJSON(`{"ch":1}`, ch); got != `{"ch":1}` {
		t.Fatalf("conflict marshal error = %q want raw", got)
	}
}

// TestMiddlewareWithOptionsAuditFailures：AuditFailures=true 时 4xx 响应
// 同样落审计（auth 公开面的失败登录必须留痕）；false（默认）保持跳过。
func TestMiddlewareWithOptionsAuditFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newRouter := func(recorder Recorder, opts Options) *gin.Engine {
		r := gin.New()
		r.Use(MiddlewareWithOptions(recorder, opts))
		r.POST("/api/v1/auth/login", func(c *gin.Context) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "bad credentials"})
		})
		return r
	}

	failing := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"u","password":"p"}`))

	t.Run("audit failures records 4xx", func(t *testing.T) {
		recorder := &stubRecorder{}
		w := httptest.NewRecorder()
		newRouter(recorder, Options{AuditFailures: true}).ServeHTTP(w, failing)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 got %d", w.Code)
		}
		if len(recorder.entries) != 1 {
			t.Fatalf("expected 1 audit entry for failed login, got %d", len(recorder.entries))
		}
		entry := recorder.entries[0]
		if entry.Success {
			t.Fatal("failed login must be recorded as success=false")
		}
		if entry.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status_code = %d want 401", entry.StatusCode)
		}
		if !strings.Contains(entry.RequestJSON, "[REDACTED]") {
			t.Fatalf("expected password redacted in request json, got %q", entry.RequestJSON)
		}
	})

	t.Run("default options skip 4xx", func(t *testing.T) {
		recorder := &stubRecorder{}
		w := httptest.NewRecorder()
		newRouter(recorder, Options{}).ServeHTTP(w, failing)
		if len(recorder.entries) != 0 {
			t.Fatalf("expected no audit entry for failed write with default options, got %d", len(recorder.entries))
		}
	})
}
