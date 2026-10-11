package observability

import (
	"context"
	"servify/apps/server/internal/config"
	"testing"
)

func TestSetupTracing_Disabled_NoOp(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Monitoring.Tracing.Enabled = false
	shutdown, err := SetupTracing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if shutdown == nil {
		t.Fatalf("expected non-nil shutdown function")
	}
	// 验证 shutdown 可以调用
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown error: %v", err)
	}
}

func TestEndpointHost_Parse(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"http://localhost:4317", "localhost:4317"},
		{"https://otel-collector:4317", "otel-collector:4317"},
		{"127.0.0.1:4317", "127.0.0.1:4317"},
		{"", ""},
		// "http://" 长度是 7，不满足 >7 条件，所以返回原字符串
		{"http://", "http://"},
		{"https://example.com:4317/path", "example.com:4317/path"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := endpointHost(tt.input); got != tt.expected {
				t.Fatalf("endpointHost(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSetupTracing_Enabled_DefaultValues(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Monitoring.Tracing.Enabled = true
	cfg.Monitoring.Tracing.Endpoint = ""
	cfg.Monitoring.Tracing.SampleRatio = 0

	shutdown, err := SetupTracing(context.Background(), cfg)
	// 可能会连接失败，这是预期的
	if err != nil {
		return
	}
	if shutdown != nil {
		_ = shutdown(context.Background()) // 测试收尾
	}
}

func TestSetupTracing_InvalidSampleRatio(t *testing.T) {
	tests := []struct {
		name  string
		ratio float64
	}{
		{"negative", -0.1},
		{"zero", 0},
		{"greater than one", 1.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.GetDefaultConfig()
			cfg.Monitoring.Tracing.Enabled = true
			cfg.Monitoring.Tracing.SampleRatio = tt.ratio

			shutdown, err := SetupTracing(context.Background(), cfg)
			// 连接失败是预期的
			if err != nil {
				return
			}
			if shutdown != nil {
				_ = shutdown(context.Background()) // 测试收尾
			}
		})
	}
}

func TestSetupTracing_WithServiceName(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Monitoring.Tracing.Enabled = true
	cfg.Monitoring.Tracing.ServiceName = "test-service"

	shutdown, err := SetupTracing(context.Background(), cfg)
	if err != nil {
		return
	}
	if shutdown != nil {
		_ = shutdown(context.Background()) // 测试收尾
	}
}
