package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	appbootstrap "servify/apps/server/internal/app/bootstrap"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// applyServerFault 在子进程内按 SERVIFY_SERVER_FAULT 注入 seam 失败/分支，
// 覆盖 main() 的 postgres-only versioned 分支与恒 nil 错误的防御性 fatal。
// 生产进程不设置该环境变量，seam 保持默认（生产实现）。
func applyServerFault(fault string) {
	switch fault {
	case "versioned":
		// versioned 分支 + RunMigrations 成功 + StartRuntime 失败：快速结束，
		// 不真正监听端口。
		resolveSchemaMode = func(string) appbootstrap.SchemaManagementMode {
			return appbootstrap.SchemaModeVersioned
		}
		runVersionedMigrations = func(*gorm.DB) error { return nil }
		startRuntime = func(*appbootstrap.App) error { return errors.New("injected runtime failure") }
	case "versioned-fail":
		resolveSchemaMode = func(string) appbootstrap.SchemaManagementMode {
			return appbootstrap.SchemaModeVersioned
		}
		runVersionedMigrations = func(*gorm.DB) error { return errors.New("injected migration failure") }
	case "workers":
		// 跳过真实 runtime 启动，注入 StartWorkers 失败：在监听端口前退出。
		startRuntime = func(*appbootstrap.App) error { return nil }
		startWorkers = func(*appbootstrap.App) error { return errors.New("injected worker failure") }
	case "shutdown":
		// 跳过 runtime/worker 启动但真正监听 HTTP，注入 App.Shutdown 失败：
		// 走完整 SIGTERM 优雅关停流程。
		startRuntime = func(*appbootstrap.App) error { return nil }
		startWorkers = func(*appbootstrap.App) error { return nil }
		shutdownApp = func(*appbootstrap.App, context.Context) error {
			return errors.New("injected shutdown failure")
		}
	}
}

func runServerFaultSubprocess(t *testing.T, dir, fault string, args ...string) (*exec.Cmd, string) {
	t.Helper()
	cmd := runServerSubprocess(t, dir, args...)
	cmd.Env = append(cmd.Env, "SERVIFY_SERVER_FAULT="+fault)
	out, _ := cmd.CombinedOutput()
	return cmd, string(out)
}

// TestServerMainVersionedMigrationBranch 覆盖 postgres-only 的
// SchemaModeVersioned 分支（case 日志 + RunMigrations 成功返回）与
// StartRuntime 的 fatal 分支。
func TestServerMainVersionedMigrationBranch(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	writeServerConfig(t, dir, baseServerConfig(port))

	cmd, out := runServerFaultSubprocess(t, dir, "versioned",
		"-db-driver=sqlite", "-dsn="+filepath.Join(dir, "versioned.db"),
		"-host=127.0.0.1", fmt.Sprintf("-port=%d", port),
	)
	require.NotNil(t, cmd.ProcessState, "output: %s", out)
	assert.EqualValues(t, 1, cmd.ProcessState.ExitCode(), "output: %s", out)
	assert.Contains(t, out, "Applying versioned database migrations...")
	assert.Contains(t, out, "Failed to start runtime: injected runtime failure")
}

// TestServerMainVersionedMigrationFailure 覆盖 versioned 迁移失败的 fatal。
func TestServerMainVersionedMigrationFailure(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	writeServerConfig(t, dir, baseServerConfig(port))

	cmd, out := runServerFaultSubprocess(t, dir, "versioned-fail",
		"-db-driver=sqlite", "-dsn="+filepath.Join(dir, "versioned-fail.db"),
		"-host=127.0.0.1", fmt.Sprintf("-port=%d", port),
	)
	require.NotNil(t, cmd.ProcessState, "output: %s", out)
	assert.EqualValues(t, 1, cmd.ProcessState.ExitCode(), "output: %s", out)
	assert.Contains(t, out, "Applying versioned database migrations...")
	assert.Contains(t, out, "Failed to run database migrations: injected migration failure")
}

// TestServerMainStartWorkersFailure 覆盖 StartWorkers 的 fatal 分支。
func TestServerMainStartWorkersFailure(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	writeServerConfig(t, dir, baseServerConfig(port))

	cmd, out := runServerFaultSubprocess(t, dir, "workers",
		"-db-driver=sqlite", "-dsn="+filepath.Join(dir, "workers.db"),
		"-host=127.0.0.1", fmt.Sprintf("-port=%d", port),
	)
	require.NotNil(t, cmd.ProcessState, "output: %s", out)
	assert.EqualValues(t, 1, cmd.ProcessState.ExitCode(), "output: %s", out)
	assert.Contains(t, out, "Failed to start workers: injected worker failure")
}

// TestServerMainShutdownErrorLogged 覆盖 app.Shutdown 失败时只记录错误、仍
// 完成 srv.Shutdown 并正常退出的完整关停流程。
func TestServerMainShutdownErrorLogged(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	writeServerConfig(t, dir, baseServerConfig(port))

	cmd := runServerSubprocess(t, dir,
		"-db-driver=sqlite", "-dsn="+filepath.Join(dir, "shutdown.db"),
		"-host=127.0.0.1", fmt.Sprintf("-port=%d", port),
	)
	cmd.Env = append(cmd.Env, "SERVIFY_SERVER_FAULT=shutdown")
	var out syncBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	assert.Contains(t, waitOrOutput(t, cmd, &out, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}), "Starting server")

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	waitServerExit(t, cmd, &out)
	assert.Contains(t, out.String(), "Failed to shutdown cleanly: injected shutdown failure")
	assert.Contains(t, out.String(), "Server exited")
}

// TestServerSeamDefaults 验证 seam 默认值即生产实现：sqlite 走 AutoMigrate、
// 空 App 的生命周期方法返回 nil。
func TestServerSeamDefaults(t *testing.T) {
	assert.Equal(t, appbootstrap.ResolveSchemaMode("sqlite"), resolveSchemaMode("sqlite"))
	assert.Equal(t, appbootstrap.ResolveSchemaMode("postgres"), resolveSchemaMode("postgres"))
	assert.Equal(t, appbootstrap.SchemaModeAutoMigrate, resolveSchemaMode("sqlite"))

	ctx := context.Background()
	assert.NoError(t, startRuntime(&appbootstrap.App{}))
	assert.NoError(t, startWorkers(&appbootstrap.App{}))
	assert.NoError(t, shutdownApp(nil, ctx))
}

// TestApplyServerFaultUnknownIsNoOp 未知 fault 值不改 seam，保证默认路径
// 不受影响。
func TestApplyServerFaultUnknownIsNoOp(t *testing.T) {
	before := fmt.Sprintf("%v %v", resolveSchemaMode("sqlite"), startWorkers(&appbootstrap.App{}))
	applyServerFault("nonexistent")
	after := fmt.Sprintf("%v %v", resolveSchemaMode("sqlite"), startWorkers(&appbootstrap.App{}))
	assert.Equal(t, before, after)
	assert.False(t, strings.Contains(after, "injected"))
}
