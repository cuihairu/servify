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
