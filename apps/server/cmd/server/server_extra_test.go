package main

import (
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	appbootstrap "servify/apps/server/internal/app/bootstrap"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestServerMainSchemaModeSkipLog 覆盖 MIGRATIONS_ENABLED=off 时的
// SchemaModeSkip 分支：跳过自动建表，用预迁移好的 sqlite 完整启动。
func TestServerMainSchemaModeSkipLog(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	writeServerConfig(t, dir, baseServerConfig(port))
	dbPath := filepath.Join(dir, "premigrated.db")
	preMigrateSQLite(t, dbPath)

	cmd := runServerSubprocess(t, dir,
		"-db-driver=sqlite", "-dsn="+dbPath,
		"-host=127.0.0.1", fmt.Sprintf("-port=%d", port),
	)
	cmd.Env = append(cmd.Env, "MIGRATIONS_ENABLED=off")
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
	}), "Automatic schema management disabled")

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	waitServerExit(t, cmd, &out)
}

// TestServerMainAutoMigrateEscapeHatch 覆盖 SERVIFY_AUTO_MIGRATE=on 的
// 旧版 AutoMigrate 逃生通道日志（sqlite 默认走 AutoMigrate，但只有显式
// opt-in 才打印该行）。
func TestServerMainAutoMigrateEscapeHatch(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	writeServerConfig(t, dir, baseServerConfig(port))
	dbPath := filepath.Join(dir, "escape.db")

	cmd := runServerSubprocess(t, dir,
		"-db-driver=sqlite", "-dsn="+dbPath,
		"-host=127.0.0.1", fmt.Sprintf("-port=%d", port),
	)
	cmd.Env = append(cmd.Env, "SERVIFY_AUTO_MIGRATE=on")
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
	}), "SERVIFY_AUTO_MIGRATE is set")

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	waitServerExit(t, cmd, &out)
}

// TestServerMainForcedShutdownTimeout 覆盖优雅关停超时的强制退出分支：
// 一个只发了部分 body 的请求让连接保持活动，30s 的 ShutdownContext 耗尽后
// srv.Shutdown 必须以 "Server forced to shutdown" fatal 结束。
func TestServerMainForcedShutdownTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("forced shutdown waits out the 30s grace period")
	}
	dir := t.TempDir()
	port := freePort(t)
	writeServerConfig(t, dir, baseServerConfig(port))
	dbPath := filepath.Join(dir, "forced.db")

	cmd := runServerSubprocess(t, dir,
		"-db-driver=sqlite", "-dsn="+dbPath,
		"-host=127.0.0.1", fmt.Sprintf("-port=%d", port),
	)
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

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/health")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 60*time.Second, 200*time.Millisecond, "server did not become healthy: %s", out.String())

	// 半截请求：声明 100 字节 body 只发 10 字节，服务器在 handler 返回后
	// 会一直排空 body，该连接永远不进入 idle，Shutdown 只能等超时。
	stalled, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer stalled.Close()
	_, err = stalled.Write([]byte("POST /health HTTP/1.1\r\nHost: " + addr +
		"\r\nContent-Type: text/plain\r\nContent-Length: 100\r\n\r\nonly-ten-bytes"))
	require.NoError(t, err)

	// 等服务端确认已读到该请求（gin 访问日志打出 POST "/health"）再发 SIGTERM：
	// 若 SIGTERM 先于服务端读请求头到达，该连接在服务端视角仍是 idle，
	// Shutdown 会直接关闭它并优雅退出 exit 0，强制超时分支根本不触发
	// （高负载 runner 上曾出现该竞态翻面）。
	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "| POST ")
	}, 10*time.Second, 100*time.Millisecond,
		"server did not acknowledge the stalled request: %s", out.String())

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		assert.Error(t, err, "forced shutdown must exit non-zero: %s", out.String())
	case <-time.After(90 * time.Second):
		t.Fatalf("server did not exit after forced shutdown timeout: %s", out.String())
	}
	assert.Contains(t, out.String(), "Server forced to shutdown")
}

// preMigrateSQLite 在父进程里用 bootstrap.AutoMigrate 预建完整 schema，
// 供 MIGRATIONS_ENABLED=off 的启动使用。
func preMigrateSQLite(t *testing.T, dbPath string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, appbootstrap.AutoMigrate(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

// waitOrOutput 轮询 cond 直到为真（正常路径）或超时（此时返回已捕获的输出
// 供断言失败信息使用），并确保日志行已被写出。
func waitOrOutput(t *testing.T, cmd *exec.Cmd, out *syncBuffer, cond func() bool) string {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return out.String()
		}
		if cmd.ProcessState != nil {
			return out.String()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return out.String()
}

func waitServerExit(t *testing.T, cmd *exec.Cmd, out *syncBuffer) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		require.NoError(t, err, "server exited with error: %s", out.String())
	case <-time.After(60 * time.Second):
		t.Fatalf("server did not exit after SIGTERM: %s", out.String())
	}
}
