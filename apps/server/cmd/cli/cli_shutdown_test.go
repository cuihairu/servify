package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	appbootstrap "servify/apps/server/internal/app/bootstrap"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newBufferLogger 返回写入缓冲的 logrus logger，供断言 shutdownRuntime 的
// 日志输出。
func newBufferLogger(buf *bytes.Buffer) *logrus.Logger {
	l := logrus.New()
	l.SetOutput(buf)
	l.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	return l
}

// newShutdownRuntimeFixture 构造 shutdownRuntime 所需的真实对象：真实
// http.Server（未监听）与仅带 logger 的空 App（Shutdown 对空 workers/
// runtime/hooks 安全）。
func newShutdownRuntimeFixture(t *testing.T) (*logrus.Logger, *bytes.Buffer, *appbootstrap.App, *http.Server) {
	t.Helper()
	buf := &bytes.Buffer{}
	logger := newBufferLogger(buf)
	app := &appbootstrap.App{Logger: logger}
	srv := appbootstrap.NewHTTPServer(nil, http.NewServeMux(), appbootstrap.HTTPServerOptions{})
	return logger, buf, app, srv
}

// TestShutdownRuntimeHappyPath 默认路径：app shutdown 与 server 关停全部正常，
// 只输出 "Server exited"。
func TestShutdownRuntimeHappyPath(t *testing.T) {
	logger, buf, app, srv := newShutdownRuntimeFixture(t)

	shutdownRuntime(logger, app, srv)

	logged := buf.String()
	assert.Contains(t, logged, "Server exited")
	assert.NotContains(t, logged, "Failed to")
}

// TestShutdownRuntimeAppError 覆盖 app 级 shutdown（workers/runtime/hooks
// 聚合）失败的 Errorf 分支：失败后仍继续关停 server。
func TestShutdownRuntimeAppError(t *testing.T) {
	logger, buf, app, srv := newShutdownRuntimeFixture(t)
	orig := cliShutdownApp
	cliShutdownApp = func(*appbootstrap.App, context.Context) error { return errors.New("app shutdown boom") }
	t.Cleanup(func() { cliShutdownApp = orig })

	shutdownRuntime(logger, app, srv)

	logged := buf.String()
	assert.Contains(t, logged, "Failed to shutdown cleanly: app shutdown boom")
	assert.Contains(t, logged, "Server exited")
}

// TestShutdownRuntimeServerError 覆盖 server.Shutdown 失败的 Errorf 分支
// （seam 注入错误；默认路径由 HappyPath 覆盖）。
func TestShutdownRuntimeServerError(t *testing.T) {
	logger, buf, app, srv := newShutdownRuntimeFixture(t)
	orig := cliShutdownServer
	cliShutdownServer = func(*http.Server, context.Context) error { return errors.New("shutdown boom") }
	t.Cleanup(func() { cliShutdownServer = orig })

	shutdownRuntime(logger, app, srv)

	logged := buf.String()
	assert.Contains(t, logged, "Server forced to shutdown: shutdown boom")
	assert.Contains(t, logged, "Server exited")
}

// TestShutdownRuntimeSeamDefaults 验证 seam 默认值即生产实现：对未启动的
// http.Server 调用 Shutdown 返回 nil，空 App 的 Shutdown/StartRuntime 也返
// 回 nil（真实 runtime 启动由冒烟子进程测试 TestCLIRunSmokeLifecycle 覆盖）。
func TestShutdownRuntimeSeamDefaults(t *testing.T) {
	_, _, app, srv := newShutdownRuntimeFixture(t)
	require.NoError(t, cliShutdownServer(srv, context.Background()))
	require.NoError(t, cliShutdownApp(app, context.Background()))
	require.NoError(t, cliStartRuntime(app))
}

// applyCLIRunFault 在子进程 worker 内按 CLI_RUN_VARIANT 注入 seam 失败，
// 覆盖 run() 的错误 fatal 分支。生产进程不设置该 variant。
func applyCLIRunFault(variant string) {
	if variant == "start-failure" {
		cliStartRuntime = func(*appbootstrap.App) error {
			return errors.New("injected start failure")
		}
	}
}

// TestCLIRunStartRuntimeFailure 覆盖 run() 的 "Failed to start runtime"
// fatal 分支：sqlite 库让装配推进到 cliStartRuntime，注入失败后 run() 以 1
// 退出。
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
