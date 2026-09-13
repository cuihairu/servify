package bootstrap

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"servify/apps/server/internal/config"
)

func ObservabilityWarnings(cfg *config.Config, repoRoot string) []string {
	if cfg == nil {
		return []string{"config is nil; observability baseline cannot be validated"}
	}

	var warnings []string

	if !cfg.Monitoring.Enabled {
		warnings = append(warnings, "monitoring is disabled")
	}
	if strings.TrimSpace(cfg.Monitoring.MetricsPath) == "" {
		warnings = append(warnings, "monitoring.metrics_path is empty")
	} else if !strings.HasPrefix(strings.TrimSpace(cfg.Monitoring.MetricsPath), "/") {
		warnings = append(warnings, "monitoring.metrics_path must start with /")
	}

	if cfg.Monitoring.Tracing.Enabled {
		if strings.TrimSpace(cfg.Monitoring.Tracing.Endpoint) == "" {
			warnings = append(warnings, "monitoring.tracing.endpoint is empty while tracing is enabled")
		}
		if cfg.Monitoring.Tracing.SampleRatio <= 0 || cfg.Monitoring.Tracing.SampleRatio > 1 {
			warnings = append(warnings, "monitoring.tracing.sample_ratio must be within (0,1]")
		}
		if strings.TrimSpace(cfg.Monitoring.Tracing.ServiceName) == "" {
			warnings = append(warnings, "monitoring.tracing.service_name is empty while tracing is enabled")
		}
	}

	root := strings.TrimSpace(repoRoot)
	if root == "" {
		root = repoRootFromSource()
	}
	if root == "" {
		warnings = append(warnings, "repository root could not be resolved for observability asset checks")
		return warnings
	}

	for _, rel := range []string{
		"infra/compose/docker-compose.observability.yml",
		"infra/observability/otel-collector-config.yaml",
		"deploy/observability/alerts/rules.yaml",
		"deploy/observability/dashboards/servify-service.json",
		"deploy/observability/dashboards/servify-business.json",
		"deploy/observability/runbook/operational-runbook.md",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			warnings = append(warnings, "observability asset missing: "+rel)
		}
	}

	return warnings
}

// repoRootFromSource 是包级变量 seam：测试可注入空实现以覆盖调用方的
// "root could not be resolved" 分支。runtime.Caller(0) 对运行中的函数
// 恒成功，故不再保留 !ok 分支。
var repoRootFromSource = func() string {
	_, file, _, _ := runtime.Caller(0)

	root := filepath.Dir(file)
	for i := 0; i < 5; i++ {
		root = filepath.Dir(root)
	}
	return root
}
