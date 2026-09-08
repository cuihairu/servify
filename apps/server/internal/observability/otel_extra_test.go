package observability

import (
	"context"
	"strings"
	"testing"

	"servify/apps/server/internal/config"
)

func TestSetupTracingEmptyServiceNameDefaultsToServify(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Monitoring.Tracing.Enabled = true
	cfg.Monitoring.Tracing.ServiceName = ""
	cfg.Monitoring.Tracing.Insecure = true
	cfg.Monitoring.Tracing.Endpoint = "127.0.0.1:4317"

	shutdown, err := SetupTracing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SetupTracing() error = %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected shutdown function")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
}

func TestSetupTracingResourceError(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Monitoring.Tracing.Enabled = true
	cfg.Monitoring.Tracing.ServiceName = "resource-error-test"
	cfg.Monitoring.Tracing.Insecure = true
	cfg.Monitoring.Tracing.Endpoint = "127.0.0.1:4317"

	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "not-a-valid-entry")

	_, err := SetupTracing(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected resource construction error")
	}
	if !strings.Contains(err.Error(), "resource") {
		t.Fatalf("error = %v, want resource failure", err)
	}
}

func TestSetupTracingValidSampleRatioKept(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Monitoring.Tracing.Enabled = true
	cfg.Monitoring.Tracing.ServiceName = "ratio-test"
	cfg.Monitoring.Tracing.Insecure = true
	cfg.Monitoring.Tracing.Endpoint = "127.0.0.1:4317"
	cfg.Monitoring.Tracing.SampleRatio = 1

	shutdown, err := SetupTracing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SetupTracing() error = %v", err)
	}
	if shutdown != nil {
		_ = shutdown(context.Background())
	}
}

func TestEndpointHostHTTPSAndPlain(t *testing.T) {
	if got := endpointHost("https://collector:4317"); got != "collector:4317" {
		t.Fatalf("endpointHost(https) = %q", got)
	}
	if got := endpointHost("collector.internal:4317"); got != "collector.internal:4317" {
		t.Fatalf("endpointHost(plain) = %q", got)
	}
	if got := endpointHost("httpx://weird"); got != "httpx://weird" {
		t.Fatalf("endpointHost(non-scheme) = %q", got)
	}
	if got := endpointHost("https:/single-slash"); got != "https:/single-slash" {
		t.Fatalf("endpointHost(single slash) = %q", got)
	}
}
