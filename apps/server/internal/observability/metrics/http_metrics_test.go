package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestHTTPMetrics_Middleware(t *testing.T) {
	reg := NewRegistry()
	hm := NewHTTPMetrics(reg)

	r := gin.New()
	r.Use(hm.Middleware())
	r.GET("/test/:id", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/123", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify metrics were recorded
	mfs, err := reg.Gatherer().Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	foundRequests := false
	foundDuration := false
	for _, mf := range mfs {
		if mf.GetName() == "http_requests_total" {
			foundRequests = true
			if len(mf.GetMetric()) == 0 {
				t.Fatal("expected at least one metric sample")
			}
			m := mf.GetMetric()[0]
			if m.GetCounter().GetValue() != 1 {
				t.Fatalf("expected counter value 1, got %v", m.GetCounter().GetValue())
			}
			// Verify path is the route pattern, not the actual path
			labels := m.GetLabel()
			pathOK := false
			for _, l := range labels {
				if l.GetName() == "path" && l.GetValue() == "/test/:id" {
					pathOK = true
				}
			}
			if !pathOK {
				t.Fatal("expected path label /test/:id (route pattern)")
			}
		}
		if mf.GetName() == "http_request_duration_seconds" {
			foundDuration = true
		}
	}

	if !foundRequests {
		t.Fatal("expected http_requests_total metric")
	}
	if !foundDuration {
		t.Fatal("expected http_request_duration_seconds metric")
	}
}

func TestHTTPMetrics_Middleware_404(t *testing.T) {
	reg := NewRegistry()
	hm := NewHTTPMetrics(reg)

	r := gin.New()
	r.Use(hm.Middleware())
	// No routes registered

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	r.ServeHTTP(w, req)

	mfs, _ := reg.Gatherer().Gather()
	for _, mf := range mfs {
		if mf.GetName() == "http_requests_total" {
			for _, m := range mf.GetMetric() {
				for _, l := range m.GetLabel() {
					if l.GetName() == "path" && l.GetValue() == "unknown" {
						return // OK: unmatched route labeled as "unknown"
					}
				}
			}
		}
	}
	t.Fatal("expected unknown path label for unmatched route")
}

func TestHTTPMetrics_Middleware_RateLimitDrop(t *testing.T) {
	reg := NewRegistry()
	hm := NewHTTPMetrics(reg)

	r := gin.New()
	r.Use(hm.Middleware())
	r.GET("/limited/:id", func(c *gin.Context) {
		c.String(http.StatusTooManyRequests, "slow down")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/limited/7", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}

	mfs, _ := reg.Gatherer().Gather()
	for _, mf := range mfs {
		if mf.GetName() != "ratelimit_dropped_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			if m.GetCounter().GetValue() != 1 {
				t.Fatalf("expected drop counter 1, got %v", m.GetCounter().GetValue())
			}
			for _, l := range m.GetLabel() {
				if l.GetName() == "path" && l.GetValue() == "/limited/:id" {
					return // OK
				}
			}
		}
		t.Fatal("expected path label /limited/:id on ratelimit_dropped_total")
	}
	t.Fatal("expected ratelimit_dropped_total metric")
}

func TestPrometheusHandler(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterGoCollector()

	r := gin.New()
	r.GET("/metrics", PrometheusHandler(reg))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "go_goroutines") {
		t.Fatal("expected go_goroutines in metrics output")
	}
}
