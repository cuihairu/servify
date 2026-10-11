package telemetry

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/trace"
)

// captureHook 捕获 logrus entry 供断言（Fire 只记录不输出）。
type captureHook struct {
	mu      sync.Mutex
	entries []*logrus.Entry
}

func (h *captureHook) Fire(e *logrus.Entry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, e)
	return nil
}

func (h *captureHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// spanContextWithTraceID 构造带 trace_id 的 OTel span 上下文（纯内存，无
// SDK），验证 FieldsFromContext 的 span 优先分支。
func spanContextWithTraceID(t *testing.T, hexID string) trace.SpanContext {
	t.Helper()
	raw, err := hex.DecodeString(hexID)
	if err != nil {
		t.Fatalf("decode trace id: %v", err)
	}
	var tid trace.TraceID
	copy(tid[:], raw)
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid})
	return sc
}

func TestFieldsFromContextAssemblesAllSources(t *testing.T) {
	r := gin.New()
	r.Use(RequestIDMiddleware())
	sc := spanContextWithTraceID(t, "4bf92f3577b34da6a3ce929d0e0e4736")
	r.GET("/test", func(c *gin.Context) {
		ctx := platformauth.ContextWithScope(c.Request.Context(), "tenant-a", "ws-1")
		ctx = trace.ContextWithSpanContext(ctx, sc)
		c.Request = c.Request.WithContext(ctx)

		fields := FieldsFromContext(c)
		if fields[FieldRequestID] == "" {
			t.Fatal("expected request_id field")
		}
		if fields[FieldTenantID] != "tenant-a" {
			t.Fatalf("expected tenant-a, got %v", fields[FieldTenantID])
		}
		if fields[FieldTraceID] != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatalf("expected span trace id, got %v", fields[FieldTraceID])
		}
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestFieldsFromContextFallsBackToTraceHeader(t *testing.T) {
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/test", func(c *gin.Context) {
		fields := FieldsFromContext(c)
		if fields[FieldTraceID] != "upstream-trace-1" {
			t.Fatalf("expected header trace id, got %v", fields[FieldTraceID])
		}
		if _, ok := fields[FieldTenantID]; ok {
			t.Fatal("tenant must be absent without scoped ctx")
		}
		if _, ok := fields[FieldRequestID]; !ok {
			t.Fatal("expected request_id present")
		}
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Trace-ID", "upstream-trace-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
}

func TestFieldsFromContextSpanWinsOverHeader(t *testing.T) {
	sc := spanContextWithTraceID(t, "4bf92f3577b34da6a3ce929d0e0e4736")
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/test", func(c *gin.Context) {
		ctx := trace.ContextWithSpanContext(c.Request.Context(), sc)
		c.Request = c.Request.WithContext(ctx)

		fields := FieldsFromContext(c)
		if fields[FieldTraceID] != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatalf("span trace id must win over header, got %v", fields[FieldTraceID])
		}
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Trace-ID", "upstream-trace-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
}

// AccessLogger 把透传字段与访问维度写进 logrus 标准出口；hook 捕获 entry
// 逐字段断言。
func TestAccessLoggerEmitsTelemetryFields(t *testing.T) {
	hook := &captureHook{}
	logrus.StandardLogger().AddHook(hook)
	defer func() { logrus.StandardLogger().Hooks = nil }()

	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.Use(AccessLogger())
	r.GET("/logtest", func(c *gin.Context) {
		ctx := platformauth.ContextWithScope(c.Request.Context(), "tenant-log", "")
		c.Request = c.Request.WithContext(ctx)
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/logtest?q=1", nil)
	req.Header.Set("X-Trace-ID", "trace-log-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	hook.mu.Lock()
	defer hook.mu.Unlock()
	var entry *logrus.Entry
	for _, e := range hook.entries {
		if e.Data["path"] == "/logtest" {
			entry = e
			break
		}
	}
	if entry == nil {
		t.Fatalf("expected access log entry, got %+v", hook.entries)
	}
	if entry.Data["request_id"] == "" {
		t.Fatal("expected request_id in access log")
	}
	if entry.Data["tenant_id"] != "tenant-log" {
		t.Fatalf("expected tenant-log, got %v", entry.Data["tenant_id"])
	}
	if entry.Data["trace_id"] != "trace-log-1" {
		t.Fatalf("expected trace-log-1, got %v", entry.Data["trace_id"])
	}
	if entry.Data["status"] != http.StatusOK {
		t.Fatalf("expected status 200, got %v", entry.Data["status"])
	}
	if entry.Data["method"] != http.MethodGet {
		t.Fatalf("expected GET, got %v", entry.Data["method"])
	}
	if entry.Data["query"] != "q=1" {
		t.Fatalf("expected query q=1, got %v", entry.Data["query"])
	}
	if entry.Data["client_ip"] == "" {
		t.Fatal("expected client_ip in access log")
	}
}

func TestTraceIDFromContextEmptyWithoutSpan(t *testing.T) {
	if got := TraceIDFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty trace id without span, got %q", got)
	}
}
