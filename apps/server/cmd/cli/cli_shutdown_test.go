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
	appserver "servify/apps/server/internal/app/server"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMessageRouter 注入 RealtimeRuntime.MessageRouter（导出字段），使
// runtime.Stop 返回错误，无需 seam 即可覆盖生产 rt.Stop 调用的错误分支。
type fakeMessageRouter struct {
	stopErr error
}

func (f *fakeMessageRouter) Start() error { return nil }
func (f *fakeMessageRouter) Stop() error  { return f.stopErr }
func (f *fakeMessageRouter) GetPlatformStats() map[string]interface{} {
	return map[string]interface{}{}
}

// 编译期守卫：fakeMessageRouter 必须持续满足 MessageRouterRuntime。
var _ services.MessageRouterRuntime = (*fakeMessageRouter)(nil)

// newBufferLogger 返回写入缓冲的 logrus logger，供断言 shutdownRuntime 的
// 日志输出。
func newBufferLogger(buf *bytes.Buffer) *logrus.Logger {
	l := logrus.New()
	l.SetOutput(buf)
	l.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	return l
}

// cfgForCLI 返回 shutdownRuntime 测试用的最小配置。
func cfgForCLI(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.GetDefaultConfig()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0
	return cfg
}

// newShutdownRuntimeFixture 构造 shutdownRuntime 所需的真实对象。
func newShutdownRuntimeFixture(t *testing.T) (*logrus.Logger, *bytes.Buffer, *appserver.RealtimeRuntime, *appbootstrap.App) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	buf := &bytes.Buffer{}
	logger := newBufferLogger(buf)
	cfg := cfgForCLI(t)
	rt := buildTestRealtimeRuntime(cfg, logger)
	app := &appbootstrap.App{Logger: logger}
	return logger, buf, rt, app
}

// TestShutdownRuntimeHappyPath 默认路径：真实 runtime/server/app 全部正常
// 关停，只输出 "Server exited"。
func TestShutdownRuntimeHappyPath(t *testing.T) {
	logger, buf, rt, app := newShutdownRuntimeFixture(t)
	srv := appbootstrap.NewHTTPServer(cfgForCLI(t), gin.New(), appbootstrap.HTTPServerOptions{})

	shutdownRuntime(logger, rt, srv, app)

	logged := buf.String()
	assert.Contains(t, logged, "Server exited")
	assert.NotContains(t, logged, "Failed to")
}

// TestShutdownRuntimeStopError 覆盖消息路由停止失败的 Errorf 分支：失败后
// 仍继续关停 server 与 hooks。
func TestShutdownRuntimeStopError(t *testing.T) {
	logger, buf, rt, app := newShutdownRuntimeFixture(t)
	rt.MessageRouter = &fakeMessageRouter{stopErr: errors.New("router boom")}
	srv := appbootstrap.NewHTTPServer(cfgForCLI(t), gin.New(), appbootstrap.HTTPServerOptions{})

	shutdownRuntime(logger, rt, srv, app)

	logged := buf.String()
	assert.Contains(t, logged, "Failed to stop message router: router boom")
	assert.Contains(t, logged, "Server exited")
}

// TestShutdownRuntimeServerError 覆盖 server.Shutdown 失败的 Errorf 分支
// （seam 注入错误；默认路径由 HappyPath 覆盖）。
func TestShutdownRuntimeServerError(t *testing.T) {
	logger, buf, rt, app := newShutdownRuntimeFixture(t)
	srv := appbootstrap.NewHTTPServer(cfgForCLI(t), gin.New(), appbootstrap.HTTPServerOptions{})
	orig := shutdownHTTPServer
	shutdownHTTPServer = func(*http.Server, context.Context) error { return errors.New("shutdown boom") }
	t.Cleanup(func() { shutdownHTTPServer = orig })

	shutdownRuntime(logger, rt, srv, app)

	logged := buf.String()
	assert.Contains(t, logged, "Server forced to shutdown: shutdown boom")
	assert.Contains(t, logged, "Server exited")
}

// TestShutdownRuntimeHookError 覆盖 shutdown hook 失败的 Errorf 分支：真实
// App 结构体携带失败 hook，无需 seam。
func TestShutdownRuntimeHookError(t *testing.T) {
	logger, buf, rt, _ := newShutdownRuntimeFixture(t)
	app := &appbootstrap.App{Logger: logger}
	app.AddShutdownHook(func() error { return errors.New("hook boom") })
	srv := appbootstrap.NewHTTPServer(cfgForCLI(t), gin.New(), appbootstrap.HTTPServerOptions{})

	shutdownRuntime(logger, rt, srv, app)

	logged := buf.String()
	assert.Contains(t, logged, "Failed to run shutdown hooks: hook boom")
	assert.Contains(t, logged, "Server exited")
}

// TestShutdownRuntimeSeamDefaults 验证 seam 默认值即生产实现：对未启动的
// http.Server 调用 Shutdown 返回 nil，真实 runtime 可直接启停。
func TestShutdownRuntimeSeamDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := appbootstrap.NewHTTPServer(cfgForCLI(t), gin.New(), appbootstrap.HTTPServerOptions{})
	require.NoError(t, shutdownHTTPServer(srv, context.Background()))

	rt := buildTestRealtimeRuntime(cfgForCLI(t), newBufferLogger(&bytes.Buffer{}))
	require.NoError(t, startMessageRouter(rt))
	require.NoError(t, rt.Stop(context.Background()))
}

// applyCLIRunFault 在子进程 worker 内按 CLI_RUN_VARIANT 注入 seam 失败，
// 覆盖 run() 在 setupRouter gin panic 之前不可达的错误 fatal 分支。生产进程
// 不设置该 variant。
func applyCLIRunFault(variant string) {
	if variant == "start-failure" {
		startMessageRouter = func(*appserver.RealtimeRuntime) error {
			return errors.New("injected start failure")
		}
	}
}

// TestCLIRunStartRouterFailure 覆盖 run() 的 "Failed to start message
// router" fatal 分支：注入 startMessageRouter 失败后 run() 以 1 退出。
func TestCLIRunStartRouterFailure(t *testing.T) {
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
	out, _ := cmd.CombinedOutput()
	require.NotNil(t, cmd.ProcessState, "output: %s", out)
	assert.EqualValues(t, 1, cmd.ProcessState.ExitCode(), "output: %s", out)
	assert.Contains(t, string(out), "Failed to start message router")
	assert.Contains(t, string(out), "injected start failure")
}
