package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// smokeCLIPort 冒烟测试专用端口：避开 perf 基线的 18107/18108 与默认 8080。
const smokeCLIPort = "18291"

// smokeCLIRunEnv 返回冒烟/注入测试子进程的 sqlite 运行环境（与验收脚本同
// 款：DB_DRIVER=sqlite + DB_DSN + SERVIFY_PORT 环境变量，经
// ResolveRuntimeOverrides 的 env 默认值生效）。
func smokeCLIRunEnv(t *testing.T, dir string) []string {
	t.Helper()
	return []string{
		"DB_DRIVER=sqlite",
		"DB_DSN=" + filepath.Join(dir, "cli-smoke.sqlite"),
		"SERVIFY_PORT=" + smokeCLIPort,
	}
}

// TestCLIRunSmokeLifecycle 走 run() 的真实装配路径（LoadConfig → BuildApp →
// 数据库 → BuildServerRuntime → StartRuntime → BuildRouter 全量路由面）：
// sqlite 库起服后 /health 返回 200，SIGTERM 触发收尾段后干净退出。非法
// OTEL_RESOURCE_ATTRIBUTES 让 SetupObservability 确定性失败，顺带覆盖
// "init tracing" 只告警不中断的分支。
func TestCLIRunSmokeLifecycle(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
		[]byte(baseCLIRunConfig(true, "127.0.0.1")), 0o600))

	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIWorker$", "-test.timeout=2m")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CLI_RUN_SUBPROCESS=1",
		"CLI_RUN_VARIANT=smoke",
		"CLI_RUN_DIR="+dir,
		"OTEL_RESOURCE_ATTRIBUTES=not-a-valid-entry",
	)
	cmd.Env = append(cmd.Env, smokeCLIRunEnv(t, dir)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	// 轮询 /health 到 200（BuildRuntime+sqlite 首次建表可能需数秒）。
	// 注意：exec.Cmd 的 stdout 拷贝 goroutine 在 Wait 前一直写 buf，轮询
	// 分支不得读 buf（bytes.Buffer 非并发安全，race 门禁必报）。
	base := fmt.Sprintf("http://127.0.0.1:%s", smokeCLIPort)
	deadline := time.Now().Add(30 * time.Second)
	healthy := false
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/health")
		if err == nil {
			code := resp.StatusCode
			_ = resp.Body.Close()
			if code == http.StatusOK {
				healthy = true
				break
			}
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			_ = cmd.Wait()
			t.Fatalf("subprocess exited early, output:\n%s", out.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.True(t, healthy, "server did not become healthy within 30s")

	// 优雅停机：SIGTERM → WaitForShutdownSignal 返回 → 收尾段 → exit 0。
	// Wait 返回后 stdout 拷贝 goroutine 已结束，读 out 才安全。
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	_ = cmd.Wait()
	require.NotNil(t, cmd.ProcessState)
	assert.EqualValues(t, 0, cmd.ProcessState.ExitCode(), "output:\n%s", out.String())
	assert.Contains(t, out.String(), "init tracing")
	assert.Contains(t, out.String(), "Server exited")
}
