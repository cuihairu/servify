package errors

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"servify/apps/server/internal/observability/metrics"

	"github.com/gin-gonic/gin"
)

// resetRegistration 把包级注册状态还原为未初始化，保证本文件的用例各自
// 独立注册，且不改变 metrics_test.go 看到的初始状态（它在本文件之后执行）。
func resetRegistration(t *testing.T) {
	t.Helper()
	registerOnce = sync.Once{}
	errorTotal = nil
	t.Cleanup(func() {
		registerOnce = sync.Once{}
		errorTotal = nil
	})
}

func statusMiddlewareRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(StatusMiddleware())
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/not-found", func(c *gin.Context) { c.Status(http.StatusNotFound) })
	r.GET("/rate-limited", func(c *gin.Context) { c.Status(http.StatusTooManyRequests) })
	r.GET("/boom", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
	r.GET("/upstream", func(c *gin.Context) { c.Status(http.StatusBadGateway) })
	r.GET("/timeout", func(c *gin.Context) { c.Status(http.StatusGatewayTimeout) })
	return r
}

// gatherErrorSeries 从 registry 摘取 errors_total 的 series 集合，key 为
// "severity|error_category|error_module"，value 为计数值。
func gatherErrorSeries(t *testing.T, reg *metrics.Registry) map[string]float64 {
	t.Helper()
	mfs, err := reg.Gatherer().Gather()
	if err != nil {
		t.Fatalf("unexpected gather error: %v", err)
	}
	series := map[string]float64{}
	for _, mf := range mfs {
		if mf.GetName() != "errors_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			key := labels["severity"] + "|" + labels["error_category"] + "|" + labels["error_module"]
			series[key] = m.GetCounter().GetValue()
		}
	}
	return series
}

func TestStatusMiddlewareRecordsOnlyServerErrors(t *testing.T) {
	resetRegistration(t)
	reg := metrics.NewRegistry()
	RegisterErrorMetrics(reg)

	r := statusMiddlewareRouter()
	for _, path := range []string{"/ok", "/not-found", "/rate-limited"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	}

	if series := gatherErrorSeries(t, reg); len(series) != 0 {
		t.Fatalf("expected no errors_total series for 2xx/4xx, got %v", series)
	}
}

func TestStatusMiddlewareClassifiesServerErrorStatuses(t *testing.T) {
	resetRegistration(t)
	reg := metrics.NewRegistry()
	RegisterErrorMetrics(reg)

	r := statusMiddlewareRouter()
	for _, path := range []string{"/boom", "/boom", "/upstream", "/timeout"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	}

	series := gatherErrorSeries(t, reg)
	expected := map[string]float64{
		"system|internal|http":       2, // 500 x2
		"dependency|network|http":    2, // 502 + 504
	}
	if len(series) != len(expected) {
		t.Fatalf("expected %d series, got %v", len(expected), series)
	}
	for key, want := range expected {
		if series[key] != want {
			t.Fatalf("series %q: expected %v, got %v", key, want, series[key])
		}
	}
}

func TestRecordHTTPStatusWithoutRegistrationIsNoop(t *testing.T) {
	resetRegistration(t)
	RecordHTTPStatus(http.StatusInternalServerError)
	RecordHTTPStatus(http.StatusBadGateway)
}

func TestClassifyHTTPStatus(t *testing.T) {
	cases := []struct {
		status int
		sev    Severity
		cat    Category
	}{
		{http.StatusBadGateway, SeverityDependency, CategoryNetwork},
		{http.StatusGatewayTimeout, SeverityDependency, CategoryNetwork},
		{http.StatusInternalServerError, SeveritySystem, CategoryInternal},
		{http.StatusNotImplemented, SeveritySystem, CategoryInternal},
		{http.StatusServiceUnavailable, SeveritySystem, CategoryInternal},
	}
	for _, tc := range cases {
		sev, cat := classifyHTTPStatus(tc.status)
		if sev != tc.sev || cat != tc.cat {
			t.Fatalf("classifyHTTPStatus(%d) = (%s, %s), want (%s, %s)",
				tc.status, sev, cat, tc.sev, tc.cat)
		}
	}
}

func TestHTTPStatusErrorString(t *testing.T) {
	e := &httpStatusError{status: http.StatusServiceUnavailable}
	if got, want := e.Error(), "http response 503"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestRegisterErrorMetricsIsIdempotentPerProcess(t *testing.T) {
	resetRegistration(t)
	reg := metrics.NewRegistry()
	RegisterErrorMetrics(reg)
	// 第二次注册必须静默跳过（不 panic on duplicate registration），
	// 否则多次 BuildRuntime 的进程会在启动期崩溃。
	RegisterErrorMetrics(reg)

	RecordHTTPStatus(http.StatusInternalServerError)
	series := gatherErrorSeries(t, reg)
	if series["system|internal|http"] != 1 {
		t.Fatalf("expected single system|internal|http series with value 1, got %v", series)
	}
}

// TestErrorCounterLabelsRegistered 锁定 CounterVec 的标签集合，防止
// PromQL（rules.yaml / dashboard）引用的标签悄悄漂移。
func TestErrorCounterLabelsRegistered(t *testing.T) {
	resetRegistration(t)
	reg := metrics.NewRegistry()
	RegisterErrorMetrics(reg)
	// CounterVec 无 series 时 Gather 不产出 metric family，先打一个点。
	RecordHTTPStatus(http.StatusInternalServerError)

	mfs, err := reg.Gatherer().Gather()
	if err != nil {
		t.Fatalf("unexpected gather error: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "errors_total" {
			continue
		}
		labels := map[string]bool{}
		for _, lp := range mf.GetMetric()[0].GetLabel() {
			labels[lp.GetName()] = true
		}
		for _, want := range []string{"severity", "error_category", "error_module"} {
			if !labels[want] {
				t.Fatalf("errors_total missing label %q, got %v", want, labels)
			}
		}
		return
	}
	t.Fatal("errors_total not registered")
}
