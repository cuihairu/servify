package scripts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSecurityObservabilityBaselineChecksPassStrict 真跑两个已发布但从未挂
// CI 的 strict 基线门（check-security-baseline.sh / check-observability-
// baseline.sh）。目标用生产示例配置：默认 config.yml 是开发形态（空 jwt
// secret / 空库密码 / 空 openai key），strict 下必红；示例配置的 ${VAR}
// 占位由 env 展开为非空哨兵值——校验的是配置面结构，不是真实凭据。
func TestSecurityObservabilityBaselineChecksPassStrict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed baseline script tests are not stable on Windows")
	}

	projectRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	configPath := filepath.Join(projectRoot, "config.production.secure.example.yml")

	// 占位 env：示例配置的 ${VAR} 经 expandEnvPlaceholders 展开为非空值，
	// jwt secret 不得落在 InsecureJWTSecrets 已知弱值清单里。
	baselineEnv := append(os.Environ(),
		"SERVIFY_JWT_SECRET=baseline-check-sentinel-secret-0123456789",
		"DB_USER=servify",
		"DB_PASSWORD=baseline-check-sentinel-password",
		"OPENAI_API_KEY=baseline-check-sentinel-openai-key",
		"DIFY_API_KEY=baseline-check-sentinel-dify-key",
		"DIFY_DATASET_ID=baseline-check-sentinel-dataset",
	)

	for _, script := range []string{"check-security-baseline.sh", "check-observability-baseline.sh"} {
		cmd := exec.Command("bash", "-lc", fmt.Sprintf(
			"bash ./scripts/%s %q", script, configPath,
		))
		cmd.Dir = projectRoot
		cmd.Env = baselineEnv
		output, scriptErr := cmd.CombinedOutput()
		if scriptErr != nil {
			t.Fatalf("%s strict check failed: %v\n%s", script, scriptErr, string(output))
		}
		if !strings.Contains(string(output), "check passed") {
			t.Fatalf("%s did not report pass: %s", script, string(output))
		}
	}
}

// TestReleaseReadinessCheckPasses 真跑 check-release-readiness.sh（默认
// config.yml 的 sqlite 豁免口径）：本地环境 → strict 安全/可观测基线 →
// 聚焦 Go 回归 → 三二进制构建 → 关键路由校验 → 迁移冒烟 → HTTP
// health/ready/metrics 冒烟。该门此前同样从未挂 CI。
func TestReleaseReadinessCheckPasses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed baseline script tests are not stable on Windows")
	}

	projectRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"SERVIFY_RELEASE_CHECK_PORT=%s bash ./scripts/check-release-readiness.sh",
		freePort(t),
	))
	cmd.Dir = projectRoot
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("release readiness check failed: %v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "Release readiness check passed.") {
		t.Fatalf("release readiness did not report pass: %s", string(output))
	}
}
