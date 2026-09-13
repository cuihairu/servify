package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCLIRunBuildAppFailure 覆盖 run() 的 BuildApp fatal 分支：坏 embedding
// 配置让 BuildApp 失败，子进程必须以 "Failed to build app" 退出。
func TestCLIRunBuildAppFailure(t *testing.T) {
	dir := t.TempDir()
	cfg := baseCLIRunConfig(false, "127.0.0.1") +
		"embedding:\n  provider: tei\n  tei:\n    base_url: \"\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), []byte(cfg), 0o600))

	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIWorker$", "-test.timeout=2m")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CLI_RUN_SUBPROCESS=1",
		"CLI_RUN_VARIANT=build-app-failure",
		"CLI_RUN_DIR="+dir,
	)
	out, _ := cmd.CombinedOutput()
	require.NotNil(t, cmd.ProcessState, "subprocess must run, output: %s", out)
	assert.EqualValues(t, 1, cmd.ProcessState.ExitCode(), "output: %s", out)
	assert.Contains(t, string(out), "Failed to build app")
}

// TestCLIRunTracingSetupWarning 覆盖 SetupObservability 失败时的 Warnf 分支：
// 非法的 OTEL_RESOURCE_ATTRIBUTES 让 resource.New 失败，run() 只告警并继续
// 启动（随后在 setupRouter 的既有 panic 处被 worker 回收退出）。
func TestCLIRunTracingSetupWarning(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
		[]byte(baseCLIRunConfig(true, "localhost")), 0o600))

	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIWorker$", "-test.timeout=2m")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CLI_RUN_SUBPROCESS=1",
		"CLI_RUN_VARIANT=tracing-warn",
		"CLI_RUN_DIR="+dir,
		"OTEL_RESOURCE_ATTRIBUTES=not-a-valid-entry",
	)
	out, _ := cmd.CombinedOutput()
	assert.Contains(t, string(out), "init tracing")
	require.NotNil(t, cmd.ProcessState, "subprocess must run, output: %s", out)
	assert.EqualValues(t, 0, cmd.ProcessState.ExitCode(), "output: %s", out)
}
