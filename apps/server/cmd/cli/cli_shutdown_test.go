package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	appbootstrap "servify/apps/server/internal/app/bootstrap"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyCLIRunFault 在子进程 worker 内按 CLI_RUN_VARIANT 注入 seam 失败，
// 覆盖共享编排根 RunStandalone 的错误 fatal 分支。seam 本体托管在 bootstrap
// 包（standalone.go），这里只是 variant 名到 fault 值的映射。生产进程不设置
// 该 variant。
func applyCLIRunFault(variant string) {
	if variant == "start-failure" {
		appbootstrap.ApplyFault("start-runtime")
	}
}

// TestCLIRunStartRuntimeFailure 覆盖 RunStandalone 的 "Failed to start
// runtime" fatal 分支：sqlite 库让装配推进到 startAppRuntime，注入失败后
// run() 以 1 退出。
func TestCLIRunStartRuntimeFailure(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
		[]byte(baseCLIRunConfig(false, "127.0.0.1")), 0o600))

	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIWorker$", "-test.timeout=2m")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CLI_RUN_SUBPROCESS=1",
		"CLI_RUN_VARIANT=start-failure",
		"CLI_RUN_DIR="+dir,
	)
	cmd.Env = append(cmd.Env, smokeCLIRunEnv(t, dir)...)
	out, _ := cmd.CombinedOutput()
	require.NotNil(t, cmd.ProcessState, "output: %s", out)
	assert.EqualValues(t, 1, cmd.ProcessState.ExitCode(), "output: %s", out)
	assert.Contains(t, string(out), "Failed to start runtime")
	assert.Contains(t, string(out), "injected start failure")
}
