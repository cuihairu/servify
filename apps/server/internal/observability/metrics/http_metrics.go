package metrics

import (
	"strconv"
	"time"

	"servify/apps/server/internal/observability/telemetry"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

// HTTPMetrics holds Prometheus instruments for HTTP request observability.
type HTTPMetrics struct {
	requestsTotal    *prometheus.CounterVec
	requestDuration  *prometheus.HistogramVec
	responseSize     *prometheus.HistogramVec
	rateLimitDropped *prometheus.CounterVec
}

// NewHTTPMetrics creates and registers HTTP metric collectors.
func NewHTTPMetrics(reg *Registry) *HTTPMetrics {
	m := &HTTPMetrics{
		rateLimitDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: telemetry.MetricRateLimitDropped,
			Help: "Total HTTP 429 responses due to rate limiting.",
		}, []string{telemetry.LabelPath}),
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: telemetry.MetricHTTPRequestTotal,
			Help: "Total number of HTTP requests processed.",
		}, []string{telemetry.LabelMethod, telemetry.LabelPath, telemetry.LabelStatusCode}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    telemetry.MetricHTTPRequestDuration,
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{telemetry.LabelMethod, telemetry.LabelPath}),
		responseSize: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    telemetry.MetricHTTPResponseSize,
			Help:    "HTTP response size in bytes.",
			Buckets: prometheus.ExponentialBuckets(100, 10, 7), // 100, 1K, 10K, 100K, 1M, 10M, 100M
		}, []string{telemetry.LabelMethod, telemetry.LabelPath}),
	}

	reg.MustRegister(m.requestsTotal, m.requestDuration, m.responseSize, m.rateLimitDropped)
	return m
}

// Middleware returns a Gin middleware that records latency, count, and response size.
// Uses c.FullPath() for the path label to avoid high cardinality from parametric routes.
func (m *HTTPMetrics) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = "unknown"
		}

		c.Next()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())
		method := c.Request.Method
		size := float64(c.Writer.Size())

		m.requestsTotal.WithLabelValues(method, path, status).Inc()
		m.requestDuration.WithLabelValues(method, path).Observe(duration)
		m.responseSize.WithLabelValues(method, path).Observe(size)
		// 429 视为限流丢弃，单独计数供 HighRateLimitDrops 告警使用。
		if status == "429" {
			m.rateLimitDropped.WithLabelValues(path).Inc()
		}
	}
}
