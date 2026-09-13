package observability

import (
	"context"
	"strings"
	"testing"

	"servify/apps/server/internal/config"
)

// TestSetupTracingExporterError 覆盖 OTLP exporter 构造失败分支:
// endpoint 携带控制字符时 gRPC target(URL)解析立即失败。
func TestSetupTracingExporterError(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Monitoring.Tracing.Enabled = true
	cfg.Monitoring.Tracing.Endpoint = "http://bad\x7fendpoint:4317"

	_, err := SetupTracing(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected exporter construction error for invalid endpoint")
	}
	if !strings.Contains(err.Error(), "otlp exporter") {
		t.Fatalf("error = %v, want otlp exporter failure", err)
	}
}
